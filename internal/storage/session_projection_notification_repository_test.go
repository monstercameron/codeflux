package storage

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

type projectionInvalidationPublisher struct {
	events []events.SessionEvent
}

func (publisher *projectionInvalidationPublisher) PublishCommitted(event events.SessionEvent) error {
	publisher.events = append(publisher.events, event)
	return nil
}

func TestSessionProjectionInvalidationRetryPublishesOneDurableSequence(t *testing.T) {
	repositories, task, sessionID := createSessionEventFixture(t, 2860)
	publisher := &projectionInvalidationPublisher{}
	input := RecordSessionProjectionInvalidation{
		TaskID: task.ID, Entity: "task", EntityRevision: task.Revision,
		IdempotencyKey: "projection-notification-retry",
	}
	first, err := repositories.RecordAndPublishSessionProjectionInvalidation(t.Context(), input, publisher)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repositories.RecordAndPublishSessionProjectionInvalidation(t.Context(), input, publisher)
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != second.Sequence || len(publisher.events) != 2 {
		t.Fatalf("retry events = first:%d second:%d published:%d", first.Sequence, second.Sequence, len(publisher.events))
	}
	sequence, err := repositories.CurrentSessionSequence(t.Context(), sessionID)
	if err != nil || sequence != first.Sequence {
		t.Fatalf("durable sequence = %d, %v", sequence, err)
	}
}

func TestSessionProjectionReconciliationClosesMutationCommitCrashBoundary(t *testing.T) {
	repositories, task, sessionID := createSessionEventFixture(t, 2880)
	publisher := &projectionInvalidationPublisher{}
	eventID, _ := domain.NewEventID()
	if _, err := repositories.TransitionTask(t.Context(), TransitionTask{
		EventID: eventID, TaskID: task.ID, ExpectedRevision: task.Revision,
		From: domain.TaskStateDraft, To: domain.TaskStateForecasting,
		IdempotencyKey: "committed-before-notification-crash",
	}); err != nil {
		t.Fatal(err)
	}

	// TransitionTask committed its normalized task_event without a session
	// notification. This represents a coordinator crash between those writes.
	if err := repositories.ReconcileSessionProjectionInvalidations(t.Context(), sessionID, publisher); err != nil {
		t.Fatal(err)
	}
	firstSequence, err := repositories.CurrentSessionSequence(t.Context(), sessionID)
	if err != nil || firstSequence == 0 || len(publisher.events) == 0 {
		t.Fatalf("first reconciliation sequence=%d published=%d err=%v", firstSequence, len(publisher.events), err)
	}
	firstPublished := len(publisher.events)
	if err := repositories.ReconcileSessionProjectionInvalidations(context.Background(), sessionID, publisher); err != nil {
		t.Fatal(err)
	}
	secondSequence, err := repositories.CurrentSessionSequence(t.Context(), sessionID)
	if err != nil || secondSequence != firstSequence || len(publisher.events) != firstPublished {
		t.Fatalf("repeat reconciliation sequence=%d published=%d err=%v", secondSequence, len(publisher.events), err)
	}

	replayed, err := repositories.ReplaySessionEvents(t.Context(), ReplaySessionEvents{
		SessionID: sessionID, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != int(firstSequence) || replayed[len(replayed)-1].Kind != events.KindTaskProjectionInvalidated {
		t.Fatalf("reconciled replay = %#v", replayed)
	}
}

func TestSessionProjectionReconciliationBackfillsMutationCommittedBeforeSessionBinding(t *testing.T) {
	repositories, task := createTaskFixture(t, 2890)
	publisher := &projectionInvalidationPublisher{}
	eventID, _ := domain.NewEventID()
	transitioned, err := repositories.TransitionTask(t.Context(), TransitionTask{
		EventID: eventID, TaskID: task.ID, ExpectedRevision: task.Revision,
		From: domain.TaskStateDraft, To: domain.TaskStateForecasting,
		IdempotencyKey: "committed-before-session-binding",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repositories.RecordAndPublishSessionProjectionInvalidation(
		t.Context(),
		RecordSessionProjectionInvalidation{
			TaskID: task.ID, Entity: "task", EntityRevision: transitioned.Task.Revision,
			IdempotencyKey: "pre-session-projection-notification",
		},
		publisher,
	)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("pre-session notification error = %v", err)
	}

	sessionID := testSessionID(t, 2894)
	if _, err := repositories.CreateSession(t.Context(), CreateSession{
		ID: sessionID, ThreadID: task.ThreadID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.ReconcileSessionProjectionInvalidations(t.Context(), sessionID, publisher); err != nil {
		t.Fatal(err)
	}
	replayed, err := repositories.ReplaySessionEvents(t.Context(), ReplaySessionEvents{
		SessionID: sessionID, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) == 0 || replayed[len(replayed)-1].Kind != events.KindTaskProjectionInvalidated {
		t.Fatalf("pre-session backfill replay = %#v", replayed)
	}
}
