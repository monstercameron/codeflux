package coordinator

import (
	"context"
	"time"
)

// dispatchPumpFallbackInterval bounds how long queued work can wait when no
// signal arrives.
//
// Signals cover the ordinary cases: a task completes, a pause is lifted, an
// approval is granted. The ticker covers the ones nothing signals, such as a
// row restored during recovery, and it means a missed signal delays a start
// rather than stranding it.
const dispatchPumpFallbackInterval = 2 * time.Second

// startDispatchPump starts the goroutine that turns freed capacity into
// started work (M11-036, M11-037, AUDIT-018).
//
// Before this, StartNext had no production caller at all. Capacity was
// released correctly when a task completed and nothing ever asked for the next
// task, so a queued task stayed queued until a test called StartNext by hand.
// Queue position was computed and shown, and the queue did not move.
//
// The variadic fallbackInterval overrides the default fallback tick when
// supplied, and exists for AUDIT-018a's own tests: they must prove a start
// came from a signal rather than the fallback tick, and a test that merely
// finishes inside the 2s window is a probabilistic argument, not a
// structural one. A test that passes a fallback interval far longer than its
// own deadline makes the tick unable to fire before the test observes its
// assertion, which rules the tick out rather than making it merely
// unlikely. Production never supplies an override, so its behavior is
// unchanged.
func (application *Application) startDispatchPump(
	ctx context.Context, fallbackInterval ...time.Duration,
) {
	defer close(application.dispatchDone)

	interval := dispatchPumpFallbackInterval
	if len(fallbackInterval) > 0 && fallbackInterval[0] > 0 {
		interval = fallbackInterval[0]
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-application.dispatchSignal:
			application.drainDispatchQueue(ctx)
		case <-ticker.C:
			application.drainDispatchQueue(ctx)
		}
	}
}

// drainDispatchQueue starts tasks until capacity or the queue runs out.
//
// It stops on the first error rather than retrying. A start failure is
// recorded by RecoverStartFailure and needs a recovery decision, so retrying
// here would spin against a task that cannot start.
func (application *Application) drainDispatchQueue(ctx context.Context) {
	if application.runtime == nil {
		return
	}
	// The bound exists so one drain cannot occupy the pump indefinitely if
	// starts keep succeeding. Whatever is left is picked up by the next signal
	// or tick.
	const maximumStartsPerDrain = 16
	for started := 0; started < maximumStartsPerDrain; started++ {
		if ctx.Err() != nil || !application.accepting.Load() {
			return
		}
		_, ok, err := application.runtime.StartNext(ctx)
		if err != nil || !ok {
			return
		}
	}
}

// notifyDispatch asks the pump to look for startable work.
//
// The send is non-blocking against a buffered channel of one, so callers on a
// completion path are never delayed and repeated signals collapse into a
// single pass. A signal that arrives while a pass is running is kept, so work
// unblocked mid-pass is not missed.
func (application *Application) notifyDispatch() {
	if application.dispatchSignal == nil {
		return
	}
	select {
	case application.dispatchSignal <- struct{}{}:
	default:
	}
}
