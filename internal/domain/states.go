package domain

import (
	"errors"
	"fmt"
)

var (
	// ErrUnknownState classifies a state value outside its declared machine.
	ErrUnknownState = errors.New("unknown state")
	// ErrInvalidTransition classifies a structurally forbidden state change.
	ErrInvalidTransition = errors.New("invalid state transition")
	// ErrApprovalRequired classifies an action that lacks a granted approval.
	ErrApprovalRequired = errors.New("granted approval required")
)

// TransitionError is a typed, user-presentable state-transition failure.
type TransitionError struct {
	Machine string
	From    string
	To      string
	Cause   error
}

func (e *TransitionError) Error() string {
	switch {
	case errors.Is(e.Cause, ErrUnknownState):
		return fmt.Sprintf("%s state transition %q -> %q contains an unknown state", e.Machine, e.From, e.To)
	case errors.Is(e.Cause, ErrApprovalRequired):
		return fmt.Sprintf("%s state transition %q -> %q requires a granted approval", e.Machine, e.From, e.To)
	default:
		return fmt.Sprintf("%s state transition %q -> %q is not permitted", e.Machine, e.From, e.To)
	}
}

// Unwrap exposes the stable transition classification.
func (e *TransitionError) Unwrap() error {
	return e.Cause
}

type transitionTable[S comparable] map[S]map[S]struct{}

func transitions[S comparable](pairs ...[2]S) transitionTable[S] {
	table := make(transitionTable[S])
	for _, pair := range pairs {
		if table[pair[0]] == nil {
			table[pair[0]] = make(map[S]struct{})
		}
		table[pair[0]][pair[1]] = struct{}{}
	}
	return table
}

func validateTransition[S ~string](
	machine string,
	from S,
	to S,
	valid func(S) bool,
	table transitionTable[S],
) error {
	if !valid(from) || !valid(to) {
		return &TransitionError{
			Machine: machine,
			From:    string(from),
			To:      string(to),
			Cause:   ErrUnknownState,
		}
	}
	if _, allowed := table[from][to]; !allowed {
		return &TransitionError{
			Machine: machine,
			From:    string(from),
			To:      string(to),
			Cause:   ErrInvalidTransition,
		}
	}
	return nil
}

// TaskState describes the durable lifecycle of a user task.
type TaskState string

const (
	TaskStateDraft                TaskState = "draft"
	TaskStateForecasting          TaskState = "forecasting"
	TaskStateAwaitingPlanApproval TaskState = "awaiting-plan-approval"
	TaskStateReady                TaskState = "ready"
	TaskStateRunning              TaskState = "running"
	TaskStatePaused               TaskState = "paused"
	TaskStateAwaitingAuthority    TaskState = "awaiting-authority"
	TaskStateValidating           TaskState = "validating"
	TaskStateAwaitingReview       TaskState = "awaiting-review"
	TaskStateCompleted            TaskState = "completed"
	TaskStateFailed               TaskState = "failed"
	TaskStateCancelled            TaskState = "cancelled"
	TaskStateRolledBack           TaskState = "rolled-back"
	TaskStateRecoveryRequired     TaskState = "recovery-required"
)

var taskStates = []TaskState{
	TaskStateDraft,
	TaskStateForecasting,
	TaskStateAwaitingPlanApproval,
	TaskStateReady,
	TaskStateRunning,
	TaskStatePaused,
	TaskStateAwaitingAuthority,
	TaskStateValidating,
	TaskStateAwaitingReview,
	TaskStateCompleted,
	TaskStateFailed,
	TaskStateCancelled,
	TaskStateRolledBack,
	TaskStateRecoveryRequired,
}

var taskTransitions = transitions(
	[2]TaskState{TaskStateDraft, TaskStateForecasting},
	[2]TaskState{TaskStateDraft, TaskStateCancelled},
	[2]TaskState{TaskStateForecasting, TaskStateAwaitingPlanApproval},
	[2]TaskState{TaskStateForecasting, TaskStatePaused},
	[2]TaskState{TaskStateForecasting, TaskStateFailed},
	[2]TaskState{TaskStateForecasting, TaskStateCancelled},
	[2]TaskState{TaskStateAwaitingPlanApproval, TaskStateForecasting},
	[2]TaskState{TaskStateAwaitingPlanApproval, TaskStateReady},
	[2]TaskState{TaskStateAwaitingPlanApproval, TaskStateCancelled},
	[2]TaskState{TaskStateReady, TaskStateRunning},
	[2]TaskState{TaskStateReady, TaskStatePaused},
	[2]TaskState{TaskStateReady, TaskStateCancelled},
	[2]TaskState{TaskStateReady, TaskStateRecoveryRequired},
	[2]TaskState{TaskStateRunning, TaskStatePaused},
	[2]TaskState{TaskStateRunning, TaskStateAwaitingAuthority},
	[2]TaskState{TaskStateRunning, TaskStateValidating},
	[2]TaskState{TaskStateRunning, TaskStateFailed},
	[2]TaskState{TaskStateRunning, TaskStateCancelled},
	[2]TaskState{TaskStateRunning, TaskStateRecoveryRequired},
	[2]TaskState{TaskStatePaused, TaskStateForecasting},
	[2]TaskState{TaskStatePaused, TaskStateReady},
	[2]TaskState{TaskStatePaused, TaskStateRunning},
	[2]TaskState{TaskStatePaused, TaskStateCancelled},
	[2]TaskState{TaskStatePaused, TaskStateRecoveryRequired},
	[2]TaskState{TaskStateAwaitingAuthority, TaskStateRunning},
	[2]TaskState{TaskStateAwaitingAuthority, TaskStatePaused},
	[2]TaskState{TaskStateAwaitingAuthority, TaskStateCancelled},
	[2]TaskState{TaskStateAwaitingAuthority, TaskStateRecoveryRequired},
	[2]TaskState{TaskStateValidating, TaskStateRunning},
	[2]TaskState{TaskStateValidating, TaskStateAwaitingReview},
	[2]TaskState{TaskStateValidating, TaskStateFailed},
	[2]TaskState{TaskStateValidating, TaskStateCancelled},
	[2]TaskState{TaskStateValidating, TaskStateRecoveryRequired},
	[2]TaskState{TaskStateAwaitingReview, TaskStateRunning},
	[2]TaskState{TaskStateAwaitingReview, TaskStateCompleted},
	[2]TaskState{TaskStateAwaitingReview, TaskStateCancelled},
	[2]TaskState{TaskStateAwaitingReview, TaskStateRolledBack},
	[2]TaskState{TaskStateCompleted, TaskStateRolledBack},
	[2]TaskState{TaskStateFailed, TaskStateReady},
	[2]TaskState{TaskStateFailed, TaskStateCancelled},
	[2]TaskState{TaskStateFailed, TaskStateRecoveryRequired},
	[2]TaskState{TaskStateRecoveryRequired, TaskStatePaused},
	[2]TaskState{TaskStateRecoveryRequired, TaskStateReady},
	[2]TaskState{TaskStateRecoveryRequired, TaskStateFailed},
	[2]TaskState{TaskStateRecoveryRequired, TaskStateCancelled},
	[2]TaskState{TaskStateRecoveryRequired, TaskStateRolledBack},
)

// AllTaskStates returns the complete declared task-state set.
func AllTaskStates() []TaskState {
	return append([]TaskState(nil), taskStates...)
}

// IsValid reports whether the task state is declared.
func (state TaskState) IsValid() bool {
	return containsState(taskStates, state)
}

// IsTerminal reports whether the task has no ordinary continuation.
func (state TaskState) IsTerminal() bool {
	switch state {
	case TaskStateCompleted, TaskStateCancelled, TaskStateRolledBack:
		return true
	default:
		return false
	}
}

// IsRecoverable reports whether explicit user or recovery action may continue the task.
func (state TaskState) IsRecoverable() bool {
	switch state {
	case TaskStatePaused, TaskStateAwaitingAuthority, TaskStateFailed, TaskStateRecoveryRequired:
		return true
	default:
		return false
	}
}

// TaskTransition carries evidence needed to validate a task-state change.
type TaskTransition struct {
	From     TaskState
	To       TaskState
	Approval ApprovalRequestState
}

// ValidateTaskTransition validates one pure task lifecycle transition.
func ValidateTaskTransition(change TaskTransition) error {
	if err := validateTransition("task", change.From, change.To, TaskState.IsValid, taskTransitions); err != nil {
		return err
	}
	if change.From == TaskStateAwaitingAuthority &&
		change.To == TaskStateRunning &&
		change.Approval != ApprovalRequestStateGranted {
		return &TransitionError{
			Machine: "task",
			From:    string(change.From),
			To:      string(change.To),
			Cause:   ErrApprovalRequired,
		}
	}
	return nil
}

// RunState describes one task execution attempt independently from the task.
type RunState string

const (
	RunStatePending          RunState = "pending"
	RunStateStarting         RunState = "starting"
	RunStateRunning          RunState = "running"
	RunStatePausing          RunState = "pausing"
	RunStatePaused           RunState = "paused"
	RunStateValidating       RunState = "validating"
	RunStateCompleted        RunState = "completed"
	RunStateFailed           RunState = "failed"
	RunStateCancelled        RunState = "cancelled"
	RunStateRecoveryRequired RunState = "recovery-required"
)

var runStates = []RunState{
	RunStatePending,
	RunStateStarting,
	RunStateRunning,
	RunStatePausing,
	RunStatePaused,
	RunStateValidating,
	RunStateCompleted,
	RunStateFailed,
	RunStateCancelled,
	RunStateRecoveryRequired,
}

var runTransitions = transitions(
	[2]RunState{RunStatePending, RunStateStarting},
	[2]RunState{RunStatePending, RunStateCancelled},
	[2]RunState{RunStateStarting, RunStateRunning},
	[2]RunState{RunStateStarting, RunStateFailed},
	[2]RunState{RunStateStarting, RunStateCancelled},
	[2]RunState{RunStateStarting, RunStateRecoveryRequired},
	[2]RunState{RunStateRunning, RunStatePausing},
	[2]RunState{RunStateRunning, RunStateValidating},
	[2]RunState{RunStateRunning, RunStateCompleted},
	[2]RunState{RunStateRunning, RunStateFailed},
	[2]RunState{RunStateRunning, RunStateCancelled},
	[2]RunState{RunStateRunning, RunStateRecoveryRequired},
	[2]RunState{RunStatePausing, RunStatePaused},
	[2]RunState{RunStatePausing, RunStateFailed},
	[2]RunState{RunStatePausing, RunStateRecoveryRequired},
	[2]RunState{RunStatePaused, RunStateStarting},
	[2]RunState{RunStatePaused, RunStateCancelled},
	[2]RunState{RunStatePaused, RunStateRecoveryRequired},
	[2]RunState{RunStateValidating, RunStateRunning},
	[2]RunState{RunStateValidating, RunStateCompleted},
	[2]RunState{RunStateValidating, RunStateFailed},
	[2]RunState{RunStateValidating, RunStateCancelled},
	[2]RunState{RunStateValidating, RunStateRecoveryRequired},
	[2]RunState{RunStateRecoveryRequired, RunStatePaused},
	[2]RunState{RunStateRecoveryRequired, RunStateFailed},
	[2]RunState{RunStateRecoveryRequired, RunStateCancelled},
)

// AllRunStates returns the complete declared run-state set.
func AllRunStates() []RunState {
	return append([]RunState(nil), runStates...)
}

func (state RunState) IsValid() bool {
	return containsState(runStates, state)
}

func (state RunState) IsTerminal() bool {
	switch state {
	case RunStateCompleted, RunStateFailed, RunStateCancelled:
		return true
	default:
		return false
	}
}

func (state RunState) IsRecoverable() bool {
	return state == RunStatePaused || state == RunStateRecoveryRequired
}

// ValidateRunTransition validates one pure run lifecycle transition.
func ValidateRunTransition(from, to RunState) error {
	return validateTransition("run", from, to, RunState.IsValid, runTransitions)
}

// CommandExecutionState describes one mediated tool action independently from a run.
type CommandExecutionState string

const (
	CommandExecutionStatePending           CommandExecutionState = "pending"
	CommandExecutionStateAwaitingAuthority CommandExecutionState = "awaiting-authority"
	CommandExecutionStateAuthorized        CommandExecutionState = "authorized"
	CommandExecutionStateRunning           CommandExecutionState = "running"
	CommandExecutionStateSucceeded         CommandExecutionState = "succeeded"
	CommandExecutionStateFailed            CommandExecutionState = "failed"
	CommandExecutionStateCancelled         CommandExecutionState = "cancelled"
	CommandExecutionStateOutcomeUnknown    CommandExecutionState = "outcome-unknown"
)

var commandExecutionStates = []CommandExecutionState{
	CommandExecutionStatePending,
	CommandExecutionStateAwaitingAuthority,
	CommandExecutionStateAuthorized,
	CommandExecutionStateRunning,
	CommandExecutionStateSucceeded,
	CommandExecutionStateFailed,
	CommandExecutionStateCancelled,
	CommandExecutionStateOutcomeUnknown,
}

var commandExecutionTransitions = transitions(
	[2]CommandExecutionState{CommandExecutionStatePending, CommandExecutionStateAwaitingAuthority},
	[2]CommandExecutionState{CommandExecutionStatePending, CommandExecutionStateAuthorized},
	[2]CommandExecutionState{CommandExecutionStatePending, CommandExecutionStateCancelled},
	[2]CommandExecutionState{CommandExecutionStateAwaitingAuthority, CommandExecutionStateAuthorized},
	[2]CommandExecutionState{CommandExecutionStateAwaitingAuthority, CommandExecutionStateCancelled},
	[2]CommandExecutionState{CommandExecutionStateAuthorized, CommandExecutionStateRunning},
	[2]CommandExecutionState{CommandExecutionStateAuthorized, CommandExecutionStateCancelled},
	[2]CommandExecutionState{CommandExecutionStateRunning, CommandExecutionStateSucceeded},
	[2]CommandExecutionState{CommandExecutionStateRunning, CommandExecutionStateFailed},
	[2]CommandExecutionState{CommandExecutionStateRunning, CommandExecutionStateCancelled},
	[2]CommandExecutionState{CommandExecutionStateRunning, CommandExecutionStateOutcomeUnknown},
	[2]CommandExecutionState{CommandExecutionStateOutcomeUnknown, CommandExecutionStateSucceeded},
	[2]CommandExecutionState{CommandExecutionStateOutcomeUnknown, CommandExecutionStateFailed},
	[2]CommandExecutionState{CommandExecutionStateOutcomeUnknown, CommandExecutionStateCancelled},
)

// AllCommandExecutionStates returns the complete command-state set.
func AllCommandExecutionStates() []CommandExecutionState {
	return append([]CommandExecutionState(nil), commandExecutionStates...)
}

func (state CommandExecutionState) IsValid() bool {
	return containsState(commandExecutionStates, state)
}

func (state CommandExecutionState) IsTerminal() bool {
	switch state {
	case CommandExecutionStateSucceeded, CommandExecutionStateFailed, CommandExecutionStateCancelled:
		return true
	default:
		return false
	}
}

func (state CommandExecutionState) IsRecoverable() bool {
	return state == CommandExecutionStateOutcomeUnknown
}

// CommandExecutionTransition carries approval evidence for a mediated action.
type CommandExecutionTransition struct {
	From     CommandExecutionState
	To       CommandExecutionState
	Approval ApprovalRequestState
}

// ValidateCommandExecutionTransition validates one mediated action transition.
func ValidateCommandExecutionTransition(change CommandExecutionTransition) error {
	if err := validateTransition(
		"command execution",
		change.From,
		change.To,
		CommandExecutionState.IsValid,
		commandExecutionTransitions,
	); err != nil {
		return err
	}
	if change.From == CommandExecutionStateAwaitingAuthority &&
		change.To == CommandExecutionStateAuthorized &&
		change.Approval != ApprovalRequestStateGranted {
		return &TransitionError{
			Machine: "command execution",
			From:    string(change.From),
			To:      string(change.To),
			Cause:   ErrApprovalRequired,
		}
	}
	return nil
}

// ApprovalRequestState describes one user authority request.
type ApprovalRequestState string

const (
	ApprovalRequestStatePending   ApprovalRequestState = "pending"
	ApprovalRequestStateGranted   ApprovalRequestState = "granted"
	ApprovalRequestStateDenied    ApprovalRequestState = "denied"
	ApprovalRequestStateExpired   ApprovalRequestState = "expired"
	ApprovalRequestStateCancelled ApprovalRequestState = "cancelled"
)

var approvalRequestStates = []ApprovalRequestState{
	ApprovalRequestStatePending,
	ApprovalRequestStateGranted,
	ApprovalRequestStateDenied,
	ApprovalRequestStateExpired,
	ApprovalRequestStateCancelled,
}

var approvalRequestTransitions = transitions(
	[2]ApprovalRequestState{ApprovalRequestStatePending, ApprovalRequestStateGranted},
	[2]ApprovalRequestState{ApprovalRequestStatePending, ApprovalRequestStateDenied},
	[2]ApprovalRequestState{ApprovalRequestStatePending, ApprovalRequestStateExpired},
	[2]ApprovalRequestState{ApprovalRequestStatePending, ApprovalRequestStateCancelled},
)

func (state ApprovalRequestState) IsValid() bool {
	return containsState(approvalRequestStates, state)
}

func (state ApprovalRequestState) IsTerminal() bool {
	return state.IsValid() && state != ApprovalRequestStatePending
}

// ValidateApprovalRequestTransition validates one approval resolution.
func ValidateApprovalRequestTransition(from, to ApprovalRequestState) error {
	return validateTransition(
		"approval request",
		from,
		to,
		ApprovalRequestState.IsValid,
		approvalRequestTransitions,
	)
}

// CheckpointState describes creation and validity of one recovery checkpoint.
type CheckpointState string

const (
	CheckpointStateCreating    CheckpointState = "creating"
	CheckpointStateReady       CheckpointState = "ready"
	CheckpointStateFailed      CheckpointState = "failed"
	CheckpointStateInvalidated CheckpointState = "invalidated"
)

var checkpointStates = []CheckpointState{
	CheckpointStateCreating,
	CheckpointStateReady,
	CheckpointStateFailed,
	CheckpointStateInvalidated,
}

var checkpointTransitions = transitions(
	[2]CheckpointState{CheckpointStateCreating, CheckpointStateReady},
	[2]CheckpointState{CheckpointStateCreating, CheckpointStateFailed},
	[2]CheckpointState{CheckpointStateReady, CheckpointStateInvalidated},
)

func (state CheckpointState) IsValid() bool {
	return containsState(checkpointStates, state)
}

func (state CheckpointState) IsTerminal() bool {
	return state == CheckpointStateFailed || state == CheckpointStateInvalidated
}

// ValidateCheckpointTransition validates one checkpoint transition.
func ValidateCheckpointTransition(from, to CheckpointState) error {
	return validateTransition("checkpoint", from, to, CheckpointState.IsValid, checkpointTransitions)
}

// ValidationState describes one required or advisory validation execution.
type ValidationState string

const (
	ValidationStatePending     ValidationState = "pending"
	ValidationStateRunning     ValidationState = "running"
	ValidationStatePassed      ValidationState = "passed"
	ValidationStateFailed      ValidationState = "failed"
	ValidationStateWaived      ValidationState = "waived"
	ValidationStateSkipped     ValidationState = "skipped"
	ValidationStateCancelled   ValidationState = "cancelled"
	ValidationStateInvalidated ValidationState = "invalidated"
)

var validationStates = []ValidationState{
	ValidationStatePending,
	ValidationStateRunning,
	ValidationStatePassed,
	ValidationStateFailed,
	ValidationStateWaived,
	ValidationStateSkipped,
	ValidationStateCancelled,
	ValidationStateInvalidated,
}

var validationTransitions = transitions(
	[2]ValidationState{ValidationStatePending, ValidationStateRunning},
	[2]ValidationState{ValidationStatePending, ValidationStateWaived},
	[2]ValidationState{ValidationStatePending, ValidationStateSkipped},
	[2]ValidationState{ValidationStatePending, ValidationStateCancelled},
	[2]ValidationState{ValidationStateRunning, ValidationStatePassed},
	[2]ValidationState{ValidationStateRunning, ValidationStateFailed},
	[2]ValidationState{ValidationStateRunning, ValidationStateCancelled},
	[2]ValidationState{ValidationStatePassed, ValidationStateInvalidated},
	[2]ValidationState{ValidationStateFailed, ValidationStateInvalidated},
	[2]ValidationState{ValidationStateWaived, ValidationStateInvalidated},
	[2]ValidationState{ValidationStateSkipped, ValidationStateInvalidated},
)

func (state ValidationState) IsValid() bool {
	return containsState(validationStates, state)
}

func (state ValidationState) IsTerminal() bool {
	switch state {
	case ValidationStatePassed,
		ValidationStateFailed,
		ValidationStateWaived,
		ValidationStateSkipped,
		ValidationStateCancelled,
		ValidationStateInvalidated:
		return true
	default:
		return false
	}
}

// ValidateValidationTransition validates one validation transition.
func ValidateValidationTransition(from, to ValidationState) error {
	return validateTransition("validation", from, to, ValidationState.IsValid, validationTransitions)
}

// GraphRevisionState describes creation and validity of an immutable graph revision.
type GraphRevisionState string

const (
	GraphRevisionStateDraft       GraphRevisionState = "draft"
	GraphRevisionStateValidating  GraphRevisionState = "validating"
	GraphRevisionStateCommitted   GraphRevisionState = "committed"
	GraphRevisionStateRejected    GraphRevisionState = "rejected"
	GraphRevisionStateSuperseded  GraphRevisionState = "superseded"
	GraphRevisionStateInvalidated GraphRevisionState = "invalidated"
)

var graphRevisionStates = []GraphRevisionState{
	GraphRevisionStateDraft,
	GraphRevisionStateValidating,
	GraphRevisionStateCommitted,
	GraphRevisionStateRejected,
	GraphRevisionStateSuperseded,
	GraphRevisionStateInvalidated,
}

var graphRevisionTransitions = transitions(
	[2]GraphRevisionState{GraphRevisionStateDraft, GraphRevisionStateValidating},
	[2]GraphRevisionState{GraphRevisionStateDraft, GraphRevisionStateRejected},
	[2]GraphRevisionState{GraphRevisionStateValidating, GraphRevisionStateCommitted},
	[2]GraphRevisionState{GraphRevisionStateValidating, GraphRevisionStateRejected},
	[2]GraphRevisionState{GraphRevisionStateCommitted, GraphRevisionStateSuperseded},
	[2]GraphRevisionState{GraphRevisionStateCommitted, GraphRevisionStateInvalidated},
)

func (state GraphRevisionState) IsValid() bool {
	return containsState(graphRevisionStates, state)
}

func (state GraphRevisionState) IsTerminal() bool {
	switch state {
	case GraphRevisionStateRejected, GraphRevisionStateSuperseded, GraphRevisionStateInvalidated:
		return true
	default:
		return false
	}
}

// ValidateGraphRevisionTransition validates one graph-revision transition.
func ValidateGraphRevisionTransition(from, to GraphRevisionState) error {
	return validateTransition(
		"graph revision",
		from,
		to,
		GraphRevisionState.IsValid,
		graphRevisionTransitions,
	)
}

// ChangeAcceptanceState describes the user's decision about a reviewed change.
type ChangeAcceptanceState string

const (
	ChangeAcceptanceStatePending         ChangeAcceptanceState = "pending"
	ChangeAcceptanceStateAccepted        ChangeAcceptanceState = "accepted"
	ChangeAcceptanceStateRepairRequested ChangeAcceptanceState = "repair-requested"
	ChangeAcceptanceStateRejected        ChangeAcceptanceState = "rejected"
	ChangeAcceptanceStateRolledBack      ChangeAcceptanceState = "rolled-back"
)

var changeAcceptanceStates = []ChangeAcceptanceState{
	ChangeAcceptanceStatePending,
	ChangeAcceptanceStateAccepted,
	ChangeAcceptanceStateRepairRequested,
	ChangeAcceptanceStateRejected,
	ChangeAcceptanceStateRolledBack,
}

var changeAcceptanceTransitions = transitions(
	[2]ChangeAcceptanceState{ChangeAcceptanceStatePending, ChangeAcceptanceStateAccepted},
	[2]ChangeAcceptanceState{ChangeAcceptanceStatePending, ChangeAcceptanceStateRepairRequested},
	[2]ChangeAcceptanceState{ChangeAcceptanceStatePending, ChangeAcceptanceStateRejected},
	[2]ChangeAcceptanceState{ChangeAcceptanceStateRepairRequested, ChangeAcceptanceStatePending},
	[2]ChangeAcceptanceState{ChangeAcceptanceStateAccepted, ChangeAcceptanceStateRolledBack},
)

func (state ChangeAcceptanceState) IsValid() bool {
	return containsState(changeAcceptanceStates, state)
}

func (state ChangeAcceptanceState) IsTerminal() bool {
	return state == ChangeAcceptanceStateRejected || state == ChangeAcceptanceStateRolledBack
}

// ValidateChangeAcceptanceTransition validates one review-decision transition.
func ValidateChangeAcceptanceTransition(from, to ChangeAcceptanceState) error {
	return validateTransition(
		"change acceptance",
		from,
		to,
		ChangeAcceptanceState.IsValid,
		changeAcceptanceTransitions,
	)
}

func containsState[S comparable](states []S, target S) bool {
	for _, state := range states {
		if state == target {
			return true
		}
	}
	return false
}
