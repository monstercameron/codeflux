package events

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestEveryInitialSessionEventKindHasTypedVersionedPayload(t *testing.T) {
	ids := newEventTestIDs(t)
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	money, err := domain.NewMoney(usd, 100)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		kind    Kind
		payload Payload
		class   DeliveryClass
	}{
		{KindMessageDelta, Payload{MessageDelta: &MessageDelta{MessageID: ids.message, RedactedDelta: "delta"}}, DeliveryEphemeralCoalescible},
		{KindMessageFinal, Payload{MessageFinal: &MessageFinal{MessageID: ids.message, Role: "assistant", RedactedBody: "final"}}, DeliveryMaterial},
		{KindThreadCreated, Payload{ThreadCreated: &ThreadCreated{WorkspaceID: &ids.workspace, Title: "Thread"}}, DeliveryMaterial},
		{KindThreadRenamed, Payload{ThreadRenamed: &ThreadRenamed{PreviousTitle: "Thread", Title: "Renamed"}}, DeliveryMaterial},
		{KindThreadArchived, Payload{ThreadArchived: &ThreadArchived{Archived: true}}, DeliveryMaterial},
		{KindPlanCreated, Payload{Plan: &Plan{Revision: 1, RedactedSummary: "plan"}}, DeliveryMaterial},
		{KindPlanChanged, Payload{Plan: &Plan{Revision: 2, RedactedSummary: "plan changed"}}, DeliveryMaterial},
		{KindToolStarted, Payload{Tool: &Tool{ExecutionID: "execution", CommandName: "test", State: "running"}}, DeliveryMaterial},
		{KindToolProgress, Payload{Tool: &Tool{ExecutionID: "execution", CommandName: "test", State: "running", RedactedSummary: "progress"}}, DeliveryEphemeralCoalescible},
		{KindToolCompleted, Payload{Tool: &Tool{ExecutionID: "execution", CommandName: "test", State: "succeeded"}}, DeliveryMaterial},
		{KindApprovalRequested, Payload{Approval: &Approval{ApprovalID: ids.approval, State: domain.ApprovalRequestStatePending, Scope: "network"}}, DeliveryMaterial},
		{KindApprovalResolved, Payload{Approval: &Approval{ApprovalID: ids.approval, State: domain.ApprovalRequestStateGranted, Scope: "network"}}, DeliveryMaterial},
		{KindTaskStateChanged, Payload{TaskStateChanged: &TaskStateChanged{From: domain.TaskStateDraft, To: domain.TaskStateForecasting}}, DeliveryMaterial},
		{KindForecastUpdated, Payload{Forecast: &Forecast{Range: domain.ForecastRange{}}}, DeliveryMaterial},
		{KindUsageUpdated, Payload{Usage: &Usage{Tokens: domain.TokenUsage{}}}, DeliveryMaterial},
		{KindCostUpdated, Payload{Cost: &Cost{}}, DeliveryMaterial},
		{KindBudgetUpdated, Payload{Budget: &Budget{HardLimit: money, Reserved: domain.Money{Currency: usd}, Actual: domain.Money{Currency: usd}}}, DeliveryMaterial},
		{KindValidationUpdated, Payload{Validation: &Validation{ValidationID: ids.validation, State: domain.ValidationStateRunning}}, DeliveryMaterial},
		{KindGraphSnapshot, Payload{Graph: &Graph{RevisionID: ids.graphRevision, EncodedChange: []byte{1}}}, DeliveryMaterial},
		{KindGraphPatch, Payload{Graph: &Graph{RevisionID: ids.graphRevision, EncodedChange: []byte{2}}}, DeliveryMaterial},
		{KindCheckpointCreated, Payload{Checkpoint: &Checkpoint{CheckpointID: ids.checkpoint, TaskRevision: 1}}, DeliveryMaterial},
		{KindRecoveryRequired, Payload{RecoveryRequired: &RecoveryRequired{CheckpointID: &ids.checkpoint, RedactedReason: "recovery required"}}, DeliveryMaterial},
		{KindError, Payload{Error: &UserError{Code: ErrorCodeProvider, RedactedMessage: "provider failed", Retryable: true}}, DeliveryMaterial},
	}
	for index, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			event := SessionEvent{
				Sequence:       uint64(index + 1),
				SessionID:      ids.session,
				ThreadID:       ids.thread,
				TaskID:         &ids.task,
				Timestamp:      time.Date(2026, 7, 30, 12, 0, index, 0, time.UTC),
				Kind:           test.kind,
				Revision:       uint64(index),
				CausationID:    &ids.event,
				CorrelationID:  &ids.correlation,
				PayloadVersion: 1,
				Payload:        test.payload,
			}
			if err := event.Validate(); err != nil {
				t.Fatal(err)
			}
			if !event.SafeForUI() || event.DeliveryClass() != test.class {
				t.Fatalf("event classification = %#v", event)
			}
		})
	}
}

func TestSessionEventRejectsMismatchedAndUnversionedPayload(t *testing.T) {
	ids := newEventTestIDs(t)
	event := SessionEvent{
		Sequence:       1,
		SessionID:      ids.session,
		ThreadID:       ids.thread,
		Timestamp:      time.Now().UTC(),
		Kind:           KindMessageFinal,
		PayloadVersion: 0,
		Payload: Payload{
			MessageDelta: &MessageDelta{MessageID: ids.message, RedactedDelta: "delta"},
		},
	}
	if err := event.Validate(); err == nil {
		t.Fatal("unversioned mismatched payload was accepted")
	}
	event.PayloadVersion = 1
	event.Payload.MessageFinal = &MessageFinal{
		MessageID:    ids.message,
		Role:         "assistant",
		RedactedBody: "final",
	}
	if err := event.Validate(); err == nil {
		t.Fatal("multiple payloads were accepted")
	}
}

func TestCorrectnessBearingKindsCannotUseEphemeralDelivery(t *testing.T) {
	for _, kind := range []Kind{
		KindMessageFinal,
		KindThreadCreated,
		KindThreadRenamed,
		KindThreadArchived,
		KindTaskStateChanged,
		KindApprovalRequested,
		KindApprovalResolved,
		KindBudgetUpdated,
		KindValidationUpdated,
		KindGraphSnapshot,
		KindGraphPatch,
		KindCheckpointCreated,
		KindRecoveryRequired,
		KindError,
	} {
		event := SessionEvent{Kind: kind}
		if !event.CorrectnessBearing() || event.DeliveryClass() != DeliveryMaterial {
			t.Fatalf("%s classification allows dropping", kind)
		}
	}
	for _, kind := range []Kind{KindMessageDelta, KindToolProgress} {
		event := SessionEvent{Kind: kind}
		if event.CorrectnessBearing() ||
			event.DeliveryClass() != DeliveryEphemeralCoalescible {
			t.Fatalf("%s classification is not coalescible", kind)
		}
	}
}

type eventTestIDs struct {
	session       domain.SessionID
	workspace     domain.WorkspaceID
	thread        domain.ThreadID
	task          domain.TaskID
	event         domain.EventID
	correlation   domain.EventID
	message       domain.MessageID
	approval      domain.ApprovalID
	validation    domain.ValidationID
	graphRevision domain.GraphRevisionID
	checkpoint    domain.CheckpointID
}

func newEventTestIDs(t *testing.T) eventTestIDs {
	t.Helper()
	var result eventTestIDs
	var err error
	if result.session, err = domain.NewSessionID(); err != nil {
		t.Fatal(err)
	}
	if result.workspace, err = domain.NewWorkspaceID(); err != nil {
		t.Fatal(err)
	}
	if result.thread, err = domain.NewThreadID(); err != nil {
		t.Fatal(err)
	}
	if result.task, err = domain.NewTaskID(); err != nil {
		t.Fatal(err)
	}
	if result.event, err = domain.NewEventID(); err != nil {
		t.Fatal(err)
	}
	if result.correlation, err = domain.NewEventID(); err != nil {
		t.Fatal(err)
	}
	if result.message, err = domain.NewMessageID(); err != nil {
		t.Fatal(err)
	}
	if result.approval, err = domain.NewApprovalID(); err != nil {
		t.Fatal(err)
	}
	if result.validation, err = domain.NewValidationID(); err != nil {
		t.Fatal(err)
	}
	if result.graphRevision, err = domain.NewGraphRevisionID(); err != nil {
		t.Fatal(err)
	}
	if result.checkpoint, err = domain.NewCheckpointID(); err != nil {
		t.Fatal(err)
	}
	return result
}
