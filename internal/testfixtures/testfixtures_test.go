package testfixtures

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/redact"
)

// TestM22_001_002_SuitePyramidIsCoherent proves the declared test pyramid is
// internally consistent: five tiers, distinct build tags, distinct name
// prefixes, and exactly one tier that runs by default.
func TestM22_001_002_SuitePyramidIsCoherent(t *testing.T) {
	if err := ValidateSuiteDefinitions(); err != nil {
		t.Fatal(err)
	}
	for _, suite := range AllSuites() {
		if !suite.Valid() {
			t.Fatalf("suite %q is not valid", suite)
		}
		command := suite.SelectionCommand("./internal/...")
		if !strings.HasPrefix(command, "go test ") {
			t.Fatalf("suite %q selection command = %q", suite, command)
		}
		if suite == SuiteFastUnit {
			if strings.Contains(command, "-tags") {
				t.Fatal("the default tier must not require a build tag")
			}
			if suite.RequiresIsolation() {
				t.Fatal("the fast unit tier must be safe to run without isolation")
			}
			continue
		}
		if !strings.Contains(command, "-tags "+suite.BuildTag()) {
			t.Fatalf("suite %q command %q must select its build tag", suite, command)
		}
		if !suite.RequiresIsolation() {
			t.Fatalf("suite %q binds real resources and must declare isolation", suite)
		}
	}
	if SuiteSummary() == "" {
		t.Fatal("the pyramid must be renderable for diagnostics")
	}
}

// TestM22_003_DeterministicClocksAndIdentifiers proves time and identity are
// reproducible: two runs of the same fixture produce the same values.
func TestM22_003_DeterministicClocksAndIdentifiers(t *testing.T) {
	fixed := NewFixedClock()
	if !fixed.Now().Equal(FixtureEpoch) || !fixed.Now().Equal(fixed.Now()) {
		t.Fatal("a fixed clock must return the same instant every time")
	}
	if fixed.Now().Location() != time.UTC {
		t.Fatal("fixture time must be UTC so it cannot vary by machine")
	}

	if _, err := NewSteppingClock(0); err == nil {
		t.Fatal("a non-positive step must be rejected rather than silently pinned")
	}
	stepping, err := NewSteppingClock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first, second := stepping.Now(), stepping.Now()
	if !second.After(first) || second.Sub(first) != time.Second {
		t.Fatalf("stepping clock advanced %v, want exactly 1s", second.Sub(first))
	}

	if _, err := NewSequenceIDGenerator(""); err == nil {
		t.Fatal("an empty identifier prefix must be rejected")
	}
	generator, err := NewSequenceIDGenerator("task")
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewSequenceIDGenerator("task")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		if generator.Next() != again.Next() {
			t.Fatal("two generators with the same prefix must produce identical sequences")
		}
	}
	if generator.Count() != 5 {
		t.Fatalf("count = %d, want 5", generator.Count())
	}
}

// TestM22_004_005_006_ScriptedProviderReplaysExactlyWhatWasScripted proves
// the fake provider never improvises, scripts tool calls and failures, and
// accounts usage and cost exactly.
func TestM22_004_005_006_ScriptedProviderReplaysExactlyWhatWasScripted(t *testing.T) {
	if _, err := NewScriptedProvider(DefaultFixturePricing()); err == nil {
		t.Fatal("a provider with no scripted turns must be rejected")
	}
	if _, err := NewScriptedProvider(DefaultFixturePricing(), ScriptedTurn{}); err == nil {
		t.Fatal("a turn that is neither text, tool call, nor failure must be rejected")
	}
	if _, err := NewScriptedProvider(DefaultFixturePricing(), ScriptedTurn{ToolName: "read"}); err == nil {
		t.Fatal("a scripted tool call without arguments must be rejected")
	}

	scriptedFailure := errors.New("fixture: provider refused")
	provider, err := NewScriptedProvider(DefaultFixturePricing(),
		ScriptedTurn{Text: "I will read the file.", Usage: FixtureUsage{UncachedInputTokens: 1_000, OutputTokens: 500}},
		ScriptedTurn{ToolName: "read_file", ToolArgumentsJSON: `{"path":"internal/server/server.go"}`,
			Usage: FixtureUsage{UncachedInputTokens: 2_000, OutputTokens: 100}},
		ScriptedTurn{Err: scriptedFailure},
	)
	if err != nil {
		t.Fatal(err)
	}

	first, err := provider.Next(context.Background())
	if err != nil || first.Text == "" {
		t.Fatalf("first turn = %#v, err = %v", first, err)
	}
	second, err := provider.Next(context.Background())
	if err != nil || second.ToolName != "read_file" {
		t.Fatalf("second turn = %#v, err = %v", second, err)
	}
	if _, err := provider.Next(context.Background()); !errors.Is(err, scriptedFailure) {
		t.Fatalf("third turn err = %v, want the scripted failure", err)
	}
	if _, err := provider.Next(context.Background()); err == nil {
		t.Fatal("asking past the end of the script must fail loudly, not return an empty reply")
	}
	if provider.Remaining() != 0 {
		t.Fatalf("remaining = %d, want 0", provider.Remaining())
	}

	usage := provider.TotalUsage()
	if usage.UncachedInputTokens != 3_000 || usage.OutputTokens != 600 {
		t.Fatalf("usage = %#v", usage)
	}
	if usage.Total() != 3_600 {
		t.Fatalf("total tokens = %d, want 3600", usage.Total())
	}
	// 3000 input @100/M + 600 output @600/M = 300000 + 360000 = 660000 -> 1 minor unit.
	if cost := provider.TotalCostMinorUnits(); cost != 1 {
		t.Fatalf("cost = %d minor units, want 1", cost)
	}
	if DefaultFixturePricing().CostMinorUnits(FixtureUsage{}) != 0 {
		t.Fatal("zero usage must cost exactly zero")
	}
}

// TestM22_004_ScriptedProviderHonoursCancellation keeps a fake from ignoring
// a cancelled context, which would hide cancellation bugs in callers.
func TestM22_004_ScriptedProviderHonoursCancellation(t *testing.T) {
	provider, err := NewScriptedProvider(DefaultFixturePricing(), ScriptedTurn{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if provider.Served() != 0 {
		t.Fatal("a cancelled call must not consume a scripted turn")
	}
}

// TestM22_007_008_RealGitRepositoryFixtureBuilds proves the builder produces
// a real repository at a real revision with the expected content.
func TestM22_007_008_RealGitRepositoryFixtureBuilds(t *testing.T) {
	requireGit(t)
	ctx, cancel := context.WithTimeout(context.Background(), FixtureCommitTimeout)
	defer cancel()

	fixture, err := NewRepositoryFixture(ctx, filepath.Join(t.TempDir(), "clean"), CleanGoRepositoryFiles())
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Revision) != 40 {
		t.Fatalf("revision = %q, want a 40-character git object name", fixture.Revision)
	}
	for _, expected := range []string{"go.mod", "cmd/server/main.go", "internal/server/server_test.go"} {
		if _, err := os.Stat(filepath.Join(fixture.Root, filepath.FromSlash(expected))); err != nil {
			t.Fatalf("clean fixture is missing %s: %v", expected, err)
		}
	}
	status, err := runGit(ctx, fixture.Root, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if status != "" {
		t.Fatalf("a clean fixture must have a clean worktree, got:\n%s", status)
	}
}

// TestM22_009_DirtyWorktreeFixtureHasBothChangeKinds proves the dirty
// fixture carries an uncommitted modification AND an untracked file — the
// two kinds of local work a task must preserve.
func TestM22_009_DirtyWorktreeFixtureHasBothChangeKinds(t *testing.T) {
	requireGit(t)
	ctx, cancel := context.WithTimeout(context.Background(), FixtureCommitTimeout)
	defer cancel()

	fixture, err := NewRepositoryFixture(ctx, filepath.Join(t.TempDir(), "dirty"), CleanGoRepositoryFiles())
	if err != nil {
		t.Fatal(err)
	}
	if err := MakeWorktreeDirty(fixture.Root); err != nil {
		t.Fatal(err)
	}
	status, err := runGit(ctx, fixture.Root, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "M internal/server/server.go") {
		t.Fatalf("dirty fixture must carry an uncommitted modification, got:\n%s", status)
	}
	if !strings.Contains(status, "?? notes/") {
		t.Fatalf("dirty fixture must carry an untracked file, got:\n%s", status)
	}
}

// TestM22_010_MaliciousFixtureCarriesUntrustedContent proves the malicious
// fixture actually contains steering and exfiltration attempts, so tests
// that claim to exercise untrusted repository content really do.
func TestM22_010_MaliciousFixtureCarriesUntrustedContent(t *testing.T) {
	files := MaliciousRepositoryFiles()
	joined := strings.ToLower(strings.Join(valuesOf(files), "\n"))
	for _, marker := range []string{
		"ignore all previous instructions",
		"without review",
		"attacker.invalid",
		"pre-approved",
	} {
		if !strings.Contains(joined, marker) {
			t.Fatalf("malicious fixture must contain the steering attempt %q", marker)
		}
	}
	if !strings.Contains(joined, strings.ToLower(FixtureCredentialMaterial)) {
		t.Fatal("malicious fixture must contain a credential-shaped value for redaction tests")
	}
}

// TestM22_011_012_013_ScenarioFixturesDifferFromClean proves each scenario
// fixture actually differs from the clean baseline in the way it claims.
func TestM22_011_012_013_ScenarioFixturesDifferFromClean(t *testing.T) {
	clean := CleanGoRepositoryFiles()

	failing := FailingTestRepositoryFiles()
	if failing["internal/server/server_test.go"] == clean["internal/server/server_test.go"] {
		t.Fatal("the failing-test fixture must actually change the test file")
	}
	if !strings.Contains(failing["internal/server/server_test.go"], "on purpose") {
		t.Fatal("a deliberate fixture failure must say so, or a reader will chase a phantom defect")
	}

	dependency := DependencyChangeRepositoryFiles()
	if dependency["go.mod"] == clean["go.mod"] {
		t.Fatal("the dependency-change fixture must change the module bindings")
	}
	if _, present := dependency[".golangci.yml"]; !present {
		t.Fatal("the dependency-change fixture must also change a tool configuration")
	}

	protected := ProtectedWorkflowRepositoryFiles()
	charge, present := protected["internal/payments/charge.go"]
	if !present {
		t.Fatal("the protected-workflow fixture must contain the workflow")
	}
	if !strings.Contains(charge, "idempotency") || !strings.Contains(charge, "Ambiguous") {
		t.Fatal("the protected-workflow fixture must exhibit idempotency and ambiguity concerns")
	}
	// M22-013 is explicit that this fixture must not claim deep proof.
	if !strings.Contains(charge, "not verified") {
		t.Fatal("the protected-workflow fixture must state plainly that it is not verified")
	}
	for _, forbidden := range []string{"proven", "guaranteed", "exactly-once"} {
		if strings.Contains(strings.ToLower(charge), forbidden) {
			t.Fatalf("the protected-workflow fixture must not claim %q", forbidden)
		}
	}
}

// TestM22_014_FixturesCarryNoRealCredentialsAndAreRedacted proves every
// credential-shaped fixture value is synthetic AND is actually redacted by
// the real pipeline, so fixture content can never leak through a boundary.
func TestM22_014_FixturesCarryNoRealCredentialsAndAreRedacted(t *testing.T) {
	shapes := FixtureCredentialShapes()
	if len(shapes) == 0 {
		t.Fatal("the fixture credential inventory must not be empty")
	}
	for _, shape := range shapes {
		lowered := strings.ToLower(shape)
		if !strings.Contains(lowered, "fixture") && !strings.Contains(lowered, "not-a-real") {
			t.Fatalf("credential-shaped fixture %q must name itself synthetic", shape)
		}
	}

	secret, err := credentials.NewSecret([]byte(FixtureCredentialMaterial))
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Destroy()
	pipeline, err := redact.NewPipeline([]credentials.Secret{secret}, redact.Limits{
		MaximumInputBytes: 32 * 1024, MaximumOutputBytes: 16 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()

	for path, contents := range MaliciousRepositoryFiles() {
		result, err := pipeline.Redact(redact.BoundaryPromptPersistence, contents)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(result.Text, FixtureCredentialMaterial) {
			t.Fatalf("%s: fixture credential survived redaction", path)
		}
	}
}

// requireGit skips when git is unavailable, so the fast tier stays runnable
// on a machine without it rather than reporting a false failure.
func requireGit(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := runGit(ctx, t.TempDir(), "--version"); err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
}

func valuesOf(files map[string]string) []string {
	values := make([]string, 0, len(files))
	for _, value := range files {
		values = append(values, value)
	}
	return values
}
