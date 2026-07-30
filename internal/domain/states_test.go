package domain

import (
	"errors"
	"testing"
)

func TestEveryAllowedTransitionPasses(t *testing.T) {
	assertAllowedTransitions(t, "task", taskTransitions, func(from, to TaskState) error {
		change := TaskTransition{From: from, To: to}
		if from == TaskStateAwaitingAuthority && to == TaskStateRunning {
			change.Approval = ApprovalRequestStateGranted
		}
		return ValidateTaskTransition(change)
	})
	assertAllowedTransitions(t, "run", runTransitions, ValidateRunTransition)
	assertAllowedTransitions(t, "command execution", commandExecutionTransitions, func(from, to CommandExecutionState) error {
		change := CommandExecutionTransition{From: from, To: to}
		if from == CommandExecutionStateAwaitingAuthority && to == CommandExecutionStateAuthorized {
			change.Approval = ApprovalRequestStateGranted
		}
		return ValidateCommandExecutionTransition(change)
	})
	assertAllowedTransitions(t, "approval request", approvalRequestTransitions, ValidateApprovalRequestTransition)
	assertAllowedTransitions(t, "checkpoint", checkpointTransitions, ValidateCheckpointTransition)
	assertAllowedTransitions(t, "validation", validationTransitions, ValidateValidationTransition)
	assertAllowedTransitions(t, "graph revision", graphRevisionTransitions, ValidateGraphRevisionTransition)
	assertAllowedTransitions(t, "change acceptance", changeAcceptanceTransitions, ValidateChangeAcceptanceTransition)
}

func TestRepresentativeForbiddenTransitionsReturnTypedErrors(t *testing.T) {
	cases := []struct {
		name     string
		validate func() error
	}{
		{name: "task", validate: func() error {
			return ValidateTaskTransition(TaskTransition{From: TaskStateDraft, To: TaskStateCompleted})
		}},
		{name: "run", validate: func() error {
			return ValidateRunTransition(RunStatePending, RunStateCompleted)
		}},
		{name: "command", validate: func() error {
			return ValidateCommandExecutionTransition(CommandExecutionTransition{
				From: CommandExecutionStatePending,
				To:   CommandExecutionStateSucceeded,
			})
		}},
		{name: "approval", validate: func() error {
			return ValidateApprovalRequestTransition(ApprovalRequestStateGranted, ApprovalRequestStateDenied)
		}},
		{name: "checkpoint", validate: func() error {
			return ValidateCheckpointTransition(CheckpointStateReady, CheckpointStateCreating)
		}},
		{name: "validation", validate: func() error {
			return ValidateValidationTransition(ValidationStatePassed, ValidationStateRunning)
		}},
		{name: "graph revision", validate: func() error {
			return ValidateGraphRevisionTransition(GraphRevisionStateCommitted, GraphRevisionStateDraft)
		}},
		{name: "change acceptance", validate: func() error {
			return ValidateChangeAcceptanceTransition(ChangeAcceptanceStateRejected, ChangeAcceptanceStateAccepted)
		}},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := test.validate()
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("error = %v, want ErrInvalidTransition", err)
			}
			var typed *TransitionError
			if !errors.As(err, &typed) || typed.Error() == "" {
				t.Fatalf("error = %#v, want user-presentable TransitionError", err)
			}
		})
	}
}

func TestUnknownStatesReturnTypedErrors(t *testing.T) {
	err := ValidateRunTransition(RunState("invented"), RunStateRunning)
	if !errors.Is(err, ErrUnknownState) {
		t.Fatalf("error = %v, want ErrUnknownState", err)
	}
	var typed *TransitionError
	if !errors.As(err, &typed) {
		t.Fatalf("error = %T, want *TransitionError", err)
	}
}

func TestTerminalTaskStatesCannotReturnToRunning(t *testing.T) {
	for _, state := range AllTaskStates() {
		if !state.IsTerminal() {
			continue
		}
		err := ValidateTaskTransition(TaskTransition{From: state, To: TaskStateRunning})
		if !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s -> running error = %v, want ErrInvalidTransition", state, err)
		}
	}
}

func TestAwaitingAuthorityRequiresGrantedApproval(t *testing.T) {
	for _, approval := range []ApprovalRequestState{
		"",
		ApprovalRequestStatePending,
		ApprovalRequestStateDenied,
		ApprovalRequestStateExpired,
		ApprovalRequestStateCancelled,
	} {
		err := ValidateTaskTransition(TaskTransition{
			From:     TaskStateAwaitingAuthority,
			To:       TaskStateRunning,
			Approval: approval,
		})
		if !errors.Is(err, ErrApprovalRequired) {
			t.Errorf("approval %q error = %v, want ErrApprovalRequired", approval, err)
		}
	}
	if err := ValidateTaskTransition(TaskTransition{
		From:     TaskStateAwaitingAuthority,
		To:       TaskStateRunning,
		Approval: ApprovalRequestStateGranted,
	}); err != nil {
		t.Fatalf("granted task transition: %v", err)
	}

	if err := ValidateCommandExecutionTransition(CommandExecutionTransition{
		From: CommandExecutionStateAwaitingAuthority,
		To:   CommandExecutionStateAuthorized,
	}); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("command error = %v, want ErrApprovalRequired", err)
	}
}

func TestTaskTerminalAndRecoverableClassifications(t *testing.T) {
	for _, state := range []TaskState{
		TaskStateCompleted,
		TaskStateCancelled,
		TaskStateRolledBack,
	} {
		if !state.IsTerminal() {
			t.Errorf("%s is not terminal", state)
		}
	}
	for _, state := range []TaskState{
		TaskStatePaused,
		TaskStateAwaitingAuthority,
		TaskStateFailed,
		TaskStateRecoveryRequired,
	} {
		if !state.IsRecoverable() {
			t.Errorf("%s is not recoverable", state)
		}
	}
}

func assertAllowedTransitions[S ~string](
	t *testing.T,
	name string,
	table transitionTable[S],
	validate func(S, S) error,
) {
	t.Helper()
	for from, destinations := range table {
		for to := range destinations {
			if err := validate(from, to); err != nil {
				t.Errorf("%s %q -> %q: %v", name, from, to, err)
			}
		}
	}
}
