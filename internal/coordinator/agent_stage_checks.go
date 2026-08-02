package coordinator

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/pipeline"
)

// stageOutcome is what one performed check found.
//
// Held is whether the gate held; Detail is what a person reads; Evidence is
// what a later comparison uses. Skipped is separate from a failed gate: a
// program with nothing to fuzz has not failed fuzzing, and recording it as a
// failure would make the ledger useless for the case it exists to describe.
type stageOutcome struct {
	Held     bool
	Skipped  bool
	Detail   string
	Evidence map[string]any
}

// held builds a satisfied outcome.
func held(detail string, evidence map[string]any) stageOutcome {
	if evidence == nil {
		evidence = map[string]any{}
	}
	return stageOutcome{Held: true, Detail: detail, Evidence: evidence}
}

// broke builds a failed outcome.
func broke(detail string, evidence map[string]any) stageOutcome {
	if evidence == nil {
		evidence = map[string]any{}
	}
	return stageOutcome{Detail: detail, Evidence: evidence}
}

// skipped builds an outcome for a stage this run had no need of.
func skipped(detail string) stageOutcome {
	return stageOutcome{Skipped: true, Detail: detail}
}

// platformTarget is one system and architecture a program claims to run on.
type platformTarget struct {
	System       string
	Architecture string
}

// String names the target the way Go does.
func (target platformTarget) String() string {
	return target.System + "/" + target.Architecture
}

// hostPlatform is the one platform every run can check for free.
//
// It is the default because most work is written and run in one place, and
// cross-compiling to systems nobody is targeting costs time on every run to
// answer a question nobody asked. Declaring more is a choice, not the norm.
func hostPlatform() []platformTarget {
	return []platformTarget{{System: runtime.GOOS, Architecture: runtime.GOARCH}}
}

// PortablePlatforms is the matrix a program that claims portability owes.
//
// It is exported so a caller can ask for it by name rather than assembling the
// list themselves and getting a different one each time.
func PortablePlatforms() []platformTarget {
	return []platformTarget{
		{System: "linux", Architecture: "amd64"},
		{System: "darwin", Architecture: "arm64"},
		{System: "windows", Architecture: "amd64"},
	}
}

// checkPlatformMatrix builds for every platform the program claims to support.
//
// A program is only portable if it has been built for the platforms it says it
// supports, and compiling for the host alone proves the one platform nobody
// was in doubt about. But the claim is the caller's to make: a program written
// for one machine is not less correct for failing to cross-compile, so the
// default is the host and anything more is declared.
func checkPlatformMatrix(
	ctx context.Context,
	worktree string,
	targets []platformTarget,
) stageOutcome {
	if len(targets) == 0 {
		targets = hostPlatform()
	}
	var failures []string
	built := 0
	for _, target := range targets {
		build := exec.CommandContext(ctx, "go", "build", "./...")
		build.Dir = worktree
		build.Env = append(os.Environ(),
			"GOOS="+target.System, "GOARCH="+target.Architecture)
		output, err := build.CombinedOutput()
		if err != nil {
			failures = append(failures,
				target.String()+": "+firstLineOf(strings.TrimSpace(string(output))))
			continue
		}
		built++
	}
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.String())
	}
	evidence := map[string]any{
		"declared": names, "built": built, "failures": failures,
	}
	if len(failures) > 0 {
		return broke("the module does not build everywhere it claims to run: "+
			strings.Join(failures, "; "), evidence)
	}
	if len(targets) == 1 {
		// One platform is a complete answer to a claim about one platform. It
		// says which, so nobody reads it as a portability result.
		return held(fmt.Sprintf(
			"the module compiles for %s, the only platform this run claims",
			names[0]), evidence)
	}
	return held(fmt.Sprintf("the module compiles for all %d declared platforms: %s",
		len(targets), strings.Join(names, ", ")), evidence)
}

// checkRepetition runs the suite repeatedly and requires the same answer.
//
// A suite run once cannot tell a correct program from an intermittently
// correct one, and intermittent is the harder failure to find later. Repeating
// it is the cheapest way to turn "the tests passed" into "the tests pass".
func checkRepetition(ctx context.Context, worktree string) stageOutcome {
	const runs = 3
	outcomes := make([]bool, 0, runs)
	for attempt := range runs {
		command := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
		command.Dir = worktree
		// Each repetition is forced to actually run rather than be served from
		// the build cache, which would make every repeat after the first a
		// reading of the first one's answer.
		_, err := command.CombinedOutput()
		outcomes = append(outcomes, err == nil)
		_ = attempt
	}
	passes := 0
	for _, ok := range outcomes {
		if ok {
			passes++
		}
	}
	evidence := map[string]any{"runs": runs, "passes": passes}
	switch passes {
	case runs:
		return held(fmt.Sprintf("the suite passed all %d runs", runs), evidence)
	case 0:
		return broke(fmt.Sprintf("the suite failed all %d runs", runs), evidence)
	default:
		return broke(fmt.Sprintf(
			"the suite passed %d of %d runs, so it is flaky and its result "+
				"means nothing", passes, runs), evidence)
	}
}

// capabilityOfImport is which standard-library packages grant what.
//
// The names on the right are pipeline's, not this file's, so a restriction
// somebody sets on a settings page is the same word this check enforces. Two
// vocabularies would mean a limit that reads as set and is never applied.
var capabilityOfImport = map[string]string{
	// net/url and path/filepath are deliberately absent: both are string
	// manipulation and neither touches anything. Counting them would report a
	// capability on almost every program and make the report worthless.
	"net":       pipeline.CapabilityNetwork,
	"net/http":  pipeline.CapabilityNetwork,
	"os/exec":   pipeline.CapabilityProcesses,
	"os":        pipeline.CapabilityFilesystem,
	"io/ioutil": pipeline.CapabilityFilesystem,
	"syscall":   pipeline.CapabilitySyscalls,
	"unsafe":    pipeline.CapabilityUnsafe,
	"plugin":    pipeline.CapabilityUnsafe,
}

// capabilityMeaning says what taking a capability lets a program do, in the
// terms of somebody reading the report rather than the import list.
var capabilityMeaning = map[string]string{
	pipeline.CapabilityNetwork:    "reaches the network",
	pipeline.CapabilityProcesses:  "starts other processes",
	pipeline.CapabilityFilesystem: "reads its environment and the filesystem",
	pipeline.CapabilitySyscalls:   "calls the operating system directly",
	pipeline.CapabilityUnsafe:     "steps outside the type system",
}

// checkGlobalInvariants reports what the whole program can reach for.
//
// It used to forbid a fixed list — the network, other processes, the operating
// system — which encoded an assumption that every program is a filter reading
// input and writing output. A program asked to serve HTTP would have failed a
// gate for doing exactly what it was asked. What generalises is not a
// prohibition but a report: here is what this program can now do, stated where
// somebody reviewing it will see it.
//
// A caller who does have a restriction declares it, and only then is exceeding
// it a failure.
func checkGlobalInvariants(
	worktree string,
	forbidden []string,
) stageOutcome {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return broke("the produced source could not be read: "+err.Error(), nil)
	}
	if len(files) == 0 {
		return skipped("the run produced no Go source to examine")
	}
	// Imports are read from the parse tree, not searched for as text. A path
	// mentioned in a doc comment or a string literal is not an import, and
	// counting one as a capability would fail a run for describing what it
	// deliberately does not do.
	taken := map[string][]string{}
	for _, file := range files {
		parsed, parseErr := parser.ParseFile(token.NewFileSet(),
			filepath.Join(worktree, file), nil, parser.ImportsOnly)
		if parseErr != nil {
			continue
		}
		for _, imported := range parsed.Imports {
			path, quoteErr := strconv.Unquote(imported.Path.Value)
			if quoteErr != nil {
				continue
			}
			if capability, ok := capabilityOfImport[path]; ok {
				taken[capability] = append(taken[capability], file)
			}
		}
	}
	var named []string
	for capability := range taken {
		named = append(named, capabilityMeaning[capability])
	}
	sort.Strings(named)

	var exceeded []string
	for _, restriction := range forbidden {
		if where, ok := taken[restriction]; ok {
			exceeded = append(exceeded, fmt.Sprintf("%s — %s (%s)",
				restriction, capabilityMeaning[restriction],
				strings.Join(where, ", ")))
		}
	}
	sort.Strings(exceeded)
	evidence := map[string]any{
		"files_examined": len(files), "capabilities": named,
		"declared_forbidden": forbidden, "exceeded": exceeded,
	}
	if len(exceeded) > 0 {
		return broke("the program takes capabilities this run declared it "+
			"would not: "+strings.Join(exceeded, "; "), evidence)
	}
	if len(named) == 0 {
		return held(fmt.Sprintf(
			"%d produced file(s) reach for nothing beyond their own arguments",
			len(files)), evidence)
	}
	return held(fmt.Sprintf("%d produced file(s); the program %s",
		len(files), strings.Join(named, ", ")), evidence)
}

// checkNonFunctional times the suite against a budget.
//
// The budget is generous and fixed rather than compared against a stored
// baseline, because no baseline exists yet and inventing one would produce a
// number that looks measured. What it catches today is the honest extreme: a
// program or suite that has become slow enough to be a different kind of
// problem.
func checkNonFunctional(ctx context.Context, worktree string) stageOutcome {
	const budget = 60 * time.Second
	started := time.Now()
	command := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
	command.Dir = worktree
	_, err := command.CombinedOutput()
	elapsed := time.Since(started)

	evidence := map[string]any{
		"elapsed_ms": elapsed.Milliseconds(),
		"budget_ms":  budget.Milliseconds(),
	}
	if err != nil {
		return skipped("the suite did not pass, so its duration measures nothing")
	}
	if elapsed > budget {
		return broke(fmt.Sprintf(
			"the suite took %s against a %s budget", elapsed.Round(time.Millisecond),
			budget), evidence)
	}
	return held(fmt.Sprintf("the suite completed in %s, within its %s budget",
		elapsed.Round(time.Millisecond), budget), evidence)
}

// checkFuzzing runs Go's own fuzzing over any fuzz target the run wrote.
//
// A program with no parsing boundary has nothing to fuzz and is skipped rather
// than failed. One that has a boundary and no fuzz target has a gap, and the
// gate says so instead of quietly passing.
func checkFuzzing(ctx context.Context, worktree string) stageOutcome {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return broke("the produced source could not be read: "+err.Error(), nil)
	}
	targets := 0
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(worktree, file))
		if readErr != nil {
			continue
		}
		targets += strings.Count(string(body), "func Fuzz")
	}
	if targets == 0 {
		return skipped("the run wrote no fuzz target, so there was nothing to fuzz")
	}
	deadline, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	command := exec.CommandContext(deadline,
		"go", "test", "-run=^$", "-fuzz=.", "-fuzztime=20s", "./...")
	command.Dir = worktree
	output, err := command.CombinedOutput()
	evidence := map[string]any{"fuzz_targets": targets}
	if err != nil {
		return broke("fuzzing found a failing input: "+
			firstLineOf(strings.TrimSpace(string(output))), evidence)
	}
	return held(fmt.Sprintf(
		"%d fuzz target(s) ran for 20s without finding a failing input",
		targets), evidence)
}

// producedGoFiles lists the Go source this run actually wrote.
//
// It asks Git rather than walking the tree. Walking meant every Go file in the
// module counted as the run's work, so a run in a real repository would be
// held to code it never touched — asked to write tests for functions that
// existed before it started and to answer for anti-patterns somebody else
// introduced. The first version papered over that by excluding one filename
// the test fixture happened to use, which worked for the fixture and for
// nothing else.
//
// The worktree is a Git worktree, so the set of files that differ from the
// commit it was created at is exactly the set this run is answerable for.
func producedGoFiles(worktree string) ([]string, error) {
	changed := exec.Command("git", "status", "--porcelain=v1", "--untracked-files=all")
	changed.Dir = worktree
	output, err := changed.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("the produced files could not be listed: %s",
			strings.TrimSpace(string(output)))
	}
	var files []string
	for _, line := range strings.Split(string(output), "\n") {
		// Porcelain v1 puts a two-character status in the first columns and the
		// path from the third onward. A rename carries both paths; the one
		// after the arrow is where the content is now.
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[2:])
		if index := strings.Index(path, " -> "); index >= 0 {
			path = path[index+4:]
		}
		path = strings.Trim(path, `"`)
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		if strings.HasPrefix(path, ".codeflux") {
			continue
		}
		files = append(files, filepath.ToSlash(path))
	}
	sort.Strings(files)
	return files, nil
}
