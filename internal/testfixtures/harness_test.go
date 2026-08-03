//go:build integration

package testfixtures

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestM22_106_RepositoryStatesAreRealGitStates covers M22-106.
//
// Each state is verified by asking git, not by trusting the builder. A fixture
// that claims to be detached but is not would make every test built on it a
// test of nothing.
func TestM22_106_RepositoryStatesAreRealGitStates(t *testing.T) {
	for _, state := range AllRepositoryStates() {
		t.Run(string(state), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "repository")
			fixture, err := NewStatefulRepository(t.Context(), root, state)
			if err != nil {
				t.Fatalf("build %s repository: %v", state, err)
			}
			if fixture.State != state {
				t.Fatalf("fixture reports state %q, want %q", fixture.State, state)
			}
			if fixture.Revision == "" || len(fixture.Revision) != 40 {
				t.Fatalf("fixture revision = %q", fixture.Revision)
			}

			dirty, err := IsDirty(t.Context(), root)
			if err != nil {
				t.Fatalf("read dirty state: %v", err)
			}
			detached, err := IsDetached(t.Context(), root)
			if err != nil {
				t.Fatalf("read detached state: %v", err)
			}
			conflicted, err := HasUnresolvedConflicts(t.Context(), root)
			if err != nil {
				t.Fatalf("read conflict state: %v", err)
			}

			switch state {
			case StateClean, StateMalicious:
				if dirty || detached || conflicted {
					t.Fatalf("%s repository is dirty=%v detached=%v conflicted=%v",
						state, dirty, detached, conflicted)
				}
			case StateDirty:
				if !dirty {
					t.Fatal("dirty repository has no uncommitted changes")
				}
			case StateDetached:
				if !detached {
					t.Fatal("detached repository still has HEAD on a branch")
				}
			case StateConflicted:
				if !conflicted {
					t.Fatal("conflicted repository has no unmerged paths")
				}
				if len(fixture.ConflictedPaths) == 0 {
					t.Fatal("conflicted repository names no conflicted path")
				}
				contents, err := os.ReadFile(
					filepath.Join(root, filepath.FromSlash(fixture.ConflictedPaths[0])))
				if err != nil {
					t.Fatalf("read conflicted file: %v", err)
				}
				for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>"} {
					if !strings.Contains(string(contents), marker) {
						t.Fatalf("conflicted file lacks marker %q", marker)
					}
				}
			case StateNested:
				if fixture.InnerRoot == "" {
					t.Fatal("nested repository names no inner root")
				}
				if _, err := os.Stat(filepath.Join(fixture.InnerRoot, ".git")); err != nil {
					t.Fatalf("inner repository has no .git: %v", err)
				}
				// The inner repository must be a genuinely separate one, or
				// the boundary the coordinator has to respect does not exist.
				if !strings.HasPrefix(fixture.InnerRoot, root) {
					t.Fatalf("inner root %q is not inside %q", fixture.InnerRoot, root)
				}
			}
		})
	}
}

// TestM22_106_OnlyCleanIsSafeToOperateOn pins the fixture's claim about what
// each state means, so the meaning cannot drift between suites.
func TestM22_106_OnlyCleanIsSafeToOperateOn(t *testing.T) {
	for _, state := range AllRepositoryStates() {
		safe := state.SafeToOperateOn()
		if state == StateClean && !safe {
			t.Fatal("a clean repository is not considered safe")
		}
		if state != StateClean && safe {
			t.Fatalf("%s is considered safe to operate on", state)
		}
	}
	if RepositoryState("invented").Valid() {
		t.Fatal("an unknown repository state validated")
	}
	if RepositoryState("invented").SafeToOperateOn() {
		t.Fatal("an unknown repository state is treated as safe")
	}
}

// TestM22_107_ScriptedStepsCoverTheWholeProviderSurface covers M22-107.
func TestM22_107_ScriptedStepsCoverTheWholeProviderSurface(t *testing.T) {
	provider, err := NewStepProvider(FullCoverageScript()...)
	if err != nil {
		t.Fatalf("build step provider: %v", err)
	}

	seen := map[StepKind]bool{}
	for provider.Remaining() > 0 {
		step, stepErr := provider.Next(t.Context())
		seen[step.Kind] = true
		switch step.Kind {
		case StepText, StepToolCall, StepUsageOnly, StepDelay:
			if stepErr != nil {
				t.Fatalf("successful step %s returned %v", step.Kind, stepErr)
			}
		case StepRateLimit:
			if !errors.Is(stepErr, ErrFixtureRateLimited) {
				t.Fatalf("rate-limit step returned %v", stepErr)
			}
		case StepAuthFailure:
			if !errors.Is(stepErr, ErrFixtureAuthentication) {
				t.Fatalf("auth-failure step returned %v", stepErr)
			}
		case StepCancellation:
			if !errors.Is(stepErr, ErrFixtureCancelled) {
				t.Fatalf("cancellation step returned %v", stepErr)
			}
		case StepPartialStream:
			if !errors.Is(stepErr, ErrFixturePartialStream) {
				t.Fatalf("partial-stream step returned %v", stepErr)
			}
			if step.Text == "" {
				t.Fatal("partial stream carried no output emitted before the break")
			}
		}
	}
	for _, kind := range AllStepKinds() {
		if !seen[kind] {
			t.Fatalf("the full-coverage script never served %s", kind)
		}
	}

	// A scripted delay must cost fixture time, not wall time.
	if provider.Elapsed() != 2*time.Second {
		t.Fatalf("scripted delay advanced the fixture clock by %v, want 2s", provider.Elapsed())
	}
	// Usage from failed steps must still be counted; tokens are spent whether
	// or not the call succeeded.
	usage := provider.UsageServed()
	if usage.Total() == 0 {
		t.Fatal("no usage was accounted across the script")
	}
	if usage.OutputTokens < 25 {
		t.Fatalf("the partial stream's output tokens were not counted: %+v", usage)
	}
}

// TestM22_107_StepRetryabilityIsClassified proves the fixture states which
// failures may be retried. A system that retried an authentication failure
// would loop against a wrong key forever.
func TestM22_107_StepRetryabilityIsClassified(t *testing.T) {
	for kind, want := range map[StepKind]bool{
		StepRateLimit:     true,
		StepDelay:         true,
		StepText:          true,
		StepAuthFailure:   false,
		StepCancellation:  false,
		StepPartialStream: false,
	} {
		if got := kind.Retryable(); got != want {
			t.Fatalf("%s retryable = %v, want %v", kind, got, want)
		}
	}
	for kind, want := range map[StepKind]bool{
		StepAuthFailure:   true,
		StepCancellation:  true,
		StepPartialStream: true,
		StepText:          false,
		StepRateLimit:     false,
	} {
		if got := kind.Terminal(); got != want {
			t.Fatalf("%s terminal = %v, want %v", kind, got, want)
		}
	}
}

// TestM22_107_StepProviderRefusesToImprovise proves the fake never invents a
// response, and that a malformed step is rejected at construction.
func TestM22_107_StepProviderRefusesToImprovise(t *testing.T) {
	provider, err := NewStepProvider(ProviderStep{Kind: StepText, Text: "only turn"})
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}
	if _, err := provider.Next(t.Context()); err != nil {
		t.Fatalf("first step: %v", err)
	}
	if _, err := provider.Next(t.Context()); err == nil {
		t.Fatal("an exhausted provider invented a response")
	}

	malformed := []ProviderStep{
		{Kind: StepText},
		{Kind: StepToolCall, ToolName: "read-file"},
		{Kind: StepPartialStream},
		{Kind: StepDelay},
		{Kind: StepRateLimit},
		{Kind: StepKind("invented")},
	}
	for index, step := range malformed {
		if _, err := NewStepProvider(step); err == nil {
			t.Fatalf("malformed step %d was accepted", index)
		}
	}
	if _, err := NewStepProvider(); err == nil {
		t.Fatal("an empty script was accepted")
	}

	// A cancelled context wins over the script.
	ready, err := NewStepProvider(ProviderStep{Kind: StepText, Text: "unreached"})
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ready.Next(cancelled); err == nil {
		t.Fatal("a cancelled context still served a step")
	}
}

// TestM22_108_CredentialStoreDetectsBoundaryCrossings covers M22-108.
func TestM22_108_CredentialStoreDetectsBoundaryCrossings(t *testing.T) {
	store := NewFakeCredentialStore()
	if err := store.Seed(); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	material, ok := store.Get("provider-api-key")
	if !ok {
		t.Fatal("seeded credential is not retrievable")
	}

	// Clean text crosses freely.
	for _, boundary := range AllCredentialBoundaries() {
		if err := store.Inspect(boundary, "clean output", "nothing secret here"); err != nil {
			t.Fatalf("clean text was flagged at %s: %v", boundary, err)
		}
	}
	if err := store.AssertNoCrossings(); err != nil {
		t.Fatalf("clean text recorded a crossing: %v", err)
	}

	// The secret is caught at every watched boundary.
	for _, boundary := range AllCredentialBoundaries() {
		err := store.Inspect(boundary, "provider error message", "auth failed for "+material)
		if !errors.Is(err, ErrCredentialCrossedBoundary) {
			t.Fatalf("a secret crossed %s undetected: %v", boundary, err)
		}
	}
	crossings := store.Crossings()
	if len(crossings) != len(AllCredentialBoundaries()) {
		t.Fatalf("recorded %d crossings, want %d", len(crossings), len(AllCredentialBoundaries()))
	}
	// The report itself must not leak what it caught.
	report := store.AssertNoCrossings()
	if report == nil {
		t.Fatal("crossings were recorded but not reported")
	}
	if strings.Contains(report.Error(), material) {
		t.Fatalf("the leak report leaked the secret: %v", report)
	}
}

// TestM22_108_CredentialStoreRefusesNonSyntheticMaterial proves a real
// credential cannot be committed to a fixture through this door.
func TestM22_108_CredentialStoreRefusesNonSyntheticMaterial(t *testing.T) {
	store := NewFakeCredentialStore()
	for _, material := range []string{
		"sk-live-abcdefghijklmnopqrstuvwxyz",
		"AKIAIOSFODNN7EXAMPLE",
		"hunter2hunter2",
	} {
		if err := store.Put("candidate", material); err == nil {
			t.Fatalf("non-synthetic material %q was accepted", material)
		}
	}
	if err := store.Put("", FixtureCredentialMaterial); err == nil {
		t.Fatal("an unnamed credential was accepted")
	}
	if err := store.Put("short", "fixture"); err == nil {
		t.Fatal("material too short to be distinguishable was accepted")
	}
	if err := store.Put("ok", "fixture-not-a-real-credential-value"); err != nil {
		t.Fatalf("synthetic material was refused: %v", err)
	}
	if names := store.Names(); len(names) != 1 || names[0] != "ok" {
		t.Fatalf("store names = %v", names)
	}
}

// TestM22_109_EventRecorderAssertsOrderCausationAndAtomicity covers M22-109.
func TestM22_109_EventRecorderAssertsOrderCausationAndAtomicity(t *testing.T) {
	recorder := NewEventRecorder(NewFixedClock())
	events := []RecordedEvent{
		{ID: "e1", Kind: "task-created", TransactionID: "t1", Published: true},
		{ID: "e2", Kind: "plan-created", CausationID: "e1", TransactionID: "t1", Published: true},
		{ID: "e3", Kind: "tool-started", CausationID: "e2", TransactionID: "t2", Published: true},
	}
	for _, event := range events {
		if _, err := recorder.Record(event); err != nil {
			t.Fatalf("record %s: %v", event.ID, err)
		}
	}

	if err := recorder.AssertSequenceIsContiguous(); err != nil {
		t.Fatalf("sequence: %v", err)
	}
	if err := recorder.AssertCausationIsResolvable(); err != nil {
		t.Fatalf("causation: %v", err)
	}
	for _, transaction := range []string{"t1", "t2"} {
		if err := recorder.AssertTransactionIsAtomic(transaction); err != nil {
			t.Fatalf("transaction %s: %v", transaction, err)
		}
	}
	if err := recorder.AssertReplayMatches(recorder.Events()); err != nil {
		t.Fatalf("identity replay: %v", err)
	}

	// A duplicate identity is refused rather than silently double-counted.
	if _, err := recorder.Record(RecordedEvent{ID: "e1", Kind: "task-created"}); err == nil {
		t.Fatal("a duplicate event identity was accepted")
	}
	// So is an unusable event.
	if _, err := recorder.Record(RecordedEvent{Kind: "no-identity"}); err == nil {
		t.Fatal("an event with no identity was accepted")
	}
	if _, err := recorder.Record(RecordedEvent{ID: "e9"}); err == nil {
		t.Fatal("an event with no kind was accepted")
	}
}

// TestM22_109_EventRecorderCatchesTheFailuresItExistsFor proves each assertion
// actually fails on the defect it names, rather than always returning nil.
func TestM22_109_EventRecorderCatchesTheFailuresItExistsFor(t *testing.T) {
	t.Run("dangling causation", func(t *testing.T) {
		recorder := NewEventRecorder(NewFixedClock())
		if _, err := recorder.Record(RecordedEvent{
			ID: "e1", Kind: "effect", CausationID: "never-recorded",
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
		if err := recorder.AssertCausationIsResolvable(); err == nil {
			t.Fatal("an effect with no recorded cause was accepted")
		}
	})

	t.Run("self causation", func(t *testing.T) {
		recorder := NewEventRecorder(NewFixedClock())
		if _, err := recorder.Record(RecordedEvent{
			ID: "e1", Kind: "loop", CausationID: "e1",
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
		if err := recorder.AssertCausationIsResolvable(); err == nil {
			t.Fatal("a self-causing event was accepted")
		}
	})

	t.Run("partially published transaction", func(t *testing.T) {
		recorder := NewEventRecorder(NewFixedClock())
		for _, event := range []RecordedEvent{
			{ID: "e1", Kind: "a", TransactionID: "t1", Published: true},
			{ID: "e2", Kind: "b", TransactionID: "t1", Published: false},
		} {
			if _, err := recorder.Record(event); err != nil {
				t.Fatalf("record: %v", err)
			}
		}
		if err := recorder.AssertTransactionIsAtomic("t1"); err == nil {
			t.Fatal("a half-published transaction was accepted")
		}
		if err := recorder.AssertTransactionIsAtomic("absent"); err == nil {
			t.Fatal("a transaction with no events was accepted")
		}
	})

	t.Run("replay mismatch", func(t *testing.T) {
		recorder := NewEventRecorder(NewFixedClock())
		for _, event := range []RecordedEvent{
			{ID: "e1", Kind: "a"}, {ID: "e2", Kind: "b"},
		} {
			if _, err := recorder.Record(event); err != nil {
				t.Fatalf("record: %v", err)
			}
		}
		if err := recorder.AssertReplayMatches(nil); err == nil {
			t.Fatal("an empty replay matched a two-event stream")
		}
		reordered := []RecordedEvent{{Sequence: 1, Kind: "b"}, {Sequence: 2, Kind: "a"}}
		if err := recorder.AssertReplayMatches(reordered); err == nil {
			t.Fatal("a reordered replay matched")
		}
	})
}

// TestM22_109_WaitForFailsUsefullyRatherThanHanging proves an unmet wait ends
// with a message naming what was actually recorded.
func TestM22_109_WaitForFailsUsefullyRatherThanHanging(t *testing.T) {
	recorder := NewEventRecorder(NewFixedClock())
	if _, err := recorder.Record(RecordedEvent{ID: "e1", Kind: "task-created"}); err != nil {
		t.Fatalf("record: %v", err)
	}

	// An already-recorded event satisfies a wait immediately.
	found, err := recorder.WaitFor(t.Context(), "task-created",
		func(event RecordedEvent) bool { return event.Kind == "task-created" })
	if err != nil {
		t.Fatalf("wait for recorded event: %v", err)
	}
	if found.ID != "e1" {
		t.Fatalf("wait returned %q", found.ID)
	}

	// A later event satisfies a pending wait.
	go func() {
		_, _ = recorder.Record(RecordedEvent{ID: "e2", Kind: "plan-created"})
	}()
	if _, err := recorder.WaitFor(t.Context(), "plan-created",
		func(event RecordedEvent) bool { return event.Kind == "plan-created" }); err != nil {
		t.Fatalf("wait for later event: %v", err)
	}

	// An event that never arrives times out with a useful message.
	bounded, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = recorder.WaitFor(bounded, "task-completed",
		func(event RecordedEvent) bool { return event.Kind == "task-completed" })
	if !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("wait error = %v, want ErrWaitTimeout", err)
	}
	for _, expected := range []string{"task-completed", "task-created", "plan-created"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("timeout message does not mention %q: %v", expected, err)
		}
	}

	// Finishing releases an outstanding waiter rather than hanging the suite.
	recorder.Finish()
	if _, err := recorder.Record(RecordedEvent{ID: "e3", Kind: "late"}); err == nil {
		t.Fatal("a finished recorder accepted an event")
	}
}

// TestM22_112_CleanupRefusesEveryUnsafeTarget covers M22-112.
func TestM22_112_CleanupRefusesEveryUnsafeTarget(t *testing.T) {
	unsafe := []string{
		"",
		" ",
		"relative/path",
		string(filepath.Separator),
		filepath.Clean(os.TempDir()),
		filepath.Join(os.TempDir(), "..", "elsewhere"),
		func() string {
			home, err := os.UserHomeDir()
			if err != nil {
				return filepath.Join(os.TempDir(), "..", "home")
			}
			return home
		}(),
	}
	for _, target := range unsafe {
		if err := ValidateCleanupTarget(target); !errors.Is(err, ErrUnsafeCleanupTarget) {
			t.Fatalf("unsafe target %q validated with %v", target, err)
		}
		if err := RemoveFixtureDirectory(target, false); !errors.Is(err, ErrUnsafeCleanupTarget) {
			t.Fatalf("unsafe target %q was passed to a delete: %v", target, err)
		}
	}

	// A file, rather than a directory, is refused.
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := ValidateCleanupTarget(file); !errors.Is(err, ErrUnsafeCleanupTarget) {
		t.Fatalf("a file validated as a cleanup target: %v", err)
	}
}

// TestM22_112_CleanupRemovesFixturesAndHonoursPreservation proves the
// validation is not simply refusing everything.
func TestM22_112_CleanupRemovesFixturesAndHonoursPreservation(t *testing.T) {
	root := t.TempDir()

	removable := filepath.Join(root, "removable")
	if err := os.MkdirAll(filepath.Join(removable, "nested"), 0o755); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if err := ValidateCleanupTarget(removable); err != nil {
		t.Fatalf("a legitimate fixture directory was refused: %v", err)
	}
	if err := RemoveFixtureDirectory(removable, false); err != nil {
		t.Fatalf("remove fixture: %v", err)
	}
	if _, err := os.Stat(removable); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture directory survived cleanup: %v", err)
	}

	// Preservation must be explicit and must actually preserve.
	preserved := filepath.Join(root, "preserved")
	if err := os.MkdirAll(preserved, 0o755); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if err := RemoveFixtureDirectory(preserved, true); err != nil {
		t.Fatalf("preserve fixture: %v", err)
	}
	if _, err := os.Stat(preserved); err != nil {
		t.Fatalf("preserved fixture was deleted: %v", err)
	}

	// An already-removed directory is a successful cleanup, not an error.
	if err := RemoveFixtureDirectory(filepath.Join(root, "never-existed"), false); err != nil {
		t.Fatalf("cleaning an absent directory failed: %v", err)
	}
}

// TestM22_105_DatabaseFixtureIsRealMigratedAndVerified covers M22-105.
func TestM22_105_DatabaseFixtureIsRealMigratedAndVerified(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "database")
	fixture, err := NewDatabaseFixture(t.Context(), DatabaseFixtureOptions{Directory: directory})
	if err != nil {
		t.Fatalf("build database fixture: %v", err)
	}
	t.Cleanup(func() { _ = fixture.Close() })

	if fixture.AppliedCount == 0 || fixture.LatestVersion == 0 {
		t.Fatalf("fixture applied %d migrations to version %d",
			fixture.AppliedCount, fixture.LatestVersion)
	}
	if _, err := os.Stat(fixture.Path); err != nil {
		t.Fatalf("fixture database file: %v", err)
	}
	if err := fixture.AssertIntegrity(t.Context()); err != nil {
		t.Fatalf("integrity: %v", err)
	}

	// The schema must be the real one, not an empty database that passed an
	// integrity check by having nothing in it.
	tables, err := fixture.TableNames(t.Context())
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	if len(tables) < 20 {
		t.Fatalf("fixture schema has only %d tables: %v", len(tables), tables)
	}
	for _, required := range []string{"tasks", "threads", "task_events", "approvals"} {
		found := false
		for _, table := range tables {
			if table == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("fixture schema is missing table %q", required)
		}
	}
}

// TestM22_105_DatabaseFixtureCanBuildThePriorSchema proves the stop-at-version
// control works, which is what makes an upgrade testable.
func TestM22_105_DatabaseFixtureCanBuildThePriorSchema(t *testing.T) {
	full, err := NewDatabaseFixture(t.Context(), DatabaseFixtureOptions{
		Directory: filepath.Join(t.TempDir(), "full"),
	})
	if err != nil {
		t.Fatalf("build full fixture: %v", err)
	}
	t.Cleanup(func() { _ = full.Close() })
	if full.LatestVersion < 2 {
		t.Skipf("catalog has only %d migrations", full.LatestVersion)
	}

	prior, err := NewDatabaseFixture(t.Context(), DatabaseFixtureOptions{
		Directory:     filepath.Join(t.TempDir(), "prior"),
		StopAtVersion: full.LatestVersion - 1,
	})
	if err != nil {
		t.Fatalf("build prior fixture: %v", err)
	}
	t.Cleanup(func() { _ = prior.Close() })
	if prior.LatestVersion >= full.LatestVersion {
		t.Fatalf("prior schema is at version %d, full is at %d",
			prior.LatestVersion, full.LatestVersion)
	}
	if prior.AppliedCount >= full.AppliedCount {
		t.Fatalf("prior applied %d migrations, full applied %d",
			prior.AppliedCount, full.AppliedCount)
	}
}

// TestM22_105_DatabaseFixtureRefusesUnsafeDirectories proves the fixture will
// not create a database somewhere its own cleanup would refuse to delete,
// which is how a fixture ends up leaking files forever.
func TestM22_105_DatabaseFixtureRefusesUnsafeDirectories(t *testing.T) {
	for _, directory := range []string{"", "relative", os.TempDir()} {
		if _, err := NewDatabaseFixture(t.Context(), DatabaseFixtureOptions{
			Directory: directory,
		}); err == nil {
			t.Fatalf("unsafe directory %q was accepted", directory)
		}
	}
}

// TestM22_105_DatabaseFixtureCleansUpAfterItself proves the registered cleanup
// actually removes the database.
func TestM22_105_DatabaseFixtureCleansUpAfterItself(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "disposable")
	fixture, err := NewDatabaseFixture(t.Context(), DatabaseFixtureOptions{Directory: directory})
	if err != nil {
		t.Fatalf("build fixture: %v", err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture directory survived close: %v", err)
	}

	preserved := filepath.Join(t.TempDir(), "kept")
	keep, err := NewDatabaseFixture(t.Context(), DatabaseFixtureOptions{
		Directory: preserved, PreserveOnCleanup: true,
	})
	if err != nil {
		t.Fatalf("build preserved fixture: %v", err)
	}
	if err := keep.Close(); err != nil {
		t.Fatalf("close preserved fixture: %v", err)
	}
	if _, err := os.Stat(preserved); err != nil {
		t.Fatalf("preserved fixture was deleted: %v", err)
	}
}
