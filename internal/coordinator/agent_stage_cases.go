package coordinator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// caseClass is what kind of question one input asks of an atom.
//
// The class decides what a test of it must assert, which is why it is carried
// rather than left implicit. A straightforward input has a right answer; a
// wrong one must be refused; a pathological one only has to not take the
// program down. A suite that asserts the same thing about all three has not
// understood any of them.
type caseClass string

const (
	// caseStraightforward is the input the request was written about. If this
	// fails nothing else matters.
	caseStraightforward caseClass = "straightforward"
	// caseEdge sits at a boundary: empty, one, first, last. Most off-by-one
	// errors are found here and nowhere else.
	caseEdge caseClass = "edge"
	// caseComplex is realistic and awkward at once: many elements, repeats,
	// ties, mixed shapes. It is where ordering and grouping decisions surface.
	caseComplex caseClass = "complex"
	// caseDegenerate is the shape of having nothing to do. A nil collection is
	// not an empty one and neither is an error.
	caseDegenerate caseClass = "degenerate"
	// caseWrong is input the atom must refuse. A test of it asserts refusal,
	// not a value, and a run that quietly returns something for it has turned
	// a caller's mistake into a wrong answer.
	caseWrong caseClass = "wrong"
	// casePathological is built to break an assumption rather than to be
	// realistic: overflow, very large input, text that is not what the author
	// pictured. A test of it asserts only that the program survives.
	casePathological caseClass = "pathological"
)

// assertion says what a test of this class of input has to check.
func (class caseClass) assertion() string {
	switch class {
	case caseWrong:
		return "assert it is refused — an error, or a documented sentinel — " +
			"never a plausible-looking value"
	case casePathological:
		return "assert it does not panic, hang, or silently truncate; a " +
			"refusal is an acceptable answer"
	case caseDegenerate:
		return "assert the empty-work answer explicitly, which is rarely the " +
			"same as the answer for one item"
	default:
		return "assert the exact expected result"
	}
}

// rank orders the ladder so a run that gets partway through has done the cases
// most likely to fail loudly.
func (class caseClass) rank() int {
	switch class {
	case caseStraightforward:
		return 1
	case caseDegenerate:
		return 2
	case caseEdge:
		return 3
	case caseComplex:
		return 4
	case caseWrong:
		return 5
	case casePathological:
		return 6
	default:
		return 7
	}
}

// atomCase is one input worth trying against an atom, and why.
type atomCase struct {
	// Shape is what the input looks like, written the way it would appear in a
	// test, so a reader sees what is being asked for rather than a description.
	Shape string
	// Why says what this case is probing. A case nobody can justify is a case
	// that gets deleted the first time it is inconvenient.
	Why   string
	Class caseClass
}

// casesForType is the full ladder one parameter type deserves.
//
// The cases are derived from the type rather than from the implementation,
// which is the whole point: a test written by reading the code checks what the
// code does, and a case derived from the signature checks what the signature
// promised. They differ exactly where the bug is.
func casesForType(typeName string) []atomCase {
	switch {
	case strings.HasPrefix(typeName, "[]"):
		element := strings.TrimPrefix(typeName, "[]")
		zero := zeroOf(element)
		return []atomCase{
			{fmt.Sprintf("%s{%s, %s, %s}", typeName, sampleOf(element, 1),
				sampleOf(element, 2), sampleOf(element, 3)),
				"an ordinary collection of a few distinct items",
				caseStraightforward},
			{"nil", "a nil slice, which is not the same as an empty one",
				caseDegenerate},
			{fmt.Sprintf("%s{}", typeName), "an empty slice: no work to do",
				caseEdge},
			{fmt.Sprintf("%s{%s}", typeName, sampleOf(element, 1)),
				"exactly one element, where off-by-one errors live", caseEdge},
			{fmt.Sprintf("%s{%s, %s, %s}", typeName, sampleOf(element, 1),
				sampleOf(element, 1), sampleOf(element, 1)),
				"the same element repeated, so ties must be broken somehow",
				caseComplex},
			{fmt.Sprintf("%s{%s, %s} reversed and re-sorted", typeName,
				sampleOf(element, 3), sampleOf(element, 1)),
				"input already in the wrong order, so ordering is not assumed",
				caseComplex},
			{fmt.Sprintf("a %s of ten thousand items", typeName),
				"far more than the author pictured", casePathological},
			{fmt.Sprintf("%s{%s}", typeName, zero),
				"a zero-valued element, which callers pass when they forget",
				caseEdge},
		}
	case strings.HasPrefix(typeName, "map["):
		return []atomCase{
			{fmt.Sprintf("%s with three entries", typeName),
				"an ordinary map", caseStraightforward},
			{"nil", "a nil map, which reads but cannot be written",
				caseDegenerate},
			{fmt.Sprintf("%s{}", typeName), "an empty map: no entries",
				caseEdge},
			{fmt.Sprintf("%s with one entry", typeName),
				"a single entry, where iteration order cannot hide a bug",
				caseEdge},
			{fmt.Sprintf("%s with entries whose values tie", typeName),
				"ties, so the ordering rule is forced to be stated",
				caseComplex},
		}
	case typeName == "string":
		return []atomCase{
			{`"hello world"`, "ordinary text", caseStraightforward},
			{`""`, "the empty string", caseDegenerate},
			{`"a"`, "a single character", caseEdge},
			{`" "`, "a single space, which is text and is also nothing",
				caseEdge},
			{`"  padded  "`, "leading and trailing whitespace", caseEdge},
			{`"a  b\tc\nd"`, "mixed separators: spaces, tabs, newlines",
				caseComplex},
			{`"héllo wörld"`, "characters outside ASCII, where byte and rune " +
				"lengths part company", casePathological},
			{`strings.Repeat("x", 100000)`, "text far longer than expected",
				casePathological},
		}
	case typeName == "int" || typeName == "int64" || typeName == "int32":
		return []atomCase{
			{"42", "an ordinary positive value", caseStraightforward},
			{"0", "zero, which is neither positive nor a count", caseDegenerate},
			{"1", "the smallest useful value", caseEdge},
			{"-1", "a negative value, which many functions accept and some must " +
				"refuse — whichever this is, a test should say so", caseEdge},
			// There is no literal that is wrong for every integer: the type
			// admits every value it can hold, so what counts as wrong comes
			// from the contract rather than the type. Moving -1 to the edge
			// class was right — a negative is legal for a general integer —
			// but it left integers with no wrong case at all, which is the one
			// outcome this class exists to prevent. So the case names the
			// question rather than a value, and is answered either by refusing
			// an out-of-domain input or by saying in a test that the domain is
			// every integer.
			{"a value this function's contract excludes, such as a negative " +
				"count, a zero divisor, or an index past the end",
				"an input the contract forbids; if the contract admits every " +
					"integer, a test should say so rather than leave the " +
					"class unexamined", caseWrong},
			{"math.MaxInt", "the largest representable value, where addition " +
				"overflows", casePathological},
			{"math.MinInt", "the smallest representable value, where negation " +
				"overflows", casePathological},
		}
	case typeName == "bool":
		return []atomCase{
			{"true", "the positive case", caseStraightforward},
			{"false", "the negative case", caseEdge},
		}
	case typeName == "io.Reader":
		return []atomCase{
			{"a reader of well-formed input", "the input the request describes",
				caseStraightforward},
			{`strings.NewReader("")`, "no input at all", caseDegenerate},
			{"a reader of one line with no trailing newline",
				"a final line the scanner must still see", caseEdge},
			{"a reader of many lines", "input larger than a single read",
				caseComplex},
			{`strings.NewReader("!!! not the expected shape")`,
				"input that cannot be parsed and must be refused", caseWrong},
			{"a reader of invalid UTF-8 bytes",
				"bytes that are not text at all", casePathological},
		}
	case typeName == "io.Writer":
		return []atomCase{
			{"a bytes.Buffer", "an ordinary destination", caseStraightforward},
			{"a writer that fails on the first write",
				"a destination that refuses, so the error is not discarded",
				caseWrong},
		}
	case strings.HasPrefix(typeName, "*"), typeName == "error",
		typeName == "interface{}", typeName == "any":
		return []atomCase{
			{"a valid value", "the ordinary case", caseStraightforward},
			{"nil", "a nil value, which every caller eventually passes",
				caseDegenerate},
		}
	default:
		return []atomCase{
			{sampleOf(typeName, 1), "an ordinary " + typeName,
				caseStraightforward},
			{zeroOf(typeName),
				"the zero value of " + typeName + ", which a caller gets when " +
					"they forget to set it", caseDegenerate},
		}
	}
}

// sampleOf renders a distinct example value of a type, for building a
// collection whose elements are not all identical.
func sampleOf(typeName string, index int) string {
	switch typeName {
	case "string":
		return fmt.Sprintf("%q", string(rune('a'+index-1)))
	case "int", "int64", "int32", "byte", "rune":
		return fmt.Sprintf("%d", index)
	case "float64":
		return fmt.Sprintf("%d.5", index)
	case "bool":
		return map[bool]string{true: "true", false: "false"}[index%2 == 1]
	default:
		return zeroOf(typeName)
	}
}

// zeroOf renders a type's zero value as it would be written in a test.
func zeroOf(typeName string) string {
	switch typeName {
	case "string":
		return `""`
	case "int", "int64", "int32", "float64", "byte", "rune":
		return "0"
	case "bool":
		return "false"
	default:
		if strings.HasPrefix(typeName, "[]") ||
			strings.HasPrefix(typeName, "map[") ||
			strings.HasPrefix(typeName, "*") {
			return "nil"
		}
		return typeName + "{}"
	}
}

// synthesiseCases builds the ladder for every atom.
//
// It is a research step, not a test: it produces the inputs worth trying and
// says why, so the stage that writes tests has something to write them from
// other than the implementation it is meant to be checking.
func synthesiseCases(functions []producedFunction) map[string][]atomCase {
	corpus := map[string][]atomCase{}
	for _, function := range functions {
		if !needsOwnTest(function) {
			continue
		}
		var cases []atomCase
		for _, parameter := range function.Parameters {
			cases = append(cases, casesForType(parameter)...)
		}
		// A function that can fail owes a case that makes it fail. Without one
		// the failure path is written and never walked, which is where the
		// worst bugs wait.
		if function.ReturnsError {
			cases = append(cases, atomCase{
				Shape: "an input this must refuse",
				Why:   "the failure path, which is written but never walked otherwise",
				Class: caseWrong,
			})
		}
		if len(cases) == 0 {
			continue
		}
		sort.SliceStable(cases, func(first, second int) bool {
			return cases[first].Class.rank() < cases[second].Class.rank()
		})
		corpus[function.Name] = cases
	}
	return corpus
}

// checkCaseCoverage reports which synthesised cases the tests never try.
//
// The check is deliberately shallow: it asks whether the case's shape appears
// in a test that names the function, not whether the test truly drives that
// path. A shallow check that names a missing empty-slice case is worth more
// than a deep one nobody can implement, and the deeper question is what the
// mutation stage exists to answer.
func checkCaseCoverage(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	corpus := synthesiseCases(functions)
	if len(corpus) == 0 {
		return skipped("the run produced no atom whose inputs could be varied")
	}
	naming, err := testsNaming(worktree)
	if err != nil {
		return broke("the produced tests could not be read: "+err.Error(), nil)
	}
	testSource, err := readTestSource(worktree)
	if err != nil {
		return broke("the produced tests could not be read: "+err.Error(), nil)
	}

	recorded := map[string]any{}
	byClass := map[string]int{}
	var untried []string
	tried, total := 0, 0
	for name, cases := range corpus {
		var missing []string
		for _, candidate := range cases {
			total++
			if caseIsTried(testSource, naming[name], candidate) {
				tried++
				byClass[string(candidate.Class)]++
				continue
			}
			missing = append(missing,
				fmt.Sprintf("[%s] %s", candidate.Class, candidate.Shape))
		}
		recorded[name] = map[string]any{
			"cases": len(cases), "untried": missing,
		}
		if len(missing) > 0 {
			untried = append(untried, fmt.Sprintf("%s: %s",
				name, strings.Join(missing, "; ")))
		}
	}
	sort.Strings(untried)
	evidence := map[string]any{
		"cases_synthesised": total, "cases_tried": tried,
		"tried_by_class": byClass, "per_function": recorded,
	}
	if len(untried) > 0 {
		return broke(fmt.Sprintf(
			"%d of %d synthesised case(s) are never tried: %s",
			total-tried, total, strings.Join(untried, " | ")), evidence)
	}
	return held(fmt.Sprintf(
		"all %d synthesised case(s) are tried, across every class from "+
			"straightforward to pathological", total), evidence)
}

// caseIsTried reports whether any test of this function mentions the shape.
func caseIsTried(testSource string, tests []string, candidate atomCase) bool {
	if len(tests) == 0 {
		return false
	}
	// Shapes that describe rather than quote cannot be searched for literally.
	// They are counted as tried when the function has any test at all, because
	// asserting more than that would be inventing a result.
	if describesRatherThanQuotes(candidate.Shape) {
		return true
	}
	return strings.Contains(testSource, candidate.Shape)
}

// readTestSource concatenates everything the run wrote as tests.
func readTestSource(worktree string) (string, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return "", err
	}
	var all strings.Builder
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(worktree, file))
		if readErr != nil {
			continue
		}
		all.Write(body)
		all.WriteString("\n")
	}
	return all.String(), nil
}

// caseInstruction tells the next attempt which inputs to try, grouped by what
// kind of assertion each one needs.
//
// Grouping by class is the point: an agent handed a flat list writes the same
// assertion for all of them, which is wrong for most. A wrong input needs a
// refusal asserted, a pathological one needs survival asserted, and only the
// straightforward ones have an exact expected value.
func caseInstruction(untried map[string][]atomCase) string {
	var report strings.Builder
	report.WriteString(
		"Your tests pass, but they only try the inputs you already had in " +
			"mind. These are implied by the signatures and none is tried:\n")

	names := make([]string, 0, len(untried))
	for name := range untried {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintf(&report, "\n%s:", name)
		grouped := map[caseClass][]atomCase{}
		var order []caseClass
		for _, candidate := range untried[name] {
			if _, seen := grouped[candidate.Class]; !seen {
				order = append(order, candidate.Class)
			}
			grouped[candidate.Class] = append(grouped[candidate.Class], candidate)
		}
		sort.Slice(order, func(first, second int) bool {
			return order[first].rank() < order[second].rank()
		})
		for _, class := range order {
			fmt.Fprintf(&report, "\n  %s — %s", class, class.assertion())
			for _, candidate := range grouped[class] {
				fmt.Fprintf(&report, "\n    - %s (%s)",
					candidate.Shape, candidate.Why)
			}
		}
	}
	report.WriteString(
		"\n\nAdd these as cases in the existing tests, simplest class first. " +
			"Do not weaken an assertion to make one pass, and do not change " +
			"behaviour the request asked for. If an input genuinely cannot " +
			"occur, say so in the test rather than omitting it.")
	return report.String()
}

// untriedCases is the corpus a run still owes, for the refinement loop.
func untriedCases(worktree string) (map[string][]atomCase, error) {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return nil, err
	}
	naming, err := testsNaming(worktree)
	if err != nil {
		return nil, err
	}
	testSource, err := readTestSource(worktree)
	if err != nil {
		return nil, err
	}
	owed := map[string][]atomCase{}
	for name, cases := range synthesiseCases(functions) {
		var missing []atomCase
		for _, candidate := range cases {
			if !caseIsTried(testSource, naming[name], candidate) {
				missing = append(missing, candidate)
			}
		}
		if len(missing) > 0 {
			owed[name] = missing
		}
	}
	return owed, nil
}

// describesRatherThanQuotes reports whether a shape is prose rather than code.
//
// The distinction decides whether the shape can be looked for in the test
// source at all. It used to be guessed from whether the shape contained a
// quote or a brace, which classified "nil" as prose — so the single most
// commonly forgotten input was never reported as missing, by the check whose
// whole job was to report missing inputs.
func describesRatherThanQuotes(shape string) bool {
	lower := strings.ToLower(shape)
	for _, opening := range []string{"a ", "an ", "the "} {
		if strings.HasPrefix(lower, opening) {
			return true
		}
	}
	for _, joining := range []string{" with ", " reversed", " of "} {
		if strings.Contains(lower, joining) {
			return true
		}
	}
	return false
}

// untriedCaseInstruction is what a run is told about inputs nothing reaches for.
//
// It names the function, the value, and what that class of input demands,
// because a run told "improve your coverage" adds a test somewhere and a run
// told "summarizeEntries is never given an empty slice, and an empty input must
// produce an empty result rather than a panic" writes that test.
//
// The cases are grouped by function and capped per function. A run handed
// thirty-nine cases at once writes a few and leaves the rest, and the ones it
// leaves are not the ones it judged least important — they are the ones at the
// bottom of a long list.
func untriedCaseInstruction(owed map[string][]atomCase) string {
	names := make([]string, 0, len(owed))
	for name := range owed {
		names = append(names, name)
	}
	sort.Strings(names)

	var instruction strings.Builder
	instruction.WriteString(
		"The code compiles and its tests pass, but these inputs are never " +
			"tried. Each one is derived from the function's own signature, so " +
			"every one of them is a value a caller can pass today.\n\n")
	for _, name := range names {
		instruction.WriteString(name)
		instruction.WriteString(":\n")
		// Six per function. Enough to cover the classes that matter and few
		// enough that the run finishes the list rather than sampling it.
		const perFunction = 6
		cases := owed[name]
		for index, candidate := range cases {
			if index == perFunction {
				instruction.WriteString(fmt.Sprintf(
					"  … and %d more of the same kind\n",
					len(cases)-perFunction))
				break
			}
			fmt.Fprintf(&instruction, "  %s — %s\n  assert: %s\n",
				candidate.Shape, candidate.Why, candidate.Class.assertion())
		}
		instruction.WriteString("\n")
	}
	instruction.WriteString(
		"Add a test for each. Where an input is one the function must refuse, " +
			"assert the refusal rather than deleting the case: a value nothing " +
			"tries is a value nobody has decided about.")
	return instruction.String()
}
