package forecast

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/providers"
)

func TestGenerateIsTransparentDeterministicAndVersionBound(t *testing.T) {
	selected := forecastTestPolicy(t)
	price := forecastTestPrice(t, selected.Model, false)
	input := Input{
		RepositoryRevision: "commit-abc",
		TaskFingerprint:    "task-fingerprint-abc",
		TaskClass:          TaskClassBugFix,
		RepositorySize: RepositorySize{
			Files: 1_500, Bytes: 60 * 1024 * 1024,
		},
		LikelyFiles:              []string{"z.go", "a.go", "a.go"},
		ValidationCommands:       []string{"go test ./..."},
		Policy:                   selected,
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
		PriceSnapshot:            &price,
	}
	first, err := Generate(input)
	if err != nil {
		t.Fatal(err)
	}
	input.LikelyFiles = []string{"a.go", "z.go"}
	second, err := Generate(input)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("normalized deterministic forecasts differ:\n%s\n%s", firstJSON, secondJSON)
	}
	digest, err := selected.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first.AlgorithmVersion != AlgorithmVersion ||
		first.Bindings.PolicyDigest != digest ||
		first.Bindings.Model != selected.Model ||
		first.Bindings.Reasoning != selected.Reasoning {
		t.Fatalf("bindings = %#v", first.Bindings)
	}
	if !reflect.DeepEqual(first.Features.LikelyFiles, []string{"a.go", "z.go"}) {
		t.Fatalf("normalized likely files = %#v", first.Features.LikelyFiles)
	}
	if first.Latency.P50Millis != 990_000 ||
		first.Latency.P90Millis != 1_980_000 ||
		first.Tokens.P50 != 116_000 ||
		first.Tokens.P90 != 232_000 ||
		first.ToolCalls.P50 != 23 ||
		first.ToolCalls.P90 != 46 {
		t.Fatalf("ranges = latency %#v, tokens %#v, tools %#v", first.Latency, first.Tokens, first.ToolCalls)
	}
	if len(first.Contributions) != 4 {
		t.Fatalf("contributions = %#v", first.Contributions)
	}
	if !first.Cost.Known ||
		first.Cost.P50 != (providers.ExactAmount{
			Currency: "USD", Numerator: 261, Denominator: 200, Known: true,
		}) ||
		first.Cost.P90 != (providers.ExactAmount{
			Currency: "USD", Numerator: 261, Denominator: 100, Known: true,
		}) {
		t.Fatalf("cost range = %#v", first.Cost)
	}
	if first.EstimateNotice != EstimateNotice ||
		!first.RequiredBeforeExecution ||
		!first.AdvisoryOnly {
		t.Fatalf("presentation contract = %#v", first)
	}
}

func TestGenerateDistinguishesUnknownPriceFromKnownZero(t *testing.T) {
	selected := forecastTestPolicy(t)
	base := Input{
		RepositoryRevision:       "commit-abc",
		TaskFingerprint:          "task-fingerprint-abc",
		TaskClass:                TaskClassSmallChange,
		RepositorySize:           RepositorySize{Files: 10, Bytes: 1_024},
		LikelyFiles:              []string{"main.go"},
		ValidationCommands:       []string{"go test ./..."},
		Policy:                   selected,
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
	}
	unknown, err := Generate(base)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Cost.Known || unknown.Cost.P50.Known ||
		!containsReason(unknown.UncertaintyReasons, UncertaintyPriceUnavailable) {
		t.Fatalf("unknown cost = %#v, reasons = %#v", unknown.Cost, unknown.UncertaintyReasons)
	}

	zero := forecastTestPrice(t, selected.Model, true)
	base.PriceSnapshot = &zero
	knownZero, err := Generate(base)
	if err != nil {
		t.Fatal(err)
	}
	if !knownZero.Cost.Known ||
		!knownZero.Cost.P50.Known ||
		knownZero.Cost.P50.Numerator != 0 ||
		knownZero.Cost.P50.Denominator != 1 ||
		containsReason(knownZero.UncertaintyReasons, UncertaintyPriceUnavailable) {
		t.Fatalf("known zero cost = %#v, reasons = %#v", knownZero.Cost, knownZero.UncertaintyReasons)
	}
}

func TestGenerateRejectsPriceForDifferentModel(t *testing.T) {
	selected := forecastTestPolicy(t)
	different := selected.Model
	different.Revision = "different-revision"
	price := forecastTestPrice(t, different, false)
	_, err := Generate(Input{
		RepositoryRevision:       "commit-abc",
		TaskFingerprint:          "task-fingerprint-abc",
		TaskClass:                TaskClassFeature,
		RepositorySize:           RepositorySize{Files: 10, Bytes: 1_024},
		Policy:                   selected,
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
		PriceSnapshot:            &price,
	})
	if !errors.Is(err, ErrInvalidForecastInput) {
		t.Fatalf("model mismatch error = %v", err)
	}
}

func TestCompareReportsP50AndP90Coverage(t *testing.T) {
	selected := forecastTestPolicy(t)
	price := forecastTestPrice(t, selected.Model, false)
	value, err := Generate(Input{
		RepositoryRevision:       "commit-abc",
		TaskFingerprint:          "task-fingerprint-abc",
		TaskClass:                TaskClassDocumentation,
		RepositorySize:           RepositorySize{Files: 1, Bytes: 1},
		LikelyFiles:              []string{"README.md"},
		ValidationCommands:       []string{"go test ./..."},
		Policy:                   selected,
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
		PriceSnapshot:            &price,
	})
	if err != nil {
		t.Fatal(err)
	}
	actualCost, err := providers.NewExactAmount("USD", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := Compare(value, ActualResult{
		LatencyMillis: value.Latency.P50Millis + 1,
		Usage: providers.Usage{
			Known: true, Source: providers.UsageSourceProvider,
			InputTokens: int64(value.Tokens.P50 + 1),
		},
		Cost:         actualCost,
		ToolCalls:    value.ToolCalls.P90 + 1,
		RepairRounds: 1, HumanInterventions: 2, Accepted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Latency.WithinP50 ||
		!comparison.Latency.WithinP90 ||
		comparison.Tokens.WithinP50 ||
		!comparison.Tokens.WithinP90 ||
		comparison.ToolCalls.WithinP90 ||
		!comparison.Cost.Known {
		t.Fatalf("comparison = %#v", comparison)
	}
}

func TestCompareRejectsMalformedKnownActualCost(t *testing.T) {
	selected := forecastTestPolicy(t)
	value, err := Generate(Input{
		RepositoryRevision:       "commit-abc",
		TaskFingerprint:          "task-fingerprint-abc",
		TaskClass:                TaskClassSmallChange,
		RepositorySize:           RepositorySize{Files: 1, Bytes: 1},
		Policy:                   selected,
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compare(value, ActualResult{
		Usage: providers.Usage{Source: providers.UsageSourceUnknown},
		Cost: providers.ExactAmount{
			Known: true, Currency: "USD", Numerator: 1, Denominator: 0,
		},
	})
	if !errors.Is(err, ErrInvalidForecastInput) {
		t.Fatalf("malformed actual cost error = %v", err)
	}
}

func TestForecastValidateRejectsMutatedPolicyAndPercentiles(t *testing.T) {
	selected := forecastTestPolicy(t)
	value, err := Generate(Input{
		RepositoryRevision:       "commit-validate",
		TaskFingerprint:          "fingerprint-validate",
		TaskClass:                TaskClassFeature,
		RepositorySize:           RepositorySize{Files: 10, Bytes: 1_024},
		LikelyFiles:              []string{"internal/feature.go"},
		ValidationCommands:       []string{"go test ./..."},
		Policy:                   selected,
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Forecast)
	}{
		{
			name: "model",
			mutate: func(candidate *Forecast) {
				candidate.Bindings.Model.Revision = "mutated-revision"
			},
		},
		{
			name: "reasoning",
			mutate: func(candidate *Forecast) {
				candidate.Bindings.Reasoning = domain.ReasoningEffortMinimal
			},
		},
		{
			name: "latency-percentiles",
			mutate: func(candidate *Forecast) {
				candidate.Latency.P90Millis = candidate.Latency.P50Millis - 1
			},
		},
		{
			name: "token-percentiles",
			mutate: func(candidate *Forecast) {
				candidate.Tokens.P90 = candidate.Tokens.P50 - 1
			},
		},
		{
			name: "unknown-cost-carries-value",
			mutate: func(candidate *Forecast) {
				candidate.Cost.P50 = providers.ExactAmount{
					Known: true, Currency: "USD", Denominator: 1,
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := value
			test.mutate(&candidate)
			if err := candidate.Validate(selected); !errors.Is(err, ErrInvalidForecastInput) {
				t.Fatalf("mutated forecast validation error = %v", err)
			}
		})
	}
}

func TestForecastValidateRejectsContributionArithmeticOverflow(t *testing.T) {
	selected := forecastTestPolicy(t)
	value, err := Generate(Input{
		RepositoryRevision:       "commit-overflow",
		TaskFingerprint:          "fingerprint-overflow",
		TaskClass:                TaskClassFeature,
		RepositorySize:           RepositorySize{Files: 1, Bytes: 1},
		Policy:                   selected,
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		contributions []Contribution
		want          string
	}{
		{
			name: "latency-total",
			contributions: []Contribution{
				{Feature: "first", LatencyMillis: domain.Milliseconds(math.MaxInt64)},
				{Feature: "second", LatencyMillis: 1},
			},
			want: "contribution totals overflow",
		},
		{
			name: "tokens-total",
			contributions: []Contribution{
				{Feature: "first", Tokens: domain.TokenCount(math.MaxUint64)},
				{Feature: "second", Tokens: 1},
			},
			want: "contribution totals overflow",
		},
		{
			name: "tools-total",
			contributions: []Contribution{
				{Feature: "first", ToolCalls: math.MaxUint32},
				{Feature: "second", ToolCalls: 1},
			},
			want: "contribution totals overflow",
		},
		{
			name: "percentile-expansion",
			contributions: []Contribution{
				{
					Feature: "first",
					Tokens:  domain.TokenCount(math.MaxInt64/2) + 1,
				},
			},
			want: "P90 expansion overflows",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := value
			candidate.Contributions = test.contributions
			err := candidate.Validate(selected)
			if !errors.Is(err, ErrInvalidForecastInput) ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("overflow validation error = %v", err)
			}
		})
	}
}

func forecastTestPolicy(t *testing.T) policy.Snapshot {
	t.Helper()
	selected, err := policy.Select(policy.SelectionInput{
		BaselineModelRevision: "revision-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func forecastTestPrice(
	t *testing.T,
	model providers.ModelIdentity,
	zero bool,
) providers.PriceSnapshot {
	t.Helper()
	inputNumerator, outputNumerator := int64(5), int64(30)
	if zero {
		inputNumerator, outputNumerator = 0, 0
	}
	input, err := providers.NewExactAmount("USD", inputNumerator, 1)
	if err != nil {
		t.Fatal(err)
	}
	output, err := providers.NewExactAmount("USD", outputNumerator, 1)
	if err != nil {
		t.Fatal(err)
	}
	unknown := providers.UnknownAmount("USD")
	return providers.PriceSnapshot{
		ID: "price-2026-07-30", Model: model,
		Price: providers.TokenPrice{
			Input: input, CachedInput: unknown, CacheWrite: unknown,
			Output: output, Reasoning: unknown,
		},
		EffectiveAt: time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC),
		CapturedAt:  time.Date(2026, time.July, 30, 1, 0, 0, 0, time.UTC),
		Source:      "fixture",
	}
}

func containsReason(values []UncertaintyReason, target UncertaintyReason) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
