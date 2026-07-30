package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestHubJoinsReplayAndLiveWithoutGapOrDuplicate(t *testing.T) {
	ids := newEventTestIDs(t)
	journal := &memoryJournal{}
	first := hubMessageFinal(t, ids, 1, "first")
	journal.commit(first)
	hub, err := NewHub(journal, 8)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := hub.Subscribe(context.Background(), SubscriptionQuery{
		SessionID: ids.session,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.PublishCommitted(first); err != nil {
		t.Fatal(err)
	}
	second := hubMessageFinal(t, ids, 2, "second")
	journal.commit(second)
	if err := hub.PublishCommitted(second); err != nil {
		t.Fatal(err)
	}
	assertNextSequence(t, subscription, 1)
	assertNextSequence(t, subscription, 2)
	if metrics := hub.GlobalMetrics(); metrics.Active != 1 ||
		metrics.Published != 2 || metrics.Delivered != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestHubCloseDisconnectsStreamsAndRejectsNewDelivery(t *testing.T) {
	ids := newEventTestIDs(t)
	journal := &memoryJournal{}
	hub, err := NewHub(journal, 8)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := hub.Subscribe(
		context.Background(),
		SubscriptionQuery{SessionID: ids.session},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := subscription.Next(context.Background()); !errors.Is(err, ErrHubClosed) {
		t.Fatalf("closed subscription error = %v", err)
	}
	if _, err := hub.Subscribe(
		context.Background(),
		SubscriptionQuery{SessionID: ids.session},
	); !errors.Is(err, ErrHubClosed) {
		t.Fatalf("subscribe after close = %v", err)
	}
	if err := hub.PublishCommitted(
		hubMessageFinal(t, ids, 1, "late"),
	); !errors.Is(err, ErrHubClosed) {
		t.Fatalf("publish after close = %v", err)
	}
	if metrics := hub.GlobalMetrics(); metrics.Active != 0 {
		t.Fatalf("closed hub metrics = %#v", metrics)
	}
	if err := hub.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHubSerializesPublicationAcrossReplayBoundary(t *testing.T) {
	ids := newEventTestIDs(t)
	first := hubMessageFinal(t, ids, 1, "first")
	second := hubMessageFinal(t, ids, 2, "second")
	journal := &blockingJournal{
		memoryJournal: memoryJournal{events: []SessionEvent{first}},
		replayStarted: make(chan struct{}),
		replayRelease: make(chan struct{}),
	}
	hub, err := NewHub(journal, 8)
	if err != nil {
		t.Fatal(err)
	}
	type subscribeResult struct {
		subscription *Subscription
		err          error
	}
	result := make(chan subscribeResult, 1)
	go func() {
		subscription, subscribeErr := hub.Subscribe(
			context.Background(),
			SubscriptionQuery{SessionID: ids.session},
		)
		result <- subscribeResult{subscription: subscription, err: subscribeErr}
	}()
	<-journal.replayStarted
	journal.commit(second)
	published := make(chan error, 1)
	go func() {
		published <- hub.PublishCommitted(second)
	}()
	select {
	case err := <-published:
		t.Fatalf("publication crossed replay boundary early: %v", err)
	default:
	}
	close(journal.replayRelease)
	joined := <-result
	if joined.err != nil {
		t.Fatal(joined.err)
	}
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	assertNextSequence(t, joined.subscription, 1)
	assertNextSequence(t, joined.subscription, 2)
}

func TestHubCoalescesEphemeralProgressButPreservesMaterialOrder(t *testing.T) {
	ids := newEventTestIDs(t)
	journal := &memoryJournal{}
	hub, err := NewHub(journal, 2)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := hub.Subscribe(
		context.Background(),
		SubscriptionQuery{SessionID: ids.session},
	)
	if err != nil {
		t.Fatal(err)
	}
	deltaOne := hubMessageDelta(t, ids, 1, "a")
	material := hubMessageFinal(t, ids, 2, "final")
	deltaTwo := hubMessageDelta(t, ids, 3, "b")
	for _, event := range []SessionEvent{deltaOne, material, deltaTwo} {
		journal.commit(event)
		if err := hub.PublishCommitted(event); err != nil {
			t.Fatal(err)
		}
	}
	assertNextSequence(t, subscription, 2)
	got, err := subscription.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Sequence != 3 || got.Payload.MessageDelta.RedactedDelta != "b" {
		t.Fatalf("coalesced delta = %#v", got)
	}
	if metrics := hub.GlobalMetrics(); metrics.EphemeralCoalesced != 1 {
		t.Fatalf("coalesced metrics = %#v", metrics)
	}
}

func TestHubDisconnectsInsteadOfDroppingMaterialEvents(t *testing.T) {
	ids := newEventTestIDs(t)
	journal := &memoryJournal{}
	hub, err := NewHub(journal, 2)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := hub.Subscribe(
		context.Background(),
		SubscriptionQuery{SessionID: ids.session},
	)
	if err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(1); sequence <= 3; sequence++ {
		event := hubMessageFinal(t, ids, sequence, "material")
		journal.commit(event)
		if err := hub.PublishCommitted(event); err != nil {
			t.Fatal(err)
		}
	}
	assertNextSequence(t, subscription, 1)
	assertNextSequence(t, subscription, 2)
	if _, err := subscription.Next(context.Background()); !errors.Is(err, ErrSlowSubscriber) {
		t.Fatalf("terminal error = %v, want slow subscriber", err)
	}
	if metrics := hub.GlobalMetrics(); metrics.Active != 0 ||
		metrics.ForcedDisconnects != 1 {
		t.Fatalf("disconnect metrics = %#v", metrics)
	}
	replay, err := journal.ReplayCommitted(
		context.Background(),
		ids.session,
		2,
		10,
	)
	if err != nil || len(replay) != 1 || replay[0].Sequence != 3 {
		t.Fatalf("durable recovery = %#v, %v", replay, err)
	}
}

func TestHubCancellationRemovesSubscriberWithoutWorkerGoroutine(t *testing.T) {
	ids := newEventTestIDs(t)
	hub, err := NewHub(&memoryJournal{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	subscription, err := hub.Subscribe(ctx, SubscriptionQuery{
		SessionID: ids.session,
		TaskID:    &ids.task,
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-subscription.done:
	case <-time.After(time.Second):
		t.Fatal("subscription did not close after cancellation")
	}
	deadline := time.Now().Add(time.Second)
	for hub.GlobalMetrics().Active != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if metrics := hub.GlobalMetrics(); metrics.Active != 0 {
		t.Fatalf("active subscriptions = %d", metrics.Active)
	}
	if _, err := subscription.Next(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled next error = %v", err)
	}
}

type memoryJournal struct {
	mu     sync.Mutex
	events []SessionEvent
}

func (journal *memoryJournal) commit(event SessionEvent) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	journal.events = append(journal.events, event)
}

func (journal *memoryJournal) ReplayCommitted(
	_ context.Context,
	sessionID domain.SessionID,
	after uint64,
	limit int,
) ([]SessionEvent, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	result := make([]SessionEvent, 0, limit)
	for _, event := range journal.events {
		if event.SessionID == sessionID && event.Sequence > after {
			result = append(result, event)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (journal *memoryJournal) CommittedSequence(
	_ context.Context,
	sessionID domain.SessionID,
) (uint64, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	var sequence uint64
	for _, event := range journal.events {
		if event.SessionID == sessionID && event.Sequence > sequence {
			sequence = event.Sequence
		}
	}
	return sequence, nil
}

type blockingJournal struct {
	memoryJournal
	replayStarted chan struct{}
	replayRelease chan struct{}
}

func (journal *blockingJournal) ReplayCommitted(
	ctx context.Context,
	sessionID domain.SessionID,
	after uint64,
	limit int,
) ([]SessionEvent, error) {
	close(journal.replayStarted)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-journal.replayRelease:
	}
	return journal.memoryJournal.ReplayCommitted(ctx, sessionID, after, limit)
}

func hubMessageFinal(
	t *testing.T,
	ids eventTestIDs,
	sequence uint64,
	body string,
) SessionEvent {
	t.Helper()
	event := SessionEvent{
		Sequence:       sequence,
		SessionID:      ids.session,
		ThreadID:       ids.thread,
		TaskID:         &ids.task,
		Timestamp:      time.UnixMicro(int64(sequence)).UTC(),
		Kind:           KindMessageFinal,
		PayloadVersion: 1,
		Payload: Payload{MessageFinal: &MessageFinal{
			MessageID:    ids.message,
			Role:         "assistant",
			RedactedBody: body,
		}},
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	return event
}

func hubMessageDelta(
	t *testing.T,
	ids eventTestIDs,
	sequence uint64,
	body string,
) SessionEvent {
	t.Helper()
	event := SessionEvent{
		Sequence:       sequence,
		SessionID:      ids.session,
		ThreadID:       ids.thread,
		TaskID:         &ids.task,
		Timestamp:      time.UnixMicro(int64(sequence)).UTC(),
		Kind:           KindMessageDelta,
		PayloadVersion: 1,
		Payload: Payload{MessageDelta: &MessageDelta{
			MessageID:     ids.message,
			RedactedDelta: body,
		}},
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	return event
}

func assertNextSequence(t *testing.T, subscription *Subscription, want uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := subscription.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != want {
		t.Fatalf("next sequence = %d, want %d", event.Sequence, want)
	}
}
