package coordinator

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// testsNaming maps each produced function to the tests that exercise it.
//
// Knowing that a function is tested somewhere is not enough to verify it: the
// question a unit's verification answers is whether *its own* tests pass, and
// that needs the tests attributed to the unit rather than a single verdict
// over the whole suite.
func testsNaming(worktree string) (map[string][]string, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	naming := map[string][]string{}
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
		for _, declaration := range tree.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Name == nil ||
				!strings.HasPrefix(function.Name.Name, "Test") {
				continue
			}
			testName := function.Name.Name
			seen := map[string]bool{}
			ast.Inspect(function, func(node ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if !isIdentifier || seen[identifier.Name] {
					return true
				}
				seen[identifier.Name] = true
				naming[identifier.Name] = append(naming[identifier.Name], testName)
				return true
			})
		}
	}
	for name := range naming {
		sort.Strings(naming[name])
	}
	return naming, nil
}

// unitVerdict is what one atom or molecule's own tests said about it.
type unitVerdict struct {
	Name   string
	Tests  []string
	Passed bool
	Detail string
}

// verifyEachUnit runs every unit's own tests and reports on each one.
//
// This replaces a single verdict over the whole suite. That verdict answered
// "did anything break", which blocks every downstream stage on one failure
// anywhere and never says which unit is at fault. A run that verifies each
// atom separately can say that nine of ten are proven and name the tenth,
// which is both more useful and more honest: the nine really were verified.
func verifyEachUnit(
	ctx context.Context,
	worktree string,
	units []producedFunction,
) []unitVerdict {
	naming, err := testsNaming(worktree)
	if err != nil {
		return nil
	}
	verdicts := make([]unitVerdict, 0, len(units))
	for _, unit := range units {
		// Only what the flow holds to account. A stub declared in a test file
		// is the checking apparatus, not the program, and demanding a test for
		// it made this stage accuse work no gate could ask about — which is
		// exactly the shape of failure the ledger exists to make impossible.
		if !needsVerification(unit) {
			continue
		}
		tests := naming[unit.Name]
		if len(tests) == 0 {
			verdicts = append(verdicts, unitVerdict{
				Name: unit.Name,
				Detail: "no test names it, so nothing has checked it does what " +
					"it was meant to",
			})
			continue
		}
		passed, detail := runNamedTests(ctx, worktree, tests)
		verdicts = append(verdicts, unitVerdict{
			Name: unit.Name, Tests: tests, Passed: passed, Detail: detail,
		})
	}
	return verdicts
}

// runNamedTests runs exactly the tests given and reports what happened.
func runNamedTests(
	ctx context.Context,
	worktree string,
	tests []string,
) (bool, string) {
	// An anchored alternation, so a test named TestParse does not drag in
	// TestParseRejectsGarbage and report its failure against the wrong unit.
	pattern := "^(" + strings.Join(tests, "|") + ")$"
	command := exec.CommandContext(ctx,
		"go", "test", "-count=1", "-run", pattern, "./...")
	command.Dir = worktree
	output, err := command.CombinedOutput()
	if err == nil {
		return true, fmt.Sprintf("%d test(s) pass: %s",
			len(tests), strings.Join(tests, ", "))
	}
	return false, failingLineOf(string(output))
}

// summariseUnits turns per-unit verdicts into one stage outcome.
func summariseUnits(
	kind string,
	verdicts []unitVerdict,
) stageOutcome {
	if len(verdicts) == 0 {
		return skipped("the run produced no " + kind + " to verify")
	}
	proven := map[string]any{}
	var unproven []string
	for _, verdict := range verdicts {
		if verdict.Passed {
			proven[verdict.Name] = verdict.Tests
			continue
		}
		unproven = append(unproven,
			fmt.Sprintf("%s (%s)", verdict.Name, verdict.Detail))
	}
	sort.Strings(unproven)
	evidence := map[string]any{
		kind + "s_verified": len(proven),
		kind + "s_total":    len(verdicts),
		"proven_by":         proven,
		"unproven":          unproven,
	}
	if len(unproven) > 0 {
		return broke(fmt.Sprintf(
			"%d of %d %s(s) are proven by their own tests; %s",
			len(proven), len(verdicts), kind, strings.Join(unproven, "; ")),
			evidence)
	}
	return held(fmt.Sprintf(
		"all %d %s(s) pass their own tests, each run on its own",
		len(verdicts), kind), evidence)
}

// checkAtomVerification proves each atom separately.
func checkAtomVerification(ctx context.Context, worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	atoms, _ := atomsAndMolecules(functions)
	return summariseUnits("atom", verifyEachUnit(ctx, worktree, atoms))
}

// checkMoleculeVerification proves each composition separately.
//
// A composition passing is a different claim from its parts passing: the
// obligation it carries is that joining them produces the whole answer, and
// only a test of the composition itself discharges that.
func checkMoleculeVerification(
	ctx context.Context,
	worktree string,
) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	_, molecules := atomsAndMolecules(functions)
	var testable []producedFunction
	for _, molecule := range molecules {
		// main is exercised by running the program, which is a later stage's
		// job and a better check than a unit test of it.
		if molecule.Name != "main" {
			testable = append(testable, molecule)
		}
	}
	return summariseUnits("molecule", verifyEachUnit(ctx, worktree, testable))
}

// checkFunctionCoverage reports how much of each function its tests reach.
//
// Package-level coverage lets a well-covered function hide one nothing
// touches. Per-function coverage names the one nothing touches, which is the
// only part of the number anybody can act on.
func checkFunctionCoverage(ctx context.Context, worktree string) stageOutcome {
	profile := filepath.Join(worktree, ".codeflux-coverage.out")
	command := exec.CommandContext(ctx,
		"go", "test", "-count=1", "-coverprofile="+profile, "./...")
	command.Dir = worktree
	if output, err := command.CombinedOutput(); err != nil {
		return broke("coverage could not be measured because the suite did "+
			"not pass: "+failingLineOf(string(output)), nil)
	}
	report := exec.CommandContext(ctx, "go", "tool", "cover", "-func="+profile)
	report.Dir = worktree
	output, err := report.CombinedOutput()
	if err != nil {
		return broke("the coverage profile could not be read: "+
			failingLineOf(string(output)), nil)
	}
	perFunction := map[string]any{}
	var untouched []string
	lowest := 100.0
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.HasSuffix(fields[len(fields)-1], "%") {
			continue
		}
		name := fields[len(fields)-2]
		percent := strings.TrimSuffix(fields[len(fields)-1], "%")
		value := parsePercent(percent)
		if name == "total:" || name == "" {
			continue
		}
		perFunction[name] = value
		// main is exercised by running the program, which the adversarial and
		// end-to-end stages do far better than a unit test of it could.
		// Demanding a unit test for it would push work toward testing the one
		// function whose job is to be untestable.
		if name == "main" {
			continue
		}
		if value == 0 {
			untouched = append(untouched, name)
		}
		if value < lowest {
			lowest = value
		}
	}
	sort.Strings(untouched)
	evidence := map[string]any{
		"per_function": perFunction, "least_covered_percent": lowest,
		"never_executed": untouched,
	}
	if len(untouched) > 0 {
		return broke(fmt.Sprintf(
			"%d function(s) are never executed by any test: %s",
			len(untouched), strings.Join(untouched, ", ")), evidence)
	}
	return held(fmt.Sprintf(
		"every function is executed by a test; the least covered reaches %.1f%%",
		lowest), evidence)
}

// parsePercent reads a coverage percentage, treating anything unreadable as
// uncovered rather than as complete.
func parsePercent(text string) float64 {
	var value float64
	if _, err := fmt.Sscanf(text, "%f", &value); err != nil {
		return 0
	}
	return value
}
