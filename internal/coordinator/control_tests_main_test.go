package coordinator

import (
	"strings"
	"testing"
)

// TestMainIsNotDemandedOfAUnitTest is the contradiction this gate carried.
//
// The flow hands the model a style directive saying "push every effect to the
// edge — input, output, and failure reporting belong in main or in a thin
// shell around the core". A run that followed that exactly was then failed by
// this gate for it: main takes decisions, and no Go unit test can call main.
//
// Observed on ladder rung 5 on 2026-08-03, where it was the single remaining
// failing stage of forty-one. end-to-end-tests already runs the built
// executable against every acceptance example, which is the only way main's
// paths can be reached at all.
func TestMainIsNotDemandedOfAUnitTest(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/control\n\ngo 1.26.0\n",
		"main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" +
			"// Total adds what it is given.\n" +
			"func Total(values []int) int {\n\tsum := 0\n" +
			"\tfor _, value := range values {\n\t\tif value > 0 {\n" +
			"\t\t\tsum += value\n\t\t}\n\t}\n\treturn sum\n}\n\n" +
			"// main reports the total and any failure.\n" +
			"func main() {\n\tif len(os.Args) < 2 {\n" +
			"\t\tfmt.Fprintln(os.Stderr, \"need arguments\")\n\t\treturn\n\t}\n" +
			"\tfmt.Println(Total(nil))\n}\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\n" +
			"func TestTotal(t *testing.T) {\n" +
			"\tif Total([]int{1, -2, 3}) != 4 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n",
	})

	outcome := checkControlTests(worktree)
	if !outcome.Held {
		t.Fatalf("main cannot be reached by a unit test and the flow's own "+
			"style directive puts effects there, so this must not fail: %s",
			outcome.Detail)
	}
	if strings.Contains(outcome.Detail, "main") {
		t.Errorf("main should not be counted among branching functions "+
			"needing a test, got %q", outcome.Detail)
	}
}

// TestABranchingFunctionOtherThanMainIsStillDemanded is the control.
//
// The exemption is for main alone. An ordinary function with a decision that
// no test reaches is exactly what this gate exists to catch, and it must still
// catch it.
func TestABranchingFunctionOtherThanMainIsStillDemanded(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/control\n\ngo 1.26.0\n",
		"main.go": "package main\n\n" +
			"// Classify decides what a value is.\n" +
			"func Classify(value int) string {\n\tif value > 0 {\n" +
			"\t\treturn \"positive\"\n\t}\n\treturn \"other\"\n}\n\n" +
			"// main does nothing interesting.\n" +
			"func main() {}\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\n" +
			"func TestNothingUseful(t *testing.T) {\n\t_ = 1\n}\n",
	})

	outcome := checkControlTests(worktree)
	if outcome.Held {
		t.Fatal("Classify branches and no test reaches it, which is the " +
			"defect this gate exists to name")
	}
	if !strings.Contains(outcome.Detail, "Classify") {
		t.Errorf("the failure should name the untested branching function, "+
			"got %q", outcome.Detail)
	}
}
