package coordinator

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/executor"
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
		// A patch is judged by the file it would produce, not by its own text.
		// Applying it here rather than reasoning about hunks means the same
		// syntax-tree comparison decides both tools, and a patch that sneaks a
		// statement past a comment round is caught by the same check that
		// catches a whole-file write doing it.
		if patched, ok := patchResultFor(worktree, path, content); ok {
			content = []byte(patched)
		}
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

// patchLimits is how much a round of this kind may change.
//
// A patch tool removes the accidental rewrite and not the deliberate one: a
// model can rewrite a file hunk by hunk as thoroughly as in one write. What
// bounds it is what was asked for. A comment repair that moves forty lines has
// not understood the request, and refusing it while the working program is
// still there costs one turn instead of an attempt.
//
// Ordinary rounds are unbounded. A run building something is entitled to write
// as much of it as it needs, and a limit there would be a limit on the work
// rather than on the drift.
func (scope editScope) patchLimits() executor.PatchLimits {
	switch scope {
	case editCommentsOnly:
		// A doc comment is a handful of lines above one declaration. Two
		// declarations documented at once is still one round's work; six is a
		// rewrite with comments as the excuse.
		return executor.PatchLimits{
			MaximumChangedLines: 40,
			MaximumHunks:        6,
			MaximumFileShare:    0.5,
		}
	case editTestsOnly:
		// A test round adds cases. It is legitimately larger than a comment
		// round — a table-driven test for four inputs is thirty lines — and it
		// still has no business replacing the file.
		return executor.PatchLimits{
			MaximumChangedLines: 120,
			MaximumHunks:        8,
			MaximumFileShare:    0.75,
		}
	default:
		return executor.UnboundedPatch
	}
}

// patchResultFor renders what a patch would leave on disk, so a scope rule can
// judge the result rather than the diff.
//
// Reports false when the argument is not a patch or cannot be applied. Both are
// somebody else's complaint with a better message than this could give, and a
// scope check is not the place to raise them.
func patchResultFor(worktree, path string, argument []byte) (string, bool) {
	request, err := executor.ParsePatch(string(argument))
	if err != nil {
		return "", false
	}
	existing, err := os.ReadFile(filepath.Join(worktree, filepath.FromSlash(path)))
	if err != nil {
		return "", false
	}
	patched, _, err := executor.ApplyPatch(string(existing), request)
	if err != nil {
		return "", false
	}
	return patched, true
}

// patchStaysSmall reports why a patch is broader than this round allows, or
// nothing when it is not.
//
// Measured after applying it in memory, because the interesting numbers — how
// many lines actually moved, how much of the file that is — are not knowable
// from the patch text. Nothing is written either way; this only decides whether
// the real write is allowed to happen.
func (narrator *narratingExecutor) patchStaysSmall(
	request executor.ToolRequest, path string,
) string {
	limits := narrator.permitted.patchLimits()
	if limits == executor.UnboundedPatch {
		return ""
	}
	parsed, err := executor.ParsePatch(toolArgument(request, "patch"))
	if err != nil {
		// Malformed patches are refused by the tool itself, with a better
		// message than this could give.
		return ""
	}
	existing, err := os.ReadFile(
		filepath.Join(narrator.worktree, filepath.FromSlash(path)))
	if err != nil {
		return ""
	}
	_, outcome, err := executor.ApplyPatch(string(existing), parsed)
	if err != nil {
		return ""
	}
	if err := limits.WithinLimits(
		outcome, strings.Count(string(existing), "\n")+1,
	); err != nil {
		return err.Error()
	}
	return ""
}
