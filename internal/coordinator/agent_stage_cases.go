package coordinator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
				"the same element repeated, so duplicate handling is decided " +
					"rather than assumed",
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
				"entries whose values are equal, so the choice between them is " +
					"decided rather than assumed",
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
			cases = append(cases,
				withoutExplosiveCases(function, casesForType(parameter))...)
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
	// A composite literal's element values are the synthesiser's invention:
	// nothing about `[]string{"a", "b", "c"}` says "a", it says "three
	// distinct strings". Searching the test source for that exact text asked
	// a test to contain values chosen at random by this file, which no
	// correct test would, so a run that wrote a thorough table-driven suite
	// over its real inputs still failed with ten of eighteen cases untried.
	//
	// What is checked instead is the shape's structure: the same type, the
	// same cardinality class, and whether the elements repeat. Those are the
	// properties the case was synthesised to demand — an empty slice, a
	// one-element slice, a slice with a duplicate — and a test exercising the
	// same structure with its own values satisfies them. A scalar shape has
	// no invented part and is still matched literally: "nil", `""` and 0 mean
	// exactly themselves.
	// A plain integer shape has an invented part too, and it is the whole of it.
	// Nothing about 42 says forty-two; it says "an ordinary positive value", and
	// a FizzBuzz test calling the function with 7 has tried one. Ladder rung 3
	// spent five of its six attempts being asked for 42 by name.
	//
	// math.MaxInt and math.MinInt are left to the literal match below. Those are
	// not invented: they name the ends of the range, and no other value answers
	// the question they ask.
	if wanted, isInteger := integerShapeSignature(candidate.Shape); isInteger {
		return testSourceHasIntegerShape(testSource, wanted)
	}
	// A quoted string shape has the same invented part a composite literal has.
	// Nothing about `"héllo wörld"` says those words; it says "text with
	// characters outside ASCII". Matching the text asked a run to guess the
	// exact greeting this file happens to contain, and asked it to write
	// strings.Repeat("x", 100000) with that x and that count, so every rung
	// taking a string parameter had cases it could not have tried.
	if wanted, isString := stringShapeSignature(candidate.Shape); isString {
		return testSourceHasStringShape(testSource, wanted)
	}
	if wanted, isComposite := compositeShapeSignature(candidate.Shape); isComposite {
		// An empty composite and nil are one demand, not two. A function that
		// ranges over a slice cannot tell []string{} from a nil []string —
		// both have length zero and both yield nothing — so a suite that
		// passes nil has tried the empty input. The synthesiser emits both
		// forms because both are worth naming in the instruction; counting
		// them separately asked the run to write the same test twice, and a
		// run that had covered the case was told it had not.
		if wanted.cardinality == "none" && mentionsNilArgument(testSource) {
			return true
		}
		return testSourceHasCompositeShape(testSource, wanted)
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

// mentionsNilArgument reports whether the tests pass nil anywhere.
//
// Deliberately shallow, in the same way this whole check is: it asks whether
// the suite ever supplies nil, not whether it supplies it to this parameter.
// The alternative is resolving argument positions through the call graph,
// which is what the mutation stage is for, and the failure mode of being
// shallow here is counting an empty case as covered by a nil somewhere else —
// far cheaper than telling a run to write a test it has already written.
func mentionsNilArgument(testSource string) bool {
	for index := 0; index < len(testSource); {
		found := strings.Index(testSource[index:], "nil")
		if found < 0 {
			return false
		}
		at := index + found
		before := byte(' ')
		if at > 0 {
			before = testSource[at-1]
		}
		after := byte(' ')
		if at+3 < len(testSource) {
			after = testSource[at+3]
		}
		if !isIdentifierByte(before) && !isIdentifierByte(after) {
			return true
		}
		index = at + 3
	}
	return false
}

// isIdentifierByte reports whether a byte can appear inside a Go identifier,
// so "nil" inside "nilable" is not read as the keyword.
func isIdentifierByte(character byte) bool {
	switch {
	case character >= 'a' && character <= 'z',
		character >= 'A' && character <= 'Z',
		character >= '0' && character <= '9',
		character == '_':
		return true
	default:
		return false
	}
}

// integerShapeKind is the property a synthesised integer case asks about.
//
// Zero and one are their own kinds because the cases say so: zero is neither
// positive nor a count, and one is the smallest useful value and where
// off-by-one lives. Everything above one is the same question — an ordinary
// positive value — and any of them answers it.
type integerShapeKind string

const (
	integerShapeZero            integerShapeKind = "zero"
	integerShapeOne             integerShapeKind = "one"
	integerShapeNegative        integerShapeKind = "negative"
	integerShapeOrdinaryPositiv integerShapeKind = "ordinary-positive"
)

// integerShapeSignature reads a shape written as a plain decimal integer.
//
// It reports false for anything else, so math.MaxInt, math.MinInt and the
// prose case describing an input the contract excludes are untouched.
func integerShapeSignature(shape string) (integerShapeKind, bool) {
	trimmed := strings.TrimSpace(shape)
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return "", false
	}
	switch {
	case value == 0:
		return integerShapeZero, true
	case value == 1:
		return integerShapeOne, true
	case value < 0:
		return integerShapeNegative, true
	default:
		return integerShapeOrdinaryPositiv, true
	}
}

// testSourceHasIntegerShape reports whether the tests use an integer with the
// same property.
func testSourceHasIntegerShape(testSource string, wanted integerShapeKind) bool {
	for _, value := range integerLiteralsIn(testSource) {
		found, _ := integerShapeSignature(strconv.Itoa(value))
		if found == wanted {
			return true
		}
	}
	return false
}

// integerLiteralsIn returns every decimal integer literal in the source.
//
// A run of digits touching an identifier character is not a literal: the 8 in
// int8 and the 256 in buffer256 are parts of names, and counting them would
// find an ordinary positive value in a suite that never passes one.
func integerLiteralsIn(source string) []int {
	var values []int
	for index := 0; index < len(source); {
		if source[index] < '0' || source[index] > '9' {
			index++
			continue
		}
		start := index
		for index < len(source) && source[index] >= '0' && source[index] <= '9' {
			index++
		}
		if start > 0 && isIdentifierByte(source[start-1]) && source[start-1] != '-' {
			continue
		}
		if index < len(source) && isIdentifierByte(source[index]) {
			continue
		}
		text := source[start:index]
		if start > 0 && source[start-1] == '-' {
			text = "-" + text
		}
		value, err := strconv.Atoi(text)
		if err != nil {
			continue
		}
		values = append(values, value)
	}
	return values
}

// stringShapeKind is the property a synthesised string case asks about, once
// the particular characters the synthesiser chose are discarded.
//
// The eight string cases map to eight distinct kinds, so this loses nothing the
// case ladder was demanding: a test that pads its own word has tried the padded
// input, and a test that repeats its own character ten thousand times has tried
// text far longer than expected.
type stringShapeKind string

const (
	stringShapeEmpty           stringShapeKind = "empty"
	stringShapeWhitespaceOnly  stringShapeKind = "whitespace-only"
	stringShapeNonASCII        stringShapeKind = "non-ascii"
	stringShapeVeryLong        stringShapeKind = "very-long"
	stringShapePadded          stringShapeKind = "padded"
	stringShapeMixedSeparators stringShapeKind = "mixed-separators"
	stringShapeSingleCharacter stringShapeKind = "single-character"
	stringShapeOrdinary        stringShapeKind = "ordinary"
)

// stringShapeSignature reads a shape written as a quoted string, or as a call
// that builds one far longer than any literal would be written out.
//
// It reports false for anything else, so "nil", 0 and 42 keep being matched
// literally. Those have no invented part: they mean exactly themselves.
func stringShapeSignature(shape string) (stringShapeKind, bool) {
	trimmed := strings.TrimSpace(shape)
	if strings.HasPrefix(trimmed, "strings.Repeat(") ||
		strings.HasPrefix(trimmed, "bytes.Repeat(") {
		return stringShapeVeryLong, true
	}
	if len(trimmed) < 2 || !strings.HasPrefix(trimmed, `"`) ||
		!strings.HasSuffix(trimmed, `"`) {
		return "", false
	}
	// One literal, not two side by side: a shape holding an unescaped quote in
	// the middle is something else, and guessing at it would classify it wrong.
	inner := trimmed[1 : len(trimmed)-1]
	if strings.Contains(strings.ReplaceAll(inner, `\"`, ""), `"`) {
		return "", false
	}
	return classifyStringShape(inner), true
}

// classifyStringShape names the property a string literal's contents have.
//
// The order is the order the properties exclude one another: text outside ASCII
// may also be padded, and a case asking for one is not answered by a test
// covering the other, so the more specific question is asked first.
func classifyStringShape(inner string) stringShapeKind {
	text := strings.ReplaceAll(inner, `\t`, "\t")
	text = strings.ReplaceAll(text, `\n`, "\n")
	switch {
	case text == "":
		return stringShapeEmpty
	case strings.TrimSpace(text) == "":
		return stringShapeWhitespaceOnly
	case containsNonASCII(text):
		return stringShapeNonASCII
	case len(text) >= 1000:
		return stringShapeVeryLong
	case text != strings.TrimSpace(text):
		return stringShapePadded
	case hasSeparatorBetweenContent(text):
		return stringShapeMixedSeparators
	case len([]rune(text)) == 1:
		return stringShapeSingleCharacter
	default:
		return stringShapeOrdinary
	}
}

// containsNonASCII reports whether the text holds a character outside the
// ASCII range, which is where byte length and rune length part company.
//
// Written either way. A test may put the character in the file directly, as
// "héllo", or escape it, as "héllo"; the two produce the same string and
// a case asking for one is answered by either.
func containsNonASCII(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] >= 0x80 {
			return true
		}
		if text[index] != '\\' || index+1 >= len(text) {
			continue
		}
		switch text[index+1] {
		case 'u', 'U':
			return true
		case 'x':
			// \x80 and above; \x41 is an ASCII letter written the long way.
			if index+2 < len(text) && text[index+2] >= '8' {
				return true
			}
		}
	}
	return false
}

// hasSeparatorBetweenContent reports whether a tab, a newline or a run of
// spaces separates content from other content.
//
// Content on both sides is required so a trailing "\n" in a message does not
// read as an input with mixed separators. That distinction is the difference
// between a case genuinely tried and a format string counted by accident.
func hasSeparatorBetweenContent(text string) bool {
	for _, separator := range []string{"\t", "\n", "  "} {
		at := strings.Index(text, separator)
		if at < 0 {
			continue
		}
		before := strings.TrimSpace(text[:at])
		after := strings.TrimSpace(text[at+len(separator):])
		if before != "" && after != "" {
			return true
		}
	}
	return false
}

// testSourceHasStringShape reports whether the tests build a string with the
// same property.
//
// Format strings and assertion messages are skipped. A test source is full of
// "%d\n" and "got %v, want %v", and counting those would satisfy the separator
// and ordinary-text cases in any suite ever written, which is a false green in
// the check whose whole job is to report what was never tried.
func testSourceHasStringShape(testSource string, wanted stringShapeKind) bool {
	if wanted == stringShapeVeryLong &&
		(strings.Contains(testSource, "strings.Repeat(") ||
			strings.Contains(testSource, "bytes.Repeat(")) {
		return true
	}
	for _, literal := range quotedStringLiterals(testSource) {
		if isFormatOrMessageLiteral(literal) {
			continue
		}
		if classifyStringShape(literal) == wanted {
			return true
		}
	}
	return false
}

// quotedStringLiterals returns the contents of every interpreted string literal
// in the source, with the escapes left as they were written.
func quotedStringLiterals(source string) []string {
	var literals []string
	for index := 0; index < len(source); index++ {
		if source[index] != '"' {
			continue
		}
		// A rune literal holding a quote is not a string literal.
		if index > 0 && source[index-1] == '\'' {
			continue
		}
		escaped := false
		for scan := index + 1; scan < len(source); scan++ {
			character := source[scan]
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == '"':
				literals = append(literals, source[index+1:scan])
				index = scan
				scan = len(source)
			case character == '\n':
				// An unterminated literal is not one; stop rather than run on
				// through the rest of the file looking for a closing quote.
				index = scan
				scan = len(source)
			}
		}
	}
	return literals
}

// isFormatOrMessageLiteral reports whether a literal is printing rather than
// being fed to the code under test.
func isFormatOrMessageLiteral(literal string) bool {
	for index := 0; index+1 < len(literal); index++ {
		if literal[index] != '%' {
			continue
		}
		next := literal[index+1]
		if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') {
			return true
		}
	}
	return false
}

// compositeShapeSignature is what a composite literal is being asked for,
// with the synthesiser's arbitrary element values discarded.
//
// cardinality is bucketed rather than exact — none, one, many — because the
// case classes ask for an empty input, a single-element input, or an input
// with several, and never for exactly three. repeats records whether every
// element is the same, which is the one element-level property a case
// genuinely demands (a duplicate is a different case from three distinct
// values, and a run that only ever tests distinct values has not tried it).
type compositeShape struct {
	typeText    string
	cardinality string
	repeats     bool
}

// compositeShapeSignature reads a shape written as `T{...}`.
//
// It reports false for anything else, including a bare scalar, so the caller
// falls back to matching that text literally.
func compositeShapeSignature(shape string) (compositeShape, bool) {
	open := strings.Index(shape, "{")
	if open < 0 || !strings.HasSuffix(strings.TrimSpace(shape), "}") {
		return compositeShape{}, false
	}
	typeText := strings.TrimSpace(shape[:open])
	if typeText == "" {
		return compositeShape{}, false
	}
	inner := strings.TrimSpace(shape[open+1 : strings.LastIndex(shape, "}")])
	return compositeShape{
		typeText:    typeText,
		cardinality: cardinalityOf(splitTopLevelElements(inner)),
		repeats:     elementsRepeat(splitTopLevelElements(inner)),
	}, true
}

// testSourceHasCompositeShape reports whether the tests build a literal of the
// same type with the same structure.
func testSourceHasCompositeShape(testSource string, wanted compositeShape) bool {
	for index := 0; ; {
		found := strings.Index(testSource[index:], wanted.typeText+"{")
		if found < 0 {
			return false
		}
		start := index + found + len(wanted.typeText)
		inner, end, ok := balancedBraceContent(testSource, start)
		if !ok {
			return false
		}
		elements := splitTopLevelElements(inner)
		if cardinalityOf(elements) == wanted.cardinality &&
			elementsRepeat(elements) == wanted.repeats {
			return true
		}
		index = end
	}
}

// balancedBraceContent returns the text between the brace at open and its
// match, and the index just past that match.
func balancedBraceContent(text string, open int) (string, int, bool) {
	if open >= len(text) || text[open] != '{' {
		return "", 0, false
	}
	depth := 0
	for index := open; index < len(text); index++ {
		switch text[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[open+1 : index], index + 1, true
			}
		}
	}
	return "", 0, false
}

// splitTopLevelElements splits a literal's contents on commas that are not
// inside a nested literal or a string.
func splitTopLevelElements(inner string) []string {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return nil
	}
	var elements []string
	depth, quoted, escaped, start := 0, false, false, 0
	for index := 0; index < len(trimmed); index++ {
		character := trimmed[index]
		switch {
		case escaped:
			escaped = false
		case character == '\\' && quoted:
			escaped = true
		case character == '"':
			quoted = !quoted
		case quoted:
		case character == '{' || character == '[' || character == '(':
			depth++
		case character == '}' || character == ']' || character == ')':
			depth--
		case character == ',' && depth == 0:
			elements = append(elements, strings.TrimSpace(trimmed[start:index]))
			start = index + 1
		}
	}
	if last := strings.TrimSpace(trimmed[start:]); last != "" {
		elements = append(elements, last)
	}
	return elements
}

// cardinalityOf buckets how many elements a literal holds.
func cardinalityOf(elements []string) string {
	switch len(elements) {
	case 0:
		return "none"
	case 1:
		return "one"
	default:
		return "many"
	}
}

// elementsRepeat reports whether any element appears more than once.
//
// Any, not all. The synthesised shape is written as three identical elements,
// but what it is asking about is stated in its own rationale: "the same element
// repeated, so ties must be broken somehow". A tie takes two equal values, and
// []string{"4", "4", "-2"} breaks one exactly as []string{"4", "4", "4"} does.
// Requiring every element to match asked for the literal rather than the
// property, and refused a test written under the heading "repeated elements"
// for holding a third value that was not a duplicate.
//
// This blocked twelve later stages on a rung whose tests were correct. The
// check downstream of this is shallow on purpose — it looks for the shape of a
// case, not proof the path ran — and a shallow check that reads a literal
// letter by letter is not shallow, it is brittle.
//
// False for fewer than two elements: a single value cannot repeat, and
// reporting that it does would make the one-element case indistinguishable
// from the duplicate case.
func elementsRepeat(elements []string) bool {
	if len(elements) < 2 {
		return false
	}
	seen := make(map[string]bool, len(elements))
	for _, element := range elements {
		if seen[element] {
			return true
		}
		seen[element] = true
	}
	return false
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
func untriedCaseInstruction(owed map[string][]atomCase, testsPassed bool) string {
	names := make([]string, 0, len(owed))
	for name := range owed {
		names = append(names, name)
	}
	sort.Strings(names)

	var instruction strings.Builder
	instruction.WriteString(
		validationPreamble(testsPassed) + "these inputs are never " +
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

// withoutExplosiveCases drops cases whose cost is proportional to the value
// being suggested.
//
// math.MaxInt is a fair question for a function that adds, and an impossible
// one for a function that counts to its argument or prints that many lines: the
// test cannot finish, and asking for it produces either a hung suite or a run
// that spends its attempts arguing with a case it cannot satisfy. Ladder rung 3
// is FizzBuzz. It was asked for math.MaxInt and math.MinInt while its whole
// body is a loop bounded by that argument.
//
// The estimate is deliberately crude, because the precise question — does this
// function do work proportional to this parameter — is undecidable, and a crude
// answer in the safe direction costs one case on a function that could have
// afforded it. A hung suite costs the run.
//
// Only the pathological integer cases are affected. Every other case is a fixed
// small value or a shape, and none of them get more expensive as the number
// gets bigger.
func withoutExplosiveCases(
	function producedFunction, cases []atomCase,
) []atomCase {
	if !workGrowsWithItsArgument(function) {
		return cases
	}
	kept := make([]atomCase, 0, len(cases))
	for _, candidate := range cases {
		if candidate.Class == casePathological &&
			(strings.Contains(candidate.Shape, "math.MaxInt") ||
				strings.Contains(candidate.Shape, "math.MinInt")) {
			continue
		}
		kept = append(kept, candidate)
	}
	// The boundary still deserves a case; what it does not deserve is a value
	// that cannot be run. A bounded representative asks the same question --
	// what happens well past what the author pictured -- and finishes.
	return append(kept, atomCase{
		Shape: "a large but bounded value, such as 100000",
		Why: "far more than the author pictured, without asking for a run " +
			"that cannot finish: this function does work proportional to " +
			"this argument, so math.MaxInt is not a test, it is a hang",
		Class: casePathological,
	})
}

// workGrowsWithItsArgument reports whether a function loops or emits output in
// proportion to what it is given.
//
// Read from the structure already recorded: a loop in the body, and an integer
// among the parameters. A function with neither cannot be made slow by a large
// number, and one with both very likely can.
func workGrowsWithItsArgument(function producedFunction) bool {
	if function.LoopDepth == 0 {
		return false
	}
	for _, parameter := range function.Parameters {
		switch parameter {
		case "int", "int64", "int32", "uint", "uint64", "uint32":
			return true
		}
	}
	return false
}
