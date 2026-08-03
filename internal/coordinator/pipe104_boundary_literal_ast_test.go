package coordinator

import (
	"strings"
	"testing"
)

// --- PIPE-104: findUncheckedBoundaries reads literal tokens and error ---
// --- assertions from the test files' syntax trees, not their raw bytes. ---

// findingWhat returns the What text of the first finding matching a
// substring, or "" if none does, so a test can assert a specific edge fired
// without depending on slice order.
func findingWhat(findings []adversarialFinding, wantSubstring string) string {
	for _, finding := range findings {
		if strings.Contains(finding.What, wantSubstring) {
			return finding.What
		}
	}
	return ""
}

// TestPIPE104_AFileModeLiteralDoesNotSuppressTheZeroEdgeFinding is the
// discrimination proof for the boundary finder's substring bug.
//
// Proven to discriminate: the prior version searched for the token "0" as a
// raw substring of the concatenated test source. This fixture's test file
// contains the text "0644" (an os.FileMode literal, unrelated to Divide's
// own int parameter) and never actually calls Divide(0). The prior
// substring search would find "0" inside "0644" and conclude the zero edge
// had been tried, silently dropping a real gap. Reading the syntax tree's
// actual literal nodes instead finds no BasicLit whose value is exactly
// "0", so the finding still fires.
func TestPIPE104_AFileModeLiteralDoesNotSuppressTheZeroEdgeFinding(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe104/filemode\n\ngo 1.26.0\n",
		"lib.go": `package lib

func Divide(divisor int) int {
	return 100 / divisor
}
`,
		"lib_test.go": `package lib

import (
	"os"
	"testing"
)

func TestDivideWithFileWrite(t *testing.T) {
	os.WriteFile("out.txt", []byte("x"), 0644)
	if Divide(5) != 20 {
		t.Fatal("bad division")
	}
}
`,
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	findings := findUncheckedBoundaries(worktree, functions)
	if what := findingWhat(findings, "zero"); what == "" {
		t.Errorf("the zero-input edge for Divide was not flagged; the 0644 "+
			"file-mode literal appears to have suppressed it: %+v", findings)
	}
}

// TestPIPE104_APassedZeroLiteralSuppressesTheZeroEdgeFinding is the
// companion regression control: when the suite genuinely passes a literal
// 0 as an argument, the edge is truly tried and must not be flagged.
func TestPIPE104_APassedZeroLiteralSuppressesTheZeroEdgeFinding(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe104/realzero\n\ngo 1.26.0\n",
		"lib.go": `package lib

func Divide(divisor int) int {
	if divisor == 0 {
		return -1
	}
	return 100 / divisor
}
`,
		"lib_test.go": `package lib

import "testing"

func TestDivideByZero(t *testing.T) {
	if Divide(0) != -1 {
		t.Fatal("expected sentinel for zero divisor")
	}
}
`,
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	findings := findUncheckedBoundaries(worktree, functions)
	if what := findingWhat(findings, "zero"); what != "" {
		t.Errorf("Divide(0) is a real literal 0 argument; the zero edge "+
			"should not be flagged: %q", what)
	}
}

// TestPIPE104_TErrorfDoesNotSuppressTheErrorAssertionFinding is the
// discrimination proof for the finder's second substring bug.
//
// Proven to discriminate: the prior version searched for the raw substring
// "Error" anywhere in the test source, which t.Errorf's own method name
// always satisfies -- this fixture calls t.Errorf on an unrelated numeric
// mismatch and never inspects the error Parse returns at all. The prior
// check would read "Error" inside "t.Errorf" and conclude an error had been
// asserted on; the syntax-tree check matches only an exact `.Error()` call
// or a nil comparison naming an error, neither of which this file contains.
func TestPIPE104_TErrorfDoesNotSuppressTheErrorAssertionFinding(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe104/errorf\n\ngo 1.26.0\n",
		"lib.go": `package lib

import "errors"

func Parse(text string) (int, error) {
	if text == "" {
		return 0, errors.New("empty")
	}
	return len(text), nil
}
`,
		"lib_test.go": `package lib

import "testing"

func TestParse(t *testing.T) {
	result, _ := Parse("abc")
	if result != 3 {
		t.Errorf("got %d, want 3", result)
	}
}
`,
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	findings := findUncheckedBoundaries(worktree, functions)
	if what := findingWhat(findings, "never asserts on a returned error"); what == "" {
		t.Errorf("Parse's error result is discarded with `_` and never " +
			"inspected; the finding should have fired but did not")
	}
}

// TestPIPE104_ARealNilComparisonSuppressesTheErrorAssertionFinding is the
// regression control: a test that genuinely compares an error to nil must
// not be flagged.
func TestPIPE104_ARealNilComparisonSuppressesTheErrorAssertionFinding(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe104/realnil\n\ngo 1.26.0\n",
		"lib.go": `package lib

import "errors"

func Parse(text string) (int, error) {
	if text == "" {
		return 0, errors.New("empty")
	}
	return len(text), nil
}
`,
		"lib_test.go": `package lib

import "testing"

func TestParseRejectsEmpty(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Fatal("expected an error for empty input")
	}
}
`,
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	findings := findUncheckedBoundaries(worktree, functions)
	if what := findingWhat(findings, "never asserts on a returned error"); what != "" {
		t.Errorf("the test compares err against nil directly; the error "+
			"assertion finding should not fire: %q", what)
	}
}
