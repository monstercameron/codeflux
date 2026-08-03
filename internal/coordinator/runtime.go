package coordinator

import (
	"context"
	"errors"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// RuntimeWorkerStarter is the low-level subprocess boundary. WorkerRuntime is
// the coordinator entry point that grants durable concurrency before invoking
// this boundary.
type RuntimeWorkerStarter interface {
	Start(context.Context, StartWorker) (storage.WorkerLease, error)
}

// WorkerRuntime coordinates durable scheduling with subprocess ownership.
// Restored queue rows are intentionally ineligible until RegisterRecoveryStart
// supplies fresh, explicitly reviewed process metadata.
type WorkerRuntime struct {
	mu         sync.Mutex
	scheduler  *DurableScheduler
	starter    RuntimeWorkerStarter
	registered map[domain.TaskID]StartWorker
	runOwners  map[domain.RunID]domain.TaskID
	active     map[domain.TaskID]StartWorker
	closed     bool
	// notify asks the dispatch pump to look for startable work as soon as a
	// restored queue row becomes eligible, rather than leaving it to the
	// pump's fallback tick (AUDIT-018a). It is nil until SetDispatchNotifier
	// installs it, and every call site treats a nil notify as "no pump to
	// signal" rather than an error.
	notify func()
}

func NewWorkerRuntime(
	scheduler *DurableScheduler,
	starter RuntimeWorkerStarter,
) (*WorkerRuntime, error) {
	if scheduler == nil || starter == nil {
		return nil, errors.New("durable scheduler and worker starter are required")
	}
	return &WorkerRuntime{
		scheduler: scheduler, starter: starter,
		registered: make(map[domain.TaskID]StartWorker),
		runOwners:  make(map[domain.RunID]domain.TaskID),
		active:     make(map[domain.TaskID]StartWorker),
	}, nil
}

// Submit persists a visible queue request and registers its fresh process
// metadata atomically with respect to runtime dispatch.
func (runtime *WorkerRuntime) Submit(
	ctx context.Context,
	queue storage.EnqueueTask,
	start StartWorker,
) (QueuePosition, error) {
	if err := validateRuntimeStart(queue.TaskID, start); err != nil {
		return QueuePosition{}, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.closed {
		return QueuePosition{}, errors.New("worker runtime is shutting down")
	}
	if err := runtime.canRegister(start); err != nil {
		return QueuePosition{}, err
	}
	position, err := runtime.scheduler.Enqueue(ctx, queue)
	if err != nil {
		return QueuePosition{}, err
	}
	runtime.register(start)
	return position, nil
}

// RegisterRecoveryStart makes one restored queue row eligible after recovery
// code has rebuilt and approved fresh process metadata. It does not start
// work itself, but it does ask the dispatch pump to look: a row restored
// across a coordinator restart is exactly the case the pump's fallback tick
// exists for, and without a signal here it would sit for up to that whole
// interval even though it became startable the instant registration
// succeeded (AUDIT-018a).
func (runtime *WorkerRuntime) RegisterRecoveryStart(start StartWorker) error {
	if err := validateRuntimeStart(start.TaskID, start); err != nil {
		return err
	}
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return errors.New("worker runtime is shutting down")
	}
	if position := runtime.scheduler.Position(start.TaskID); position.Position == 0 {
		runtime.mu.Unlock()
		return errors.New("recovery worker has no durable queued task")
	}
	if err := runtime.canRegister(start); err != nil {
		runtime.mu.Unlock()
		return err
	}
	runtime.register(start)
	notify := runtime.notify
	runtime.mu.Unlock()
	// The signal is raised only after registration is durably visible to the
	// scheduler (registration itself is in-memory but guarded by the same
	// lock a drain takes), so a drain the pump runs because of this signal
	// never observes the row as still ineligible. The notify reference is
	// read and released before calling it, so a pump signal can never be
	// sent while this runtime's own lock is held.
	if notify != nil {
		notify()
	}
	return nil
}

// SetDispatchNotifier installs the dispatch pump signal this runtime raises
// after a restored queue row becomes eligible (AUDIT-018a). It is optional:
// a runtime without one behaves exactly as before, relying on the pump's
// fallback tick. The send notify performs must be non-blocking, matching
// notifyDispatch's buffer-of-one channel, so recovery registration is never
// delayed by it.
func (runtime *WorkerRuntime) SetDispatchNotifier(notify func()) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.notify = notify
}

// StartNext grants one durable worker/provider slot before starting a process.
// Unregistered restored rows are skipped but remain queued and visible.
func (runtime *WorkerRuntime) StartNext(
	ctx context.Context,
) (storage.WorkerLease, bool, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.closed {
		return storage.WorkerLease{}, false, nil
	}
	request, ok, err := runtime.scheduler.DispatchEligible(
		ctx,
		func(request ScheduleRequest) bool {
			_, exists := runtime.registered[request.TaskID]
			return exists
		},
	)
	if err != nil || !ok {
		return storage.WorkerLease{}, false, err
	}
	start := runtime.registered[request.TaskID]
	lease, startErr := runtime.starter.Start(ctx, start)
	if startErr != nil {
		delete(runtime.registered, request.TaskID)
		delete(runtime.runOwners, start.RunID)
		recoveryContext, cancelRecovery := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancelRecovery()
		recoveryErr := runtime.scheduler.RecoverStartFailure(
			recoveryContext,
			request.TaskID,
			start.RunID,
			"worker process failed to start; retry requires a recovery choice",
		)
		return storage.WorkerLease{}, false, errors.Join(startErr, recoveryErr)
	}
	delete(runtime.registered, request.TaskID)
	runtime.active[request.TaskID] = start
	return lease, true, nil
}

// Complete releases the exact worker and provider slot after the subprocess
// owner has durably recorded its terminal lease state.
func (runtime *WorkerRuntime) Complete(
	taskID domain.TaskID,
	runID domain.RunID,
) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	start, exists := runtime.active[taskID]
	if !exists || start.RunID != runID {
		return errors.New("worker runtime completion does not own task and run")
	}
	if err := runtime.scheduler.Complete(taskID); err != nil {
		return err
	}
	delete(runtime.active, taskID)
	delete(runtime.runOwners, runID)
	return nil
}

func (runtime *WorkerRuntime) Position(taskID domain.TaskID) QueuePosition {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.scheduler.Position(taskID)
}

// Shutdown rejects new submissions and starts, then applies the durable
// scheduler's explicit cancellation policy to queued tasks.
func (runtime *WorkerRuntime) Shutdown(ctx context.Context) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.closed {
		return nil
	}
	runtime.closed = true
	err := runtime.scheduler.Shutdown(ctx)
	runtime.registered = make(map[domain.TaskID]StartWorker)
	for runID, taskID := range runtime.runOwners {
		if _, active := runtime.active[taskID]; !active {
			delete(runtime.runOwners, runID)
		}
	}
	return err
}

func (runtime *WorkerRuntime) canRegister(start StartWorker) error {
	if _, exists := runtime.registered[start.TaskID]; exists {
		return errors.New("task already has registered worker metadata")
	}
	if _, exists := runtime.active[start.TaskID]; exists {
		return errors.New("task already has an active worker")
	}
	if owner, exists := runtime.runOwners[start.RunID]; exists &&
		owner != start.TaskID {
		return errors.New("run already belongs to another runtime task")
	}
	return nil
}

func (runtime *WorkerRuntime) register(start StartWorker) {
	runtime.registered[start.TaskID] = start
	runtime.runOwners[start.RunID] = start.TaskID
}

func validateRuntimeStart(taskID domain.TaskID, start StartWorker) error {
	if taskID.IsZero() || start.TaskID != taskID || start.RunID.IsZero() ||
		start.LeaseID == "" || start.Executable == "" {
		return errors.New("queued task and worker start metadata do not match")
	}
	return nil
}
