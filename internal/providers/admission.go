package providers

import (
	"context"
	"errors"
	"sync"
)

// AdmissionController is the one gate every physical provider request passes
// through before it reaches the network. It bounds how many requests are ever
// concurrently in flight against a provider to a configured ceiling, and it
// can narrow that ceiling under sustained backpressure without ever widening
// it back — see Narrow.
//
// It is deliberately independent of the pricing, budget, and storage
// packages: it coordinates in-process concurrency only, so it cannot become a
// place those concerns leak into. A caller that also needs a cost bound
// enforces it separately, before or after admission as its own ordering
// requires.
type AdmissionController struct {
	mu        sync.Mutex
	slots     chan struct{}
	ceiling   int
	maximum   int
	drainDebt int
}

var errAdmissionMaximumConcurrency = errors.New(
	"admission controller maximum concurrency must be at least one",
)

// NewAdmissionController builds an admission point with the given initial
// concurrency ceiling.
func NewAdmissionController(maximumConcurrency int) (*AdmissionController, error) {
	if maximumConcurrency < 1 {
		return nil, errAdmissionMaximumConcurrency
	}
	slots := make(chan struct{}, maximumConcurrency)
	for index := 0; index < maximumConcurrency; index++ {
		slots <- struct{}{}
	}
	return &AdmissionController{
		slots:   slots,
		ceiling: maximumConcurrency,
		maximum: maximumConcurrency,
	}, nil
}

// Acquire blocks until a slot is admitted or ctx is done. The returned
// release function must be called exactly once (extra calls are safely
// ignored) to return the slot, unless the ceiling has narrowed underneath it.
func (controller *AdmissionController) Acquire(
	ctx context.Context,
) (func(), error) {
	if controller == nil {
		return func() {}, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-controller.slots:
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			controller.mu.Lock()
			defer controller.mu.Unlock()
			if controller.drainDebt > 0 {
				controller.drainDebt--
				return
			}
			controller.slots <- struct{}{}
		})
	}
	return release, nil
}

// Narrow lowers the concurrency ceiling to at most newCeiling, floored at 1
// so the controller can never deadlock every future request. It is
// monotonically decreasing: a newCeiling at or above the current ceiling is a
// no-op, matching "narrows for the remainder of the run" rather than
// oscillating back up on its own.
//
// A slot already checked out cannot be preempted, so narrowing is applied
// opportunistically to slots sitting idle in the pool right now, and the rest
// is collected as debt that the next Release calls pay down instead of
// returning their slot to the pool. Effective capacity therefore converges to
// newCeiling as in-flight requests finish, rather than requiring them to be
// cancelled.
func (controller *AdmissionController) Narrow(newCeiling int) {
	if controller == nil {
		return
	}
	if newCeiling < 1 {
		newCeiling = 1
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if newCeiling >= controller.ceiling {
		return
	}
	reduceBy := controller.ceiling - newCeiling
	controller.ceiling = newCeiling
	controller.drainDebt += reduceBy
	for controller.drainDebt > 0 {
		select {
		case <-controller.slots:
			controller.drainDebt--
		default:
			return
		}
	}
}

// Ceiling returns the current configured maximum concurrency.
func (controller *AdmissionController) Ceiling() int {
	if controller == nil {
		return 0
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.ceiling
}
