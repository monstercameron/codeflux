package forecast

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/providers"
)

const (
	// AlgorithmVersion changes whenever a feature, coefficient, or interval
	// rule changes.
	AlgorithmVersion = "transparent-heuristic-v1"
	// EstimateNotice is mandatory presentation copy for every forecast.
	EstimateNotice = "Estimate, not a promise."
)

var ErrInvalidForecastInput = errors.New("invalid forecast input")

// TaskClass is the declared primary task-shape feature.
type TaskClass string

const (
	TaskClassDocumentation TaskClass = "documentation"
	TaskClassSmallChange   TaskClass = "small-change"
	TaskClassBugFix        TaskClass = "bug-fix"
	TaskClassFeature       TaskClass = "feature"
	TaskClassRefactor      TaskClass = "refactor"
	TaskClassMigration     TaskClass = "migration"
	TaskClassSecurity      TaskClass = "security"
)

func (class TaskClass) valid() bool {
	switch class {
	case TaskClassDocumentation, TaskClassSmallChange, TaskClassBugFix,
		TaskClassFeature, TaskClassRefactor, TaskClassMigration,
		TaskClassSecurity:
		return true
	default:
		return false
	}
}

// RepositorySize records deterministic repository scale features.
type RepositorySize struct {
	Files uint64 `json:"files"`
	Bytes uint64 `json:"bytes"`
}

// Bindings make applicability and replay evidence explicit.
type Bindings struct {
	RepositoryRevision       string                  `json:"repository_revision"`
	TaskFingerprint          string                  `json:"task_fingerprint"`
	PolicyDigest             string                  `json:"policy_digest"`
	Model                    providers.ModelIdentity `json:"model"`
	Reasoning                domain.ReasoningEffort  `json:"reasoning"`
	ToolConfigurationVersion string                  `json:"tool_configuration_version"`
	ValidationProfileVersion string                  `json:"validation_profile_version"`
}

// Features is the normalized input retained for later calibration.
type Features struct {
	TaskClass          TaskClass      `json:"task_class"`
	RepositorySize     RepositorySize `json:"repository_size"`
	LikelyFiles        []string       `json:"likely_files"`
	ValidationCommands []string       `json:"validation_commands"`
}

// Input contains every value read by the heuristic.
type Input struct {
	RepositoryRevision       string
	TaskFingerprint          string
	TaskClass                TaskClass
	RepositorySize           RepositorySize
	LikelyFiles              []string
	ValidationCommands       []string
	Policy                   policy.Snapshot
	ToolConfigurationVersion string
	ValidationProfileVersion string
	PriceSnapshot            *providers.PriceSnapshot
}

// Contribution explains one additive P50 heuristic component.
type Contribution struct {
	Feature       string              `json:"feature"`
	LatencyMillis domain.Milliseconds `json:"latency_ms"`
	Tokens        domain.TokenCount   `json:"tokens"`
	ToolCalls     uint32              `json:"tool_calls"`
}

// UsageEstimate is an inspectable estimated provider usage shape.
type UsageEstimate struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

func (usage UsageEstimate) providerUsage() providers.Usage {
	return providers.Usage{
		Known: true, Source: providers.UsageSourceEstimated,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
	}
}

func (usage UsageEstimate) total() domain.TokenCount {
	return domain.TokenCount(usage.InputTokens + usage.OutputTokens)
}

// LatencyRange is a wall-clock P50/P90 interval.
type LatencyRange struct {
	P50Millis domain.Milliseconds `json:"p50_ms"`
	P90Millis domain.Milliseconds `json:"p90_ms"`
}

// TokenRange includes both totals and the category shapes used for pricing.
type TokenRange struct {
	P50      domain.TokenCount `json:"p50"`
	P90      domain.TokenCount `json:"p90"`
	P50Usage UsageEstimate     `json:"p50_usage"`
	P90Usage UsageEstimate     `json:"p90_usage"`
}

// ToolCallRange is a P50/P90 count interval.
type ToolCallRange struct {
	P50 uint32 `json:"p50"`
	P90 uint32 `json:"p90"`
}

// CostRange preserves rational cost and explicit unknown pricing.
type CostRange struct {
	Known bool                  `json:"known"`
	P50   providers.ExactAmount `json:"p50"`
	P90   providers.ExactAmount `json:"p90"`
}

// PriceAssumptions identify the immutable snapshot used for the estimate.
type PriceAssumptions struct {
	Known      bool      `json:"known"`
	SnapshotID string    `json:"snapshot_id,omitempty"`
	CapturedAt time.Time `json:"captured_at,omitempty"`
	Source     string    `json:"source,omitempty"`
}

// UncertaintyReason is a stable, machine-readable caveat.
type UncertaintyReason string

const (
	UncertaintyPriceUnavailable  UncertaintyReason = "price-unavailable"
	UncertaintyScopeNotLocalized UncertaintyReason = "scope-not-localized"
	UncertaintyValidationUnknown UncertaintyReason = "validation-unknown"
	UncertaintyLargeRepository   UncertaintyReason = "large-repository"
	UncertaintyWideLikelyFileSet UncertaintyReason = "wide-likely-file-set"
)

// Forecast is a transparent advisory estimate bound to the fixed policy.
type Forecast struct {
	AlgorithmVersion        string              `json:"algorithm_version"`
	Bindings                Bindings            `json:"bindings"`
	Features                Features            `json:"features"`
	Contributions           []Contribution      `json:"contributions"`
	Latency                 LatencyRange        `json:"latency"`
	Tokens                  TokenRange          `json:"tokens"`
	ToolCalls               ToolCallRange       `json:"tool_calls"`
	Cost                    CostRange           `json:"cost"`
	PriceAssumptions        PriceAssumptions    `json:"price_assumptions"`
	UncertaintyReasons      []UncertaintyReason `json:"uncertainty_reasons"`
	EstimateNotice          string              `json:"estimate_notice"`
	RequiredBeforeExecution bool                `json:"required_before_execution"`
	AdvisoryOnly            bool                `json:"advisory_only"`
}

// Generate applies documented integer coefficients and a fixed 2x P90 rule.
func Generate(input Input) (Forecast, error) {
	if err := input.Policy.Validate(); err != nil {
		return Forecast{}, fmt.Errorf("%w: policy: %v", ErrInvalidForecastInput, err)
	}
	if !input.TaskClass.valid() {
		return Forecast{}, fmt.Errorf("%w: unsupported task class", ErrInvalidForecastInput)
	}
	fields := []struct {
		name  string
		value string
	}{
		{name: "repository revision", value: input.RepositoryRevision},
		{name: "task fingerprint", value: input.TaskFingerprint},
		{name: "tool configuration version", value: input.ToolConfigurationVersion},
		{name: "validation profile version", value: input.ValidationProfileVersion},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) != field.value ||
			field.value == "" ||
			len(field.value) > 512 {
			return Forecast{}, fmt.Errorf("%w: %s must be non-empty, trimmed, and bounded", ErrInvalidForecastInput, field.name)
		}
	}
	if input.RepositorySize.Files > 10_000_000 ||
		input.RepositorySize.Bytes > 10*1024*1024*1024*1024 {
		return Forecast{}, fmt.Errorf("%w: repository size exceeds heuristic bounds", ErrInvalidForecastInput)
	}
	likelyFiles, err := normalizeValues("likely file", input.LikelyFiles, 512)
	if err != nil {
		return Forecast{}, err
	}
	validation, err := normalizeValues("validation command", input.ValidationCommands, 128)
	if err != nil {
		return Forecast{}, err
	}
	digest, err := input.Policy.Digest()
	if err != nil {
		return Forecast{}, fmt.Errorf("%w: policy digest: %v", ErrInvalidForecastInput, err)
	}
	if input.PriceSnapshot != nil {
		if !reflect.DeepEqual(input.PriceSnapshot.Model, input.Policy.Model) {
			return Forecast{}, fmt.Errorf("%w: price snapshot model differs from selected model", ErrInvalidForecastInput)
		}
		if strings.TrimSpace(input.PriceSnapshot.ID) == "" {
			return Forecast{}, fmt.Errorf("%w: price snapshot ID is required", ErrInvalidForecastInput)
		}
		if input.PriceSnapshot.EffectiveAt.IsZero() ||
			input.PriceSnapshot.CapturedAt.IsZero() ||
			strings.TrimSpace(input.PriceSnapshot.Source) == "" {
			return Forecast{}, fmt.Errorf("%w: price snapshot timestamp and source are required", ErrInvalidForecastInput)
		}
	}

	contributions := []Contribution{classContribution(input.TaskClass)}
	fileBuckets := boundedCeiling(input.RepositorySize.Files, 1_000, 10)
	byteBuckets := boundedCeiling(input.RepositorySize.Bytes, 50*1024*1024, 10)
	if fileBuckets+byteBuckets > 0 {
		contributions = append(contributions, Contribution{
			Feature:       "repository-size",
			LatencyMillis: domain.Milliseconds((fileBuckets + byteBuckets) * 30_000),
			Tokens:        domain.TokenCount((fileBuckets * 5_000) + (byteBuckets * 2_500)),
			ToolCalls:     uint32(fileBuckets/2 + byteBuckets/2),
		})
	}
	if len(likelyFiles) > 0 {
		contributions = append(contributions, Contribution{
			Feature:       "likely-files",
			LatencyMillis: domain.Milliseconds(len(likelyFiles) * 45_000),
			Tokens:        domain.TokenCount(len(likelyFiles) * 4_000),
			ToolCalls:     uint32(len(likelyFiles)),
		})
	}
	if len(validation) > 0 {
		contributions = append(contributions, Contribution{
			Feature:       "validation-commands",
			LatencyMillis: domain.Milliseconds(len(validation) * 60_000),
			Tokens:        domain.TokenCount(len(validation) * 3_000),
			ToolCalls:     uint32(len(validation)),
		})
	}

	var latencyP50 domain.Milliseconds
	var tokensP50 domain.TokenCount
	var toolsP50 uint32
	for _, contribution := range contributions {
		latencyP50 += contribution.LatencyMillis
		tokensP50 += contribution.Tokens
		toolsP50 += contribution.ToolCalls
	}
	p50Usage := splitUsage(tokensP50)
	p90Usage := splitUsage(tokensP50 * 2)
	cost := CostRange{
		P50: providers.UnknownAmount(""),
		P90: providers.UnknownAmount(""),
	}
	price := PriceAssumptions{}
	if input.PriceSnapshot != nil {
		cost.P50 = input.PriceSnapshot.Cost(p50Usage.providerUsage())
		cost.P90 = input.PriceSnapshot.Cost(p90Usage.providerUsage())
		cost.Known = cost.P50.Known && cost.P90.Known
		if !cost.Known {
			currency := priceCurrency(input.PriceSnapshot.Price)
			cost.P50 = providers.UnknownAmount(currency)
			cost.P90 = providers.UnknownAmount(currency)
		}
		price = PriceAssumptions{
			Known: cost.Known, SnapshotID: input.PriceSnapshot.ID,
			CapturedAt: input.PriceSnapshot.CapturedAt,
			Source:     input.PriceSnapshot.Source,
		}
	}
	reasons := uncertaintyReasons(input.RepositorySize, likelyFiles, validation, cost.Known)
	value := Forecast{
		AlgorithmVersion: AlgorithmVersion,
		Bindings: Bindings{
			RepositoryRevision: input.RepositoryRevision,
			TaskFingerprint:    input.TaskFingerprint,
			PolicyDigest:       digest, Model: input.Policy.Model,
			Reasoning:                input.Policy.Reasoning,
			ToolConfigurationVersion: input.ToolConfigurationVersion,
			ValidationProfileVersion: input.ValidationProfileVersion,
		},
		Features: Features{
			TaskClass: input.TaskClass, RepositorySize: input.RepositorySize,
			LikelyFiles: likelyFiles, ValidationCommands: validation,
		},
		Contributions: contributions,
		Latency: LatencyRange{
			P50Millis: latencyP50,
			P90Millis: latencyP50 * 2,
		},
		Tokens: TokenRange{
			P50: p50Usage.total(), P90: p90Usage.total(),
			P50Usage: p50Usage, P90Usage: p90Usage,
		},
		ToolCalls: ToolCallRange{P50: toolsP50, P90: toolsP50 * 2},
		Cost:      cost, PriceAssumptions: price,
		UncertaintyReasons:      reasons,
		EstimateNotice:          EstimateNotice,
		RequiredBeforeExecution: true,
		AdvisoryOnly:            true,
	}
	if err := value.Validate(input.Policy); err != nil {
		return Forecast{}, err
	}
	return value, nil
}

// Validate checks a persisted or transported forecast against its exact
// selected policy. This rejects mutated advisory output before it can become
// execution-preflight or calibration evidence.
func (value Forecast) Validate(expectedPolicy policy.Snapshot) error {
	if err := expectedPolicy.Validate(); err != nil {
		return fmt.Errorf("%w: policy: %v", ErrInvalidForecastInput, err)
	}
	digest, err := expectedPolicy.Digest()
	if err != nil {
		return fmt.Errorf("%w: policy digest: %v", ErrInvalidForecastInput, err)
	}
	switch {
	case value.AlgorithmVersion != AlgorithmVersion:
		return fmt.Errorf("%w: unsupported algorithm version", ErrInvalidForecastInput)
	case value.EstimateNotice != EstimateNotice:
		return fmt.Errorf("%w: estimate notice is required", ErrInvalidForecastInput)
	case !value.RequiredBeforeExecution:
		return fmt.Errorf("%w: pre-execution presentation is required", ErrInvalidForecastInput)
	case !value.AdvisoryOnly:
		return fmt.Errorf("%w: forecast must remain advisory", ErrInvalidForecastInput)
	case value.Bindings.PolicyDigest != digest:
		return fmt.Errorf("%w: policy digest differs", ErrInvalidForecastInput)
	case !reflect.DeepEqual(value.Bindings.Model, expectedPolicy.Model):
		return fmt.Errorf("%w: selected model differs from policy", ErrInvalidForecastInput)
	case value.Bindings.Reasoning != expectedPolicy.Reasoning:
		return fmt.Errorf("%w: reasoning effort differs from policy", ErrInvalidForecastInput)
	}
	for name, binding := range map[string]string{
		"repository revision":        value.Bindings.RepositoryRevision,
		"task fingerprint":           value.Bindings.TaskFingerprint,
		"tool configuration version": value.Bindings.ToolConfigurationVersion,
		"validation profile version": value.Bindings.ValidationProfileVersion,
	} {
		if strings.TrimSpace(binding) != binding ||
			binding == "" ||
			len(binding) > 512 {
			return fmt.Errorf("%w: %s is invalid", ErrInvalidForecastInput, name)
		}
	}
	if !value.Features.TaskClass.valid() ||
		value.Features.RepositorySize.Files > 10_000_000 ||
		value.Features.RepositorySize.Bytes > 10*1024*1024*1024*1024 {
		return fmt.Errorf("%w: feature values are invalid", ErrInvalidForecastInput)
	}
	if !normalizedValues(value.Features.LikelyFiles, 512) ||
		!normalizedValues(value.Features.ValidationCommands, 128) {
		return fmt.Errorf("%w: feature lists are not normalized", ErrInvalidForecastInput)
	}
	if len(value.Contributions) == 0 {
		return fmt.Errorf("%w: transparent contributions are required", ErrInvalidForecastInput)
	}
	var latencyP50 domain.Milliseconds
	var tokensP50 domain.TokenCount
	var toolsP50 uint32
	for _, contribution := range value.Contributions {
		if strings.TrimSpace(contribution.Feature) != contribution.Feature ||
			contribution.Feature == "" ||
			contribution.LatencyMillis < 0 {
			return fmt.Errorf("%w: contribution is invalid", ErrInvalidForecastInput)
		}
		if contribution.LatencyMillis >
			domain.Milliseconds(math.MaxInt64)-latencyP50 ||
			contribution.Tokens > domain.TokenCount(math.MaxUint64)-tokensP50 ||
			contribution.ToolCalls > math.MaxUint32-toolsP50 {
			return fmt.Errorf("%w: contribution totals overflow", ErrInvalidForecastInput)
		}
		latencyP50 += contribution.LatencyMillis
		tokensP50 += contribution.Tokens
		toolsP50 += contribution.ToolCalls
	}
	if latencyP50 > domain.Milliseconds(math.MaxInt64/2) ||
		tokensP50 > domain.TokenCount(math.MaxInt64/2) ||
		toolsP50 > math.MaxUint32/2 {
		return fmt.Errorf("%w: P90 expansion overflows", ErrInvalidForecastInput)
	}
	if value.Latency.P50Millis < 0 ||
		value.Latency.P50Millis > value.Latency.P90Millis ||
		value.Latency.P50Millis != latencyP50 ||
		value.Latency.P90Millis != value.Latency.P50Millis*2 {
		return fmt.Errorf("%w: latency percentiles are inconsistent", ErrInvalidForecastInput)
	}
	if value.Tokens.P50 > value.Tokens.P90 ||
		value.Tokens.P50 != tokensP50 ||
		value.Tokens.P90 != value.Tokens.P50*2 ||
		!usageEstimateValid(value.Tokens.P50Usage, value.Tokens.P50) ||
		!usageEstimateValid(value.Tokens.P90Usage, value.Tokens.P90) {
		return fmt.Errorf("%w: token percentiles are inconsistent", ErrInvalidForecastInput)
	}
	if value.ToolCalls.P50 > value.ToolCalls.P90 ||
		value.ToolCalls.P50 != toolsP50 ||
		value.ToolCalls.P90 != value.ToolCalls.P50*2 {
		return fmt.Errorf("%w: tool-call percentiles are inconsistent", ErrInvalidForecastInput)
	}
	if err := validateCostRange(value.Cost); err != nil {
		return err
	}
	if value.Cost.Known {
		if !value.PriceAssumptions.Known ||
			strings.TrimSpace(value.PriceAssumptions.SnapshotID) == "" ||
			value.PriceAssumptions.CapturedAt.IsZero() ||
			strings.TrimSpace(value.PriceAssumptions.Source) == "" {
			return fmt.Errorf("%w: known cost lacks price evidence", ErrInvalidForecastInput)
		}
	} else if value.PriceAssumptions.Known {
		return fmt.Errorf("%w: unknown cost has known price assumptions", ErrInvalidForecastInput)
	}
	if !uncertaintyReasonsValid(value.UncertaintyReasons, value.Cost.Known) {
		return fmt.Errorf("%w: uncertainty reasons are inconsistent", ErrInvalidForecastInput)
	}
	return nil
}

func normalizedValues(values []string, maximum int) bool {
	if len(values) > maximum {
		return false
	}
	for index, value := range values {
		if strings.TrimSpace(value) != value || value == "" || len(value) > 1_024 {
			return false
		}
		if index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func usageEstimateValid(value UsageEstimate, expected domain.TokenCount) bool {
	return value.InputTokens >= 0 &&
		value.OutputTokens >= 0 &&
		domain.TokenCount(value.InputTokens)+domain.TokenCount(value.OutputTokens) == expected
}

func validateCostRange(value CostRange) error {
	if !value.Known {
		if value.P50.Known || value.P90.Known ||
			value.P50.Numerator != 0 || value.P50.Denominator != 0 ||
			value.P90.Numerator != 0 || value.P90.Denominator != 0 {
			return fmt.Errorf("%w: unknown cost carries exact values", ErrInvalidForecastInput)
		}
		if value.P50.Currency != value.P90.Currency {
			return fmt.Errorf("%w: unknown cost currencies differ", ErrInvalidForecastInput)
		}
		return nil
	}
	if err := validateKnownAmount(value.P50); err != nil {
		return err
	}
	if err := validateKnownAmount(value.P90); err != nil {
		return err
	}
	if value.P50.Currency != value.P90.Currency {
		return fmt.Errorf("%w: cost currencies differ", ErrInvalidForecastInput)
	}
	p50 := new(big.Rat).SetFrac(
		big.NewInt(value.P50.Numerator),
		big.NewInt(value.P50.Denominator),
	)
	p90 := new(big.Rat).SetFrac(
		big.NewInt(value.P90.Numerator),
		big.NewInt(value.P90.Denominator),
	)
	if p50.Cmp(p90) > 0 {
		return fmt.Errorf("%w: cost P50 exceeds P90", ErrInvalidForecastInput)
	}
	return nil
}

func validateKnownAmount(value providers.ExactAmount) error {
	if !value.Known ||
		value.Currency != strings.ToUpper(value.Currency) ||
		len(value.Currency) != 3 ||
		value.Numerator < 0 ||
		value.Denominator < 1 {
		return fmt.Errorf("%w: known cost is malformed", ErrInvalidForecastInput)
	}
	return nil
}

func uncertaintyReasonsValid(values []UncertaintyReason, costKnown bool) bool {
	seen := make(map[UncertaintyReason]struct{}, len(values))
	priceUnavailable := false
	for _, value := range values {
		switch value {
		case UncertaintyPriceUnavailable:
			priceUnavailable = true
		case UncertaintyScopeNotLocalized,
			UncertaintyValidationUnknown,
			UncertaintyLargeRepository,
			UncertaintyWideLikelyFileSet:
		default:
			return false
		}
		if _, found := seen[value]; found {
			return false
		}
		seen[value] = struct{}{}
	}
	return priceUnavailable != costKnown
}

func priceCurrency(price providers.TokenPrice) string {
	for _, amount := range []providers.ExactAmount{
		price.Input,
		price.Output,
		price.CachedInput,
		price.CacheWrite,
		price.Reasoning,
	} {
		if len(amount.Currency) == 3 {
			return amount.Currency
		}
	}
	return ""
}

func classContribution(class TaskClass) Contribution {
	switch class {
	case TaskClassDocumentation:
		return Contribution{Feature: "task-class:documentation", LatencyMillis: 180_000, Tokens: 30_000, ToolCalls: 6}
	case TaskClassSmallChange:
		return Contribution{Feature: "task-class:small-change", LatencyMillis: 360_000, Tokens: 50_000, ToolCalls: 10}
	case TaskClassBugFix:
		return Contribution{Feature: "task-class:bug-fix", LatencyMillis: 720_000, Tokens: 90_000, ToolCalls: 18}
	case TaskClassFeature:
		return Contribution{Feature: "task-class:feature", LatencyMillis: 1_200_000, Tokens: 140_000, ToolCalls: 26}
	case TaskClassRefactor:
		return Contribution{Feature: "task-class:refactor", LatencyMillis: 1_440_000, Tokens: 160_000, ToolCalls: 30}
	case TaskClassMigration:
		return Contribution{Feature: "task-class:migration", LatencyMillis: 1_800_000, Tokens: 200_000, ToolCalls: 36}
	default:
		return Contribution{Feature: "task-class:security", LatencyMillis: 2_100_000, Tokens: 220_000, ToolCalls: 40}
	}
}

func boundedCeiling(value, unit, maximum uint64) uint64 {
	if value == 0 {
		return 0
	}
	result := (value-1)/unit + 1
	if result > maximum {
		return maximum
	}
	return result
}

func splitUsage(total domain.TokenCount) UsageEstimate {
	input := int64(total * 3 / 4)
	return UsageEstimate{InputTokens: input, OutputTokens: int64(total) - input}
}

func normalizeValues(name string, values []string, maximum int) ([]string, error) {
	if len(values) > maximum {
		return nil, fmt.Errorf("%w: too many %ss", ErrInvalidForecastInput, name)
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != value || value == "" || len(value) > 1_024 {
			return nil, fmt.Errorf("%w: %s must be non-empty, trimmed, and bounded", ErrInvalidForecastInput, name)
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func uncertaintyReasons(
	size RepositorySize,
	files []string,
	validation []string,
	priceKnown bool,
) []UncertaintyReason {
	var reasons []UncertaintyReason
	if !priceKnown {
		reasons = append(reasons, UncertaintyPriceUnavailable)
	}
	if len(files) == 0 {
		reasons = append(reasons, UncertaintyScopeNotLocalized)
	}
	if len(validation) == 0 {
		reasons = append(reasons, UncertaintyValidationUnknown)
	}
	if size.Files > 100_000 || size.Bytes > 10*1024*1024*1024 {
		reasons = append(reasons, UncertaintyLargeRepository)
	}
	if len(files) > 20 {
		reasons = append(reasons, UncertaintyWideLikelyFileSet)
	}
	return reasons
}
