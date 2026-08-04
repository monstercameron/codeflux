package coordinator

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/tracing"
)

// The trace primitives live in internal/tracing so the model client can write
// to the same stream. A trace that shows which stage is running and not what
// was sent to the model answers "where is it" and not "why". The environment
// variable that turns it on is tracing.Environment; this package reads it
// through tracing.Enabled rather than naming it a second time.

// traceEnabled reports whether a run should narrate itself to stderr.
func traceEnabled() bool { return tracing.Enabled() }

// tracef writes one line of live commentary to stderr.
func tracef(category, format string, arguments ...any) {
	tracing.Printf(category, format, arguments...)
}

// traceBlock writes a multi-line body under one heading.
func traceBlock(category, heading, body string) {
	tracing.Block(category, heading, body)
}

// traceOneLine flattens text to a single line, shortened to a limit.
func traceOneLine(text string, limit int) string {
	return tracing.OneLine(text, limit)
}

// traceStage narrates one stage decision as it is recorded.
//
// The stage's own gate is printed beside its verdict, because "atom-fuzz
// skipped" says nothing on its own and "atom-fuzz skipped — the gate wanted a
// decoding boundary and the source has none" is the whole answer.
//
// The severity is printed beside every decision, because a reader watching a
// run go past cannot otherwise tell a failure that will stop the work from one
// that will be carried as a note, and the two look identical in a log. So is
// the elapsed time, because "which stage is slow" should not require
// subtracting timestamps by eye.
func traceStage(
	stage pipeline.Number, state pipeline.State, detail string,
	started time.Time,
) {
	if !traceEnabled() {
		return
	}
	name, gate := "unknown", ""
	if declared, found := pipeline.StageByNumber(stage); found {
		name, gate = declared.Name, declared.Gate
	}
	took := time.Duration(0)
	if !started.IsZero() {
		took = time.Since(started)
	}
	recordPhaseTime(stage, started, took)
	elapsed := ""
	if took > 0 {
		elapsed = fmt.Sprintf("%6.2fs", took.Seconds())
	}
	tracef("stage", "%2d %-26s %-16s %-8s %8s  %s",
		int(stage), name, state, severityOf(stage), elapsed,
		traceOneLine(detail, 110))
	if state != pipeline.StateSatisfied && gate != "" {
		tracef("gate", "   %-26s wanted: %s", "", traceOneLine(gate, 130))
	}
}

// Phase accounting, so a reader can see where a run spends itself.
//
// Phases are not contiguous in this flow, and pretending they are produced
// nonsense: stages are decided in dependency order rather than in numeric
// order, so a run bounced between "specification" and "delivery" twenty-three
// times in four minutes and the boundaries said nothing.
//
// What a reader wants is not a line every time the label changes. It is how
// much of the run each phase accounted for, which is a total rather than a
// sequence. So a phase is announced once, the first time one of its stages is
// decided, and its total is reported at the end.

// phaseInterval is one window in which a stage of a phase was running,
// measured from the run's own start.
//
// Kept as intervals and merged at the end rather than accumulated into a total,
// because neither simpler answer is true here. Summing per-stage durations
// counts the same seconds many times over: stages inside a phase run
// concurrently and are re-decided on every attempt, and that reported
// "specification 2991.96s 77.6% of measured" for a run lasting 371 seconds.
// Taking each phase's first-to-last span is no better, because phases
// interleave rather than run in sequence — a run bounces between
// specification and delivery for its whole length — so two phases each claimed
// 100% of the run and the unaccounted remainder came out as zero.
//
// The union of the intervals is the honest answer to "how much of the run was
// this phase running", and it is the only one that leaves the remainder
// meaning what it says.
type phaseInterval struct {
	from time.Duration
	to   time.Duration
}

var (
	phaseMutex   sync.Mutex
	phaseWindows = map[pipeline.Phase][]phaseInterval{}
	phaseSeen    = map[pipeline.Phase]bool{}
	phaseOrder   []pipeline.Phase
)

// recordPhaseTime announces a phase the first time it is entered and widens its
// span to include this stage.
func recordPhaseTime(
	stage pipeline.Number, started time.Time, elapsed time.Duration,
) {
	phase, known := pipeline.PhaseOf(stage)
	if !known {
		return
	}
	phaseMutex.Lock()
	defer phaseMutex.Unlock()
	if !phaseSeen[phase] {
		phaseSeen[phase] = true
		phaseOrder = append(phaseOrder, phase)
		fmt.Fprintf(os.Stderr, "[%7.1fs] %-9s ── %s begins ──\n",
			tracing.Since().Seconds(), "phase", phase)
	}
	// A stage with no recorded start contributes only its own duration, which
	// is the best available answer and never reaches past the run itself.
	finished := tracing.Since()
	begin := finished - elapsed
	if !started.IsZero() {
		begin = finished - time.Since(started)
	}
	if begin < 0 {
		begin = 0
	}
	phaseWindows[phase] = append(
		phaseWindows[phase], phaseInterval{from: begin, to: finished})
}

// mergedDuration is how long at least one of these intervals was open.
func mergedDuration(windows []phaseInterval) time.Duration {
	if len(windows) == 0 {
		return 0
	}
	ordered := append([]phaseInterval(nil), windows...)
	sort.Slice(ordered, func(first, second int) bool {
		return ordered[first].from < ordered[second].from
	})
	var total time.Duration
	current := ordered[0]
	for _, window := range ordered[1:] {
		if window.from > current.to {
			total += current.to - current.from
			current = window
			continue
		}
		if window.to > current.to {
			current.to = window.to
		}
	}
	return total + current.to - current.from
}

// tracePhaseTotals reports what each phase accounted for, once, at the end.
//
// The per-stage lines answer "what happened". This answers "where did the time
// go", which is the question somebody watching a four-minute run actually has
// and which reading individual lines answers slowly if at all.
//
// The unaccounted remainder is the most useful number in it. On a run whose
// stages measured nine seconds out of four minutes, the answer to "why is this
// slow" is not in any stage — it is the model calls and the tool runs between
// them, and saying so plainly is better than leaving a reader to subtract.
func tracePhaseTotals() {
	if !traceEnabled() {
		return
	}
	phaseMutex.Lock()
	defer phaseMutex.Unlock()
	if len(phaseOrder) == 0 {
		return
	}
	wall := tracing.Since()
	totals := make(map[pipeline.Phase]time.Duration, len(phaseOrder))
	var everything []phaseInterval
	for _, phase := range phaseOrder {
		totals[phase] = mergedDuration(phaseWindows[phase])
		everything = append(everything, phaseWindows[phase]...)
	}
	// Measured is the union across every phase, not the sum of the per-phase
	// unions. Phases overlap each other as well as themselves — specification
	// and delivery are both running for most of a run — so adding their totals
	// exceeds the wall clock and drives the remainder to zero, which is how
	// this line came to read 0.00s on a run with four minutes of model calls in
	// it. Each phase's own figure is still its own union, which is why the
	// percentages can and should add to more than a hundred.
	measured := mergedDuration(everything)
	fmt.Fprintf(os.Stderr, "[%7.1fs] %-9s ── where the time went ──\n",
		tracing.Since().Seconds(), "phase")
	for _, phase := range phaseOrder {
		share := 0.0
		if wall > 0 {
			share = float64(totals[phase]) / float64(wall) * 100
		}
		fmt.Fprintf(os.Stderr, "%22s| %-16s %7.2fs  %5.1f%% of the run\n",
			"", phase, totals[phase].Seconds(), share)
	}
	// Always printed, including when it is nothing. The remainder is the point
	// of the report — a run whose phases account for nine seconds out of four
	// minutes has its answer here and nowhere else — and a line that appears
	// only when it happens to be positive is one a reader cannot rely on.
	gap := wall - measured
	if gap < 0 {
		gap = 0
	}
	share := 0.0
	if wall > 0 {
		share = float64(gap) / float64(wall) * 100
	}
	fmt.Fprintf(os.Stderr,
		"%22s| %-16s %7.2fs  %5.1f%% — model calls, tool runs, and waiting\n",
		"", "unaccounted", gap.Seconds(), share)
}
