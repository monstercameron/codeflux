package coordinator

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// testsNaming maps each produced function to the tests that exercise it.
//
// Knowing that a function is tested somewhere is not enough to verify it: the
// question a unit's verification answers is whether *its own* tests pass, and
// that needs the tests attributed to the unit rather than a single verdict
// over the whole suite.
//
// It enumerates files through producedGoFiles' git-status view. An
// attribution-aware caller that already has the correct, base-revision file
// list should call testsNamingInFiles directly instead (PIPE-112).
func testsNaming(worktree string) (map[string][]string, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	return testsNamingInFiles(worktree, files)
}

// testsNamingInFiles is testsNaming restricted to exactly the given files.
//
// It used to collect every identifier appearing anywhere in a test
// function's body — a plain *ast.Ident sweep — so a produced function
// counted as named by a test the moment any identifier of that name turned
// up: a local variable, a struct-literal field key, a type used only in a
// declaration. checkAtomVerification (StageAtomVerification) reads this map
// to decide which tests to run for a unit, so a unit whose name coincided
// with a local variable was reported verified by a test that never called
// it at all.
//
// This is the second site of the defect PIPE-008 repairs at testedNames
// (agent_stage_structure.go) and is not reached by that fix, because the two
// functions are separate parses feeding separate stages. The repair is the
// same one, applied here: only call sites count now — a plain call by its
// identifier, and a method or package-qualified call by its selector.
//
// Deliberate strictness, matching PIPE-008's own: a function invoked
// indirectly — passed as a value to something that calls it later — is not
// counted. That makes the check stricter than the truth rather than looser,
// which is the safe direction for a stage whose whole claim is that a unit's
// own tests were run and passed.
func testsNamingInFiles(
	worktree string, files []string,
) (map[string][]string, error) {
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
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				var callee string
				switch target := call.Fun.(type) {
				case *ast.Ident:
					// A plain call: helper(...)
					callee = target.Name
				case *ast.SelectorExpr:
					// A method or package-qualified call: value.Method(...)
					// or package.Function(...). The selector is the name
					// that matches a produced function.
					callee = target.Sel.Name
				}
				if callee == "" || seen[callee] {
					return true
				}
				seen[callee] = true
				naming[callee] = append(naming[callee], testName)
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
		"go", "test", suiteTimeout, "-count=1", "-run", pattern, "./...")
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

// checkFunctionCoverage reports how much of what this run changed its tests
// reach.
//
// It used to report the least-covered function found anywhere `go test
// -coverprofile` touched — which is every function reachable from `go test
// ./...`, not only the ones this run wrote. On any repository with
// pre-existing code, that named a function the run never opened as the
// reason the gate failed, describing code the run is not answerable for
// (PIPE-115). Coverage is now measured over the changed lines attribution
// derives from the base revision (PIPE-111): the question this gate answers
// is "did a test exercise what this run wrote", not "does anything in this
// module have a gap".
//
// When attribution could not be established, the gate falls back to the
// prior whole-module, per-function measurement rather than reporting nothing
// — an attribution failure must fail toward examining more, not less
// (PIPE-111/PIPE-111a design caution).
func checkFunctionCoverage(
	ctx context.Context, worktree string, attribution changeAttribution,
) stageOutcome {
	profile := filepath.Join(worktree, ".codeflux-coverage.out")
	command := exec.CommandContext(ctx,
		"go", "test", suiteTimeout, "-count=1", "-coverprofile="+profile, "./...")
	command.Dir = worktree
	if output, err := command.CombinedOutput(); err != nil {
		return broke("coverage could not be measured because the suite did "+
			"not pass: "+failingLineOf(string(output)), nil)
	}

	if !attribution.Established {
		return wholeModuleFunctionCoverage(ctx, worktree, profile)
	}

	content, err := os.ReadFile(profile)
	if err != nil {
		return broke("the coverage profile could not be read: "+err.Error(), nil)
	}
	blocks := parseCoverageProfile(string(content))
	result := coverageOverChangedLines(blocks, attribution)
	evidence := map[string]any{
		"changed_coverable_lines": result.CoverableLines,
		"changed_covered_lines":   result.CoveredLines,
		"uncovered_changed_lines": result.Uncovered,
		"attribution_established": true,
		"scope":                   "changed lines only, per PIPE-115",
	}
	if result.CoverableLines == 0 {
		return skipped(
			"this run changed no line a test could exercise, so there is " +
				"nothing to measure coverage of")
	}
	percent := float64(result.CoveredLines) / float64(result.CoverableLines) * 100
	evidence["changed_line_coverage_percent"] = percent
	if len(result.Uncovered) > 0 {
		return broke(fmt.Sprintf(
			"%d of %d changed line(s) this run wrote are never executed by any "+
				"test (%.1f%% covered): %s",
			len(result.Uncovered), result.CoverableLines, percent,
			strings.Join(result.Uncovered, ", ")), evidence)
	}
	return held(fmt.Sprintf(
		"every one of the %d changed line(s) this run wrote is executed by a "+
			"test", result.CoverableLines), evidence)
}

// wholeModuleFunctionCoverage is the coverage measurement this stage used
// before attribution existed: least-covered function across the whole
// module. It survives only as the fallback for when attribution could not be
// established (PIPE-115's fail-toward-inclusion rule) — a run with no known
// base revision is still held to a coverage standard, just not a
// line-attributed one.
func wholeModuleFunctionCoverage(
	ctx context.Context, worktree, profile string,
) stageOutcome {
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
		"never_executed":          untouched,
		"attribution_established": false,
		"scope":                   "whole module: attribution could not be established",
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

// coverageBlock is one statement block read from a raw `go test
// -coverprofile` file: a contiguous span the compiler instrumented as one
// unit, and how many times it ran.
type coverageBlock struct {
	File       string
	StartLine  int
	EndLine    int
	Statements int
	Count      int
}

// coverageLinePattern matches one profile record:
// "path/to/file.go:startLine.startCol,endLine.endCol numStatements count".
var coverageLinePattern = regexp.MustCompile(
	`^(.+):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)$`)

// parseCoverageProfile reads every block out of a raw coverage profile's
// text, skipping the leading "mode:" line and anything it cannot parse
// rather than failing the whole read over one malformed record.
func parseCoverageProfile(content string) []coverageBlock {
	var blocks []coverageBlock
	for _, line := range strings.Split(content, "\n") {
		match := coverageLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		startLine, startErr := strconv.Atoi(match[2])
		endLine, endErr := strconv.Atoi(match[3])
		statements, statementsErr := strconv.Atoi(match[4])
		count, countErr := strconv.Atoi(match[5])
		if startErr != nil || endErr != nil || statementsErr != nil || countErr != nil {
			continue
		}
		blocks = append(blocks, coverageBlock{
			File: match[1], StartLine: startLine, EndLine: endLine,
			Statements: statements, Count: count,
		})
	}
	return blocks
}

// changedLineCoverageResult is what changed-line coverage found.
type changedLineCoverageResult struct {
	CoverableLines int
	CoveredLines   int
	// Uncovered names each changed, coverable, uncovered line as "file:line",
	// sorted, so a run is told exactly where to add a test rather than only
	// how much it is missing.
	Uncovered []string
}

// coverageOverChangedLines measures coverage restricted to the lines
// attribution says this run changed (PIPE-115).
//
// A line counts as coverable only when some block in the profile actually
// instruments it — a blank line, a comment, or a brace carries no statement
// and asking for its coverage would be asking for something that cannot be
// measured. A line covered by more than one block (a compound statement
// written on one line produces this) counts as covered if any block covering
// it ran at least once, since the question is whether the line executed at
// all, not how the compiler happened to divide it into blocks.
func coverageOverChangedLines(
	blocks []coverageBlock, attribution changeAttribution,
) changedLineCoverageResult {
	coverable := map[string]map[int]bool{}
	covered := map[string]map[int]bool{}
	for _, block := range blocks {
		for line := block.StartLine; line <= block.EndLine; line++ {
			if coverable[block.File] == nil {
				coverable[block.File] = map[int]bool{}
			}
			coverable[block.File][line] = true
			if block.Count > 0 {
				if covered[block.File] == nil {
					covered[block.File] = map[int]bool{}
				}
				covered[block.File][line] = true
			}
		}
	}

	var result changedLineCoverageResult
	var uncovered []string
	for file, ranges := range attribution.Ranges {
		profileFile := matchingProfileFile(coverable, file)
		if profileFile == "" {
			continue
		}
		for _, span := range ranges {
			for line := span.Start; line <= span.End; line++ {
				if !coverable[profileFile][line] {
					continue
				}
				result.CoverableLines++
				if covered[profileFile][line] {
					result.CoveredLines++
					continue
				}
				uncovered = append(uncovered, fmt.Sprintf("%s:%d", file, line))
			}
		}
	}
	sort.Strings(uncovered)
	result.Uncovered = uncovered
	return result
}

// matchingProfileFile finds the coverage profile's own key for a
// repository-relative path.
//
// The profile records each file by its Go import path — the module path
// joined with the path inside it — while attribution's ranges are keyed by
// the path Git reports, relative to the worktree root. The two agree on
// everything after the module path, so a suffix match finds the right file
// without needing to resolve the module path at all.
func matchingProfileFile(coverable map[string]map[int]bool, relativeFile string) string {
	for profileFile := range coverable {
		if profileFile == relativeFile ||
			strings.HasSuffix(profileFile, "/"+relativeFile) {
			return profileFile
		}
	}
	return ""
}
