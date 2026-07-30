package coordinator

import (
	"context"
	"errors"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestWorkerRuntimeEnforcesWorkerAndProviderLimitsBeforeStart(t *testing.T) {
	store := &memoryQueueStore{entries: make(map[string]storage.TaskQueueEntry)}
	memory, err := NewScheduler(2, map[string]int{"slow": 1, "fast": 2})
	if err != nil {
		t.Fatal(err)
	}
	durable, err := NewDurableScheduler(t.Context(), memory, store)
	if err != nil {
		t.Fatal(err)
	}
	starter := &recordingRuntimeStarter{}
	runtime, err := NewWorkerRuntime(durable, starter)
	if err != nil {
		t.Fatal(err)
	}
	slowOne := runtimeStartFixture(t)
	slowTwo := runtimeStartFixture(t)
	fast := runtimeStartFixture(t)
	submitRuntimeTask(t, runtime, slowOne, "slow", 1, 1)
	submitRuntimeTask(t, runtime, slowTwo, "slow", 1, 2)
	submitRuntimeTask(t, runtime, fast, "fast", 1, 3)

	first := mustStartNext(t, runtime)
	if first.RunID != slowOne.RunID {
		t.Fatalf("first lease = %#v", first)
	}
	second := mustStartNext(t, runtime)
	if second.RunID != fast.RunID {
		t.Fatalf("provider-blocked task bypassed eligible work: %#v", second)
	}
	if _, ok, err := runtime.StartNext(t.Context()); err != nil || ok {
		t.Fatalf("worker limit allowed third start: ok=%t err=%v", ok, err)
	}
	if len(starter.starts) != 2 {
		t.Fatalf("low-level starts = %d, want 2", len(starter.starts))
	}
	position := runtime.Position(slowTwo.TaskID)
	if position.Position != 1 ||
		position.Reason != "waiting for provider concurrency capacity" {
		t.Fatalf("queued provider position = %#v", position)
	}
	if store.entries[runtimeQueueID(slowTwo.TaskID)].State !=
		storage.TaskQueueStateQueued {
		t.Fatalf("excess task was not durably queued: %#v",
			store.entries[runtimeQueueID(slowTwo.TaskID)])
	}
	if err := runtime.Complete(slowOne.TaskID, slowOne.RunID); err != nil {
		t.Fatal(err)
	}
	third := mustStartNext(t, runtime)
	if third.RunID != slowTwo.RunID {
		t.Fatalf("released provider slot started %#v", third)
	}
}

func TestWorkerRuntimeRequiresExplicitRecoveryRegistration(t *testing.T) {
	restored := runtimeStartFixture(t)
	store := &memoryQueueStore{entries: map[string]storage.TaskQueueEntry{
		"queue-restored-runtime": {
			ID: "queue-restored-runtime", TaskID: restored.TaskID,
			ProviderKey: "provider", State: storage.TaskQueueStateQueued,
			Reason:   "coordinator restart requires recovery",
			Priority: 10, EnqueueSequence: 1, EnqueuedAt: time.Now(),
		},
	}}
	memory, _ := NewScheduler(1, nil)
	durable, err := NewDurableScheduler(t.Context(), memory, store)
	if err != nil {
		t.Fatal(err)
	}
	starter := &recordingRuntimeStarter{}
	runtime, err := NewWorkerRuntime(durable, starter)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := runtime.StartNext(t.Context()); err != nil || ok {
		t.Fatalf("unregistered restored task started: ok=%t err=%v", ok, err)
	}
	if len(starter.starts) != 0 ||
		store.entries["queue-restored-runtime"].State !=
			storage.TaskQueueStateQueued {
		t.Fatalf("restored work changed before recovery registration")
	}
	if position := runtime.Position(restored.TaskID); position.Position != 1 ||
		position.Reason != "coordinator restart requires recovery" {
		t.Fatalf("restored position = %#v", position)
	}

	mismatched := restored
	mismatched.TaskID, _ = domain.NewTaskID()
	if err := runtime.RegisterRecoveryStart(mismatched); err == nil {
		t.Fatal("mismatched recovery registration was accepted")
	}
	if err := runtime.RegisterRecoveryStart(restored); err != nil {
		t.Fatal(err)
	}
	if len(starter.starts) != 0 {
		t.Fatal("recovery registration started work automatically")
	}
	lease := mustStartNext(t, runtime)
	if lease.RunID != restored.RunID {
		t.Fatalf("recovered lease = %#v", lease)
	}
}

func TestWorkerRuntimeStartFailureCannotBypassNextDurableGrant(t *testing.T) {
	store := &memoryQueueStore{entries: make(map[string]storage.TaskQueueEntry)}
	memory, _ := NewScheduler(1, nil)
	durable, err := NewDurableScheduler(t.Context(), memory, store)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	starter := &recordingRuntimeStarter{
		nextErr:      errors.New("launch failed"),
		beforeReturn: cancel,
	}
	runtime, err := NewWorkerRuntime(durable, starter)
	if err != nil {
		t.Fatal(err)
	}
	failed := runtimeStartFixture(t)
	next := runtimeStartFixture(t)
	submitRuntimeTask(t, runtime, failed, "provider", 1, 1)
	submitRuntimeTask(t, runtime, next, "provider", 1, 2)

	if _, ok, err := runtime.StartNext(ctx); err == nil || ok {
		t.Fatalf("failed low-level start = ok=%t err=%v", ok, err)
	}
	failedEntry := store.entries[runtimeQueueID(failed.TaskID)]
	if failedEntry.State != storage.TaskQueueStateCancelled ||
		len(store.recoveries) != 1 ||
		store.recoveries[0].RunID != failed.RunID {
		t.Fatalf("failed start recovery = %#v / %#v", failedEntry, store.recoveries)
	}
	lease := mustStartNext(t, runtime)
	if lease.RunID != next.RunID {
		t.Fatalf("slot was not safely released after failed start: %#v", lease)
	}
	if len(starter.starts) != 2 {
		t.Fatalf("low-level start calls = %d", len(starter.starts))
	}
	if _, ok, err := runtime.StartNext(t.Context()); err != nil || ok {
		t.Fatalf("failed task was retried without recovery: ok=%t err=%v", ok, err)
	}
}

func TestWorkerRuntimeShutdownCancelsQueuedWorkBeforeAnyStart(t *testing.T) {
	store := &memoryQueueStore{entries: make(map[string]storage.TaskQueueEntry)}
	memory, _ := NewScheduler(1, nil)
	durable, err := NewDurableScheduler(t.Context(), memory, store)
	if err != nil {
		t.Fatal(err)
	}
	starter := &recordingRuntimeStarter{}
	runtime, err := NewWorkerRuntime(durable, starter)
	if err != nil {
		t.Fatal(err)
	}
	start := runtimeStartFixture(t)
	submitRuntimeTask(t, runtime, start, "provider", 1, 1)
	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.entries[runtimeQueueID(start.TaskID)].State !=
		storage.TaskQueueStateCancelled {
		t.Fatalf("shutdown queue state = %#v",
			store.entries[runtimeQueueID(start.TaskID)])
	}
	if _, ok, err := runtime.StartNext(t.Context()); err != nil || ok {
		t.Fatalf("shutdown runtime started work: ok=%t err=%v", ok, err)
	}
	if len(starter.starts) != 0 {
		t.Fatal("shutdown invoked low-level worker starter")
	}
	late := runtimeStartFixture(t)
	if _, err := runtime.Submit(
		t.Context(),
		runtimeQueueInput(late.TaskID, "provider", 1, 2),
		late,
	); err == nil {
		t.Fatal("shutdown runtime accepted new work")
	}
}

type recordingRuntimeStarter struct {
	starts       []StartWorker
	nextErr      error
	beforeReturn func()
}

func (starter *recordingRuntimeStarter) Start(
	_ context.Context,
	input StartWorker,
) (storage.WorkerLease, error) {
	starter.starts = append(starter.starts, input)
	if starter.beforeReturn != nil {
		starter.beforeReturn()
		starter.beforeReturn = nil
	}
	if starter.nextErr != nil {
		err := starter.nextErr
		starter.nextErr = nil
		return storage.WorkerLease{}, err
	}
	return storage.WorkerLease{
		ID: input.LeaseID, TaskID: input.TaskID, RunID: input.RunID,
		State: storage.WorkerLeaseStarting,
	}, nil
}

func submitRuntimeTask(
	t *testing.T,
	runtime *WorkerRuntime,
	start StartWorker,
	provider string,
	priority int,
	sequence uint64,
) {
	t.Helper()
	position, err := runtime.Submit(
		t.Context(),
		runtimeQueueInput(start.TaskID, provider, priority, sequence),
		start,
	)
	if err != nil {
		t.Fatal(err)
	}
	if position.Position < 1 || position.Reason == "" {
		t.Fatalf("submitted queue position = %#v", position)
	}
}

func runtimeQueueInput(
	taskID domain.TaskID,
	provider string,
	priority int,
	sequence uint64,
) storage.EnqueueTask {
	return storage.EnqueueTask{
		ID: runtimeQueueID(taskID), TaskID: taskID,
		ProviderKey: provider, Reason: "waiting for runtime capacity",
		Priority: priority, EnqueueSequence: sequence,
	}
}

func runtimeQueueID(taskID domain.TaskID) string {
	return "queue-runtime-" + taskID.String()
}

func runtimeStartFixture(t *testing.T) StartWorker {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	return StartWorker{
		LeaseID: "lease-runtime-" + runID.String(),
		TaskID:  taskID, RunID: runID, WorktreePath: t.TempDir(),
		ToolSchemaVersion:   1,
		CoordinatorEndpoint: "http://127.0.0.1:43117",
		Executable:          "worker-fixture",
	}
}

func mustStartNext(
	t *testing.T,
	runtime *WorkerRuntime,
) storage.WorkerLease {
	t.Helper()
	lease, ok, err := runtime.StartNext(t.Context())
	if err != nil || !ok {
		t.Fatalf("start next = %#v, %t, %v", lease, ok, err)
	}
	return lease
}
