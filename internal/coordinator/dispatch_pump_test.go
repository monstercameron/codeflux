package coordinator

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestAUDIT018_FreedCapacityStartsQueuedWork covers AUDIT-018, reconciling
// M11-036 and M11-037.
//
// Releasing a slot and asking for the next task are two steps and only the
// first was wired. StartNext is called when a run is enqueued, and
// WorkerRuntime.Complete is called when a worker exits through the
// supervisor's completion observer, but nothing looked at the queue after
// capacity freed. A task queued beyond the limit therefore waited for an
// unrelated launch rather than for a slot.
func TestAUDIT018_FreedCapacityStartsQueuedWork(t *testing.T) {
	runtime, starter := dispatchPumpRuntime(t, 1)

	first := runtimeStartFixture(t)
	second := runtimeStartFixture(t)
	submitRuntimeTask(t, runtime, first, "fixture", 1, 1)
	submitRuntimeTask(t, runtime, second, "fixture", 1, 2)

	// The limit is one, so the first starts and the second waits.
	if _, ok, err := runtime.StartNext(t.Context()); err != nil || !ok {
		t.Fatalf("the first task did not start: ok=%v err=%v", ok, err)
	}
	if _, ok, err := runtime.StartNext(t.Context()); err != nil || ok {
		t.Fatalf("a second task started past the limit: ok=%v err=%v", ok, err)
	}
	if position := runtime.Position(second.TaskID); position.Reason == "" {
		t.Error("the queued task has no visible reason for waiting")
	}

	// Freeing the slot on its own leaves the queue where it is. This is the
	// state the product was in.
	if err := runtime.Complete(first.TaskID, first.RunID); err != nil {
		t.Fatalf("completing the first task: %v", err)
	}
	if len(starter.starts) != 1 {
		t.Fatalf("started %d tasks before any pump ran, want 1", len(starter.starts))
	}

	// The pump turns the freed slot into a start.
	application := newDispatchPumpApplication(runtime, true)
	application.drainDispatchQueue(t.Context())

	if len(starter.starts) != 2 {
		t.Fatalf("started %d tasks after the pump ran, want 2", len(starter.starts))
	}
	if starter.starts[1].TaskID != second.TaskID {
		t.Error("the pump started a task other than the one that was queued")
	}
}

// TestAUDIT018_ThePumpStopsWhenCapacityRunsOut proves a drain terminates
// rather than spinning against a full runtime.
func TestAUDIT018_ThePumpStopsWhenCapacityRunsOut(t *testing.T) {
	runtime, starter := dispatchPumpRuntime(t, 1)
	submitRuntimeTask(t, runtime, runtimeStartFixture(t), "fixture", 1, 1)
	submitRuntimeTask(t, runtime, runtimeStartFixture(t), "fixture", 1, 2)

	application := newDispatchPumpApplication(runtime, true)
	done := make(chan struct{})
	go func() {
		application.drainDispatchQueue(t.Context())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the drain did not stop when capacity ran out")
	}
	if len(starter.starts) != 1 {
		t.Fatalf("started %d tasks against a limit of one", len(starter.starts))
	}
}

// TestAUDIT018_ThePumpDoesNothingWhileNotAccepting proves shutdown ordering: a
// coordinator that has stopped accepting mutations must not start new work
// while it is draining.
func TestAUDIT018_ThePumpDoesNothingWhileNotAccepting(t *testing.T) {
	runtime, starter := dispatchPumpRuntime(t, 2)
	submitRuntimeTask(t, runtime, runtimeStartFixture(t), "fixture", 1, 1)

	application := newDispatchPumpApplication(runtime, false)
	application.drainDispatchQueue(t.Context())

	if len(starter.starts) != 0 {
		t.Fatalf("started %d tasks while refusing mutations", len(starter.starts))
	}
}

// TestAUDIT018_ACancelledContextStopsTheDrain proves the cancellation path.
func TestAUDIT018_ACancelledContextStopsTheDrain(t *testing.T) {
	runtime, starter := dispatchPumpRuntime(t, 4)
	submitRuntimeTask(t, runtime, runtimeStartFixture(t), "fixture", 1, 1)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	application := newDispatchPumpApplication(runtime, true)
	application.drainDispatchQueue(ctx)

	if len(starter.starts) != 0 {
		t.Fatalf("started %d tasks against a cancelled context", len(starter.starts))
	}
}

// TestAUDIT018_SignallingIsNonBlockingAndCoalesces proves the completion path
// is never delayed by the pump.
func TestAUDIT018_SignallingIsNonBlockingAndCoalesces(t *testing.T) {
	application := &Application{dispatchSignal: make(chan struct{}, 1)}

	done := make(chan struct{})
	go func() {
		for range 100 {
			application.notifyDispatch()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("notifyDispatch blocked its caller")
	}
	if len(application.dispatchSignal) != 1 {
		t.Fatalf("signal depth = %d, want repeated signals to collapse into one",
			len(application.dispatchSignal))
	}

	// The zero-value application has no channel and must not panic.
	(&Application{}).notifyDispatch()
}

// dispatchPumpRuntime builds a runtime over the in-memory scheduler with the
// package's recording starter, so a start is observable without a process.
func dispatchPumpRuntime(
	t *testing.T, limit int,
) (*WorkerRuntime, *recordingRuntimeStarter) {
	t.Helper()
	memory, err := NewScheduler(limit, nil)
	if err != nil {
		t.Fatal(err)
	}
	durable, err := NewDurableScheduler(t.Context(), memory,
		&memoryQueueStore{entries: make(map[string]storage.TaskQueueEntry)})
	if err != nil {
		t.Fatal(err)
	}
	starter := &recordingRuntimeStarter{}
	runtime, err := NewWorkerRuntime(durable, starter)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, starter
}

func newDispatchPumpApplication(
	runtime *WorkerRuntime, accepting bool,
) *Application {
	application := &Application{
		runtime:        runtime,
		dispatchSignal: make(chan struct{}, 1),
	}
	application.accepting.Store(accepting)
	return application
}

// TestAUDIT018a_PauseLiftedSignalsPumpBeforeFallbackTick proves
// TaskControlService.ResumeTask signals the dispatch pump immediately after
// a pause is durably lifted: the signal never blocks the resume, and a task
// queued behind that signal starts promptly and correctly rather than
// waiting for the fallback tick.
//
// The pump's own fallback interval is set to an hour here, far longer than
// this test's deadlines, so a start observed within those deadlines cannot
// be explained by the tick firing early: the tick structurally cannot have
// fired yet. That excludes the fallback as the cause rather than merely
// making it improbable, and it is a real clock only in the sense that the
// assertions still race a deadline — the margin is four orders of
// magnitude, not a tight one.
func TestAUDIT018a_PauseLiftedSignalsPumpBeforeFallbackTick(t *testing.T) {
	memory, err := NewScheduler(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	durable, err := NewDurableScheduler(t.Context(), memory,
		&memoryQueueStore{entries: make(map[string]storage.TaskQueueEntry)})
	if err != nil {
		t.Fatal(err)
	}
	starter := newSignallingStarter()
	runtime, err := NewWorkerRuntime(durable, starter)
	if err != nil {
		t.Fatal(err)
	}
	// Both tasks are queued and registered but never manually started: the
	// pre-AUDIT-018 condition. Capacity is one, so fairness requires the
	// earlier-sequenced task to start, not merely some task.
	first := runtimeStartFixture(t)
	second := runtimeStartFixture(t)
	submitRuntimeTask(t, runtime, first, "fixture", 1, 1)
	submitRuntimeTask(t, runtime, second, "fixture", 1, 2)

	application := &Application{
		runtime: runtime, dispatchSignal: make(chan struct{}, 1),
		dispatchDone: make(chan struct{}),
	}
	application.accepting.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	go application.startDispatchPump(ctx, time.Hour)
	t.Cleanup(func() {
		cancel()
		<-application.dispatchDone
	})

	taskID := newTaskControlTaskID(t)
	runID := newTaskControlRunID(t)
	store := &taskControlStoreStub{
		snapshot: taskControlSnapshot(taskID, runID, storage.TaskControlPaused),
	}
	var stateWhenSignalled domain.TaskState
	service, err := NewTaskControlService(TaskControlDependencies{
		Store: store, Actions: NewActiveActionRegistry(),
		Workers:     &taskControlWorkersStub{},
		Checkpoints: &taskControlCheckpointStub{},
		Facts:       taskControlFactsStub{},
		Resume: &resumeVerifierStub{
			assessment: compatibleResumeAssessment(PausedEditsUnchanged, nil),
		},
		NotifyDispatch: func() {
			// Read the durable snapshot from inside the signal itself: if the
			// pump were signalled before the resume committed, this would
			// still read the paused state instead of the resumed one.
			store.mu.Lock()
			stateWhenSignalled = store.snapshot.TaskState
			store.mu.Unlock()
			application.notifyDispatch()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	resumeDone := make(chan struct{})
	go func() {
		if _, err := service.ResumeTask(t.Context(), ResumeTaskInput{
			ResumedEventID:       newTaskControlEventID(t),
			BlockedEventID:       newTaskControlEventID(t),
			TaskID:               taskID,
			RunID:                runID,
			ExpectedTaskRevision: 7,
			ExpectedRunRevision:  11,
			IdempotencyKey:       "audit-018a-pause-lifted",
		}); err != nil {
			t.Errorf("resume failed: %v", err)
		}
		close(resumeDone)
	}()
	select {
	case <-resumeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ResumeTask did not return; the dispatch signal must never block it")
	}
	if stateWhenSignalled != domain.TaskStateRunning {
		t.Fatalf(
			"pump was signalled before the resume was durably recorded: task state = %s",
			stateWhenSignalled,
		)
	}

	select {
	case got := <-starter.started:
		if got.TaskID != first.TaskID {
			t.Fatalf("started %#v, want the earlier-queued task", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no task started promptly after the pause was lifted")
	}
	select {
	case extra := <-starter.started:
		t.Fatalf("unexpected extra start: %#v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestAUDIT018a_ApprovalGrantedSignalsPumpBeforeFallbackTick proves
// TaskLifecycleAdapter.ApproveTaskPlan signals the dispatch pump immediately
// after a plan approval is durably granted: the signal never blocks the
// approval, and a task queued behind that signal starts promptly and
// correctly rather than waiting for the fallback tick.
//
// As with the pause test above, the pump's fallback interval is set to an
// hour, structurally excluding the tick as the explanation for any start
// observed inside this test's much shorter deadlines.
func TestAUDIT018a_ApprovalGrantedSignalsPumpBeforeFallbackTick(t *testing.T) {
	ctx := context.Background()
	repositories, retrievalService := mustOpenRetrievalRepositories(t)
	preflight, err := NewTaskPreflightService(repositories, retrievalService)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewTaskLifecycleAdapter(preflight, repositories, &recordingRunLauncher{})
	if err != nil {
		t.Fatal(err)
	}
	thread := mustIntakeFixtureThread(t, repositories)
	created, err := adapter.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 thread.ID,
		Requirement:              "Add a readiness probe to the server.",
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("c", 40),
		BaselineModelRevision:    "fixture-model-2026-08-01",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"internal/server"},
		IdempotencyKey:           "audit-018a-approval-create",
	})
	if err != nil {
		t.Fatal(err)
	}

	memory, err := NewScheduler(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	durable, err := NewDurableScheduler(t.Context(), memory,
		&memoryQueueStore{entries: make(map[string]storage.TaskQueueEntry)})
	if err != nil {
		t.Fatal(err)
	}
	starter := newSignallingStarter()
	runtime, err := NewWorkerRuntime(durable, starter)
	if err != nil {
		t.Fatal(err)
	}
	// Standing in for "whatever this approval unblocks": the dispatch pump
	// reacts to a bare signal the same way regardless of cause, exactly as
	// AUDIT-018 already established for the completion path, so wiring the
	// same signal here and observing a prompt, correct start proves the
	// mechanism this ticket adds. Capacity is one, so fairness requires the
	// earlier-sequenced task to start, not merely some task.
	first := runtimeStartFixture(t)
	second := runtimeStartFixture(t)
	submitRuntimeTask(t, runtime, first, "fixture", 1, 1)
	submitRuntimeTask(t, runtime, second, "fixture", 1, 2)

	application := &Application{
		runtime: runtime, dispatchSignal: make(chan struct{}, 1),
		dispatchDone: make(chan struct{}),
	}
	application.accepting.Store(true)
	pumpCtx, cancel := context.WithCancel(t.Context())
	go application.startDispatchPump(pumpCtx, time.Hour)
	t.Cleanup(func() {
		cancel()
		<-application.dispatchDone
	})

	var stateWhenSignalled domain.TaskState
	adapter.SetDispatchNotifier(func() {
		// Read the task's durable state from inside the signal itself: if
		// the pump were signalled before the approval committed, this would
		// still read the pre-approval state instead of Ready.
		if current, readErr := repositories.GetTask(
			context.Background(), created.TaskID,
		); readErr == nil {
			stateWhenSignalled = current.State
		}
		application.notifyDispatch()
	})

	approveDone := make(chan struct{})
	go func() {
		if _, err := adapter.ApproveTaskPlan(context.Background(), transport.ApprovePlanCommand{
			TaskID: created.TaskID, ExpectedRevision: created.Revision,
			IdempotencyKey: "audit-018a-approval-grant",
		}); err != nil {
			t.Errorf("approving the plan failed: %v", err)
		}
		close(approveDone)
	}()
	select {
	case <-approveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ApproveTaskPlan did not return; the dispatch signal must never block it")
	}
	if stateWhenSignalled != domain.TaskStateReady {
		t.Fatalf(
			"pump was signalled before the approval was durably recorded: task state = %s",
			stateWhenSignalled,
		)
	}

	select {
	case got := <-starter.started:
		if got.TaskID != first.TaskID {
			t.Fatalf("started %#v, want the earlier-queued task", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no task started promptly after the approval was granted")
	}
	select {
	case extra := <-starter.started:
		t.Fatalf("unexpected extra start: %#v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestAUDIT018a_RestartRegistrationSignalsPumpBeforeFallbackTick proves
// WorkerRuntime.RegisterRecoveryStart signals the dispatch pump immediately
// after a restored queue row becomes eligible, so work queued across a
// coordinator restart starts promptly rather than waiting for the fallback
// tick, and that the correct row starts when more than one is restored.
func TestAUDIT018a_RestartRegistrationSignalsPumpBeforeFallbackTick(t *testing.T) {
	restored := runtimeStartFixture(t)
	other := runtimeStartFixture(t)
	store := &memoryQueueStore{entries: map[string]storage.TaskQueueEntry{
		"queue-restored-a": {
			ID: "queue-restored-a", TaskID: restored.TaskID,
			ProviderKey: "provider", State: storage.TaskQueueStateQueued,
			Reason:   "coordinator restart requires recovery",
			Priority: 10, EnqueueSequence: 1, EnqueuedAt: time.Now(),
		},
		"queue-restored-b": {
			ID: "queue-restored-b", TaskID: other.TaskID,
			ProviderKey: "provider", State: storage.TaskQueueStateQueued,
			Reason:   "coordinator restart requires recovery",
			Priority: 10, EnqueueSequence: 2, EnqueuedAt: time.Now(),
		},
	}}
	memory, err := NewScheduler(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	durable, err := NewDurableScheduler(t.Context(), memory, store)
	if err != nil {
		t.Fatal(err)
	}
	starter := newSignallingStarter()
	runtime, err := NewWorkerRuntime(durable, starter)
	if err != nil {
		t.Fatal(err)
	}

	application := &Application{
		runtime: runtime, dispatchSignal: make(chan struct{}, 1),
		dispatchDone: make(chan struct{}),
	}
	application.accepting.Store(true)
	runtime.SetDispatchNotifier(application.notifyDispatch)
	ctx, cancel := context.WithCancel(t.Context())
	go application.startDispatchPump(ctx, time.Hour)
	t.Cleanup(func() {
		cancel()
		<-application.dispatchDone
	})

	if _, ok, err := runtime.StartNext(t.Context()); err != nil || ok {
		t.Fatalf(
			"an unregistered restored row started before recovery registration: ok=%t err=%v",
			ok, err,
		)
	}

	// Registering the earlier-sequenced row first proves fairness under
	// contention for the single slot: it is the one that starts.
	if err := runtime.RegisterRecoveryStart(restored); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-starter.started:
		if got.TaskID != restored.TaskID {
			t.Fatalf("started %#v, want the earliest-sequenced restored task", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no task started promptly after recovery registration")
	}

	if err := runtime.RegisterRecoveryStart(other); err != nil {
		t.Fatal(err)
	}
	select {
	case extra := <-starter.started:
		t.Fatalf("second restored row started despite exhausted capacity: %#v", extra)
	case <-time.After(200 * time.Millisecond):
	}
	if position := runtime.Position(other.TaskID); position.Position != 1 {
		t.Fatalf("second restored row position = %#v, want still queued", position)
	}
}

// signallingStarter records every low-level start under a mutex and posts
// each one to a buffered channel, so a test can wait for a start the
// dispatch pump makes on its own goroutine without racing a plain slice
// across goroutines and without falling back to a fixed sleep.
type signallingStarter struct {
	mu      sync.Mutex
	starts  []StartWorker
	started chan StartWorker
}

func newSignallingStarter() *signallingStarter {
	return &signallingStarter{started: make(chan StartWorker, 16)}
}

func (starter *signallingStarter) Start(
	_ context.Context, input StartWorker,
) (storage.WorkerLease, error) {
	starter.mu.Lock()
	starter.starts = append(starter.starts, input)
	starter.mu.Unlock()
	starter.started <- input
	return storage.WorkerLease{
		ID: input.LeaseID, TaskID: input.TaskID, RunID: input.RunID,
		State: storage.WorkerLeaseStarting,
	}, nil
}
