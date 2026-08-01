package testharness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// TestM22_113_NamedScenariosAreCompleteAndRunnable covers M22-113.
func TestM22_113_NamedScenariosAreCompleteAndRunnable(t *testing.T) {
	if err := ValidateNamedScenarios(); err != nil {
		t.Fatalf("named scenarios are invalid: %v", err)
	}
	scenarios := NamedScenarios()
	if len(scenarios) != 12 {
		t.Fatalf("M22-113 names 12 scenarios, %d are declared", len(scenarios))
	}

	// Every safe outcome must be represented, or the scenario set only
	// exercises the ways things go right.
	outcomes := map[testfixtures.SafeOutcome]bool{}
	faults := 0
	approvals := 0
	for _, scenario := range scenarios {
		outcomes[scenario.ExpectedOutcome] = true
		if scenario.Fault != "" {
			faults++
		}
		if scenario.RequiresApproval {
			approvals++
		}
	}
	for _, outcome := range []testfixtures.SafeOutcome{
		testfixtures.OutcomeResumable,
		testfixtures.OutcomeRetryable,
		testfixtures.OutcomeRequiresReconciliation,
		testfixtures.OutcomeTerminatedCleanly,
	} {
		if !outcomes[outcome] {
			t.Fatalf("no scenario expects outcome %q", outcome)
		}
	}
	if faults < 4 {
		t.Fatalf("only %d scenarios inject a fault; the crash and failure cases need them", faults)
	}
	if approvals < 2 {
		t.Fatalf("only %d scenarios stop for the user", approvals)
	}
}

// TestM22_113_ScenarioValidationRejectsUnrunnableScenarios proves the
// validator is load-bearing.
func TestM22_113_ScenarioValidationRejectsUnrunnableScenarios(t *testing.T) {
	base, err := ScenarioByName(ScenarioSuccess)
	if err != nil {
		t.Fatalf("look up scenario: %v", err)
	}
	corruptions := map[string]func(Scenario) Scenario{
		"unknown name": func(scenario Scenario) Scenario {
			scenario.Name = ScenarioName("invented")
			return scenario
		},
		"no summary": func(scenario Scenario) Scenario {
			scenario.Summary = ""
			return scenario
		},
		"invalid repository state": func(scenario Scenario) Scenario {
			scenario.RepositoryState = testfixtures.RepositoryState("invented")
			return scenario
		},
		"no provider script": func(scenario Scenario) Scenario {
			scenario.ProviderScript = nil
			return scenario
		},
		"malformed step": func(scenario Scenario) Scenario {
			scenario.ProviderScript = []testfixtures.ProviderStep{{Kind: testfixtures.StepText}}
			return scenario
		},
		"invalid outcome": func(scenario Scenario) Scenario {
			scenario.ExpectedOutcome = testfixtures.SafeOutcome("invented")
			return scenario
		},
		"no events": func(scenario Scenario) Scenario {
			scenario.EventKinds = nil
			return scenario
		},
		"unknown fault": func(scenario Scenario) Scenario {
			scenario.Fault = testfixtures.FaultPoint("invented")
			return scenario
		},
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(base).Validate(); err == nil {
				t.Fatalf("an unrunnable scenario validated: %s", name)
			}
		})
	}
	if _, err := ScenarioByName(ScenarioName("invented")); err == nil {
		t.Fatal("an unknown scenario name resolved")
	}
}

// TestM22_113_ScenariosBuildRunnableHarnessOptions proves a declared scenario
// can actually start a coordinator, which is what makes it interactive rather
// than documentation.
func TestM22_113_ScenariosBuildRunnableHarnessOptions(t *testing.T) {
	scenario, err := ScenarioByName(ScenarioConcurrentEdit)
	if err != nil {
		t.Fatalf("look up scenario: %v", err)
	}
	root := filepath.Join(t.TempDir(), "scenario")
	options, err := HarnessOptionsFor(scenario, root)
	if err != nil {
		t.Fatalf("build options: %v", err)
	}
	if options.RepositoryState != testfixtures.StateDirty {
		t.Fatalf("concurrent-edit runs against %q", options.RepositoryState)
	}

	harness, err := NewCoordinatorHarness(t.Context(), options)
	if err != nil {
		t.Fatalf("start scenario harness: %v", err)
	}
	t.Cleanup(func() { _ = harness.Close(t.Context()) })
	if err := harness.AssertIsolated(); err != nil {
		t.Fatalf("scenario harness is not isolated: %v", err)
	}

	// A scenario that could not build its own options is not runnable.
	broken := scenario
	broken.EventKinds = nil
	if _, err := HarnessOptionsFor(broken, root); err == nil {
		t.Fatal("an invalid scenario produced harness options")
	}
}

// recordingConsumer applies events, refusing a gap and deduplicating repeats.
type recordingConsumer struct {
	through    uint64
	applied    []uint64
	repairs    []uint64
	reconnects int
	seen       map[uint64]bool
}

func newRecordingConsumer(through uint64) *recordingConsumer {
	return &recordingConsumer{through: through, seen: map[uint64]bool{}}
}

func (consumer *recordingConsumer) Apply(event ReplayEvent) (bool, error) {
	if consumer.seen[event.Sequence] {
		return false, nil
	}
	if event.Sequence != consumer.through+1 {
		return false, fmt.Errorf("gap: expected %d, got %d",
			consumer.through+1, event.Sequence)
	}
	consumer.seen[event.Sequence] = true
	consumer.through = event.Sequence
	consumer.applied = append(consumer.applied, event.Sequence)
	return true, nil
}

func (consumer *recordingConsumer) RequestSnapshotRepair(through uint64) error {
	consumer.repairs = append(consumer.repairs, through)
	return nil
}

func (consumer *recordingConsumer) Reconnect() error {
	consumer.reconnects++
	return nil
}

func replayFixtureFixture() ReplayFixture {
	return ReplayFixture{
		Name: "plan-and-run", Redacted: true,
		Snapshot: ReplaySnapshot{ThroughSequence: 0, TaskState: "draft", TaskRevision: 1},
		Events: []ReplayEvent{
			{Sequence: 1, Kind: "task-created", Revision: 1},
			{Sequence: 2, Kind: "plan-created", Revision: 2},
			{Sequence: 3, Kind: "tool-started", Revision: 3},
			{Sequence: 4, Kind: "tool-completed", Revision: 4},
			{Sequence: 5, Kind: "task-completed", Revision: 5},
		},
	}
}

// TestM22_114_ReplayFixtureRoundTripsAndRefusesUnredactedSessions covers
// M22-114.
func TestM22_114_ReplayFixtureRoundTripsAndRefusesUnredactedSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	fixture := replayFixtureFixture()
	if err := SaveReplayFixture(path, fixture); err != nil {
		t.Fatalf("save fixture: %v", err)
	}
	loaded, err := LoadReplayFixture(path, testfixtures.FixtureCredentialShapes())
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if loaded.Name != fixture.Name || len(loaded.Events) != len(fixture.Events) {
		t.Fatalf("round trip lost content: %+v", loaded)
	}

	// An unredacted export must never be storable.
	unredacted := fixture
	unredacted.Redacted = false
	if err := SaveReplayFixture(filepath.Join(t.TempDir(), "bad.json"), unredacted); err == nil {
		t.Fatal("an unredacted session was saved as a fixture")
	}

	// A file containing credential material must be refused on load, even if
	// it is structurally valid and claims to be redacted.
	leaky := fixture
	leaky.Events = append(leaky.Events, ReplayEvent{
		Sequence: 6, Kind: "tool-completed", Revision: 6,
		Payload: json.RawMessage(
			`{"output":"` + testfixtures.FixtureCredentialMaterial + `"}`),
	})
	leakyPath := filepath.Join(t.TempDir(), "leaky.json")
	if err := SaveReplayFixture(leakyPath, leaky); err != nil {
		t.Fatalf("save leaky fixture: %v", err)
	}
	if _, err := LoadReplayFixture(leakyPath, testfixtures.FixtureCredentialShapes()); err == nil {
		t.Fatal("a fixture containing credential material was loaded")
	}
}

// TestM22_114_ReplayFixtureRejectsGappyRecordings proves a bad recording is
// caught at load rather than mistaken for a replay bug later.
func TestM22_114_ReplayFixtureRejectsGappyRecordings(t *testing.T) {
	bad := map[string]func(ReplayFixture) ReplayFixture{
		"no name": func(fixture ReplayFixture) ReplayFixture {
			fixture.Name = ""
			return fixture
		},
		"no events": func(fixture ReplayFixture) ReplayFixture {
			fixture.Events = nil
			return fixture
		},
		"event without kind": func(fixture ReplayFixture) ReplayFixture {
			fixture.Events[0].Kind = ""
			return fixture
		},
		"sequence gap": func(fixture ReplayFixture) ReplayFixture {
			fixture.Events = fixture.Events[1:]
			return fixture
		},
		"sequence repeat": func(fixture ReplayFixture) ReplayFixture {
			fixture.Events[2].Sequence = 2
			return fixture
		},
	}
	for name, corrupt := range bad {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(replayFixtureFixture()).Validate(); err == nil {
				t.Fatalf("an unreplayable fixture validated: %s", name)
			}
		})
	}
}

// TestM22_115_ReplayControlsReproduceEachTransportCondition covers M22-115.
func TestM22_115_ReplayControlsReproduceEachTransportCondition(t *testing.T) {
	fixture := replayFixtureFixture()

	t.Run("clean replay", func(t *testing.T) {
		consumer := newRecordingConsumer(0)
		result, err := Replay(fixture, ReplayControls{}, consumer)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if len(result.AppliedSequences) != 5 {
			t.Fatalf("applied %v", result.AppliedSequences)
		}
		if result.GapDetectedAt != 0 || result.StoppedEarly {
			t.Fatalf("clean replay reported a gap or an early stop: %+v", result)
		}
	})

	t.Run("stop at sequence", func(t *testing.T) {
		consumer := newRecordingConsumer(0)
		result, err := Replay(fixture, ReplayControls{StopAtSequence: 3}, consumer)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if !result.StoppedEarly {
			t.Fatal("stop-at did not stop the replay")
		}
		if len(consumer.applied) != 3 {
			t.Fatalf("consumer applied %v after stopping at 3", consumer.applied)
		}
	})

	t.Run("step event", func(t *testing.T) {
		consumer := newRecordingConsumer(0)
		result, err := Replay(fixture, ReplayControls{StepEvent: true}, consumer)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if !result.StoppedEarly || len(consumer.applied) != 1 {
			t.Fatalf("step mode applied %v", consumer.applied)
		}
	})

	t.Run("duplicate delivery", func(t *testing.T) {
		consumer := newRecordingConsumer(0)
		result, err := Replay(fixture, ReplayControls{DuplicateSequences: []uint64{2, 4}}, consumer)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		// The duplicate must be filtered by the consumer, not applied twice.
		if len(consumer.applied) != 5 {
			t.Fatalf("consumer applied %v; a duplicate was applied twice", consumer.applied)
		}
		duplicates := 0
		for _, delivery := range result.Deliveries {
			if delivery.Duplicate {
				duplicates++
				if delivery.Applied {
					t.Fatalf("duplicate of %d was applied", delivery.Sequence)
				}
			}
		}
		if duplicates != 2 {
			t.Fatalf("delivered %d duplicates, want 2", duplicates)
		}
	})

	t.Run("gap detection", func(t *testing.T) {
		consumer := newRecordingConsumer(0)
		result, err := Replay(fixture, ReplayControls{GapSequences: []uint64{3}}, consumer)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		// Events after the gap must be refused, not applied out of order.
		if result.GapDetectedAt != 4 {
			t.Fatalf("gap detected at %d, want 4", result.GapDetectedAt)
		}
		if len(consumer.applied) != 2 {
			t.Fatalf("consumer applied %v across a gap", consumer.applied)
		}
	})

	t.Run("reconnect and snapshot repair", func(t *testing.T) {
		consumer := newRecordingConsumer(0)
		result, err := Replay(fixture, ReplayControls{
			ReconnectAfterSequence:   2,
			SnapshotRepairAtSequence: 3,
		}, consumer)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if consumer.reconnects != 1 {
			t.Fatalf("reconnected %d times", consumer.reconnects)
		}
		if result.RepairsRequested != 1 || len(consumer.repairs) != 1 {
			t.Fatalf("repairs = %d / %v", result.RepairsRequested, consumer.repairs)
		}
		if len(consumer.applied) != 5 {
			t.Fatalf("reconnect lost events: %v", consumer.applied)
		}
	})
}

// TestM22_115_ReplayControlsRejectContradictions proves the controls are
// checked against the fixture rather than accepted blindly.
func TestM22_115_ReplayControlsRejectContradictions(t *testing.T) {
	fixture := replayFixtureFixture()
	consumer := newRecordingConsumer(0)
	bad := []ReplayControls{
		{StopAtSequence: 99},
		{ReconnectAfterSequence: 99},
		{SnapshotRepairAtSequence: 99},
		{DuplicateSequences: []uint64{99}},
		{GapSequences: []uint64{99}},
		{DuplicateSequences: []uint64{2}, GapSequences: []uint64{2}},
	}
	for index, controls := range bad {
		if _, err := Replay(fixture, controls, consumer); err == nil {
			t.Fatalf("contradictory controls %d were accepted", index)
		}
	}
	if _, err := Replay(fixture, ReplayControls{}, nil); err == nil {
		t.Fatal("a replay with no consumer was accepted")
	}
}

// TestM22_116_ProjectionComparisonFindsRealDisagreements covers M22-116.
func TestM22_116_ProjectionComparisonFindsRealDisagreements(t *testing.T) {
	server := Projection{Side: "server", Values: map[string]string{
		"task_state": "running", "task_revision": "4", "budget_remaining": "150",
	}}
	client := Projection{Side: "client", Values: map[string]string{
		"task_state": "running", "task_revision": "4", "budget_remaining": "150",
	}}

	differences, err := CompareProjections(server, client)
	if err != nil {
		t.Fatalf("compare identical projections: %v", err)
	}
	if len(differences) != 0 {
		t.Fatalf("identical projections differ: %+v", differences)
	}

	client.Values["task_revision"] = "3"
	delete(client.Values, "budget_remaining")
	client.Values["client_only"] = "x"

	differences, err = CompareProjections(server, client)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(differences) != 3 {
		t.Fatalf("found %d differences, want 3: %+v", len(differences), differences)
	}
	// Absence must be reported as a difference, not silently matched to empty.
	byKey := map[string]ProjectionDifference{}
	for _, difference := range differences {
		byKey[difference.Key] = difference
	}
	if byKey["budget_remaining"].Client != "(absent)" {
		t.Fatalf("a missing client value was not reported as absent: %+v", byKey["budget_remaining"])
	}
	if byKey["client_only"].Server != "(absent)" {
		t.Fatalf("a client-only key was not reported: %+v", byKey["client_only"])
	}
	// Ordering must be stable so two runs produce the same report.
	for index := 1; index < len(differences); index++ {
		if differences[index-1].Key > differences[index].Key {
			t.Fatalf("differences are not ordered: %+v", differences)
		}
	}

	if _, err := CompareProjections(Projection{}, client); err == nil {
		t.Fatal("a projection with no side was compared")
	}
	if _, err := CompareProjections(server, Projection{Side: "server"}); err == nil {
		t.Fatal("two projections claiming the same side were compared")
	}
}

// TestM22_117_GraphRebuildComparisonDetectsDrift covers M22-117.
func TestM22_117_GraphRebuildComparisonDetectsDrift(t *testing.T) {
	original := NewGraphRevisionDigest(4,
		[]string{"node-b", "node-a", "node-c"},
		[]string{"node-a->node-b", "node-b->node-c"})

	// A rebuild that produced the same graph in a different order must match:
	// the graph is a set, and ordering is a rendering concern.
	rebuilt := NewGraphRevisionDigest(4,
		[]string{"node-c", "node-a", "node-b"},
		[]string{"node-b->node-c", "node-a->node-b"})
	if err := CompareGraphRevisions(original, rebuilt); err != nil {
		t.Fatalf("an order-only difference was reported as drift: %v", err)
	}

	drifted := map[string]GraphRevisionDigest{
		"different ordinal": NewGraphRevisionDigest(5,
			[]string{"node-a", "node-b", "node-c"},
			[]string{"node-a->node-b", "node-b->node-c"}),
		"missing node": NewGraphRevisionDigest(4,
			[]string{"node-a", "node-b"},
			[]string{"node-a->node-b", "node-b->node-c"}),
		"renamed node": NewGraphRevisionDigest(4,
			[]string{"node-a", "node-b", "node-z"},
			[]string{"node-a->node-b", "node-b->node-c"}),
		"missing edge": NewGraphRevisionDigest(4,
			[]string{"node-a", "node-b", "node-c"},
			[]string{"node-a->node-b"}),
		"rewired edge": NewGraphRevisionDigest(4,
			[]string{"node-a", "node-b", "node-c"},
			[]string{"node-a->node-b", "node-a->node-c"}),
	}
	for name, candidate := range drifted {
		t.Run(name, func(t *testing.T) {
			if err := CompareGraphRevisions(original, candidate); err == nil {
				t.Fatalf("graph drift was not detected: %s", name)
			}
		})
	}
}

// TestM22_114_ReplayFixtureLoadReportsMissingFiles guards the error path.
func TestM22_114_ReplayFixtureLoadReportsMissingFiles(t *testing.T) {
	if _, err := LoadReplayFixture(filepath.Join(t.TempDir(), "absent.json"), nil); err == nil {
		t.Fatal("a missing fixture loaded")
	}
	malformed := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(malformed, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write malformed fixture: %v", err)
	}
	_, err := LoadReplayFixture(malformed, nil)
	if err == nil {
		t.Fatal("a malformed fixture loaded")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Fatalf("malformed fixture error = %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatal("a parse failure was reported as a missing file")
	}
}
