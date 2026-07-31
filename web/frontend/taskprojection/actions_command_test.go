package taskprojection_test

import (
	"errors"
	"reflect"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

func TestAvailableTaskActionsExercisesEveryMatrixRow(t *testing.T) {
	ids := fixtureIDs(t)
	expected := map[domain.TaskState][]taskprojection.ActionKind{
		domain.TaskStateDraft:                {taskprojection.ActionSend, taskprojection.ActionChangePolicy, taskprojection.ActionChangeBudget},
		domain.TaskStateForecasting:          {taskprojection.ActionStop},
		domain.TaskStateAwaitingPlanApproval: {taskprojection.ActionApprovePlan, taskprojection.ActionRequestChange, taskprojection.ActionStop},
		domain.TaskStateReady:                {taskprojection.ActionStart, taskprojection.ActionChangeBudget, taskprojection.ActionStop},
		domain.TaskStateRunning:              {taskprojection.ActionPause, taskprojection.ActionStop, taskprojection.ActionInspectGraph},
		domain.TaskStateAwaitingAuthority:    {taskprojection.ActionAllowOnce, taskprojection.ActionAllowForTask, taskprojection.ActionDeny, taskprojection.ActionStop},
		domain.TaskStatePaused:               {taskprojection.ActionResume, taskprojection.ActionChangeBudget, taskprojection.ActionReview, taskprojection.ActionStop},
		domain.TaskStateValidating:           {taskprojection.ActionPause, taskprojection.ActionStop, taskprojection.ActionInspectChecks},
		domain.TaskStateAwaitingReview:       {taskprojection.ActionReview, taskprojection.ActionAccept, taskprojection.ActionRepair, taskprojection.ActionReject, taskprojection.ActionRollback},
		domain.TaskStateRecoveryRequired:     {taskprojection.ActionSafeResume, taskprojection.ActionReconcile, taskprojection.ActionPreservePatch, taskprojection.ActionAbandon},
		domain.TaskStateCompleted:            {taskprojection.ActionInspectEvidence, taskprojection.ActionStartRelatedTask},
		domain.TaskStateFailed:               {taskprojection.ActionInspect, taskprojection.ActionRepair, taskprojection.ActionPreservePatch},
		domain.TaskStateCancelled:            {taskprojection.ActionInspect, taskprojection.ActionPreservePatch, taskprojection.ActionNewAttempt},
		domain.TaskStateRolledBack:           {taskprojection.ActionResumeNewPlan, taskprojection.ActionFinish},
	}
	states := domain.AllTaskStates()
	if len(states) != len(expected) {
		t.Fatalf("declared states=%d matrix rows=%d", len(states), len(expected))
	}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			task := actionableTask(ids, state)
			actions := taskprojection.AvailableTaskActions(task, taskprojection.ConnectionLive)
			kinds := make([]taskprojection.ActionKind, 0, len(actions))
			for _, action := range actions {
				kinds = append(kinds, action.Kind)
				if !action.Enabled && action.Reason == "" {
					t.Fatalf("disabled action %s has no explanation", action.Kind)
				}
			}
			if !reflect.DeepEqual(kinds, expected[state]) {
				t.Fatalf("actions=%v want=%v", kinds, expected[state])
			}
		})
	}
}

func TestAvailableTaskActionsCoversEveryConnectionCertainty(t *testing.T) {
	ids := fixtureIDs(t)
	task := actionableTask(ids, domain.TaskStateRunning)
	connections := taskprojection.AllConnectionProjections()
	if len(connections) != 7 {
		t.Fatalf("connection states=%d, want 7", len(connections))
	}
	for _, connection := range connections {
		actions := taskprojection.AvailableTaskActions(task, connection)
		pause := actionByKind(t, actions, taskprojection.ActionPause)
		stop := actionByKind(t, actions, taskprojection.ActionStop)
		inspect := actionByKind(t, actions, taskprojection.ActionInspectGraph)
		if connection == taskprojection.ConnectionLive {
			if !pause.Enabled || !stop.Enabled || !inspect.Enabled {
				t.Fatalf("live actions=%#v", actions)
			}
			continue
		}
		if pause.Enabled || stop.Enabled || pause.Reason == "" || stop.Reason == "" {
			t.Fatalf("%s mutation decisions=%#v", connection, actions)
		}
		if !inspect.Enabled {
			t.Fatalf("%s disabled read-only graph inspection: %#v", connection, inspect)
		}
	}
}

func TestActionMatrixAppliesPolicyPendingApprovalReviewAndRecoveryConstraints(t *testing.T) {
	ids := fixtureIDs(t)
	running := actionableTask(ids, domain.TaskStateRunning)
	running.Policy = taskprojection.ActionPolicy{
		Denied: []taskprojection.ActionKind{taskprojection.ActionPause}, SafeReason: "Pause is not safe in this phase",
	}
	pause := actionByKind(t, taskprojection.AvailableTaskActions(running, taskprojection.ConnectionLive), taskprojection.ActionPause)
	if pause.Enabled || pause.Reason != running.Policy.SafeReason {
		t.Fatalf("policy decision=%#v", pause)
	}
	key, _ := taskprojection.ParseCommandKey("command-key-0000000001")
	running.PendingCommand = taskprojection.CommandState{Action: taskprojection.ActionStop, Key: key, Status: taskprojection.CommandDisconnected}
	stop := actionByKind(t, taskprojection.AvailableTaskActions(running, taskprojection.ConnectionLive), taskprojection.ActionStop)
	inspect := actionByKind(t, taskprojection.AvailableTaskActions(running, taskprojection.ConnectionLive), taskprojection.ActionInspectGraph)
	if stop.Enabled || stop.Reason == "" || !inspect.Enabled {
		t.Fatalf("pending command decisions stop=%#v inspect=%#v", stop, inspect)
	}

	authority := actionableTask(ids, domain.TaskStateAwaitingAuthority)
	authority.Approval.State = domain.ApprovalRequestStateDenied
	allow := actionByKind(t, taskprojection.AvailableTaskActions(authority, taskprojection.ConnectionLive), taskprojection.ActionAllowOnce)
	if allow.Enabled || allow.Reason == "" {
		t.Fatalf("resolved approval action=%#v", allow)
	}

	review := actionableTask(ids, domain.TaskStateAwaitingReview)
	review.Acceptance.Bindings.Graph++
	accept := actionByKind(t, taskprojection.AvailableTaskActions(review, taskprojection.ConnectionLive), taskprojection.ActionAccept)
	repair := actionByKind(t, taskprojection.AvailableTaskActions(review, taskprojection.ConnectionLive), taskprojection.ActionRepair)
	if accept.Enabled || accept.Reason == "" || !repair.Enabled {
		t.Fatalf("stale review decisions accept=%#v repair=%#v", accept, repair)
	}

	recovery := actionableTask(ids, domain.TaskStateRecoveryRequired)
	recovery.Recovery = taskprojection.RecoveryAmbiguousOutcome
	if actionByKind(t, taskprojection.AvailableTaskActions(recovery, taskprojection.ConnectionLive), taskprojection.ActionSafeResume).Enabled {
		t.Fatal("ambiguous recovery enabled safe resume")
	}
	if !actionByKind(t, taskprojection.AvailableTaskActions(recovery, taskprojection.ConnectionLive), taskprojection.ActionPreservePatch).Enabled {
		t.Fatal("ambiguous recovery disabled patch preservation")
	}
}

func TestRequiredFailedValidationNeedsAcknowledgementBeforeAcceptance(t *testing.T) {
	ids := fixtureIDs(t)
	task := actionableTask(ids, domain.TaskStateAwaitingReview)
	task.Validation.State = domain.ValidationStateFailed

	accept := actionByKind(t, taskprojection.AvailableTaskActions(task, taskprojection.ConnectionLive), taskprojection.ActionAccept)
	if accept.Enabled || accept.Reason == "" {
		t.Fatalf("unacknowledged required failure allowed acceptance: %#v", accept)
	}

	task.Validation.Acknowledged = true
	accept = actionByKind(t, taskprojection.AvailableTaskActions(task, taskprojection.ConnectionLive), taskprojection.ActionAccept)
	if !accept.Enabled {
		t.Fatalf("acknowledged required failure blocked acceptance: %#v", accept)
	}
}

func TestCommandRetainsOneKeyThroughEverySettlement(t *testing.T) {
	key, err := taskprojection.ParseCommandKey("command-key-0000000001")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := taskprojection.ParseCommandKey("command-key-0000000002")
	begin, err := taskprojection.BeginCommand(taskprojection.CommandState{}, taskprojection.ActionPause, key, 5)
	if err != nil || !begin.OwnsKey() || begin.Status != taskprojection.CommandBusy {
		t.Fatalf("begin=%#v error=%v", begin, err)
	}
	if _, err := taskprojection.BeginCommand(begin, taskprojection.ActionStop, other, 5); !errors.Is(err, taskprojection.ErrCommandInFlight) {
		t.Fatalf("second command error=%v", err)
	}
	if got, err := taskprojection.SettleCommand(begin, taskprojection.CommandSettlement{
		Key: other, Kind: taskprojection.SettlementCommitted, AuthoritativeRevision: 6,
	}); !errors.Is(err, taskprojection.ErrCommandKeyMismatch) || got != begin {
		t.Fatalf("mismatched settlement=%#v error=%v", got, err)
	}

	tests := []struct {
		name       string
		settlement taskprojection.CommandSettlement
		status     taskprojection.CommandStatus
		owns       bool
		refresh    bool
		uncertain  bool
	}{
		{name: "committed", settlement: taskprojection.CommandSettlement{Key: key, Kind: taskprojection.SettlementCommitted, AuthoritativeRevision: 6}, status: taskprojection.CommandCommitted},
		{name: "stale", settlement: taskprojection.CommandSettlement{Key: key, Kind: taskprojection.SettlementStale, AuthoritativeRevision: 7, ChangedEntity: "task"}, status: taskprojection.CommandStale, owns: true, refresh: true},
		{name: "denied", settlement: taskprojection.CommandSettlement{Key: key, Kind: taskprojection.SettlementDenied, SafeExplanation: "Not authorized"}, status: taskprojection.CommandDenied, owns: true},
		{name: "disconnected", settlement: taskprojection.CommandSettlement{Key: key, Kind: taskprojection.SettlementDisconnected, SafeExplanation: "Settlement unknown"}, status: taskprojection.CommandDisconnected, owns: true, uncertain: true},
		{name: "failed", settlement: taskprojection.CommandSettlement{Key: key, Kind: taskprojection.SettlementFailed, SafeExplanation: "Command failed"}, status: taskprojection.CommandFailed, owns: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, settleErr := taskprojection.SettleCommand(begin, test.settlement)
			if settleErr != nil {
				t.Fatal(settleErr)
			}
			if got.Key != key || got.Status != test.status || got.OwnsKey() != test.owns ||
				got.RefreshRequested != test.refresh || got.SettlementUncertain != test.uncertain {
				t.Fatalf("settled=%#v", got)
			}
			if got.OwnsKey() {
				abandoned, abandonErr := taskprojection.AbandonCommand(got, key)
				if abandonErr != nil || abandoned.Key != "" || abandoned.Status != taskprojection.CommandIdle {
					t.Fatalf("abandoned=%#v error=%v", abandoned, abandonErr)
				}
			}
		})
	}
}

func actionableTask(ids projectionIDs, state domain.TaskState) taskprojection.TaskProjection {
	bindings := taskprojection.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 1}
	return taskprojection.TaskProjection{
		TaskID: ids.task, State: state, Revision: 1, Recovery: taskprojection.RecoverySafeResume,
		Plan: taskprojection.PlanProjection{
			Present: true, Revision: 1, RedactedSummary: "Plan",
			Approval: map[bool]domain.ApprovalRequestState{
				true: domain.ApprovalRequestStateGranted, false: domain.ApprovalRequestStatePending,
			}[state == domain.TaskStateReady],
		},
		Approval: taskprojection.ApprovalProjection{
			Present: true, ID: ids.approval, State: domain.ApprovalRequestStatePending, Revision: 1,
		},
		Validation: taskprojection.ValidationProjection{
			Present: true, ID: ids.validation, State: domain.ValidationStatePassed, Required: true, Revision: 1, DiffRevision: 1,
		},
		Review:     taskprojection.ReviewProjection{Present: true, Revision: 1, Bindings: bindings},
		Acceptance: taskprojection.AcceptanceProjection{Present: true, State: domain.ChangeAcceptanceStatePending, Revision: 1, Bindings: bindings},
	}
}

func actionByKind(t *testing.T, actions []taskprojection.TaskAction, kind taskprojection.ActionKind) taskprojection.TaskAction {
	t.Helper()
	for _, action := range actions {
		if action.Kind == kind {
			return action
		}
	}
	t.Fatalf("action %s absent from %#v", kind, actions)
	return taskprojection.TaskAction{}
}
