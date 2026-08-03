package coordinator

import "testing"

// TestSynthesisedCaseIsTriedByStructureNotByItsInventedValues is the
// discrimination the atom-case-synthesis gate needed.
//
// The synthesiser writes a case's shape as code so a reader sees what is being
// asked for, and it fills that code with values it invents: `[]string{"a",
// "b", "c"}` says "three distinct strings", not "the letters a, b and c".
// Matching the test source against that exact text asked a test to contain
// literals this file chose at random, which no correct test does.
//
// Proven to discriminate: against the previous implementation every case here
// reported untried, which is what ladder rung 2 recorded — "10 of 18
// synthesised case(s) are never tried" — for a run whose produced program was
// correct, built, ran, and survived the adversarial probe. Thirteen stages
// were blocked behind it.
func TestSynthesisedCaseIsTriedByStructureNotByItsInventedValues(t *testing.T) {
	// A table-driven test over the function's real inputs, which is what a
	// thorough suite looks like and what the old check could not recognise.
	testSource := `
		func TestSumArguments(t *testing.T) {
			for _, testCase := range []struct{
				name string
				in   []string
				want int
			}{
				{"several", []string{"7", "11", "24"}, 42},
				{"empty", []string{}, 0},
				{"single", []string{"5"}, 5},
				{"repeated", []string{"3", "3", "3"}, 9},
			} {
				_ = testCase
			}
		}`
	tests := []string{"TestSumArguments"}

	for _, testCase := range []struct {
		name  string
		shape string
		tried bool
		why   string
	}{
		{"several distinct", `[]string{"a", "b", "c"}`, true,
			"the suite passes three distinct strings"},
		{"empty", `[]string{}`, true, "the suite passes an empty slice"},
		{"single", `[]string{"a"}`, true, "the suite passes a one-element slice"},
		{"repeated", `[]string{"a", "a", "a"}`, true,
			"the suite passes three copies of one value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := atomCase{Shape: testCase.shape, Class: caseEdge}
			if got := caseIsTried(testSource, tests, candidate); got != testCase.tried {
				t.Errorf("case %s: tried = %t, want %t — %s",
					testCase.shape, got, testCase.tried, testCase.why)
			}
		})
	}
}

// TestSynthesisedCaseStillUntriedWhenTheStructureIsAbsent is the control, and
// it is what stops the fix from being a hole.
//
// Structure is checked, not ignored. A suite that only ever passes several
// distinct values has genuinely not tried the empty input, the single-element
// input, or the duplicate — those are the cases this gate exists to demand,
// and it must still demand them.
func TestSynthesisedCaseStillUntriedWhenTheStructureIsAbsent(t *testing.T) {
	testSource := `
		func TestSumArguments(t *testing.T) {
			if sumArguments([]string{"7", "11", "24"}) != 42 {
				t.Fatal("wrong")
			}
		}`
	tests := []string{"TestSumArguments"}

	for _, testCase := range []struct {
		name  string
		shape string
	}{
		{"empty is not tried", `[]string{}`},
		{"single is not tried", `[]string{"a"}`},
		{"duplicate is not tried", `[]string{"a", "a", "a"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := atomCase{Shape: testCase.shape, Class: caseEdge}
			if caseIsTried(testSource, tests, candidate) {
				t.Errorf("a suite that only passes three distinct values has "+
					"not tried %s, and reporting otherwise would make this "+
					"gate agree with anything", testCase.shape)
			}
		})
	}
}

// TestScalarCaseShapesAreStillMatchedLiterally pins the half that must not
// change.
//
// A scalar shape has no invented part: nil means nil, "" means the empty
// string, 0 means zero. Those are matched as text exactly as before, and the
// structural path must not swallow them — nil in particular was already once
// the single most commonly forgotten input this check failed to report.
func TestScalarCaseShapesAreStillMatchedLiterally(t *testing.T) {
	tests := []string{"TestParse"}
	present := `func TestParse(t *testing.T) { parse(nil); parse("") }`

	for _, shape := range []string{"nil", `""`} {
		candidate := atomCase{Shape: shape, Class: caseEdge}
		if !caseIsTried(present, tests, candidate) {
			t.Errorf("%s appears in the test source and must count as tried", shape)
		}
		if caseIsTried(`func TestParse(t *testing.T) { parse("x") }`, tests, candidate) {
			t.Errorf("%s does not appear and must not count as tried", shape)
		}
	}
}

// TestAnEmptySliceCaseIsSatisfiedByNil is the equivalence the gate was missing.
//
// A function that ranges over a slice cannot tell []string{} from a nil
// []string: both have length zero and both yield nothing. The synthesiser
// emits both forms because both are worth naming in the instruction, but
// counting them as two demands told a run that had already covered the empty
// input to write the same test again.
//
// Observed on ladder rung 2: the model wrote `{name: "empty", args: nil}`,
// which is the empty case, and the gate reported []string{} as never tried.
func TestAnEmptySliceCaseIsSatisfiedByNil(t *testing.T) {
	tests := []string{"TestParseIntegers"}
	withNil := `
		func TestParseIntegers(t *testing.T) {
			for _, tt := range []struct{ args []string }{
				{args: nil},
				{args: []string{"12", "-3", "0"}},
			} {
				_ = tt
			}
		}`
	candidate := atomCase{Shape: `[]string{}`, Class: caseEdge}
	if !caseIsTried(withNil, tests, candidate) {
		t.Error("a suite that passes nil has tried the empty input")
	}

	// The equivalence is for the empty case only. A nil somewhere in the
	// suite must not excuse the single-element or duplicate cases, or the
	// gate would agree with almost anything.
	for _, shape := range []string{`[]string{"a"}`, `[]string{"a", "a", "a"}`} {
		if caseIsTried(withNil, tests, atomCase{Shape: shape, Class: caseEdge}) {
			t.Errorf("nil says nothing about %s, which is still untried", shape)
		}
	}
}

// TestNilInsideAnIdentifierIsNotTheKeyword pins the scan.
func TestNilInsideAnIdentifierIsNotTheKeyword(t *testing.T) {
	tests := []string{"TestParse"}
	source := `func TestParse(t *testing.T) { nilable := 1; _ = nilable }`
	candidate := atomCase{Shape: `[]string{}`, Class: caseEdge}
	if caseIsTried(source, tests, candidate) {
		t.Error("nil inside nilable is not the keyword and must not count")
	}
}
