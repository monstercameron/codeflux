package coordinator

import (
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// TestASendBackIsOnlyIssuedWhenAnAttemptCanFollow is the agreement the two
// predicates were missing.
//
// lastAttempt decides whether sending the work back is worth doing;
// moreAttempts decides whether the loop runs again. They answer the same
// question and must answer it the same way. They did not: lastAttempt treated a
// rung above and a decomposition as available allowances, but record only
// grants those on a stall, so for a run that never repeated itself they were
// promises nothing would keep.
//
// Ladder rung 5 on 2026-08-03 ended there twice. Six attempts, six different
// gates, nothing repeated — then path-coverage was raised naming one uncovered
// line out of thirty-three, and the next thing in the trace is the run
// finishing. Told, then denied the attempt to act.
func TestASendBackIsOnlyIssuedWhenAnAttemptCanFollow(t *testing.T) {
	settings := pipeline.DefaultSettings()

	newTracker := func() *convergence {
		tracker := &convergence{settings: settings}
		tracker.rung = settings.ModelLadder[0]
		return tracker
	}

	// A converging run with its allowance spent: the one-time extension is the
	// only thing left, and both predicates must see it.
	converging := newTracker()
	converging.spent = settings.MaximumAttempts
	converging.seen = map[string]int{"a": 1, "b": 1, "c": 1}

	if converging.lastAttempt() {
		t.Error("a converging run with the extension unspent was told nothing " +
			"follows, so its work is never sent back")
	}
	if !converging.moreAttempts() {
		t.Fatal("the extension did not grant an attempt, so the fixture no " +
			"longer reproduces the case")
	}

	// Once the extension is spent, both must agree the run is over — otherwise
	// the work is sent back into an attempt that never runs.
	spent := newTracker()
	spent.spent = settings.MaximumAttempts
	spent.seen = map[string]int{"a": 1, "b": 1}
	spent.extended = true

	if !spent.lastAttempt() {
		t.Error("a converging run with the extension already spent was told " +
			"something follows this attempt")
	}
	if spent.moreAttempts() {
		t.Error("moreAttempts granted an attempt the extension had already " +
			"paid for")
	}
}

// TestAStallStillReachesTheRungAbove is the control.
//
// The hatches are not wrong, they are conditional. A run that has repeated
// itself is exactly what the model ladder and decomposition exist for, and
// record does grant a fresh allowance on that path — so for a stalling run
// lastAttempt must still say something follows.
func TestAStallStillReachesTheRungAbove(t *testing.T) {
	settings := pipeline.DefaultSettings()
	if len(settings.ModelLadder) < 2 {
		t.Skip("this project declares no rung above the first")
	}
	tracker := &convergence{settings: settings}
	tracker.rung = settings.ModelLadder[0]
	tracker.spent = settings.MaximumAttempts
	// The same failure twice: a stall, not progress.
	tracker.seen = map[string]int{"the same gate and reason": 2}

	if tracker.lastAttempt() {
		t.Error("a stalling run on the lowest rung was told nothing follows, " +
			"so it can never reach the stronger model that exists for it")
	}
}

// TestConvergingUsesTheSameStallThresholdRecordDoes is the third place that
// was deciding one fact its own way.
//
// record escalates when a failure has repeated StallBeforeEscalation times,
// three by default. converging read any repeat at all as not-converging, so a
// run the tracker had never once called stalled was refused the extension that
// exists for exactly that run.
//
// It bites because the fingerprint is over the gate and its one-line reason,
// not over what was asked, and adversarial-review's reason is the fixed
// sentence "a review found it weaker than it looks". A review bounded to two
// findings a round is built to fire more than once; doing so recorded as the
// same failure twice. Ladder rung 5 on 2026-08-03 lost its extension that way
// while every attempt had cleared a different gate.
func TestConvergingUsesTheSameStallThresholdRecordDoes(t *testing.T) {
	settings := pipeline.DefaultSettings()
	if settings.StallBeforeEscalation < 3 {
		t.Skip("this project stalls at two, so there is no gap to measure")
	}
	tracker := &convergence{settings: settings}

	// One gate raised twice for the same stated reason. Under record's own
	// definition that is not yet a stall.
	tracker.seen = map[string]int{
		"adversarial-review" + fingerprintSeparator + "a review found it weaker": 2,
	}
	if !tracker.converging() {
		t.Error("a failure seen twice was read as a stall, though record does " +
			"not escalate until it has been seen three times")
	}

	// At the threshold it is a stall, and the run should stop being given
	// extensions for going round.
	tracker.seen = map[string]int{
		"adversarial-review" + fingerprintSeparator + "a review found it weaker": 3,
	}
	if tracker.converging() {
		t.Error("a failure at the stall threshold was still read as progress")
	}
}

// TestARegressionProneGateStallsAtTwo is the control.
//
// Losing a property already satisfied is a cycle rather than slow progress, and
// record halves the threshold for those gates. converging has to agree, or the
// two disagree again in the opposite direction.
func TestARegressionProneGateStallsAtTwo(t *testing.T) {
	settings := pipeline.DefaultSettings()
	var prone string
	for gate := range regressionProneGates {
		prone = gate
		break
	}
	if prone == "" {
		t.Skip("no gate is marked regression-prone")
	}
	tracker := &convergence{settings: settings}
	tracker.seen = map[string]int{
		prone + fingerprintSeparator + "lost what it had": 2,
	}
	if tracker.converging() {
		t.Errorf("%s was seen twice and still read as progress; losing a "+
			"property already satisfied is a cycle at two", prone)
	}
}
