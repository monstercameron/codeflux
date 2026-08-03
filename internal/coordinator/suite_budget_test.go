package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// TestTheProducedSuiteIsAlwaysBounded is the defect this closes.
//
// SuiteBudgetSeconds was declared, defaulted to sixty, and listed in the
// settings a person can see — and read by nothing. Every run of the produced
// suite went out unbounded, so a generated program that loops forever hung
// whichever stage reached it, indefinitely and silently.
//
// Observed on ladder rung 10 on 2026-08-03: the requirement asks for a
// shortest path through a grid, and the first program produced looped forever
// on the example the requirement itself supplies. `go test` in that worktree
// ran for five minutes without returning.
func TestTheProducedSuiteIsAlwaysBounded(t *testing.T) {
	if suiteTimeout == "" {
		t.Fatal("every run of the produced suite must carry a bound")
	}
	if !strings.HasPrefix(suiteTimeout, "-timeout=") {
		t.Errorf("the bound should be go test's own -timeout so the binary is "+
			"killed with a goroutine dump to read, got %q", suiteTimeout)
	}
}

// TestAnUnsetBudgetFallsBackRatherThanRemovingTheBound is the control.
//
// Zero must not mean "no timeout". A settings value nobody filled in is the
// most likely way this bound would quietly disappear again.
func TestAnUnsetBudgetFallsBackRatherThanRemovingTheBound(t *testing.T) {
	for _, seconds := range []int{0, -1} {
		settings := pipeline.DefaultSettings()
		settings.SuiteBudgetSeconds = seconds
		got := suiteTimeoutArgument(settings)
		if got == "-timeout=0s" || got == "" {
			t.Errorf("a budget of %d must fall back to a real bound, got %q",
				seconds, got)
		}
	}
}

// TestAConfiguredBudgetIsUsed pins that the setting is read at all, which is
// the thing that was not true.
func TestAConfiguredBudgetIsUsed(t *testing.T) {
	settings := pipeline.DefaultSettings()
	settings.SuiteBudgetSeconds = 17
	if got := suiteTimeoutArgument(settings); got != "-timeout=17s" {
		t.Errorf("got %q, want the configured budget", got)
	}
}
