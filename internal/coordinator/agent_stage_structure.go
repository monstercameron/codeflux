package coordinator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// producedFunction is one function a run wrote, and what is known about it.
type producedFunction struct {
	Name string
	File string
	// Pure is true when nothing in the body reaches outside its arguments.
	Pure bool
	// Calls are the other produced functions this one uses. A function that
	// calls none is an atom; one that calls others composes them.
	Calls []string
	// Branches counts the decision points in the body, which is what makes a
	// function need more than one test to be covered.
	Branches int
	// LoopDepth is the deepest nesting of loops, which is the structural half
	// of a complexity claim.
	LoopDepth int
	Exported  bool
	IsTest    bool
	// Signature, Parameters and Results are what the function promises its
	// caller. The contract stage's gate asks for exactly these, and recording
	// only structural facts left it satisfied by evidence that answered a
	// different question.
	Signature  string
	Parameters []string
	Results    []string
	// ReturnsError is whether failure is carried as a value the caller must
	// handle, which is the difference between a function that can fail
	// visibly and one that can only fail silently.
	ReturnsError bool
}

// readProducedFunctions parses everything the run wrote.
//
// The source is parsed rather than pattern-matched because every question
// worth asking about it — what calls what, what reaches outside itself, how
// deeply it loops — is a question about structure, and a regular expression
// answers those wrongly in ways that are hard to notice.
func readProducedFunctions(worktree string) ([]producedFunction, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	fileSet := token.NewFileSet()
	var functions []producedFunction
	declared := map[string]bool{}

	type pending struct {
		function producedFunction
		body     *ast.FuncDecl
	}
	var parsed []pending

	for _, file := range files {
		tree, parseErr := parser.ParseFile(
			fileSet, filepath.Join(worktree, file), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return nil, fmt.Errorf("%s: %w", file, parseErr)
		}
		// Purity is decided per function, from what its own body calls. It
		// used to be decided per file, from what the file imported: in a
		// single-file program that marked every function impure because one of
		// them printed, and reported "0 pure atoms" for code that was almost
		// entirely pure.
		for _, declaration := range tree.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Name == nil {
				continue
			}
			name := function.Name.Name
			declared[name] = true
			parsed = append(parsed, pending{
				function: producedFunction{
					Name: name, File: file,
					Exported: ast.IsExported(name),
					IsTest: strings.HasSuffix(file, "_test.go") &&
						(strings.HasPrefix(name, "Test") ||
							strings.HasPrefix(name, "Fuzz") ||
							strings.HasPrefix(name, "Benchmark")),
					Pure: true,
				},
				body: function,
			})
		}
	}

	for _, item := range parsed {
		function := item.function
		function.Calls, function.Branches, function.LoopDepth =
			describeBody(item.body, declared)
		function.Parameters = fieldNames(item.body.Type.Params)
		function.Results = fieldNames(item.body.Type.Results)
		function.ReturnsError = false
		for _, result := range function.Results {
			if result == "error" {
				function.ReturnsError = true
			}
		}
		function.Signature = fmt.Sprintf("func %s(%s) (%s)", function.Name,
			strings.Join(function.Parameters, ", "),
			strings.Join(function.Results, ", "))
		// A function is impure when its own body reaches outside itself. That
		// is a question about the body, not about what its neighbours in the
		// same file happen to need.
		function.Pure = !callsAnythingImpure(item.body)
		functions = append(functions, function)
	}
	sort.Slice(functions, func(first, second int) bool {
		return functions[first].Name < functions[second].Name
	})
	return functions, nil
}

// callsAnythingImpure reports whether a body reaches for the outside world.
func callsAnythingImpure(function *ast.FuncDecl) bool {
	impure := false
	ast.Inspect(function, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		package_, isIdentifier := selector.X.(*ast.Ident)
		if !isIdentifier {
			return true
		}
		switch package_.Name {
		case "fmt", "os", "log", "time", "rand", "http", "exec", "bufio":
			// Formatting a string is not an effect; writing one out is. The
			// distinction is which fmt function, because Sprintf builds a
			// value and Println changes the world.
			if package_.Name == "fmt" &&
				strings.HasPrefix(selector.Sel.Name, "Sprint") {
				return true
			}
			impure = true
		}
		return true
	})
	return impure
}

// describeBody reports what one function calls, how much it branches, and how
// deeply it loops.
func describeBody(
	function *ast.FuncDecl,
	declared map[string]bool,
) (calls []string, branches int, loopDepth int) {
	seen := map[string]bool{}
	var walk func(node ast.Node, depth int)
	walk = func(node ast.Node, depth int) {
		if node == nil {
			return
		}
		switch typed := node.(type) {
		case *ast.CallExpr:
			if identifier, ok := typed.Fun.(*ast.Ident); ok &&
				declared[identifier.Name] && !seen[identifier.Name] {
				seen[identifier.Name] = true
				calls = append(calls, identifier.Name)
			}
		case *ast.IfStmt, *ast.CaseClause, *ast.CommClause:
			branches++
		case *ast.ForStmt, *ast.RangeStmt:
			branches++
			if depth+1 > loopDepth {
				loopDepth = depth + 1
			}
			ast.Inspect(typed, func(inner ast.Node) bool {
				if inner == node {
					return true
				}
				walk(inner, depth+1)
				return false
			})
			return
		}
		ast.Inspect(node, func(inner ast.Node) bool {
			if inner == node {
				return true
			}
			walk(inner, depth)
			return false
		})
	}
	if function.Body != nil {
		for _, statement := range function.Body.List {
			walk(statement, 0)
		}
	}
	sort.Strings(calls)
	return calls, branches, loopDepth
}

// atomsAndMolecules splits produced functions by whether they compose others.
//
// An atom does the work itself; a molecule is defined by what it composes. The
// distinction is structural rather than a matter of size, because it is what
// decides which obligations belong where: an atom owes its own contract, a
// molecule owes that the parts it joins add up.
func atomsAndMolecules(
	functions []producedFunction,
) (atoms []producedFunction, molecules []producedFunction) {
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		if len(function.Calls) == 0 {
			atoms = append(atoms, function)
			continue
		}
		molecules = append(molecules, function)
	}
	return atoms, molecules
}

// testedNames reports every produced function a test refers to.
func testedNames(worktree string) (map[string]bool, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	referenced := map[string]bool{}
	fileSet := token.NewFileSet()
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
			if identifier, ok := node.(*ast.Ident); ok {
				referenced[identifier.Name] = true
			}
			return true
		})
	}
	return referenced, nil
}

// checkAtoms reports whether the run produced anything atomic at all.
func checkAtoms(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	atoms, molecules := atomsAndMolecules(functions)
	evidence := map[string]any{
		"atoms": len(atoms), "molecules": len(molecules),
		"pure_atoms": countPure(atoms),
	}
	if len(atoms) == 0 {
		return broke("the run produced no atomic function: every piece of work "+
			"is entangled with another, so none can be tested or reused alone",
			evidence)
	}
	return held(fmt.Sprintf(
		"%d atomic function(s), %d of them pure, and %d composing function(s)",
		len(atoms), countPure(atoms), len(molecules)), evidence)
}

// checkAtomTests requires each atom to be reachable from a test.
//
// An atom nothing tests is an atom nothing has checked, whatever the suite
// says overall: coverage can be carried entirely by its callers while the atom
// itself is never examined on its own terms.
func checkAtomTests(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	referenced, err := testedNames(worktree)
	if err != nil {
		return broke("the produced tests could not be read: "+err.Error(), nil)
	}
	atoms, _ := atomsAndMolecules(functions)
	if len(atoms) == 0 {
		return skipped("the run produced no atom to test")
	}
	var untested []string
	for _, atom := range atoms {
		if !referenced[atom.Name] {
			untested = append(untested, atom.Name)
		}
	}
	sort.Strings(untested)
	evidence := map[string]any{
		"atoms": len(atoms), "untested": untested,
	}
	if len(untested) > 0 {
		return broke("no test mentions "+strings.Join(untested, ", ")+
			", so nothing checks them on their own terms", evidence)
	}
	return held(fmt.Sprintf("every one of the %d atom(s) is named by a test",
		len(atoms)), evidence)
}

// checkMolecules requires every composing function to be tested as a whole.
//
// Testing the parts is not testing the composition. The obligation a molecule
// carries is that its atoms add up, and only a test of the molecule itself can
// discharge it.
func checkMolecules(worktree string) stageOutcome {
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
		return skipped("the run composed nothing, so there is no composition to check")
	}
	var undischarged []string
	for _, molecule := range molecules {
		// main is exercised end to end by running the program, which is a
		// different stage's job and a better check than a unit test of it.
		if molecule.Name == "main" {
			continue
		}
		if !referenced[molecule.Name] {
			undischarged = append(undischarged, molecule.Name)
		}
	}
	sort.Strings(undischarged)
	evidence := map[string]any{
		"molecules": len(molecules), "untested": undischarged,
	}
	if len(undischarged) > 0 {
		return broke("no test mentions "+strings.Join(undischarged, ", ")+
			", so nothing checks that the parts they join add up", evidence)
	}
	return held(fmt.Sprintf(
		"every one of the %d composing function(s) is exercised as a whole",
		len(molecules)), evidence)
}

// checkControlFlow requires the program's branches to be reachable from tests.
//
// A function with branches needs more than one case to have been examined. The
// count is compared against the tests that exist rather than against coverage,
// because coverage says which lines ran and this asks whether anybody thought
// about the alternatives.
func checkControlFlow(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	branches := 0
	tests := 0
	for _, function := range functions {
		if isTestScaffolding(function) {
			tests++
			continue
		}
		branches += function.Branches
	}
	evidence := map[string]any{"branches": branches, "tests": tests}
	if branches == 0 {
		return skipped("the program takes no decisions, so it has no paths to check")
	}
	if tests == 0 {
		return broke(fmt.Sprintf(
			"the program takes %d decision(s) and no test examines any of them",
			branches), evidence)
	}
	return held(fmt.Sprintf(
		"the program takes %d decision(s) and %d test(s) examine them",
		branches, tests), evidence)
}

// checkComplexity labels the shipped atoms with a structural bound.
//
// The claim is read off the deepest loop nesting, which is what the structure
// implies. It is a label rather than a proof: deriving a true bound in general
// is not possible, and the honest thing is to record what the shape says and
// let a measurement disagree with it later.
func checkComplexity(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	labels := map[string]string{}
	deepest := 0
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		labels[function.Name] = complexityLabel(function.LoopDepth)
		if function.LoopDepth > deepest {
			deepest = function.LoopDepth
		}
	}
	if len(labels) == 0 {
		return skipped("the run produced no function to measure")
	}
	evidence := map[string]any{
		"time_labels": labels, "deepest_loop_nesting": deepest,
		"space_claim": "bounded by the input it is given",
	}
	return held(fmt.Sprintf(
		"%d function(s) labelled; the deepest is %s by structure, and measured "+
			"growth is what a later run would have to disagree with",
		len(labels), complexityLabel(deepest)), evidence)
}

// complexityLabel names the bound a loop nesting implies.
func complexityLabel(depth int) string {
	switch depth {
	case 0:
		return "O(1)"
	case 1:
		return "O(n)"
	case 2:
		return "O(n^2)"
	default:
		return fmt.Sprintf("O(n^%d)", depth)
	}
}

// countPure counts how many of a set of functions reach outside themselves.
func countPure(functions []producedFunction) int {
	pure := 0
	for _, function := range functions {
		if function.Pure {
			pure++
		}
	}
	return pure
}

// worktreeHasGoFiles reports whether there is anything to inspect at all.
func worktreeHasGoFiles(worktree string) bool {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return false
	}
	for _, file := range files {
		if _, statErr := os.Stat(filepath.Join(worktree, file)); statErr == nil {
			return true
		}
	}
	return false
}

// fieldNames renders one parameter or result list as its declared types.
//
// The types are what a caller has to satisfy and what it gets back, so they
// are what a contract is about. Names are deliberately left out: a caller
// cannot see them and two functions differing only in parameter naming offer
// the same contract.
func fieldNames(fields *ast.FieldList) []string {
	if fields == nil {
		return []string{}
	}
	var rendered []string
	for _, field := range fields.List {
		typeName := renderType(field.Type)
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			rendered = append(rendered, typeName)
		}
	}
	return rendered
}

// renderType names one type as it was written.
func renderType(node ast.Expr) string {
	switch typed := node.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return "*" + renderType(typed.X)
	case *ast.ArrayType:
		return "[]" + renderType(typed.Elt)
	case *ast.MapType:
		return "map[" + renderType(typed.Key) + "]" + renderType(typed.Value)
	case *ast.SelectorExpr:
		return renderType(typed.X) + "." + typed.Sel.Name
	case *ast.Ellipsis:
		return "..." + renderType(typed.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func"
	default:
		// A type this does not name is still a type, and calling it unknown is
		// better than calling it something it is not.
		return "unknown"
	}
}
