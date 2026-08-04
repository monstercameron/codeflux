package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// stallTwice drives a tracker to the stall that triggers escalation.
func stallTwice(tracker *convergence, threshold int) verdict {
	const gate = "adversarial-review"
	const stuck = "executeCommands nests 5 levels deep"
	var last verdict
	for range threshold {
		last = tracker.record(gate, stuck, stuck)
	}
	return last
}

// TestEscalationDoesNotMoveARunOntoADeadRung is the failure that ended four
// ladder runs in a row.
//
// Escalating is meant to give a stuck run a better chance. Escalating onto a
// provider that just refused another run gives it no chance at all: the first
// request fails at the transport, the adapter exhausts its own retries, and a
// run that was still making progress is abandoned for a reason that has nothing
// to do with the work.
//
// Proven to discriminate: against the previous implementation this moves the
// run to the second rung and reports it as an escalation. Rung 15 on 2026-08-04
// died exactly there in four of five passes, every one on the first request
// after moving up.
func TestEscalationDoesNotMoveARunOntoADeadRung(t *testing.T) {
	settings := pipeline.DefaultSettings()
	first := settings.FirstRung()
	next, more := settings.NextRung(first)
	if !more {
		t.Skip("the ladder has one rung, so there is nothing to escalate onto")
	}

	tracker := newConvergence(settings)
	tracker.health = func(rung string) (string, bool) {
		return "the provider exhausted its own retries", rung == next
	}

	decision := stallTwice(tracker, settings.StallBeforeEscalation)
	if decision.Escalated != "" {
		t.Fatalf("the run was moved onto %s, which is down, so its next "+
			"request ends it", decision.Escalated)
	}
	if tracker.currentModel() != first {
		t.Errorf("the run left the rung that was answering: %s",
			tracker.currentModel())
	}
	if !strings.Contains(decision.Why, "unavailable") {
		t.Errorf("the reason should say the rung is down, got %q", decision.Why)
	}
	// And it keeps the allowance escalating would have granted, because the
	// attempts are still worth spending — just here rather than there.
	if !tracker.moreAttempts() {
		t.Error("staying put must not also cost the run its fresh allowance, " +
			"or declining a dead rung is worse than dying on it")
	}
}

// TestEscalationStillHappensWhenTheRungIsUp is the control.
//
// The rule is about a rung that is known to be down, not about escalation being
// doubtful in general. With nothing wrong upstream a stalled run must still get
// the stronger model, which is the whole point of the ladder.
func TestEscalationStillHappensWhenTheRungIsUp(t *testing.T) {
	settings := pipeline.DefaultSettings()
	next, more := settings.NextRung(settings.FirstRung())
	if !more {
		t.Skip("the ladder has one rung")
	}

	tracker := newConvergence(settings)
	tracker.health = func(string) (string, bool) { return "", false }

	decision := stallTwice(tracker, settings.StallBeforeEscalation)
	if decision.Escalated != next {
		t.Fatalf("a stalled run on a healthy ladder must escalate to %s, got "+
			"%+v", next, decision)
	}
	if tracker.currentModel() != next {
		t.Errorf("the tracker reports %s after escalating to %s",
			tracker.currentModel(), next)
	}
}

// TestARunComesBackDownTheWayItWentUp is the recovery the guard above cannot
// provide on its own.
//
// The health record only helps the next run: within one run, the first request
// at a new rung is what discovers the rung is down, and by then the run has
// already moved. Stopping there throws away a run that was still making
// progress because a socket closed on one request. So the run steps back to the
// rung it escalated from — which was answering minutes ago — and carries on.
//
// Proven to discriminate: against the previous implementation the circuit
// simply opened and the run ended. Ladder rung 15 on 2026-08-04 ended that way
// in four of five passes.
func TestARunComesBackDownTheWayItWentUp(t *testing.T) {
	settings := pipeline.DefaultSettings()
	first := settings.FirstRung()
	next, more := settings.NextRung(first)
	if !more {
		t.Skip("the ladder has one rung")
	}

	tracker := newConvergence(settings)
	tracker.health = func(string) (string, bool) { return "", false }
	if decision := stallTwice(tracker, settings.StallBeforeEscalation); decision.Escalated != next {
		t.Fatalf("the fixture needs the run to have escalated: %+v", decision)
	}

	stepped, why := tracker.stepDown()
	if !stepped {
		t.Fatal("a run that escalated must be able to come back down, or a " +
			"closed socket on the rung above ends it")
	}
	if tracker.currentModel() != first {
		t.Errorf("the run came down to %s rather than the rung it climbed "+
			"from, %s", tracker.currentModel(), first)
	}
	if !strings.Contains(why, next) {
		t.Errorf("the reason should name the rung that did not answer: %q", why)
	}
	if !tracker.moreAttempts() {
		t.Error("coming down must leave the run something to spend, or it is " +
			"the same as stopping")
	}
}

// TestARunThatNeverEscalatedCannotStepDown is the control.
//
// Stepping down is undoing an escalation, not a general licence to change
// model. A run still on the rung it started on has nowhere to go, and the
// circuit must open exactly as it did before — otherwise a dead provider on the
// first rung becomes an infinite loop instead of a stopped run.
func TestARunThatNeverEscalatedCannotStepDown(t *testing.T) {
	tracker := newConvergence(pipeline.DefaultSettings())
	if stepped, _ := tracker.stepDown(); stepped {
		t.Fatalf("a run that never escalated stepped down to %s",
			tracker.currentModel())
	}
}

// TestSteppingDownConsumesTheEscalation keeps one outage from unwinding a whole
// ladder.
//
// Each step down undoes one escalation and no more. Without that, a run that
// climbed once could keep descending on repeated failures and end up below
// where it started, spending attempts on a model it was never granted.
func TestSteppingDownConsumesTheEscalation(t *testing.T) {
	settings := pipeline.DefaultSettings()
	if _, more := settings.NextRung(settings.FirstRung()); !more {
		t.Skip("the ladder has one rung")
	}
	tracker := newConvergence(settings)
	tracker.health = func(string) (string, bool) { return "", false }
	stallTwice(tracker, settings.StallBeforeEscalation)

	if stepped, _ := tracker.stepDown(); !stepped {
		t.Fatal("the first step down should undo the escalation")
	}
	if stepped, _ := tracker.stepDown(); stepped {
		t.Errorf("a second step down took the run to %s, below where it "+
			"started", tracker.currentModel())
	}
}
