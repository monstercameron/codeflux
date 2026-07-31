package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/providers"
)

// ErrActionBlocked reports the race where durable control closed the action
// gate after the loop's between-action check but before registration.
var ErrActionBlocked = errors.New("durable control blocks new actions")

// StepKind is the closed semantic class of one immutable plan step. Each kind
// has exactly one completion tool and fixed materiality/validation semantics.
type StepKind string

const (
	StepKindEdit           StepKind = "edit"
	StepKindReadFile       StepKind = "read-file"
	StepKindListDirectory  StepKind = "list-directory"
	StepKindSearchText     StepKind = "search-text"
	StepKindSearchSymbol   StepKind = "search-symbol"
	StepKindInspectDiff    StepKind = "inspect-diff"
	StepKindGitStatus      StepKind = "git-status"
	StepKindGitHistory     StepKind = "git-history"
	StepKindTest           StepKind = "test"
	StepKindBuild          StepKind = "build"
	StepKindStaticAnalysis StepKind = "static-analysis"
)

// StepState is the execution projection for one immutable plan step.
type StepState string

const (
	StepPending     StepState = "pending"
	StepInProgress  StepState = "in-progress"
	StepImplemented StepState = "implemented"
	StepValidated   StepState = "validated"
	StepFailed      StepState = "failed"
	StepSkipped     StepState = "skipped"
)

// PlanStep is the bounded step state exposed to the fixed model.
type PlanStep struct {
	ID                 string
	Kind               StepKind
	SummaryRedacted    string
	State              StepState
	MaterialEdit       bool
	ValidationRequired bool
	// ExpectedFiles is the immutable, canonical repository-relative scope for
	// path-scoped completion tools.
	ExpectedFiles []string
	// CompletionTools is the immutable plan-authorized set of tool kinds whose
	// successful execution may move this step to implemented.
	CompletionTools []executor.ToolName
}

// PlanProjection is the exact current approved plan revision.
type PlanProjection struct {
	Revision           uint64
	RepositoryRevision string
	Steps              []PlanStep
}

// RepositoryContextItem is selected, revision-bound repository context. It is
// not an arbitrary transcript replay.
type RepositoryContextItem struct {
	Path            string
	ContentSHA256   string
	ContentRedacted string
}

// FactualEvent is a bounded immutable fact selected for the current turn.
type FactualEvent struct {
	Sequence        uint64
	Type            string
	SummaryRedacted string
}

// ToolArgumentDefinition defines the only accepted fields for a tool call.
// Tool arguments are strings because executor.ToolRequest preserves ordered
// string values and redaction metadata.
type ToolArgumentDefinition struct {
	Name      string
	Required  bool
	Sensitive bool
	MaxBytes  int
}

// ApprovedTool is one exact tool schema granted to this run.
type ApprovedTool struct {
	Descriptor        executor.ToolDescriptor
	Arguments         []ToolArgumentDefinition
	DefaultTimeout    time.Duration
	MaterialEdit      bool
	CreatesCheckpoint bool
}

// ModelInput is the complete bounded observation supplied for one round.
type ModelInput struct {
	TaskID            domain.TaskID
	RunID             domain.RunID
	Model             providers.ModelIdentity
	Round             uint32
	RepositoryContext []RepositoryContextItem
	Plan              PlanProjection
	FactualEvents     []FactualEvent
	ApprovedTools     []providers.ToolDeclaration
	PreviousResults   []ToolFeedback
}

// CompletionSignal separates source implementation from validation evidence.
type CompletionSignal string

const (
	CompletionNone                   CompletionSignal = ""
	CompletionImplementationComplete CompletionSignal = "implementation-complete"
	CompletionValidationComplete     CompletionSignal = "validation-complete"
	CompletionNeedsDirection         CompletionSignal = "needs-direction"
)

// ModelToolCall binds a normalized provider call to an existing plan step.
type ModelToolCall struct {
	Call       providers.ToolCall
	PlanStepID string
	// CompletesStep is retained for wire compatibility only. The execution
	// loop deliberately ignores this untrusted model claim: a successful,
	// step-compatible tool determines implementation state.
	CompletesStep bool
}

// ModelTurn is one attributable fixed-model decision.
type ModelTurn struct {
	ModelRequestID domain.ModelRequestID
	Model          providers.ModelIdentity
	ToolCalls      []ModelToolCall
	Completion     CompletionSignal
	Usage          providers.Usage
	Cost           providers.ExactAmount
}

// FixedModel is the already-budgeted fixed-provider turn boundary. Provider
// reservation, request intent, streaming, and usage settlement remain owned by
// the provider coordinator beneath this port.
type FixedModel interface {
	Identity() providers.ModelIdentity
	ObserveThink(context.Context, ModelInput) (ModelTurn, error)
}

// ToolAuthorization is the exact policy decision for a proposed action.
type ToolAuthorization struct {
	Classification executor.AuthorityClassification
	DecisionID     string
	PolicyRevision uint64
	PolicySHA256   string
}

// AuthorityRouter persists or resolves authority outside the model. Approval
// and denial are returned as policy outcomes; the loop never upgrades them.
type AuthorityRouter interface {
	RouteTool(context.Context, executor.ToolRequest) (ToolAuthorization, error)
}

// ToolExecutor runs only a request carrying executable authority.
type ToolExecutor interface {
	ExecuteTool(
		context.Context,
		executor.AuthorizedToolRequest,
	) (executor.ToolResult, error)
}

// ToolStartRecord is persisted before any tool effect begins.
type ToolStartRecord struct {
	TaskID                domain.TaskID
	RunID                 domain.RunID
	PlanRevision          uint64
	PlanStepID            string
	Round                 uint32
	RequestID             string
	ModelRequestID        domain.ModelRequestID
	ToolName              executor.ToolName
	ToolSchemaVersion     int
	ArgumentsRedactedJSON string
	ArgumentsSHA256       string
	Authorization         ToolAuthorization
}

// ToolResultRecord is persisted after execution and contains only bounded,
// already-redacted output.
type ToolResultRecord struct {
	TaskID       domain.TaskID
	RunID        domain.RunID
	PlanRevision uint64
	PlanStepID   string
	Round        uint32
	RequestID    string
	Result       executor.ToolResult
	ResultSHA256 string
}

// ToolJournal owns durable start/result ordering and idempotency.
type ToolJournal interface {
	PersistToolStart(context.Context, ToolStartRecord) error
	PersistToolResult(context.Context, ToolResultRecord) error
}

// PlanStepTransition is one attributable plan projection update.
type PlanStepTransition struct {
	TaskID         domain.TaskID
	RunID          domain.RunID
	PlanRevision   uint64
	PlanStepID     string
	From           StepState
	To             StepState
	Reason         string
	ToolRequestID  string
	ModelRequestID domain.ModelRequestID
}

// PlanStepStore persists step changes before the next model round.
type PlanStepStore interface {
	PersistPlanStepTransition(context.Context, PlanStepTransition) error
}

// CheckpointTrigger identifies the correctness boundary that requested a
// recovery point.
type CheckpointTrigger string

const (
	CheckpointMaterialEdit CheckpointTrigger = "material-edit-applied"
	CheckpointBeforeRisky  CheckpointTrigger = "before-risky-action"
)

// CheckpointRequest binds a recovery point to plan, action, and authority.
type CheckpointRequest struct {
	TaskID         domain.TaskID
	RunID          domain.RunID
	PlanRevision   uint64
	PlanStepID     string
	ToolRequestID  string
	ModelRequestID domain.ModelRequestID
	Round          uint32
	Trigger        CheckpointTrigger
	PermissionID   string
	ActionSHA256   string
	Reason         string
}

// CheckpointStore persists recovery points after material edit batches.
type CheckpointStore interface {
	CreateCheckpoint(context.Context, CheckpointRequest) error
}

// PlanApprovedCheckpointRequest binds the exact approved plan authority to
// the recovery point that must exist before model or tool work starts.
type PlanApprovedCheckpointRequest struct {
	TaskID       domain.TaskID
	RunID        domain.RunID
	PlanRevision uint64
	ApprovalID   domain.ApprovalID
}

// PlanApprovedCheckpointStore owns the mandatory execution-start checkpoint.
type PlanApprovedCheckpointStore interface {
	CreatePlanApprovedCheckpoint(
		context.Context,
		PlanApprovedCheckpointRequest,
	) error
}

// ControlDisposition is the externally authoritative run control state.
type ControlDisposition string

const (
	ControlActive         ControlDisposition = "active"
	ControlPauseRequested ControlDisposition = "pause-requested"
	ControlPaused         ControlDisposition = "paused"
	ControlCancelled      ControlDisposition = "cancelled"
	ControlStopped        ControlDisposition = "stopped"
)

// ControlState combines the latest pause/cancel, budget, policy, and
// validation facts checked between actions.
type ControlState struct {
	Disposition        ControlDisposition
	BudgetAvailable    bool
	PolicyCurrent      bool
	ValidationComplete bool
}

// ControlReader returns current durable control facts.
type ControlReader interface {
	ReadControl(context.Context, domain.TaskID, domain.RunID) (ControlState, error)
}

// ActionKind identifies the in-flight operation governed by pause/cancel
// policy.
type ActionKind string

const (
	ActionModel      ActionKind = "model"
	ActionTool       ActionKind = "tool"
	ActionValidation ActionKind = "validation"
)

// ActionDescriptor is registered before an operation starts. Only explicitly
// safe mediated reads may finish after a pause request.
type ActionDescriptor struct {
	Kind        ActionKind
	RequestID   string
	SafeRead    bool
	LongRunning bool
}

// ControlInterruptBridge binds durable pause/cancel/stop changes to an active
// model or tool context. Implementations typically subscribe to committed
// control events and cancel the returned context when work must stop.
type ControlInterruptBridge interface {
	BindActionContext(
		context.Context,
		domain.TaskID,
		domain.RunID,
		ActionDescriptor,
	) (context.Context, context.CancelFunc, error)
}

// ToolFeedback is the bounded result returned to the model on the next round.
type ToolFeedback struct {
	CallID          string
	Tool            string
	State           string
	ExitCode        int
	SummaryRedacted string
	StdoutRedacted  string
	StderrRedacted  string
	Truncated       bool
	IsError         bool
}

// LoopLimits are immutable run ceilings. Provider and durable task budgets may
// be stricter; these limits never grant additional authority.
type LoopLimits struct {
	MaximumRounds            uint32
	MaximumToolCalls         uint32
	MaximumToolCallsPerRound uint32
	MaximumTokens            domain.TokenCount
	MaximumTokensPerRound    domain.TokenCount
	MaximumWallClock         time.Duration
	MaximumCost              providers.ExactAmount
	MaximumIdenticalFailures uint32
	MaximumContextItems      int
	MaximumFactualEvents     int
	MaximumContextBytes      int
	MaximumResultBytes       int
}

// LoopInput binds one run to its current plan, selected context, facts, tools,
// and fixed worktree scope.
type LoopInput struct {
	TaskID            domain.TaskID
	RunID             domain.RunID
	PlanApprovalID    domain.ApprovalID
	WorktreePath      string
	PolicyRevision    uint64
	PolicySHA256      string
	Plan              PlanProjection
	RepositoryContext []RepositoryContextItem
	FactualEvents     []FactualEvent
	ApprovedTools     []ApprovedTool
	Limits            LoopLimits
}

// OutcomeKind is the honest terminal or paused result of this loop invocation.
type OutcomeKind string

const (
	OutcomeImplementationComplete OutcomeKind = "implementation-complete"
	OutcomeValidationComplete     OutcomeKind = "validation-complete"
	OutcomeAwaitingDirection      OutcomeKind = "awaiting-direction"
	OutcomeAwaitingApproval       OutcomeKind = "awaiting-approval"
	OutcomePermissionDenied       OutcomeKind = "permission-denied"
	OutcomePaused                 OutcomeKind = "paused"
	OutcomeCancelled              OutcomeKind = "cancelled"
	OutcomeStopped                OutcomeKind = "stopped"
	OutcomeBudgetExhausted        OutcomeKind = "budget-exhausted"
	OutcomePolicyBlocked          OutcomeKind = "policy-blocked"
	OutcomeLimitReached           OutcomeKind = "limit-reached"
)

// LoopOutcome is factual execution state; it is not correctness evidence.
type LoopOutcome struct {
	Kind               OutcomeKind
	Reason             string
	Rounds             uint32
	ToolCalls          uint32
	Tokens             domain.TokenCount
	Cost               providers.ExactAmount
	Plan               PlanProjection
	ValidationRequired bool
}

// LoopDependencies are explicit ports to fixed-provider, authority, durable
// journal, tool, checkpoint, plan, and control boundaries.
type LoopDependencies struct {
	Model                   FixedModel
	Authority               AuthorityRouter
	Tools                   ToolExecutor
	Journal                 ToolJournal
	PlanSteps               PlanStepStore
	Checkpoints             CheckpointStore
	PlanApprovalCheckpoints PlanApprovedCheckpointStore
	Control                 ControlReader
	Interrupts              ControlInterruptBridge
	Now                     func() time.Time
}

// ApprovedToolSchema returns the strict JSON schema sent to the model.
func ApprovedToolSchema(tool ApprovedTool) (json.RawMessage, error) {
	return approvedToolSchema(tool)
}
