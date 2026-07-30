package coordinator

import (
	"context"
	"errors"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

type TaskQueueStore interface {
	EnqueueTask(context.Context, storage.EnqueueTask) (storage.TaskQueueEntry, error)
	ListQueuedTasks(context.Context, int) ([]storage.TaskQueueEntry, error)
	TransitionQueuedTask(context.Context, string, uint64, storage.TaskQueueState) (storage.TaskQueueEntry, error)
	RecoverDispatchedWorkerStart(
		context.Context,
		storage.RecoverDispatchedWorkerStart,
	) error
}

// DurableScheduler keeps the SQLite queue and bounded in-memory dispatcher in
// lockstep. SQLite remains authoritative across coordinator restarts.
type DurableScheduler struct {
	mu         sync.Mutex
	scheduler  *Scheduler
	store      TaskQueueStore
	entries    map[domain.TaskID]storage.TaskQueueEntry
	dispatched map[domain.TaskID]storage.TaskQueueEntry
	closed     bool
}

func NewDurableScheduler(
	ctx context.Context,
	scheduler *Scheduler,
	store TaskQueueStore,
) (*DurableScheduler, error) {
	if scheduler == nil || store == nil {
		return nil, errors.New("scheduler and task queue store are required")
	}
	entries, err := store.ListQueuedTasks(ctx, maximumQueuedTasks+1)
	if err != nil {
		return nil, err
	}
	if len(entries) > maximumQueuedTasks {
		return nil, errors.New("durable task queue exceeds supported capacity")
	}
	durable := &DurableScheduler{
		scheduler: scheduler, store: store,
		entries:    make(map[domain.TaskID]storage.TaskQueueEntry, len(entries)),
		dispatched: make(map[domain.TaskID]storage.TaskQueueEntry),
	}
	for _, entry := range entries {
		if _, err := scheduler.Enqueue(scheduleFromQueueEntry(entry)); err != nil {
			return nil, err
		}
		durable.entries[entry.TaskID] = entry
	}
	return durable, nil
}

func (scheduler *DurableScheduler) Enqueue(
	ctx context.Context,
	input storage.EnqueueTask,
) (QueuePosition, error) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.closed {
		return QueuePosition{}, errors.New("scheduler is shutting down")
	}
	request := ScheduleRequest{
		TaskID: input.TaskID, ProviderKey: input.ProviderKey,
		QueueReason: input.Reason, Priority: input.Priority,
		Resuming: input.Resuming, EnqueuedAt: time.Unix(0, 1).UTC(),
		Sequence: input.EnqueueSequence,
	}
	position, err := scheduler.scheduler.Enqueue(request)
	if err != nil {
		return QueuePosition{}, err
	}
	entry, err := scheduler.store.EnqueueTask(ctx, input)
	if err != nil {
		removeErr := scheduler.scheduler.RemoveQueued(input.TaskID)
		return QueuePosition{}, errors.Join(err, removeErr)
	}
	scheduler.entries[entry.TaskID] = entry
	return position, nil
}

func (scheduler *DurableScheduler) Dispatch(
	ctx context.Context,
) (ScheduleRequest, bool, error) {
	return scheduler.DispatchEligible(
		ctx, func(ScheduleRequest) bool { return true },
	)
}

// DispatchEligible durably dispatches only work whose process metadata has
// been registered by the runtime. This leaves restored but unregistered queue
// rows authoritative and visible instead of starting them speculatively.
func (scheduler *DurableScheduler) DispatchEligible(
	ctx context.Context,
	eligible func(ScheduleRequest) bool,
) (ScheduleRequest, bool, error) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.closed || eligible == nil {
		return ScheduleRequest{}, false, nil
	}
	request, ok := scheduler.scheduler.DispatchEligible(eligible)
	if !ok {
		return ScheduleRequest{}, false, nil
	}
	entry, exists := scheduler.entries[request.TaskID]
	if !exists {
		_ = scheduler.scheduler.Complete(request.TaskID)
		return ScheduleRequest{}, false, errors.New("dispatched task has no durable queue entry")
	}
	dispatched, err := scheduler.store.TransitionQueuedTask(
		ctx, entry.ID, entry.Revision, storage.TaskQueueStateDispatched,
	)
	if err != nil {
		_ = scheduler.scheduler.Complete(request.TaskID)
		_, restoreErr := scheduler.scheduler.Enqueue(request)
		return ScheduleRequest{}, false, errors.Join(err, restoreErr)
	}
	delete(scheduler.entries, request.TaskID)
	scheduler.dispatched[request.TaskID] = dispatched
	return request, true, nil
}

func (scheduler *DurableScheduler) Complete(taskID domain.TaskID) error {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if err := scheduler.scheduler.Complete(taskID); err != nil {
		return err
	}
	delete(scheduler.dispatched, taskID)
	return nil
}

func (scheduler *DurableScheduler) RecoverStartFailure(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	reason string,
) error {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	entry, exists := scheduler.dispatched[taskID]
	if !exists {
		return errors.New("failed worker start has no dispatched queue entry")
	}
	if err := scheduler.store.RecoverDispatchedWorkerStart(
		ctx,
		storage.RecoverDispatchedWorkerStart{
			QueueID: entry.ID, TaskID: taskID, RunID: runID, Reason: reason,
		},
	); err != nil {
		return err
	}
	if err := scheduler.scheduler.Complete(taskID); err != nil {
		return err
	}
	delete(scheduler.dispatched, taskID)
	return nil
}

func (scheduler *DurableScheduler) Position(taskID domain.TaskID) QueuePosition {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.scheduler.Position(taskID)
}

func (scheduler *DurableScheduler) Shutdown(ctx context.Context) error {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.closed {
		return nil
	}
	scheduler.closed = true
	queued := scheduler.scheduler.Shutdown()
	var shutdownErr error
	for _, request := range queued {
		entry, exists := scheduler.entries[request.TaskID]
		if !exists {
			shutdownErr = errors.Join(
				shutdownErr,
				errors.New("queued task has no durable queue entry"),
			)
			continue
		}
		if _, err := scheduler.store.TransitionQueuedTask(
			ctx, entry.ID, entry.Revision, storage.TaskQueueStateCancelled,
		); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
			continue
		}
		delete(scheduler.entries, request.TaskID)
	}
	return shutdownErr
}

func scheduleFromQueueEntry(entry storage.TaskQueueEntry) ScheduleRequest {
	return ScheduleRequest{
		TaskID: entry.TaskID, ProviderKey: entry.ProviderKey,
		QueueReason: entry.Reason, Priority: entry.Priority,
		Resuming:   entry.Resuming,
		EnqueuedAt: entry.EnqueuedAt, Sequence: entry.EnqueueSequence,
	}
}
