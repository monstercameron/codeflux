package coordinator

import (
	"context"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/storage"
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
