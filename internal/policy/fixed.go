package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

const (
	// FixedBaselineVersion changes whenever any selection or limit changes.
	FixedBaselineVersion = "fixed-baseline-v2"

	FixedBaselineAdapter         = "openai-responses"
	FixedBaselineAdapterVersion  = "1"
	FixedBaselineProvider        = "openai"
	FixedBaselineProviderVersion = "responses-v1"
	FixedBaselineModel           = "gpt-5.6-sol"

	defaultWarningCostMinorUnits int64 = 3_750
	defaultHardCostMinorUnits    int64 = 5_000
)

var (
	// ErrInvalidPolicy classifies malformed baseline inputs or snapshots.
	ErrInvalidPolicy = errors.New("invalid fixed policy")
	// ErrBudgetAdjustmentTooLate prevents an unapproved budget mutation after
	// the pre-execution approval boundary.
	ErrBudgetAdjustmentTooLate = errors.New("budget adjustment is only allowed before approval")
)

// Phase is one fixed-policy agent phase.
type Phase string

const (
	PhasePlanning  Phase = "planning"
	PhaseExecution Phase = "execution"
	PhaseRepair    Phase = "repair"
	PhaseReview    Phase = "review"
)

// PhaseBehavior makes the baseline loop limits and authority visible.
type PhaseBehavior struct {
	Phase                      Phase                  `json:"phase"`
	Reasoning                  domain.ReasoningEffort `json:"reasoning"`
	MaximumRounds              uint32                 `json:"maximum_rounds"`
	MaximumToolCallsPerRound   uint32                 `json:"maximum_tool_calls_per_round"`
	MayMutateWorkspace         bool                   `json:"may_mutate_workspace"`
	RequiresValidationEvidence bool                   `json:"requires_validation_evidence"`
}

// Limits are fixed prototype ceilings, independent of forecast estimates.
type Limits struct {
	MaximumPlanningRounds    uint32            `json:"maximum_planning_rounds"`
	MaximumExecutionRounds   uint32            `json:"maximum_execution_rounds"`
	MaximumRepairRounds      uint32            `json:"maximum_repair_rounds"`
	MaximumReviewRounds      uint32            `json:"maximum_review_rounds"`
	MaximumToolCallsPerRound uint32            `json:"maximum_tool_calls_per_round"`
	MaximumContextTokens     domain.TokenCount `json:"maximum_context_tokens"`
}

// BudgetDefaults are copied into a task-owned budget with its stable ID.
type BudgetDefaults struct {
	WarningCost           domain.Money        `json:"warning_cost"`
	HardStopCost          domain.Money        `json:"hard_stop_cost"`
	WarningTokens         domain.TokenCount   `json:"warning_tokens"`
	HardStopTokens        domain.TokenCount   `json:"hard_stop_tokens"`
	WarningWallClock      domain.Milliseconds `json:"warning_wall_clock_ms"`
	HardStopWallClock     domain.Milliseconds `json:"hard_stop_wall_clock_ms"`
	MaximumProviderCalls  uint32              `json:"maximum_provider_calls"`
	MaximumRepairRounds   uint32              `json:"maximum_repair_rounds"`
	MaximumToolExecutions uint32              `json:"maximum_tool_executions"`
}

// Materialize assigns a task-owned stable identity without changing defaults.
func (defaults BudgetDefaults) Materialize(id domain.BudgetID) (domain.TaskBudget, error) {
	budget := domain.TaskBudget{
		ID:                    id,
		WarningCost:           defaults.WarningCost,
		HardStopCost:          defaults.HardStopCost,
		WarningTokens:         defaults.WarningTokens,
		HardStopTokens:        defaults.HardStopTokens,
		WarningWallClock:      defaults.WarningWallClock,
		HardStopWallClock:     defaults.HardStopWallClock,
		MaximumProviderCalls:  defaults.MaximumProviderCalls,
		MaximumRepairRounds:   defaults.MaximumRepairRounds,
		MaximumToolExecutions: defaults.MaximumToolExecutions,
	}
	if err := budget.Validate(); err != nil {
		return domain.TaskBudget{}, fmt.Errorf("%w: materialize budget: %v", ErrInvalidPolicy, err)
	}
	return budget, nil
}

// SelectionSource explains why the selected model differs from the baseline.
type SelectionSource string

const (
	SelectionSourceFixedBaseline  SelectionSource = "fixed-baseline"
	SelectionSourceManualOverride SelectionSource = "manual-override"
)

// ManualOverride is an explicit, attributable user choice. Every field is
// selection input so replay never depends on ambient state or a clock.
type ManualOverride struct {
	Model              providers.ModelIdentity `json:"model"`
	Reasoning          domain.ReasoningEffort  `json:"reasoning"`
	Actor              string                  `json:"actor"`
	AuthorityReference string                  `json:"authority_reference"`
	Reason             string                  `json:"reason"`
}

// SelectionInput contains all policy selection inputs.
type SelectionInput struct {
	// BaselineModelRevision is the exact provider-returned revision or
	// fingerprint. A changed value creates a distinct policy digest and
	// measurement stratum without changing the frozen provider, model, or
	// reasoning effort.
	BaselineModelRevision string          `json:"baseline_model_revision"`
	Override              *ManualOverride `json:"override,omitempty"`
}

// Snapshot is the exact inspectable policy persisted with each task and run.
type Snapshot struct {
	Version        string                  `json:"version"`
	Source         SelectionSource         `json:"source"`
	Model          providers.ModelIdentity `json:"model"`
	Reasoning      domain.ReasoningEffort  `json:"reasoning"`
	Limits         Limits                  `json:"limits"`
	BudgetDefaults BudgetDefaults          `json:"budget_defaults"`
	Phases         []PhaseBehavior         `json:"phases"`
	ManualOverride *ManualOverride         `json:"manual_override,omitempty"`
}

// Select returns the frozen baseline or an explicitly recorded manual choice.
// It performs no I/O and is deterministic for identical inputs.
func Select(input SelectionInput) (Snapshot, error) {
	baselineModel := fixedBaselineModel(input.BaselineModelRevision)
	if err := validateModel(baselineModel); err != nil {
		return Snapshot{}, err
	}
	limits := fixedLimits()
	snapshot := Snapshot{
		Version:        FixedBaselineVersion,
		Source:         SelectionSourceFixedBaseline,
		Model:          baselineModel,
		Reasoning:      domain.ReasoningEffortMaximum,
		Limits:         limits,
		BudgetDefaults: fixedBudgetDefaults(),
		Phases:         fixedPhases(limits),
	}
	if input.Override != nil {
		override := *input.Override
		if err := validateOverride(override); err != nil {
			return Snapshot{}, err
		}
		snapshot.Source = SelectionSourceManualOverride
		snapshot.Model = override.Model
		snapshot.Reasoning = override.Reasoning
		snapshot.ManualOverride = &override
		for index := range snapshot.Phases {
			snapshot.Phases[index].Reasoning = override.Reasoning
		}
	}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// Validate checks that a snapshot is a valid output of this policy version.
func (snapshot Snapshot) Validate() error {
	if snapshot.Version != FixedBaselineVersion {
		return fmt.Errorf("%w: unsupported version %q", ErrInvalidPolicy, snapshot.Version)
	}
	if err := validateModel(snapshot.Model); err != nil {
		return err
	}
	if !snapshot.Reasoning.IsValid() {
		return fmt.Errorf("%w: invalid reasoning effort", ErrInvalidPolicy)
	}
	expectedLimits := fixedLimits()
	if snapshot.Limits != expectedLimits {
		return fmt.Errorf("%w: fixed limits differ from %s", ErrInvalidPolicy, FixedBaselineVersion)
	}
	expectedDefaults := fixedBudgetDefaults()
	if !reflect.DeepEqual(snapshot.BudgetDefaults, expectedDefaults) {
		return fmt.Errorf("%w: fixed budget defaults differ from %s", ErrInvalidPolicy, FixedBaselineVersion)
	}
	expectedPhases := fixedPhases(expectedLimits)
	for index := range expectedPhases {
		expectedPhases[index].Reasoning = snapshot.Reasoning
	}
	if !reflect.DeepEqual(snapshot.Phases, expectedPhases) {
		return fmt.Errorf("%w: phase behavior differs from %s", ErrInvalidPolicy, FixedBaselineVersion)
	}
	switch snapshot.Source {
	case SelectionSourceFixedBaseline:
		if snapshot.ManualOverride != nil ||
			snapshot.Reasoning != domain.ReasoningEffortMaximum ||
			!sameFixedBaselineModel(snapshot.Model) {
			return fmt.Errorf("%w: fixed selection was altered", ErrInvalidPolicy)
		}
	case SelectionSourceManualOverride:
		if snapshot.ManualOverride == nil {
			return fmt.Errorf("%w: manual selection lacks override evidence", ErrInvalidPolicy)
		}
		if err := validateOverride(*snapshot.ManualOverride); err != nil {
			return err
		}
		if !reflect.DeepEqual(snapshot.Model, snapshot.ManualOverride.Model) ||
			snapshot.Reasoning != snapshot.ManualOverride.Reasoning {
			return fmt.Errorf("%w: manual selection differs from override evidence", ErrInvalidPolicy)
		}
	default:
		return fmt.Errorf("%w: unknown selection source", ErrInvalidPolicy)
	}
	return nil
}

// CanonicalJSON returns the stable persistence representation of a snapshot.
func (snapshot Snapshot) CanonicalJSON() ([]byte, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("%w: encode snapshot: %v", ErrInvalidPolicy, err)
	}
	return encoded, nil
}

// Digest returns a lowercase SHA-256 binding for the exact policy snapshot.
func (snapshot Snapshot) Digest() (string, error) {
	encoded, err := snapshot.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func fixedLimits() Limits {
	return Limits{
		MaximumPlanningRounds:    2,
		MaximumExecutionRounds:   8,
		MaximumRepairRounds:      3,
		MaximumReviewRounds:      1,
		MaximumToolCallsPerRound: 24,
		MaximumContextTokens:     128_000,
	}
}

func fixedBaselineModel(revision string) providers.ModelIdentity {
	return providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter:         FixedBaselineAdapter,
			AdapterVersion:  FixedBaselineAdapterVersion,
			Provider:        FixedBaselineProvider,
			ProviderVersion: FixedBaselineProviderVersion,
		},
		Model:    FixedBaselineModel,
		Revision: revision,
	}
}

func sameFixedBaselineModel(model providers.ModelIdentity) bool {
	return model.Provider.Adapter == FixedBaselineAdapter &&
		model.Provider.AdapterVersion == FixedBaselineAdapterVersion &&
		model.Provider.Provider == FixedBaselineProvider &&
		model.Provider.ProviderVersion == FixedBaselineProviderVersion &&
		model.Model == FixedBaselineModel
}

func fixedBudgetDefaults() BudgetDefaults {
	currency, _ := domain.ParseCurrencyCode("USD")
	warning, _ := domain.NewMoney(currency, defaultWarningCostMinorUnits)
	hard, _ := domain.NewMoney(currency, defaultHardCostMinorUnits)
	return BudgetDefaults{
		WarningCost:           warning,
		HardStopCost:          hard,
		WarningTokens:         750_000,
		HardStopTokens:        1_000_000,
		WarningWallClock:      5_400_000,
		HardStopWallClock:     7_200_000,
		MaximumProviderCalls:  16,
		MaximumRepairRounds:   3,
		MaximumToolExecutions: 192,
	}
}

func fixedPhases(limits Limits) []PhaseBehavior {
	return []PhaseBehavior{
		{
			Phase: PhasePlanning, Reasoning: domain.ReasoningEffortMaximum,
			MaximumRounds:            limits.MaximumPlanningRounds,
			MaximumToolCallsPerRound: limits.MaximumToolCallsPerRound,
		},
		{
			Phase: PhaseExecution, Reasoning: domain.ReasoningEffortMaximum,
			MaximumRounds:            limits.MaximumExecutionRounds,
			MaximumToolCallsPerRound: limits.MaximumToolCallsPerRound,
			MayMutateWorkspace:       true,
		},
		{
			Phase: PhaseRepair, Reasoning: domain.ReasoningEffortMaximum,
			MaximumRounds:            limits.MaximumRepairRounds,
			MaximumToolCallsPerRound: limits.MaximumToolCallsPerRound,
			MayMutateWorkspace:       true, RequiresValidationEvidence: true,
		},
		{
			Phase: PhaseReview, Reasoning: domain.ReasoningEffortMaximum,
			MaximumRounds:              limits.MaximumReviewRounds,
			MaximumToolCallsPerRound:   limits.MaximumToolCallsPerRound,
			RequiresValidationEvidence: true,
		},
	}
}

func validateOverride(override ManualOverride) error {
	if err := validateModel(override.Model); err != nil {
		return err
	}
	if !override.Reasoning.IsValid() {
		return fmt.Errorf("%w: override reasoning is invalid", ErrInvalidPolicy)
	}
	fields := []struct {
		name  string
		value string
	}{
		{name: "actor", value: override.Actor},
		{name: "authority reference", value: override.AuthorityReference},
		{name: "reason", value: override.Reason},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) != field.value ||
			field.value == "" ||
			len(field.value) > 512 {
			return fmt.Errorf("%w: override %s must be non-empty, trimmed, and bounded", ErrInvalidPolicy, field.name)
		}
	}
	return nil
}

func validateModel(model providers.ModelIdentity) error {
	fields := []struct {
		name  string
		value string
	}{
		{name: "adapter", value: model.Provider.Adapter},
		{name: "adapter version", value: model.Provider.AdapterVersion},
		{name: "provider", value: model.Provider.Provider},
		{name: "provider version", value: model.Provider.ProviderVersion},
		{name: "model", value: model.Model},
		{name: "model revision", value: model.Revision},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) != field.value ||
			field.value == "" ||
			len(field.value) > 255 {
			return fmt.Errorf("%w: %s must be non-empty, trimmed, and bounded", ErrInvalidPolicy, field.name)
		}
	}
	return nil
}
