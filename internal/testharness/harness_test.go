package testharness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// TestM22_110_CoordinatorHarnessIsFullyIsolated covers M22-110.
func TestM22_110_CoordinatorHarnessIsFullyIsolated(t *testing.T) {
	root := filepath.Join(t.TempDir(), "harness")
	harness, err := NewCoordinatorHarness(t.Context(), CoordinatorOptions{Root: root})
	if err != nil {
		t.Fatalf("build harness: %v", err)
	}
	t.Cleanup(func() { _ = harness.Close(context.Background()) })

	if err := harness.AssertIsolated(); err != nil {
		t.Fatalf("harness is not isolated: %v", err)
	}
	if harness.Repositories() == nil {
		t.Fatal("harness exposes no repositories")
	}
	if harness.Repository.State != testfixtures.StateClean {
		t.Fatalf("default repository state = %q", harness.Repository.State)
	}
	if harness.Provider == nil || harness.Provider.Remaining() == 0 {
		t.Fatal("harness has no scripted provider")
	}
	if harness.Clock == nil || harness.IDs == nil || harness.Events == nil {
		t.Fatal("harness is missing a deterministic component")
	}
	// The clock must be the deterministic one, not wall time.
	first := harness.Clock.Now()
	second := harness.Clock.Now()
	if !second.After(first) {
		t.Fatalf("harness clock did not advance: %v then %v", first, second)
	}
	if first.Before(testfixtures.FixtureEpoch) {
		t.Fatalf("harness clock started at %v, before the fixture epoch", first)
	}
	if err := harness.AssertNoCredentialLeak(); err != nil {
		t.Fatalf("a fresh harness reports a credential leak: %v", err)
	}
}

// TestM22_110_ConcurrentHarnessesDoNotObserveEachOther is the property that
// makes the harness usable: two running at once must be independent, or test
// failures become a function of which tests happened to run together.
func TestM22_110_ConcurrentHarnessesDoNotObserveEachOther(t *testing.T) {
	first, err := NewCoordinatorHarness(t.Context(), CoordinatorOptions{
		Root: filepath.Join(t.TempDir(), "first"),
	})
	if err != nil {
		t.Fatalf("build first harness: %v", err)
	}
	t.Cleanup(func() { _ = first.Close(context.Background()) })

	second, err := NewCoordinatorHarness(t.Context(), CoordinatorOptions{
		Root: filepath.Join(t.TempDir(), "second"),
	})
	if err != nil {
		t.Fatalf("build second harness: %v", err)
	}
	t.Cleanup(func() { _ = second.Close(context.Background()) })

	if first.BrowserAddress() == second.BrowserAddress() {
		t.Fatalf("both harnesses bound %s", first.BrowserAddress())
	}
	if first.TaskControlAddress() == second.TaskControlAddress() {
		t.Fatalf("both harnesses bound task control on %s", first.TaskControlAddress())
	}
	if first.Repository.Root == second.Repository.Root {
		t.Fatal("both harnesses share a repository")
	}
	if first.Repositories() == second.Repositories() {
		t.Fatal("both harnesses share a database")
	}
	// Session secrets are per-launch, so two harnesses must not share one.
	if first.Application.BrowserSessionSecret() == second.Application.BrowserSessionSecret() {
		t.Fatal("both harnesses share a session secret")
	}
}

// TestM22_110_HarnessBuildsEveryRepositoryState proves the harness can put the
// coordinator in front of each condition a user can arrive in.
func TestM22_110_HarnessBuildsEveryRepositoryState(t *testing.T) {
	for _, state := range testfixtures.AllRepositoryStates() {
		t.Run(string(state), func(t *testing.T) {
			harness, err := NewCoordinatorHarness(t.Context(), CoordinatorOptions{
				Root:            filepath.Join(t.TempDir(), "harness"),
				RepositoryState: state,
			})
			if err != nil {
				t.Fatalf("build harness in %s state: %v", state, err)
			}
			t.Cleanup(func() { _ = harness.Close(context.Background()) })
			if harness.Repository.State != state {
				t.Fatalf("harness repository state = %q, want %q",
					harness.Repository.State, state)
			}
		})
	}
}

// TestM22_110_HarnessRefusesUnsafeRootsAndClosesIdempotently covers the
// lifecycle contract.
func TestM22_110_HarnessRefusesUnsafeRootsAndClosesIdempotently(t *testing.T) {
	for _, root := range []string{"", "relative/path", os.TempDir()} {
		if _, err := NewCoordinatorHarness(t.Context(), CoordinatorOptions{
			Root: root,
		}); err == nil {
			t.Fatalf("unsafe root %q was accepted", root)
		}
	}

	root := filepath.Join(t.TempDir(), "closable")
	harness, err := NewCoordinatorHarness(t.Context(), CoordinatorOptions{Root: root})
	if err != nil {
		t.Fatalf("build harness: %v", err)
	}
	if err := harness.Close(t.Context()); err != nil {
		t.Fatalf("close harness: %v", err)
	}
	// A second close must be a no-op rather than a double shutdown.
	if err := harness.Close(t.Context()); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("harness root survived close: %v", err)
	}
}

// scriptedInspector answers a scenario's questions from a fixed script, so the
// browser harness itself can be tested without Chromium.
type scriptedInspector struct {
	unnamed      []string
	unlabelled   []string
	colourOnly   []string
	focusOrder   []string
	focusIndex   int
	focusVisible bool
	appliedSeq   map[uint64]bool
	pressed      []string
	deliverErr   error
}

func newScriptedInspector() *scriptedInspector {
	return &scriptedInspector{
		focusVisible: true,
		appliedSeq:   map[uint64]bool{},
	}
}

func (inspector *scriptedInspector) UnnamedControls() ([]string, error) {
	return inspector.unnamed, nil
}

func (inspector *scriptedInspector) UnlabelledRegions() ([]string, error) {
	return inspector.unlabelled, nil
}

func (inspector *scriptedInspector) ColourOnlyStates() ([]string, error) {
	return inspector.colourOnly, nil
}

func (inspector *scriptedInspector) FocusedTestID() (string, error) {
	if len(inspector.focusOrder) == 0 {
		return "", nil
	}
	index := inspector.focusIndex - 1
	if index < 0 {
		index = 0
	}
	return inspector.focusOrder[index%len(inspector.focusOrder)], nil
}

func (inspector *scriptedInspector) FocusIsVisible() (bool, error) {
	return inspector.focusVisible, nil
}

func (inspector *scriptedInspector) DeliverEvent(event ScenarioEvent) (bool, error) {
	if inspector.deliverErr != nil {
		return false, inspector.deliverErr
	}
	if inspector.appliedSeq[event.Sequence] {
		return false, nil
	}
	inspector.appliedSeq[event.Sequence] = true
	return true, nil
}

func (inspector *scriptedInspector) PressKey(key string) error {
	inspector.pressed = append(inspector.pressed, key)
	inspector.focusIndex++
	return nil
}

func healthyScenario() BrowserScenario {
	return BrowserScenario{
		Name: "plan approval",
		Bootstrap: SessionBootstrap{
			ConnectionState: "live", Route: "/tasks",
			SelectedThread: "thr_1", SelectedTask: "tsk_1",
		},
		Events: []ScenarioEvent{
			{Sequence: 1, Kind: "task-created"},
			{Sequence: 2, Kind: "plan-created"},
			{Sequence: 2, Kind: "plan-created", Duplicate: true},
		},
		Keys: []KeyAction{
			{Key: "Tab", ExpectFocusTestID: "approve-plan"},
			{Key: "Enter"},
		},
	}
}

// TestM22_111_BrowserHarnessRunsAScenarioAndWritesNoArtifactOnSuccess covers
// the M22-111 happy path.
func TestM22_111_BrowserHarnessRunsAScenarioAndWritesNoArtifactOnSuccess(t *testing.T) {
	artifacts := filepath.Join(t.TempDir(), "artifacts")
	harness, err := NewBrowserHarness(BrowserHarnessOptions{ArtifactRoot: artifacts})
	if err != nil {
		t.Fatalf("build browser harness: %v", err)
	}
	t.Cleanup(func() { _ = harness.Close() })

	inspector := newScriptedInspector()
	inspector.focusOrder = []string{"approve-plan", "request-change"}

	result, err := harness.Run(healthyScenario(), inspector)
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	if result.Failed {
		t.Fatalf("healthy scenario failed: %+v", result)
	}
	if result.EventsDelivered != 2 {
		t.Fatalf("delivered %d events, want 2", result.EventsDelivered)
	}
	// The deliberate duplicate must be filtered, not applied twice.
	if result.DuplicatesFiltered != 1 {
		t.Fatalf("filtered %d duplicates, want 1", result.DuplicatesFiltered)
	}
	if result.KeysDriven != 2 {
		t.Fatalf("drove %d keys, want 2", result.KeysDriven)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("healthy scenario produced findings: %+v", result.Findings)
	}
	// A passing scenario writes nothing: an artifact directory full of
	// successes is noise nobody reads.
	entries, err := os.ReadDir(artifacts)
	if err != nil {
		t.Fatalf("read artifact root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a passing scenario wrote %d artifacts", len(entries))
	}
}

// TestM22_111_BrowserHarnessWritesAnArtifactOnFailure proves the failure path
// leaves something reproducible behind.
func TestM22_111_BrowserHarnessWritesAnArtifactOnFailure(t *testing.T) {
	artifacts := filepath.Join(t.TempDir(), "artifacts")
	captured := ""
	harness, err := NewBrowserHarness(BrowserHarnessOptions{
		ArtifactRoot: artifacts,
		Capture: func(scenario, path string) error {
			captured = scenario
			return os.WriteFile(path, []byte("screenshot for "+scenario), 0o600)
		},
	})
	if err != nil {
		t.Fatalf("build browser harness: %v", err)
	}
	t.Cleanup(func() { _ = harness.Close() })

	inspector := newScriptedInspector()
	inspector.unnamed = []string{"button-3"}
	inspector.unlabelled = []string{"aside-1"}
	inspector.colourOnly = []string{"status-2"}
	inspector.focusOrder = []string{"approve-plan", "request-change"}

	result, err := harness.Run(healthyScenario(), inspector)
	if err == nil {
		t.Fatal("a scenario with accessibility findings passed")
	}
	if !result.Failed {
		t.Fatal("result does not record the failure")
	}
	if len(result.Findings) != 3 {
		t.Fatalf("recorded %d findings, want 3: %+v", len(result.Findings), result.Findings)
	}
	// Findings must be ordered so a reader sees the same report every run.
	rules := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		rules = append(rules, finding.Rule)
	}
	if !sortedAscending(rules) {
		t.Fatalf("findings are not deterministically ordered: %v", rules)
	}
	if captured != "plan approval" {
		t.Fatalf("capture hook received %q", captured)
	}
	if len(result.ArtifactPaths) != 1 {
		t.Fatalf("failure produced %d artifacts, want 1", len(result.ArtifactPaths))
	}
	contents, err := os.ReadFile(result.ArtifactPaths[0])
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !strings.Contains(string(contents), "plan approval") {
		t.Fatalf("artifact does not name the scenario: %q", contents)
	}
}

// TestM22_111_BrowserHarnessCatchesTheFailuresItExistsFor proves each guard
// fires on its own defect.
func TestM22_111_BrowserHarnessCatchesTheFailuresItExistsFor(t *testing.T) {
	build := func(t *testing.T) *BrowserHarness {
		t.Helper()
		harness, err := NewBrowserHarness(BrowserHarnessOptions{
			ArtifactRoot: filepath.Join(t.TempDir(), "artifacts"),
		})
		if err != nil {
			t.Fatalf("build harness: %v", err)
		}
		t.Cleanup(func() { _ = harness.Close() })
		return harness
	}

	t.Run("dropped event that was not a duplicate", func(t *testing.T) {
		harness := build(t)
		inspector := newScriptedInspector()
		// Pre-mark sequence 1 applied, so delivering it reads as a filter of a
		// non-duplicate: the page dropped an event it should have applied.
		inspector.appliedSeq[1] = true
		scenario := healthyScenario()
		if _, err := harness.Run(scenario, inspector); err == nil {
			t.Fatal("a dropped non-duplicate event passed")
		}
	})

	t.Run("focus landed on the wrong element", func(t *testing.T) {
		harness := build(t)
		inspector := newScriptedInspector()
		inspector.focusOrder = []string{"something-else"}
		if _, err := harness.Run(healthyScenario(), inspector); err == nil {
			t.Fatal("misplaced focus passed")
		}
	})

	t.Run("focus is not visible", func(t *testing.T) {
		harness := build(t)
		inspector := newScriptedInspector()
		inspector.focusOrder = []string{"approve-plan", "request-change"}
		inspector.focusVisible = false
		result, err := harness.Run(healthyScenario(), inspector)
		if err == nil {
			t.Fatal("invisible focus passed")
		}
		found := false
		for _, finding := range result.Findings {
			if finding.Rule == "focus-is-visible" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no focus-visibility finding was recorded: %+v", result.Findings)
		}
	})

	t.Run("inspector error", func(t *testing.T) {
		harness := build(t)
		inspector := newScriptedInspector()
		inspector.deliverErr = errors.New("transport closed")
		if _, err := harness.Run(healthyScenario(), inspector); err == nil {
			t.Fatal("an inspector error passed")
		}
	})

	t.Run("missing inspector", func(t *testing.T) {
		harness := build(t)
		if _, err := harness.Run(healthyScenario(), nil); err == nil {
			t.Fatal("a run with no inspector was accepted")
		}
	})
}

// TestM22_111_ScenarioValidationRejectsUnrunnableScenarios proves a scenario is
// checked before anything is driven.
func TestM22_111_ScenarioValidationRejectsUnrunnableScenarios(t *testing.T) {
	bad := map[string]func(BrowserScenario) BrowserScenario{
		"no name": func(scenario BrowserScenario) BrowserScenario {
			scenario.Name = ""
			return scenario
		},
		"unknown connection state": func(scenario BrowserScenario) BrowserScenario {
			scenario.Bootstrap.ConnectionState = "invented"
			return scenario
		},
		"relative route": func(scenario BrowserScenario) BrowserScenario {
			scenario.Bootstrap.Route = "tasks"
			return scenario
		},
		"task without thread": func(scenario BrowserScenario) BrowserScenario {
			scenario.Bootstrap.SelectedThread = ""
			return scenario
		},
		"event without sequence": func(scenario BrowserScenario) BrowserScenario {
			scenario.Events = []ScenarioEvent{{Kind: "task-created"}}
			return scenario
		},
		"event without kind": func(scenario BrowserScenario) BrowserScenario {
			scenario.Events = []ScenarioEvent{{Sequence: 1}}
			return scenario
		},
		"unmarked repeat": func(scenario BrowserScenario) BrowserScenario {
			scenario.Events = []ScenarioEvent{
				{Sequence: 1, Kind: "a"}, {Sequence: 1, Kind: "a"},
			}
			return scenario
		},
		"key without a key": func(scenario BrowserScenario) BrowserScenario {
			scenario.Keys = []KeyAction{{}}
			return scenario
		},
		"negative repeat": func(scenario BrowserScenario) BrowserScenario {
			scenario.Keys = []KeyAction{{Key: "Tab", Repeat: -1}}
			return scenario
		},
	}
	for name, corrupt := range bad {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(healthyScenario()).Validate(); err == nil {
				t.Fatalf("an unrunnable scenario validated: %s", name)
			}
		})
	}
	if err := healthyScenario().Validate(); err != nil {
		t.Fatalf("a runnable scenario was rejected: %v", err)
	}
}

// TestM22_111_AccessibilityRulesAreStableAndActionable pins the rule set.
func TestM22_111_AccessibilityRulesAreStableAndActionable(t *testing.T) {
	rules := AccessibilityRules()
	if len(rules) != 5 {
		t.Fatalf("rule set has %d rules: %v", len(rules), rules)
	}
	seen := map[string]bool{}
	for _, rule := range rules {
		if seen[rule] {
			t.Fatalf("rule %q is listed twice", rule)
		}
		seen[rule] = true
		if strings.TrimSpace(rule) == "" || strings.ToLower(rule) != rule {
			t.Fatalf("rule %q is not a stable lower-case slug", rule)
		}
	}
	if ScenarioTimeout < 10*time.Second {
		t.Fatalf("scenario timeout %v is too short for a headless render", ScenarioTimeout)
	}
}

func sortedAscending(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] > values[index] {
			return false
		}
	}
	return true
}
