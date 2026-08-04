package coordinator

import "testing"

// TestAFindingThatMovedDownTheFileIsTheSameFinding is the repeat the tracker
// could not see.
//
// A review names where it found something, and where it found something moves:
// every edit above the defect shifts it. The fingerprint already normalised a
// compiler's "main.go:37:12" — file, line and column — and a review writes
// "main.go:37" with no column, which fell straight through.
//
// Proven to discriminate: against the previous implementation these two
// fingerprint differently, so the tracker counts one failure twice as two
// separate failures and never escalates. Ladder rung 11 on 2026-08-03 was told
// "executeCommands nests 5 levels deep" at line 37, then at line 40, then at
// line 40 again, across four of its nine attempts. It stayed on the cheapest
// model for all of them and ran out of attempts with the same defect in place.
func TestAFindingThatMovedDownTheFileIsTheSameFinding(t *testing.T) {
	at37 := failureFingerprint(
		"cmd/generated/main.go:37: executeCommands nests 5 levels deep")
	at40 := failureFingerprint(
		"cmd/generated/main.go:40: executeCommands nests 5 levels deep")
	if at37 != at40 {
		t.Fatalf("the same finding at two line numbers must fingerprint the "+
			"same, or a run circling one defect never escalates:\n %q\n %q",
			at37, at40)
	}

	// The compiler's form, which already worked, must keep working.
	withColumn := failureFingerprint("main.go:37:12: undefined: run")
	otherColumn := failureFingerprint("main.go:41:9: undefined: run")
	if withColumn != otherColumn {
		t.Errorf("a position with a column should still normalise:\n %q\n %q",
			withColumn, otherColumn)
	}
}

// TestTwoDifferentFindingsStillFingerprintDifferently is the control.
//
// Normalising positions must not normalise the finding itself. A run that
// clears one defect and is shown a different one is making progress, and
// counting that as a repeat would escalate a converging run to a more expensive
// model and then stop it — spending money to punish work that was going well.
func TestTwoDifferentFindingsStillFingerprintDifferently(t *testing.T) {
	nesting := failureFingerprint(
		"cmd/generated/main.go:37: executeCommands nests 5 levels deep")
	exitCode := failureFingerprint(
		"cmd/generated/main.go:37: main exits zero whatever happens")
	if nesting == exitCode {
		t.Fatalf("two different findings must not collapse into one: %q",
			nesting)
	}

	// And two files are two places, even at the same line.
	inMain := failureFingerprint("main.go:37: executeCommands nests 5 levels deep")
	inTest := failureFingerprint("helper.go:37: executeCommands nests 5 levels deep")
	if inMain == inTest {
		t.Errorf("the file is part of what identifies a finding: %q", inMain)
	}
}
