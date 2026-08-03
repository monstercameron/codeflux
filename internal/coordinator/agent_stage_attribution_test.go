package coordinator

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// newAttributionFixture creates a Git repository, commits the given files as
// its base revision, and returns the worktree path together with the base
// revision's full object ID — the exact reference point PIPE-111 measures
// changed line ranges against.
func newAttributionFixture(
	t *testing.T, baseFiles map[string]string,
) (worktree, base string) {
	t.Helper()
	worktree = t.TempDir()
	run := func(arguments ...string) string {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", arguments...)
		command.Dir = worktree
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "--initial-branch=main")
	all := map[string]string{
		"go.mod": "module codeflux.test/attribution\n\ngo 1.26.0\n",
	}
	for path, content := range baseFiles {
		all[path] = content
	}
	for path, content := range all {
		full := filepath.Join(worktree, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		run("add", path)
	}
	run("-c", "user.name=Codeflux Test", "-c", "user.email=codeflux@example.invalid",
		"commit", "-m", "base")
	base = run("rev-parse", "HEAD")
	return worktree, base
}

// writeAttributionFile writes path into an already-created attribution
// fixture, simulating one edit a run makes. commit controls whether the edit
// is committed to the worktree's own branch — a run committing to its own
// worktree, which is exactly what PIPE-111's design caution says the base
// revision must still be measured correctly against — or left as an ordinary
// uncommitted edit. Attribution has to read both the same way.
func writeAttributionFile(
	t *testing.T, worktree, path, content string, commit bool,
) {
	t.Helper()
	full := filepath.Join(worktree, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if !commit {
		return
	}
	command := exec.CommandContext(t.Context(), "git", "add", path)
	command.Dir = worktree
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add %s: %v\n%s", path, err, output)
	}
	commitCommand := exec.CommandContext(t.Context(),
		"git", "-c", "user.name=Codeflux Test",
		"-c", "user.email=codeflux@example.invalid",
		"commit", "-m", "run edit")
	commitCommand.Dir = worktree
	if output, err := commitCommand.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
}

// TestPIPE111_ChangedLineRangesComeFromTheBaseRevisionNotHEAD covers PIPE-111.
//
// A single-revision `git diff <base>` compares the base against the current
// working tree, which folds a committed-since-base edit and an uncommitted
// one together. Proving both are attributed is what shows this does not
// silently miss the committed case, which a diff against HEAD alone would:
// HEAD moves every time a run commits to its own worktree, so a diff against
// HEAD would show nothing for anything already committed.
func TestPIPE111_ChangedLineRangesComeFromTheBaseRevisionNotHEAD(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\n" +
			"func Old(value int) int {\n" +
			"\treturn value\n" +
			"}\n\n" +
			"func Untouched(value int) int {\n" +
			"\treturn value * 2\n" +
			"}\n",
	})

	// Committed: a run committing to its own worktree.
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"func Old(value int) int {\n"+
		"\treturn value + 1\n"+
		"}\n\n"+
		"func Untouched(value int) int {\n"+
		"\treturn value * 2\n"+
		"}\n", true)

	// Uncommitted: a brand-new untracked file, still in progress.
	writeAttributionFile(t, worktree, "new.go",
		"package lib\n\nfunc New(value int) int {\n\treturn value\n}\n", false)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established against a real base revision")
	}
	if !attribution.TouchesLine("lib.go", 4) {
		t.Error("the committed edit to lib.go's changed line is not attributed")
	}
	if attribution.TouchesLine("lib.go", 8) {
		t.Error("an untouched line in a touched file is attributed")
	}
	if !attribution.TouchesLine("new.go", 1) {
		t.Error("an uncommitted, brand-new file is not attributed at all")
	}

	// Discrimination: an empty base revision is the failure this design
	// exists to make visible rather than silently misread. It must report
	// Established: false, and TouchesLine must fail toward "yes" rather than
	// toward "no" once that happens (the design caution PIPE-111/PIPE-111a
	// state explicitly) — under-attribution on a failure is the same class of
	// defect as the gates this milestone repairs.
	failed := deriveChangeAttribution(t.Context(), worktree, "")
	if failed.Established {
		t.Fatal("an empty base revision was reported as an established attribution")
	}
	if !failed.TouchesLine("lib.go", 8) {
		t.Error("an unestablished attribution must fail toward inclusion, not exclusion")
	}
}

// TestPIPE111_UnreadableBaseRevisionFailsTowardInclusion covers the other
// failure path: a base revision that does not resolve in this worktree at
// all (git error), as opposed to an empty string.
func TestPIPE111_UnreadableBaseRevisionFailsTowardInclusion(t *testing.T) {
	worktree, _ := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\nfunc Old() int { return 1 }\n",
	})
	attribution := deriveChangeAttribution(
		t.Context(), worktree, "0000000000000000000000000000000000000000")
	if attribution.Established {
		t.Fatal("a base revision this repository does not have was reported established")
	}
	if !attribution.TouchesLine("lib.go", 1) {
		t.Error("a failed diff must fail toward inclusion, not exclusion")
	}
}

// TestPIPE111a_OneLineFixAttributesOnlyItsEnclosingDeclaration covers
// PIPE-111a.
//
// A file holding three functions has one line, inside the middle one,
// changed. Only that function may be attributed — the other two are
// pre-existing code this run never touched, and attributing them is the same
// over-attribution PIPE-111 exists to remove, pushed down one level.
func TestPIPE111a_OneLineFixAttributesOnlyItsEnclosingDeclaration(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\n" +
			"func A(value int) int {\n" +
			"\treturn value\n" +
			"}\n\n" +
			"func B(value int) int {\n" +
			"\treturn value\n" +
			"}\n\n" +
			"func C(value int) int {\n" +
			"\treturn value\n" +
			"}\n",
	})
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"func A(value int) int {\n"+
		"\treturn value\n"+
		"}\n\n"+
		"func B(value int) int {\n"+
		"\treturn value + 1\n"+ // the one changed line
		"}\n\n"+
		"func C(value int) int {\n"+
		"\treturn value\n"+
		"}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	functions, err := attributedFunctions(worktree, attribution)
	if err != nil {
		t.Fatal(err)
	}
	scope := attributeDeclarations(functions, attribution)

	if !scope.Established {
		t.Fatal("declaration attribution was not established")
	}
	if !scope.Contains("B") {
		t.Error("the function whose own line changed is not attributed")
	}
	if scope.Contains("A") || scope.Contains("C") {
		t.Errorf("a function nothing inside it changed is attributed: %+v", scope.Names)
	}
	if scope.Count() != 1 {
		t.Errorf("expected exactly one attributed declaration, got %d: %+v",
			scope.Count(), scope.Names)
	}

	// Discrimination: readProducedFunctions is what every gate used before
	// this fix to enumerate what it was answerable for, and it reads
	// producedGoFiles' `git status` view — which sees nothing at all here,
	// because the edit above was committed and the working tree is clean.
	// That is a stronger version of the same class of defect PIPE-111a
	// removes at the declaration level: not over-attribution, but total
	// blindness. Confirming it is what shows attributedFunctions is doing
	// real work, not agreeing with an already-correct answer.
	blind, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(blind) != 0 {
		t.Fatalf("fixture assumption broken: the git-status view unexpectedly "+
			"still sees %d produced function(s) after a committed edit; this "+
			"test's discrimination claim depends on it seeing none", len(blind))
	}
}

// TestPIPE111a_ZeroAttributionIsDistinguishedFromFailedAttribution covers the
// design caution shared by PIPE-111 and PIPE-111a: a run that changed no
// declaration at all (a legitimate refactor, PIPE-124's shape) must not read
// the same way as a run whose attribution could not be computed.
func TestPIPE111a_ZeroAttributionIsDistinguishedFromFailedAttribution(t *testing.T) {
	legitimateZero := attributedScope{Established: true, Names: map[string]bool{}}
	failedComputation := attributedScope{}

	if !legitimateZero.Established {
		t.Error("a legitimate zero-declaration attribution must record Established: true")
	}
	if failedComputation.Established {
		t.Error("the zero value must not read as an established, empty attribution")
	}
	// Both currently report Contains(name) differently for the same reason a
	// caller must not conflate them: the legitimate zero excludes everything
	// (nothing was attributed), the failure includes everything (the
	// fail-toward-inclusion rule).
	if legitimateZero.Contains("Anything") {
		t.Error("a legitimate empty attribution incorrectly includes a name")
	}
	if !failedComputation.Contains("Anything") {
		t.Error("a failed attribution incorrectly excludes a name")
	}
}

// wideOpenAttribution simulates the file-level scoping every gate used
// before PIPE-111a: every one of files is "established" as changed across its
// entire span, rather than only the lines actually different from base. Tests
// use it to isolate the declaration/line-scoping defect PIPE-112 through
// PIPE-115 remove from the separate committed-file-visibility defect
// PIPE-111's own tests cover — comparing against attributedScope{} or
// changeAttribution{} directly would fall back to producedGoFiles' git-status
// view, which a committed fixture edit makes blind for an unrelated reason.
func wideOpenAttribution(files []string) changeAttribution {
	ranges := make(map[string][]changedLineRange, len(files))
	for _, file := range files {
		ranges[file] = []changedLineRange{{Start: 1, End: 1 << 30}}
	}
	return changeAttribution{
		Established: true, BaseRevision: "wide-open (test double)", Ranges: ranges,
	}
}

// TestPIPE112_CompletenessGapsNameOnlyAttributedDeclarations covers PIPE-112.
func TestPIPE112_CompletenessGapsNameOnlyAttributedDeclarations(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\n" +
			"// Old is already here before this run starts.\n" +
			"func Old(value int) int {\n" +
			"\treturn value\n" +
			"}\n",
	})
	// The run adds New — untested, undocumented — and never touches Old.
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"// Old is already here before this run starts.\n"+
		"func Old(value int) int {\n"+
		"\treturn value\n"+
		"}\n\n"+
		"func New(value int) int {\n"+
		"\treturn value\n"+
		"}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}

	gaps, err := findCompletenessGaps(worktree, pipeline.DefaultSettings(), attribution)
	if err != nil {
		t.Fatal(err)
	}
	if !stringSliceHas(gaps.UntestedAtoms, "New") {
		t.Errorf("the function this run actually wrote is not reported untested: %+v", gaps)
	}
	if stringSliceHas(gaps.UntestedAtoms, "Old") {
		t.Errorf("a pre-existing function nobody touched was sent back to be tested: %+v", gaps)
	}
	if stringSliceHas(gaps.UndocumentedAtoms, "Old") {
		t.Errorf("a pre-existing function nobody touched was sent back for a doc comment: %+v", gaps)
	}

	// Discrimination: a wide-open attribution — file-level scoping, the
	// pre-PIPE-112 unit — does name Old on this exact worktree, proving the
	// assertions above are actually exercising the re-scoping and not
	// something that happened to already be true.
	wide := wideOpenAttribution(attributionFiles(attribution))
	unscoped, err := findCompletenessGaps(worktree, pipeline.DefaultSettings(), wide)
	if err != nil {
		t.Fatal(err)
	}
	if !stringSliceHas(unscoped.UntestedAtoms, "Old") {
		t.Fatal("the unscoped baseline does not reproduce the defect this " +
			"test guards against, so it proves nothing")
	}
}

// TestPIPE113_AntiPatternsGateOnlyOnChangedLines covers PIPE-113's
// anti-patterns half.
func TestPIPE113_AntiPatternsGateOnlyOnChangedLines(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\n" +
			"// Old panics; it is pre-existing and untouched by this run.\n" +
			"func Old() {\n" +
			"\tpanic(\"no\")\n" +
			"}\n",
	})
	// The run adds New, which also panics: a fresh finding on a line this run
	// actually wrote, alongside the pre-existing one it never touches.
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"// Old panics; it is pre-existing and untouched by this run.\n"+
		"func Old() {\n"+
		"\tpanic(\"no\")\n"+
		"}\n\n"+
		"func New() {\n"+
		"\tpanic(\"no\")\n"+
		"}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	outcome := checkAntiPatterns(worktree, attribution)
	if outcome.Held {
		t.Errorf("a fresh anti-pattern on a line this run wrote was not gated: %+v", outcome)
	}
	if !strings.Contains(outcome.Detail, "panic inside New") {
		t.Errorf("the failure does not name the finding this run is answerable "+
			"for: %q", outcome.Detail)
	}
	if strings.Contains(outcome.Detail, "panic inside Old") {
		t.Errorf("a pre-existing finding is reported as a gate failure: %q",
			outcome.Detail)
	}
	context, _ := outcome.Evidence["pre_existing_context"].([]string)
	if len(context) == 0 {
		t.Error("the pre-existing finding is not preserved as context evidence")
	}

	// Discrimination: a wide-open attribution — every finding in the file is
	// a gate failure, the pre-PIPE-113 behaviour — does name Old's.
	wide := wideOpenAttribution(attributionFiles(attribution))
	unscoped := checkAntiPatterns(worktree, wide)
	if !strings.Contains(unscoped.Detail, "panic inside Old") {
		t.Fatal("the unscoped baseline does not reproduce the defect this " +
			"test guards against, so it proves nothing")
	}
}

// TestPIPE113_ComplexityAndSimplificationReportOnlyAttributedFunctions covers
// PIPE-113's complexity and simplification half.
func TestPIPE113_ComplexityAndSimplificationReportOnlyAttributedFunctions(t *testing.T) {
	tangledOld := "func Old(values []int) int {\n" +
		"\ttotal := 0\n" +
		"\tfor _, value := range values {\n" +
		"\t\tif value > 0 {\n" +
		"\t\t\tif value > 1 {\n" +
		"\t\t\t\tif value > 2 {\n" +
		"\t\t\t\t\tif value > 3 {\n" +
		"\t\t\t\t\t\tif value > 4 {\n" +
		"\t\t\t\t\t\t\tif value > 5 {\n" +
		"\t\t\t\t\t\t\t\tif value > 6 {\n" +
		"\t\t\t\t\t\t\t\t\ttotal++\n" +
		"\t\t\t\t\t\t\t\t}\n\t\t\t\t\t\t\t}\n\t\t\t\t\t\t}\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n" +
		"\treturn total\n}\n"
	worktree, base := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\n" + tangledOld,
	})
	// New is a small, untangled function the run adds; Old, tangled enough to
	// be a simplification candidate under the stage's own threshold, is
	// pre-existing and untouched.
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+tangledOld+
		"\nfunc New(value int) int {\n\treturn value\n}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}
	functions, err := attributedFunctions(worktree, attribution)
	if err != nil {
		t.Fatal(err)
	}
	scope := attributeDeclarations(functions, attribution)
	if scope.Contains("Old") {
		t.Fatal("fixture assumption broken: Old must not be attributed")
	}
	if !scope.Contains("New") {
		t.Fatal("fixture assumption broken: New must be attributed")
	}

	complexity := checkComplexity(worktree, attribution)
	if labels, ok := complexity.Evidence["time_labels"].(map[string]string); ok {
		if _, present := labels["Old"]; present {
			t.Errorf("a pre-existing, untouched function is labelled by this "+
				"run's complexity evidence: %+v", labels)
		}
	}

	simplification := checkSimplification(worktree, attribution)
	if worth, ok := simplification.Evidence["worth_simplifying"].([]string); ok {
		for _, candidate := range worth {
			if strings.Contains(candidate, "Old") {
				t.Errorf("a pre-existing, untouched function is named as worth "+
					"simplifying: %+v", worth)
			}
		}
	}

	// Discrimination: a wide-open attribution — file-level scoping, the
	// pre-PIPE-113 unit — does name Old, since it genuinely is tangled by the
	// stage's own threshold.
	wide := wideOpenAttribution(attributionFiles(attribution))
	unscopedComplexity := checkComplexity(worktree, wide)
	unscopedLabels, _ := unscopedComplexity.Evidence["time_labels"].(map[string]string)
	if _, present := unscopedLabels["Old"]; !present {
		t.Fatal("the unscoped baseline does not reproduce the defect this " +
			"test guards against, so it proves nothing")
	}
}

// TestPIPE114_MutationTargetsOnlyAttributedLines covers PIPE-114.
//
// firstAttributedLine is the pure line-selection rule checkMutations applies
// before ever running the suite, so this proves the rule directly rather than
// paying for a `go build`/`go test` cycle to observe its effect indirectly.
func TestPIPE114_MutationTargetsOnlyAttributedLines(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\n" +
			"func Old(a, b int) bool {\n" +
			"\treturn a == b\n" +
			"}\n",
	})
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"func Old(a, b int) bool {\n"+
		"\treturn a == b\n"+ // unchanged
		"}\n\n"+
		"func New(a, b int) bool {\n"+
		"\treturn a == b\n"+ // this run's own equality comparison
		"}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	content, err := os.ReadFile(filepath.Join(worktree, "lib.go"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(content), "\n")

	target := firstAttributedLine(lines, "lib.go", " == ", attribution)
	if target == -1 {
		t.Fatal("no attributed line was found for a pattern that occurs twice")
	}
	if target+1 == 4 {
		t.Error("the mutation targeted line 4, Old's pre-existing comparison")
	}
	if !attribution.TouchesLine("lib.go", target+1) {
		t.Error("the selected line is not one attribution marks as changed")
	}
	if target+1 != 8 {
		t.Errorf("expected New's comparison at line 8, selected line %d", target+1)
	}

	// Discrimination: without attribution — the pre-PIPE-114 behaviour, first
	// textual occurrence anywhere in the file — the same lines and pattern
	// resolve to Old's pre-existing line 4.
	unscoped := firstAttributedLine(lines, "lib.go", " == ", changeAttribution{})
	if unscoped != 3 {
		t.Fatalf("the unscoped baseline picked line %d, not Old's line 4; it "+
			"does not reproduce the defect this test guards against", unscoped+1)
	}
}

// TestPIPE115_CoverageIsMeasuredOverChangedLinesNotTheLeastCoveredFunction
// covers PIPE-115.
//
// Old is pre-existing, untested, dead code — the same shape as production
// code nobody has gotten around to deleting. New is this run's own change,
// fully covered by its own test. The old whole-module measurement would fail
// the gate on Old, which this run never touched; the changed-lines
// measurement must not.
func TestPIPE115_CoverageIsMeasuredOverChangedLinesNotTheLeastCoveredFunction(t *testing.T) {
	worktree, base := newAttributionFixture(t, map[string]string{
		"lib.go": "package lib\n\n" +
			"func Existing(value int) int { return value }\n\n" +
			"func Old(value int) int { return value + 1 }\n",
		"lib_test.go": "package lib\n\nimport \"testing\"\n\n" +
			"func TestExisting(t *testing.T) {\n" +
			"\tif Existing(1) != 1 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
	})
	writeAttributionFile(t, worktree, "lib.go", "package lib\n\n"+
		"func Existing(value int) int { return value }\n\n"+
		"func Old(value int) int { return value + 1 }\n\n"+
		"func New(value int) int { return value + 2 }\n", true)
	writeAttributionFile(t, worktree, "lib_test.go",
		"package lib\n\nimport \"testing\"\n\n"+
			"func TestExisting(t *testing.T) {\n"+
			"\tif Existing(1) != 1 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n\n"+
			"func TestNew(t *testing.T) {\n"+
			"\tif New(1) != 3 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n", true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established")
	}
	scoped := checkFunctionCoverage(t.Context(), worktree, attribution)
	if !scoped.Held {
		t.Errorf("this run's own, fully-tested change was failed for coverage "+
			"it does not own: %s", scoped.Detail)
	}

	// Discrimination: the whole-module fallback (attribution not established)
	// measures Old's pre-existing, untested function and fails the gate for
	// it, reproducing the defect PIPE-115 exists to remove.
	unscoped := checkFunctionCoverage(t.Context(), worktree, changeAttribution{})
	if unscoped.Held {
		t.Fatal("the unscoped baseline holds, so it does not reproduce the " +
			"defect this test guards against")
	}
	if !strings.Contains(unscoped.Detail, "Old") {
		t.Errorf("the unscoped baseline's failure does not name Old, so it "+
			"is not actually the least-covered-function defect: %q", unscoped.Detail)
	}
}

// pipe116FixtureFiles names the fixture's own Go source, so the "how big is
// the base library" sanity check can parse it directly rather than through
// producedGoFiles' `git status` view — which reads clean, and so empty,
// immediately after the fixture's own base commit.
var pipe116FixtureFiles = []string{"textutil.go", "numeric.go", "lib_test.go"}

// TestPIPE116_ASubstantialPreExistingRepositoryIsJudgedOnOneDeclaration is
// the fixture-repository test PIPE-116 asks for.
//
// internal/coordinator/testdata/pipe116_substantial_repository is a small but
// genuinely multi-function, multi-file library — six functions in textutil.go
// and six more in numeric.go, all pre-existing. numeric.go also carries a
// real anti-pattern (LoadConfigValue's swallowed parse error) and a real
// completeness gap (parseConfigInt has no test and no doc comment), both
// deliberately untouched by the one-line, one-declaration edit this test then
// makes to Sum, in the same file. A clean-slate fixture cannot prove the
// pre-existing-finding-is-context claim at all: the finding has to be real,
// in the same file the run touches, and still untouched by the run's own
// edit for "context, not a gate failure" to mean anything.
func TestPIPE116_ASubstantialPreExistingRepositoryIsJudgedOnOneDeclaration(t *testing.T) {
	worktree := t.TempDir()
	copyFixtureTree(t,
		filepath.Join("testdata", "pipe116_substantial_repository"), worktree)

	run := func(arguments ...string) string {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", arguments...)
		command.Dir = worktree
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "--initial-branch=main")
	run("add", ".")
	run("-c", "user.name=Codeflux Test", "-c", "user.email=codeflux@example.invalid",
		"commit", "-m", "base: substantial pre-existing library")
	base := run("rev-parse", "HEAD")

	baseline, err := parseProducedFunctions(worktree, pipe116FixtureFiles)
	if err != nil {
		t.Fatal(err)
	}
	var baselineNamed int
	for _, function := range baseline {
		if !isTestScaffolding(function) {
			baselineNamed++
		}
	}
	if baselineNamed < 12 {
		t.Fatalf("fixture assumption broken: only %d pre-existing declarations "+
			"were parsed, which is not substantial enough to make this test "+
			"discriminate", baselineNamed)
	}

	// The one-line, one-declaration change: Sum's accumulation is rewritten
	// in an equivalent form. It stays behaviourally identical, so nothing
	// about the suite's own correctness is at stake; only attribution is
	// under test. Sum shares numeric.go with LoadConfigValue's real
	// anti-pattern and parseConfigInt's real completeness gap, which is what
	// lets this test prove those two are recorded as context rather than
	// gated: a pre-existing finding in an untouched file was never examined
	// by any version of these checks, so proving that would prove nothing.
	numeric, err := os.ReadFile(filepath.Join(worktree, "numeric.go"))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(numeric),
		"total += value", "total = total + value", 1)
	if edited == string(numeric) {
		t.Fatal("the fixture's Sum body has changed; update the fixture edit to match")
	}
	writeAttributionFile(t, worktree, "numeric.go", edited, true)

	attribution := deriveChangeAttribution(t.Context(), worktree, base)
	if !attribution.Established {
		t.Fatal("attribution was not established against the fixture's base revision")
	}
	functions, err := attributedFunctions(worktree, attribution)
	if err != nil {
		t.Fatal(err)
	}
	scope := attributeDeclarations(functions, attribution)

	if scope.Count() != 1 {
		t.Fatalf("expected exactly one attributed declaration out of %d "+
			"pre-existing ones, got %d: %+v", baselineNamed, scope.Count(), scope.Names)
	}
	if !scope.Contains("Sum") {
		t.Fatalf("the one function actually changed is not the one attributed: %+v",
			scope.Names)
	}

	// Phase B: anti-patterns. LoadConfigValue's swallowed parse error is
	// real, pre-existing, in the same file as this run's edit, and
	// untouched — it must not gate-fail this run.
	antiPatterns := checkAntiPatterns(worktree, attribution)
	if !antiPatterns.Held {
		t.Errorf("a pre-existing anti-pattern in an untouched declaration "+
			"gated this run: %s", antiPatterns.Detail)
	}
	preExisting, _ := antiPatterns.Evidence["pre_existing_count"].(int)
	if preExisting == 0 {
		t.Error("the fixture's real anti-pattern in LoadConfigValue was not " +
			"even recorded as context, so this proves nothing about scoping")
	}

	// Phase B: completeness. parseConfigInt is untested and undocumented —
	// deliberately, in the fixture — and untouched; it must not be sent back.
	gaps, err := findCompletenessGaps(worktree, pipeline.DefaultSettings(), attribution)
	if err != nil {
		t.Fatal(err)
	}
	if !gaps.Empty() {
		t.Errorf("the completeness loop found something to send back for a "+
			"run that only edited a fully tested, fully documented function: %+v",
			gaps)
	}
	wide := wideOpenAttribution(attributionFiles(attribution))
	unscopedGaps, err := findCompletenessGaps(worktree, pipeline.DefaultSettings(), wide)
	if err != nil {
		t.Fatal(err)
	}
	if !stringSliceHas(unscopedGaps.UntestedAtoms, "parseConfigInt") {
		t.Fatal("the unscoped baseline does not reproduce parseConfigInt's " +
			"pre-existing gap, so the completeness assertion above proves nothing")
	}

	// Phase D: coverage. Sum is already covered by its own pre-existing
	// test, and this run's edit did not change that.
	coverage := checkFunctionCoverage(t.Context(), worktree, attribution)
	if !coverage.Held {
		t.Errorf("a fully covered one-line change failed the changed-line "+
			"coverage gate: %s", coverage.Detail)
	}
}

// copyFixtureTree copies every regular file under source into destination,
// preserving relative paths, so a checked-in testdata fixture can be
// committed into a fresh, disposable Git repository per test run rather than
// mutated in place.
func copyFixtureTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o600)
	})
	if err != nil {
		t.Fatalf("copy fixture tree: %v", err)
	}
}

// stringSliceHas reports whether values contains text, without pulling in
// slices.Contains's generic instantiation for one call site.
func stringSliceHas(values []string, text string) bool {
	for _, value := range values {
		if value == text {
			return true
		}
	}
	return false
}
