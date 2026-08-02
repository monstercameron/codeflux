package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrInvalidDomainValue classifies an invalid policy-bearing value.
var ErrInvalidDomainValue = errors.New("invalid domain value")

// ValueError identifies one invalid domain value without exposing internal data.
type ValueError struct {
	Field  string
	Reason string
}

func (e *ValueError) Error() string {
	return fmt.Sprintf("%s is invalid: %s", e.Field, e.Reason)
}

// Unwrap allows errors.Is(err, ErrInvalidDomainValue).
func (e *ValueError) Unwrap() error {
	return ErrInvalidDomainValue
}

// PolicyPreset selects a user latency/cost posture without weakening correctness.
type PolicyPreset string

const (
	PolicyPresetCorrectness PolicyPreset = "correctness"
	PolicyPresetBalanced    PolicyPreset = "balanced"
	PolicyPresetFast        PolicyPreset = "fast"
	PolicyPresetEconomical  PolicyPreset = "economical"
)

func (value PolicyPreset) IsValid() bool {
	switch value {
	case PolicyPresetCorrectness, PolicyPresetBalanced, PolicyPresetFast, PolicyPresetEconomical:
		return true
	default:
		return false
	}
}

// ReasoningEffort is a provider-neutral ordinal reasoning policy.
type ReasoningEffort string

const (
	ReasoningEffortMinimal  ReasoningEffort = "minimal"
	ReasoningEffortStandard ReasoningEffort = "standard"
	ReasoningEffortExtended ReasoningEffort = "extended"
	ReasoningEffortMaximum  ReasoningEffort = "maximum"
)

func (value ReasoningEffort) IsValid() bool {
	switch value {
	case ReasoningEffortMinimal,
		ReasoningEffortStandard,
		ReasoningEffortExtended,
		ReasoningEffortMaximum:
		return true
	default:
		return false
	}
}

// TokenCount is an exact non-negative token quantity.
type TokenCount uint64

// ModelCapabilityMetadata describes one version-bound provider model.
type ModelCapabilityMetadata struct {
	Provider         ProviderID `json:"provider"`
	Model            string     `json:"model"`
	Revision         string     `json:"revision"`
	ContextTokens    TokenCount `json:"context_tokens"`
	MaxOutputTokens  TokenCount `json:"max_output_tokens"`
	ToolCalling      bool       `json:"tool_calling"`
	StructuredOutput bool       `json:"structured_output"`
	Streaming        bool       `json:"streaming"`
	ImageInput       bool       `json:"image_input"`
	PromptCaching    bool       `json:"prompt_caching"`
	Reasoning        bool       `json:"reasoning"`
}

// Validate checks required and internally consistent model metadata.
func (value ModelCapabilityMetadata) Validate() error {
	switch {
	case value.Provider.IsZero():
		return valueError("model.provider", "must not be empty")
	case strings.TrimSpace(value.Model) == "":
		return valueError("model.model", "must not be empty")
	case strings.TrimSpace(value.Revision) == "":
		return valueError("model.revision", "must not be empty")
	case value.ContextTokens == 0:
		return valueError("model.context_tokens", "must be greater than zero")
	case value.MaxOutputTokens == 0:
		return valueError("model.max_output_tokens", "must be greater than zero")
	case value.MaxOutputTokens > value.ContextTokens:
		return valueError("model.max_output_tokens", "must not exceed context_tokens")
	default:
		return nil
	}
}

// CurrencyCode is a canonical uppercase ISO-style three-letter currency code.
type CurrencyCode string

// ParseCurrencyCode validates a canonical three-letter currency code.
func ParseCurrencyCode(raw string) (CurrencyCode, error) {
	if len(raw) != 3 {
		return "", valueError("currency", "must contain exactly three uppercase letters")
	}
	for _, character := range raw {
		if character < 'A' || character > 'Z' {
			return "", valueError("currency", "must contain exactly three uppercase letters")
		}
	}
	return CurrencyCode(raw), nil
}

// Money stores an exact signed count of currency minor units.
type Money struct {
	Currency   CurrencyCode `json:"currency"`
	MinorUnits int64        `json:"minor_units"`
}

// NewMoney creates an exact monetary value.
func NewMoney(currency CurrencyCode, minorUnits int64) (Money, error) {
	value := Money{Currency: currency, MinorUnits: minorUnits}
	if err := value.Validate(); err != nil {
		return Money{}, err
	}
	return value, nil
}

// Validate checks the monetary currency representation.
func (value Money) Validate() error {
	parsed, err := ParseCurrencyCode(string(value.Currency))
	if err != nil || parsed != value.Currency {
		return valueError("money.currency", "must be a canonical currency code")
	}
	return nil
}

// Add returns an exact sum and rejects currency mismatch or integer overflow.
func (value Money) Add(other Money) (Money, error) {
	if err := value.Validate(); err != nil {
		return Money{}, err
	}
	if err := other.Validate(); err != nil {
		return Money{}, err
	}
	if value.Currency != other.Currency {
		return Money{}, valueError("money.currency", "cannot add different currencies")
	}
	sum := value.MinorUnits + other.MinorUnits
	if other.MinorUnits > 0 && sum < value.MinorUnits ||
		other.MinorUnits < 0 && sum > value.MinorUnits {
		return Money{}, valueError("money.minor_units", "addition overflow")
	}
	return Money{Currency: value.Currency, MinorUnits: sum}, nil
}

// TokenUsage records exact provider token categories without collapsing unknowns.
type TokenUsage struct {
	Known            bool                  `json:"known"`
	Input            TokenCount            `json:"input"`
	CachedInput      TokenCount            `json:"cached_input"`
	CacheWrite       TokenCount            `json:"cache_write"`
	Output           TokenCount            `json:"output"`
	Reasoning        TokenCount            `json:"reasoning"`
	ProviderSpecific map[string]TokenCount `json:"provider_specific"`
}

// Validate rejects blank provider-specific category names.
func (value TokenUsage) Validate() error {
	if !value.Known {
		if value.Input != 0 ||
			value.CachedInput != 0 ||
			value.CacheWrite != 0 ||
			value.Output != 0 ||
			value.Reasoning != 0 ||
			len(value.ProviderSpecific) != 0 {
			return valueError("token_usage", "unknown usage must not carry numeric values")
		}
		return nil
	}
	for category := range value.ProviderSpecific {
		if strings.TrimSpace(category) == "" {
			return valueError("token_usage.provider_specific", "category names must not be blank")
		}
	}
	return nil
}

// Total returns the exact sum and whether usage is known.
func (value TokenUsage) Total() (TokenCount, bool, error) {
	if err := value.Validate(); err != nil {
		return 0, false, err
	}
	if !value.Known {
		return 0, false, nil
	}
	counts := []TokenCount{
		value.Input,
		value.CachedInput,
		value.CacheWrite,
		value.Output,
		value.Reasoning,
	}
	keys := make([]string, 0, len(value.ProviderSpecific))
	for key := range value.ProviderSpecific {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		counts = append(counts, value.ProviderSpecific[key])
	}
	var total TokenCount
	for _, count := range counts {
		next := total + count
		if next < total {
			return 0, false, valueError("token_usage", "total overflow")
		}
		total = next
	}
	return total, true, nil
}

// Milliseconds is an exact non-negative wall-clock duration.
type Milliseconds int64

// TaskBudget defines warning and hard-stop thresholds for one task.
type TaskBudget struct {
	ID                    BudgetID     `json:"id"`
	WarningCost           Money        `json:"warning_cost"`
	HardStopCost          Money        `json:"hard_stop_cost"`
	WarningTokens         TokenCount   `json:"warning_tokens"`
	HardStopTokens        TokenCount   `json:"hard_stop_tokens"`
	WarningWallClock      Milliseconds `json:"warning_wall_clock_ms"`
	HardStopWallClock     Milliseconds `json:"hard_stop_wall_clock_ms"`
	MaximumProviderCalls  uint32       `json:"maximum_provider_calls"`
	MaximumRepairRounds   uint32       `json:"maximum_repair_rounds"`
	MaximumToolExecutions uint32       `json:"maximum_tool_executions"`
}

// Validate checks monotonic warning and hard-stop thresholds.
func (value TaskBudget) Validate() error {
	switch {
	case value.ID.IsZero():
		return valueError("budget.id", "must not be empty")
	case value.WarningCost.Validate() != nil:
		return valueError("budget.warning_cost", "must contain valid exact money")
	case value.HardStopCost.Validate() != nil:
		return valueError("budget.hard_stop_cost", "must contain valid exact money")
	case value.WarningCost.Currency != value.HardStopCost.Currency:
		return valueError("budget.cost", "warning and hard stop must use the same currency")
	case value.WarningCost.MinorUnits < 0 || value.HardStopCost.MinorUnits < 0:
		return valueError("budget.cost", "thresholds must not be negative")
	case value.WarningCost.MinorUnits > value.HardStopCost.MinorUnits:
		return valueError("budget.cost", "warning must not exceed hard stop")
	case value.WarningTokens > value.HardStopTokens:
		return valueError("budget.tokens", "warning must not exceed hard stop")
	case value.WarningWallClock < 0 || value.HardStopWallClock < 0:
		return valueError("budget.wall_clock", "thresholds must not be negative")
	case value.WarningWallClock > value.HardStopWallClock:
		return valueError("budget.wall_clock", "warning must not exceed hard stop")
	default:
		return nil
	}
}

// ForecastRange defines correctness-neutral P50 and P90 task estimates.
type ForecastRange struct {
	LatencyKnown     bool         `json:"latency_known"`
	LatencyP50Millis Milliseconds `json:"latency_p50_ms"`
	LatencyP90Millis Milliseconds `json:"latency_p90_ms"`
	TokensKnown      bool         `json:"tokens_known"`
	TokensP50        TokenCount   `json:"tokens_p50"`
	TokensP90        TokenCount   `json:"tokens_p90"`
	CostKnown        bool         `json:"cost_known"`
	CostP50          Money        `json:"cost_p50"`
	CostP90          Money        `json:"cost_p90"`
}

// Validate checks percentile ordering and exact non-negative values.
func (value ForecastRange) Validate() error {
	switch {
	case !value.LatencyKnown && (value.LatencyP50Millis != 0 || value.LatencyP90Millis != 0):
		return valueError("forecast.latency", "unknown values must not carry estimates")
	case !value.TokensKnown && (value.TokensP50 != 0 || value.TokensP90 != 0):
		return valueError("forecast.tokens", "unknown values must not carry estimates")
	case !value.CostKnown && (value.CostP50 != (Money{}) || value.CostP90 != (Money{})):
		return valueError("forecast.cost", "unknown values must not carry estimates")
	case !value.LatencyKnown && !value.TokensKnown && !value.CostKnown:
		return nil
	case value.LatencyKnown && (value.LatencyP50Millis < 0 || value.LatencyP90Millis < 0):
		return valueError("forecast.latency", "values must not be negative")
	case value.LatencyKnown && value.LatencyP50Millis > value.LatencyP90Millis:
		return valueError("forecast.latency", "P50 must not exceed P90")
	case value.TokensKnown && value.TokensP50 > value.TokensP90:
		return valueError("forecast.tokens", "P50 must not exceed P90")
	case value.CostKnown && (value.CostP50.Validate() != nil || value.CostP90.Validate() != nil):
		return valueError("forecast.cost", "values must contain valid exact money")
	case value.CostKnown && value.CostP50.Currency != value.CostP90.Currency:
		return valueError("forecast.cost", "P50 and P90 must use the same currency")
	case value.CostKnown && (value.CostP50.MinorUnits < 0 || value.CostP90.MinorUnits < 0):
		return valueError("forecast.cost", "values must not be negative")
	case value.CostKnown && value.CostP50.MinorUnits > value.CostP90.MinorUnits:
		return valueError("forecast.cost", "P50 must not exceed P90")
	default:
		return nil
	}
}

// RiskLevel determines immutable validation and authority floors.
type RiskLevel string

const (
	RiskLevelRoutine   RiskLevel = "routine"
	RiskLevelElevated  RiskLevel = "elevated"
	RiskLevelProtected RiskLevel = "protected"
)

func (value RiskLevel) IsValid() bool {
	switch value {
	case RiskLevelRoutine, RiskLevelElevated, RiskLevelProtected:
		return true
	default:
		return false
	}
}

// AssuranceLevel describes the strength and validity of correctness evidence.
type AssuranceLevel string

const (
	AssuranceLevelFullyEvaluated  AssuranceLevel = "fully-evaluated"
	AssuranceLevelModelVerified   AssuranceLevel = "model-verified"
	AssuranceLevelContractChecked AssuranceLevel = "contract-checked"
	AssuranceLevelRuntimeOnly     AssuranceLevel = "runtime-only"
	AssuranceLevelInvalidated     AssuranceLevel = "invalidated"
)

func (value AssuranceLevel) IsValid() bool {
	switch value {
	case AssuranceLevelFullyEvaluated,
		AssuranceLevelModelVerified,
		AssuranceLevelContractChecked,
		AssuranceLevelRuntimeOnly,
		AssuranceLevelInvalidated:
		return true
	default:
		return false
	}
}

// ValidationSeverity is independent from diagnostic log severity.
type ValidationSeverity string

const (
	ValidationSeverityAdvisory ValidationSeverity = "advisory"
	ValidationSeverityWarning  ValidationSeverity = "warning"
	ValidationSeverityError    ValidationSeverity = "error"
	ValidationSeverityBlocking ValidationSeverity = "blocking"
)

func (value ValidationSeverity) IsValid() bool {
	switch value {
	case ValidationSeverityAdvisory,
		ValidationSeverityWarning,
		ValidationSeverityError,
		ValidationSeverityBlocking:
		return true
	default:
		return false
	}
}

// PauseReason explains why active task work stopped temporarily.
type PauseReason string

const (
	PauseReasonUserRequested       PauseReason = "user-requested"
	PauseReasonAwaitingAuthority   PauseReason = "awaiting-authority"
	PauseReasonBudgetExhausted     PauseReason = "budget-exhausted"
	PauseReasonProviderUnavailable PauseReason = "provider-unavailable"
	PauseReasonValidationFailed    PauseReason = "validation-failed"
	PauseReasonRecoveryUncertain   PauseReason = "recovery-uncertain"
)

func (value PauseReason) IsValid() bool {
	switch value {
	case PauseReasonUserRequested,
		PauseReasonAwaitingAuthority,
		PauseReasonBudgetExhausted,
		PauseReasonProviderUnavailable,
		PauseReasonValidationFailed,
		PauseReasonRecoveryUncertain:
		return true
	default:
		return false
	}
}

// FailureReason classifies an unsuccessful task or run.
type FailureReason string

const (
	FailureReasonPlanning   FailureReason = "planning"
	FailureReasonTool       FailureReason = "tool"
	FailureReasonProvider   FailureReason = "provider"
	FailureReasonValidation FailureReason = "validation"
	FailureReasonInternal   FailureReason = "internal"
	FailureReasonCorruption FailureReason = "corruption"
)

func (value FailureReason) IsValid() bool {
	switch value {
	case FailureReasonPlanning,
		FailureReasonTool,
		FailureReasonProvider,
		FailureReasonValidation,
		FailureReasonInternal,
		FailureReasonCorruption:
		return true
	default:
		return false
	}
}

// CancellationReason classifies an explicit stop decision.
type CancellationReason string

const (
	CancellationReasonUserRequested   CancellationReason = "user-requested"
	CancellationReasonSuperseded      CancellationReason = "superseded"
	CancellationReasonShutdown        CancellationReason = "shutdown"
	CancellationReasonBudgetExhausted CancellationReason = "budget-exhausted"
)

func (value CancellationReason) IsValid() bool {
	switch value {
	case CancellationReasonUserRequested,
		CancellationReasonSuperseded,
		CancellationReasonShutdown,
		CancellationReasonBudgetExhausted:
		return true
	default:
		return false
	}
}

// RollbackReason explains why an accepted or pending change was restored.
type RollbackReason string

const (
	RollbackReasonUserRequested         RollbackReason = "user-requested"
	RollbackReasonRegression            RollbackReason = "regression"
	RollbackReasonValidationInvalidated RollbackReason = "validation-invalidated"
	RollbackReasonRecovery              RollbackReason = "recovery"
)

func (value RollbackReason) IsValid() bool {
	switch value {
	case RollbackReasonUserRequested,
		RollbackReasonRegression,
		RollbackReasonValidationInvalidated,
		RollbackReasonRecovery:
		return true
	default:
		return false
	}
}

// InvalidationReason explains why prior evidence or a revision no longer applies.
type InvalidationReason string

const (
	InvalidationReasonDependencyChanged InvalidationReason = "dependency-changed"
	InvalidationReasonSourceChanged     InvalidationReason = "source-changed"
	InvalidationReasonPolicyChanged     InvalidationReason = "policy-changed"
	InvalidationReasonDefectAttributed  InvalidationReason = "defect-attributed"
	InvalidationReasonEvidenceExpired   InvalidationReason = "evidence-expired"
)

func (value InvalidationReason) IsValid() bool {
	switch value {
	case InvalidationReasonDependencyChanged,
		InvalidationReasonSourceChanged,
		InvalidationReasonPolicyChanged,
		InvalidationReasonDefectAttributed,
		InvalidationReasonEvidenceExpired:
		return true
	default:
		return false
	}
}

// ExecutionPolicy combines the fixed policy-bearing domain values for one task.
type ExecutionPolicy struct {
	Preset            PolicyPreset    `json:"preset"`
	Reasoning         ReasoningEffort `json:"reasoning"`
	Risk              RiskLevel       `json:"risk"`
	RequiredAssurance AssuranceLevel  `json:"required_assurance"`
}

// Validate checks every execution-policy enum.
func (value ExecutionPolicy) Validate() error {
	switch {
	case !value.Preset.IsValid():
		return valueError("policy.preset", "is not declared")
	case !value.Reasoning.IsValid():
		return valueError("policy.reasoning", "is not declared")
	case !value.Risk.IsValid():
		return valueError("policy.risk", "is not declared")
	case !value.RequiredAssurance.IsValid() || value.RequiredAssurance == AssuranceLevelInvalidated:
		return valueError("policy.required_assurance", "must be a valid non-invalidated level")
	default:
		return nil
	}
}

func valueError(field, reason string) error {
	return &ValueError{Field: field, Reason: reason}
}

// AmbiguityPolicy decides what a run does when the request it was given can be
// read more than one way.
//
// A request that is genuinely ambiguous has no right answer available to the
// machine, and the two honest responses are to ask or to state an assumption
// and proceed. Which one is right depends on the person and the moment — a
// supervised session wants the question, an overnight queue wants progress —
// so it is a declared posture rather than a decision buried in the planner.
//
// Guessing silently is not one of the options.
type AmbiguityPolicy string

const (
	// AmbiguityAsk stops before any work and puts the question to the person.
	// It is the default: unasked questions become wrong work, and wrong work
	// costs more to review than a question costs to answer.
	AmbiguityAsk AmbiguityPolicy = "ask"
	// AmbiguityAssume proceeds on the narrowest defensible reading and records
	// the assumption where the person will see it. It never hides that a choice
	// was made on their behalf.
	AmbiguityAssume AmbiguityPolicy = "assume"
)

func (value AmbiguityPolicy) IsValid() bool {
	switch value {
	case AmbiguityAsk, AmbiguityAssume:
		return true
	default:
		return false
	}
}
