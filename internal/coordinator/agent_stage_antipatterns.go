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

// antiPattern is code that compiles, passes its tests, and is still wrong.
//
// These are not style preferences. Each one is a shape that has a known way of
// going wrong later: a swallowed error becomes a wrong answer, a shadowed name
// becomes a reader's mistake, a package-level variable becomes a test that
// passes alone and fails in a suite. They are caught here because no test
// written against the current behaviour would ever fail on them.
type antiPattern struct {
	Where string
	// File and Line are Where's parts, kept apart rather than parsed back out
	// of it, so a caller can check a finding against a changed line range
	// (PIPE-113) without re-parsing the display string.
	File string
	Line int
	What string
	Why  string
}

// findingLocation is where one finder placed an antiPattern, before What and
// Why are known — split out so a finder can be handed the position and decide
// the message, without re-deriving the position itself.
type findingLocation struct {
	Where string
	File  string
	Line  int
}

// findAntiPatterns inspects everything the run produced.
//
// It enumerates files through producedGoFiles' git-status view. An
// attribution-aware caller that already has the correct, base-revision file
// list should call findAntiPatternsInFiles directly instead (PIPE-113),
// since producedGoFiles goes blind to anything this run has committed to its
// own worktree.
func findAntiPatterns(worktree string) ([]antiPattern, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	return findAntiPatternsInFiles(worktree, files)
}

// findAntiPatternsInFiles is findAntiPatterns restricted to exactly the
// given files.
func findAntiPatternsInFiles(worktree string, files []string) ([]antiPattern, error) {
	fileSet := token.NewFileSet()
	var found []antiPattern
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		tree, parseErr := parser.ParseFile(
			fileSet, filepath.Join(worktree, file), nil, parser.ParseComments)
		if parseErr != nil {
			continue
		}
		at := func(node ast.Node) findingLocation {
			line := fileSet.Position(node.Pos()).Line
			return findingLocation{
				Where: fmt.Sprintf("%s:%d", file, line), File: file, Line: line,
			}
		}
		found = append(found, findSwallowedErrors(tree, at)...)
		found = append(found, findMutableGlobals(tree, at)...)
		found = append(found, findUncheckedAssertions(tree, at)...)
		found = append(found, findPanicsOutsideMain(tree, at)...)
		found = append(found, findShadowedNames(tree, at)...)
		found = append(found, findUntypedParameters(tree, at)...)
		found = append(found, findFlagParameters(tree, at)...)
		found = append(found, findDeepNesting(tree, at)...)
	}
	sort.Slice(found, func(first, second int) bool {
		if found[first].Where != found[second].Where {
			return found[first].Where < found[second].Where
		}
		return found[first].What < found[second].What
	})
	return found, nil
}

// findSwallowedErrors reports failures that vanish where they are handled.
//
// This is the one the model produces most: a check that notices the error and
// then returns nothing, exits zero, or discards it with a blank identifier.
// The program then reports success for work it did not do, which is
// indistinguishable from success to everything downstream.
func findSwallowedErrors(
	tree *ast.File,
	at func(ast.Node) findingLocation,
) []antiPattern {
	var found []antiPattern
	ast.Inspect(tree, func(node ast.Node) bool {
		if branch, isBranch := node.(*ast.IfStmt); isBranch &&
			comparesAgainstNilError(branch.Cond) && branch.Body != nil {
			switch {
			case len(branch.Body.List) == 0:
				location := at(branch)
				found = append(found, antiPattern{
					Where: location.Where, File: location.File, Line: location.Line,
					What: "an error is noticed and the handler is empty",
					Why: "the failure is detected and then discarded, which is " +
						"worse than not checking: the code looks careful",
				})
			case isBareReturn(branch.Body.List):
				location := at(branch)
				found = append(found, antiPattern{
					Where: location.Where, File: location.File, Line: location.Line,
					What: "an error is noticed and the function returns without it",
					Why: "the caller cannot tell this apart from success, so a " +
						"failure becomes a wrong answer rather than a report",
				})
			}
		}
		// x, _ := f() where the discarded value is an error.
		if assign, isAssign := node.(*ast.AssignStmt); isAssign {
			for index, target := range assign.Lhs {
				name, isName := target.(*ast.Ident)
				if !isName || name.Name != "_" || index == 0 {
					continue
				}
				if len(assign.Rhs) == 1 {
					if _, isCall := assign.Rhs[0].(*ast.CallExpr); isCall {
						location := at(assign)
						found = append(found, antiPattern{
							Where: location.Where, File: location.File, Line: location.Line,
							What: "a returned value is discarded with _",
							Why: "if that value is an error, the only report of " +
								"a failure has been thrown away at the point it " +
								"was produced",
						})
					}
				}
			}
		}
		return true
	})
	return found
}

// comparesAgainstNilError reports whether a condition is an err != nil check.
func comparesAgainstNilError(condition ast.Expr) bool {
	binary, isBinary := condition.(*ast.BinaryExpr)
	if !isBinary || binary.Op != token.NEQ {
		return false
	}
	right, isName := binary.Y.(*ast.Ident)
	if !isName || right.Name != "nil" {
		return false
	}
	left, isLeftName := binary.X.(*ast.Ident)
	return isLeftName && strings.Contains(strings.ToLower(left.Name), "err")
}

// isBareReturn reports whether a body only returns, carrying nothing.
func isBareReturn(statements []ast.Stmt) bool {
	if len(statements) != 1 {
		return false
	}
	returned, isReturn := statements[0].(*ast.ReturnStmt)
	return isReturn && len(returned.Results) == 0
}

// findMutableGlobals reports package-level state anything can change.
//
// A test that passes alone and fails in a suite is almost always this. It also
// makes a function's result depend on what ran before it, which is the exact
// property the house style asks code not to have.
func findMutableGlobals(
	tree *ast.File,
	at func(ast.Node) findingLocation,
) []antiPattern {
	var found []antiPattern
	for _, declaration := range tree.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value, isValue := specification.(*ast.ValueSpec)
			if !isValue {
				continue
			}
			for _, name := range value.Names {
				// A sentinel is a package-level variable only because Go has no
				// constant of that type. errors.New at package scope, a
				// compiled pattern, a lookup table nothing writes to — these
				// are idiomatic and flagging them teaches people to ignore the
				// detector, which is how it stops catching the real thing.
				if declaresSentinel(value) {
					continue
				}
				location := at(name)
				found = append(found, antiPattern{
					Where: location.Where, File: location.File, Line: location.Line,
					What: "package-level variable " + name.Name,
					Why: "anything in the package can change it, so a function's " +
						"result depends on what ran before it; make it a " +
						"constant or pass it in",
				})
			}
		}
	}
	return found
}

// findUncheckedAssertions reports type assertions that panic on being wrong.
func findUncheckedAssertions(
	tree *ast.File,
	at func(ast.Node) findingLocation,
) []antiPattern {
	var found []antiPattern
	ast.Inspect(tree, func(node ast.Node) bool {
		// The comma-ok form appears as an assignment with two targets, so a
		// bare assertion used as an expression is the unchecked one.
		assign, isAssign := node.(*ast.AssignStmt)
		if !isAssign || len(assign.Lhs) >= 2 {
			return true
		}
		for _, value := range assign.Rhs {
			if assertion, isAssertion := value.(*ast.TypeAssertExpr); isAssertion &&
				assertion.Type != nil {
				location := at(assertion)
				found = append(found, antiPattern{
					Where: location.Where, File: location.File, Line: location.Line,
					What: "a type assertion with no second return value",
					Why: "it panics when the value is not that type, turning a " +
						"wrong assumption into a crash instead of a decision",
				})
			}
		}
		return true
	})
	return found
}

// findPanicsOutsideMain reports library code that takes the process down.
func findPanicsOutsideMain(
	tree *ast.File,
	at func(ast.Node) findingLocation,
) []antiPattern {
	var found []antiPattern
	for _, declaration := range tree.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Name == nil || function.Name.Name == "main" {
			continue
		}
		ast.Inspect(function, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			name, isName := call.Fun.(*ast.Ident)
			if isName && name.Name == "panic" {
				location := at(call)
				found = append(found, antiPattern{
					Where: location.Where, File: location.File, Line: location.Line,
					What: "panic inside " + function.Name.Name,
					Why: "a function that panics cannot be recovered from by its " +
						"caller; return an error and let the caller decide",
				})
			}
			return true
		})
	}
	return found
}

// findShadowedNames reports a local that hides a package-level name.
//
// It is legal and it makes a reader wrong: inside the loop, the name means
// something else than it does two lines above. This appeared in produced code
// as a loop variable named for the very type it ranged over.
func findShadowedNames(
	tree *ast.File,
	at func(ast.Node) findingLocation,
) []antiPattern {
	declared := map[string]bool{}
	for _, declaration := range tree.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if typed.Name != nil {
				declared[typed.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, specification := range typed.Specs {
				if spec, isType := specification.(*ast.TypeSpec); isType &&
					spec.Name != nil {
					declared[spec.Name.Name] = true
				}
			}
		}
	}
	var found []antiPattern
	ast.Inspect(tree, func(node ast.Node) bool {
		loop, isRange := node.(*ast.RangeStmt)
		if !isRange || loop.Value == nil {
			return true
		}
		name, isName := loop.Value.(*ast.Ident)
		if isName && declared[name.Name] {
			location := at(name)
			found = append(found, antiPattern{
				Where: location.Where, File: location.File, Line: location.Line,
				What: "the loop variable " + name.Name + " hides a name declared in this file",
				Why: "inside the loop that name means something else than it " +
					"does outside it, which is a reader's mistake waiting to happen",
			})
		}
		return true
	})
	return found
}

// findUntypedParameters reports signatures that give up on types.
func findUntypedParameters(
	tree *ast.File,
	at func(ast.Node) findingLocation,
) []antiPattern {
	var found []antiPattern
	for _, declaration := range tree.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Type.Params == nil {
			continue
		}
		for _, field := range function.Type.Params.List {
			rendered := renderType(field.Type)
			if rendered == "interface{}" || rendered == "any" {
				location := at(field)
				found = append(found, antiPattern{
					Where: location.Where, File: location.File, Line: location.Line,
					What: "a parameter typed " + rendered,
					Why: "the compiler can no longer say what a caller may pass, " +
						"so the mistake moves from build time to run time",
				})
			}
		}
	}
	return found
}

// findFlagParameters reports boolean arguments that make call sites unreadable.
func findFlagParameters(
	tree *ast.File,
	at func(ast.Node) findingLocation,
) []antiPattern {
	var found []antiPattern
	for _, declaration := range tree.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Type.Params == nil || function.Name == nil {
			continue
		}
		booleans := 0
		for _, field := range function.Type.Params.List {
			if renderType(field.Type) != "bool" {
				continue
			}
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			booleans += count
		}
		if booleans >= 2 {
			location := at(function)
			found = append(found, antiPattern{
				Where: location.Where, File: location.File, Line: location.Line,
				What: fmt.Sprintf("%s takes %d boolean parameters",
					function.Name.Name, booleans),
				Why: "a call site reads f(x, true, false) and says nothing about " +
					"what either flag means; take an option type or split the " +
					"function",
			})
		}
	}
	return found
}

// findDeepNesting reports bodies indented past the point of being followable.
func findDeepNesting(
	tree *ast.File,
	at func(ast.Node) findingLocation,
) []antiPattern {
	const tooDeep = 4
	var found []antiPattern
	for _, declaration := range tree.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Body == nil || function.Name == nil {
			continue
		}
		if depth := nestingDepth(function.Body, 0); depth > tooDeep {
			location := at(function)
			found = append(found, antiPattern{
				Where: location.Where, File: location.File, Line: location.Line,
				What: fmt.Sprintf("%s nests %d levels deep",
					function.Name.Name, depth),
				Why: "the condition that reaches the innermost statement can no " +
					"longer be held in mind; return early or split it out",
			})
		}
	}
	return found
}

// nestingDepth reports how deeply a body nests control flow.
func nestingDepth(node ast.Node, depth int) int {
	deepest := depth
	ast.Inspect(node, func(inner ast.Node) bool {
		if inner == node {
			return true
		}
		switch inner.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
			*ast.TypeSwitchStmt, *ast.SelectStmt:
			if found := nestingDepth(inner, depth+1); found > deepest {
				deepest = found
			}
			return false
		}
		return true
	})
	return deepest
}

// checkAntiPatterns is the stage.
//
// It used to fail the gate for any anti-pattern in any file the run touched
// at all, which held a run answerable for a shape somebody else wrote in the
// same file and never went near. attribution narrows that to the line the
// finding actually sits on (PIPE-113): a finding on a changed line is this
// run's problem and fails the gate; a finding on an untouched line, in a file
// the run otherwise changed, is recorded as context so it is not lost, but it
// no longer blocks a run for code it did not write.
func checkAntiPatterns(worktree string, attribution changeAttribution) stageOutcome {
	// Scanned from attribution's own file set when established, rather than
	// from producedGoFiles directly: producedGoFiles reads `git status`,
	// which only sees uncommitted edits and goes blind the moment a run
	// commits to its own worktree — silently finding nothing to report
	// instead of narrowing correctly (PIPE-111's design caution).
	files, err := attributionOrProducedFiles(worktree, attribution)
	if err != nil {
		return broke("the produced source could not be listed: "+err.Error(), nil)
	}
	found, err := findAntiPatternsInFiles(worktree, files)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	var attributed, preexisting []antiPattern
	for _, pattern := range found {
		if attribution.TouchesLine(pattern.File, pattern.Line) {
			attributed = append(attributed, pattern)
			continue
		}
		preexisting = append(preexisting, pattern)
	}
	describe := func(patterns []antiPattern) []string {
		listed := make([]string, 0, len(patterns))
		for _, pattern := range patterns {
			listed = append(listed,
				fmt.Sprintf("%s: %s", pattern.Where, pattern.What))
		}
		return listed
	}
	attributedListed := describe(attributed)
	preexistingListed := describe(preexisting)
	evidence := map[string]any{
		"findings": attributedListed, "count": len(attributed),
		"pre_existing_context":    preexistingListed,
		"pre_existing_count":      len(preexisting),
		"attribution_established": attribution.Established,
	}
	if len(attributed) == 0 {
		if len(preexisting) > 0 {
			return held(fmt.Sprintf(
				"no anti-pattern appears on a line this run changed; %d "+
					"pre-existing finding(s) elsewhere in the same file(s) are "+
					"recorded as context, not a gate failure: %s",
				len(preexisting), strings.Join(preexistingListed, "; ")), evidence)
		}
		return held("no known anti-pattern appears in the produced source",
			evidence)
	}
	return broke(fmt.Sprintf("%d anti-pattern(s) on a line this run changed: %s",
		len(attributed), strings.Join(attributedListed, "; ")), evidence)
}

// declaresSentinel reports whether a package-level var is one Go has no better
// way to express.
//
// A sentinel error, a compiled pattern, a table built once and only read: each
// is a constant in everything but the type system's opinion. Treating them as
// mutable state would flag idiomatic code on every run.
func declaresSentinel(value *ast.ValueSpec) bool {
	if len(value.Values) == 0 {
		return false
	}
	for _, expression := range value.Values {
		call, isCall := expression.(*ast.CallExpr)
		if !isCall {
			// A composite literal assigned once and never written is the table
			// case. Whether it is written is beyond what this can see, so the
			// name carries the claim: an exported or capitalised name reads as
			// a declared constant.
			if _, isComposite := expression.(*ast.CompositeLit); isComposite &&
				len(value.Names) > 0 && ast.IsExported(value.Names[0].Name) {
				continue
			}
			return false
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if !isSelector {
			return false
		}
		switch selector.Sel.Name {
		case "New", "Errorf", "MustCompile":
		default:
			return false
		}
	}
	return true
}
