package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// escalationSettings is the default configuration with a short fuse, so the
// tests exercise the rule rather than the number.
func escalationSettings(stall int) pipeline.Settings {
	settings := pipeline.DefaultSettings()
	settings.StallBeforeEscalation = stall
	return settings
}

// TestARunMakingProgressIsLeftAlone is the rule the obvious design gets wrong.
//
// Escalating on the attempt count would move this run to the expensive model
// while it was working: it fixed the compile error, then the missing tests,
// then the acceptance mismatch, and every failure was new information. Paying
// four times as much to finish work that was already finishing is the failure
// mode of the naive rule, and it is invisible — the run succeeds, so nothing
// ever says the money was wasted.
func TestARunMakingProgressIsLeftAlone(t *testing.T) {
	tracker := newConvergence(escalationSettings(2))
	for _, step := range []struct{ gate, failure string }{
		{"assembly", "the code does not compile: undefined: parse"},
		{"completeness", "3 functions have no test"},
		{"acceptance", "example 1 differs at line 4"},
		{"acceptance", "example 2 differs at line 9"},
		{"completeness", "1 function has no doc comment"},
	} {
		if decision := tracker.record(step.gate, step.failure); decision.Escalated != "" ||
			decision.Decompose {
			t.Fatalf("a run failing something different each time was escalated "+
				"at %q: %s", step.gate, decision.Why)
		}
	}
	if tracker.currentModel() != pipeline.DefaultSettings().FirstRung() {
		t.Errorf("the run moved off the first rung while it was converging: %s",
			tracker.currentModel())
	}
}

// TestARunRepeatingItselfIsEscalated is the case escalation exists for.
func TestARunRepeatingItselfIsEscalated(t *testing.T) {
	tracker := newConvergence(escalationSettings(3))
	const stuck = "example 1 differs at line 1: expected \"4\", got \"5\""

	if decision := tracker.record("acceptance", stuck); decision.Escalated != "" {
		t.Fatal("the first failure escalated, so nothing was given a chance")
	}
	if decision := tracker.record("acceptance", stuck); decision.Escalated != "" {
		t.Fatal("the second identical failure escalated one attempt early")
	}
	decision := tracker.record("acceptance", stuck)
	// The second rung, which is the same model thinking harder — the cheaper
	// axis. Escalating straight to a different model would raise the rate on
	// every token before establishing that more thinking would not have done.
	wantNext := pipeline.DefaultSettings().ModelLadder[1]
	if decision.Escalated != wantNext {
		t.Fatalf("the third identical failure did not escalate to %s: %+v",
			wantNext, decision)
	}
	if !strings.Contains(decision.Why, "ceiling") {
		t.Errorf("the reason does not say why repetition means escalating: %q",
			decision.Why)
	}
	if tracker.currentModel() != wantNext {
		t.Errorf("the tracker reports %s after escalating to %s",
			tracker.currentModel(), wantNext)
	}
}

// TestTheSameFailureFromTwoGatesIsNotARepeat keeps the fingerprint honest.
//
// The message is only half the identity. Two different gates rejecting work
// for reasons that happen to read alike are two findings, and treating them as
// one would escalate a run that had just been told something new.
func TestTheSameFailureFromTwoGatesIsNotARepeat(t *testing.T) {
	tracker := newConvergence(escalationSettings(2))
	tracker.record("assembly", "it is wrong")
	if decision := tracker.record("acceptance", "it is wrong"); decision.Escalated != "" {
		t.Error("the same words from a different gate counted as a repeat")
	}
}

// TestNoiseInAFailureDoesNotHideARepeat is what makes the tracker fire at all.
//
// Every real failure carries a temporary path, a duration, or a line number
// that moved. Comparing raw text would find two runs of one defect different
// forever: the tracker would never fire, the ladder would never be climbed,
// and nothing anywhere would report that the feature was inert.
func TestNoiseInAFailureDoesNotHideARepeat(t *testing.T) {
	tracker := newConvergence(escalationSettings(2))
	tracker.record("assembly",
		`C:\Temp\worktree-8814\cmd\thing\main.go:12:6: undefined: parse`)
	decision := tracker.record("assembly",
		`C:\Temp\worktree-9207\cmd\thing\main.go:14:6: undefined: parse`)
	if decision.Escalated == "" {
		t.Error("the same compile error from a different temporary directory " +
			"was treated as new information")
	}
}

// TestTheTopOfTheLadderDecomposesRatherThanRepeats covers the third rung.
func TestTheTopOfTheLadderDecomposesRatherThanRepeats(t *testing.T) {
	// A two-rung ladder, so the top is reached in one escalation and the test
	// is about what happens there rather than about the length of the default.
	settings := escalationSettings(2)
	settings.ModelLadder = []string{
		pipeline.Rung{Model: pipeline.ModelLuna, Effort: pipeline.EffortLow}.String(),
		pipeline.Rung{Model: pipeline.ModelSol, Effort: pipeline.EffortHigh}.String(),
	}
	tracker := newConvergence(settings)
	const stuck = "example 1 differs at line 1"

	tracker.record("acceptance", stuck)
	if decision := tracker.record("acceptance", stuck); decision.Escalated == "" {
		t.Fatalf("the run did not reach the top of the ladder: %+v", decision)
	}
	tracker.record("acceptance", stuck)
	decision := tracker.record("acceptance", stuck)
	if !decision.Decompose {
		t.Fatalf("stalling at the top of the ladder did not decompose: %+v",
			decision)
	}
	if decision.Escalated != "" {
		t.Error("decomposing also claimed to escalate, so a caller would " +
			"rebuild a model that does not exist")
	}

	// And only once. Repeating the decomposition would produce ever finer units
	// of a request that is not failing for being coarse.
	tracker.record("acceptance", stuck)
	if again := tracker.record("acceptance", stuck); again.Decompose {
		t.Error("the run decomposed a second time on the same stall")
	}
}

// TestEscalatingResetsTheCount stops a run climbing without being tried.
//
// Without the reset, a run at the threshold escalates and then escalates again
// on the very next attempt — before the new model has had one chance to do
// anything differently — so the ladder is climbed on a single stall.
func TestEscalatingResetsTheCount(t *testing.T) {
	settings := escalationSettings(2)
	tracker := newConvergence(settings)
	const stuck = "the same thing again"

	tracker.record("acceptance", stuck)
	tracker.record("acceptance", stuck)
	if decision := tracker.record("acceptance", stuck); decision.Decompose {
		t.Error("the attempt straight after escalating decomposed, so the " +
			"stronger model was never actually tried")
	}
}

// TestASettingsThatForbidsDecompositionStops covers the switch being off.
func TestASettingsThatForbidsDecompositionStops(t *testing.T) {
	settings := escalationSettings(1)
	only := pipeline.Rung{Model: pipeline.ModelSol, Effort: pipeline.EffortHigh}.String()
	settings.ModelLadder = []string{only}
	settings.DecomposeWhenExhausted = false
	tracker := newConvergence(settings)

	tracker.record("acceptance", "stuck")
	if decision := tracker.record("acceptance", "stuck"); decision.Decompose {
		t.Error("decomposition happened though the setting turns it off")
	}
	if !strings.Contains(tracker.summary(), "converged on "+only) {
		t.Errorf("the summary misreports a run that never escalated: %q",
			tracker.summary())
	}
}

// TestTheSummarySaysWhatTheRunCost is what makes escalation auditable.
//
// A run that quietly cost four times as much as another is worse than one that
// says so: the cost shows up on a bill with nothing in the record explaining
// which runs earned it.
func TestTheSummarySaysWhatTheRunCost(t *testing.T) {
	tracker := newConvergence(escalationSettings(1))
	tracker.beginAttempt()
	tracker.record("acceptance", "one")
	tracker.beginAttempt()
	tracker.record("acceptance", "one")
	summary := tracker.summary()
	if !strings.Contains(summary, tracker.currentModel()) {
		t.Errorf("the summary does not name the rung the run moved to: %q",
			summary)
	}
	if !strings.Contains(summary, "2 attempts") {
		t.Errorf("the summary does not say what the run took: %q", summary)
	}
}

// TestEveryRungOfTheLadderIsReachableOnTheShippedDefaults is the check that
// caught a rung that could never fire.
//
// With one flat attempt budget, the attempts a run spent proving the cheap
// model could not do the work were charged to the model it escalated to. On
// the shipped defaults that left the strong model the tail of a spent
// allowance and put decomposition out of reach entirely: a configured,
// documented, settable rung of the ladder that no run could ever reach. A
// feature nobody can reach is worse than one that is absent, because the
// setting says it is there.
func TestEveryRungOfTheLadderIsReachableOnTheShippedDefaults(t *testing.T) {
	settings := pipeline.DefaultSettings()
	tracker := newConvergence(settings)
	const stuck = "the same failure, unchanged"

	escalated, decomposed := false, false
	for tracker.moreAttempts() {
		tracker.beginAttempt()
		decision := tracker.record("acceptance", stuck)
		if decision.Escalated != "" {
			escalated = true
		}
		if decision.Decompose {
			decomposed = true
		}
	}
	if !escalated {
		t.Error("a run stuck on one failure never escalated within its budget")
	}
	if !decomposed {
		t.Error("a run stuck on one failure at the top of the ladder never " +
			"decomposed, so that rung cannot be reached on the defaults")
	}
	top := settings.ModelLadder[len(settings.ModelLadder)-1]
	if tracker.currentModel() != top {
		t.Errorf("the run finished on %s rather than the top of its ladder, %s",
			tracker.currentModel(), top)
	}
}

// TestTheAttemptCeilingBoundsWhatARunCanCost keeps a per-rung budget from
// being an open cheque.
//
// Granting a fresh allowance on every escalation is what makes the ladder
// usable; without an overall ceiling it would also make a stuck run
// unbounded, which is the failure mode a cost-conscious default exists to
// prevent.
func TestTheAttemptCeilingBoundsWhatARunCanCost(t *testing.T) {
	settings := pipeline.DefaultSettings()
	tracker := newConvergence(settings)

	attempts := 0
	for tracker.moreAttempts() {
		tracker.beginAttempt()
		tracker.record("acceptance", "unchanged")
		attempts++
		if attempts > 1000 {
			t.Fatal("a stuck run never ran out of attempts")
		}
	}
	// Two rungs plus one decomposition, each with the full allowance, is the
	// most a run can spend. It is a bound rather than an amount: a run that
	// stalls at the threshold escalates part-way through a rung and never
	// spends the rest of it, which is the point — the allowance is granted so
	// the new approach has room, not so the run uses every attempt available.
	ceiling := settings.MaximumAttempts * (len(settings.ModelLadder) + 1)
	if attempts > ceiling {
		t.Errorf("a stuck run made %d attempts, past the ceiling of %d",
			attempts, ceiling)
	}
	// And more than one rung's worth, or the grant did nothing.
	if attempts <= settings.MaximumAttempts {
		t.Errorf("a stuck run made %d attempts, which is no more than one "+
			"rung's budget of %d — the escalated model was never given room",
			attempts, settings.MaximumAttempts)
	}
}

// TestARunThatConvergesCostsWhatItAlwaysDid is the guard on rarity.
//
// The per-rung budget must not quietly hand every run three times the
// attempts. A run that is not stalling gets exactly what it got before, so
// the extra allowance is reachable only by the runs that earned it.
func TestARunThatConvergesCostsWhatItAlwaysDid(t *testing.T) {
	settings := pipeline.DefaultSettings()
	tracker := newConvergence(settings)

	attempts := 0
	for tracker.moreAttempts() {
		tracker.beginAttempt()
		// A different failure every time: the run is learning, so nothing is
		// ever granted.
		tracker.record("acceptance", "failure number "+counted(attempts, "x"))
		attempts++
	}
	// The ordinary allowance plus one bounded top-up, and no more. A run that
	// keeps fixing something different is still working, and cutting it off at
	// the allowance ships the gates it had not reached yet — rung 5 ended with
	// three. The top-up is half an allowance rather than a whole one, because
	// converging is the normal case and a full extra would raise the price of
	// almost every run to pay for the few that needed it.
	want := settings.MaximumAttempts + settings.MaximumAttempts/2
	if attempts != want {
		t.Errorf("a run failing something new each time made %d attempts, "+
			"want its allowance plus one top-up, %d", attempts, want)
	}
	if attempts >= settings.MaximumAttempts*2 {
		t.Errorf("the top-up doubled a converging run's budget: %d", attempts)
	}
	if len(tracker.escalations) > 0 {
		t.Errorf("a converging run escalated: %v", tracker.escalations)
	}
}

// TestDecompositionTellsTheRunToStopTryingItInOnePiece pins the instruction.
//
// The instruction is the whole content of the third rung. If it merely repeats
// the failure, the run makes the same attempt a third time on a more expensive
// model, which is the outcome the rung exists to avoid.
func TestDecompositionTellsTheRunToStopTryingItInOnePiece(t *testing.T) {
	instruction := decompositionInstruction(
		"Create cmd/thing/main.go that parses a config and serves it.")
	for _, required := range []string{
		"Do not try to satisfy it in one piece again",
		"smallest units",
		"Get each one passing before writing the next",
		"Re-read what was asked",
		"Create cmd/thing/main.go",
	} {
		if !strings.Contains(instruction, required) {
			t.Errorf("the decomposition instruction never says %q:\n%s",
				required, instruction)
		}
	}
}

// TestAnInterruptedStallIsStillAStall is the sequence rung 5 actually ran.
//
// The same missing doc comment on attempts one, two and four, with a refused
// turn in between. Counting only against the immediately previous failure, the
// refusals reset the tally and the run never escalated — so a run could be
// stuck on one gate indefinitely as long as something else interrupted now and
// then. Worse, the interruptions came from the malformed-turn recovery added
// to make runs survive model slips: one repair quietly disabled another.
func TestAnInterruptedStallIsStillAStall(t *testing.T) {
	tracker := newConvergence(escalationSettings(3))
	const stuck = "1 function has no doc comment: main"

	tracker.record("completeness", stuck)
	tracker.record("completeness", stuck)
	// An unrelated failure. Nothing was learned about the doc comment.
	tracker.record("assembly", "the loop refused a malformed turn")
	decision := tracker.record("completeness", stuck)
	if decision.Escalated == "" {
		t.Fatal("the third occurrence of an identical failure did not escalate " +
			"because an unrelated failure happened in between")
	}
}

// TestAnEscalatedRunDoesNotInheritTheOldTallies keeps the fix from causing the
// problem it replaced.
//
// Counting per failure across the run means a rung that inherits the previous
// rung's tallies escalates again on its first failure, before the model it
// just moved to has had one chance to do anything differently.
func TestAnEscalatedRunDoesNotInheritTheOldTallies(t *testing.T) {
	tracker := newConvergence(escalationSettings(2))
	const stuck = "the same failure"

	tracker.record("acceptance", stuck)
	if decision := tracker.record("acceptance", stuck); decision.Escalated == "" {
		t.Fatal("the run did not escalate at its threshold")
	}
	if decision := tracker.record("acceptance", stuck); decision.Escalated != "" ||
		decision.Decompose {
		t.Errorf("the attempt straight after escalating moved again, so the "+
			"new rung was never tried: %+v", decision)
	}
}
