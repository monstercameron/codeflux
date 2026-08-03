// Package tracing is the live account a run gives of itself while it runs.
//
// A run's narration goes to the database and its stage results go to the
// ledger, both of which are read after the fact. Neither helps somebody
// watching a run that is taking too long or stopping somewhere unexpected, and
// diagnosing that has meant reading a goroutine dump or querying SQLite while
// the run was still going.
//
// It lives in its own package because the two things most worth watching are
// produced in different layers: the coordinator knows which stage is running,
// and the model client knows what was actually sent and what came back. A trace
// that has the first and not the second answers "where is it" and not "why".
package tracing

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Environment turns the trace on.
const Environment = "CODEFLUX_TRACE"

var (
	once    sync.Once
	wanted  bool
	started time.Time
	mutex   sync.Mutex
)

// Enabled reports whether a run should narrate itself to stderr.
func Enabled() bool {
	once.Do(func() {
		wanted = strings.TrimSpace(os.Getenv(Environment)) != ""
		started = time.Now()
	})
	return wanted
}

// Since is how long the traced process has been running.
func Since() time.Duration {
	if !Enabled() {
		return 0
	}
	return time.Since(started)
}

// Printf writes one line of live commentary to stderr.
//
// stderr, not the test log: Go buffers a subtest's log output until the subtest
// ends, which is precisely when a person watching a slow run stops needing it.
// Writing to stderr streams as it happens.
//
// The elapsed prefix is what makes it useful for the thing it was added for.
// "Which stage is slow" is answered by the gaps between lines, and a bare
// sequence of stage names does not answer it.
func Printf(category, format string, arguments ...any) {
	if !Enabled() {
		return
	}
	mutex.Lock()
	defer mutex.Unlock()
	fmt.Fprintf(os.Stderr, "[%7.1fs] %-9s %s\n",
		time.Since(started).Seconds(), category,
		fmt.Sprintf(format, arguments...))
}

// Block writes a multi-line body under one heading, indented so it reads as one
// thing rather than as several unrelated lines.
func Block(category, heading, body string) {
	if !Enabled() || strings.TrimSpace(body) == "" {
		return
	}
	Printf(category, "%s", heading)
	mutex.Lock()
	defer mutex.Unlock()
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "%22s| %s\n", "", line)
	}
}

// OneLine flattens text to a single line, shortened to a limit.
func OneLine(text string, limit int) string {
	flattened := strings.Join(strings.Fields(text), " ")
	if limit > 0 && len(flattened) > limit {
		return flattened[:limit] + "…"
	}
	return flattened
}

// Excerpt keeps the head and the tail of a long body and says how much it
// dropped.
//
// A prompt is thousands of characters and its middle is repository context the
// reader already knows. What they need is how it opens — the instruction — and
// how it closes, which is where the specific ask usually is. Dropping the
// middle silently would misrepresent the size of what was sent, so the count is
// stated.
func Excerpt(body string, head, tail int) string {
	trimmed := strings.TrimRight(body, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= head+tail {
		return trimmed
	}
	var kept []string
	kept = append(kept, lines[:head]...)
	kept = append(kept, fmt.Sprintf(
		"… %d line(s) omitted …", len(lines)-head-tail))
	kept = append(kept, lines[len(lines)-tail:]...)
	return strings.Join(kept, "\n")
}
