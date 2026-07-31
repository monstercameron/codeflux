// Package taskcontrols renders observable task state, exact cost and budget
// facts, safe task commands, and recovery choices without owning transport.
package taskcontrols

import (
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/primitives"
)

type Phase string

const (
	PhasePlanning   Phase = "planning"
	PhaseEditing    Phase = "editing"
	PhaseValidating Phase = "validating"
	PhaseRepairing  Phase = "repairing"
	PhaseReviewing  Phase = "reviewing"
)

func (phase Phase) valid() bool {
	switch phase {
	case PhasePlanning, PhaseEditing, PhaseValidating, PhaseRepairing, PhaseReviewing:
		return true
	default:
		return false
	}
}

type DeliveryState string

const (
	DeliveryLive         DeliveryState = "live"
	DeliveryDisconnected DeliveryState = "disconnected"
	DeliveryDegraded     DeliveryState = "degraded"
)

func (state DeliveryState) valid() bool {
	switch state {
	case DeliveryLive, DeliveryDisconnected, DeliveryDegraded:
		return true
	default:
		return false
	}
}

type DeliveryView struct {
	State            DeliveryState
	SequenceCertain  bool
	TimelineReadable bool
	Explanation      string
}

type SelectionView struct {
	Provider string
	Model    string
	Effort   string
}

type ForecastView struct {
	Range       domain.ForecastRange
	Assumptions string
}

type ActualCostView struct {
	Known           bool
	Value           domain.Money
	PricingSnapshot string
	UnknownReason   string
}

type UsageView struct {
	Tokens domain.TokenUsage
	Cost   ActualCostView
}

type BudgetView struct {
	HardLimitKnown        bool
	HardLimit             domain.Money
	RemainingKnown        bool
	Remaining             domain.Money
	WarningThresholdKnown bool
	WarningThreshold      domain.Money
	WarningReached        bool
	WarningAcknowledged   bool
	HardCapReached        bool
	SettlingInFlightKnown bool
	SettlingInFlight      bool
	CheckpointKnown       bool
	CheckpointedState     string
}

type CommandState struct {
	Busy           bool
	IdempotencyKey string
	DisabledReason string
}

func (state CommandState) validate(name string) error {
	if state.Busy && strings.TrimSpace(state.IdempotencyKey) == "" {
		return fmt.Errorf("%s busy command requires an idempotency key", name)
	}
	return nil
}

type ControlState struct {
	Pause        CommandState
	Resume       CommandState
	Stop         CommandState
	AdjustBudget CommandState
}

type StopConfirmation struct {
	Required    bool
	Open        bool
	Consequence string
}

type BudgetAdjustment struct {
	Editing         bool
	Open            bool
	DraftMinorUnits string
	InvalidMessage  string
	OldLimit        domain.Money
	NewLimit        domain.Money
	Command         CommandState
}

type RecoveryDetailKind string

const (
	RecoveryDetailEvent RecoveryDetailKind = "event"
	RecoveryDetailFile  RecoveryDetailKind = "file"
)

type RecoveryDetail struct {
	Kind           RecoveryDetailKind
	Identity       string
	Label          string
	DisabledReason string
}

type RecoveryView struct {
	Required                 bool
	KnownState               string
	LastCheckpointAt         time.Time
	LastCheckpointPlanStep   string
	DivergenceSummary        string
	Ambiguity                string
	ExternalOutcomeAmbiguous bool
	SafestRecommendation     string
	SafeResumeVerified       bool
	ReconcileRequired        bool
	PatchPreservable         bool
	SafeResume               CommandState
	Reconcile                CommandState
	PreservePatch            CommandState
	Details                  []RecoveryDetail
}

type ReviewStaleness struct {
	Stale   bool
	Reasons []string
}

type Props struct {
	Mode                   primitives.Mode
	TaskID                 domain.TaskID
	TaskRevision           uint64
	BudgetRevision         uint64
	BudgetRevisionKnown    bool
	TaskSummary            string
	TaskState              domain.TaskState
	Phase                  Phase
	Delivery               DeliveryView
	Selection              SelectionView
	Forecast               ForecastView
	Usage                  UsageView
	Budget                 BudgetView
	Controls               ControlState
	StopConfirm            StopConfirmation
	BudgetAdjust           BudgetAdjustment
	Recovery               RecoveryView
	Review                 ReviewStaleness
	CommandNotice          string
	OnPause                func()
	OnResume               func()
	OnStop                 func()
	OnStopConfirm          func()
	OnStopCancel           func()
	OnBudgetAdjust         func()
	OnBudgetDraftChange    func(string)
	OnBudgetPreview        func()
	OnBudgetConfirm        func()
	OnBudgetCancel         func()
	OnBudgetWarningDismiss func()
	OnSafeResume           func()
	OnReconcile            func()
	OnPreservePatch        func()
	OnOpenDetail           func(RecoveryDetail)
}

func (props Props) Validate() error {
	if !props.TaskState.IsValid() {
		return fmt.Errorf("task state %q is not declared", props.TaskState)
	}
	if !props.Phase.valid() {
		return fmt.Errorf("phase %q is not declared", props.Phase)
	}
	if !props.Delivery.State.valid() {
		return fmt.Errorf("delivery state %q is not declared", props.Delivery.State)
	}
	if err := props.Forecast.Range.Validate(); err != nil {
		return fmt.Errorf("forecast: %w", err)
	}
	if err := props.Usage.Tokens.Validate(); err != nil {
		return fmt.Errorf("usage: %w", err)
	}
	if err := validateCost(props.Usage.Cost); err != nil {
		return err
	}
	if err := validateBudget(props.Budget); err != nil {
		return err
	}
	for name, command := range map[string]CommandState{
		"pause": props.Controls.Pause, "resume": props.Controls.Resume,
		"stop": props.Controls.Stop, "adjust budget": props.Controls.AdjustBudget,
		"budget confirmation": props.BudgetAdjust.Command,
		"safe resume":         props.Recovery.SafeResume, "reconcile": props.Recovery.Reconcile,
		"preserve patch": props.Recovery.PreservePatch,
	} {
		if err := command.validate(name); err != nil {
			return err
		}
	}
	if props.StopConfirm.Required && strings.TrimSpace(props.StopConfirm.Consequence) == "" {
		return fmt.Errorf("stop confirmation requires a consequence")
	}
	if props.BudgetAdjust.Open {
		if err := validateBudgetAdjustment(props.BudgetAdjust); err != nil {
			return err
		}
	}
	if props.Recovery.Required {
		if strings.TrimSpace(props.Recovery.KnownState) == "" ||
			strings.TrimSpace(props.Recovery.SafestRecommendation) == "" {
			return fmt.Errorf("recovery requires known state and safest recommendation")
		}
	}
	return nil
}

func validateCost(cost ActualCostView) error {
	if !cost.Known {
		if cost.Value != (domain.Money{}) {
			return fmt.Errorf("unknown actual cost must not carry a numeric value")
		}
		return nil
	}
	if err := cost.Value.Validate(); err != nil || cost.Value.MinorUnits < 0 {
		return fmt.Errorf("known actual cost must contain non-negative exact money")
	}
	if strings.TrimSpace(cost.PricingSnapshot) == "" {
		return fmt.Errorf("known actual cost requires its pricing snapshot")
	}
	return nil
}

func validateBudget(budget BudgetView) error {
	if !budget.HardLimitKnown {
		if budget.HardLimit != (domain.Money{}) || budget.RemainingKnown ||
			budget.WarningThresholdKnown || budget.WarningReached || budget.HardCapReached {
			return fmt.Errorf("unknown hard budget must not carry derived numeric or threshold state")
		}
		return nil
	}
	if err := budget.HardLimit.Validate(); err != nil || budget.HardLimit.MinorUnits < 0 {
		return fmt.Errorf("hard budget must contain non-negative exact money")
	}
	if budget.WarningThresholdKnown {
		if err := budget.WarningThreshold.Validate(); err != nil || budget.WarningThreshold.MinorUnits < 0 ||
			budget.WarningThreshold.Currency != budget.HardLimit.Currency ||
			budget.WarningThreshold.MinorUnits > budget.HardLimit.MinorUnits {
			return fmt.Errorf("budget warning threshold must use the hard-limit currency and not exceed it")
		}
	} else if budget.WarningThreshold != (domain.Money{}) || budget.WarningReached {
		return fmt.Errorf("unknown budget warning threshold must not carry numeric or reached state")
	}
	if budget.RemainingKnown {
		if err := budget.Remaining.Validate(); err != nil || budget.Remaining.MinorUnits < 0 ||
			budget.Remaining.Currency != budget.HardLimit.Currency {
			return fmt.Errorf("remaining budget must use the hard-limit currency and be non-negative")
		}
	} else if budget.Remaining != (domain.Money{}) {
		return fmt.Errorf("unknown remaining budget must not carry a numeric value")
	}
	return nil
}

func validateBudgetAdjustment(adjustment BudgetAdjustment) error {
	if err := adjustment.OldLimit.Validate(); err != nil || adjustment.OldLimit.MinorUnits < 0 {
		return fmt.Errorf("old budget must contain non-negative exact money")
	}
	if err := adjustment.NewLimit.Validate(); err != nil || adjustment.NewLimit.MinorUnits < 0 ||
		adjustment.OldLimit.Currency != adjustment.NewLimit.Currency {
		return fmt.Errorf("new budget must use the old budget currency and be non-negative")
	}
	return nil
}

func isPausable(state domain.TaskState) bool {
	return state == domain.TaskStateRunning || state == domain.TaskStateValidating
}

func isResumable(state domain.TaskState) bool { return state == domain.TaskStatePaused }

func isBudgetAdjustable(state domain.TaskState) bool {
	switch state {
	case domain.TaskStateDraft, domain.TaskStateReady, domain.TaskStatePaused:
		return true
	default:
		return false
	}
}

func isActive(state domain.TaskState) bool {
	switch state {
	case domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval,
		domain.TaskStateReady, domain.TaskStateRunning, domain.TaskStatePaused,
		domain.TaskStateAwaitingAuthority, domain.TaskStateValidating:
		return true
	default:
		return false
	}
}
