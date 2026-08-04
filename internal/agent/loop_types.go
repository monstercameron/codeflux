package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	// StepKindEdit writes a file whole. It is how a file comes into existence.
	StepKindEdit StepKind = "edit"
	// StepKindPatch changes part of a file that already exists.
	//
	// A separate kind rather than a second tool on the edit kind. Every step
	// kind names exactly one completion tool, and that is what makes the
	// mapping usable in four places at once — plan validation, the round's tool
	// allowlist, completion, and scope enforcement. Broadening one kind to two
	// tools broke the first of those and silently disabled the second: the
	// validator refused every plan, and no attempt ever reached a prompt.
	StepKindPatch          StepKind = "patch"
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
	// StructuredOutput pins the answer to a schema, for the calls whose result
	// is data rather than work.
	//
	// A model asked in prose for "one behaviour per line and nothing else"
	// returns numbered lines, bullets and headings often enough that the parser
	// reading it back has a case for each, and the cases it does not have are
	// silent: a paragraph counted as one behaviour produced a plan claiming the
	// work was a single unit when nothing established that. A schema removes
	// the guessing rather than adding another case to it.
	//
	// Nil for the ordinary working turns, which answer with tool calls. Those
	// are already structured -- the arguments arrive as JSON against the tool's
	// own schema -- and constraining the whole answer would leave the model no
	// way to call anything.
	StructuredOutput *providers.StructuredOutputRequirement
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
	// MessageRedacted is what the model said in words this turn.
	//
	// The loop itself acts on tool calls and the completion signal, not on
	// prose, and it is right to: a run driven by what a model claims rather
	// than by what it did is not supervised. But the text was being collected,
	// scanned for a completion keyword, and then discarded, so a caller that
	// asked the model a question — rather than asking it to work — had no way
	// to read the answer. Carrying it costs nothing and makes a direct
	// question answerable through the same port.
	MessageRedacted string
	Completion      CompletionSignal
	Usage           providers.Usage
	Cost            providers.ExactAmount
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

// maximumUnverifiedCompletionRefusals bounds how many times one attempt may be
// sent back for ending on work it has not checked.
//
// Bounded because the guard must not become the thing that spends the attempt.
// Not bounded to one, which was the first version and left the hole it was
// meant to close: the run claimed completion, was refused, tested, saw the
// failure, patched, and claimed completion again with the refusal already
// spent. Three is enough for test-fix-test and short of a round budget of
// sixteen.
const maximumUnverifiedCompletionRefusals = 3

// toolCanStillBeCalled reports whether an open plan step can accept this tool.
//
// The approved list is not the answer: a tool is offered to the model only
// while some step that is still open names it, so asking the approved list
// whether the suite can be run could demand a tool the run no longer has —
// the unfollowable instruction this loop has already paid for twice.
func toolCanStillBeCalled(plan PlanProjection, name executor.ToolName) bool {
	for _, step := range plan.Steps {
		if step.State == StepImplemented || step.State == StepValidated ||
			step.State == StepSkipped {
			continue
		}
		for _, completion := range step.CompletionTools {
			if completion == name {
				return true
			}
		}
	}
	return false
}

// errEmptyModelTurn is a turn that called no tool and declared no completion.
//
// Named separately from every other malformed turn because it is the one the
// loop can answer itself. The others are the model disagreeing with the
// contract — proposing a call and a completion together, or more calls than a
// round allows — and the next attempt has to be told about those. This one is
// the model losing its place, and the next round fixes it.
var errEmptyModelTurn = errors.New(
	"turn contains neither tool calls nor completion")

// maximumEmptyTurnRetries bounds how many silent turns one run answers before
// the attempt is spent, so a model that has stopped responding usefully cannot
// spend the whole round budget saying nothing.
//
// Four rather than two. Rung 4 on 2026-08-03 produced three empty turns in a
// row twice, so a bound of two absorbed the first two and lost the attempt to
// the third — paying the round cost and the attempt cost. A round is about four
// seconds against an attempt's thirty to eighty, and the round budget is
// sixteen, so absorbing more is the cheaper mistake in both directions.
const maximumEmptyTurnRetries = 4

// openStepAdvice names the steps a call can still be bound to.
//
// A turn whose every tool call named a closed step arrives here indistinguishable
// from a turn that said nothing: the adapter discards a call it cannot bind
// rather than failing the round over it, so the loop sees an empty turn and the
// model is told only that nothing happened. Telling it which steps are open
// turns that into something it can act on.
func openStepAdvice(plan PlanProjection) string {
	var open []string
	for _, step := range plan.Steps {
		if !StepMayAcceptAnotherCall(step.State) {
			continue
		}
		tools := make([]string, 0, len(step.CompletionTools))
		for _, tool := range step.CompletionTools {
			tools = append(tools, string(tool))
		}
		open = append(open, step.ID+" ("+strings.Join(tools, ", ")+")")
	}
	if len(open) == 0 {
		return "No step is still open."
	}
	return "Still open: " + strings.Join(open, "; ") + "."
}

// StepMayAcceptAnotherCall reports whether a plan step can still take a tool
// call, including after it has already been completed once.
//
// Completing a step is not the same as freezing what it names. Refinement
// rewrites the same files repeatedly — a patch that fixes the compile error the
// previous patch introduced is the ordinary case, not an anomaly — and running
// the suite again after a change is the whole point of running it at all. A
// step is spent only when it failed or was skipped, which are the two states
// that say this route is closed rather than this route has been used.
//
// Ladder rung 4 on 2026-08-03 is what the stricter reading costs. After two
// successful patches both write steps closed, the tool list collapsed from
// [apply-patch test] to [test], and the run — which still had a file to fix —
// spent the rest of the attempt emitting turns nothing could bind, five in a
// row, until the attempt died. The loop already intended to allow this: "a
// write bound to a closed step is the model rewriting a file the plan named,
// which stepOwningPath deliberately allows rather than discarding the attempt
// over". Three checks ahead of it refused first.
//
// What still constrains a rewrite is unchanged and lives elsewhere: the file
// scope a step declares, the whole-file rewrite limit, and the transition that
// is skipped rather than repeated for a step already implemented.
func StepMayAcceptAnotherCall(state StepState) bool {
	return state != StepFailed && state != StepSkipped
}

// maximumOutOfScopeRetries bounds how many times one attempt is told that the
// file it reached for is not in the plan.
//
// Two, because the answer is short and specific — here are the files you may
// change — and a run that has not taken it twice is not going to take it a
// fourth time. Past the bound the attempt ends, which is what the budget is
// for.
const maximumOutOfScopeRetries = 2

// writableFilesAdvice names the files this plan actually permits.
//
// A refusal that says only "outside plan step scope" leaves the run to guess
// which file it should have written, and on a generated workspace the wrong
// guess is right there in its opening context: a stub main.go beside the
// cmd/generated/main.go the plan owns.
func writableFilesAdvice(plan PlanProjection) string {
	seen := map[string]bool{}
	var files []string
	for _, step := range plan.Steps {
		if !StepMayAcceptAnotherCall(step.State) {
			continue
		}
		for _, file := range step.ExpectedFiles {
			if seen[file] {
				continue
			}
			seen[file] = true
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return "This plan names no file you may change."
	}
	return "The files this plan lets you change are " +
		strings.Join(files, " and ") + "."
}

// WritableFilesAdvice names the files a plan permits, for the round document.
//
// Exported because the observation the model reads is built in another package,
// and the alternative is that package deciding for itself which files are
// writable — a second answer to a question this one already answers, which is
// how the plan and its readers drift apart.
func WritableFilesAdvice(plan PlanProjection) string {
	return writableFilesAdvice(plan)
}
