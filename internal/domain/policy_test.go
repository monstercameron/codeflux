package domain

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestMoneyUsesExactMinorUnits(t *testing.T) {
	usd, err := ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewMoney(usd, 125)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewMoney(usd, 75)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := first.Add(second)
	if err != nil {
		t.Fatal(err)
	}
	if sum.MinorUnits != 200 || sum.Currency != usd {
		t.Fatalf("sum = %#v, want USD 200 minor units", sum)
	}

	eur, err := ParseCurrencyCode("EUR")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Add(Money{Currency: eur, MinorUnits: 1}); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("currency mismatch error = %v", err)
	}
	if _, err := (Money{Currency: usd, MinorUnits: math.MaxInt64}).Add(
		Money{Currency: usd, MinorUnits: 1},
	); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("overflow error = %v", err)
	}
	if _, err := ParseCurrencyCode("usd"); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("lowercase currency error = %v", err)
	}
}

func TestTokenUsageKeepsUnknownDistinctFromZero(t *testing.T) {
	unknown := TokenUsage{}
	total, known, err := unknown.Total()
	if err != nil || known || total != 0 {
		t.Fatalf("unknown total = %d, %v, %v", total, known, err)
	}
	if err := (TokenUsage{Input: 1}).Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("unknown usage with count error = %v", err)
	}

	usage := TokenUsage{
		Known:       true,
		Input:       10,
		CachedInput: 5,
		CacheWrite:  3,
		Output:      7,
		Reasoning:   2,
		ProviderSpecific: map[string]TokenCount{
			"accepted_prediction": 4,
			"audio":               6,
		},
	}
	total, known, err = usage.Total()
	if err != nil || !known || total != 37 {
		t.Fatalf("known total = %d, %v, %v; want 37, true", total, known, err)
	}
}

func TestBudgetAndForecastValidation(t *testing.T) {
	budgetID, err := ParseBudgetID("bdg_" + uuidV7Fixture)
	if err != nil {
		t.Fatal(err)
	}
	usd, err := ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	budget := TaskBudget{
		ID:                    budgetID,
		WarningCost:           Money{Currency: usd, MinorUnits: 500},
		HardStopCost:          Money{Currency: usd, MinorUnits: 1_000},
		WarningTokens:         50_000,
		HardStopTokens:        100_000,
		WarningWallClock:      60_000,
		HardStopWallClock:     120_000,
		MaximumProviderCalls:  10,
		MaximumRepairRounds:   2,
		MaximumToolExecutions: 50,
	}
	if err := budget.Validate(); err != nil {
		t.Fatalf("valid budget: %v", err)
	}
	budget.WarningTokens = budget.HardStopTokens + 1
	if err := budget.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("invalid budget error = %v", err)
	}

	forecast := ForecastRange{
		LatencyKnown:     true,
		LatencyP50Millis: 10_000,
		LatencyP90Millis: 30_000,
		TokensKnown:      true,
		TokensP50:        20_000,
		TokensP90:        40_000,
		CostKnown:        true,
		CostP50:          Money{Currency: usd, MinorUnits: 100},
		CostP90:          Money{Currency: usd, MinorUnits: 300},
	}
	if err := forecast.Validate(); err != nil {
		t.Fatalf("valid forecast: %v", err)
	}
	forecast.LatencyP50Millis = forecast.LatencyP90Millis + 1
	if err := forecast.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("invalid forecast error = %v", err)
	}
	if err := (ForecastRange{}).Validate(); err != nil {
		t.Fatalf("fully unknown forecast: %v", err)
	}
}

func TestPolicyEnumsAndTypedReasons(t *testing.T) {
	policy := ExecutionPolicy{
		Preset:            PolicyPresetBalanced,
		Reasoning:         ReasoningEffortExtended,
		Risk:              RiskLevelProtected,
		RequiredAssurance: AssuranceLevelFullyEvaluated,
	}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	policy.RequiredAssurance = AssuranceLevelInvalidated
	if err := policy.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("invalid assurance error = %v", err)
	}
	if !ValidationSeverityBlocking.IsValid() ||
		!PauseReasonAwaitingAuthority.IsValid() ||
		!FailureReasonValidation.IsValid() ||
		!CancellationReasonUserRequested.IsValid() ||
		!RollbackReasonRegression.IsValid() ||
		!InvalidationReasonSourceChanged.IsValid() {
		t.Fatal("declared policy enum is invalid")
	}
	if reflect.TypeOf(PauseReason("")).AssignableTo(reflect.TypeOf(FailureReason(""))) {
		t.Fatal("pause and failure reasons are assignable")
	}
}

func TestModelCapabilityMetadataValidation(t *testing.T) {
	providerID, err := ParseProviderID("prv_" + uuidV7Fixture)
	if err != nil {
		t.Fatal(err)
	}
	metadata := ModelCapabilityMetadata{
		Provider:         providerID,
		Model:            "fixture-model",
		Revision:         "2026-07-30",
		ContextTokens:    128_000,
		MaxOutputTokens:  16_000,
		ToolCalling:      true,
		StructuredOutput: true,
		Streaming:        true,
		PromptCaching:    true,
		Reasoning:        true,
	}
	if err := metadata.Validate(); err != nil {
		t.Fatal(err)
	}
	metadata.MaxOutputTokens = metadata.ContextTokens + 1
	if err := metadata.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("invalid metadata error = %v", err)
	}
}

func TestPolicySerializationIsDeterministic(t *testing.T) {
	providerID, err := ParseProviderID("prv_" + uuidV7Fixture)
	if err != nil {
		t.Fatal(err)
	}
	budgetID, err := ParseBudgetID("bdg_" + uuidV7Fixture)
	if err != nil {
		t.Fatal(err)
	}
	usd, err := ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}

	values := []any{
		PolicyPresetCorrectness,
		PolicyPresetBalanced,
		PolicyPresetFast,
		PolicyPresetEconomical,
		ReasoningEffortMinimal,
		ReasoningEffortStandard,
		ReasoningEffortExtended,
		ReasoningEffortMaximum,
		ExecutionPolicy{
			Preset:            PolicyPresetCorrectness,
			Reasoning:         ReasoningEffortMaximum,
			Risk:              RiskLevelElevated,
			RequiredAssurance: AssuranceLevelContractChecked,
		},
		ModelCapabilityMetadata{
			Provider:         providerID,
			Model:            "fixture-model",
			Revision:         "revision-1",
			ContextTokens:    64_000,
			MaxOutputTokens:  8_000,
			ToolCalling:      true,
			StructuredOutput: true,
		},
		usd,
		Money{Currency: usd, MinorUnits: 123},
		TokenCount(456),
		TokenUsage{
			Known: true,
			Input: 10,
			ProviderSpecific: map[string]TokenCount{
				"zeta":  2,
				"alpha": 1,
			},
		},
		TaskBudget{
			ID:                budgetID,
			WarningCost:       Money{Currency: usd, MinorUnits: 100},
			HardStopCost:      Money{Currency: usd, MinorUnits: 200},
			WarningTokens:     1_000,
			HardStopTokens:    2_000,
			WarningWallClock:  10_000,
			HardStopWallClock: 20_000,
		},
		ForecastRange{
			LatencyKnown:     true,
			LatencyP50Millis: 1_000,
			LatencyP90Millis: 2_000,
			TokensKnown:      true,
			TokensP50:        100,
			TokensP90:        200,
			CostKnown:        true,
			CostP50:          Money{Currency: usd, MinorUnits: 10},
			CostP90:          Money{Currency: usd, MinorUnits: 20},
		},
		Milliseconds(789),
		RiskLevelRoutine,
		RiskLevelElevated,
		RiskLevelProtected,
		AssuranceLevelFullyEvaluated,
		AssuranceLevelModelVerified,
		AssuranceLevelContractChecked,
		AssuranceLevelRuntimeOnly,
		AssuranceLevelInvalidated,
		ValidationSeverityAdvisory,
		ValidationSeverityWarning,
		ValidationSeverityError,
		ValidationSeverityBlocking,
		PauseReasonUserRequested,
		PauseReasonAwaitingAuthority,
		PauseReasonBudgetExhausted,
		PauseReasonProviderUnavailable,
		PauseReasonValidationFailed,
		PauseReasonRecoveryUncertain,
		FailureReasonPlanning,
		FailureReasonTool,
		FailureReasonProvider,
		FailureReasonValidation,
		FailureReasonInternal,
		FailureReasonCorruption,
		CancellationReasonUserRequested,
		CancellationReasonSuperseded,
		CancellationReasonShutdown,
		CancellationReasonBudgetExhausted,
		RollbackReasonUserRequested,
		RollbackReasonRegression,
		RollbackReasonValidationInvalidated,
		RollbackReasonRecovery,
		InvalidationReasonDependencyChanged,
		InvalidationReasonSourceChanged,
		InvalidationReasonPolicyChanged,
		InvalidationReasonDefectAttributed,
		InvalidationReasonEvidenceExpired,
	}

	for _, value := range values {
		first, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %T: %v", value, err)
		}
		for iteration := 0; iteration < 10; iteration++ {
			next, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("marshal %T: %v", value, err)
			}
			if string(next) != string(first) {
				t.Fatalf("%T serialization changed:\n%s\n%s", value, first, next)
			}
		}
	}

	left := TokenUsage{
		Known: true,
		ProviderSpecific: map[string]TokenCount{
			"alpha": 1,
			"zeta":  2,
		},
	}
	right := TokenUsage{
		Known: true,
		ProviderSpecific: map[string]TokenCount{
			"zeta":  2,
			"alpha": 1,
		},
	}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	if string(leftJSON) != string(rightJSON) {
		t.Fatalf("semantic map order changed JSON:\n%s\n%s", leftJSON, rightJSON)
	}
}

func TestPolicySourceUsesNoBinaryFloatingPoint(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	policyPath := filepath.Join(filepath.Dir(currentFile), "policy.go")
	file, err := parser.ParseFile(token.NewFileSet(), policyPath, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && (identifier.Name == "float32" || identifier.Name == "float64") {
			t.Errorf("binary floating-point type appears in policy source at %d", identifier.Pos())
		}
		return true
	})
}
