package coordinator

import (
	"strings"
	"testing"
)

// TestAnInstructionDoesNotClaimTheTestsPassed is the false statement this
// closes.
//
// Three instruction builders opened with the fixed words "The code compiles
// and its tests pass, but…". That is a claim, and it was made whether or not
// anything had established it.
//
// Observed on ladder rung 10 on 2026-08-03: the produced program looped
// forever, its test run was cancelled, and the model was then told its tests
// passed and asked to improve its coverage — reasoning from a premise the run
// had every reason to know was false.
func TestAnInstructionDoesNotClaimTheTestsPassed(t *testing.T) {
	unproven := validationPreamble(false)
	if strings.Contains(unproven, "tests pass,") {
		t.Errorf("nothing established that the tests passed, so this must not "+
			"say they did: %q", unproven)
	}
	if !strings.Contains(unproven, "compiles") {
		t.Errorf("compiling is established by the assembly stage and may still "+
			"be stated: %q", unproven)
	}
	if !strings.Contains(unproven, "not been shown to pass") {
		t.Errorf("the wording should say what is unknown rather than go "+
			"silent: %q", unproven)
	}
}

// TestAProvenSuiteIsStillStatedPlainly is the control.
//
// Saying less when nothing is known must not turn into saying less when
// something is: a run whose suite genuinely passed is told so, because that is
// context the next attempt uses.
func TestAProvenSuiteIsStillStatedPlainly(t *testing.T) {
	proven := validationPreamble(true)
	if !strings.Contains(proven, "its tests pass") {
		t.Errorf("a suite that passed should be stated as passing: %q", proven)
	}
}
