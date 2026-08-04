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

// TestPropertyTestsSkipWhenTheOnlyParameterIsAnOutputSink is the same
// exemption, for the shape ladder rung 1 actually produced.
//
// The first fixture above is the easy case: a function with no parameters at
// all. Rung 1 did not write that. It wrote writeGreeting(w io.Writer) error —
// still a program that prints one constant line, but with the destination
// injected so the failure path could be tested, which is good practice and
// which the run arrived at on its own before any gate spoke.
//
// That signature has a parameter and returns an error, so case synthesis has
// things to say about it and the previous implementation, which read case
// synthesis' answer, applied the gate. There is still no set of values over
// which any property holds: varying the writer varies where the output goes.
//
// Proven to discriminate: against the previous implementation this fixture
// recorded broke with "all 2 test(s) check a single example, so the suite
// passes for exactly the inputs somebody thought of". Ladder rung 1 recorded
// that on 2026-08-03 and spent an attempt on thirteen consecutive patches to
// one test file, never running the suite, trying to comply.
func TestPropertyTestsSkipWhenTheOnlyParameterIsAnOutputSink(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/property\n\ngo 1.26.0\n",
		"greet.go": "package property\n\nimport (\n\t\"fmt\"\n\t\"io\"\n)\n\n" +
			"// WriteGreeting writes the one line it was asked for to w.\n" +
			"func WriteGreeting(w io.Writer) error {\n" +
			"\t_, err := fmt.Fprintln(w, \"Hello from CodeFlux\")\n" +
			"\treturn err\n}\n",
		"greet_test.go": "package property\n\n" +
			"import (\n\t\"bytes\"\n\t\"testing\"\n)\n\n" +
			"func TestWriteGreeting(t *testing.T) {\n" +
			"\tif err := WriteGreeting(&bytes.Buffer{}); err != nil {\n" +
			"\t\tt.Fatal(err)\n\t}\n}\n",
	})

	outcome := checkPropertyTests(worktree)
	if !outcome.Skipped {
		t.Fatalf("an injected output sink is a destination, not an input space, "+
			"so this must record skipped rather than held=%t: %s",
			outcome.Held, outcome.Detail)
	}
}

// TestPropertyTestsStillApplyToAnInjectedReader is the second control.
//
// The rule is about which way the data flows, not about interfaces being
// exempt. A reader hands the function bytes, and the bytes it hands over are
// an input space with an empty case, a short case and a huge case in it — so a
// run given one still owes a property over them.
func TestPropertyTestsStillApplyToAnInjectedReader(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/property\n\ngo 1.26.0\n",
		"count.go": "package property\n\n" +
			"import (\n\t\"bufio\"\n\t\"io\"\n)\n\n" +
			"// CountLines counts the lines it can read from r.\n" +
			"func CountLines(r io.Reader) int {\n" +
			"\tscanner := bufio.NewScanner(r)\n\tlines := 0\n" +
			"\tfor scanner.Scan() {\n\t\tlines++\n\t}\n\treturn lines\n}\n",
		"count_test.go": "package property\n\n" +
			"import (\n\t\"strings\"\n\t\"testing\"\n)\n\n" +
			"func TestCountLines(t *testing.T) {\n" +
			"\tif CountLines(strings.NewReader(\"a\\nb\\n\")) != 2 {\n" +
			"\t\tt.Fatal(\"wrong\")\n\t}\n}\n",
	})

	outcome := checkPropertyTests(worktree)
	if outcome.Skipped {
		t.Fatalf("the bytes a reader yields are an input space, so a single "+
			"example is still the defect this gate exists to name: %s",
			outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "single example") {
		t.Errorf("the reason should still name the single-example suite, "+
			"got %q", outcome.Detail)
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
