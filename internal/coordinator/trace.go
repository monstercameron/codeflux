package coordinator

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/pipeline"
)

// traceEnvironment turns on a live account of what a run is doing.
//
// A run's own narration goes to the database and its stage results go to the
// ledger, both of which are read after the fact. Neither helps somebody
// watching a run that is taking too long or stopping somewhere unexpected, and
// diagnosing that has meant reading a goroutine dump or querying SQLite while
// the run was still going.
const traceEnvironment = "CODEFLUX_TRACE"

// traceOnce guards reading the environment exactly once.
var (
	traceOnce    sync.Once
	traceWanted  bool
	traceStarted time.Time
	traceMutex   sync.Mutex
)

// traceEnabled reports whether a run should narrate itself to stderr.
func traceEnabled() bool {
	traceOnce.Do(func() {
		traceWanted = strings.TrimSpace(os.Getenv(traceEnvironment)) != ""
		traceStarted = time.Now()
	})
	return traceWanted
}

// tracef writes one line of live commentary to stderr.
//
// stderr, not the test log: Go buffers a subtest's log output until the
// subtest ends, which is precisely when a person watching a slow run stops
// needing it. Writing to stderr streams as it happens.
//
// The elapsed prefix is what makes it useful for the thing it was added for.
// "which stage is slow" is answered by the gaps between lines, and a bare
// sequence of stage names does not answer it.
func tracef(category, format string, arguments ...any) {
	if !traceEnabled() {
		return
	}
	traceMutex.Lock()
	defer traceMutex.Unlock()
	fmt.Fprintf(os.Stderr, "[%7.1fs] %-9s %s\n",
		time.Since(traceStarted).Seconds(), category,
		fmt.Sprintf(format, arguments...))
}

// traceBlock writes a multi-line body under one heading, indented so it reads
// as one thing rather than as several unrelated lines.
//
// Instructions are the reason this exists: they are the part of a run hardest
// to reconstruct afterwards, because they are assembled from several gates and
// never stored whole.
func traceBlock(category, heading, body string) {
	if !traceEnabled() {
		return
	}
	traceMutex.Lock()
	defer traceMutex.Unlock()
	fmt.Fprintf(os.Stderr, "[%7.1fs] %-9s %s\n",
		time.Since(traceStarted).Seconds(), category, heading)
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "                    | %s\n", line)
	}
}

// traceOneLine collapses text to a single readable line of bounded length.
func traceOneLine(text string, limit int) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	if limit > 0 && len(collapsed) > limit {
		return collapsed[:limit] + "…"
	}
	return collapsed
}

// traceStage narrates one stage decision as it is recorded.
//
// The stage's own gate is printed beside its verdict, because "atom-fuzz
// skipped" says nothing on its own and "atom-fuzz skipped — the gate wanted a
// decoding boundary and the source has none" is the whole answer.
func traceStage(stage pipeline.Number, state pipeline.State, detail string) {
	if !traceEnabled() {
		return
	}
	name, gate := "unknown", ""
	if declared, found := pipeline.StageByNumber(stage); found {
		name, gate = declared.Name, declared.Gate
	}
	tracef("stage", "%2d %-26s %-16s %s",
		int(stage), name, state, traceOneLine(detail, 130))
	if state != pipeline.StateSatisfied && gate != "" {
		tracef("gate", "   %-26s wanted: %s", "", traceOneLine(gate, 130))
	}
}
