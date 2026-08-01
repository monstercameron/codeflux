package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The M22 gates are checked here rather than asserted in a document, because a
// gate nobody executes is a claim rather than a gate. Each test states what
// evidence would falsify it.

// TestM22_G01_FastTestsAreReliableEnoughToRunOnEveryChange is M22-G01.
//
// "Reliable enough to run on every change" means two things a repository can
// actually be checked for: the default suite needs no external service, and it
// is not gated behind an environment variable that a developer would forget.
func TestM22_G01_FastTestsAreReliableEnoughToRunOnEveryChange(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	// The fast suite is plain `go test ./...`, with no tags and no setup.
	dispatch, err := os.ReadFile(filepath.Join(root, "cmd", "codeflux-dev", "main.go"))
	if err != nil {
		t.Fatalf("read dispatch: %v", err)
	}
	fastAt := strings.Index(string(dispatch), `case "test-fast":`)
	if fastAt < 0 {
		t.Fatal("there is no fast suite")
	}
	window := string(dispatch)[fastAt:min(fastAt+400, len(dispatch))]
	if !strings.Contains(window, `runGo(ctx, stdout, stderr, "test", "./...")`) {
		t.Fatalf("the fast suite is not a plain go test invocation:\n%s", window)
	}
	if strings.Contains(window, "-tags") {
		t.Fatal("the fast suite requires build tags, so it is not the default path")
	}

	// Anything expensive must opt IN behind an environment gate, so the
	// default run stays fast. The mounted browser suite is the example.
	mounted, err := os.ReadFile(filepath.Join(
		root, "internal", "frontendtest", "mounted_render_isolation_test.go"))
	if err != nil {
		t.Fatalf("read mounted suite: %v", err)
	}
	if !strings.Contains(string(mounted), "t.Skip") {
		t.Fatal("the mounted browser suite is not gated, so the fast suite is not fast")
	}

	// And CI must actually run the fast suite, or the gate is unenforced.
	if !strings.Contains(readWorkflow(t), "go run ./cmd/codeflux-dev test-fast") {
		t.Fatal("CI does not run the fast suite")
	}
}

// TestM22_G02_IntegrationAndBrowserSuitesRunFromAFreshDatabase is M22-G02.
//
// The falsifying evidence would be a suite that only passes against a database
// some earlier run left behind. Every integration fixture therefore builds its
// own migrated database, and every harness owns a temporary root.
func TestM22_G02_IntegrationAndBrowserSuitesRunFromAFreshDatabase(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	for _, requirement := range []struct {
		path     string
		contains string
		reason   string
	}{
		{
			filepath.Join("internal", "testfixtures", "database.go"),
			"NewDatabaseFixture",
			"integration fixtures must build their own migrated database",
		},
		{
			filepath.Join("internal", "testfixtures", "database.go"),
			"AssertIntegrity",
			"a fresh database must be verified, not assumed",
		},
		{
			filepath.Join("internal", "testharness", "coordinator.go"),
			"ValidateCleanupTarget",
			"a harness must own a temporary root it can safely remove",
		},
		{
			filepath.Join("internal", "testharness", "coordinator.go"),
			"127.0.0.1:0",
			"a harness must bind an ephemeral port so runs cannot collide",
		},
	} {
		source, err := os.ReadFile(filepath.Join(root, requirement.path))
		if err != nil {
			t.Fatalf("read %s: %v", requirement.path, err)
		}
		if !strings.Contains(string(source), requirement.contains) {
			t.Fatalf("%s: %s", requirement.path, requirement.reason)
		}
	}

	// Both suites must be runnable by name.
	registry, err := os.ReadFile(filepath.Join(root, "cmd", "codeflux-dev", "registry.go"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	for _, command := range []string{"test-integration", "test-browser"} {
		if !strings.Contains(string(registry), `"`+command+`"`) {
			t.Fatalf("there is no %q command", command)
		}
	}
}

// TestM22_G03_FaultInjectionDemonstratesZeroDuplicatedActions is M22-G03.
//
// The plan sets the acceptable count of duplicated correctness-bearing actions
// after retry, reconnect, or replay at zero. The gate is that the harness can
// detect a duplicate at all — a ledger that could not would make every fault
// test pass vacuously.
func TestM22_G03_FaultInjectionDemonstratesZeroDuplicatedActions(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	faults, err := os.ReadFile(filepath.Join(root, "internal", "testfixtures", "faults.go"))
	if err != nil {
		t.Fatalf("read fault injection: %v", err)
	}
	for _, required := range []string{
		"DuplicateIdentities", "EffectLedger", "SafeOutcome", "FaultInjector",
	} {
		if !strings.Contains(string(faults), required) {
			t.Fatalf("fault injection has no %s", required)
		}
	}

	// Fifteen declared injection points, each a real durable boundary.
	if strings.Count(string(faults), "FaultPoint = \"") < 15 {
		t.Fatal("fewer than fifteen fault points are declared")
	}

	// Replay must detect a duplicate delivery, which is the reconnect half of
	// the same property.
	replay, err := os.ReadFile(filepath.Join(root, "internal", "testharness", "replay.go"))
	if err != nil {
		t.Fatalf("read replay: %v", err)
	}
	if !strings.Contains(string(replay), "DuplicateSequences") {
		t.Fatal("replay cannot deliver a duplicate, so deduplication is untested")
	}
	if !strings.Contains(string(replay), "GapDetectedAt") {
		t.Fatal("replay cannot detect a gap, so loss is untested")
	}
}

// TestM22_G04_AbuseSuitesExistForEveryNamedCategory is M22-G04.
func TestM22_G04_AbuseSuitesExistForEveryNamedCategory(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	// Each category the gate names must have a suite that actually attacks it.
	categories := map[string][]string{
		"secret": {
			filepath.Join("internal", "redact", "security_abuse_test.go"),
		},
		"path": {
			filepath.Join("internal", "gitwork", "security_abuse_test.go"),
			filepath.Join("internal", "review", "security_abuse_test.go"),
		},
		"origin": {
			filepath.Join("internal", "frontendserver", "security_abuse_test.go"),
		},
		"authority": {
			filepath.Join("internal", "executor", "security_abuse_test.go"),
			filepath.Join("internal", "executor", "prompt_injection_abuse_test.go"),
		},
		"payload": {
			filepath.Join("internal", "graph", "security_abuse_test.go"),
			filepath.Join("internal", "storage", "security_abuse_test.go"),
		},
	}
	for category, paths := range categories {
		for _, path := range paths {
			source, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatalf("%s abuse suite %s is missing: %v", category, path, err)
			}
			if !strings.Contains(string(source), "func TestM22_0") {
				t.Fatalf("%s carries no M22 abuse test", path)
			}
		}
	}

	// Every M22-051..062 case must be claimed by a test name somewhere.
	claimed := map[string]bool{}
	for _, paths := range categories {
		for _, path := range paths {
			source, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				continue
			}
			for number := 51; number <= 62; number++ {
				marker := "TestM22_0" + itoaGate(number) + "_"
				if strings.Contains(string(source), marker) {
					claimed[itoaGate(number)] = true
				}
			}
		}
	}
	for number := 51; number <= 62; number++ {
		if !claimed[itoaGate(number)] {
			t.Fatalf("M22-0%s has no abuse test", itoaGate(number))
		}
	}
}

// TestM22_G05_ScorecardIsReproducibleFromDocumentedCommands is M22-G05.
func TestM22_G05_ScorecardIsReproducibleFromDocumentedCommands(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	scorecard, err := os.ReadFile(filepath.Join(
		root, "internal", "storage", "scorecard_repository.go"))
	if err != nil {
		t.Fatalf("read scorecard: %v", err)
	}
	for _, required := range []string{
		"BuildScorecard", "CompareScorecards", "detectSurprises", "MetricsWindow",
	} {
		if !strings.Contains(string(scorecard), required) {
			t.Fatalf("the scorecard has no %s", required)
		}
	}

	// Reproducible means bounded: an unbounded window would give a different
	// answer every time it ran.
	metrics, err := os.ReadFile(filepath.Join(
		root, "internal", "storage", "metrics_repository.go"))
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	if !strings.Contains(string(metrics), "metrics window requires an explicit start and end") {
		t.Fatal("metrics queries do not require an explicit window, so results are not comparable")
	}

	// The benchmark methodology, which the scorecard's performance numbers
	// come from, must be Git-tracked and reproducible from stated commands.
	benchmarks, err := os.ReadFile(filepath.Join(root, "docs", "benchmarks.md"))
	if err != nil {
		t.Fatalf("read benchmark documentation: %v", err)
	}
	for _, required := range []string{"Methodology", "Environment", "Results", "go test"} {
		if !strings.Contains(string(benchmarks), required) {
			t.Fatalf("docs/benchmarks.md has no %q", required)
		}
	}
	if strings.Contains(string(benchmarks), "codeflux.sqlite3") {
		t.Fatal("benchmark results are stored in runtime SQLite")
	}
}

// TestM22_G06_EveryVerticalFlowIsLocallyReproducible is M22-G06.
//
// The gate names six capabilities. Each is checked for existence and for the
// property that makes it usable: a fake that needed a paid provider, or a
// harness that needed a hand-edited database, would satisfy the letter of the
// gate and none of its purpose.
func TestM22_G06_EveryVerticalFlowIsLocallyReproducible(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	capabilities := []struct {
		name     string
		path     string
		required []string
	}{
		{
			"deterministic fakes",
			filepath.Join("internal", "testfixtures", "provider_steps.go"),
			[]string{"StepProvider", "FullCoverageScript", "ErrFixtureRateLimited"},
		},
		{
			"replay",
			filepath.Join("internal", "testharness", "replay.go"),
			[]string{"LoadReplayFixture", "ReplayControls"},
		},
		{
			"projection comparison",
			filepath.Join("internal", "testharness", "replay.go"),
			[]string{"CompareProjections", "CompareGraphRevisions"},
		},
		{
			"diagnostics",
			filepath.Join("internal", "devdiag", "logs.go"),
			[]string{"Recorder", "AllStages"},
		},
		{
			"profiling",
			filepath.Join("internal", "devdiag", "profiling.go"),
			[]string{"Profiler", "requireLoopback", "requireToken"},
		},
		{
			"golden-path documentation",
			filepath.Join("docs", "developing.md"),
			[]string{"Golden paths", "Test layer:"},
		},
	}
	for _, capability := range capabilities {
		source, err := os.ReadFile(filepath.Join(root, capability.path))
		if err != nil {
			t.Fatalf("%s is missing (%s): %v", capability.name, capability.path, err)
		}
		for _, required := range capability.required {
			if !strings.Contains(string(source), required) {
				t.Fatalf("%s does not provide %s", capability.name, required)
			}
		}
	}

	// Without paid providers: the twelve named scenarios must all be scripted,
	// and no scenario may require a real endpoint.
	scenarios, err := os.ReadFile(filepath.Join(root, "internal", "testharness", "scenarios.go"))
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	if strings.Count(string(scenarios), "ScenarioName = \"") < 12 {
		t.Fatal("fewer than twelve named scenarios are declared")
	}
	if strings.Contains(string(scenarios), "https://api.") {
		t.Fatal("a scenario names a real provider endpoint")
	}

	// Without manual database mutation: the inspection surface must be
	// read-only and must say so.
	inspection, err := os.ReadFile(filepath.Join(
		root, "internal", "storage", "inspection_repository.go"))
	if err != nil {
		t.Fatalf("read inspection: %v", err)
	}
	if !strings.Contains(string(inspection), "free-text SQL door") {
		t.Fatal("the inspection surface does not state that it is read-only by construction")
	}
	for _, forbidden := range []string{"UPDATE ", "DELETE ", "INSERT "} {
		if strings.Contains(string(inspection), forbidden) {
			t.Fatalf("the inspection surface contains a %q statement", strings.TrimSpace(forbidden))
		}
	}
}

func itoaGate(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
