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
func checkSimplification(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	// A function with many decision points is doing more than one thing. The
	// threshold is loose on purpose: the aim is to name the genuinely tangled,
	// not to have an opinion about every function.
	const tangled = 8
	var candidates []string
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		if function.Branches >= tangled {
			candidates = append(candidates, fmt.Sprintf(
				"%s (%d decision points)", function.Name, function.Branches))
		}
	}
	sort.Strings(candidates)
	evidence := map[string]any{
		"functions": len(functions), "worth_simplifying": candidates,
		"rewritten": false,
	}
	if len(functions) == 0 {
		return skipped("the run produced nothing to simplify")
	}
	if len(candidates) > 0 {
		return held(fmt.Sprintf(
			"%d function(s) are tangled enough to be worth simplifying: %s; "+
				"nothing was rewritten, because a rewrite is only safe once "+
				"the tests are known to detect a mistake",
			len(candidates), strings.Join(candidates, ", ")), evidence)
	}
	return held("no function is tangled enough to be worth rewriting", evidence)
}

// stateCompositionObligations says what each composition has to guarantee.
//
// A molecule's obligation is not that its parts work; it is that joining them
// produces the whole answer. Stating it explicitly is what lets a later reader
// tell a composition that was checked from one that merely compiled, since
// type compatibility alone is not evidence of safe composition.
func stateCompositionObligations(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	_, molecules := atomsAndMolecules(functions)
	if len(molecules) == 0 {
		return skipped("the run composed nothing, so it owes no composition obligation")
	}
	obligations := map[string]any{}
	for _, molecule := range molecules {
		obligations[molecule.Name] = map[string]any{
			"discharged_by": molecule.Calls,
			"guarantee": fmt.Sprintf(
				"the result of joining %s is the whole answer, not merely a "+
					"well-typed arrangement of the parts",
				describeCalls(molecule.Calls)),
		}
	}
	return held(fmt.Sprintf(
		"%d composition(s) each state what they guarantee and which functions "+
			"discharge it", len(molecules)),
		map[string]any{"obligations": obligations})
}

// stateControlObligations says what the program promises on every path.
func stateControlObligations(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	paths := 0
	for _, function := range functions {
		if !isTestScaffolding(function) {
			paths += function.Branches
		}
	}
	if paths == 0 {
		return skipped("the program takes no decision, so every run follows one path")
	}
	return held(fmt.Sprintf(
		"%d decision point(s) declared; each must terminate and report its "+
			"outcome rather than exit silently", paths),
		map[string]any{
			"decision_points": paths,
			"obligations": []string{
				"every path terminates",
				"every failure path reports rather than exits silently",
				"no path is unreachable",
			},
		})
}

// checkControlTests requires the failure paths to be examined, not just the
// happy one.
//
// A suite that only walks the intended path has checked the case least likely
// to be wrong. The program's branches are counted from the source and compared
// against whether any test provokes one.
func checkControlTests(worktree string) stageOutcome {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return broke("the produced tests could not be read: "+err.Error(), nil)
	}
	fileSet := token.NewFileSet()
	cases := 0
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		tree, parseErr := parser.ParseFile(
			fileSet, filepath.Join(worktree, file), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			continue
		}
		ast.Inspect(tree, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.IfStmt, *ast.CaseClause:
				cases++
			}
			return true
		})
	}
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	branches := 0
	for _, function := range functions {
		if !isTestScaffolding(function) {
			branches += function.Branches
		}
	}
	evidence := map[string]any{
		"program_branches": branches, "test_cases": cases,
	}
	if branches == 0 {
		return skipped("the program takes no decision, so it has no path to test")
	}
	if cases == 0 {
		return broke(fmt.Sprintf(
			"the program takes %d decision(s) and no test provokes any of them",
			branches), evidence)
	}
	return held(fmt.Sprintf(
		"%d test case(s) against %d program decision(s)", cases, branches),
		evidence)
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
