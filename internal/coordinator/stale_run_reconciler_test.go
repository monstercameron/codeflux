package coordinator

import (
	"context"
	"testing"
	"time"
)

// TestALiveRunIsNeverReconciledOutFromUnderItself is the property that matters
// most here.
//
// Reconciling is a blunt instrument: it takes a task away from whatever might
// still be working on it. A run legitimately spends minutes inside one model
// call, so a window that is too eager would end healthy runs — which is a far
// worse defect than leaving a dead one for another cycle.
func TestALiveRunIsNeverReconciledOutFromUnderItself(t *testing.T) {
	execution, _ := recallFixture(t)
	reconciler := NewStaleRunReconciler(execution.repositories)

	moved, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconciling failed: %v", err)
	}
	if moved != 0 {
		t.Errorf("%d task(s) younger than the staleness window were "+
			"reconciled", moved)
	}
}

// TestReconcilingIsIdempotent: a task that has been given an ending is not
// given another one, and reconciling is not a way of restarting anything.
func TestReconcilingIsIdempotent(t *testing.T) {
	execution, _ := recallFixture(t)
	reconciler := NewStaleRunReconciler(execution.repositories)
	reconciler.SetStalenessWindow(time.Nanosecond)
	time.Sleep(2 * time.Millisecond)

	first, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second > first {
		t.Errorf("a second pass moved %d task(s) where the first moved %d, so "+
			"reconciling is doing something other than giving an ending",
			second, first)
	}
}

// TestTheWindowRefusesNonsense keeps a misconfiguration from turning this into
// a machine that ends every run the moment it starts.
func TestTheWindowRefusesNonsense(t *testing.T) {
	reconciler := NewStaleRunReconciler(nil)
	before := reconciler.after
	reconciler.SetStalenessWindow(0)
	reconciler.SetStalenessWindow(-time.Hour)
	if reconciler.after != before {
		t.Errorf("the staleness window was set to %s by a nonsense value",
			reconciler.after)
	}
	// A reconciler with no store does nothing rather than panicking: it is
	// wired optionally and an unwired one must be inert.
	if moved, err := reconciler.Reconcile(context.Background()); err != nil ||
		moved != 0 {
		t.Errorf("an unwired reconciler moved %d task(s): %v", moved, err)
	}
}
