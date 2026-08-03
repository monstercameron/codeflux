package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/testfixtures"
	"codeflux.dev/codeflux/internal/testharness"
)

// The seed, replay, and inspect-db commands are the local vertical loop
// M22-113 through M22-118 describe: produce a known situation, drive a
// recorded session through it, and read the durable result back safely.
//
// Every capability behind them was already implemented and tested in
// internal/testharness and internal/storage. What was missing was any way to
// reach them without writing a Go test, which is what left the registry
// reporting three unavailable skeletons while the subsystems underneath were
// complete.

// runSeed implements `codeflux-dev seed [scenario]` (M22-113).
//
// With no scenario it lists the closed set. With one it prints that scenario's
// full description: what it starts from, what the provider does, which fault
// is injected, the durable events it must produce, and the safe outcome the
// user must be left with.
func runSeed(
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	const label = "codeflux-dev seed"

	if len(invocation.Positional) == 0 {
		scenarios := testharness.NamedScenarios()
		if invocation.JSON {
			return writeCommandJSON(stdout, stderr, label, map[string]any{
				"scenarios": scenarioSummaries(scenarios),
				"count":     len(scenarios),
			})
		}
		fmt.Fprintf(stdout, "%d named scenarios:\n", len(scenarios))
		for _, scenario := range scenarios {
			fmt.Fprintf(stdout, "  %-20s %s\n", scenario.Name, scenario.Summary)
		}
		return exitSuccess
	}

	name := testharness.ScenarioName(invocation.Positional[0])
	scenario, err := testharness.ScenarioByName(name)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		fmt.Fprintf(stderr, "%s: known scenarios: %s\n", label, strings.Join(allScenarioNameStrings(), ", "))
		return exitUsage
	}
	if err := scenario.Validate(); err != nil {
		fmt.Fprintf(stderr, "%s: scenario %q is not runnable: %v\n", label, name, err)
		return exitFailure
	}

	// CODEFLUX_DEV_SEED_SERVE opts into actually opening a database and
	// driving a real requirement through it (REPO-041), rather than only
	// printing the scenario's description. It is an environment toggle
	// rather than a new flag or a second positional argument because
	// registry.go's invocation parser and main.go's per-command argument
	// limits are outside this file's ownership, and every existing caller of
	// `seed` -- including TestAUDIT029_SeedListsAndDescribesTheClosedScenarioSet
	// -- depends on the descriptive behavior staying the default.
	if os.Getenv(seedServeEnvironment) == "1" {
		return runSeedServe(stdout, stderr, invocation, scenario, label)
	}

	if invocation.JSON {
		return writeCommandJSON(stdout, stderr, label, describeScenario(scenario))
	}
	fmt.Fprintf(stdout, "scenario:         %s\n", scenario.Name)
	fmt.Fprintf(stdout, "summary:          %s\n", scenario.Summary)
	fmt.Fprintf(stdout, "repository state: %s\n", scenario.RepositoryState)
	fmt.Fprintf(stdout, "expected outcome: %s\n", scenario.ExpectedOutcome)
	fmt.Fprintf(stdout, "requires approval:%t\n", scenario.RequiresApproval)
	if scenario.Fault != "" {
		fmt.Fprintf(stdout, "fault injected:   %s\n", scenario.Fault)
	}
	fmt.Fprintf(stdout, "provider steps:   %d\n", len(scenario.ProviderScript))
	fmt.Fprintf(stdout, "durable events:\n")
	for index, kind := range scenario.EventKinds {
		fmt.Fprintf(stdout, "  %2d. %s\n", index+1, kind)
	}
	return exitSuccess
}

// seedServeEnvironment opts `seed` into driving a real requirement through a
// real booted coordinator and serving it, instead of only printing the named
// scenario's description. See the comment at its one call site in runSeed.
const seedServeEnvironment = "CODEFLUX_DEV_SEED_SERVE"

// seedServeSecondsEnvironment bounds how long the served application stays up
// before it shuts itself down. A bounded default rather than "forever" means
// a caller that forgets to stop it does not leave an orphaned coordinator
// running past the session that needed it, which is exactly the failure mode
// AGENTS.md's "Stop What You Start" section exists to prevent.
const seedServeSecondsEnvironment = "CODEFLUX_DEV_SEED_SERVE_SECONDS"

const defaultSeedServeSeconds = 20 * 60

// runSeedServe drives one real requirement through a real Application over
// real SQLite (REPO-041) and serves it on a loopback address until it is
// interrupted or its bounded lifetime elapses.
//
// It exists because the mounted browser matrix needs a durably-open thread
// carrying a real task graph and real timeline cards, and nothing on a
// freshly migrated database creates one: bare `/tasks` is deliberately a
// thread picker, not a route that auto-opens a session. This gives the
// picker something real to pick.
//
// Only the "success" scenario is wired to a real run today. The other eleven
// named scenarios describe fault behavior -- denied approvals, budget caps,
// worker crashes -- that this command does not attempt to inject into the
// real engine; driving those honestly would mean reproducing fault injection
// this repository already has in internal/coordinator's own test suite, which
// is out of scope for the seeding gap this ticket closes.
func runSeedServe(
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
	scenario testharness.Scenario,
	label string,
) int {
	if scenario.Name != testharness.ScenarioSuccess {
		fmt.Fprintf(stderr,
			"%s: --serve only drives the %q scenario through a real run today; "+
				"the other named scenarios describe fault behavior this command does not "+
				"inject into the real engine\n",
			label, testharness.ScenarioSuccess)
		return exitUsage
	}

	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "%s: repository: %v\n", label, err)
		return exitFailure
	}
	root, err := resolveCommandRoot(repository, "seed", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "%s: root: %v\n", label, err)
		return exitUsage
	}
	// The served database describes exactly one run: a stale one from a
	// previous invocation left sitting under this root would be a second,
	// silent seeding path that this ticket exists to close off.
	if err := os.RemoveAll(root); err != nil {
		fmt.Fprintf(stderr, "%s: clear %s: %v\n", label, root, err)
		return exitFailure
	}

	buildCtx, cancelBuild := context.WithTimeout(context.Background(), 3*time.Minute)
	workerPath, err := buildSeedWorkerExecutable(buildCtx, repository, root)
	cancelBuild()
	if err != nil {
		fmt.Fprintf(stderr, "%s: build worker executable: %v\n", label, err)
		return exitFailure
	}

	// A coordinator with no frontend asset set mounts no browser server at
	// all: coordinator.ApplicationOptions gates the browser server, its
	// security headers, and its same-origin CSP on
	// FrontendAssetsDirectory/FrontendAssets being set, so without this a
	// served coordinator answers every request 404 and app-root never
	// appears. Built through the same runBuildFrontend `build-frontend`
	// itself calls, at its fixed default location, so a stale asset set from
	// a previous invocation cannot silently linger.
	assetsCtx, cancelAssets := context.WithTimeout(context.Background(), 5*time.Minute)
	assetCode := runBuildFrontend(assetsCtx, stdout, stderr, commandInvocation{})
	cancelAssets()
	if assetCode != exitSuccess {
		fmt.Fprintf(stderr, "%s: build frontend assets: exit %d\n", label, assetCode)
		return exitFailure
	}
	assetsDirectory, err := resolveCommandRoot(repository, frontendAssetDirectory, "")
	if err != nil {
		fmt.Fprintf(stderr, "%s: resolve frontend assets directory: %v\n", label, err)
		return exitFailure
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	const program = "package main\n\nimport \"fmt\"\n\n" +
		"// main prints the report line this task was asked for.\n" +
		"func main() {\n\tfmt.Println(\"report\")\n}\n"
	const programTest = "package main\n\nimport \"testing\"\n\n" +
		"// TestReport calls main to confirm the program runs without a panic.\n" +
		"func TestReport(t *testing.T) {\n\tmain()\n}\n"

	result, err := testharness.StartMountedThread(ctx, testharness.MountedThreadOptions{
		Root:                    filepath.Join(root, "mounted"),
		WorkerExecutable:        workerPath,
		FrontendAssetsDirectory: assetsDirectory,
		Requirement:             "Create cmd/report/main.go so the program prints a report line.",
		AffectedPackages:        []string{"cmd/report"},
		EditPath:                "cmd/report/main.go",
		EditContent:             program,
		TestPath:                "cmd/report/main_test.go",
		TestContent:             programTest,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: drive the seeded requirement: %v\n", label, err)
		return exitFailure
	}

	seconds := defaultSeedServeSeconds
	if raw := os.Getenv(seedServeSecondsEnvironment); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			seconds = parsed
		} else {
			fmt.Fprintf(stderr, "%s: %s=%q must be a positive integer; using the default\n",
				label, seedServeSecondsEnvironment, raw)
		}
	}

	fmt.Fprintf(stdout, "CODEFLUX_FRONTEND_URL=http://%s/\n", result.Application.Address())
	fmt.Fprintf(stdout, "database:  %s\n", filepath.Join(root, "mounted", "coordinator.sqlite3"))
	fmt.Fprintf(stdout, "thread:    %s\n", result.ThreadID)
	fmt.Fprintf(stdout, "task:      %s state=%s\n", result.TaskID, result.FinalState)
	fmt.Fprintf(stdout, "%s: serving for up to %ds; interrupt (Ctrl+C) to stop sooner\n", label, seconds)

	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(seconds) * time.Second):
		fmt.Fprintf(stdout, "%s: bounded serve lifetime elapsed; stopping\n", label)
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()
	if err := result.Application.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(stderr, "%s: shutdown: %v\n", label, err)
		return exitFailure
	}
	return exitSuccess
}

// buildSeedWorkerExecutable compiles the real codeflux-worker subprocess
// entry point beneath the artifact root, mirroring how
// internal/coordinator's own end-to-end fixtures build it for a test. A
// stub would prove only that some process was spawned; the real subprocess
// proves the coordinator hands it something it can decode.
func buildSeedWorkerExecutable(ctx context.Context, repository string, root string) (string, error) {
	name := "codeflux-worker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	output := filepath.Join(root, "bin", name)
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx,
		"go", "build", "-o", output, "codeflux.dev/codeflux/cmd/codeflux-worker")
	command.Dir = repository
	if built, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w\n%s", err, built)
	}
	return output, nil
}

// runReplay implements `codeflux-dev replay <fixture>` (M22-114 through
// M22-117).
//
// It refuses a fixture carrying credential material before reading it as a
// session, which is the same refusal LoadReplayFixture enforces for tests.
func runReplay(
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	const label = "codeflux-dev replay"

	if len(invocation.Positional) == 0 {
		fmt.Fprintf(stderr, "%s: a fixture path is required\n", label)
		return exitUsage
	}
	fixture, err := testharness.LoadReplayFixture(
		invocation.Positional[0], testfixtures.FixtureCredentialShapes())
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return exitFailure
	}

	consumer := newOrderedReplayConsumer()
	result, err := testharness.Replay(fixture, testharness.ReplayControls{}, consumer)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return exitFailure
	}

	// A replay that detected a gap is reported as a failure rather than a
	// summary with a number in it, because a gap is the condition the replay
	// exists to surface.
	outcome := map[string]any{
		"fixture":           invocation.Positional[0],
		"delivered":         len(result.Deliveries),
		"applied":           len(result.AppliedSequences),
		"gap_detected_at":   result.GapDetectedAt,
		"repairs_requested": result.RepairsRequested,
	}
	if invocation.JSON {
		if code := writeCommandJSON(stdout, stderr, label, outcome); code != exitSuccess {
			return code
		}
	} else {
		fmt.Fprintf(stdout, "fixture:           %s\n", invocation.Positional[0])
		fmt.Fprintf(stdout, "delivered:         %d\n", len(result.Deliveries))
		fmt.Fprintf(stdout, "applied:           %d\n", len(result.AppliedSequences))
		fmt.Fprintf(stdout, "repairs requested: %d\n", result.RepairsRequested)
		if result.GapDetectedAt != 0 {
			fmt.Fprintf(stdout, "gap detected at:   %d\n", result.GapDetectedAt)
		}
	}
	if result.GapDetectedAt != 0 {
		fmt.Fprintf(stderr, "%s: the session has a gap at sequence %d\n",
			label, result.GapDetectedAt)
		return exitFailure
	}
	return exitSuccess
}

// runInspectDB implements `codeflux-dev inspect-db <entity>` (M22-118).
//
// It goes through storage.Repositories.Inspect, which is read-only by
// construction: every entity maps to a fixed parameterised statement and there
// is no free-text SQL door. A developer tool that opened one would be the
// manual-mutation path the plan forbids.
func runInspectDB(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	const label = "codeflux-dev inspect-db"

	if len(invocation.Positional) == 0 {
		fmt.Fprintf(stderr, "%s: a database path is required\n", label)
		return exitUsage
	}
	path := invocation.Positional[0]

	database, err := storage.Open(ctx, storage.OpenOptions{Path: path})
	if err != nil {
		fmt.Fprintf(stderr, "%s: open database: %v\n", label, err)
		return exitFailure
	}
	defer func() { _ = database.Close(ctx) }()

	// An unmigrated database is a state, not a defect, and opening one is the
	// most likely way this command is first run. Reporting it as a missing
	// table would send a reader looking for a schema bug.
	version, err := database.SchemaVersion(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "%s: read schema version: %v\n", label, err)
		return exitFailure
	}
	if version == 0 {
		fmt.Fprintf(stderr,
			"%s: %s has no application schema yet; "+
				"start the product against it once so its migrations run\n",
			label, path)
		return exitFailure
	}

	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return exitFailure
	}

	// Every declared entity is reported, including the empty ones. A summary
	// that silently omits what it found none of cannot be told apart from one
	// that never looked.
	const perEntityLimit = 20
	entities := make([]map[string]any, 0, len(storage.AllInspectionEntities()))
	for _, entity := range storage.AllInspectionEntities() {
		result, inspectErr := repositories.Inspect(ctx, storage.InspectionQuery{
			Entity: entity,
			Limit:  perEntityLimit,
		})
		if inspectErr != nil {
			fmt.Fprintf(stderr, "%s: inspect %s: %v\n", label, entity, inspectErr)
			return exitFailure
		}
		rows := make([]map[string]string, 0, len(result.Rows))
		for _, row := range result.Rows {
			rows = append(rows, row.Fields)
		}
		entities = append(entities, map[string]any{
			"entity":    string(entity),
			"count":     len(result.Rows),
			"truncated": result.Truncated,
			"rows":      rows,
		})
	}

	if invocation.JSON {
		return writeCommandJSON(stdout, stderr, label, map[string]any{
			"database": path,
			"limit":    perEntityLimit,
			"entities": entities,
		})
	}
	fmt.Fprintf(stdout, "database: %s\n", path)
	for _, summary := range entities {
		truncated := ""
		if summary["truncated"].(bool) {
			// Truncation is reported rather than left implicit: a clipped
			// answer read as a complete one is how a developer concludes a row
			// does not exist.
			truncated = fmt.Sprintf(" (truncated at %d)", perEntityLimit)
		}
		fmt.Fprintf(stdout, "\n%s: %d%s\n", summary["entity"], summary["count"], truncated)
		for index, row := range summary["rows"].([]map[string]string) {
			keys := make([]string, 0, len(row))
			for key := range row {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, key := range keys {
				parts = append(parts, key+"="+row[key])
			}
			fmt.Fprintf(stdout, "  %2d. %s\n", index+1, strings.Join(parts, " "))
		}
	}
	return exitSuccess
}

// orderedReplayConsumer applies events in order, deduplicating repeats and
// refusing a gap, which is what a correct client does.
type orderedReplayConsumer struct {
	applied  []uint64
	seen     map[uint64]bool
	through  uint64
	repaired int
}

func newOrderedReplayConsumer() *orderedReplayConsumer {
	return &orderedReplayConsumer{seen: make(map[uint64]bool)}
}

func (consumer *orderedReplayConsumer) Apply(event testharness.ReplayEvent) (bool, error) {
	if consumer.seen[event.Sequence] {
		return false, nil
	}
	if consumer.through != 0 && event.Sequence > consumer.through+1 {
		return false, fmt.Errorf(
			"gap: expected sequence %d, received %d", consumer.through+1, event.Sequence)
	}
	consumer.seen[event.Sequence] = true
	consumer.applied = append(consumer.applied, event.Sequence)
	consumer.through = event.Sequence
	return true, nil
}

func (consumer *orderedReplayConsumer) RequestSnapshotRepair(throughSequence uint64) error {
	consumer.repaired++
	consumer.through = throughSequence
	return nil
}

func (consumer *orderedReplayConsumer) Reconnect() error { return nil }

func scenarioSummaries(scenarios []testharness.Scenario) []map[string]any {
	summaries := make([]map[string]any, 0, len(scenarios))
	for _, scenario := range scenarios {
		summaries = append(summaries, describeScenario(scenario))
	}
	return summaries
}

func describeScenario(scenario testharness.Scenario) map[string]any {
	return map[string]any{
		"name":              string(scenario.Name),
		"summary":           scenario.Summary,
		"repository_state":  string(scenario.RepositoryState),
		"expected_outcome":  string(scenario.ExpectedOutcome),
		"requires_approval": scenario.RequiresApproval,
		"fault":             string(scenario.Fault),
		"provider_steps":    len(scenario.ProviderScript),
		"event_kinds":       scenario.EventKinds,
	}
}

func allScenarioNameStrings() []string {
	names := testharness.AllScenarioNames()
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, string(name))
	}
	return out
}

// writeCommandJSON emits one machine-readable object, failing loudly rather
// than printing a partial document.
func writeCommandJSON(stdout io.Writer, stderr io.Writer, label string, payload any) int {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "%s: encode result: %v\n", label, err)
		return exitFailure
	}
	fmt.Fprintln(stdout, string(encoded))
	return exitSuccess
}
