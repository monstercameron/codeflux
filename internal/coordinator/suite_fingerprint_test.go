package coordinator

import "testing"

// TestTheSameFailingTestsAreTheSameFailure is the repeat a suite run hides.
//
// A failing suite's prose is mostly the values it compared, and a run getting
// closer without arriving changes those values every round. What identifies the
// failure is which tests are red.
//
// Proven to discriminate: against the previous implementation these two
// fingerprint differently, because the prose diverges within the first four
// hundred characters. Ladder rung 15 on 2026-08-03 failed
// TestGivesLeftmostGapsRemainderSpaces and TestDistributesExtraSpacesEvenly
// eighteen times each across eight attempts and escalated for none of them.
func TestTheSameFailingTestsAreTheSameFailure(t *testing.T) {
	first := failureFingerprint(
		"--- FAIL: TestDistributesExtraSpacesEvenly (0.00s)\n" +
			"    main_test.go:31: got \"a  b c\", want \"a  b  c\"\n" +
			"--- FAIL: TestGivesLeftmostGapsRemainderSpaces (0.00s)\n" +
			"    main_test.go:44: got \"x y  z\", want \"x  y z\"\n")
	second := failureFingerprint(
		"--- FAIL: TestDistributesExtraSpacesEvenly (0.00s)\n" +
			"    main_test.go:31: got \"a b  c\", want \"a  b  c\"\n" +
			"--- FAIL: TestGivesLeftmostGapsRemainderSpaces (0.00s)\n" +
			"    main_test.go:44: got \"x  y z\", want \"x y  z\"\n")
	if first != second {
		t.Fatalf("the same two tests failing is one failure, whatever the "+
			"values were:\n %q\n %q", first, second)
	}
}

// TestFixingOneTestIsProgress is the first control.
//
// Identifying a suite failure by its tests must not make a run that is actually
// converging look stuck. Three red tests becoming two is exactly the progress
// the tracker exists to leave alone.
func TestFixingOneTestIsProgress(t *testing.T) {
	three := failureFingerprint(
		"--- FAIL: TestOne (0.00s)\n--- FAIL: TestTwo (0.00s)\n" +
			"--- FAIL: TestThree (0.00s)\n")
	two := failureFingerprint(
		"--- FAIL: TestOne (0.00s)\n--- FAIL: TestTwo (0.00s)\n")
	if three == two {
		t.Fatal("clearing a failing test must read as progress")
	}
}

// TestASubtestIsItsParentsFailure keeps one red test from reading as several.
//
// Go prints the parent line and a line per failing subtest. Counting both would
// mean a run that split one test into a table of cases had "more" failing tests
// than before, which reads as a new failure every time the table changes.
func TestASubtestIsItsParentsFailure(t *testing.T) {
	parentOnly := failureFingerprint("--- FAIL: TestJustify (0.00s)\n")
	withSubtests := failureFingerprint(
		"--- FAIL: TestJustify (0.00s)\n" +
			"    --- FAIL: TestJustify/two_words (0.00s)\n" +
			"    --- FAIL: TestJustify/three_words (0.00s)\n")
	if parentOnly != withSubtests {
		t.Errorf("a subtest failing is its parent failing:\n %q\n %q",
			parentOnly, withSubtests)
	}
}

// TestANonSuiteFailureKeepsItsProseFingerprint is the second control.
//
// Only a suite run is identified by test names. Everything else — a compiler
// error, a review finding, a coverage shortfall — carries its identity in its
// prose, and must keep the fingerprint it has always had.
func TestANonSuiteFailureKeepsItsProseFingerprint(t *testing.T) {
	if got := suiteFingerprint("main.go:37: executeCommands nests 5 levels deep"); got != "" {
		t.Fatalf("a review finding is not a suite run, got %q", got)
	}
	nesting := failureFingerprint("main.go:37: executeCommands nests 5 levels deep")
	exits := failureFingerprint("main.go:37: main exits zero whatever happens")
	if nesting == exits {
		t.Error("two different findings must still be two failures")
	}
}
