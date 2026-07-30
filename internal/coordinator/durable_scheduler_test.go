package coordinator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestDurableSchedulerRestoresDispatchesAndCancelsSQLiteQueue(t *testing.T) {
	restoredTask, _ := domain.NewTaskID()
	store := &memoryQueueStore{entries: map[string]storage.TaskQueueEntry{
		"queue-restored": {
			ID: "queue-restored", TaskID: restoredTask,
			ProviderKey: "provider-a", State: storage.TaskQueueStateQueued,
			Reason: "resuming after approval", Priority: 1, Resuming: true,
			EnqueueSequence: 1, EnqueuedAt: time.Now(),
		},
	}}
	memory, _ := NewScheduler(1, nil)
	scheduler, err := NewDurableScheduler(t.Context(), memory, store)
	if err != nil {
		t.Fatal(err)
	}
	freshTask, _ := domain.NewTaskID()
	if _, err := scheduler.Enqueue(t.Context(), storage.EnqueueTask{
		ID: "queue-fresh", TaskID: freshTask, ProviderKey: "provider-a",
		Reason: "worker capacity", Priority: 1000, EnqueueSequence: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if position := scheduler.Position(restoredTask); position.Position != 1 ||
		position.Reason != "resuming after approval" {
		t.Fatalf("restored queue position = %#v", position)
	}
	dispatched, ok, err := scheduler.Dispatch(t.Context())
	if err != nil || !ok || dispatched.TaskID != restoredTask {
		t.Fatalf("dispatch = %#v, %t, %v", dispatched, ok, err)
	}
	if store.entries["queue-restored"].State != storage.TaskQueueStateDispatched {
		t.Fatalf("restored entry = %#v", store.entries["queue-restored"])
	}
	if err := scheduler.Complete(restoredTask); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.entries["queue-fresh"].State != storage.TaskQueueStateCancelled {
		t.Fatalf("fresh entry = %#v", store.entries["queue-fresh"])
	}
	if _, err := scheduler.Enqueue(t.Context(), storage.EnqueueTask{
		ID: "queue-after-shutdown", TaskID: freshTask,
		ProviderKey: "provider-a", Reason: "late work",
		Priority: 1, EnqueueSequence: 3,
	}); err == nil {
		t.Fatal("durable scheduler accepted work after shutdown")
	}
	if _, exists := store.entries["queue-after-shutdown"]; exists {
		t.Fatal("shutdown scheduler wrote late work to SQLite store")
	}
}

func TestDurableSchedulerRestoresLocalQueueAfterDatabaseContention(t *testing.T) {
	store := &memoryQueueStore{entries: make(map[string]storage.TaskQueueEntry)}
	memory, _ := NewScheduler(1, nil)
	scheduler, err := NewDurableScheduler(t.Context(), memory, store)
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := domain.NewTaskID()
	if _, err := scheduler.Enqueue(t.Context(), storage.EnqueueTask{
		ID: "queue-busy", TaskID: taskID, ProviderKey: "provider-a",
		Reason: "worker capacity is full", Priority: 10, EnqueueSequence: 1,
	}); err != nil {
		t.Fatal(err)
	}
	store.transitionErr = storage.ErrBusy
	if request, ok, err := scheduler.Dispatch(t.Context()); ok ||
		!errors.Is(err, storage.ErrBusy) || !request.TaskID.IsZero() {
		t.Fatalf("busy dispatch = %#v, %t, %v", request, ok, err)
	}
	if position := scheduler.Position(taskID); position.Position != 1 ||
		position.Reason != "worker capacity is full" {
		t.Fatalf("restored position = %#v", position)
	}
	if store.entries["queue-busy"].State != storage.TaskQueueStateQueued {
		t.Fatalf("busy transition changed durable state: %#v", store.entries["queue-busy"])
	}
	request, ok, err := scheduler.Dispatch(t.Context())
	if err != nil || !ok || request.TaskID != taskID {
		t.Fatalf("retried dispatch = %#v, %t, %v", request, ok, err)
	}
}

func TestDurableSchedulerPreservesUncancelledRowsWhenShutdownWriteIsBusy(
	t *testing.T,
) {
	store := &memoryQueueStore{entries: make(map[string]storage.TaskQueueEntry)}
	memory, _ := NewScheduler(1, nil)
	scheduler, err := NewDurableScheduler(t.Context(), memory, store)
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := domain.NewTaskID()
	if _, err := scheduler.Enqueue(t.Context(), storage.EnqueueTask{
		ID: "queue-shutdown-busy", TaskID: taskID,
		ProviderKey: "provider-a", Reason: "waiting for a worker",
		Priority: 1, EnqueueSequence: 1,
	}); err != nil {
		t.Fatal(err)
	}
	store.transitionErr = storage.ErrBusy
	if err := scheduler.Shutdown(t.Context()); !errors.Is(err, storage.ErrBusy) {
		t.Fatalf("shutdown error = %v, want database busy", err)
	}
	if store.entries["queue-shutdown-busy"].State != storage.TaskQueueStateQueued {
		t.Fatalf("failed cancellation lost durable work: %#v", store.entries["queue-shutdown-busy"])
	}

	restartedMemory, _ := NewScheduler(1, nil)
	restarted, err := NewDurableScheduler(
		t.Context(), restartedMemory, store,
	)
	if err != nil {
		t.Fatal(err)
	}
	if position := restarted.Position(taskID); position.Position != 1 {
		t.Fatalf("restart did not restore uncancelled work: %#v", position)
	}
}

func TestDurableSchedulerRejectsDatabaseErrorsAndOversizedRestoration(
	t *testing.T,
) {
	store := &memoryQueueStore{
		entries:    make(map[string]storage.TaskQueueEntry),
		enqueueErr: storage.ErrBusy,
	}
	memory, _ := NewScheduler(1, nil)
	scheduler, err := NewDurableScheduler(t.Context(), memory, store)
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := domain.NewTaskID()
	if _, err := scheduler.Enqueue(t.Context(), storage.EnqueueTask{
		ID: "queue-enqueue-busy", TaskID: taskID,
		ProviderKey: "provider-a", Reason: "waiting",
		Priority: 1, EnqueueSequence: 1,
	}); !errors.Is(err, storage.ErrBusy) {
		t.Fatalf("enqueue error = %v, want database busy", err)
	}
	if position := scheduler.Position(taskID); position != (QueuePosition{}) {
		t.Fatalf("failed durable enqueue entered memory: %#v", position)
	}

	oversized := &memoryQueueStore{
		entries: make(map[string]storage.TaskQueueEntry, maximumQueuedTasks+1),
	}
	for index := 0; index <= maximumQueuedTasks; index++ {
		id := fmt.Sprintf("queue-%04d", index)
		oversized.entries[id] = storage.TaskQueueEntry{
			ID: id, State: storage.TaskQueueStateQueued,
		}
	}
	otherMemory, _ := NewScheduler(1, nil)
	if _, err := NewDurableScheduler(
		t.Context(), otherMemory, oversized,
	); err == nil {
		t.Fatal("oversized durable queue was accepted")
	}
}

type memoryQueueStore struct {
	entries       map[string]storage.TaskQueueEntry
	enqueueErr    error
	listErr       error
	transitionErr error
	recoveries    []storage.RecoverDispatchedWorkerStart
}

func (store *memoryQueueStore) RecoverDispatchedWorkerStart(
	_ context.Context,
	input storage.RecoverDispatchedWorkerStart,
) error {
	entry, exists := store.entries[input.QueueID]
	if !exists || entry.TaskID != input.TaskID ||
		entry.State != storage.TaskQueueStateDispatched {
		return errors.New("dispatched queue entry is unavailable")
	}
	entry.State = storage.TaskQueueStateCancelled
	entry.Reason = input.Reason
	entry.Revision++
	store.entries[input.QueueID] = entry
	store.recoveries = append(store.recoveries, input)
	return nil
}

func (store *memoryQueueStore) EnqueueTask(
	_ context.Context,
	input storage.EnqueueTask,
) (storage.TaskQueueEntry, error) {
	if store.enqueueErr != nil {
		err := store.enqueueErr
		store.enqueueErr = nil
		return storage.TaskQueueEntry{}, err
	}
	if _, exists := store.entries[input.ID]; exists {
		return storage.TaskQueueEntry{}, errors.New("duplicate queue entry")
	}
	entry := storage.TaskQueueEntry{
		ID: input.ID, TaskID: input.TaskID, ProviderKey: input.ProviderKey,
		State:  storage.TaskQueueStateQueued,
		Reason: input.Reason, Priority: input.Priority,
		Resuming: input.Resuming, EnqueueSequence: input.EnqueueSequence,
		EnqueuedAt: time.Now(),
	}
	store.entries[input.ID] = entry
	return entry, nil
}

func (store *memoryQueueStore) ListQueuedTasks(
	context.Context,
	int,
) ([]storage.TaskQueueEntry, error) {
	if store.listErr != nil {
		return nil, store.listErr
	}
	var queued []storage.TaskQueueEntry
	for _, entry := range store.entries {
		if entry.State == storage.TaskQueueStateQueued {
			queued = append(queued, entry)
		}
	}
	slices.SortFunc(queued, func(left, right storage.TaskQueueEntry) int {
		if left.EnqueueSequence < right.EnqueueSequence {
			return -1
		}
		if left.EnqueueSequence > right.EnqueueSequence {
			return 1
		}
		return 0
	})
	return queued, nil
}

func (store *memoryQueueStore) TransitionQueuedTask(
	_ context.Context,
	id string,
	revision uint64,
	to storage.TaskQueueState,
) (storage.TaskQueueEntry, error) {
	if store.transitionErr != nil {
		err := store.transitionErr
		store.transitionErr = nil
		return storage.TaskQueueEntry{}, err
	}
	entry, exists := store.entries[id]
	if !exists || entry.State != storage.TaskQueueStateQueued ||
		entry.Revision != revision {
		return storage.TaskQueueEntry{}, errors.New("stale queue entry")
	}
	entry.State = to
	entry.Revision++
	store.entries[id] = entry
	return entry, nil
}
