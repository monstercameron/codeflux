package coordinator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

var (
	// ErrTaskControlBlocked reports that a durable pause or cancellation
	// prevents a new action from starting.
	ErrTaskControlBlocked = agentloop.ErrActionBlocked
	// ErrResumeReconciliationRequired reports paused divergence that requires
	// reconciliation or a new plan revision.
	ErrResumeReconciliationRequired = errors.New(
		"task resume requires reconciliation or a new plan revision",
	)
	// ErrResumeCompatibilityChanged reports a binding change that makes direct
	// resume unsafe.
	ErrResumeCompatibilityChanged = errors.New(
		"task resume compatibility changed",
	)
)

// ActionKind distinguishes model, tool, and validation cancellation policy.
type ActionKind string

const (
	ActionModel      ActionKind = "model"
	ActionTool       ActionKind = "tool"
	ActionValidation ActionKind = "validation"
)

// ActionDescriptor is the minimum immutable policy needed to decide whether
// an in-flight operation may finish after a pause request.
type ActionDescriptor struct {
	Kind        ActionKind
	RequestID   string
	SafeRead    bool
	LongRunning bool
}

func (value ActionDescriptor) validate() error {
	switch value.Kind {
	case ActionModel, ActionTool, ActionValidation:
	default:
		return errors.New("active action kind is invalid")
	}
	if strings.TrimSpace(value.RequestID) != value.RequestID ||
		value.RequestID == "" || len(value.RequestID) > 255 {
		return errors.New("active action request identity is invalid")
	}
	if value.SafeRead && value.Kind != ActionTool {
		return errors.New("only mediated tool reads may finish during pause")
	}
	return nil
}

type registeredAction struct {
	descriptor ActionDescriptor
	cancel     context.CancelFunc
	done       chan struct{}
}

type runActions struct {
	blocked storage.TaskControlDisposition
	nextID  uint64
	active  map[uint64]*registeredAction
}

// ActiveActionRegistry blocks new work and cooperatively cancels active model,
// tool, and validation contexts. Safe reads alone may finish during pause.
type ActiveActionRegistry struct {
	mu   sync.Mutex
	runs map[domain.RunID]*runActions
}

// NewActiveActionRegistry constructs an empty task-local cancellation registry.
func NewActiveActionRegistry() *ActiveActionRegistry {
	return &ActiveActionRegistry{
		runs: make(map[domain.RunID]*runActions),
	}
}

// BindActionContext registers one active action until the returned cancel
// function is called. A concurrently committed pause/cancel returns an already
// cancelled context so the caller cannot start new work.
func (registry *ActiveActionRegistry) BindActionContext(
	parent context.Context,
	_ domain.TaskID,
	runID domain.RunID,
	descriptor agentloop.ActionDescriptor,
) (context.Context, context.CancelFunc, error) {
	value := ActionDescriptor{
		Kind: ActionKind(descriptor.Kind), RequestID: descriptor.RequestID,
		SafeRead: descriptor.SafeRead, LongRunning: descriptor.LongRunning,
	}
	if runID.IsZero() {
		return nil, nil, errors.New("active action run is required")
	}
	if err := value.validate(); err != nil {
		return nil, nil, err
	}
	registry.mu.Lock()
	run := registry.runLocked(runID)
	if run.blocked != storage.TaskControlActive {
		registry.mu.Unlock()
		return nil, nil, ErrTaskControlBlocked
	}
	run.nextID++
	actionID := run.nextID
	ctx, cancelContext := context.WithCancel(parent)
	action := &registeredAction{
		descriptor: value, cancel: cancelContext, done: make(chan struct{}),
	}
	run.active[actionID] = action
	registry.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancelContext()
			registry.mu.Lock()
			if current := registry.runs[runID]; current != nil {
				delete(current.active, actionID)
			}
			close(action.done)
			registry.mu.Unlock()
		})
	}
	return ctx, release, nil
}

// RequestPause closes the new-action gate and cancels every action except a
// safe read. It returns without waiting so the worker pause can be issued
// immediately.
func (registry *ActiveActionRegistry) RequestPause(
	runID domain.RunID,
) error {
	if runID.IsZero() {
		return errors.New("pause registry run is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	run := registry.runLocked(runID)
	run.blocked = storage.TaskControlPauseRequested
	for _, action := range run.active {
		if !action.descriptor.SafeRead {
			action.cancel()
		}
	}
	return nil
}

// WaitForQuiescence waits for every action registered before the pause gate
// closed. Safe reads finish naturally; cancelled actions must persist their
// terminal outcome before releasing registration.
func (registry *ActiveActionRegistry) WaitForQuiescence(
	ctx context.Context,
	runID domain.RunID,
) error {
	registry.mu.Lock()
	run := registry.runLocked(runID)
	wait := make([]<-chan struct{}, 0, len(run.active))
	for _, action := range run.active {
		wait = append(wait, action.done)
	}
	registry.mu.Unlock()
	for _, done := range wait {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// CancelRun closes the action gate and cancels every active action.
func (registry *ActiveActionRegistry) CancelRun(
	runID domain.RunID,
) error {
	if runID.IsZero() {
		return errors.New("cancel registry run is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	run := registry.runLocked(runID)
	run.blocked = storage.TaskControlCancelled
	for _, action := range run.active {
		action.cancel()
	}
	return nil
}

// ActivateRun reopens the new-action gate after compatibility-checked resume.
func (registry *ActiveActionRegistry) ActivateRun(
	runID domain.RunID,
) error {
	if runID.IsZero() {
		return errors.New("resume registry run is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	run := registry.runLocked(runID)
	if len(run.active) != 0 {
		return errors.New("cannot resume while prior actions remain active")
	}
	run.blocked = storage.TaskControlActive
	return nil
}

func (registry *ActiveActionRegistry) runLocked(
	runID domain.RunID,
) *runActions {
	run := registry.runs[runID]
	if run == nil {
		run = &runActions{
			blocked: storage.TaskControlActive,
			active:  make(map[uint64]*registeredAction),
		}
		registry.runs[runID] = run
	}
	return run
}

// TaskControlStore is the durable state/event boundary.
type TaskControlStore interface {
	ReadTaskControl(
		context.Context,
		domain.TaskID,
	) (storage.TaskControlSnapshot, error)
	RequestTaskPause(
		context.Context,
		storage.RequestTaskPause,
	) (storage.TaskControlSnapshot, error)
	CompleteTaskPause(
		context.Context,
		storage.CompleteTaskPause,
	) (storage.TaskControlSnapshot, error)
	CancelControlledTask(
		context.Context,
		storage.CancelControlledTask,
	) (storage.TaskControlSnapshot, error)
	ResumeControlledTask(
		context.Context,
		storage.ResumeControlledTask,
	) (storage.TaskControlSnapshot, error)
	RecordTaskResumeBlocked(
		context.Context,
		storage.RecordTaskResumeBlocked,
	) (storage.TaskEvent, error)
	MarkTaskControlRecoveryRequired(
		context.Context,
		storage.MarkTaskControlRecoveryRequired,
	) (storage.TaskControlSnapshot, error)
	ReadTaskControlReplay(
		context.Context,
		storage.TaskControlReplayRequest,
	) (storage.TaskControlReplay, error)
}

// TaskControlCheckpointer is the narrow trigger-specific Lane A adapter used
// by this control plane.
type TaskControlCheckpointer interface {
	CapturePauseCheckpoint(
		context.Context,
		storage.TaskControlSnapshot,
		string,
	) (domain.CheckpointID, error)
}

// TaskControlWorkers applies control to the exact worker/run owner.
type TaskControlWorkers interface {
	PauseRun(domain.RunID, string) error
	ResumeRun(domain.RunID, string) error
	CancelRun(domain.RunID, string) error
}

// TaskControlFacts preserves the M14 budget, policy, and validation facts in
// the agent's between-action control check.
type TaskControlFacts interface {
	ReadTaskControlFacts(
		context.Context,
		domain.TaskID,
		domain.RunID,
	) (budgetAvailable, policyCurrent, validationComplete bool, err error)
}

// PausedEditClassification describes changes observed relative to the pause
// checkpoint.
type PausedEditClassification string

const (
	PausedEditsUnchanged      PausedEditClassification = "unchanged"
	PausedEditsNonOverlapping PausedEditClassification = "non-overlapping"
	PausedEditsConflicting    PausedEditClassification = "conflicting"
)

// ResumeAssessment is the bounded result of repository/worktree, policy,
// provider, tool, and ambiguous-effect verification.
type ResumeAssessment struct {
	RepositoryCurrent        bool
	WorktreeCurrent          bool
	PolicyCurrent            bool
	ProviderCurrent          bool
	ToolsCurrent             bool
	AmbiguousExternalOutcome bool
	PausedEdits              PausedEditClassification
	ChangedFiles             []string
	ConflictFiles            []string
	ReasonRedacted           string
}

func (value ResumeAssessment) validate() error {
	switch value.PausedEdits {
	case PausedEditsUnchanged, PausedEditsNonOverlapping,
		PausedEditsConflicting:
	default:
		return errors.New("resume edit classification is invalid")
	}
	if !canonicalControlPaths(value.ChangedFiles) ||
		!canonicalControlPaths(value.ConflictFiles) ||
		(value.PausedEdits == PausedEditsConflicting &&
			len(value.ConflictFiles) == 0) ||
		(value.PausedEdits != PausedEditsConflicting &&
			len(value.ConflictFiles) != 0) {
		return errors.New("resume changed-file classification is invalid")
	}
	if strings.TrimSpace(value.ReasonRedacted) != value.ReasonRedacted ||
		value.ReasonRedacted == "" ||
		len(value.ReasonRedacted) > 2048 {
		return errors.New("resume assessment reason is invalid")
	}
	return nil
}

func canonicalControlPaths(values []string) bool {
	if len(values) > 256 {
		return false
	}
	return slices.IsSorted(values) &&
		!hasDuplicateControlPaths(values) &&
		!slices.ContainsFunc(values, func(value string) bool {
			return value == "" || strings.TrimSpace(value) != value ||
				strings.Contains(value, "\\") ||
				strings.HasPrefix(value, "/") ||
				value == ".." || strings.HasPrefix(value, "../") ||
				strings.Contains(value, "/../")
		})
}

func hasDuplicateControlPaths(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}

// ResumeVerifier compares current state with the last verified checkpoint and
// immutable run bindings.
type ResumeVerifier interface {
	VerifyResume(
		context.Context,
		storage.TaskControlSnapshot,
	) (ResumeAssessment, error)
}

// TaskControlDependencies are explicit durable, cancellation, worker,
// checkpoint, and compatibility ports.
type TaskControlDependencies struct {
	Store       TaskControlStore
	Actions     *ActiveActionRegistry
	Workers     TaskControlWorkers
	Checkpoints TaskControlCheckpointer
	Facts       TaskControlFacts
	Resume      ResumeVerifier
	NewEventID  func() (domain.EventID, error)
}

// TaskControlService owns pause, cancel, resume, and the agent control reader.
type TaskControlService struct {
	dependencies TaskControlDependencies
}

// NewTaskControlService validates the complete control plane.
func NewTaskControlService(
	dependencies TaskControlDependencies,
) (*TaskControlService, error) {
	if dependencies.Store == nil || dependencies.Actions == nil ||
		dependencies.Workers == nil || dependencies.Checkpoints == nil ||
		dependencies.Facts == nil || dependencies.Resume == nil {
		return nil, errors.New("task control dependencies are required")
	}
	if dependencies.NewEventID == nil {
		dependencies.NewEventID = domain.NewEventID
	}
	return &TaskControlService{dependencies: dependencies}, nil
}

// PauseTask durably closes the action gate, applies the safe-read policy,
// captures the pause checkpoint, and only then commits paused.
func (service *TaskControlService) PauseTask(
	ctx context.Context,
	input PauseTaskInput,
) (storage.TaskControlSnapshot, error) {
	if err := input.validate(); err != nil {
		return storage.TaskControlSnapshot{}, err
	}
	requested, err := service.dependencies.Store.RequestTaskPause(
		ctx,
		storage.RequestTaskPause{
			EventID: input.RequestEventID, TaskID: input.TaskID,
			RunID: input.RunID, ExpectedTaskRevision: input.ExpectedTaskRevision,
			ExpectedRunRevision: input.ExpectedRunRevision,
			Reason:              domain.PauseReasonUserRequested,
			ReasonRedacted:      input.ReasonRedacted,
			IdempotencyKey:      input.IdempotencyKey + "/requested",
		},
	)
	if err != nil {
		return storage.TaskControlSnapshot{}, err
	}
	if err := service.dependencies.Actions.RequestPause(input.RunID); err != nil {
		return requested, err
	}
	if err := service.dependencies.Workers.PauseRun(
		input.RunID, input.ReasonRedacted,
	); err != nil {
		return requested, err
	}
	if err := service.dependencies.Actions.WaitForQuiescence(
		ctx, input.RunID,
	); err != nil {
		return requested, err
	}
	checkpointID, err := service.dependencies.Checkpoints.
		CapturePauseCheckpoint(
			ctx, requested, input.IdempotencyKey+"/checkpoint",
		)
	if err != nil {
		return requested, err
	}
	return service.dependencies.Store.CompleteTaskPause(
		ctx,
		storage.CompleteTaskPause{
			EventID: input.PausedEventID, TaskID: input.TaskID,
			RunID: input.RunID, ExpectedTaskRevision: requested.TaskRevision,
			ExpectedRunRevision: requested.RunRevision,
			Reason:              domain.PauseReasonUserRequested,
			ReasonRedacted:      input.ReasonRedacted,
			CheckpointID:        checkpointID,
			IdempotencyKey:      input.IdempotencyKey + "/paused",
		},
	)
}

// CancelTask commits terminal cancellation first, then interrupts local and
// worker-owned actions. Retrying the same command safely retries delivery.
func (service *TaskControlService) CancelTask(
	ctx context.Context,
	input CancelTaskInput,
) (storage.TaskControlSnapshot, error) {
	if err := input.validate(); err != nil {
		return storage.TaskControlSnapshot{}, err
	}
	cancelled, err := service.dependencies.Store.CancelControlledTask(
		ctx,
		storage.CancelControlledTask{
			EventID: input.EventID, TaskID: input.TaskID, RunID: input.RunID,
			ExpectedTaskRevision: input.ExpectedTaskRevision,
			ExpectedRunRevision:  input.ExpectedRunRevision,
			Reason:               domain.CancellationReasonUserRequested,
			ReasonRedacted:       input.ReasonRedacted,
			IdempotencyKey:       input.IdempotencyKey,
		},
	)
	if err != nil {
		return storage.TaskControlSnapshot{}, err
	}
	actionErr := service.dependencies.Actions.CancelRun(input.RunID)
	workerErr := service.dependencies.Workers.CancelRun(
		input.RunID, input.ReasonRedacted,
	)
	return cancelled, errors.Join(actionErr, workerErr)
}

// ResumeTask verifies every immutable binding and paused edit before starting
// a new attempt. Conflicting edits remain paused and are durably surfaced.
func (service *TaskControlService) ResumeTask(
	ctx context.Context,
	input ResumeTaskInput,
) (ResumeTaskResult, error) {
	if err := input.validate(); err != nil {
		return ResumeTaskResult{}, err
	}
	current, err := service.dependencies.Store.ReadTaskControl(
		ctx, input.TaskID,
	)
	if err != nil {
		return ResumeTaskResult{}, err
	}
	if current.RunID != input.RunID ||
		current.TaskRevision != input.ExpectedTaskRevision ||
		current.RunRevision != input.ExpectedRunRevision ||
		current.Disposition != storage.TaskControlPaused {
		return ResumeTaskResult{}, storage.ErrStaleRevision
	}
	assessment, err := service.dependencies.Resume.VerifyResume(ctx, current)
	if err != nil {
		return ResumeTaskResult{}, err
	}
	if err := assessment.validate(); err != nil {
		return ResumeTaskResult{}, err
	}
	compatible := assessment.RepositoryCurrent &&
		assessment.WorktreeCurrent &&
		assessment.PolicyCurrent &&
		assessment.ProviderCurrent &&
		assessment.ToolsCurrent &&
		!assessment.AmbiguousExternalOutcome
	if !compatible || assessment.PausedEdits == PausedEditsConflicting {
		_, recordErr := service.dependencies.Store.RecordTaskResumeBlocked(
			context.WithoutCancel(ctx),
			storage.RecordTaskResumeBlocked{
				EventID: input.BlockedEventID, TaskID: input.TaskID,
				RunID:                input.RunID,
				ExpectedTaskRevision: input.ExpectedTaskRevision,
				ExpectedRunRevision:  input.ExpectedRunRevision,
				ReasonRedacted:       assessment.ReasonRedacted,
				ChangedFiles:         assessment.ChangedFiles,
				ConflictFiles:        assessment.ConflictFiles,
				IdempotencyKey:       input.IdempotencyKey + "/blocked",
			},
		)
		classification := ErrResumeCompatibilityChanged
		if assessment.PausedEdits == PausedEditsConflicting {
			classification = ErrResumeReconciliationRequired
		}
		return ResumeTaskResult{
			Control: current, Assessment: assessment,
		}, errors.Join(classification, recordErr)
	}
	resumed, err := service.dependencies.Store.ResumeControlledTask(
		ctx,
		storage.ResumeControlledTask{
			EventID: input.ResumedEventID, TaskID: input.TaskID,
			RunID:                input.RunID,
			ExpectedTaskRevision: input.ExpectedTaskRevision,
			ExpectedRunRevision:  input.ExpectedRunRevision,
			NonOverlappingFiles:  assessment.ChangedFiles,
			IdempotencyKey:       input.IdempotencyKey + "/resumed",
		},
	)
	if err != nil {
		return ResumeTaskResult{}, err
	}
	if err := service.dependencies.Workers.ResumeRun(
		input.RunID, "compatibility-checked task resume",
	); err != nil {
		recovered, recoveryErr := service.markResumeRecovery(
			ctx,
			input,
			resumed,
			"worker could not be reacquired after resume",
		)
		return ResumeTaskResult{
			Control: recovered, Assessment: assessment,
		}, errors.Join(err, recoveryErr)
	}
	if err := service.dependencies.Actions.ActivateRun(input.RunID); err != nil {
		recovered, recoveryErr := service.markResumeRecovery(
			ctx,
			input,
			resumed,
			"prior action did not quiesce before resume",
		)
		return ResumeTaskResult{
			Control: recovered, Assessment: assessment,
		}, errors.Join(err, recoveryErr)
	}
	return ResumeTaskResult{
		Control: resumed, Assessment: assessment,
	}, nil
}

// ResumeVerifiedRecovery reacquires execution only after a separate recovery
// authority transaction has moved the exact run to a paused boundary. It
// deliberately reuses the full resume verifier and worker/action compensation
// path rather than bypassing it from the recovery RPC.
func (service *TaskControlService) ResumeVerifiedRecovery(
	ctx context.Context,
	paused storage.TaskControlSnapshot,
	reasonRedacted string,
	idempotencyKey string,
) (storage.TaskControlSnapshot, error) {
	if service == nil || paused.TaskID.IsZero() || paused.RunID.IsZero() ||
		paused.Disposition != storage.TaskControlPaused ||
		strings.TrimSpace(reasonRedacted) != reasonRedacted || reasonRedacted == "" ||
		strings.TrimSpace(idempotencyKey) != idempotencyKey || idempotencyKey == "" {
		return storage.TaskControlSnapshot{}, errors.New("verified recovery resume input is invalid")
	}
	blockedEventID, err := service.dependencies.NewEventID()
	if err != nil {
		return storage.TaskControlSnapshot{}, err
	}
	resumedEventID, err := service.dependencies.NewEventID()
	if err != nil {
		return storage.TaskControlSnapshot{}, err
	}
	result, err := service.ResumeTask(ctx, ResumeTaskInput{
		TaskID: paused.TaskID, RunID: paused.RunID,
		ExpectedTaskRevision: paused.TaskRevision,
		ExpectedRunRevision:  paused.RunRevision,
		IdempotencyKey:       idempotencyKey,
		BlockedEventID:       blockedEventID,
		ResumedEventID:       resumedEventID,
	})
	return result.Control, err
}

func (service *TaskControlService) markResumeRecovery(
	ctx context.Context,
	input ResumeTaskInput,
	resumed storage.TaskControlSnapshot,
	reason string,
) (storage.TaskControlSnapshot, error) {
	recoveryEventID, err := service.dependencies.NewEventID()
	if err != nil {
		return resumed, err
	}
	return service.dependencies.Store.MarkTaskControlRecoveryRequired(
		context.WithoutCancel(ctx),
		storage.MarkTaskControlRecoveryRequired{
			EventID:              recoveryEventID,
			TaskID:               input.TaskID,
			RunID:                input.RunID,
			ExpectedTaskRevision: resumed.TaskRevision,
			ExpectedRunRevision:  resumed.RunRevision,
			ReasonRedacted:       reason,
			IdempotencyKey:       input.IdempotencyKey + "/recovery",
		},
	)
}

// ReadControl adapts durable task control to the M14 agent loop.
func (service *TaskControlService) ReadControl(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) (agentloop.ControlState, error) {
	current, err := service.dependencies.Store.ReadTaskControl(ctx, taskID)
	if err != nil {
		return agentloop.ControlState{}, err
	}
	if current.RunID != runID {
		return agentloop.ControlState{}, errors.New(
			"task control belongs to another run",
		)
	}
	budget, policy, validation, err :=
		service.dependencies.Facts.ReadTaskControlFacts(
			ctx, taskID, runID,
		)
	if err != nil {
		return agentloop.ControlState{}, err
	}
	disposition := agentloop.ControlActive
	switch current.Disposition {
	case storage.TaskControlActive:
	case storage.TaskControlPauseRequested:
		disposition = agentloop.ControlPauseRequested
	case storage.TaskControlPaused:
		disposition = agentloop.ControlPaused
	case storage.TaskControlCancelled:
		disposition = agentloop.ControlCancelled
	case storage.TaskControlRecovery:
		disposition = agentloop.ControlStopped
	default:
		return agentloop.ControlState{}, errors.New(
			"durable task control disposition is invalid",
		)
	}
	return agentloop.ControlState{
		Disposition: disposition, BudgetAvailable: budget,
		PolicyCurrent: policy, ValidationComplete: validation,
	}, nil
}

// PauseTaskInput is one attributable optimistic pause request.
type PauseTaskInput struct {
	RequestEventID       domain.EventID
	PausedEventID        domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	ReasonRedacted       string
	IdempotencyKey       string
}

func (input PauseTaskInput) validate() error {
	if input.RequestEventID.IsZero() || input.PausedEventID.IsZero() ||
		input.RequestEventID == input.PausedEventID ||
		input.TaskID.IsZero() || input.RunID.IsZero() {
		return errors.New("pause control identities are required and distinct")
	}
	return validateTaskControlText(input.ReasonRedacted, input.IdempotencyKey)
}

// CancelTaskInput is one attributable optimistic terminal cancellation.
type CancelTaskInput struct {
	EventID              domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	ReasonRedacted       string
	IdempotencyKey       string
}

func (input CancelTaskInput) validate() error {
	if input.EventID.IsZero() || input.TaskID.IsZero() ||
		input.RunID.IsZero() {
		return errors.New("cancel control identities are required")
	}
	return validateTaskControlText(input.ReasonRedacted, input.IdempotencyKey)
}

// ResumeTaskInput is one compatibility-checked optimistic resume.
type ResumeTaskInput struct {
	ResumedEventID       domain.EventID
	BlockedEventID       domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	IdempotencyKey       string
}

func (input ResumeTaskInput) validate() error {
	if input.ResumedEventID.IsZero() || input.BlockedEventID.IsZero() ||
		input.ResumedEventID == input.BlockedEventID ||
		input.TaskID.IsZero() || input.RunID.IsZero() ||
		strings.TrimSpace(input.IdempotencyKey) != input.IdempotencyKey ||
		input.IdempotencyKey == "" || len(input.IdempotencyKey) > 180 {
		return errors.New("resume control input is invalid")
	}
	return nil
}

// ResumeTaskResult surfaces non-overlapping edits as well as the durable state.
type ResumeTaskResult struct {
	Control    storage.TaskControlSnapshot
	Assessment ResumeAssessment
}

func validateTaskControlText(reason, key string) error {
	if strings.TrimSpace(reason) != reason || reason == "" ||
		len(reason) > 2048 ||
		strings.TrimSpace(key) != key || key == "" || len(key) > 180 {
		return fmt.Errorf("task control reason or idempotency key is invalid")
	}
	return nil
}

var (
	_ agentloop.ControlReader          = (*TaskControlService)(nil)
	_ agentloop.ControlInterruptBridge = (*ActiveActionRegistry)(nil)
)
