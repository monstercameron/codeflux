package coordinator

import (
	"strings"
	"testing"
)

// TestPropertyTestsSkipWhenThereIsNoInputSpaceToVary is the discrimination the
// atom-property-tests gate needed.
//
// The gate reads a suite made entirely of single examples as a defect, which
// it is when there are inputs somebody could have varied and did not. It is
// not one when the produced unit takes no inputs at all: there is no property
// over an empty input space to state, and no table for a test to be driven
// from, so the demand cannot be met by any test anybody could write.
//
// Proven to discriminate: against the previous implementation this fixture
// recorded failed with "all 1 test(s) check a single example, so the suite
// passes for exactly the inputs somebody thought of", which is what ladder
// rung 1 recorded on 2026-08-03 with correct produced code — and it blocked
// fourteen downstream stages behind it, so the run could not reach
// atom-registration and nothing was ever registered.
//
// The judgement is deliberately synthesiseCases', shared with
// atom-case-synthesis, rather than a second rule written here. Those two
// stages had already drifted into contradicting each other about this exact
// fixture: case-synthesis skipped saying nothing could be varied, while this
// stage failed for not having varied it.
func TestPropertyTestsSkipWhenThereIsNoInputSpaceToVary(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/property\n\ngo 1.26.0\n",
		"greet.go": "package property\n\nimport \"fmt\"\n\n" +
			"// Greet prints the one line it was asked for.\n" +
			"func Greet() {\n\tfmt.Println(\"Hello from CodeFlux\")\n}\n",
		"greet_test.go": "package property\n\nimport \"testing\"\n\n" +
			"func TestGreet(t *testing.T) {\n\tGreet()\n}\n",
	})

	outcome := checkPropertyTests(worktree)
	if !outcome.Skipped {
		t.Fatalf("a unit with no inputs has no property over an input space "+
			"to state, so this must record skipped rather than held=%t: %s",
			outcome.Held, outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "no atom whose inputs could be varied") {
		t.Errorf("the reason should say what atom-case-synthesis says for the "+
			"same fact, got %q", outcome.Detail)
	}
}

// TestPropertyTestsStillFailWhenInputsCouldHaveBeenVaried is the control, and
// it is what keeps the change from being a hole rather than a correction.
//
// The skip turns on there being no input space at all. A unit that does take
// inputs, tested only by a single example, is the defect this gate exists to
// catch, and it must still be caught.
func TestPropertyTestsStillFailWhenInputsCouldHaveBeenVaried(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/property\n\ngo 1.26.0\n",
		"total.go": "package property\n\n" +
			"// Total adds every amount it is given.\n" +
			"func Total(amounts []int) int {\n\tsum := 0\n" +
			"\tfor _, amount := range amounts {\n\t\tsum += amount\n\t}\n" +
			"\treturn sum\n}\n",
		"total_test.go": "package property\n\nimport \"testing\"\n\n" +
			"func TestTotal(t *testing.T) {\n" +
			"\tif Total([]int{1, 2}) != 3 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n",
	})

	outcome := checkPropertyTests(worktree)
	if outcome.Skipped {
		t.Fatalf("a slice argument is an input space with an empty case and a "+
			"many-element case in it, so a single example is still the defect "+
			"this gate exists to name: %s", outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "single example") {
		t.Errorf("the reason should still name the single-example suite, "+
			"got %q", outcome.Detail)
	}
}
