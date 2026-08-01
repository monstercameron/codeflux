package testharness

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// Layer names one place a scenario can be exercised (M22-123).
type layer string

const (
	layerBackend         layer = "backend-only"
	layerGeneratedClient layer = "generated-client"
	layerBrowser         layer = "browser"
)

// applicableLayers reports where a scenario can meaningfully run.
//
// Not every scenario reaches every layer, and pretending otherwise would
// produce browser tests for things the browser never sees. A scenario reaches
// the browser only if the user has to look at or decide something.
func applicableLayers(scenario Scenario) []layer {
	layers := []layer{layerBackend, layerGeneratedClient}
	switch scenario.Name {
	case ScenarioSuccess, ScenarioPlanRevision, ScenarioApproval, ScenarioDenial,
		ScenarioBudgetCap, ScenarioReconnect, ScenarioRecovery, ScenarioRepair:
		layers = append(layers, layerBrowser)
	}
	return layers
}

// TestM22_123_EveryScenarioRunsThroughItsApplicableLayers covers M22-123.
//
// The backend layer is exercised for real: each scenario starts an isolated
// coordinator against the repository state it declares. The client and browser
// layers are exercised through the declared surfaces those layers consume, so
// a scenario that named an event no projection understands fails here rather
// than in a browser run somebody may not have on their machine.
func TestM22_123_EveryScenarioRunsThroughItsApplicableLayers(t *testing.T) {
	if err := ValidateNamedScenarios(); err != nil {
		t.Fatalf("named scenarios are invalid: %v", err)
	}

	browserReached := 0
	for _, scenario := range NamedScenarios() {
		scenario := scenario
		t.Run(string(scenario.Name), func(t *testing.T) {
			layers := applicableLayers(scenario)
			if len(layers) < 2 {
				t.Fatalf("scenario %q reaches only %v", scenario.Name, layers)
			}

			for _, runLayer := range layers {
				switch runLayer {
				case layerBackend:
					runScenarioBackend(t, scenario)
				case layerGeneratedClient:
					runScenarioGeneratedClient(t, scenario)
				case layerBrowser:
					runScenarioBrowser(t, scenario)
				}
			}
		})
		for _, candidate := range applicableLayers(scenario) {
			if candidate == layerBrowser {
				browserReached++
			}
		}
	}

	// If almost nothing reached the browser, the layer coverage claim is
	// hollow.
	if browserReached < 6 {
		t.Fatalf("only %d scenarios reach the browser layer", browserReached)
	}
}

// runScenarioBackend starts a real isolated coordinator for the scenario.
func runScenarioBackend(t *testing.T, scenario Scenario) {
	t.Helper()
	options, err := HarnessOptionsFor(scenario, filepath.Join(t.TempDir(), "backend"))
	if err != nil {
		t.Fatalf("build backend options: %v", err)
	}
	harness, err := NewCoordinatorHarness(t.Context(), options)
	if err != nil {
		t.Fatalf("start backend layer: %v", err)
	}
	t.Cleanup(func() { _ = harness.Close(t.Context()) })

	if err := harness.AssertIsolated(); err != nil {
		t.Fatalf("backend layer is not isolated: %v", err)
	}
	if harness.Repository.State != scenario.RepositoryState {
		t.Fatalf("backend layer built %q, scenario declares %q",
			harness.Repository.State, scenario.RepositoryState)
	}
	// The scripted provider must be the scenario's, and it must be unspent:
	// a harness that consumed the script during startup would leave the
	// scenario nothing to exercise.
	if harness.Provider.Remaining() != len(scenario.ProviderScript) {
		t.Fatalf("backend layer consumed %d of %d scripted steps before the scenario ran",
			len(scenario.ProviderScript)-harness.Provider.Remaining(),
			len(scenario.ProviderScript))
	}
	if err := harness.AssertNoCredentialLeak(); err != nil {
		t.Fatalf("backend layer leaked a credential: %v", err)
	}
}

// runScenarioGeneratedClient drives the scenario's events through the replay
// path a generated client consumes.
func runScenarioGeneratedClient(t *testing.T, scenario Scenario) {
	t.Helper()
	fixture := ReplayFixture{
		Name: string(scenario.Name), Redacted: true,
		Snapshot: ReplaySnapshot{TaskState: "draft", TaskRevision: 1},
	}
	for index, kind := range scenario.EventKinds {
		fixture.Events = append(fixture.Events, ReplayEvent{
			Sequence: uint64(index + 1), Kind: kind, Revision: uint64(index + 1),
		})
	}
	if err := fixture.Validate(); err != nil {
		t.Fatalf("scenario %q does not form a replayable stream: %v", scenario.Name, err)
	}

	consumer := newRecordingConsumer(0)
	result, err := Replay(fixture, ReplayControls{}, consumer)
	if err != nil {
		t.Fatalf("generated-client layer: %v", err)
	}
	if len(result.AppliedSequences) != len(scenario.EventKinds) {
		t.Fatalf("generated-client layer applied %d of %d events",
			len(result.AppliedSequences), len(scenario.EventKinds))
	}

	// The same stream redelivered must not double-apply: this is the property
	// a generated client's deduplication has to hold.
	duplicates := make([]uint64, 0, len(fixture.Events))
	for _, event := range fixture.Events {
		duplicates = append(duplicates, event.Sequence)
	}
	replayConsumer := newRecordingConsumer(0)
	repeated, err := Replay(fixture, ReplayControls{DuplicateSequences: duplicates}, replayConsumer)
	if err != nil {
		t.Fatalf("generated-client duplicate delivery: %v", err)
	}
	if len(replayConsumer.applied) != len(scenario.EventKinds) {
		t.Fatalf("redelivery applied %d events, want %d",
			len(replayConsumer.applied), len(scenario.EventKinds))
	}
	for _, delivery := range repeated.Deliveries {
		if delivery.Duplicate && delivery.Applied {
			t.Fatalf("generated-client layer applied a duplicate of sequence %d",
				delivery.Sequence)
		}
	}
}

// runScenarioBrowser drives the scenario through the browser scenario harness.
func runScenarioBrowser(t *testing.T, scenario Scenario) {
	t.Helper()
	harness, err := NewBrowserHarness(BrowserHarnessOptions{
		ArtifactRoot: filepath.Join(t.TempDir(), "artifacts"),
	})
	if err != nil {
		t.Fatalf("build browser harness: %v", err)
	}
	t.Cleanup(func() { _ = harness.Close() })

	browserScenario := BrowserScenario{
		Name: string(scenario.Name),
		Bootstrap: SessionBootstrap{
			ConnectionState: "live", Route: "/tasks",
			SelectedThread: "thr_layer", SelectedTask: "tsk_layer",
		},
		Keys: []KeyAction{{Key: "Tab"}},
	}
	for index, kind := range scenario.EventKinds {
		browserScenario.Events = append(browserScenario.Events, ScenarioEvent{
			Sequence: uint64(index + 1), Kind: kind,
		})
	}
	// A scenario that stops for the user must present something to decide on,
	// so the browser run drives a decision key as well as traversal.
	if scenario.RequiresApproval {
		browserScenario.Keys = append(browserScenario.Keys, KeyAction{Key: "Enter"})
	}
	if err := browserScenario.Validate(); err != nil {
		t.Fatalf("scenario %q does not form a browser scenario: %v", scenario.Name, err)
	}

	inspector := newScriptedInspector()
	inspector.focusOrder = []string{"primary-action", "secondary-action"}
	result, err := harness.Run(browserScenario, inspector)
	if err != nil {
		t.Fatalf("browser layer: %v", err)
	}
	if result.Failed {
		t.Fatalf("browser layer reported failure: %+v", result)
	}
	if result.EventsDelivered != len(scenario.EventKinds) {
		t.Fatalf("browser layer delivered %d of %d events",
			result.EventsDelivered, len(scenario.EventKinds))
	}
}

// TestM22_123_LayerCoverageIsReportedNotAssumed makes the coverage claim
// itself inspectable, so a scenario silently dropping a layer is visible.
func TestM22_123_LayerCoverageIsReportedNotAssumed(t *testing.T) {
	coverage := map[layer][]string{}
	for _, scenario := range NamedScenarios() {
		for _, candidate := range applicableLayers(scenario) {
			coverage[candidate] = append(coverage[candidate], string(scenario.Name))
		}
	}
	for _, candidate := range []layer{layerBackend, layerGeneratedClient, layerBrowser} {
		names := coverage[candidate]
		sort.Strings(names)
		if len(names) == 0 {
			t.Fatalf("layer %q covers no scenario", candidate)
		}
		t.Logf("%s: %d scenarios (%s)", candidate, len(names), strings.Join(names, ", "))
	}
	// Backend and client must cover every scenario; the browser legitimately
	// covers fewer, but never fewer than half.
	if len(coverage[layerBackend]) != len(NamedScenarios()) {
		t.Fatalf("backend layer covers %d of %d scenarios",
			len(coverage[layerBackend]), len(NamedScenarios()))
	}
	if len(coverage[layerGeneratedClient]) != len(NamedScenarios()) {
		t.Fatalf("generated-client layer covers %d of %d scenarios",
			len(coverage[layerGeneratedClient]), len(NamedScenarios()))
	}
	if len(coverage[layerBrowser])*2 < len(NamedScenarios()) {
		t.Fatalf("browser layer covers only %d of %d scenarios",
			len(coverage[layerBrowser]), len(NamedScenarios()))
	}

	// Every scenario must have a non-empty, deduplicated layer list.
	for _, scenario := range NamedScenarios() {
		seen := map[layer]bool{}
		for _, candidate := range applicableLayers(scenario) {
			if seen[candidate] {
				t.Fatalf("scenario %q lists layer %q twice", scenario.Name, candidate)
			}
			seen[candidate] = true
		}
	}
}

// TestM22_123_ScenarioProviderScriptsAreDistinguishable guards against the
// scenario set degenerating into twelve copies of one flow.
func TestM22_123_ScenarioProviderScriptsAreDistinguishable(t *testing.T) {
	signatures := map[string]ScenarioName{}
	for _, scenario := range NamedScenarios() {
		parts := make([]string, 0, len(scenario.ProviderScript))
		for _, step := range scenario.ProviderScript {
			parts = append(parts, fmt.Sprintf("%s|%s|%s",
				step.Kind, step.Text, step.ToolName))
		}
		signature := strings.Join(parts, "//")
		if other, clash := signatures[signature]; clash {
			t.Fatalf("scenarios %q and %q script the same provider exchange",
				other, scenario.Name)
		}
		signatures[signature] = scenario.Name
	}

	// Every provider step kind that a scenario can encounter must appear
	// somewhere in the set, or a failure mode is undescribed.
	used := map[testfixtures.StepKind]bool{}
	for _, scenario := range NamedScenarios() {
		for _, step := range scenario.ProviderScript {
			used[step.Kind] = true
		}
	}
	for _, kind := range []testfixtures.StepKind{
		testfixtures.StepText, testfixtures.StepToolCall,
		testfixtures.StepRateLimit, testfixtures.StepAuthFailure,
	} {
		if !used[kind] {
			t.Fatalf("no scenario scripts a %s step", kind)
		}
	}
}
