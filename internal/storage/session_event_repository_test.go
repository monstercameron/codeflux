//go:build integration

package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/quick"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

func TestSessionEventAppendAllocatesSequenceAtomicallyAndReplaysExclusively(t *testing.T) {
	ctx := context.Background()
	repositories, task, sessionID := createSessionEventFixture(t, 800)
	input := sessionMessageEvent(sessionID, task, 801, "first")

	rollback := errors.New("rollback fixture")
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		event, err := repositories.AppendSessionEvent(ctx, transaction, input)
		if err != nil {
			return err
		}
		if event.Sequence != 1 {
			t.Fatalf("uncommitted sequence = %d, want 1", event.Sequence)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("rollback error = %v", err)
	}
	if sequence, err := repositories.CurrentSessionSequence(ctx, sessionID); err != nil || sequence != 0 {
		t.Fatalf("sequence after rollback = %d, %v", sequence, err)
	}

	first, err := repositories.PersistSessionEvent(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repositories.PersistSessionEvent(
		ctx,
		sessionMessageEvent(sessionID, task, 802, "second"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("sequences = %d, %d", first.Sequence, second.Sequence)
	}
	replayed, err := repositories.ReplaySessionEvents(ctx, ReplaySessionEvents{
		SessionID:     sessionID,
		AfterSequence: 1,
		Limit:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 || replayed[0].Sequence != 2 {
		t.Fatalf("exclusive replay = %#v", replayed)
	}
	if replayed[0].Payload.MessageFinal == nil ||
		replayed[0].Payload.MessageFinal.RedactedBody != "second" {
		t.Fatalf("replayed payload = %#v", replayed[0].Payload)
	}
}

func TestSessionEventSequenceIsStrictlyIncreasingUnderConcurrentAppends(t *testing.T) {
	ctx := context.Background()
	repositories, task, sessionID := createSessionEventFixture(t, 820)
	const count = 64
	sequences := make(chan uint64, count)
	failures := make(chan error, count)
	var group sync.WaitGroup
	for index := 0; index < count; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			event, err := repositories.PersistSessionEvent(
				ctx,
				sessionMessageEvent(
					sessionID,
					task,
					830+index,
					fmt.Sprintf("event-%d", index),
				),
			)
			if err != nil {
				failures <- err
				return
			}
			sequences <- event.Sequence
		}(index)
	}
	group.Wait()
	close(failures)
	close(sequences)
	for err := range failures {
		t.Fatal(err)
	}
	got := make([]int, 0, count)
	for sequence := range sequences {
		got = append(got, int(sequence))
	}
	sort.Ints(got)
	if len(got) != count {
		t.Fatalf("sequence count = %d, want %d", len(got), count)
	}
	for index, sequence := range got {
		if sequence != index+1 {
			t.Fatalf("sequence[%d] = %d, want %d", index, sequence, index+1)
		}
	}
	current, err := repositories.CurrentSessionSequence(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if current != count {
		t.Fatalf("current sequence = %d, want %d", current, count)
	}
}

func TestSessionEventsAreImmutableAndBoundToSessionThread(t *testing.T) {
	ctx := context.Background()
	repositories, task, sessionID := createSessionEventFixture(t, 920)
	event, err := repositories.PersistSessionEvent(
		ctx,
		sessionMessageEvent(sessionID, task, 921, "immutable"),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repositories.database.sql.ExecContext(
		ctx,
		`UPDATE session_events SET payload_json = '{}' WHERE session_id = ? AND sequence = ?`,
		sessionID,
		event.Sequence,
	)
	if !errors.Is(classify("rewrite session event", err), ErrConstraint) {
		t.Fatalf("immutable update error = %v", err)
	}
}

func TestSessionEventPublishesOnlyAfterCommitAndRemainsDurableOnPublishFailure(t *testing.T) {
	ctx := context.Background()
	repositories, task, sessionID := createSessionEventFixture(t, 940)
	publisher := &commitObservingPublisher{
		repositories: repositories,
		sessionID:    sessionID,
		failure:      errors.New("subscriber transport unavailable"),
	}
	event, err := repositories.PersistAndPublishSessionEvent(
		ctx,
		sessionMessageEvent(sessionID, task, 941, "committed"),
		publisher,
	)
	if !errors.Is(err, publisher.failure) {
		t.Fatalf("publication error = %v", err)
	}
	if publisher.calls != 1 || publisher.observedSequence != event.Sequence {
		t.Fatalf("publisher = %#v, event = %#v", publisher, event)
	}
	replayed, replayErr := repositories.ReplaySessionEvents(ctx, ReplaySessionEvents{
		SessionID: sessionID,
		Limit:     10,
	})
	if replayErr != nil || len(replayed) != 1 || replayed[0].Sequence != event.Sequence {
		t.Fatalf("committed replay = %#v, %v", replayed, replayErr)
	}

	invalid := sessionMessageEvent(sessionID, task, 942, "invalid")
	invalid.PayloadVersion = 0
	if _, err := repositories.PersistAndPublishSessionEvent(
		ctx,
		invalid,
		publisher,
	); err == nil {
		t.Fatal("invalid event was accepted")
	}
	if publisher.calls != 1 {
		t.Fatalf("publisher calls after failed transaction = %d, want 1", publisher.calls)
	}
}

func TestSessionReplayReturnsSnapshotAndSubsequentEventsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	repositories, task, sessionID := createSessionEventFixture(t, 960)
	taskID := task.ID
	transition, err := repositories.PersistSessionEvent(ctx, events.NewSessionEvent{
		SessionID:      sessionID,
		ThreadID:       task.ThreadID,
		TaskID:         &taskID,
		Kind:           events.KindTaskStateChanged,
		Revision:       1,
		PayloadVersion: 1,
		Payload: events.Payload{TaskStateChanged: &events.TaskStateChanged{
			From: domain.TaskStateDraft,
			To:   domain.TaskStateForecasting,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.PersistSessionEvent(
		ctx,
		sessionMessageEvent(sessionID, task, 961, "after snapshot"),
	); err != nil {
		t.Fatal(err)
	}
	snapshot := events.SessionSnapshot{
		SessionID:       sessionID,
		ThreadID:        task.ThreadID,
		ThroughSequence: transition.Sequence,
		TaskID:          &taskID,
		TaskState:       domain.TaskStateForecasting,
		TaskRevision:    1,
		SnapshotVersion: 1,
		CreatedAt:       time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC),
	}
	if err := repositories.StoreSessionSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, OpenOptions{Path: repositories.database.Path()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(context.Background()); err != nil {
			t.Errorf("close reopened session database: %v", err)
		}
	})
	restarted, err := NewRepositories(reopened, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.ReplaySession(ctx, ReplaySessionEvents{
		SessionID:     sessionID,
		AfterSequence: 0,
		Limit:         10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Snapshot == nil ||
		replay.Snapshot.SessionID != snapshot.SessionID ||
		replay.Snapshot.ThreadID != snapshot.ThreadID ||
		replay.Snapshot.ThroughSequence != snapshot.ThroughSequence ||
		replay.Snapshot.TaskID == nil ||
		*replay.Snapshot.TaskID != *snapshot.TaskID ||
		replay.Snapshot.TaskState != snapshot.TaskState ||
		replay.Snapshot.TaskRevision != snapshot.TaskRevision ||
		replay.Snapshot.SnapshotVersion != snapshot.SnapshotVersion ||
		!replay.Snapshot.CreatedAt.Equal(snapshot.CreatedAt) {
		t.Fatalf("replay snapshot = %#v, want %#v", replay.Snapshot, snapshot)
	}
	if replay.Boundary != 2 || len(replay.Events) != 1 ||
		replay.Events[0].Sequence != 2 {
		t.Fatalf("restart replay = %#v", replay)
	}
	reduced, err := events.ReduceTaskEvents(*replay.Snapshot, replay.Events)
	if err != nil {
		t.Fatal(err)
	}
	if reduced.ThroughSequence != replay.Boundary ||
		reduced.TaskState != domain.TaskStateForecasting {
		t.Fatalf("reconstructed state = %#v", reduced)
	}
}

func TestSessionCommandsPersistOneAtomicResultForConcurrentRetries(t *testing.T) {
	ctx := context.Background()
	repositories, task, sessionID := createSessionEventFixture(t, 980)
	command := SessionCommand{
		SessionID:      sessionID,
		IdempotencyKey: "submit-message",
		RequestSHA256:  strings.Repeat("a", 64),
	}
	var operations atomic.Int32
	operation := func(transaction *Transaction) (string, uint64, error) {
		operations.Add(1)
		event, err := repositories.AppendSessionEvent(
			ctx,
			transaction,
			sessionMessageEvent(sessionID, task, 981, "once"),
		)
		return `{"accepted":true}`, event.Sequence, err
	}
	const callers = 16
	results := make(chan SessionCommandResult, callers)
	failures := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := repositories.ExecuteSessionCommand(ctx, command, operation)
			if err != nil {
				failures <- err
				return
			}
			results <- result
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var replayed int
	for result := range results {
		if result.FinalSequence != 1 || result.ResultJSON != `{"accepted":true}` {
			t.Fatalf("command result = %#v", result)
		}
		if result.Replayed {
			replayed++
		}
	}
	if operations.Load() != 1 || replayed != callers-1 {
		t.Fatalf("operations = %d, replayed = %d", operations.Load(), replayed)
	}
	if sequence, err := repositories.CurrentSessionSequence(ctx, sessionID); err != nil ||
		sequence != 1 {
		t.Fatalf("command sequence = %d, %v", sequence, err)
	}
	conflict := command
	conflict.RequestSHA256 = strings.Repeat("b", 64)
	if _, err := repositories.ExecuteSessionCommand(
		ctx,
		conflict,
		operation,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting command error = %v", err)
	}
}

func TestSessionCommandRollsBackEffectsWhenResultIsInvalid(t *testing.T) {
	ctx := context.Background()
	repositories, task, sessionID := createSessionEventFixture(t, 1000)
	_, err := repositories.ExecuteSessionCommand(
		ctx,
		SessionCommand{
			SessionID:      sessionID,
			IdempotencyKey: "invalid-result",
			RequestSHA256:  strings.Repeat("c", 64),
		},
		func(transaction *Transaction) (string, uint64, error) {
			event, appendErr := repositories.AppendSessionEvent(
				ctx,
				transaction,
				sessionMessageEvent(sessionID, task, 1001, "rolled back"),
			)
			return "[]", event.Sequence, appendErr
		},
	)
	if err == nil {
		t.Fatal("non-object command result was accepted")
	}
	if sequence, currentErr := repositories.CurrentSessionSequence(
		ctx,
		sessionID,
	); currentErr != nil || sequence != 0 {
		t.Fatalf("sequence after command rollback = %d, %v", sequence, currentErr)
	}
}

func TestReconnectAtEveryBoundaryReconstructsInterleavedSessionState(t *testing.T) {
	ctx := context.Background()
	repositories, task, sessionID := createSessionEventFixture(t, 1020)
	taskID := task.ID
	approvalID := testApprovalID(t, 1025)
	graphRevision, err := domain.ParseGraphRevisionID("grv_" + testUUID(1026))
	if err != nil {
		t.Fatal(err)
	}
	inputs := []events.NewSessionEvent{
		sessionMessageEvent(sessionID, task, 1027, "request"),
		sessionTaskTransition(sessionID, task, 1, domain.TaskStateDraft, domain.TaskStateForecasting),
		{
			SessionID: sessionID, ThreadID: task.ThreadID, TaskID: &taskID,
			Kind: events.KindCostUpdated, PayloadVersion: 1,
			Payload: events.Payload{Cost: &events.Cost{}},
		},
		sessionTaskTransition(
			sessionID,
			task,
			2,
			domain.TaskStateForecasting,
			domain.TaskStateAwaitingPlanApproval,
		),
		{
			SessionID: sessionID, ThreadID: task.ThreadID, TaskID: &taskID,
			Kind: events.KindApprovalRequested, Revision: 1, PayloadVersion: 1,
			Payload: events.Payload{Approval: &events.Approval{
				ApprovalID: approvalID,
				State:      domain.ApprovalRequestStatePending,
				Scope:      "plan",
			}},
		},
		sessionTaskTransition(
			sessionID,
			task,
			3,
			domain.TaskStateAwaitingPlanApproval,
			domain.TaskStateReady,
		),
		{
			SessionID: sessionID, ThreadID: task.ThreadID, TaskID: &taskID,
			Kind: events.KindGraphPatch, Revision: 1, PayloadVersion: 1,
			Payload: events.Payload{Graph: &events.Graph{
				RevisionID:    graphRevision,
				EncodedChange: []byte{1, 2, 3},
			}},
		},
		sessionTaskTransition(sessionID, task, 4, domain.TaskStateReady, domain.TaskStateRunning),
	}
	persisted := make([]events.SessionEvent, 0, len(inputs))
	for _, input := range inputs {
		event, err := repositories.PersistSessionEvent(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		persisted = append(persisted, event)
	}
	hub, err := events.NewHub(repositories, len(inputs)+1)
	if err != nil {
		t.Fatal(err)
	}
	initial := events.SessionSnapshot{
		SessionID:       sessionID,
		ThreadID:        task.ThreadID,
		TaskID:          &taskID,
		TaskState:       domain.TaskStateDraft,
		SnapshotVersion: 1,
		CreatedAt:       time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC),
	}
	for boundary := 0; boundary <= len(persisted); boundary++ {
		before, err := events.ReduceTaskEvents(initial, persisted[:boundary])
		if err != nil {
			t.Fatalf("reduce before boundary %d: %v", boundary, err)
		}
		subscription, err := hub.Subscribe(ctx, events.SubscriptionQuery{
			SessionID:     sessionID,
			TaskID:        &taskID,
			AfterSequence: uint64(boundary),
		})
		if err != nil {
			t.Fatalf("subscribe boundary %d: %v", boundary, err)
		}
		missed := make([]events.SessionEvent, 0, len(persisted)-boundary)
		for range len(persisted) - boundary {
			next, err := subscription.Next(ctx)
			if err != nil {
				t.Fatalf("next boundary %d: %v", boundary, err)
			}
			missed = append(missed, next)
		}
		if err := subscription.Close(); err != nil {
			t.Fatal(err)
		}
		reconstructed, err := events.ReduceTaskEvents(before, missed)
		if err != nil {
			t.Fatalf("reconstruct boundary %d: %v", boundary, err)
		}
		if reconstructed.ThroughSequence != uint64(len(persisted)) ||
			reconstructed.TaskState != domain.TaskStateRunning ||
			reconstructed.TaskRevision != 4 {
			t.Fatalf("boundary %d state = %#v", boundary, reconstructed)
		}
	}
	if metrics := hub.GlobalMetrics(); metrics.Active != 0 {
		t.Fatalf("active subscribers after reconnect test = %d", metrics.Active)
	}
}

func TestPropertySessionSequencesRemainContiguous(t *testing.T) {
	property := func(raw uint8) bool {
		count := int(raw%16) + 1
		repositories, task, sessionID := createSessionEventFixture(t, 1080)
		for index := 0; index < count; index++ {
			event, err := repositories.PersistSessionEvent(
				context.Background(),
				sessionMessageEvent(sessionID, task, 1090+index, "property"),
			)
			if err != nil || event.Sequence != uint64(index+1) {
				return false
			}
		}
		replayed, err := repositories.ReplaySessionEvents(
			context.Background(),
			ReplaySessionEvents{SessionID: sessionID, Limit: count},
		)
		if err != nil || len(replayed) != count {
			return false
		}
		for index, event := range replayed {
			if event.Sequence != uint64(index+1) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 12}); err != nil {
		t.Fatal(err)
	}
}

func sessionMessageEvent(
	sessionID domain.SessionID,
	task Task,
	messageNumber int,
	body string,
) events.NewSessionEvent {
	messageID, err := domain.ParseMessageID("msg_" + testUUID(messageNumber))
	if err != nil {
		panic(err)
	}
	taskID := task.ID
	return events.NewSessionEvent{
		SessionID:      sessionID,
		ThreadID:       task.ThreadID,
		TaskID:         &taskID,
		Kind:           events.KindMessageFinal,
		Revision:       task.Revision,
		PayloadVersion: 1,
		Payload: events.Payload{
			MessageFinal: &events.MessageFinal{
				MessageID:    messageID,
				Role:         "assistant",
				RedactedBody: body,
			},
		},
	}
}

func sessionTaskTransition(
	sessionID domain.SessionID,
	task Task,
	revision uint64,
	from domain.TaskState,
	to domain.TaskState,
) events.NewSessionEvent {
	taskID := task.ID
	return events.NewSessionEvent{
		SessionID:      sessionID,
		ThreadID:       task.ThreadID,
		TaskID:         &taskID,
		Kind:           events.KindTaskStateChanged,
		Revision:       revision,
		PayloadVersion: 1,
		Payload: events.Payload{TaskStateChanged: &events.TaskStateChanged{
			From: from,
			To:   to,
		}},
	}
}

type commitObservingPublisher struct {
	repositories     *Repositories
	sessionID        domain.SessionID
	failure          error
	calls            int
	observedSequence uint64
}

func (publisher *commitObservingPublisher) PublishCommitted(
	event events.SessionEvent,
) error {
	publisher.calls++
	sequence, err := publisher.repositories.CurrentSessionSequence(
		context.Background(),
		publisher.sessionID,
	)
	if err != nil {
		return err
	}
	replayed, err := publisher.repositories.ReplaySessionEvents(
		context.Background(),
		ReplaySessionEvents{
			SessionID:     publisher.sessionID,
			AfterSequence: event.Sequence - 1,
			Limit:         1,
		},
	)
	if err != nil {
		return err
	}
	if sequence != event.Sequence || len(replayed) != 1 ||
		replayed[0].Sequence != event.Sequence {
		return errors.New("publisher observed event before durable commit")
	}
	publisher.observedSequence = sequence
	return publisher.failure
}
