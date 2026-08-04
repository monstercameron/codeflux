package coordinator

import (
	"strings"
	"testing"
)

// TestAnInstructionThatAsksForCodeShowsIt is the sweep this session's evidence
// justifies.
//
// Two instructions were fixed reactively, each after a rung failed on it. The
// property gate is the measured one: described in prose it produced nothing —
// rung 18 read it twice and wrote twenty-two tests without a loop in any of
// them — and with five lines of Go under it the same model on the same rung
// went to thirteen of thirty-one tests examining a set.
//
// The fuzz instruction had the same shape and the same symptom. atom-fuzz was a
// sendback on rungs 9, 12, 13, 14, 16, 18 and 19, and a Go fuzz target is
// fiddly in a way prose hides: every parameter of the f.Fuzz closure after
// *testing.T has to match what f.Add supplies, in order and in type, and a
// mismatch is a compile error rather than a weak test. Applied here before a
// rung demanded it rather than after.
func TestAnInstructionThatAsksForCodeShowsIt(t *testing.T) {
	for _, subject := range []struct {
		name        string
		instruction string
		shows       []string
	}{
		{
			name: "property test",
			instruction: propertyTestInstruction(
				"all 8 test(s) check one example", true),
			shows: []string{"for _, in := range", "t.Errorf"},
		},
		{
			name: "fuzz target",
			instruction: fuzzTargetInstruction(
				[]string{"parse/parse.go:Parse"}, true),
			shows: []string{"func FuzzParse(f *testing.F)", "f.Add(", "f.Fuzz("},
		},
		{
			name:        "unreachable marker",
			instruction: uncoveredLineInstruction([]string{"main.go:12"}),
			shows:       []string{"//codeflux:unreachable"},
		},
		{
			name: "atom documentation",
			instruction: atomDocumentationInstruction(
				[]string{"fp/result.go:Ok"}),
			shows: []string{"//codeflux:atom", "//   Inputs:"},
		},
	} {
		for _, want := range subject.shows {
			if !strings.Contains(subject.instruction, want) {
				t.Errorf("the %s instruction never shows %q, so a run has to "+
					"infer the shape from a description of it",
					subject.name, want)
			}
		}
	}
}

// TestTheFuzzExampleNamesTheMistakeItPrevents is what makes the example worth
// its tokens.
//
// An example that only shows a correct answer teaches the shape and not the
// trap. The trap here is specific and costly: the closure's parameters must
// match f.Add's, and a returned error from a decoder is a pass rather than a
// finding — a run that treats a refusal as a failure will report defects that
// are not there.
func TestTheFuzzExampleNamesTheMistakeItPrevents(t *testing.T) {
	instruction := fuzzTargetInstruction([]string{"parse/parse.go:Parse"}, true)

	if !strings.Contains(instruction, "must match what f.Add supplies") {
		t.Errorf("the arity trap is not named:\n%s", instruction)
	}
	if !strings.Contains(instruction, "refusing bad input is correct") {
		t.Errorf("the example does not say a refusal is a pass, which is the "+
			"other way this target gets written wrong:\n%s", instruction)
	}
}

// TestAnInstructionSaysWhetherTheTestsPassed keeps every ask honest about the
// state it was raised in.
//
// Three builders once asserted the suite had passed whether or not anything
// established it. The ones that can be raised while the build is broken now
// take testsPassed and say which it is; the two that cannot — the coverage and
// documentation asks — state no such thing rather than guessing.
func TestAnInstructionSaysWhetherTheTestsPassed(t *testing.T) {
	for name, instruction := range map[string]string{
		"property": propertyTestInstruction("d", false),
		"fuzz":     fuzzTargetInstruction([]string{"a:B"}, false),
		"mutation": survivingDefectInstruction([]string{"a:B"}, false),
		"hostile":  hostileInputInstruction([]string{"nothing at all"}, false),
	} {
		if !strings.Contains(instruction, "have not been shown to pass") {
			t.Errorf("the %s instruction does not say the suite is unproven, "+
				"so a run reads it as a refinement on working code", name)
		}
	}
	// And the two that assert nothing must not start asserting.
	for name, instruction := range map[string]string{
		"coverage":      uncoveredLineInstruction([]string{"main.go:12"}),
		"documentation": atomDocumentationInstruction([]string{"a.go:B"}),
	} {
		if strings.Contains(instruction, "tests pass") {
			t.Errorf("the %s instruction claims the tests pass without being "+
				"told whether they did", name)
		}
	}
}
