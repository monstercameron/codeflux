// Package timelinecard defines typed, renderer-neutral timeline card models.
package timelinecard

import (
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

type Kind string

const (
	KindMessage      Kind = "message"
	KindThreadState  Kind = "thread-state"
	KindRequirement  Kind = "requirement"
	KindForecast     Kind = "forecast"
	KindPlan         Kind = "plan"
	KindPlanRevision Kind = "plan-revision"
	KindContext      Kind = "context-selection"
	KindTool         Kind = "tool-activity"
	KindApproval     Kind = "approval"
	KindCheckpoint   Kind = "checkpoint"
	KindValidation   Kind = "validation"
	KindDiff         Kind = "diff-summary"
	KindCostBudget   Kind = "cost-budget"
	KindRecovery     Kind = "recovery"
	KindError        Kind = "error"
	KindCompletion   Kind = "completion"
	KindTaskState    Kind = "task-state"
	KindUsage        Kind = "usage"
	KindGraphChange  Kind = "graph-change"
	KindUnknown      Kind = "unknown"
)

type MessageStatus string

const (
	MessageProvisional MessageStatus = "provisional"
	MessageComplete    MessageStatus = "complete"
	MessageInterrupted MessageStatus = "interrupted"
)

type Message struct {
	ID                   string
	Role                 string
	Body                 string
	Status               MessageStatus
	NodeIDs              []string
	GraphRevision        string
	GraphRevisionCurrent bool
	OccurredAt           time.Time
}

type ThreadState struct {
	Action        string
	WorkspaceID   string
	PreviousTitle string
	Title         string
	Archived      bool
}

type Requirement struct {
	Goal                  string
	Constraints           []string
	Assumptions           []string
	UnresolvedAmbiguities []string
	ExplicitIdentities    []string
	RiskCues              []string
}

type Forecast struct {
	Range              domain.ForecastRange
	Model              string
	Effort             string
	ValidationProfile  string
	UncertaintyReasons []string
}

type PlanStepStatus string

const (
	PlanStepPending  PlanStepStatus = "pending"
	PlanStepActive   PlanStepStatus = "active"
	PlanStepComplete PlanStepStatus = "complete"
	PlanStepBlocked  PlanStepStatus = "blocked"
)

type PlanStep struct {
	Ordinal uint32
	Title   string
	Status  PlanStepStatus
}

type Plan struct {
	Revision           uint64
	Summary            string
	Scope              []string
	Steps              []PlanStep
	ExpectedFiles      []string
	PlannedChecks      []string
	Risks              []string
	Assumptions        []string
	AuthorityNeeds     []string
	CompletionCriteria []string
	Superseded         bool
	ApprovalPending    bool
	PriorRevisions     []uint64
}

type PlanRevision struct {
	PreviousRevision uint64
	CurrentRevision  uint64
	Summary          string
	Added            []string
	Removed          []string
	ApprovalReset    bool
	PriorHistory     []uint64
}

type ContextSelection struct {
	RepositoryRevision string
	Included           []ContextIdentity
	Exclusions         []string
	BudgetUsed         uint64
	BudgetLimit        uint64
}

type ContextIdentity struct {
	Identity string
	Reason   string
	Revision string
}

type ToolActivity struct {
	ExecutionID string
	Tool        string
	Purpose     string
	Scope       string
	State       string
	Duration    time.Duration
	ExitStatus  string
	Summary     string
	Output      RedactedOutput
}

type Approval struct {
	ID                string
	Action            string
	SafeArguments     string
	Scope             string
	Reason            string
	Consequences      string
	ExpiresAt         time.Time
	PreviousGrantUsed bool
	State             string
	ResolvedBy        string
	ResolvedAt        time.Time
}

type ApprovalAction string

const (
	ApprovalAllowOnce    ApprovalAction = "allow-once"
	ApprovalAllowForTask ApprovalAction = "allow-for-task"
	ApprovalDeny         ApprovalAction = "deny"
)

func (approval Approval) ActionsAvailable() bool {
	return approval.State == "pending"
}

func (approval Approval) AvailableActions() []ApprovalAction {
	if !approval.ActionsAvailable() {
		return nil
	}
	return []ApprovalAction{ApprovalAllowOnce, ApprovalAllowForTask, ApprovalDeny}
}

type Checkpoint struct {
	ID           string
	TaskRevision uint64
	PlanStep     string
	DiffSummary  string
	Reason       string
	RestoreSafe  bool
}

type ValidationStatus string

const (
	ValidationPending     ValidationStatus = "pending"
	ValidationRunning     ValidationStatus = "running"
	ValidationPassed      ValidationStatus = "passed"
	ValidationFailed      ValidationStatus = "failed"
	ValidationWaived      ValidationStatus = "waived"
	ValidationSkipped     ValidationStatus = "skipped"
	ValidationUnavailable ValidationStatus = "unavailable"
	ValidationCancelled   ValidationStatus = "cancelled"
	ValidationStale       ValidationStatus = "stale"
)

type Validation struct {
	ID              string
	Status          ValidationStatus
	Required        bool
	Summary         string
	RevisionBinding string
	Baseline        string
	Output          RedactedOutput
}

type DiffSummary struct {
	Files     []ChangedFile
	Additions uint64
	Deletions uint64
	Warnings  []string
	ReviewID  string
}

type ChangedFile struct {
	Path     string
	Category string
	Added    uint64
	Deleted  uint64
}

type CostBudget struct {
	Reason    string
	Known     bool
	Actual    domain.Money
	HardLimit domain.Money
	Reserved  domain.Money
	Threshold string
}

type Recovery struct {
	CheckpointID string
	Reason       string
	Known        []string
	Ambiguous    []string
	Choices      []RecoveryChoice
}

type RecoveryChoice struct {
	Kind        string
	Label       string
	Explanation string
	Destructive bool
}

type Error struct {
	Code           string
	Message        string
	AffectedAction string
	Retryable      bool
	NextSteps      []string
}

type CompletionStatus string

const (
	CompletionImplemented CompletionStatus = "implemented"
	CompletionValidated   CompletionStatus = "validated"
	CompletionReviewed    CompletionStatus = "reviewed"
	CompletionAccepted    CompletionStatus = "accepted"
	CompletionRejected    CompletionStatus = "rejected"
	CompletionRolledBack  CompletionStatus = "rolled-back"
)

type Completion struct {
	Status          CompletionStatus
	Files           []string
	Validation      []Validation
	Evidence        []string
	Cost            *domain.Money
	Assumptions     []string
	Limitations     []string
	MemoryInfluence []string
}

// TaskTransition is the presentation payload for a task-state event. The
// authoritative state type remains domain.TaskState.
type TaskTransition struct {
	From     string
	To       string
	Approval string
}

type Usage struct {
	Tokens domain.TokenUsage
}

type GraphChange struct {
	RevisionID string
	Patch      bool
	ByteCount  int
}

type Unknown struct {
	EventKind       string
	OccurredAt      time.Time
	Sequence        uint64
	SafeDetails     string
	DiagnosticsPath string
}

// Card is a typed one-of presentation model.
type Card struct {
	Kind         Kind
	Sequence     uint64
	StableKey    string
	OccurredAt   time.Time
	Message      *Message
	ThreadState  *ThreadState
	Requirement  *Requirement
	Forecast     *Forecast
	Plan         *Plan
	PlanRevision *PlanRevision
	Context      *ContextSelection
	Tool         *ToolActivity
	Approval     *Approval
	Checkpoint   *Checkpoint
	Validation   *Validation
	Diff         *DiffSummary
	CostBudget   *CostBudget
	Recovery     *Recovery
	Error        *Error
	Completion   *Completion
	TaskState    *TaskTransition
	Usage        *Usage
	GraphChange  *GraphChange
	Unknown      *Unknown
}

func (card Card) Validate() error {
	count := 0
	for _, present := range []bool{
		card.Message != nil, card.ThreadState != nil, card.Requirement != nil, card.Forecast != nil,
		card.Plan != nil, card.PlanRevision != nil, card.Context != nil,
		card.Tool != nil, card.Approval != nil, card.Checkpoint != nil,
		card.Validation != nil, card.Diff != nil, card.CostBudget != nil,
		card.Recovery != nil, card.Error != nil, card.Completion != nil,
		card.TaskState != nil, card.Usage != nil, card.GraphChange != nil,
		card.Unknown != nil,
	} {
		if present {
			count++
		}
	}
	if card.Kind == "" || card.StableKey == "" || count != 1 {
		return fmt.Errorf("timeline card must contain one typed model")
	}
	if modelKind := card.modelKind(); modelKind != card.Kind {
		return fmt.Errorf("timeline card kind %q contains %q model", card.Kind, modelKind)
	}
	return nil
}

func (card Card) modelKind() Kind {
	switch {
	case card.Message != nil:
		return KindMessage
	case card.ThreadState != nil:
		return KindThreadState
	case card.Requirement != nil:
		return KindRequirement
	case card.Forecast != nil:
		return KindForecast
	case card.Plan != nil:
		return KindPlan
	case card.PlanRevision != nil:
		return KindPlanRevision
	case card.Context != nil:
		return KindContext
	case card.Tool != nil:
		return KindTool
	case card.Approval != nil:
		return KindApproval
	case card.Checkpoint != nil:
		return KindCheckpoint
	case card.Validation != nil:
		return KindValidation
	case card.Diff != nil:
		return KindDiff
	case card.CostBudget != nil:
		return KindCostBudget
	case card.Recovery != nil:
		return KindRecovery
	case card.Error != nil:
		return KindError
	case card.Completion != nil:
		return KindCompletion
	case card.TaskState != nil:
		return KindTaskState
	case card.Usage != nil:
		return KindUsage
	case card.GraphChange != nil:
		return KindGraphChange
	case card.Unknown != nil:
		return KindUnknown
	default:
		return ""
	}
}

func KnownKinds() []Kind {
	return []Kind{
		KindMessage, KindThreadState, KindRequirement, KindForecast, KindPlan, KindPlanRevision,
		KindContext, KindTool, KindApproval, KindCheckpoint, KindValidation,
		KindDiff, KindCostBudget, KindRecovery, KindError, KindCompletion,
		KindTaskState, KindUsage, KindGraphChange, KindUnknown,
	}
}

// EventKind is retained on descriptors and fallbacks without accepting raw
// executable presentation content.
type EventKind = events.Kind
