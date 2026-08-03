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
	tracePhaseBoundary(stage)
	// The severity is printed beside every decision. A reader watching a run go
	// past cannot otherwise tell a failure that will stop the work from one
	// that will be carried as a note, and the two look identical in a log.
	// Each stage carries how long it took, so a reader can see which one is
	// slow without subtracting timestamps by eye.
	elapsed := ""
	if !started.IsZero() {
		elapsed = fmt.Sprintf("%6.2fs", time.Since(started).Seconds())
	}
	tracef("stage", "%2d %-26s %-16s %-8s %8s  %s",
		int(stage), name, state, severityOf(stage), elapsed,
		traceOneLine(detail, 110))
	if state != pipeline.StateSatisfied && gate != "" {
		tracef("gate", "   %-26s wanted: %s", "", traceOneLine(gate, 130))
	}
}

// Phase and stage timing, so a reader can see where a run spends itself.
//
// The stage lines alone say what happened and in what order. What they do not
// say is how long any of it took, and "which part of this is slow" is the
// question somebody watching a three-hundred-second run is actually asking.
var (
	timingMutex   sync.Mutex
	currentPhase  pipeline.Phase
	phaseStarted  time.Time
	phaseOrdinals int
)

// tracePhaseBoundary announces a phase when the first of its stages is decided,
// and closes the previous one with what it cost.
//
// Phases are what the flow is organised around — contracts, atoms, molecules,
// the program, delivery — and a run that spends four minutes in one of them has
// said something a stage-by-stage reading makes you assemble for yourself.
func tracePhaseBoundary(stage pipeline.Number) {
	phase, known := pipeline.PhaseOf(stage)
	if !known {
		return
	}
	timingMutex.Lock()
	defer timingMutex.Unlock()
	if phase == currentPhase {
		return
	}
	if currentPhase != "" {
		fmt.Fprintf(os.Stderr,
			"[%7.1fs] %-9s ── %s ended after %.1fs ──\n",
			tracing.Since().Seconds(), "phase", currentPhase,
			time.Since(phaseStarted).Seconds())
	}
	phaseOrdinals++
	currentPhase, phaseStarted = phase, time.Now()
	fmt.Fprintf(os.Stderr, "[%7.1fs] %-9s ── phase %d: %s began ──\n",
		tracing.Since().Seconds(), "phase", phaseOrdinals, phase)
}
