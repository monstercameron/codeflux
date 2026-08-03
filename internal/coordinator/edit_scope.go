package coordinator

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// editScope is what an attempt is allowed to change.
//
// The instructions already say it — "add the missing test first", "these
// functions have no doc comment" — and an instruction is a request. Ladder rung
// 6 was asked for a comment on main and rewrote the evaluator's error
// semantics; it was asked to add two tests and rewrote production code in the
// same turn, breaking a program that was already correct about both inputs.
//
// Saying it twice does not help. What helps is refusing the write, at the
// moment it is made, with the reason: the run then knows immediately rather
// than three gates later, and the working program is still there.
type editScope int

const (
	// editAnything is the ordinary case: the run is building or repairing and
	// may touch whatever it needs.
	editAnything editScope = iota
	// editTestsOnly is a blind-spot round. The finding is that nothing checks a
	// behaviour, and the remedy is a test. If the test passes, the behaviour
	// was already right and there was nothing to change; if it fails, that is
	// the run's evidence for changing production code, and it gets a round to
	// do so.
	editTestsOnly
	// editCommentsOnly is a documentation round. Every executable byte must
	// come out identical, because the thing being asked for is a comment.
	editCommentsOnly
)

// permits reports whether a write is inside the scope, and says why when it is
// not.
//
// The message is written to the model rather than to a log. It names the scope,
// what it saw, and the one thing to do instead, because a refusal a run cannot
// act on is a wasted turn either way.
func (scope editScope) permits(
	worktree, path string, content []byte,
) (bool, string) {
	switch scope {
	case editTestsOnly:
		if isProducedTestFile(path) {
			return true, ""
		}
		return false, "This round is for tests. The review found behaviour " +
			"that nothing checks, which is not the same as behaviour that is " +
			"wrong: write the test, run it, and only if it fails is there " +
			"anything to change in " + path + ". Add the test to a _test.go " +
			"file and run the suite."
	case editCommentsOnly:
		if isProducedTestFile(path) {
			return false, "This round is for documentation. " + path +
				" is a test file, and the request was for comments on " +
				"declarations; nothing here needs a test to change."
		}
		identical, err := executableCodeIsIdentical(
			filepath.Join(worktree, filepath.FromSlash(path)), content)
		if err != nil {
			// Unparseable content is refused by the write guard on its own
			// terms, with a better message than this one could give.
			return true, ""
		}
		if identical {
			return true, ""
		}
		return false, "This round is for documentation, and this write " +
			"changes what " + path + " does, not only what it says. Add the " +
			"comment and leave every declaration, statement, import and " +
			"signature exactly as it is. If the code looks wrong to you, say " +
			"so in your reply rather than changing it here — a documentation " +
			"round is not the place, and the program currently passes its " +
			"tests and its acceptance examples."
	default:
		return true, ""
	}
}

// isProducedTestFile reports whether a path is a Go test file.
func isProducedTestFile(path string) bool {
	return strings.HasSuffix(filepath.ToSlash(path), "_test.go")
}

// executableCodeIsIdentical reports whether two versions of a file differ only
// in their comments.
//
// Compared by printing both syntax trees with comments discarded, so formatting
// and comment text fall out and everything else — declarations, statements,
// imports, signatures — has to match byte for byte. Reports an error when
// either side does not parse, which the caller treats as "not this check's
// business": a file that does not parse has a better complaint waiting for it.
func executableCodeIsIdentical(path string, proposed []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	before, err := executableShapeOf(existing)
	if err != nil {
		return false, err
	}
	after, err := executableShapeOf(proposed)
	if err != nil {
		return false, err
	}
	return before == after, nil
}

// executableShapeOf renders a file with its comments removed.
func executableShapeOf(source []byte) (string, error) {
	fileSet := token.NewFileSet()
	tree, err := parser.ParseFile(fileSet, "source.go", source, 0)
	if err != nil {
		return "", err
	}
	// Comments are already absent: parsing without ParseComments drops them,
	// and printing the tree renders only what executes.
	tree.Comments = nil
	var rendered strings.Builder
	if err := (&printer.Config{Mode: printer.RawFormat}).Fprint(
		&rendered, fileSet, tree,
	); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

// scopeOfNextAttempt decides what the next attempt may touch, from what it is
// being asked for.
//
// Keyed on the gate rather than on the prose, because the prose is what the
// model reads and the gate is what the coordinator decided. The two can drift;
// the gate cannot.
func scopeOfNextAttempt(gate string, blindSpotsOnly bool) editScope {
	switch {
	case gate == "adversarial-review" && blindSpotsOnly:
		return editTestsOnly
	case gate == "atom-documentation":
		return editCommentsOnly
	default:
		return editAnything
	}
}

// unusedASTImport keeps the go/ast import honest if a later edit drops its use.
var _ = ast.Node(nil)

// String names the scope for the trace.
func (scope editScope) String() string {
	switch scope {
	case editTestsOnly:
		return "test files only"
	case editCommentsOnly:
		return "comments only, with the executable code unchanged"
	default:
		return "anything"
	}
}
