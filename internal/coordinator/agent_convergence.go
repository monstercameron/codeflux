package coordinator

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// health answers whether a rung is currently down, so escalation does not
	// move a run onto a provider that just refused another one. Nil means the
	// process-wide record, which is what production uses.
	health     func(rung string) (string, bool)
	decomposed bool
	// extended records the one extra allowance a still-converging run is given
	// when its budget runs out with gates left. Once per run: a second would
	// make the ceiling advisory.
	extended bool
	// refunded counts attempts given back because the machinery failed rather
	// than the work. Bounded so a provider that stays down cannot loop.
	refunded int
}

// regressionProneGates are the ones whose second failure means a property was
// lost rather than never held.
//
// Compilation, the suite, and the acceptance examples are all things a run
// establishes and can then undo. Failing one of them twice is a different
// situation from grinding against a coverage threshold that has never been met:
// the first says the run is going round, the second says it is going slowly.
//
// The research gates are deliberately absent. A run can legitimately need
// several rounds to cover its cases, and escalating on the second would spend a
// stronger model on ordinary progress.
// escalationWouldNotHelp names the gates a dearer model cannot satisfy any
// better than a cheap one.
//
// Both ask for text the run has already been given in full: which declarations
// lack a comment, and which fields the schema wants in which order. Nothing
// about that is a reasoning problem, and escalating on it spends the ladder's
// most expensive rung on typing.
var escalationWouldNotHelp = map[string]bool{
	"atom-documentation": true,
	"completeness":       true,
}

var regressionProneGates = map[string]bool{
	"assembly":          true,
	"integration-tests": true,
	"acceptance":        true,
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

// refund gives back an attempt that was never spent on the work.
//
// A provider that would not answer, a transport that closed mid-turn: the run
// learned nothing, produced nothing, and left the worktree exactly as it found
// it. Charging it an attempt spends the run's budget on the machinery's behalf,
// and a run that was two gates from finished can die of a socket.
//
// Bounded, because a provider that is down tends to stay down. Past the bound
// the run pays for the attempt like any other and the budget ends it, which is
// the correct outcome for a provider that is not coming back.
func (tracker *convergence) refund() {
	if tracker.refunded >= tracker.settings.MaximumAttempts {
		return
	}
	tracker.refunded++
	if tracker.spent > 0 {
		tracker.spent--
	}
	if tracker.total > 0 {
		tracker.total--
	}
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
	// Converging is the opposite of stalling, and stalling has a definition
	// already: record escalates when a failure has repeated
	// StallBeforeEscalation times, three by default. Reading any repeat at all
	// as not-converging made this the only place in the file where two was
	// enough, so a run the tracker had never once called stalled was refused
	// the extension that exists for exactly that run.
	//
	// It is not a hypothetical gap. The fingerprint is over the gate and its
	// one-line reason, not over what was asked, and adversarial-review's reason
	// is the fixed sentence "a review found it weaker than it looks". So a
	// review that raises two entirely different findings across two rounds —
	// which is what a review bounded to two findings a round is built to do —
	// records as the same failure twice. Ladder rung 5 on 2026-08-03 was denied
	// its extension that way while every attempt had cleared a different gate.
	//
	// The regression-prone gates keep their lower bar, because losing a
	// property you had already satisfied is a cycle at two and record says so.
	// Counted per failure and deliberately not per gate.
	//
	// Counting visits to a gate instead would read "cleared example 1, now
	// example 2 fails" as going round, when it is the clearest possible sign of
	// progress. Ladder rung 15 on 2026-08-03 returned to integration-tests
	// three times with a different set of tests red each time, and that is the
	// same shape: some fixed, others revealed. Escalating on it would raise the
	// price of every run that is working to catch the few that are not, which
	// is the trade TestARunThatConvergesCostsWhatItAlwaysDid exists to refuse.
	for print, count := range tracker.seen {
		threshold := tracker.settings.StallBeforeEscalation
		if gate, _, found := strings.Cut(print, fingerprintSeparator); found &&
			regressionProneGates[gate] && threshold > 2 {
			threshold = 2
		}
		if count >= threshold {
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
	// A rung above and a decomposition are allowances only a stall collects.
	//
	// Both are granted inside record, and record grants them when a failure has
	// repeated often enough to count as a stall. A run that never repeats
	// itself never reaches that branch, so treating them as available made this
	// predicate answer "something follows this attempt" for a run where nothing
	// did: the work was sent back, moreAttempts disagreed, and the run stopped
	// between being told and being able to act.
	//
	// Ladder rung 5 on 2026-08-03 ended exactly there, twice. Six attempts, six
	// different gates, nothing repeated — then path-coverage was raised at
	// 155.9s naming one uncovered line out of thirty-three, and the next trace
	// line is the run finishing. It was told, and then denied the attempt.
	//
	// So they count only when the run is actually stalling. What remains for a
	// converging run is the one-time extension below, which moreAttempts grants
	// on the same terms — which is the point: the two must answer the same
	// question the same way.
	if !tracker.converging() {
		if _, more := tracker.settings.NextRung(tracker.rung); more {
			return false
		}
		if !tracker.decomposed && tracker.settings.DecomposeWhenExhausted {
			return false
		}
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
func (tracker *convergence) record(
	gate string, failure string, asked string,
) verdict {
	if tracker.seen == nil {
		tracker.seen = map[string]int{}
	}
	// What was asked, not only which gate asked it.
	//
	// The reason a gate gives is a fixed sentence per gate:
	// adversarial-review's is always "a review found it weaker than it looks".
	// Keyed on that alone, a review raising two entirely different findings
	// across two rounds records as the same failure twice — and a review
	// bounded to a couple of findings a round is built to fire more than once.
	//
	// Ladder rung 9 on 2026-08-03 ran three review rounds, each addressing a
	// different finding and each verified, and the tracker read three identical
	// failures. It called the run stalled, withheld the extension, and the run
	// ended at 359 seconds with four minutes of budget unused and two gates it
	// had never been asked about.
	//
	// The instruction distinguishes them: the same finding asked again produces
	// the same instruction and still counts as a repeat, which is what a stall
	// is. All three are in the key, so a repeat needs the gate, the reason and
	// the ask to match.
	print := gate + fingerprintSeparator + failureFingerprint(failure) +
		fingerprintSeparator + failureFingerprint(asked)
	tracker.seen[print]++
	tracker.repeats = tracker.seen[print]
	// Losing a property twice is a cycle, not a coincidence.
	//
	// The stall threshold is three, which is right for a gate a run is grinding
	// against and wrong for one it had already satisfied. A run whose
	// acceptance examples passed and then stopped passing has undone its own
	// work, and a run that does that twice is going round rather than forward.
	// Ladder rung 5 lost and regained its acceptance output across four
	// attempts on the lowest rung, and by the tracker's count nothing had
	// repeated often enough to be a stall.
	//
	// Counted per gate over the whole run rather than consecutively, because a
	// cycle is by definition not consecutive: the failures alternate with the
	// fixes, which is what makes it a cycle and what made it invisible.
	threshold := tracker.settings.StallBeforeEscalation
	if regressionProneGates[gate] && threshold > 2 {
		threshold = 2
	}
	if tracker.repeats < threshold {
		return verdict{}
	}
	// A stronger model is not the remedy for a missing comment.
	//
	// Escalation buys reasoning, and the gates below do not need any: the run
	// was told which declarations lack a doc comment and which nineteen fields
	// the schema wants, and a model that did not do it was not held back by
	// its capacity. Ladder rung 6 escalated to the most expensive rung over
	// work of exactly this kind and then spent 165 seconds there before the
	// provider gave out.
	//
	// The run keeps its attempts and stays where it is. If it cannot do the
	// work at this rung it will not do it at a dearer one, and the gate is
	// advisory anyway.
	if escalationWouldNotHelp[gate] {
		return verdict{Why: "the " + gate + " gate is asking for text rather " +
			"than for judgement, so a stronger model would not help; the run " +
			"stays where it is"}
	}
	if next, more := tracker.settings.NextRung(tracker.rung); more {
		// A rung that is known to be down is not an escalation.
		//
		// Escalating onto it spends the run: the request fails at the
		// transport, the adapter exhausts its own retries, and a run that was
		// still making progress is abandoned for a reason that has nothing to
		// do with the work. Ladder rung 15 on 2026-08-04 died that way in four
		// of five passes, every one on the first request after moving up.
		// Staying put is worse than moving up and better than stopping, so the
		// run keeps the fresh allowance and spends it where it can be answered.
		if why, down := tracker.rungIsDown(next); down {
			tracker.grant()
			return verdict{
				Why: next + " is unavailable (" + why + "), so moving up would " +
					"end the run rather than strengthen it; staying on " +
					tracker.rung + " with a fresh allowance",
			}
		}
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

// stepDown moves the run back to the rung it escalated from, and says whether
// there was one.
//
// Only a rung this run actually climbed from, read off the escalations it
// recorded rather than off the ladder. Reading the ladder would let a run that
// started high drop to a model it was never granted, and reading it as "the
// rung below" would let a run drop repeatedly on one outage. Each step down
// consumes the escalation that produced it, so a run can only come back down
// the way it went up.
//
// The allowance is refreshed on the way down for the same reason it was on the
// way up: the attempts that established the cheaper model's ceiling were spent
// establishing it, and the run is now going to have to work within that ceiling
// after all.
func (tracker *convergence) stepDown() (bool, string) {
	if len(tracker.escalations) == 0 {
		return false, ""
	}
	last := tracker.escalations[len(tracker.escalations)-1]
	from, _, found := strings.Cut(last, " → ")
	if !found || from == "" {
		return false, ""
	}
	tracker.escalations = tracker.escalations[:len(tracker.escalations)-1]
	unreachable := tracker.rung
	tracker.rung = from
	tracker.grant()
	return true, unreachable + " did not answer"
}

// rungIsDown reports whether a rung gave up on some run inside the health
// window, and why.
//
// Injectable so a test can state the fact rather than arrange an outage. The
// default reads the process-wide record, which is where a run that exhausted a
// provider wrote what it found out.
func (tracker *convergence) rungIsDown(rung string) (string, bool) {
	if tracker.health != nil {
		return tracker.health(rung)
	}
	return sharedProviderHealth.recentlyExhausted(rung, time.Now())
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
//
// A line number with no column beside it counts too. A compiler writes
// "main.go:37:12" and a review writes "main.go:37", and the review is the one
// whose findings move: every edit above the defect shifts it. Ladder rung 11 on
// 2026-08-03 was told "executeCommands nests 5 levels deep" at line 37, then at
// line 40, then at line 40 again across four attempts, escalated for none of
// them because each read as a new failure, and ran out of attempts still on the
// first model.
var varying = regexp.MustCompile(
	`(0x[0-9a-fA-F]+)|` + // addresses
		`([0-9]+(\.[0-9]+)?(ns|µs|ms|s)\b)|` + // durations
		`(\.[A-Za-z0-9]+:[0-9]+(:[0-9]+)?)|` + // file positions, with or without a column
		`(:[0-9]+:[0-9]+)|` + // a position whose file is not named here
		`([A-Za-z]:\\[^\s:]+|/tmp/[^\s:]+|/var/folders/[^\s:]+)|` + // temp paths
		`(\b[0-9]{4,}\b)`) // long numbers: pids, seeds, sizes

// failingTests matches the line a Go test suite prints for each test that
// failed, capturing the name.
var failingTests = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)

// suiteFingerprint identifies a failing suite by which tests failed.
//
// A suite failure's prose is dominated by the values it compared, and those
// change on every round a run spends getting closer without arriving. The thing
// that identifies the failure is which tests are red. Ladder rung 15 on
// 2026-08-03 failed TestGivesLeftmostGapsRemainderSpaces and
// TestDistributesExtraSpacesEvenly eighteen times each across eight attempts,
// and escalated for none of them, because each round's expected-versus-actual
// text was new.
//
// Empty when the failure is not a suite run, so every other failure keeps the
// prose fingerprint it has always had. Fixing one of three failing tests does
// change this, which is right: that is progress, and the tracker should see it.
func suiteFingerprint(failure string) string {
	matches := failingTests.FindAllStringSubmatch(failure, -1)
	if len(matches) == 0 {
		return ""
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		// Subtests are the parent's failure said again. The parent line is
		// always present, so counting both would make one failing test look
		// like several and a run splitting a test look like a new failure.
		name := match[1]
		if slash := strings.Index(name, "/"); slash >= 0 {
			name = name[:slash]
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return "failing tests: " + strings.Join(names, " ")
}

// failureFingerprint reduces a failure to what identifies it.
func failureFingerprint(failure string) string {
	if suite := suiteFingerprint(failure); suite != "" {
		return suite
	}
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

// currentRung is the model this run is on now, for the live trace.
func (tracker *convergence) currentRung() string {
	if tracker == nil {
		return "unknown"
	}
	return tracker.rung
}

// fingerprintSeparator joins a gate to its failure fingerprint in the key the
// tracker counts repeats under. Named rather than written inline so the two
// places that build and read the key cannot drift.
const fingerprintSeparator = "\x00"
