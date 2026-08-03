package coordinator

import (
	"fmt"

	"codeflux.dev/codeflux/internal/pipeline"
)

// suiteTimeoutArgument is the -timeout flag every run of the produced suite
// carries, derived from the configured budget.
//
// SuiteBudgetSeconds was declared, defaulted to sixty, and listed in the
// settings a person can see — and read by nothing. The flow ran the produced
// tests with no time bound anywhere, so a generated program that loops forever
// hung whichever stage reached it, indefinitely and silently.
//
// That is not a corner case for a code-generation platform: it is the ordinary
// failure of a first attempt at a search. Ladder rung 10 asks for a shortest
// path through a grid, and the first program it produced looped forever on the
// example the requirement gives. The stage that ran it did not fail, did not
// time out, and did not report — it stopped, and so did the run.
//
// go test's own -timeout is used rather than a context deadline alone because
// it kills the test binary and prints the goroutine dump that says where it
// hung, which is the thing a person or a next attempt needs. A context
// deadline kills the process and leaves nothing to read.
func suiteTimeoutArgument(settings pipeline.Settings) string {
	seconds := settings.SuiteBudgetSeconds
	if seconds <= 0 {
		seconds = pipeline.DefaultSettings().SuiteBudgetSeconds
	}
	return fmt.Sprintf("-timeout=%ds", seconds)
}

// suiteTimeout is the flag the produced suite is run with.
//
// It is derived from the default settings rather than from the run's own,
// because the checks that run the suite are free functions that were never
// given the settings and threading them through every one is a wider change
// than closing the hang. A run that configures a different budget therefore
// still gets the default bound here, which is a real limitation and is
// stated rather than hidden: the point of this value is that there is a bound
// at all, where before there was none.
var suiteTimeout = suiteTimeoutArgument(pipeline.DefaultSettings())
