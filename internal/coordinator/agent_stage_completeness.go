package coordinator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/pipeline"
)

// completenessGaps are the ways work can compile, pass its tests, and still not
// be finished.
//
// The thesis of this flow is that every atom is tested, documented, and simple
// enough to read. Checking that after the fact and reporting it produced runs
// that ended green-ish with four untested functions and a note about it, which
// is a worse outcome than not checking at all: it makes the gap look accounted
// for. These are the same checks, expressed as work the run still owes, so the
// refinement loop can go and do it.
type completenessGaps struct {
	UntestedAtoms     []string
	UndocumentedAtoms []string
	TangledFunctions  []string
}

// Empty reports whether the run owes nothing further.
func (gaps completenessGaps) Empty() bool {
	return len(gaps.UntestedAtoms) == 0 &&
		len(gaps.UndocumentedAtoms) == 0 &&
		len(gaps.TangledFunctions) == 0
}

// Instruction is what the next attempt is told to do about it.
//
// It names the functions rather than describing the shortfall, because an
// agent told "improve your test coverage" will add a test somewhere, and one
// told "neighbors and traversable have no test" will test those two.
func (gaps completenessGaps) Instruction() string {
	var parts []string
	if len(gaps.UntestedAtoms) > 0 {
		parts = append(parts, fmt.Sprintf(
			"These functions have no test that calls them directly: %s. Write "+
				"a test for each one, exercising it on its own rather than "+
				"through whatever calls it. Cover the cases where it refuses "+
				"or returns nothing, not only the case where it works.",
			strings.Join(gaps.UntestedAtoms, ", ")))
	}
	if len(gaps.UndocumentedAtoms) > 0 {
		parts = append(parts, fmt.Sprintf(
			"These functions have no doc comment: %s. Give each one a comment "+
				"starting with its name that says what it is for, what its "+
				"arguments mean, what it returns, and how it works — the "+
				"algorithm, not a restatement of the signature.",
			strings.Join(gaps.UndocumentedAtoms, ", ")))
	}
	if len(gaps.TangledFunctions) > 0 {
		parts = append(parts, fmt.Sprintf(
			"These functions are doing too much at once: %s. Split each into "+
				"smaller functions that each do one thing, keeping the "+
				"behaviour identical so the existing tests still pass.",
			strings.Join(gaps.TangledFunctions, ", ")))
	}
	return "The code compiles and its tests pass, but the work is not " +
		"finished:\n\n" + strings.Join(parts, "\n\n")
}

// findCompletenessGaps reports what the run still owes.
//
// The tangle threshold is higher here than the one the optimisation stage
// reports on. That stage names anything worth a second look; this one only
// asks for a rewrite where the function is genuinely unreadable, because
// sending an agent back to restructure working code has a cost and a risk.
func findCompletenessGaps(
	worktree string,
	settings pipeline.Settings,
) (completenessGaps, error) {
	// The threshold for asking to split a function is looser than the one the
	// optimisation stage reports on: that stage names anything worth a second
	// look, this one only asks for a rewrite where the function is genuinely
	// unreadable, because sending an agent back to restructure working code
	// has a cost and a risk.
	unreadable := settings.TangleThreshold + 4

	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return completenessGaps{}, err
	}
	// The same rule the verification stage uses: a test that names the
	// function, not any identifier of that name anywhere in a test file.
	//
	// The looser rule counted a function's own declaration in a _test.go file
	// as evidence that it was tested, so this gate reported nothing while the
	// verification stage reported the function unproven. The run was told its
	// work was finished and the record said otherwise, which is the one thing
	// the ledger exists to prevent.
	naming, err := testsNaming(worktree)
	if err != nil {
		return completenessGaps{}, err
	}
	documented, err := documentedNames(worktree)
	if err != nil {
		return completenessGaps{}, err
	}

	var gaps completenessGaps
	for _, function := range functions {
		if settings.RequireTests && needsOwnTest(function) &&
			len(naming[function.Name]) == 0 {
			gaps.UntestedAtoms = append(gaps.UntestedAtoms, function.Name)
		}
		if settings.RequireDocComments && needsDocComment(function) &&
			!documented[function.Name] {
			gaps.UndocumentedAtoms = append(
				gaps.UndocumentedAtoms, function.Name)
		}
		if !isTestScaffolding(function) && function.Branches >= unreadable {
			gaps.TangledFunctions = append(gaps.TangledFunctions, function.Name)
		}
	}
	sort.Strings(gaps.UntestedAtoms)
	sort.Strings(gaps.UndocumentedAtoms)
	sort.Strings(gaps.TangledFunctions)
	return gaps, nil
}

// documentedNames reports which produced functions carry a doc comment.
//
// A doc comment is one attached to the declaration, which is where Go tooling
// looks and where a reader finds it. A comment floating elsewhere in the file
// documents nothing in particular.
func documentedNames(worktree string) (map[string]bool, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	documented := map[string]bool{}
	fileSet := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		tree, parseErr := parser.ParseFile(
			fileSet, filepath.Join(worktree, file), nil, parser.ParseComments)
		if parseErr != nil {
			continue
		}
		for _, declaration := range tree.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Name == nil {
				continue
			}
			if function.Doc == nil || len(function.Doc.List) == 0 {
				continue
			}
			// A comment that says nothing is not documentation. The bar is
			// low — more than a restatement of the name — because the aim is
			// to catch the empty gesture, not to grade prose.
			text := strings.TrimSpace(function.Doc.Text())
			if len(text) > len(function.Name.Name)+8 {
				documented[function.Name.Name] = true
			}
		}
	}
	return documented, nil
}

// checkAtomDocumentation requires every produced function to be documented in
// the code itself.
//
// The documentation stage used to record what a function was, derived from its
// structure, into the run's evidence. That is a true description in a place
// nobody reading the code will ever look. This checks the comment is on the
// declaration, where the next person to open the file will find it.
func checkAtomDocumentation(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	documented, err := documentedNames(worktree)
	if err != nil {
		return broke("the produced source could not be read: "+err.Error(), nil)
	}
	var missing []string
	total := 0
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		total++
		if !documented[function.Name] {
			missing = append(missing, function.Name)
		}
	}
	sort.Strings(missing)
	evidence := map[string]any{
		"functions": total, "undocumented": missing,
	}
	if total == 0 {
		return skipped("the run produced no function to document")
	}
	if len(missing) > 0 {
		return broke(fmt.Sprintf(
			"%d of %d function(s) carry no doc comment: %s",
			len(missing), total, strings.Join(missing, ", ")), evidence)
	}
	return held(fmt.Sprintf(
		"all %d function(s) carry a doc comment on the declaration itself",
		total), evidence)
}
