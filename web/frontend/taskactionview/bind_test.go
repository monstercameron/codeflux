package taskactionview_test

import (
	"slices"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/taskactionview"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

func TestBindRendersEveryAuthoritativeTaskMatrixRow(t *testing.T) {
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
	invoked := map[taskprojection.ActionKind]int{}
	record := func(action taskprojection.ActionKind) func() {
		return func() { invoked[action]++ }
	}
	controls := &taskcontrols.Props{
		OnPause: record(taskprojection.ActionPause), OnResume: record(taskprojection.ActionResume),
		OnStop: record(taskprojection.ActionStop), OnBudgetAdjust: record(taskprojection.ActionChangeBudget),
		OnSafeResume: record(taskprojection.ActionSafeResume), OnReconcile: record(taskprojection.ActionReconcile),
		OnPreservePatch: record(taskprojection.ActionPreservePatch),
	}
	callbacks := taskactionview.Callbacks{
		InspectGraph: record(taskprojection.ActionInspectGraph),
		Review:       record(taskprojection.ActionReview),
	}
	for _, taskState := range matrixStates() {
		t.Run(string(taskState), func(t *testing.T) {
			task := authoritativeTask(t, taskState, taskprojection.RecoveryNeedsReconcile)
			props := taskactionview.Bind(
				composer.Props{}, task, taskprojection.ConnectionLive, controls, callbacks,
			)
			wantKinds := expected[taskState]
			want := make([]composer.TaskAction, len(wantKinds))
			for index, kind := range wantKinds {
				want[index] = composer.TaskAction(kind)
			}
			if !slices.Equal(props.View.Task.Actions, want) {
				t.Fatalf("actions=%v want=%v", props.View.Task.Actions, want)
			}
			if props.View.Task.PrimaryMessage != composer.AvailableTaskActions(taskState).PrimaryMessage {
				t.Fatalf("primary message=%q", props.View.Task.PrimaryMessage)
			}
			for _, action := range props.View.Task.Actions {
				reason := strings.TrimSpace(props.View.Task.DisabledReason(action))
				if reason == "" {
					before := invoked[taskprojection.ActionKind(action)]
					props.OnTaskAction(action)
					if invoked[taskprojection.ActionKind(action)] != before+1 {
						t.Fatalf("enabled action %s did not invoke its authoritative callback", action)
					}
					continue
				}
				before := invoked[taskprojection.ActionKind(action)]
				props.OnTaskAction(action)
				if invoked[taskprojection.ActionKind(action)] != before {
					t.Fatalf("disabled action %s invoked despite reason %q", action, reason)
				}
			}
		})
	}
}

func TestUnavailableReasonIsSpecificForEveryUnimplementedMatrixAction(t *testing.T) {
	for _, action := range []taskprojection.ActionKind{
		taskprojection.ActionApprovePlan, taskprojection.ActionRequestChange,
		taskprojection.ActionStart, taskprojection.ActionAllowOnce, taskprojection.ActionAllowForTask,
		taskprojection.ActionDeny, taskprojection.ActionInspectChecks, taskprojection.ActionAccept,
		taskprojection.ActionRepair, taskprojection.ActionReject, taskprojection.ActionRollback,
		taskprojection.ActionAbandon, taskprojection.ActionInspectEvidence,
		taskprojection.ActionStartRelatedTask, taskprojection.ActionInspect,
		taskprojection.ActionNewAttempt, taskprojection.ActionResumeNewPlan, taskprojection.ActionFinish,
	} {
		reason := taskactionview.UnavailableReason(action)
		if strings.TrimSpace(reason) == "" || strings.Contains(reason, "no authoritative implementation") {
			t.Errorf("%s reason is not specific: %q", action, reason)
		}
	}
}

func matrixStates() []domain.TaskState {
	return []domain.TaskState{
		domain.TaskStateDraft, domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval,
		domain.TaskStateReady, domain.TaskStateRunning, domain.TaskStateAwaitingAuthority,
		domain.TaskStatePaused, domain.TaskStateValidating, domain.TaskStateAwaitingReview,
		domain.TaskStateRecoveryRequired, domain.TaskStateCompleted, domain.TaskStateFailed,
		domain.TaskStateCancelled, domain.TaskStateRolledBack,
	}
}

func authoritativeTask(
	t *testing.T,
	state domain.TaskState,
	recovery taskprojection.RecoveryClassification,
) taskprojection.TaskProjection {
	t.Helper()
	taskID, err := domain.ParseTaskID("tsk_018f0123-4567-789a-8bcd-ef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	approvalID, err := domain.ParseApprovalID("apr_018f0123-4567-789a-8bcd-ef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	validationID, err := domain.ParseValidationID("val_018f0123-4567-789a-8bcd-ef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	bindings := taskprojection.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 1}
	projection, err := taskprojection.ApplySnapshot(taskprojection.Snapshot{Projection: taskprojection.TaskProjection{
		TaskID: taskID, State: state, Revision: 1, Recovery: recovery,
		Plan: taskprojection.PlanProjection{
			Present: true, Revision: 1, RedactedSummary: "Authoritative plan",
			Approval: map[bool]domain.ApprovalRequestState{
				true: domain.ApprovalRequestStateGranted, false: domain.ApprovalRequestStatePending,
			}[state == domain.TaskStateReady],
		},
		Approval: taskprojection.ApprovalProjection{
			Present: true, ID: approvalID, State: domain.ApprovalRequestStatePending, Revision: 1,
		},
		Validation: taskprojection.ValidationProjection{
			Present: true, ID: validationID, State: domain.ValidationStatePassed,
			Required: true, Revision: 1, DiffRevision: 1,
		},
		Review:     taskprojection.ReviewProjection{Present: true, Revision: 1, Bindings: bindings},
		Acceptance: taskprojection.AcceptanceProjection{Present: true, State: domain.ChangeAcceptanceStatePending, Revision: 1, Bindings: bindings},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return projection
}
