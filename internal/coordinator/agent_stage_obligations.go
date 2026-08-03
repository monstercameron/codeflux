package coordinator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

// checkPropertyTests looks for tests that examine more than one case.
//
// A test with a single example checks one point. A table-driven test checks a
// set, and a set is the closest thing Go's standard library gives you to a
// property. The distinction matters because a suite made entirely of single
// examples passes for exactly the inputs somebody thought of, which are the
// inputs least likely to be wrong.
func checkPropertyTests(worktree string) stageOutcome {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return broke("the produced tests could not be read: "+err.Error(), nil)
	}
	fileSet := token.NewFileSet()
	tests, tabular := 0, 0
	var singleExample []string
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		tree, parseErr := parser.ParseFile(
			fileSet, filepath.Join(worktree, file), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			continue
		}
		for _, declaration := range tree.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Name == nil ||
				!strings.HasPrefix(function.Name.Name, "Test") {
				continue
			}
			tests++
			if iteratesOverCases(function) {
				tabular++
				continue
			}
			singleExample = append(singleExample, function.Name.Name)
		}
	}
	sort.Strings(singleExample)
	evidence := map[string]any{
		"tests": tests, "over_a_set_of_cases": tabular,
		"single_example": singleExample,
	}
	if tests == 0 {
		return broke("the run wrote no test, so nothing examines any property "+
			"of what it built", evidence)
	}
	if tabular == 0 {
		return broke(fmt.Sprintf(
			"all %d test(s) check a single example, so the suite passes for "+
				"exactly the inputs somebody thought of", tests), evidence)
	}
	return held(fmt.Sprintf(
		"%d of %d test(s) examine a set of cases rather than one example",
		tabular, tests), evidence)
}

// iteratesOverCases reports whether a test loops over a collection.
func iteratesOverCases(function *ast.FuncDecl) bool {
	found := false
	ast.Inspect(function, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.RangeStmt, *ast.ForStmt:
			found = true
			return false
		}
		return true
	})
	return found
}

// checkSimplification reports whether anything produced is more tangled than
// it needs to be.
//
// This is the optimiser's gate seen from the only angle that is safe: it does
// not rewrite anything, it reports what a rewrite would target, and it says so
// plainly. An optimiser that changed code before the tests were known to
// detect a mistake would be a defect generator wearing a useful name.
//
// Candidates are drawn only from the declarations this run is answerable for
// (PIPE-113): a pre-existing tangled function nobody touched is not a
// simplification this run owes, and naming it here would ask a future
// attempt to rewrite code its own task never mentioned. attribution fails
// toward inclusion when it could not be established.
//
// Functions are enumerated from attribution's own file set, not
// producedGoFiles' git-status view: a run that has committed to its own
// worktree leaves git status clean, and the old enumeration would silently
// find nothing rather than correctly narrowing to what changed (PIPE-111's
// design caution).
func checkSimplification(worktree string, attribution changeAttribution) stageOutcome {
	functions, err := attributedFunctions(worktree, attribution)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	scope := attributeDeclarations(functions, attribution)
	// A function with many decision points is doing more than one thing. The
	// threshold is loose on purpose: the aim is to name the genuinely tangled,
	// not to have an opinion about every function.
	const tangled = 8
	var candidates []string
	attributedCount := 0
	for _, function := range functions {
		if isTestScaffolding(function) || !scope.Contains(function.Name) {
			continue
		}
		attributedCount++
		if function.Branches >= tangled {
			candidates = append(candidates, fmt.Sprintf(
				"%s (%d decision points)", function.Name, function.Branches))
		}
	}
	sort.Strings(candidates)
	evidence := map[string]any{
		"functions": attributedCount, "worth_simplifying": candidates,
		"rewritten": false,
	}
	if len(functions) == 0 {
		return skipped("the run produced nothing to simplify")
	}
	if attributedCount == 0 {
		return skipped(
			"none of the produced functions is a declaration this run changed, " +
				"so there is nothing this run is answerable for simplifying")
	}
	// The gate says the atom is rewritten to be simpler where it can be. No
	// rewrite is performed, so this stage may not report satisfied: doing so
	// claimed a rewrite that never happened, in the ledger whose whole rule is
	// that "we did not check this" and "this passed" must not render the same
	// way (PIPE-010). The candidates are kept as evidence, because naming what
	// is worth simplifying is the useful part of what the check did do.
	if len(candidates) > 0 {
		return skippedWith(fmt.Sprintf(
			"no rewrite is performed by this build; %d function(s) are tangled "+
				"enough to be worth simplifying: %s",
			len(candidates), strings.Join(candidates, ", ")), evidence)
	}
	return skippedWith(
		"no rewrite is performed by this build; no function is tangled enough "+
			"to be worth rewriting in any case", evidence)
}

// checkControlTests requires the failure paths to be examined, not just the
// happy one.
//
// A suite that only walks the intended path has checked the case least likely
// to be wrong. The program's branches are counted from the source and compared
// against whether any test provokes one.
func checkControlTests(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	referenced, err := testedNames(worktree)
	if err != nil {
		return broke("the produced tests could not be read: "+err.Error(), nil)
	}

	// The gate is about declared paths, and this counted `if` and `case` nodes
	// in test files instead (PIPE-015). One `if err != nil` anywhere in any
	// test therefore satisfied a claim about every failure path in the
	// program, and adding a branch to the code could not make the stage fail.
	//
	// A path is declared by the function that takes the decision, so that is
	// the unit: every function with a branch must be reached by a test. It is
	// weaker than "each individual path has a test" and much stronger than
	// counting nodes, and what it cannot establish is stated rather than
	// implied.
	var (
		declaring []string
		examined  []string
		untested  []string
		branches  int
	)
	for _, function := range functions {
		if isTestScaffolding(function) || function.Branches == 0 {
			continue
		}
		branches += function.Branches
		declaring = append(declaring, function.Name)
		if referenced[function.Name] {
			examined = append(examined, function.Name)
			continue
		}
		untested = append(untested, function.Name)
	}
	sort.Strings(declaring)
	sort.Strings(examined)
	sort.Strings(untested)

	evidence := map[string]any{
		"branching_functions": declaring,
		"examined":            examined,
		"untested":            untested,
		"program_branches":    branches,
		"unit": "one branching function, not one individual path: a function " +
			"reached by a test may still have a path no test provokes",
	}
	if len(declaring) == 0 {
		return skippedWith(
			"the program takes no decision, so it declares no path to test",
			evidence)
	}
	if len(untested) > 0 {
		return broke(fmt.Sprintf(
			"%d function(s) take a decision that no test reaches: %s",
			len(untested), strings.Join(untested, ", ")), evidence)
	}
	return held(fmt.Sprintf(
		"every one of the %d branching function(s) is reached by a test, "+
			"covering %d decision point(s)", len(declaring), branches), evidence)
}

// checkMoleculeTests requires each composition to be named by a test.
func checkMoleculeTests(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	referenced, err := testedNames(worktree)
	if err != nil {
		return broke("the produced tests could not be read: "+err.Error(), nil)
	}
	_, molecules := atomsAndMolecules(functions)
	if len(molecules) == 0 {
		return skipped("the run composed nothing, so there is no composition to test")
	}
	// The same rule as the molecules stage, deliberately. These two asked the
	// same question with different thresholds — one passed if any composition
	// was tested, the other failed unless all were — so a single run could
	// report both that its compositions were tested and that they were not.
	// This stage asks whether the tests exist; the molecules stage asks whether
	// they hold. Both count a composition as covered on the same terms.
	var untested []string
	for _, molecule := range molecules {
		if molecule.Name == "main" || referenced[molecule.Name] {
			continue
		}
		untested = append(untested, molecule.Name)
	}
	sort.Strings(untested)
	evidence := map[string]any{
		"molecules": len(molecules), "untested": untested,
	}
	if len(untested) > 0 {
		return broke("no test names "+strings.Join(untested, ", ")+
			", so the composition it performs is unexamined", evidence)
	}
	return held(fmt.Sprintf("all %d composition(s) are named by a test",
		len(molecules)), evidence)
}
