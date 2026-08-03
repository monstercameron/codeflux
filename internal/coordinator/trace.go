package coordinator

import (
	"fmt"
	"os"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/tracing"
)

// The trace primitives live in internal/tracing so the model client can write
// to the same stream. A trace that shows which stage is running and not what
// was sent to the model answers "where is it" and not "why".
const traceEnvironment = tracing.Environment

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
	recordPhaseTime(stage, took)
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
var (
	phaseMutex  sync.Mutex
	phaseTotals = map[pipeline.Phase]time.Duration{}
	phaseSeen   = map[pipeline.Phase]bool{}
	phaseOrder  []pipeline.Phase
)

// recordPhaseTime announces a phase the first time it is entered and adds this
// stage's cost to its total.
func recordPhaseTime(stage pipeline.Number, elapsed time.Duration) {
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
	phaseTotals[phase] += elapsed
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
	var measured time.Duration
	for _, phase := range phaseOrder {
		measured += phaseTotals[phase]
	}
	fmt.Fprintf(os.Stderr, "[%7.1fs] %-9s ── where the time went ──\n",
		tracing.Since().Seconds(), "phase")
	for _, phase := range phaseOrder {
		total := phaseTotals[phase]
		share := 0.0
		if measured > 0 {
			share = float64(total) / float64(measured) * 100
		}
		fmt.Fprintf(os.Stderr, "%22s| %-16s %7.2fs  %5.1f%% of measured\n",
			"", phase, total.Seconds(), share)
	}
	if gap := tracing.Since() - measured; gap > 0 {
		fmt.Fprintf(os.Stderr,
			"%22s| %-16s %7.2fs  model calls, tool runs, and waiting\n",
			"", "unaccounted", gap.Seconds())
	}
}
