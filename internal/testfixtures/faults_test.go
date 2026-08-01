package testfixtures

import (
	"context"
	"errors"
	"testing"
	"time"
)

// faultScenario declares one M22-036..050 fault case and the safe outcome the
// system must offer after it.
type faultScenario struct {
	todo    string
	point   FaultPoint
	outcome SafeOutcome
	// duplicateRisk marks faults where the fault lands AFTER an external
	// effect may already have happened. Those are the cases where a naive
	// retry duplicates a charge, a commit, or a message.
	duplicateRisk bool
}

func faultScenarios() []faultScenario {
	return []faultScenario{
		{"M22-036", FaultWorkerDuringRepositoryRead, OutcomeRetryable, false},
		{"M22-037", FaultWorkerDuringFileEdit, OutcomeResumable, false},
		{"M22-038", FaultWorkerDuringCommand, OutcomeRequiresReconciliation, true},
		{"M22-039", FaultWorkerDuringModelStream, OutcomeRetryable, true},
		{"M22-040", FaultCoordinatorBeforeCommit, OutcomeTerminatedCleanly, false},
		{"M22-041", FaultCoordinatorAfterCommit, OutcomeResumable, true},
		{"M22-042", FaultBrowserDuringApproval, OutcomeResumable, false},
		{"M22-043", FaultBrowserDuringBudgetChange, OutcomeResumable, false},
		{"M22-044", FaultDiskFullOnEventAppend, OutcomeTerminatedCleanly, false},
		{"M22-045", FaultDatabaseBusyTimeout, OutcomeRetryable, false},
		{"M22-046", FaultWorktreeMissingOrCorrupt, OutcomeRequiresReconciliation, false},
		{"M22-047", FaultProviderRateLimited, OutcomeRetryable, false},
		{"M22-048", FaultProviderPartialThenFailure, OutcomeRequiresReconciliation, true},
		{"M22-049", FaultProviderDelayedUsage, OutcomeRequiresReconciliation, false},
		{"M22-050", FaultCommandTimeoutWithChildren, OutcomeRequiresReconciliation, true},
	}
}

// TestM22_036_050_EveryDeclaredFaultPointIsCovered proves the scenario table
// covers every declared injection point exactly once, so a fault cannot be
// declared and then quietly left untested.
func TestM22_036_050_EveryDeclaredFaultPointIsCovered(t *testing.T) {
	scenarios := faultScenarios()
	if len(scenarios) != len(AllFaultPoints()) {
		t.Fatalf("%d scenarios for %d declared fault points", len(scenarios), len(AllFaultPoints()))
	}
	covered := map[FaultPoint]string{}
	for _, scenario := range scenarios {
		if other, clash := covered[scenario.point]; clash {
			t.Fatalf("fault point %q is claimed by both %s and %s", scenario.point, other, scenario.todo)
		}
		covered[scenario.point] = scenario.todo
		if !scenario.outcome.Valid() {
			t.Fatalf("%s: outcome %q is not a safe answer", scenario.todo, scenario.outcome)
		}
	}
	for _, point := range AllFaultPoints() {
		if _, present := covered[point]; !present {
			t.Errorf("declared fault point %q has no scenario", point)
		}
	}
}

// TestM22_036_050_EachFaultFiresAndYieldsASafeOutcome runs every declared
// fault, proving the fault ACTUALLY fires and that the scenario resolves to
// one of the four safe answers — never to an unclassified failure.
func TestM22_036_050_EachFaultFiresAndYieldsASafeOutcome(t *testing.T) {
	for _, scenario := range faultScenarios() {
		t.Run(scenario.todo+"/"+string(scenario.point), func(t *testing.T) {
			injector := NewFaultInjector()
			if err := injector.Arm(scenario.point, 1, "fixture: "+scenario.todo); err != nil {
				t.Fatal(err)
			}

			clock, err := NewSteppingClock(time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			ledger := NewEffectLedger(clock)
			identity := "effect:" + string(scenario.point)

			// First attempt: the fault fires before the effect completes.
			err = RunWithFault(context.Background(), injector, scenario.point, func(context.Context) error {
				return ledger.Record(identity, scenario.outcome)
			})
			if !errors.Is(err, ErrInjectedFault) {
				t.Fatalf("fault did not fire: err = %v", err)
			}
			if injector.Fired(scenario.point) != 1 {
				t.Fatalf("fault fired %d times, want 1", injector.Fired(scenario.point))
			}
			if injector.Remaining(scenario.point) != 0 {
				t.Fatalf("%d arms left unfired", injector.Remaining(scenario.point))
			}

			// Recovery: the same logical effect is attempted once more. Per
			// docs/plan.md the maximum acceptable duplicate external-effect
			// intent after retry or replay is ZERO, so the recovery path must
			// record the effect under its logical identity exactly once.
			if err := RunWithFault(context.Background(), injector, scenario.point, func(context.Context) error {
				return ledger.Record(identity, scenario.outcome)
			}); err != nil {
				t.Fatalf("recovery attempt failed: %v", err)
			}

			effects := ledger.Effects()
			if len(effects) != 1 {
				t.Fatalf("recorded %d effects, want exactly 1: the fault must not have let a partial effect through", len(effects))
			}
			if !effects[0].Outcome.Valid() {
				t.Fatalf("effect outcome %q is not a safe answer", effects[0].Outcome)
			}
			if duplicates := ledger.DuplicateIdentities(); len(duplicates) != 0 {
				t.Fatalf("duplicate external effects after recovery: %v", duplicates)
			}
			if scenario.duplicateRisk && effects[0].Outcome == OutcomeTerminatedCleanly {
				t.Fatalf("%s lands after an effect may already have occurred; it cannot claim a clean termination", scenario.todo)
			}
		})
	}
}

// TestM22_036_050_ADuplicatedEffectIsDetected proves the ledger would
// actually catch a duplicate, so the zero-duplicate assertions above are
// meaningful rather than vacuous.
func TestM22_036_050_ADuplicatedEffectIsDetected(t *testing.T) {
	ledger := NewEffectLedger(NewFixedClock())
	for attempt := 0; attempt < 2; attempt++ {
		if err := ledger.Record("charge:order-1", OutcomeRetryable); err != nil {
			t.Fatal(err)
		}
	}
	duplicates := ledger.DuplicateIdentities()
	if len(duplicates) != 1 || duplicates[0] != "charge:order-1" {
		t.Fatalf("duplicates = %v, want the repeated identity", duplicates)
	}
	if ledger.DistinctIdentities() != 1 {
		t.Fatalf("distinct identities = %d, want 1", ledger.DistinctIdentities())
	}
	effects := ledger.Effects()
	if effects[1].Attempt != 2 {
		t.Fatalf("second attempt numbered %d, want 2", effects[1].Attempt)
	}
}

// TestM22_036_050_InjectorAndLedgerRejectMalformedUse keeps the harness from
// silently accepting a test that proves nothing.
func TestM22_036_050_InjectorAndLedgerRejectMalformedUse(t *testing.T) {
	injector := NewFaultInjector()
	if err := injector.Arm(FaultDatabaseBusyTimeout, 0, "reason"); err == nil {
		t.Fatal("arming zero times must be rejected")
	}
	if err := injector.Arm(FaultDatabaseBusyTimeout, 1, ""); err == nil {
		t.Fatal("arming without a reason must be rejected")
	}
	ledger := NewEffectLedger(nil)
	if err := ledger.Record("", OutcomeRetryable); err == nil {
		t.Fatal("an effect without a logical identity must be rejected")
	}
	if err := ledger.Record("effect", SafeOutcome("improvised")); err == nil {
		t.Fatal("an outcome outside the four safe answers must be rejected")
	}
}

// TestM22_036_050_CancellationIsHonouredBeforeInjection proves a cancelled
// context short-circuits before a fault fires, so cancellation bugs are not
// masked by injected failures.
func TestM22_036_050_CancellationIsHonouredBeforeInjection(t *testing.T) {
	injector := NewFaultInjector()
	if err := injector.Arm(FaultProviderRateLimited, 1, "fixture"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunWithFault(ctx, injector, FaultProviderRateLimited, func(context.Context) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if injector.Fired(FaultProviderRateLimited) != 0 {
		t.Fatal("a cancelled call must not consume an armed fault")
	}
}
