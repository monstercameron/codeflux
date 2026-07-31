package taskprojection_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

type adapterFixture struct {
	session       domain.SessionID
	thread        domain.ThreadID
	task          domain.TaskID
	workspace     domain.WorkspaceID
	message       domain.MessageID
	approval      domain.ApprovalID
	validation    domain.ValidationID
	graphRevision domain.GraphRevisionID
	checkpoint    domain.CheckpointID
	money         domain.Money
}

func TestSessionEventAdapterExhaustivelyClassifiesEveryDurableKind(t *testing.T) {
	fixture := newAdapterFixture(t)
	current := adapterProjection(fixture)
	tests := []struct {
		kind        events.Kind
		payload     events.Payload
		revision    uint64
		want        taskprojection.EventKind
		unsupported bool
	}{
		{events.KindMessageDelta, events.Payload{MessageDelta: &events.MessageDelta{MessageID: fixture.message, RedactedDelta: "delta"}}, 0, taskprojection.EventNoop, false},
		{events.KindMessageFinal, events.Payload{MessageFinal: &events.MessageFinal{MessageID: fixture.message, Role: "assistant", RedactedBody: "final"}}, 0, taskprojection.EventNoop, false},
		{events.KindThreadCreated, events.Payload{ThreadCreated: &events.ThreadCreated{WorkspaceID: &fixture.workspace, Title: "Thread"}}, 0, taskprojection.EventNoop, false},
		{events.KindThreadRenamed, events.Payload{ThreadRenamed: &events.ThreadRenamed{PreviousTitle: "Thread", Title: "Renamed"}}, 1, taskprojection.EventNoop, false},
		{events.KindThreadArchived, events.Payload{ThreadArchived: &events.ThreadArchived{Archived: true}}, 1, taskprojection.EventNoop, false},
		{events.KindPlanCreated, events.Payload{Plan: &events.Plan{Revision: 1, RedactedSummary: "Plan"}}, 1, taskprojection.EventPlanRevision, false},
		{events.KindPlanChanged, events.Payload{Plan: &events.Plan{Revision: 1, RedactedSummary: "Changed plan"}}, 1, taskprojection.EventPlanRevision, false},
		{events.KindToolStarted, events.Payload{Tool: &events.Tool{ExecutionID: "tool", CommandName: "go test", State: "running"}}, 1, taskprojection.EventToolUpdate, false},
		{events.KindToolProgress, events.Payload{Tool: &events.Tool{ExecutionID: "tool", CommandName: "go test", State: "running", RedactedSummary: "progress"}}, 1, taskprojection.EventToolUpdate, false},
		{events.KindToolCompleted, events.Payload{Tool: &events.Tool{ExecutionID: "tool", CommandName: "go test", State: "succeeded"}}, 1, taskprojection.EventToolUpdate, false},
		{events.KindApprovalRequested, events.Payload{Approval: &events.Approval{ApprovalID: fixture.approval, State: domain.ApprovalRequestStatePending, Scope: "network"}}, 1, taskprojection.EventApprovalUpdate, false},
		{events.KindApprovalResolved, events.Payload{Approval: &events.Approval{ApprovalID: fixture.approval, State: domain.ApprovalRequestStateGranted, Scope: "network"}}, 1, taskprojection.EventApprovalUpdate, false},
		{events.KindTaskStateChanged, events.Payload{TaskStateChanged: &events.TaskStateChanged{From: domain.TaskStateDraft, To: domain.TaskStateForecasting}}, 2, taskprojection.EventTaskTransition, false},
		{events.KindForecastUpdated, events.Payload{Forecast: &events.Forecast{Range: domain.ForecastRange{}}}, 0, taskprojection.EventNoop, false},
		{events.KindUsageUpdated, events.Payload{Usage: &events.Usage{Tokens: domain.TokenUsage{}}}, 0, taskprojection.EventNoop, false},
		{events.KindCostUpdated, events.Payload{Cost: &events.Cost{}}, 0, taskprojection.EventNoop, false},
		{events.KindBudgetUpdated, events.Payload{Budget: &events.Budget{HardLimit: fixture.money, Reserved: zeroAdapterMoney(fixture.money), Actual: zeroAdapterMoney(fixture.money)}}, 1, taskprojection.EventBudgetUpdate, false},
		{events.KindValidationUpdated, events.Payload{Validation: &events.Validation{ValidationID: fixture.validation, State: domain.ValidationStateRunning, Required: true, DiffRevision: 1}}, 1, taskprojection.EventValidationUpdate, false},
		{events.KindGraphSnapshot, events.Payload{Graph: &events.Graph{RevisionID: fixture.graphRevision, EncodedChange: []byte{1}}}, 1, taskprojection.EventGraphSnapshot, false},
		{events.KindGraphPatch, events.Payload{Graph: &events.Graph{RevisionID: fixture.graphRevision, EncodedChange: []byte{2}}}, 1, taskprojection.EventGraphPatch, false},
		{events.KindCheckpointCreated, events.Payload{Checkpoint: &events.Checkpoint{CheckpointID: fixture.checkpoint, TaskRevision: 1, PlanStep: "test"}}, 1, taskprojection.EventCheckpoint, false},
		{events.KindRecoveryRequired, events.Payload{RecoveryRequired: &events.RecoveryRequired{
			CheckpointID: &fixture.checkpoint, RedactedReason: "recovery required",
			Classification: events.RecoveryAmbiguousOutcome, DivergenceSummary: "worktree differs",
			ExternalOutcomeAmbiguous: true, PreservePatchAvailable: true,
			Bindings: events.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 1},
		}}, 1, taskprojection.EventRecoveryUpdate, false},
		{events.KindChangeAcceptanceUpdated, events.Payload{ChangeAcceptance: &events.ChangeAcceptance{
			State:    domain.ChangeAcceptanceStatePending,
			Bindings: events.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 1},
		}}, 1, taskprojection.EventAcceptanceUpdate, false},
		{events.KindTaskProjectionInvalidated, events.Payload{TaskProjectionInvalidated: &events.TaskProjectionInvalidated{
			Entity: "budget", Revision: 2,
		}}, 2, "", true},
		{events.KindError, events.Payload{Error: &events.UserError{Code: events.ErrorCodeProvider, RedactedMessage: "provider failed", Retryable: true}}, 0, taskprojection.EventNoop, false},
	}
	if len(tests) != 25 {
		t.Fatalf("adapter cases = %d, want all 25 durable kinds", len(tests))
	}
	seen := make(map[events.Kind]struct{}, len(tests))
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			if _, duplicate := seen[test.kind]; duplicate {
				t.Fatalf("duplicate adapter case for %s", test.kind)
			}
			seen[test.kind] = struct{}{}
			event := adapterEvent(fixture, 1, test.kind, test.revision, test.payload)
			adapted, err := taskprojection.ProjectionEventFromSessionEvent(current, event)
			if test.unsupported {
				if !errors.Is(err, taskprojection.ErrUnsupportedSessionEventProjection) {
					t.Fatalf("unsupported adapter error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if adapted.Sequence != event.Sequence || adapted.Kind != test.want {
				t.Fatalf("adapted event = %#v, want kind %s", adapted, test.want)
			}
		})
	}
	if len(seen) != len(events.Registry) {
		t.Fatalf("classified event kinds = %d, generated registry kinds = %d", len(seen), len(events.Registry))
	}
}

func TestSessionEventNoopsAdvanceSequenceWithoutChangingTaskFacts(t *testing.T) {
	fixture := newAdapterFixture(t)
	current := adapterProjection(fixture)
	noops := []struct {
		kind    events.Kind
		payload events.Payload
	}{
		{events.KindMessageDelta, events.Payload{MessageDelta: &events.MessageDelta{MessageID: fixture.message, RedactedDelta: "delta"}}},
		{events.KindMessageFinal, events.Payload{MessageFinal: &events.MessageFinal{MessageID: fixture.message, Role: "assistant", RedactedBody: "final"}}},
		{events.KindThreadCreated, events.Payload{ThreadCreated: &events.ThreadCreated{WorkspaceID: &fixture.workspace, Title: "Thread"}}},
		{events.KindThreadRenamed, events.Payload{ThreadRenamed: &events.ThreadRenamed{PreviousTitle: "Thread", Title: "Renamed"}}},
		{events.KindThreadArchived, events.Payload{ThreadArchived: &events.ThreadArchived{Archived: true}}},
		{events.KindForecastUpdated, events.Payload{Forecast: &events.Forecast{Range: domain.ForecastRange{}}}},
		{events.KindUsageUpdated, events.Payload{Usage: &events.Usage{Tokens: domain.TokenUsage{}}}},
		{events.KindCostUpdated, events.Payload{Cost: &events.Cost{}}},
		{events.KindError, events.Payload{Error: &events.UserError{Code: events.ErrorCodeProvider, RedactedMessage: "provider failed", Retryable: true}}},
	}
	for _, test := range noops {
		before := current
		event := adapterEvent(fixture, current.LastSequence+1, test.kind, 0, test.payload)
		next, err := taskprojection.ApplySessionEvent(current, event)
		if err != nil {
			t.Fatalf("apply %s: %v", test.kind, err)
		}
		before.LastSequence = next.LastSequence
		if next.LastSequence != event.Sequence || !reflect.DeepEqual(next, before) {
			t.Fatalf("noop %s changed task facts: before=%#v after=%#v", test.kind, current, next)
		}
		current = next
	}
}

func TestSessionEventAdapterAppliesProvenTaskBudgetToolAndGraphFacts(t *testing.T) {
	fixture := newAdapterFixture(t)
	current := adapterProjection(fixture)

	transition := adapterEvent(fixture, 1, events.KindTaskStateChanged, 2, events.Payload{
		TaskStateChanged: &events.TaskStateChanged{From: domain.TaskStateDraft, To: domain.TaskStateForecasting},
	})
	current = applyAdapterEvent(t, current, transition)
	if current.State != domain.TaskStateForecasting || current.Revision != 2 {
		t.Fatalf("task transition = %#v", current)
	}

	budget := adapterEvent(fixture, 2, events.KindBudgetUpdated, 1, events.Payload{
		Budget: &events.Budget{HardLimit: fixture.money, Reserved: zeroAdapterMoney(fixture.money), Actual: zeroAdapterMoney(fixture.money)},
	})
	current = applyAdapterEvent(t, current, budget)
	if !current.Budget.Present || current.Budget.HardLimit != fixture.money {
		t.Fatalf("budget projection = %#v", current.Budget)
	}

	started := adapterEvent(fixture, 3, events.KindToolStarted, 1, events.Payload{
		Tool: &events.Tool{ExecutionID: "tool", CommandName: "go test", State: "running"},
	})
	current = applyAdapterEvent(t, current, started)
	progress := adapterEvent(fixture, 4, events.KindToolProgress, 2, events.Payload{
		Tool: &events.Tool{ExecutionID: "tool", CommandName: "go test", State: "running", RedactedSummary: "still running"},
	})
	current = applyAdapterEvent(t, current, progress)
	if current.Tool.Revision != 2 || current.Tool.SafeSummary != "still running" {
		t.Fatalf("same-state tool progress = %#v", current.Tool)
	}

	snapshot := adapterEvent(fixture, 5, events.KindGraphSnapshot, 3, events.Payload{
		Graph: &events.Graph{RevisionID: fixture.graphRevision, EncodedChange: []byte{1}},
	})
	current = applyAdapterEvent(t, current, snapshot)
	patch := adapterEvent(fixture, 6, events.KindGraphPatch, 4, events.Payload{
		Graph: &events.Graph{RevisionID: fixture.graphRevision, EncodedChange: []byte{2}},
	})
	current = applyAdapterEvent(t, current, patch)
	if !current.Graph.Present || current.Graph.Revision != 4 || current.LastSequence != 6 {
		t.Fatalf("graph projection = %#v", current)
	}

	checkpoint := adapterEvent(fixture, 7, events.KindCheckpointCreated, 1, events.Payload{
		Checkpoint: &events.Checkpoint{CheckpointID: fixture.checkpoint, TaskRevision: 2, PlanStep: "validate"},
	})
	current = applyAdapterEvent(t, current, checkpoint)
	validation := adapterEvent(fixture, 8, events.KindValidationUpdated, 1, events.Payload{
		Validation: &events.Validation{
			ValidationID: fixture.validation, State: domain.ValidationStateRunning,
			Required: true, DiffRevision: 1, RedactedSummary: "checks running",
		},
	})
	current = applyAdapterEvent(t, current, validation)
	acceptance := adapterEvent(fixture, 9, events.KindChangeAcceptanceUpdated, 1, events.Payload{
		ChangeAcceptance: &events.ChangeAcceptance{
			State:    domain.ChangeAcceptanceStatePending,
			Bindings: events.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 4},
		},
	})
	current = applyAdapterEvent(t, current, acceptance)
	if !current.Checkpoint.Present || current.Checkpoint.PlanStep != "validate" ||
		!current.Validation.Present || !current.Validation.Required || current.Validation.DiffRevision != 1 ||
		!current.Acceptance.Present || current.Acceptance.State != domain.ChangeAcceptanceStatePending ||
		current.Acceptance.Bindings.Graph != 4 || current.LastSequence != 9 {
		t.Fatalf("checkpoint/validation/acceptance projection = %#v", current)
	}
}

func TestSessionEventAdapterProjectsRecoveryDetailsWithoutRawContent(t *testing.T) {
	fixture := newAdapterFixture(t)
	current := adapterProjection(fixture)
	eventID, err := domain.ParseEventID("evt_" + projectionUUID)
	if err != nil {
		t.Fatal(err)
	}
	event := adapterEvent(fixture, 1, events.KindRecoveryRequired, 1, events.Payload{
		RecoveryRequired: &events.RecoveryRequired{
			CheckpointID: &fixture.checkpoint, RedactedReason: "settlement uncertain",
			Classification: events.RecoveryAmbiguousOutcome, DivergenceSummary: "worktree differs",
			ExternalOutcomeAmbiguous: true, PreservePatchAvailable: true,
			Bindings:        events.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 1},
			RelatedEventIDs: []domain.EventID{eventID}, RelatedFiles: []string{"internal/task.go"},
		},
	})
	next, err := taskprojection.ApplySessionEvent(current, event)
	if err != nil {
		t.Fatal(err)
	}
	if next.Recovery != taskprojection.RecoveryAmbiguousOutcome ||
		!next.RecoveryDetail.Present || !next.RecoveryDetail.ExternalOutcomeAmbiguous ||
		!next.RecoveryDetail.PreservePatchAvailable || next.RecoveryDetail.DivergenceSummary != "worktree differs" ||
		len(next.RecoveryDetail.RelatedEventIDs) != 1 || len(next.RecoveryDetail.RelatedFiles) != 1 {
		t.Fatalf("recovery projection = %#v", next.RecoveryDetail)
	}
}

func TestRecordedSessionFixtureMatchesServerProjection(t *testing.T) {
	fixture := newAdapterFixture(t)
	base := events.SessionSnapshot{
		SessionID: fixture.session, ThreadID: fixture.thread, ThroughSequence: 1,
		TaskID: &fixture.task, TaskState: domain.TaskStateRunning, TaskRevision: 1,
		SnapshotVersion: 1, CreatedAt: time.UnixMicro(1).UTC(),
	}
	bindings := events.RevisionBindings{Diff: 2, Plan: 3, Validation: 1, Evidence: 4, Graph: 5}
	recorded := []events.SessionEvent{
		adapterEvent(fixture, 2, events.KindCheckpointCreated, 1, events.Payload{
			Checkpoint: &events.Checkpoint{CheckpointID: fixture.checkpoint, TaskRevision: 1, PlanStep: "validate"},
		}),
		adapterEvent(fixture, 3, events.KindValidationUpdated, 1, events.Payload{
			Validation: &events.Validation{
				ValidationID: fixture.validation, State: domain.ValidationStateRunning,
				Required: true, DiffRevision: 2, RedactedSummary: "required checks running",
			},
		}),
		adapterEvent(fixture, 4, events.KindChangeAcceptanceUpdated, 1, events.Payload{
			ChangeAcceptance: &events.ChangeAcceptance{
				State: domain.ChangeAcceptanceStatePending, Bindings: bindings,
			},
		}),
	}
	server, err := events.ReduceTaskEvents(base, recorded)
	if err != nil {
		t.Fatal(err)
	}
	client := taskprojection.TaskProjection{
		TaskID: fixture.task, State: domain.TaskStateRunning, Revision: 1,
		LastSequence: 1, Recovery: taskprojection.RecoveryNone,
	}
	for _, event := range recorded {
		client, err = taskprojection.ApplySessionEvent(client, event)
		if err != nil {
			t.Fatalf("client apply %s: %v", event.Kind, err)
		}
	}
	if server.ThroughSequence != client.LastSequence || server.TaskState != client.State ||
		server.TaskRevision != client.Revision || server.CheckpointRevision != client.Checkpoint.Revision ||
		server.Checkpoint == nil || server.Checkpoint.CheckpointID != client.Checkpoint.ID ||
		server.Checkpoint.PlanStep != client.Checkpoint.PlanStep ||
		server.ValidationRevision != client.Validation.Revision || server.Validation == nil ||
		server.Validation.ValidationID != client.Validation.ID ||
		server.Validation.Required != client.Validation.Required ||
		server.Validation.Acknowledged != client.Validation.Acknowledged ||
		server.Validation.DiffRevision != client.Validation.DiffRevision ||
		server.ChangeAcceptanceRevision != client.Acceptance.Revision || server.ChangeAcceptance == nil ||
		server.ChangeAcceptance.State != client.Acceptance.State ||
		server.ChangeAcceptance.Bindings.Diff != client.Acceptance.Bindings.Diff ||
		server.ChangeAcceptance.Bindings.Plan != client.Acceptance.Bindings.Plan ||
		server.ChangeAcceptance.Bindings.Validation != client.Acceptance.Bindings.Validation ||
		server.ChangeAcceptance.Bindings.Evidence != client.Acceptance.Bindings.Evidence ||
		server.ChangeAcceptance.Bindings.Graph != client.Acceptance.Bindings.Graph {
		t.Fatalf("server/client projection mismatch: server=%#v client=%#v", server, client)
	}
}

func adapterProjection(fixture adapterFixture) taskprojection.TaskProjection {
	return taskprojection.TaskProjection{
		TaskID: fixture.task, State: domain.TaskStateDraft, Revision: 1,
		Recovery: taskprojection.RecoveryNone,
	}
}

func adapterEvent(
	fixture adapterFixture,
	sequence uint64,
	kind events.Kind,
	revision uint64,
	payload events.Payload,
) events.SessionEvent {
	taskID := fixture.task
	return events.SessionEvent{
		Sequence: sequence, SessionID: fixture.session, ThreadID: fixture.thread,
		TaskID: &taskID, Timestamp: time.UnixMicro(int64(sequence)).UTC(),
		Kind: kind, Revision: revision, PayloadVersion: 1, Payload: payload,
	}
}

func applyAdapterEvent(
	t *testing.T,
	current taskprojection.TaskProjection,
	event events.SessionEvent,
) taskprojection.TaskProjection {
	t.Helper()
	next, err := taskprojection.ApplySessionEvent(current, event)
	if err != nil {
		t.Fatalf("apply %s: %v", event.Kind, err)
	}
	return next
}

func zeroAdapterMoney(value domain.Money) domain.Money {
	return domain.Money{Currency: value.Currency}
}

func newAdapterFixture(t *testing.T) adapterFixture {
	t.Helper()
	parse := func(prefix string) string { return prefix + projectionUUID }
	session, err := domain.ParseSessionID(parse("ses_"))
	if err != nil {
		t.Fatal(err)
	}
	thread, err := domain.ParseThreadID(parse("thr_"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := domain.ParseTaskID(parse("tsk_"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := domain.ParseWorkspaceID(parse("wsp_"))
	if err != nil {
		t.Fatal(err)
	}
	message, err := domain.ParseMessageID(parse("msg_"))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := domain.ParseApprovalID(parse("apr_"))
	if err != nil {
		t.Fatal(err)
	}
	validation, err := domain.ParseValidationID(parse("val_"))
	if err != nil {
		t.Fatal(err)
	}
	graphRevision, err := domain.ParseGraphRevisionID(parse("grv_"))
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := domain.ParseCheckpointID(parse("ckp_"))
	if err != nil {
		t.Fatal(err)
	}
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	money, err := domain.NewMoney(usd, 500)
	if err != nil {
		t.Fatal(err)
	}
	return adapterFixture{
		session: session, thread: thread, task: task, workspace: workspace,
		message: message, approval: approval, validation: validation,
		graphRevision: graphRevision, checkpoint: checkpoint, money: money,
	}
}
