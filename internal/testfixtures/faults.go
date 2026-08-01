package testfixtures

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// FaultPoint names a place where a fault can be injected (M22-036..050).
//
// The set is closed and named after the REAL boundary each fault crosses,
// not after the code that happens to be there today. A fault a test can
// describe but the system cannot actually experience would prove nothing.
type FaultPoint string

const (
	FaultWorkerDuringRepositoryRead FaultPoint = "worker-during-repository-read"
	FaultWorkerDuringFileEdit       FaultPoint = "worker-during-file-edit"
	FaultWorkerDuringCommand        FaultPoint = "worker-during-command-execution"
	FaultWorkerDuringModelStream    FaultPoint = "worker-during-model-streaming"
	FaultCoordinatorBeforeCommit    FaultPoint = "coordinator-before-event-commit"
	FaultCoordinatorAfterCommit     FaultPoint = "coordinator-after-commit-before-delivery"
	FaultBrowserDuringApproval      FaultPoint = "browser-disconnect-during-approval"
	FaultBrowserDuringBudgetChange  FaultPoint = "browser-disconnect-during-budget-increase"
	FaultDiskFullOnEventAppend      FaultPoint = "disk-exhausted-on-event-append"
	FaultDatabaseBusyTimeout        FaultPoint = "database-busy-timeout"
	FaultWorktreeMissingOrCorrupt   FaultPoint = "worktree-missing-or-corrupt"
	FaultProviderRateLimited        FaultPoint = "provider-rate-limited"
	FaultProviderPartialThenFailure FaultPoint = "provider-partial-stream-then-failure"
	FaultProviderDelayedUsage       FaultPoint = "provider-delayed-usage-report"
	FaultCommandTimeoutWithChildren FaultPoint = "command-timeout-with-child-processes"
)

// AllFaultPoints returns every declared injection point.
func AllFaultPoints() []FaultPoint {
	return []FaultPoint{
		FaultWorkerDuringRepositoryRead, FaultWorkerDuringFileEdit,
		FaultWorkerDuringCommand, FaultWorkerDuringModelStream,
		FaultCoordinatorBeforeCommit, FaultCoordinatorAfterCommit,
		FaultBrowserDuringApproval, FaultBrowserDuringBudgetChange,
		FaultDiskFullOnEventAppend, FaultDatabaseBusyTimeout,
		FaultWorktreeMissingOrCorrupt, FaultProviderRateLimited,
		FaultProviderPartialThenFailure, FaultProviderDelayedUsage,
		FaultCommandTimeoutWithChildren,
	}
}

// SafeOutcome is what a system must offer the user after a fault
// (docs/plan.md Layer 12: "failure at every tested durable boundary yields a
// safe user choice").
type SafeOutcome string

const (
	// OutcomeResumable means the work can continue from a known-good point.
	OutcomeResumable SafeOutcome = "resumable"
	// OutcomeRetryable means the operation may be retried without
	// duplicating an external effect.
	OutcomeRetryable SafeOutcome = "retryable"
	// OutcomeRequiresReconciliation means the outcome is genuinely ambiguous
	// and must be reconciled before anything else happens. This is the
	// honest answer when the system cannot know what happened.
	OutcomeRequiresReconciliation SafeOutcome = "requires-reconciliation"
	// OutcomeTerminatedCleanly means the work stopped with no partial
	// durable effect.
	OutcomeTerminatedCleanly SafeOutcome = "terminated-cleanly"
)

// Valid reports whether an outcome is one of the four safe answers.
func (outcome SafeOutcome) Valid() bool {
	switch outcome {
	case OutcomeResumable, OutcomeRetryable, OutcomeRequiresReconciliation, OutcomeTerminatedCleanly:
		return true
	default:
		return false
	}
}

// ErrInjectedFault is returned by an armed injector.
var ErrInjectedFault = errors.New("injected fault")

// FaultInjector arms a fault at a named point and records whether it fired.
//
// A fault that never fires is a test that proved nothing, so Fired() is
// deliberately observable and every fault test asserts it.
type FaultInjector struct {
	mutex  sync.Mutex
	armed  map[FaultPoint]int
	fired  map[FaultPoint]int
	reason map[FaultPoint]string
}

// NewFaultInjector returns an injector with nothing armed.
func NewFaultInjector() *FaultInjector {
	return &FaultInjector{
		armed:  map[FaultPoint]int{},
		fired:  map[FaultPoint]int{},
		reason: map[FaultPoint]string{},
	}
}

// Arm schedules a fault to fire the next `times` times the point is reached.
func (injector *FaultInjector) Arm(point FaultPoint, times int, reason string) error {
	if times <= 0 {
		return fmt.Errorf("arming %q requires a positive count", point)
	}
	if reason == "" {
		return fmt.Errorf("arming %q requires a reason, so a failure message says what was simulated", point)
	}
	injector.mutex.Lock()
	defer injector.mutex.Unlock()
	injector.armed[point] += times
	injector.reason[point] = reason
	return nil
}

// Check is called at an injection point. It returns ErrInjectedFault while
// the point remains armed.
func (injector *FaultInjector) Check(point FaultPoint) error {
	injector.mutex.Lock()
	defer injector.mutex.Unlock()
	if injector.armed[point] <= 0 {
		return nil
	}
	injector.armed[point]--
	injector.fired[point]++
	return fmt.Errorf("%w at %s: %s", ErrInjectedFault, point, injector.reason[point])
}

// Fired reports how many times a point actually fired.
func (injector *FaultInjector) Fired(point FaultPoint) int {
	injector.mutex.Lock()
	defer injector.mutex.Unlock()
	return injector.fired[point]
}

// Remaining reports how many arms are still pending, so a test can prove the
// code under test reached the point as often as expected.
func (injector *FaultInjector) Remaining(point FaultPoint) int {
	injector.mutex.Lock()
	defer injector.mutex.Unlock()
	return injector.armed[point]
}

// ExternalEffect records one externally visible action attempted during a
// fault scenario, keyed by its logical identity.
//
// docs/plan.md sets the maximum acceptable duplicate external-effect intent
// after retry, reconnect, or replay at ZERO, so a fault test's real job is
// counting distinct attempts per identity, not merely observing an error.
type ExternalEffect struct {
	LogicalIdentity string
	Attempt         int
	Outcome         SafeOutcome
	At              time.Time
}

// EffectLedger records external-effect attempts across a fault scenario.
type EffectLedger struct {
	mutex   sync.Mutex
	effects []ExternalEffect
	clock   Clock
}

// NewEffectLedger returns a ledger stamped by clock.
func NewEffectLedger(clock Clock) *EffectLedger {
	if clock == nil {
		clock = NewFixedClock()
	}
	return &EffectLedger{clock: clock}
}

// Record appends one attempt.
func (ledger *EffectLedger) Record(identity string, outcome SafeOutcome) error {
	if identity == "" {
		return errors.New("an external effect must carry a logical identity or duplication cannot be detected")
	}
	if !outcome.Valid() {
		return fmt.Errorf("outcome %q is not one of the four safe answers", outcome)
	}
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	ledger.effects = append(ledger.effects, ExternalEffect{
		LogicalIdentity: identity,
		Attempt:         ledger.countLocked(identity) + 1,
		Outcome:         outcome,
		At:              ledger.clock.Now(),
	})
	return nil
}

func (ledger *EffectLedger) countLocked(identity string) int {
	count := 0
	for _, effect := range ledger.effects {
		if effect.LogicalIdentity == identity {
			count++
		}
	}
	return count
}

// DistinctIdentities returns how many logically distinct effects were
// attempted.
func (ledger *EffectLedger) DistinctIdentities() int {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	seen := map[string]bool{}
	for _, effect := range ledger.effects {
		seen[effect.LogicalIdentity] = true
	}
	return len(seen)
}

// DuplicateIdentities returns identities attempted more than once. The
// required answer in every fault scenario is an empty slice.
func (ledger *EffectLedger) DuplicateIdentities() []string {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	counts := map[string]int{}
	for _, effect := range ledger.effects {
		counts[effect.LogicalIdentity]++
	}
	var duplicates []string
	for identity, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, identity)
		}
	}
	return duplicates
}

// Effects returns a copy of the ledger.
func (ledger *EffectLedger) Effects() []ExternalEffect {
	ledger.mutex.Lock()
	defer ledger.mutex.Unlock()
	copied := make([]ExternalEffect, len(ledger.effects))
	copy(copied, ledger.effects)
	return copied
}

// RunWithFault executes work at an injection point, returning the injected
// fault when armed and the work's own result otherwise.
func RunWithFault(
	ctx context.Context,
	injector *FaultInjector,
	point FaultPoint,
	work func(context.Context) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if injector != nil {
		if err := injector.Check(point); err != nil {
			return err
		}
	}
	return work(ctx)
}
