package coordinator

import (
	"strings"
	"testing"
)

// TestTheCaseLadderComesFromTheSignatureNotTheCode is the whole reason this
// stage exists.
//
// A test written by reading an implementation checks what the code does. The
// ladder is derived from the declared types, so it asks for inputs the author
// may never have considered — which is where the difference between "what it
// does" and "what it promised" lives.
func TestTheCaseLadderComesFromTheSignatureNotTheCode(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"// Total adds every value it is given.\n" +
			"func Total(values []int) int {\n\ttotal := 0\n" +
			"\tfor _, value := range values {\n\t\ttotal += value\n\t}\n" +
			"\treturn total\n}\n\nfunc main() {}\n",
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	corpus := synthesiseCases(functions)
	cases, present := corpus["Total"]
	if !present {
		t.Fatal("no cases were derived for a function taking a slice")
	}
	shapes := map[string]bool{}
	for _, candidate := range cases {
		shapes[candidate.Shape] = true
		if strings.TrimSpace(candidate.Why) == "" {
			t.Errorf("case %q says nothing about why it is worth trying",
				candidate.Shape)
		}
	}
	// The three a caller actually passes and an author rarely tests.
	for _, wanted := range []string{"nil", "[]int{}", "[]int{0}"} {
		if !shapes[wanted] {
			t.Errorf("the ladder never asks for %s", wanted)
		}
	}
	// Simplest class first, so a run that only gets partway through has done
	// the cases most likely to fail loudly.
	for index := 1; index < len(cases); index++ {
		if cases[index].Class.rank() < cases[index-1].Class.rank() {
			t.Errorf("a %s case is ordered before a %s one",
				cases[index].Class, cases[index-1].Class)
		}
	}

	// The whole gamut, not just the comfortable end of it. A ladder missing
	// its wrong and pathological rungs tests only what the author imagined.
	classes := map[caseClass]bool{}
	for _, candidate := range cases {
		classes[candidate.Class] = true
	}
	for _, wanted := range []caseClass{
		caseStraightforward, caseDegenerate, caseEdge, caseComplex,
		casePathological,
	} {
		if !classes[wanted] {
			t.Errorf("the ladder for a slice has no %s case", wanted)
		}
	}
}

// TestAFunctionThatCanFailOwesACaseThatMakesItFail closes the path that is
// written and never walked.
func TestAFunctionThatCanFailOwesACaseThatMakesItFail(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"// Parse reads a number.\n" +
			"func Parse(text string) (int, error) {\n\treturn 0, nil\n}\n\n" +
			"func main() {}\n",
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	cases := synthesiseCases(functions)["Parse"]
	failing := false
	for _, candidate := range cases {
		if strings.Contains(candidate.Why, "failure path") {
			failing = true
		}
	}
	if !failing {
		t.Error("a function returning an error was given no case that makes " +
			"it fail, so its failure path is written and never walked")
	}
}

// TestUntriedCasesAreReportedAgainstTheFunctionThatOwesThem keeps the finding
// actionable.
func TestUntriedCasesAreReportedAgainstTheFunctionThatOwesThem(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"// Total adds every value it is given.\n" +
			"func Total(values []int) int {\n\treturn len(values)\n}\n\n" +
			"func main() {}\n",
		// A test that only ever passes the comfortable input.
		"cmd/thing/main_test.go": "package main\n\nimport \"testing\"\n\n" +
			"func TestTotal(t *testing.T) {\n" +
			"\tif Total([]int{1, 2, 3}) != 3 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
	})
	outcome := checkCaseCoverage(worktree)
	if outcome.Held {
		t.Error("a test that only tries one comfortable input was accepted " +
			"as having tried the ladder")
	}
	if !strings.Contains(outcome.Detail, "Total") {
		t.Errorf("the finding does not name the function that owes the cases: %q",
			outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "nil") {
		t.Errorf("the finding does not name the untried nil case: %q",
			outcome.Detail)
	}
}

// TestEachClassDemandsItsOwnAssertion is why the class is carried at all.
//
// An agent handed a flat list of inputs writes the same assertion for all of
// them, which is wrong for most: a wrong input needs a refusal asserted, a
// pathological one needs survival asserted, and only the straightforward ones
// have an exact expected value.
func TestEachClassDemandsItsOwnAssertion(t *testing.T) {
	for _, check := range []struct {
		class caseClass
		wants string
	}{
		{caseWrong, "refused"},
		{casePathological, "does not panic"},
		{caseDegenerate, "empty-work answer"},
		{caseStraightforward, "exact expected result"},
	} {
		if !strings.Contains(check.class.assertion(), check.wants) {
			t.Errorf("a %s case is not told to assert %q: %q",
				check.class, check.wants, check.class.assertion())
		}
	}
	// Refusal and correctness must not be asked for in the same words, or the
	// distinction the class exists to draw is lost.
	if caseWrong.assertion() == caseStraightforward.assertion() {
		t.Error("a wrong input and a correct one are asked for the same assertion")
	}
}

// TestIntegersCarryAWrongAndAPathologicalCase covers the two classes most
// often skipped.
func TestIntegersCarryAWrongAndAPathologicalCase(t *testing.T) {
	present := map[caseClass][]string{}
	for _, candidate := range casesForType("int") {
		present[candidate.Class] = append(present[candidate.Class], candidate.Shape)
	}
	if len(present[caseWrong]) == 0 {
		t.Error("an integer parameter is never given a value that is wrong")
	}
	if len(present[casePathological]) == 0 {
		t.Error("an integer parameter is never taken to its representable limit")
	}
	overflow := strings.Join(present[casePathological], " ")
	if !strings.Contains(overflow, "MaxInt") || !strings.Contains(overflow, "MinInt") {
		t.Errorf("neither end of the integer range is tried: %s", overflow)
	}
}
