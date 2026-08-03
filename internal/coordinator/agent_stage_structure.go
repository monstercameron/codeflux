package coordinator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// producedFunction is one function a run wrote, and what is known about it.
type producedFunction struct {
	Name string
	File string
	// Pure is true when nothing in the body reaches outside its arguments.
	Pure bool
	// Effects names what the body reaches for outside its own arguments,
	// resolved by observedEffects (PIPE-139): the file's own imports, not a
	// bare identifier, decide what package a selector call reaches, and only
	// a curated set of names within that package count as effectful, so a
	// type conversion sharing a package with an effect (time.Duration versus
	// time.Now) is not confused with one. Pure is exactly len(Effects)==0.
	Effects []string
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
	// StartLine and EndLine bound the declaration in its file, in the
	// worktree's current content — StartLine from the doc comment when one is
	// attached, so an edit to only the comment still falls inside the span.
	// This is what change attribution (PIPE-111a) maps a changed line range
	// onto: a declaration is attributed to a run when some changed line falls
	// anywhere in [StartLine, EndLine].
	StartLine int
	EndLine   int
}

// readProducedFunctions parses everything the run wrote.
//
// The source is parsed rather than pattern-matched because every question
// worth asking about it — what calls what, what reaches outside itself, how
// deeply it loops — is a question about structure, and a regular expression
// answers those wrongly in ways that are hard to notice.
//
// It enumerates files through producedGoFiles, which reads `git status` —
// uncommitted edits only. That is the correct, unchanged behaviour for every
// caller outside this pipeline's attribution machinery. A caller that already
// knows the attributed file list (PIPE-111) must call parseProducedFunctions
// directly with that list instead, or it inherits producedGoFiles' blindness
// to a run that has committed to its own worktree.
func readProducedFunctions(worktree string) ([]producedFunction, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	return parseProducedFunctions(worktree, files)
}

// parseProducedFunctions parses declarations from exactly the given files,
// which readProducedFunctions factors out so an attribution-aware caller can
// supply its own, more accurate file list (PIPE-111/PIPE-111a) instead of
// producedGoFiles' git-status view.
func parseProducedFunctions(
	worktree string, files []string,
) ([]producedFunction, error) {
	fileSet := token.NewFileSet()
	var functions []producedFunction
	declared := map[string]bool{}
	// fileImports resolves, per file, the package each import name actually
	// binds to (PIPE-139), so a purity determination is checked against what
	// the file imported rather than trusted from a bare identifier that a
	// renamed import or a shadowing local name would make wrong in either
	// direction.
	fileImports := map[string]map[string]string{}

	type pending struct {
		function producedFunction
		body     *ast.FuncDecl
	}
	var parsed []pending

	for _, file := range files {
		// parser.ParseComments is required here, not only in documentedNames'
		// own separate parse: a declaration's span has to start at its doc
		// comment, when one is attached, or an edit that only changes the
		// comment would fall outside the declaration attribution is computed
		// against (PIPE-111a).
		tree, parseErr := parser.ParseFile(
			fileSet, filepath.Join(worktree, file), nil,
			parser.SkipObjectResolution|parser.ParseComments)
		if parseErr != nil {
			return nil, fmt.Errorf("%s: %w", file, parseErr)
		}
		fileImports[file] = importMap(tree)
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
		//
		// observedEffects (PIPE-139) is used here rather than the older,
		// coarser callsAnythingImpure: it resolves a call's package from the
		// file's own imports instead of a bare identifier, and matches a
		// curated set of effectful names within a package instead of treating
		// every call into it as one, so it does not confuse a type
		// conversion (time.Duration(n)) with an effect (time.Now()).
		// callsAnythingImpure itself is left as it was for its other, unowned
		// callers (agent_stage_recall.go, code_collection_application.go);
		// this is a new, additional function, not a signature change to it.
		function.Effects = observedEffects(item.body, fileImports[item.function.File])
		function.Pure = len(function.Effects) == 0
		declarationStart := item.body.Pos()
		if item.body.Doc != nil {
			declarationStart = item.body.Doc.Pos()
		}
		function.StartLine = fileSet.Position(declarationStart).Line
		function.EndLine = fileSet.Position(item.body.End()).Line
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

// importMap resolves each name a file's own import declarations bind to the
// import path it names (PIPE-139).
//
// A purity check that trusts a call's bare identifier text is wrong in two
// directions at once: a renamed import ("import ios \"os\"") reaches the
// operating system under a name no fixed list of package identifiers
// contains, and a local variable or parameter that happens to share a
// package's conventional name is not that package at all. Resolving through
// the file's own imports fixes both, because only a name this file actually
// imported appears in the returned map.
func importMap(file *ast.File) map[string]string {
	imports := map[string]string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path
		if slash := strings.LastIndex(path, "/"); slash >= 0 {
			name = path[slash+1:]
		}
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" || name == "." {
			// A blank import is never called by name, and a dot import puts
			// its names directly in scope with no selector at all — neither
			// shape is a `pkg.Name` call this resolves.
			continue
		}
		imports[name] = path
	}
	return imports
}

// effectPackages are the import paths this check knows reach outside the
// program: a file, the clock, randomness, another process, the network, or a
// log. Membership is keyed by import path, resolved through importMap
// (PIPE-139), not by the identifier a call happens to be written with.
var effectPackages = map[string]bool{
	"fmt": true, "os": true, "log": true, "time": true,
	"math/rand": true, "net/http": true, "os/exec": true, "bufio": true,
}

// pureConversionTypes names, per import path, the identifiers that package
// exports as types rather than functions, so a call-shaped type conversion
// into that package — time.Duration(n) is the case this ticket names
// (PIPE-139) — is not read as a call to the package's clock, its file, or
// whatever else shares its import path. This is a curated list rather than a
// type-checker's answer: distinguishing a conversion from a call in general
// needs full type information this check does not load, and a short,
// reviewable list of known type names is the bounded alternative that fixes
// the named case without pretending to a precision this check does not have.
var pureConversionTypes = map[string]map[string]bool{
	"time": {"Duration": true, "Month": true, "Weekday": true},
}

// observedEffects names what a function's body actually reaches for outside
// its own arguments (PIPE-139), replacing a blanket "this package is an
// effect" reading with two narrower ones: which package a selector call
// really names, resolved through the file's own imports rather than trusted
// from a bare identifier (importMap); and which names within that package are
// effectful, resolved through a curated list rather than treated as every
// exported name in it (effectPackages, pureConversionTypes).
//
// Stated limit: a call through a value this cannot resolve to an import —
// a method on a local variable, a struct field, or an injected interface —
// is not examined here at all, so an effect reached only that way is
// invisible to this check. Widening the rule to flag every such call was
// tried and rejected: an ordinary local value most produced code holds
// (a strings.Builder, a sync.WaitGroup, an error) is called through exactly
// the same shape, and flagging all of them as impure would have traded this
// false negative for a much larger false positive across ordinary code. That
// gap is real and is left for a caller with actual type information to close.
func observedEffects(function *ast.FuncDecl, imports map[string]string) []string {
	var effects []string
	seen := map[string]bool{}
	ast.Inspect(function, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		base, isIdentifier := selector.X.(*ast.Ident)
		if !isIdentifier {
			return true
		}
		path, isImported := imports[base.Name]
		if !isImported || !effectPackages[path] {
			return true
		}
		if pureConversionTypes[path][selector.Sel.Name] {
			// A type conversion, not a call: time.Duration(n) shares an
			// import path with time.Now() and nothing else.
			return true
		}
		if path == "fmt" && strings.HasPrefix(selector.Sel.Name, "Sprint") {
			// Formatting a string is not an effect; writing one out is. The
			// distinction is which fmt function, because Sprintf builds a
			// value and Println changes the world.
			return true
		}
		name := path + "." + selector.Sel.Name
		if !seen[name] {
			seen[name] = true
			effects = append(effects, name)
		}
		return true
	})
	sort.Strings(effects)
	return effects
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
			// A call reaches another produced function two ways, and only the
			// first was counted (PIPE-009). A program composed entirely
			// through methods therefore showed zero calls and was classified
			// as atomic, and every phase B and C gate rests on that split.
			//
			// declared holds every produced FuncDecl name, methods included,
			// so a selector is matched on its name. A package-qualified call
			// whose function happens to share a produced name counts too; that
			// errs toward finding a call, which is the direction that avoids
			// calling a composed program atomic.
			var callee string
			switch target := typed.Fun.(type) {
			case *ast.Ident:
				callee = target.Name
			case *ast.SelectorExpr:
				callee = target.Sel.Name
			}
			if callee != "" && declared[callee] && !seen[callee] {
				seen[callee] = true
				calls = append(calls, callee)
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

// testedNames reports every produced function a test actually calls
// (PIPE-008).
//
// It collected every identifier in every test file, so an atom counted as
// tested when any identifier of that name appeared anywhere in a test: a local
// variable, a field, a type, a struct literal key. A gate that says "this was
// tested" was satisfiable by coincidence.
//
// Only call sites count now: a plain call, and a method or package-qualified
// call by its selector. A function invoked indirectly — passed as a value to
// something that calls it later — is deliberately not counted. That makes the
// check stricter than the truth rather than looser, which is the safe
// direction for a gate whose whole claim is that something was examined.
func testedNames(worktree string) (map[string]bool, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	return testedNamesInFiles(worktree, files)
}

// testedNamesInFiles is testedNames restricted to exactly the given files,
// factored out the same way parseProducedFunctions is (PIPE-111/PIPE-057) so
// a caller that already holds the produced-file list from elsewhere in the
// same pass does not have to pay for producedGoFiles' `git status` a second
// time to get it again.
func testedNamesInFiles(worktree string, files []string) (map[string]bool, error) {
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
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				// A plain call: helper(...)
				referenced[callee.Name] = true
			case *ast.SelectorExpr:
				// A method or package-qualified call: value.Method(...) or
				// package.Function(...). The selector is the name that matches
				// a produced function.
				referenced[callee.Sel.Name] = true
			}
			return true
		})
	}
	return referenced, nil
}

// producedFunctionCache memoizes the produced-file list and the parsed
// function tree for the span of one stage-examination pass over one fixed
// worktree state (PIPE-057).
//
// A single instance is created at the top of examineStructure and used only
// by the checks that pass — checkAtoms, checkAtomTests, checkMolecules, and
// checkAtomDocumentation — which run consecutively, before any stage that
// executes repository code or otherwise writes to the worktree, and which
// therefore all see the same produced-file list and the same parsed tree.
// Ledger recording between them (ledger.decide) commits verdicts to the
// run's own storage, not to the worktree, so it cannot invalidate anything
// held here.
//
// It must never be created once and held across an attempt boundary, or
// across the boundary between outstandingWork's own call and this run's
// later call to examineStructure: the agent writes files to its worktree
// between attempts, so a cache instance surviving either boundary would
// answer questions about a worktree that no longer exists. A fresh instance
// belongs at the top of each call that needs one, and nowhere else.
//
// It caches the parsed tree by the exact file list requested, not by the
// worktree alone, which is what keeps a producedGoFiles-derived list and an
// attribution-derived list from silently sharing one entry: the two can
// legitimately disagree once a run has committed to its own worktree
// (PIPE-111's design caution), and a cache keyed only on the worktree would
// answer one question with the other's cached result.
//
// It is safe for concurrent use by every check that shares one instance
// (PIPE-058a/PIPE-059): checkAtoms, checkAtomTests, checkMolecules, and
// checkAtomDocumentation are all classified pure-ast, which is exactly the
// resource class the scheduler is free to run side by side within one wave,
// and every one of them reaches this cache. Before PIPE-058a this was safe
// by construction — the four calls that share one cache ran one after
// another, so nothing here needed a lock — but a plain map read racing a
// plain map write is a Go runtime crash, not merely a wrong answer, the
// moment two of those goroutines reach functionsFor (or either haveTested-
// Names/haveDocumentedNames path) for the same cache at once. mutex is a
// single, coarse lock over every field rather than one per map: the work
// this cache does per call is a `git status` shell-out and one parse of
// already-read source, so serializing the whole cache costs nothing next to
// that, and a coarse lock cannot deadlock the way per-field locking
// acquired in different orders could.
type producedFunctionCache struct {
	worktree string

	mutex sync.Mutex

	haveProducedFiles bool
	producedFiles     []string
	producedFilesErr  error

	parsed   map[string][]producedFunction
	parseErr map[string]error

	haveTestedNames bool
	testedNames     map[string]bool
	testedNamesErr  error

	haveDocumentedNames bool
	documentedNames     map[string]bool
	documentedNamesErr  error
}

// newProducedFunctionCache creates an empty cache bound to one worktree. See
// producedFunctionCache's own doc for the lifetime rule that makes this safe.
func newProducedFunctionCache(worktree string) *producedFunctionCache {
	return &producedFunctionCache{worktree: worktree}
}

// producedFilesList is producedGoFiles' `git status` view, shelled out at
// most once for this cache's lifetime.
func (cache *producedFunctionCache) producedFilesList() ([]string, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if !cache.haveProducedFiles {
		cache.producedFiles, cache.producedFilesErr = producedGoFiles(cache.worktree)
		cache.haveProducedFiles = true
	}
	return cache.producedFiles, cache.producedFilesErr
}

// functionsFor parses exactly the given files, once, and returns the same
// result to every later caller in this cache's lifetime that asks for the
// identical file set — which every check sharing one cache does, because
// they all resolve their file list the same way within one static pass.
func (cache *producedFunctionCache) functionsFor(
	files []string,
) ([]producedFunction, error) {
	key := producedFunctionCacheKey(files)
	cache.mutex.Lock()
	if cache.parsed == nil {
		cache.parsed = map[string][]producedFunction{}
		cache.parseErr = map[string]error{}
	}
	if functions, cached := cache.parsed[key]; cached {
		err := cache.parseErr[key]
		cache.mutex.Unlock()
		return functions, err
	}
	cache.mutex.Unlock()
	// Parsing runs outside the lock: it only reads the worktree's files, not
	// this cache's own state, so two goroutines racing to fill different
	// (or even the same) key parse concurrently rather than queuing behind
	// one another. The lock retaken below to store the result is what keeps
	// the eventual map write itself safe; a duplicate parse on a same-key
	// race is wasted work, not a correctness problem, and it is the losing
	// goroutine's own answer that gets discarded, never a caller's.
	functions, err := parseProducedFunctions(cache.worktree, files)
	cache.mutex.Lock()
	cache.parsed[key] = functions
	cache.parseErr[key] = err
	cache.mutex.Unlock()
	return functions, err
}

// readProducedFunctions is readProducedFunctions, memoized for this cache's
// lifetime: the same producedGoFiles file list and the same parse are
// reused by every caller that asks through this cache instead of through
// the bare package function.
func (cache *producedFunctionCache) readProducedFunctions() ([]producedFunction, error) {
	files, err := cache.producedFilesList()
	if err != nil {
		return nil, err
	}
	return cache.functionsFor(files)
}

// testedNamesCached is testedNames, memoized for this cache's lifetime.
func (cache *producedFunctionCache) testedNamesCached() (map[string]bool, error) {
	files, err := cache.producedFilesList()
	if err != nil {
		return nil, err
	}
	cache.mutex.Lock()
	if cache.haveTestedNames {
		names, namesErr := cache.testedNames, cache.testedNamesErr
		cache.mutex.Unlock()
		return names, namesErr
	}
	cache.mutex.Unlock()
	// Computed outside the lock, the same as functionsFor: a same-key race
	// costs a duplicate computation, never a wrong answer, and the loser's
	// result is simply discarded below.
	names, namesErr := testedNamesInFiles(cache.worktree, files)
	cache.mutex.Lock()
	if !cache.haveTestedNames {
		cache.testedNames, cache.testedNamesErr = names, namesErr
		cache.haveTestedNames = true
	}
	names, namesErr = cache.testedNames, cache.testedNamesErr
	cache.mutex.Unlock()
	return names, namesErr
}

// documentedNamesCached is documentedNames, memoized for this cache's
// lifetime and sharing its producedFilesList rather than shelling out to
// `git status` again to get the same list documentedNames would have asked
// for itself.
func (cache *producedFunctionCache) documentedNamesCached() (map[string]bool, error) {
	files, err := cache.producedFilesList()
	if err != nil {
		return nil, err
	}
	cache.mutex.Lock()
	if cache.haveDocumentedNames {
		names, namesErr := cache.documentedNames, cache.documentedNamesErr
		cache.mutex.Unlock()
		return names, namesErr
	}
	cache.mutex.Unlock()
	names, namesErr := documentedNamesInFiles(cache.worktree, files)
	cache.mutex.Lock()
	if !cache.haveDocumentedNames {
		cache.documentedNames, cache.documentedNamesErr = names, namesErr
		cache.haveDocumentedNames = true
	}
	names, namesErr = cache.documentedNames, cache.documentedNamesErr
	cache.mutex.Unlock()
	return names, namesErr
}

// producedFunctionCacheKey canonicalizes a file list into a cache key. The
// list is sorted before joining because the same set of files requested in
// a different order is still the same parse, and every producer of a file
// list here (producedGoFiles, attributionFiles) is already sorted in
// practice — the sort is a correctness guarantee against that changing
// silently, not a workaround for a known case that needs it today.
func producedFunctionCacheKey(files []string) string {
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	return strings.Join(sorted, "\x1f")
}

// checkAtoms reports whether the run produced anything atomic at all, and —
// PIPE-138 — enforces purity where a contract declares it.
//
// The flow's own gate for this stage says an atom "reads nothing outside its
// arguments", and until this ticket nothing checked that: the stage counted
// how many atoms happened to be pure and never failed on one that was not.
// This compares each atom's declared contract — the //codeflux:atom doc
// comment PIPE-137's contracts stage now reads from, the same source, not
// re-derived here — against what it was actually observed to do
// (producedFunction.Effects, PIPE-139). An atom that declares "Effects: -
// None: pure atom" and reaches outside itself anyway is a gate failure, named
// with what it reaches for; an atom with no declaration, or one that declares
// an effect, is not judged here — there is nothing declared for it to
// contradict.
func checkAtoms(worktree string, cache *producedFunctionCache) stageOutcome {
	functions, err := cache.readProducedFunctions()
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

	files, err := cache.producedFilesList()
	if err != nil {
		return broke("the produced source could not be read: "+err.Error(), nil)
	}
	declared, err := declaredContracts(worktree, files)
	if err != nil {
		return broke("the declared atom documentation could not be read: "+
			err.Error(), nil)
	}
	var violations []string
	declaredPureCount := 0
	for _, atom := range atoms {
		document, hasDeclaration := declared[atom.Name]
		if !hasDeclaration || !declaredPurity(document) {
			continue
		}
		declaredPureCount++
		if !atom.Pure {
			violations = append(violations, fmt.Sprintf(
				"%s declares \"Effects: None: pure atom\" but reaches outside "+
					"its arguments: %s", atom.Name, strings.Join(atom.Effects, ", ")))
		}
	}
	sort.Strings(violations)
	evidence["declared_pure_atoms"] = declaredPureCount
	if len(violations) > 0 {
		evidence["purity_violations"] = violations
		return broke(fmt.Sprintf(
			"%d atom(s) declare purity their own body contradicts: %s",
			len(violations), strings.Join(violations, "; ")), evidence)
	}
	return held(fmt.Sprintf(
		"%d atomic function(s), %d of them pure, and %d composing function(s); "+
			"%d atom(s) declare purity and none of them contradicts it",
		len(atoms), countPure(atoms), len(molecules), declaredPureCount), evidence)
}

// checkAtomTests requires each atom to be reachable from a test.
//
// An atom nothing tests is an atom nothing has checked, whatever the suite
// says overall: coverage can be carried entirely by its callers while the atom
// itself is never examined on its own terms.
func checkAtomTests(worktree string, cache *producedFunctionCache) stageOutcome {
	functions, err := cache.readProducedFunctions()
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	referenced, err := cache.testedNamesCached()
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
func checkMolecules(worktree string, cache *producedFunctionCache) stageOutcome {
	functions, err := cache.readProducedFunctions()
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	referenced, err := cache.testedNamesCached()
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
//
// Labels are reported for the declarations this run is answerable for
// (PIPE-113): a pre-existing function nobody touched carries a bound this run
// did not derive and should not be advertised as this run's evidence, even as
// a label. scope fails toward inclusion when attribution could not be
// established, so a run whose attribution failed to compute sees the old,
// whole-worktree behaviour rather than a silently narrowed one.
func checkComplexity(worktree string, attribution changeAttribution) stageOutcome {
	// Enumerated from attribution's own file set, not producedGoFiles'
	// git-status view: a run that has committed to its own worktree leaves
	// git status clean, and readProducedFunctions would silently find
	// nothing rather than correctly narrowing to what changed (PIPE-111's
	// design caution).
	functions, err := attributedFunctions(worktree, attribution)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	scope := attributeDeclarations(functions, attribution)
	labels := map[string]string{}
	deepest := 0
	touched := 0
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		touched++
		if !scope.Contains(function.Name) {
			continue
		}
		labels[function.Name] = complexityLabel(function.LoopDepth)
		if function.LoopDepth > deepest {
			deepest = function.LoopDepth
		}
	}
	if touched == 0 {
		return skipped("the run produced no function to measure")
	}
	if len(labels) == 0 {
		// The run produced functions, but none of them is one this run is
		// answerable for — every candidate is pre-existing code in a touched
		// file. That is a legitimate zero (PIPE-124's shape, applied here),
		// distinct from having nothing to measure at all.
		return skipped(
			"none of the produced functions is a declaration this run changed, " +
				"so there is nothing this run is answerable for measuring")
	}
	// The gate requires a bound that measured growth across input sizes agrees
	// with. Nothing here runs the produced code at two sizes and compares, so
	// the stage may not report satisfied: a structural label is a reading of
	// the source, not a measurement of the program (PIPE-011).
	//
	// The space claim is gone. "bounded by the input it is given" was asserted
	// for every function without anything having looked at allocation, which
	// made it a claim about memory derived from loop nesting.
	//
	// The label's own limits are recorded beside it, because a label that is
	// wrong in a named way is usable while an unqualified one is not: it is
	// read from loop nesting, so recursion and a call to a library sort are
	// both labelled O(1).
	evidence := map[string]any{
		"time_labels":          labels,
		"deepest_loop_nesting": deepest,
		"label_source":         "loop nesting in the produced source",
		"label_limits": "recursion and calls into other functions are not " +
			"followed, so a recursive function and a call to a library sort " +
			"are both labelled O(1)",
		"growth_measured": false,
	}
	return skippedWith(fmt.Sprintf(
		"no growth measurement is performed by this build, so the structural "+
			"label is unconfirmed; %d function(s) labelled from loop nesting, "+
			"the deepest being %s",
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
