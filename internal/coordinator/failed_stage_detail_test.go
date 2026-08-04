package coordinator

import (
	"strings"
	"testing"
)

// TestAFailedStageGetsRoomToSayWhy is the diagnosis gap in the stage trace.
//
// A hundred and ten characters is enough to skim a satisfied stage and too few
// to act on a failed one. atom-registration reported "0 of 1 produced atom(s)
// reached the registry, so no later task can find this work: Calculate:
// documentation wa" — the line ends exactly where the only actionable part
// begins, and reading the reason means opening the run's database. That is the
// same shape as the provider failures, which named a category and stopped
// before the cause.
func TestAFailedStageGetsRoomToSayWhy(t *testing.T) {
	const detail = "0 of 1 produced atom(s) reached the registry, so no later " +
		"task can find this work: Calculate: documentation was rejected " +
		"because the Preconditions field is missing, and every field of the " +
		"schema is required so that a later task reading the registry is " +
		"reading the same shape every time"

	// The satisfied width, which is what the line used to get regardless.
	skimmed := traceOneLine(detail, 110)
	if strings.Contains(skimmed, "Preconditions") {
		t.Fatal("the fixture is not long enough to demonstrate the truncation")
	}

	// The width a failure now gets.
	full := traceOneLine(detail, 420)
	if !strings.Contains(full, "Preconditions") {
		t.Errorf("a failed stage still stops before its reason: %q", full)
	}
}

// TestASatisfiedStageStaysShort is the control.
//
// Most stages are satisfied, and most of those say something a reader skims
// past. Widening every line would bury the failures among them, which is the
// opposite of what this is for.
func TestASatisfiedStageStaysShort(t *testing.T) {
	long := strings.Repeat("every one of the changed lines is executed. ", 20)
	if got := traceOneLine(long, 110); len(got) > 130 {
		t.Errorf("a satisfied stage's line is %d characters", len(got))
	}
}
