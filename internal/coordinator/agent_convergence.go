package coordinator

import (
	"regexp"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/pipeline"
)

// convergence decides whether a run is making progress or repeating itself.
//
// The obvious rule — escalate after N attempts — measures the wrong thing. A
// run on its eighth attempt that has failed a different gate each time is
// working: it fixed the compile error, then the missing tests, then the
// acceptance mismatch, and each failure was new information. A run that has
// failed the same gate three times with the same message is not going to
// succeed on the twelfth; the attempts in between are spent establishing what
// was already established. What separates the two is repetition, so that is
// what is counted.
type convergence struct {
	settings pipeline.Settings
	// rung is the model and effort the run is currently on.
	rung string
	// seen counts how many times each distinct failure has occurred in this
	// run, and repeats is the count for the failure just recorded.
	//
	// It is a count per failure rather than a single previous value. Comparing
	// only against the immediately preceding failure meant an unrelated one in
	// between erased the evidence of a stall: a run could be stuck on the same
	// gate indefinitely so long as something else interrupted occasionally.
	// Rung 5 did exactly that — the same missing doc comment on attempts one,
	// two and four, with a refused turn in between resetting the count each
	// time — and never escalated, because the failures were identical but
	// never consecutive. A fingerprint that recurs is the same failure
	// recurring whether or not anything happened between.
	seen    map[string]int
	repeats int
	// spent is attempts used on the current rung, and total is attempts used
	// by the run overall.
	//
	// The budget is per rung rather than per run. With one flat budget the
	// attempts a run spent proving the cheap model could not do the work were
	// charged to the model it escalated to: on the shipped defaults the strong
	// model got the tail of a spent allowance and decomposition could not be
	// reached at all — a rung of the ladder that could never fire. A rung that
	// has just been reached has not had its turn yet.
	spent int
	total int
	// escalations and decomposed record what happened, for the ledger. A run
	// that quietly cost four times as much is worse than one that says so.
	escalations []string
	decomposed  bool
	// extended records the one extra allowance a still-converging run is given
	// when its budget runs out with gates left. Once per run: a second would
	// make the ceiling advisory.
	extended bool
}

// newConvergence starts a run at the bottom of its ladder.
func newConvergence(settings pipeline.Settings) *convergence {
	return &convergence{settings: settings, rung: settings.FirstRung()}
}

// beginAttempt counts one attempt against the current rung and the run.
func (tracker *convergence) beginAttempt() {
	tracker.spent++
	tracker.total++
}

// moreAttempts reports whether the run may make another attempt at all.
//
// The overall ceiling is what keeps a per-rung budget from being an open
// cheque: a run can be granted a fresh allowance once per rung, once for
// decomposing, and once for still converging, and no more than that.
func (tracker *convergence) moreAttempts() bool {
	if tracker.total >= tracker.ceiling() {
		return false
	}
	if tracker.spent < tracker.settings.MaximumAttempts {
		return true
	}
	// The allowance is spent, but if the run has never once repeated itself
	// then every attempt bought something and stopping here is arbitrary.
	//
	// The ladder only grants more attempts on a stall, which leaves the
	// opposite case unhandled: a run with several gates left to satisfy fixes
	// a different one each time, is told it is converging, and is then cut off
	// for taking as many attempts as it had gates. Rung 5 did exactly that —
	// six attempts, six different failures, none repeated, and it ran out with
	// three gaps left and its last rewrite ungated.
	//
	// Once, and only while nothing has repeated. A run that has started
	// repeating itself is stalling, and stalling is what the ladder is for.
	if !tracker.extended && tracker.converging() {
		tracker.extended = true
		tracker.spent = tracker.settings.MaximumAttempts - tracker.topUp()
		return true
	}
	return false
}

// topUp is how many extra attempts a still-converging run is given.
//
// Half the allowance, not another whole one. The purpose is to keep a run that
// is one or two gates from finished from being cut off mid-progress, not to
// double what every converging run costs — and since converging is the normal
// case, a full extra allowance would raise the price of almost every run to
// pay for the few that needed it.
func (tracker *convergence) topUp() int {
	half := tracker.settings.MaximumAttempts / 2
	if half < 1 {
		return 1
	}
	return half
}

// converging reports that no failure has been seen twice in this run.
//
// It is the direct opposite of the stall condition and reads off the same
// tally, so the two can never disagree about whether a run is making progress.
func (tracker *convergence) converging() bool {
	for _, count := range tracker.seen {
		if count > 1 {
			return false
		}
	}
	return true
}

// lastAttempt reports that nothing follows this one, so there is no point
// sending the work back.
// It asks whether anything can follow this attempt, which is not the same as
// whether this rung's allowance is spent. A rung above, a decomposition, or
// the converging extension each grant a fresh allowance — but all three are
// decided inside record, which only runs when work is sent back. So a run that
// declined to send back because its allowance looked spent could never reach
// the escalation that would have refilled it, and the last attempt of every
// rung was ungated.
func (tracker *convergence) lastAttempt() bool {
	if tracker.total >= tracker.ceiling() {
		return true
	}
	if tracker.spent < tracker.settings.MaximumAttempts {
		return false
	}
	if _, more := tracker.settings.NextRung(tracker.rung); more {
		return false
	}
	if !tracker.decomposed && tracker.settings.DecomposeWhenExhausted {
		return false
	}
	return tracker.extended || !tracker.converging()
}

// ceiling is the most attempts a run may make across every rung.
func (tracker *convergence) ceiling() int {
	grants := len(tracker.settings.ModelLadder)
	if tracker.settings.DecomposeWhenExhausted {
		grants++
	}
	// One more for the converging run, so the extension above cannot push a
	// run past a ceiling that is meant to bound what it can cost.
	grants++
	if grants < 1 {
		grants = 1
	}
	return tracker.settings.MaximumAttempts * grants
}

// grant gives the current approach its own full allowance.
func (tracker *convergence) grant() {
	tracker.spent = 0
	// The stall count goes with it. Without that, a run at the threshold
	// escalates and then escalates again on the very next attempt, before the
	// model it just moved to has had one chance to do anything differently.
	tracker.repeats = 0
	// Counts start over, not only the one that fired. A rung inheriting the
	// tallies of the rung below would escalate again on its first failure,
	// before the new approach had one chance to do anything differently.
	tracker.seen = map[string]int{}
}

// verdict is what the tracker says to do after one failed attempt.
type verdict struct {
	// Escalated is the model to move to, empty when the rung is unchanged.
	Escalated string
	// Decompose is true when the ladder is spent and the request should be
	// broken into smaller units and tried again.
	Decompose bool
	// Why is what a person reads in the timeline.
	Why string
}

// record takes one failure and says whether the run should change something.
//
// Gate and failure are kept separate because the same message from two
// different gates is two different situations, and the same gate with two
// different messages is progress.
func (tracker *convergence) record(gate string, failure string) verdict {
	if tracker.seen == nil {
		tracker.seen = map[string]int{}
	}
	print := gate + "\x00" + failureFingerprint(failure)
	tracker.seen[print]++
	tracker.repeats = tracker.seen[print]
	if tracker.repeats < tracker.settings.StallBeforeEscalation {
		return verdict{}
	}
	if next, more := tracker.settings.NextRung(tracker.rung); more {
		from := tracker.rung
		tracker.rung = next
		tracker.escalations = append(tracker.escalations, from+" → "+next)
		// A fresh allowance, because the attempts just spent were spent
		// establishing that the previous model could not do this.
		tracker.grant()
		return verdict{
			Escalated: next,
			Why: "the same check failed the same way " +
				counted(tracker.settings.StallBeforeEscalation, "time") +
				" in a row on " + from + ", so this is a ceiling rather than " +
				"bad luck; moving to " + next,
		}
	}
	if tracker.decomposed || !tracker.settings.DecomposeWhenExhausted {
		return verdict{}
	}
	// Once. Decomposing repeatedly on the same stall would produce ever finer
	// units of a request that is not failing for being coarse.
	tracker.decomposed = true
	// Its own allowance too: building the work in pieces takes more rounds than
	// trying to land it whole, and a decomposition given the dregs of a spent
	// budget is the instruction without the attempts to carry it out.
	tracker.grant()
	return verdict{
		Decompose: true,
		Why: "the same check failed the same way on " + tracker.rung +
			", which is the top of the ladder, so the request is being split " +
			"into smaller pieces rather than asked again",
	}
}

// currentModel is the rung the run is on now.
func (tracker *convergence) currentModel() string {
	return tracker.rung
}

// summary says what the run did about not converging, for the ledger.
//
// The attempt count is part of it. Escalating is meant to be rare, and the
// only way to find out whether it is rare in practice is for every run to say
// what it did and what that took.
func (tracker *convergence) summary() string {
	spent := " after " + counted(tracker.total, "attempt")
	switch {
	case tracker.decomposed:
		return "escalated " + strings.Join(tracker.escalations, ", ") +
			" and then decomposed" + spent
	case len(tracker.escalations) > 0:
		return "escalated " + strings.Join(tracker.escalations, ", ") + spent
	default:
		return "converged on " + tracker.rung + " without escalating" + spent
	}
}

// varying is everything in a failure that changes between two runs of the same
// defect without the defect being different.
//
// A compiler error naming a temporary directory, a duration, a memory address
// or a line number that moved by one is the same failure. Comparing the raw
// text would see two different ones and never count a repeat, which would make
// the whole tracker inert — it would never fire, and nothing would say so.
var varying = regexp.MustCompile(
	`(0x[0-9a-fA-F]+)|` + // addresses
		`([0-9]+(\.[0-9]+)?(ns|µs|ms|s)\b)|` + // durations
		`(:[0-9]+:[0-9]+)|` + // file positions
		`([A-Za-z]:\\[^\s:]+|/tmp/[^\s:]+|/var/folders/[^\s:]+)|` + // temp paths
		`(\b[0-9]{4,}\b)`) // long numbers: pids, seeds, sizes

// failureFingerprint reduces a failure to what identifies it.
func failureFingerprint(failure string) string {
	reduced := varying.ReplaceAllString(failure, "#")
	reduced = strings.ToLower(strings.Join(strings.Fields(reduced), " "))
	// Long output is dominated by its tail — a stack trace, a diff of every
	// line — while what identifies the failure is at the front. Comparing the
	// whole thing would call two runs of one defect different because their
	// traces differed somewhere on line ninety.
	const identifying = 400
	if len(reduced) > identifying {
		reduced = reduced[:identifying]
	}
	return reduced
}

// counted renders a count with its noun, because "1 times" reads as a bug in
// the message and makes a reader doubt the number beside it.
func counted(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}
