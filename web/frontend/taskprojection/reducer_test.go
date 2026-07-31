package taskprojection_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

func TestTaskSnapshotAllowsOnlyInitialDraftAtRevisionZero(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	initial, err := taskprojection.ApplySnapshot(taskprojection.Snapshot{
		Projection: taskprojection.TaskProjection{
			TaskID: taskID, State: domain.TaskStateDraft,
		},
	})
	if err != nil || initial.Revision != 0 || initial.State != domain.TaskStateDraft {
		t.Fatalf("initial draft snapshot = %#v, %v", initial, err)
	}
	_, err = taskprojection.ApplySnapshot(taskprojection.Snapshot{
		Projection: taskprojection.TaskProjection{
			TaskID: taskID, State: domain.TaskStateRunning,
		},
	})
	if err == nil {
		t.Fatal("non-draft revision-zero snapshot was accepted")
	}
}

const projectionUUID = "019fb8c8-670d-796c-8569-7d3252348b52"

type projectionIDs struct {
	task       domain.TaskID
	approval   domain.ApprovalID
	checkpoint domain.CheckpointID
	validation domain.ValidationID
}

func fixtureIDs(t *testing.T) projectionIDs {
	t.Helper()
	task, err := domain.ParseTaskID("tsk_" + projectionUUID)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := domain.ParseApprovalID("apr_" + projectionUUID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := domain.ParseCheckpointID("ckp_" + projectionUUID)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := domain.ParseValidationID("val_" + projectionUUID)
	if err != nil {
		t.Fatal(err)
	}
	return projectionIDs{task: task, approval: approval, checkpoint: checkpoint, validation: validation}
}

func TestProjectMatchesRecordedAuthoritativeFixture(t *testing.T) {
	ids := fixtureIDs(t)
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	money := func(units int64) domain.Money {
		value, valueErr := domain.NewMoney(usd, units)
		if valueErr != nil {
			t.Fatal(valueErr)
		}
		return value
	}
	bindings := taskprojection.RevisionBindings{Diff: 1, Plan: 2, Validation: 2, Evidence: 1, Graph: 1}
	checkpointAt := time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC)
	events := []taskprojection.ProjectionEvent{
		{Sequence: 11, Kind: taskprojection.EventPlanRevision, Plan: taskprojection.PlanRevisionEvent{Revision: 1, RedactedSummary: "Initial plan"}},
		{Sequence: 12, Kind: taskprojection.EventTaskTransition, TaskTransition: taskprojection.TaskTransitionEvent{From: domain.TaskStateDraft, To: domain.TaskStateForecasting, Revision: 2}},
		{Sequence: 13, Kind: taskprojection.EventTaskTransition, TaskTransition: taskprojection.TaskTransitionEvent{From: domain.TaskStateForecasting, To: domain.TaskStateAwaitingPlanApproval, Revision: 3}},
		{Sequence: 14, Kind: taskprojection.EventPlanRevision, Plan: taskprojection.PlanRevisionEvent{Revision: 2, RedactedSummary: "Revised plan"}},
		{Sequence: 15, Kind: taskprojection.EventTaskTransition, TaskTransition: taskprojection.TaskTransitionEvent{From: domain.TaskStateAwaitingPlanApproval, To: domain.TaskStateReady, Revision: 4, Approval: domain.ApprovalRequestStateGranted}},
		{Sequence: 16, Kind: taskprojection.EventBudgetUpdate, Budget: taskprojection.BudgetEvent{Revision: 1, HardLimit: money(5000), Reserved: money(1200), Actual: money(800)}},
		{Sequence: 17, Kind: taskprojection.EventGraphPatch, Graph: taskprojection.GraphPatchEvent{BaseRevision: 0, Revision: 1}},
		{Sequence: 18, Kind: taskprojection.EventTaskTransition, TaskTransition: taskprojection.TaskTransitionEvent{From: domain.TaskStateReady, To: domain.TaskStateRunning, Revision: 5}},
		{Sequence: 19, Kind: taskprojection.EventToolUpdate, Tool: taskprojection.ToolEvent{ExecutionID: "tool-1", CommandName: "go test", State: domain.CommandExecutionStateRunning, Revision: 1}},
		{Sequence: 20, Kind: taskprojection.EventToolUpdate, Tool: taskprojection.ToolEvent{ExecutionID: "tool-1", CommandName: "go test", State: domain.CommandExecutionStateSucceeded, Revision: 2, SafeSummary: "passed"}},
		{Sequence: 21, Kind: taskprojection.EventApprovalUpdate, Approval: taskprojection.ApprovalEvent{ID: ids.approval, State: domain.ApprovalRequestStatePending, Scope: "repository", Revision: 1}},
		{Sequence: 22, Kind: taskprojection.EventTaskTransition, TaskTransition: taskprojection.TaskTransitionEvent{From: domain.TaskStateRunning, To: domain.TaskStateAwaitingAuthority, Revision: 6}},
		{Sequence: 23, Kind: taskprojection.EventApprovalUpdate, Approval: taskprojection.ApprovalEvent{ID: ids.approval, State: domain.ApprovalRequestStateGranted, Scope: "repository", Revision: 2}},
		{Sequence: 24, Kind: taskprojection.EventTaskTransition, TaskTransition: taskprojection.TaskTransitionEvent{From: domain.TaskStateAwaitingAuthority, To: domain.TaskStateRunning, Revision: 7, Approval: domain.ApprovalRequestStateGranted}},
		{Sequence: 25, Kind: taskprojection.EventCheckpoint, Checkpoint: taskprojection.CheckpointEvent{ID: ids.checkpoint, TaskRevision: 7, PlanStep: "test", CreatedAt: checkpointAt, Revision: 1}},
		{Sequence: 26, Kind: taskprojection.EventTaskTransition, TaskTransition: taskprojection.TaskTransitionEvent{From: domain.TaskStateRunning, To: domain.TaskStateValidating, Revision: 8}},
		{Sequence: 27, Kind: taskprojection.EventValidationUpdate, Validation: taskprojection.ValidationEvent{ID: ids.validation, State: domain.ValidationStateRunning, Required: true, Revision: 1, DiffRevision: 1}},
		{Sequence: 28, Kind: taskprojection.EventValidationUpdate, Validation: taskprojection.ValidationEvent{ID: ids.validation, State: domain.ValidationStatePassed, Required: true, SafeSummary: "all required checks passed", Revision: 2, DiffRevision: 1}},
		{Sequence: 29, Kind: taskprojection.EventReviewRevision, Review: taskprojection.ReviewRevisionEvent{Revision: 1, Bindings: bindings}},
		{Sequence: 30, Kind: taskprojection.EventAcceptanceUpdate, Acceptance: taskprojection.AcceptanceEvent{State: domain.ChangeAcceptanceStatePending, Revision: 1, Bindings: bindings}},
		{Sequence: 31, Kind: taskprojection.EventTaskTransition, TaskTransition: taskprojection.TaskTransitionEvent{From: domain.TaskStateValidating, To: domain.TaskStateAwaitingReview, Revision: 9}},
		{Sequence: 32, Kind: taskprojection.EventAcceptanceUpdate, Acceptance: taskprojection.AcceptanceEvent{State: domain.ChangeAcceptanceStateAccepted, Revision: 2, Bindings: bindings}},
		{Sequence: 33, Kind: taskprojection.EventTaskTransition, TaskTransition: taskprojection.TaskTransitionEvent{From: domain.TaskStateAwaitingReview, To: domain.TaskStateCompleted, Revision: 10}},
	}
	got, err := taskprojection.Project(taskprojection.Snapshot{Projection: taskprojection.TaskProjection{
		TaskID: ids.task, State: domain.TaskStateDraft, Revision: 1, LastSequence: 10,
	}}, events)
	if err != nil {
		t.Fatal(err)
	}
	want := taskprojection.TaskProjection{
		TaskID: ids.task, State: domain.TaskStateCompleted, Revision: 10, LastSequence: 33,
		Plan: taskprojection.PlanProjection{
			Present: true, Revision: 2, RedactedSummary: "Revised plan",
			Approval: domain.ApprovalRequestStateGranted, PriorRevisions: []uint64{1},
		},
		Tool: taskprojection.ToolProjection{
			Present: true, ExecutionID: "tool-1", CommandName: "go test",
			State: domain.CommandExecutionStateSucceeded, Revision: 2, SafeSummary: "passed",
		},
		Approval: taskprojection.ApprovalProjection{
			Present: true, ID: ids.approval, State: domain.ApprovalRequestStateGranted,
			Scope: "repository", Revision: 2,
		},
		Checkpoint: taskprojection.CheckpointProjection{
			Present: true, ID: ids.checkpoint, TaskRevision: 7,
			PlanStep: "test", CreatedAt: checkpointAt, Revision: 1,
		},
		Validation: taskprojection.ValidationProjection{
			Present: true, ID: ids.validation, State: domain.ValidationStatePassed,
			Required: true, SafeSummary: "all required checks passed", Revision: 2, DiffRevision: 1,
		},
		Review: taskprojection.ReviewProjection{Present: true, Revision: 1, Bindings: bindings},
		Acceptance: taskprojection.AcceptanceProjection{
			Present: true, State: domain.ChangeAcceptanceStateAccepted, Revision: 2, Bindings: bindings,
		},
		Budget: taskprojection.BudgetProjection{
			Present: true, Revision: 1, HardLimit: money(5000), Reserved: money(1200), Actual: money(800),
		},
		Graph:          taskprojection.GraphProjection{Present: true, Revision: 1},
		Recovery:       taskprojection.RecoveryNone,
		PendingCommand: taskprojection.CommandState{Status: taskprojection.CommandIdle},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestProjectionInconsistencyRequestsFreshSnapshotWithoutRawContent(t *testing.T) {
	ids := fixtureIDs(t)
	base := taskprojection.TaskProjection{
		TaskID: ids.task, State: domain.TaskStateDraft, Revision: 1, LastSequence: 4,
		Recovery: taskprojection.RecoveryNone,
	}
	event := taskprojection.ProjectionEvent{
		Sequence: 5, Kind: taskprojection.EventTaskTransition,
		TaskTransition: taskprojection.TaskTransitionEvent{
			From: domain.TaskStateDraft, To: domain.TaskStateCompleted, Revision: 2,
		},
	}
	got, err := taskprojection.ApplyEvent(base, event)
	if err == nil {
		t.Fatal("impossible transition succeeded")
	}
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("failed transition mutated projection: %#v", got)
	}
	repair, diagnostic, ok := taskprojection.RepairSignal(err, base.LastSequence)
	if !ok || !repair.Required || repair.AfterSequence != 4 ||
		diagnostic.Code != "task-transition" || diagnostic.Entity != "task" {
		t.Fatalf("repair=%#v diagnostic=%#v ok=%t", repair, diagnostic, ok)
	}
	if strings.Contains(err.Error(), "private requirement text") {
		t.Fatalf("diagnostic exposed raw task content: %v", err)
	}

	_, gapErr := taskprojection.ApplyEvent(base, taskprojection.ProjectionEvent{
		Sequence: 7, Kind: taskprojection.EventRecoveryUpdate, Recovery: taskprojection.RecoverySafeResume,
	})
	gapRepair, _, gapOK := taskprojection.RepairSignal(gapErr, base.LastSequence)
	if !gapOK || gapRepair.ReasonCode != "event-sequence" {
		t.Fatalf("gap repair=%#v error=%v", gapRepair, gapErr)
	}

	_, graphErr := taskprojection.ApplyGraphPatch(base, taskprojection.GraphPatchEvent{BaseRevision: 3, Revision: 4})
	graphRepair, graphDiagnostic, graphOK := taskprojection.RepairSignal(graphErr, base.LastSequence)
	if !graphOK || graphRepair.Entity != "graph" || graphDiagnostic.Code != "graph-revision" {
		t.Fatalf("graph repair=%#v diagnostic=%#v error=%v", graphRepair, graphDiagnostic, graphErr)
	}
}

func TestReducersDoNotMutateCallerOwnedSlices(t *testing.T) {
	ids := fixtureIDs(t)
	prior := []uint64{1}
	denied := []taskprojection.ActionKind{taskprojection.ActionRollback}
	base := taskprojection.TaskProjection{
		TaskID: ids.task, State: domain.TaskStateAwaitingPlanApproval, Revision: 3,
		Plan: taskprojection.PlanProjection{
			Present: true, Revision: 2, RedactedSummary: "second",
			Approval: domain.ApprovalRequestStatePending, PriorRevisions: prior,
		},
		Policy: taskprojection.ActionPolicy{Denied: denied}, Recovery: taskprojection.RecoveryNone,
	}
	next, err := taskprojection.ApplyPlanRevision(base, taskprojection.PlanRevisionEvent{
		Revision: 3, RedactedSummary: "third",
	})
	if err != nil {
		t.Fatal(err)
	}
	next.Plan.PriorRevisions[0] = 99
	next.Policy.Denied[0] = taskprojection.ActionStop
	if prior[0] != 1 || denied[0] != taskprojection.ActionRollback || base.Plan.PriorRevisions[0] != 1 {
		t.Fatalf("caller-owned state mutated: prior=%v denied=%v base=%#v", prior, denied, base)
	}
}

func TestReducersRejectReusedTerminalIdentitiesAndRegressedCheckpoint(t *testing.T) {
	ids := fixtureIDs(t)
	base := taskprojection.TaskProjection{
		TaskID: ids.task, State: domain.TaskStateRunning, Revision: 4,
		Tool: taskprojection.ToolProjection{
			Present: true, ExecutionID: "tool-1", CommandName: "go test",
			State: domain.CommandExecutionStateSucceeded, Revision: 2,
		},
		Approval: taskprojection.ApprovalProjection{
			Present: true, ID: ids.approval, State: domain.ApprovalRequestStateGranted, Revision: 2,
		},
		Checkpoint: taskprojection.CheckpointProjection{
			Present: true, ID: ids.checkpoint, TaskRevision: 4,
			CreatedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC), Revision: 1,
		},
		Recovery: taskprojection.RecoveryNone,
	}

	if _, err := taskprojection.ApplyToolUpdate(base, taskprojection.ToolEvent{
		ExecutionID: "tool-1", CommandName: "go test", State: domain.CommandExecutionStateRunning, Revision: 3,
	}); err == nil {
		t.Fatal("terminal tool identity was reused")
	}
	if _, err := taskprojection.ApplyApprovalUpdate(base, taskprojection.ApprovalEvent{
		ID: ids.approval, State: domain.ApprovalRequestStatePending, Revision: 3,
	}); err == nil {
		t.Fatal("terminal approval identity was reused")
	}
	if _, err := taskprojection.ApplyCheckpointUpdate(base, taskprojection.CheckpointEvent{
		ID: ids.checkpoint, TaskRevision: 3,
		CreatedAt: time.Date(2026, 7, 31, 12, 1, 0, 0, time.UTC), Revision: 2,
	}); err == nil {
		t.Fatal("checkpoint task revision regressed")
	}
}

func TestValidationIdentityAndInvalidationTransitions(t *testing.T) {
	ids := fixtureIDs(t)
	base := taskprojection.TaskProjection{
		TaskID: ids.task, State: domain.TaskStateValidating, Revision: 3,
		Validation: taskprojection.ValidationProjection{
			Present: true, ID: ids.validation, State: domain.ValidationStatePassed,
			Required: true, Revision: 2, DiffRevision: 1,
		},
		Recovery: taskprojection.RecoveryNone,
	}

	invalidated, err := taskprojection.ApplyValidationUpdate(base, taskprojection.ValidationEvent{
		ID: ids.validation, State: domain.ValidationStateInvalidated,
		Required: true, Revision: 3, DiffRevision: 1,
	})
	if err != nil || invalidated.Validation.State != domain.ValidationStateInvalidated {
		t.Fatalf("valid invalidation failed: projection=%#v error=%v", invalidated, err)
	}

	if _, err := taskprojection.ApplyValidationUpdate(base, taskprojection.ValidationEvent{
		ID: ids.validation, State: domain.ValidationStateRunning,
		Required: true, Revision: 3, DiffRevision: 1,
	}); err == nil {
		t.Fatal("terminal validation identity restarted")
	}
}

func TestSnapshotRejectsMalformedSecondaryProjections(t *testing.T) {
	ids := fixtureIDs(t)
	base := taskprojection.TaskProjection{
		TaskID: ids.task, State: domain.TaskStateRunning, Revision: 3,
		Recovery: taskprojection.RecoveryNone,
	}
	tests := []struct {
		name   string
		mutate func(*taskprojection.TaskProjection)
	}{
		{name: "checkpoint newer than task", mutate: func(task *taskprojection.TaskProjection) {
			task.Checkpoint = taskprojection.CheckpointProjection{
				Present: true, ID: ids.checkpoint, TaskRevision: 4,
				CreatedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC), Revision: 1,
			}
		}},
		{name: "incomplete review bindings", mutate: func(task *taskprojection.TaskProjection) {
			task.Review = taskprojection.ReviewProjection{Present: true, Revision: 1}
		}},
		{name: "graph without revision", mutate: func(task *taskprojection.TaskProjection) {
			task.Graph = taskprojection.GraphProjection{Present: true}
		}},
		{name: "busy command without key", mutate: func(task *taskprojection.TaskProjection) {
			task.PendingCommand = taskprojection.CommandState{
				Action: taskprojection.ActionPause, ExpectedRevision: 3, Status: taskprojection.CommandBusy,
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projection := base
			test.mutate(&projection)
			if _, err := taskprojection.ApplySnapshot(taskprojection.Snapshot{Projection: projection}); err == nil {
				t.Fatal("malformed snapshot was accepted")
			}
		})
	}
}
