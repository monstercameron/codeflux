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

// TestADuplicateIsEnoughToCountAsRepetition covers a synthesised case being
// matched by the property it names rather than by the literal it is written as.
//
// Ladder rung 2 wrote []string{"4", "4", "-2"} under the heading "repeated
// elements" and was refused for holding a third value that was not a
// duplicate, which failed one stage and blocked eleven behind it.
func TestADuplicateIsEnoughToCountAsRepetition(t *testing.T) {
	for _, elements := range [][]string{
		{"4", "4", "-2"},
		{"8", "-3", "8"},
		{"a", "a", "a"},
	} {
		if !elementsRepeat(elements) {
			t.Errorf("%v was not read as holding a repeat", elements)
		}
	}
	for _, elements := range [][]string{
		{"1", "2", "3"},
		{"7"},
		{},
	} {
		if elementsRepeat(elements) {
			t.Errorf("%v was read as holding a repeat", elements)
		}
	}
}

// TestEveryStringCaseAsksADifferentQuestion checks the eight string shapes
// classify into eight distinct properties.
//
// If two collapsed, a test covering one would silently satisfy the other and
// the case ladder would report coverage it does not have.
func TestEveryStringCaseAsksADifferentQuestion(t *testing.T) {
	seen := map[stringShapeKind]string{}
	for _, candidate := range casesForType("string") {
		kind, ok := stringShapeSignature(candidate.Shape)
		if !ok {
			t.Errorf("%s was not read as a string shape", candidate.Shape)
			continue
		}
		if earlier, clash := seen[kind]; clash {
			t.Errorf("%s and %s both ask for %s", earlier, candidate.Shape, kind)
		}
		seen[kind] = candidate.Shape
	}
	if len(seen) != len(casesForType("string")) {
		t.Errorf("%d string case(s) collapsed into %d propert(ies)",
			len(casesForType("string")), len(seen))
	}
}

// TestAStringCaseIsTriedByATestUsingItsOwnValues is the point of matching on
// the property: nothing about "héllo wörld" says those words.
func TestAStringCaseIsTriedByATestUsingItsOwnValues(t *testing.T) {
	source := `func TestParse(t *testing.T) {
		cases := []string{"", " ", "x", "  spaced  ", "one\ttwo\nthree",
			"caf\u00e9 au lait", strings.Repeat("q", 50000), "ordinary text"}
		_ = cases
	}`
	for _, candidate := range casesForType("string") {
		if !caseIsTried(source, []string{"TestParse"}, candidate) {
			t.Errorf("a test covering the property left %s untried", candidate.Shape)
		}
	}
}

// TestAFormatStringDoesNotCountAsAnInput guards the leniency the property
// match buys back: a suite is full of "%d\n", and counting those would satisfy
// the separator case in every suite ever written.
func TestAFormatStringDoesNotCountAsAnInput(t *testing.T) {
	source := `func TestParse(t *testing.T) {
		t.Errorf("got %v, want %v\n", got, want)
	}`
	if testSourceHasStringShape(source, stringShapeMixedSeparators) {
		t.Error("an assertion message counted as an input with mixed separators")
	}
	if testSourceHasStringShape(source, stringShapeOrdinary) {
		t.Error("an assertion message counted as ordinary input text")
	}
}

// TestAnOrdinaryPositiveValueIsAnyOrdinaryPositiveValue covers the third place
// the same defect appeared: nothing about 42 says forty-two.
//
// Ladder rung 3 is FizzBuzz. Its tests call the function with 1, 3, 5 and 15,
// which is every ordinary positive value that rung has any reason to use, and
// it spent five of its six attempts being asked for 42 by name.
func TestAnOrdinaryPositiveValueIsAnyOrdinaryPositiveValue(t *testing.T) {
	source := `func TestFizzBuzz(t *testing.T) {
		for _, n := range []int{1, 3, 5, 15} {
			printFizzBuzz(n)
		}
		printFizzBuzz(0)
		printFizzBuzz(-7)
	}`
	for _, shape := range []string{"42", "0", "1", "-1"} {
		candidate := atomCase{Shape: shape, Class: caseStraightforward}
		if !caseIsTried(source, []string{"TestFizzBuzz"}, candidate) {
			t.Errorf("a suite covering the property left %s untried", shape)
		}
	}
}

// TestDigitsInsideAnIdentifierAreNotAValueTried guards the leniency: finding an
// ordinary positive value in int8 or buffer256 would satisfy the case in a
// suite that never passes one.
func TestDigitsInsideAnIdentifierAreNotAValueTried(t *testing.T) {
	source := `var x int8; var buffer256 [256]byte; _ = x`
	if testSourceHasIntegerShape(`type int8 buffer256 sha1`, integerShapeOrdinaryPositiv) {
		t.Error("digits inside an identifier counted as a value the suite tried")
	}
	if !testSourceHasIntegerShape(source, integerShapeOrdinaryPositiv) {
		t.Error("the array length 256 is a real literal and was not counted")
	}
}

// TestAnOutputLinearFunctionIsNotAskedForMaxInt covers the cost guard.
//
// Ladder rung 3 is FizzBuzz: its whole body is a loop bounded by its argument,
// and it was asked for math.MaxInt and math.MinInt. That test cannot finish.
func TestAnOutputLinearFunctionIsNotAskedForMaxInt(t *testing.T) {
	fizzbuzz := producedFunction{
		Name: "printFizzBuzz", Parameters: []string{"int"}, LoopDepth: 1,
	}
	for _, candidate := range withoutExplosiveCases(
		fizzbuzz, casesForType("int"),
	) {
		if strings.Contains(candidate.Shape, "math.MaxInt") ||
			strings.Contains(candidate.Shape, "math.MinInt") {
			t.Errorf("a loop bounded by its argument was asked for %s",
				candidate.Shape)
		}
	}
}

// TestTheBoundaryStillGetsACase guards against the guard becoming a deletion:
// well past what the author pictured is still worth asking, bounded.
func TestTheBoundaryStillGetsACase(t *testing.T) {
	fizzbuzz := producedFunction{
		Name: "printFizzBuzz", Parameters: []string{"int"}, LoopDepth: 1,
	}
	pathological := 0
	for _, candidate := range withoutExplosiveCases(
		fizzbuzz, casesForType("int"),
	) {
		if candidate.Class == casePathological {
			pathological++
		}
	}
	if pathological == 0 {
		t.Error("dropping the unrunnable cases left the boundary unexamined")
	}
}

// TestAnAdderKeepsItsOverflowCase keeps the guard narrow: math.MaxInt is a fair
// question for a function that adds.
func TestAnAdderKeepsItsOverflowCase(t *testing.T) {
	adder := producedFunction{
		Name: "add", Parameters: []string{"int", "int"},
		Results: []string{"int"}, Pure: true,
	}
	found := false
	for _, candidate := range withoutExplosiveCases(adder, casesForType("int")) {
		if strings.Contains(candidate.Shape, "math.MaxInt") {
			found = true
		}
	}
	if !found {
		t.Error("a function with no loop lost its overflow case")
	}
}

// TestAReaderCaseIsTriedByAReaderOverTheSameKindOfInput covers the fourth place
// the invented-literal defect appeared.
//
// strings.NewReader("!!! not the expected shape") asks a run to guess three
// exclamation marks and a particular sentence. Ladder rung 5 was refused eight
// cases on readEntries for this, which failed stage 7 and blocked thirteen hard
// stages behind it.
func TestAReaderCaseIsTriedByAReaderOverTheSameKindOfInput(t *testing.T) {
	source := `func TestReadEntries(t *testing.T) {
		for _, input := range []string{"", "{\"a\":1}", "not json at all", "  padded  ",
			"one\ttwo\nthree", "caf\u00e9", strings.Repeat("q", 50000), "x"} {
			_, _ = readEntries(strings.NewReader(input))
		}
	}`
	for _, candidate := range casesForType("io.Reader") {
		if !caseIsTried(source, []string{"TestReadEntries"}, candidate) {
			t.Errorf("a suite reading every kind of input left %q untried",
				candidate.Shape)
		}
	}
}

// TestAReaderCaseNeedsASuiteThatActuallyReads keeps the leniency bounded: the
// property match runs over every literal in the file, so requiring that a
// reader is built somewhere stops it satisfying a reader case in a suite that
// never reads anything.
func TestAReaderCaseNeedsASuiteThatActuallyReads(t *testing.T) {
	source := `func TestSomethingElse(t *testing.T) {
		got := describe("")
		if got != "" { t.Fail() }
	}`
	empty := atomCase{Shape: `strings.NewReader("")`, Class: caseDegenerate}
	if caseIsTried(source, []string{"TestSomethingElse"}, empty) {
		t.Error("a suite that never builds a reader satisfied a reader case")
	}
}
