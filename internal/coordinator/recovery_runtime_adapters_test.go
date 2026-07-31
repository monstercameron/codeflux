package coordinator

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/workspace"
)

func TestDurableRecoveryActionSourceIncludesPostCheckpointReplayGates(
	t *testing.T,
) {
	t.Parallel()

	fixture := recoveryClassificationFixture(t)
	reader := &recoveryActionObservationStub{
		value: storage.RecoveryActionObservation{
			CompletedActionIDs: []string{"tool:completed-after-checkpoint"},
			AmbiguousExternalActions: []checkpoint.AmbiguousExternalAction{{
				ActionID:      "tool-intent-without-result",
				Kind:          "tool-request",
				IntentSHA256:  recoverySHA('e'),
				ToolRequestID: "tool-intent-without-result",
			}},
		},
	}
	source, err := NewDurableRecoveryActionSource(reader)
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.ObserveRecoveryActions(
		t.Context(),
		fixture.Checkpoint.TaskID,
		fixture.Checkpoint.RunID,
		fixture.Checkpoint.LastDurableEventSequence,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.CompletedActionIDs) != 1 ||
		got.CompletedActionIDs[0] != "tool:completed-after-checkpoint" ||
		len(got.AmbiguousExternalActions) != 1 ||
		got.AmbiguousExternalActions[0].ActionID !=
			"tool-intent-without-result" {
		t.Fatalf("recovery action facts = %#v", got)
	}
	if reader.calls != 1 {
		t.Fatalf("action observation calls = %d, want 1", reader.calls)
	}
}

func TestDurableRecoveryCompatibilitySourceReturnsExactBindings(t *testing.T) {
	t.Parallel()

	fixture := recoveryClassificationFixture(t)
	runtime := &recoveryRuntimeStateStub{value: checkpoint.RuntimeState{
		Policy: fixture.Checkpoint.Policy, Provider: fixture.Checkpoint.Provider,
		Tools: fixture.Checkpoint.Tools,
	}}
	source, err := NewDurableRecoveryCompatibilitySource(runtime)
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.ObserveRecoveryCompatibility(
		t.Context(),
		fixture.Checkpoint.TaskID,
		fixture.Checkpoint.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Policy != fixture.Checkpoint.Policy ||
		got.Provider != fixture.Checkpoint.Provider ||
		got.Tools != fixture.Checkpoint.Tools {
		t.Fatalf("recovery compatibility facts = %#v", got)
	}
}

func TestDurableRecoveryPatchLocatorVerifiesSharedRepositoryRef(
	t *testing.T,
) {
	t.Parallel()

	fixture := recoveryClassificationFixture(t)
	canonical, err := checkpoint.Canonicalize(fixture.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	reference := "refs/codeflux/checkpoints/" +
		fixture.CheckpointID.String()
	metadata := &recoveryPatchMetadataStub{
		checkpoint: checkpoint.PersistedCheckpoint{
			ID: fixture.CheckpointID, TaskID: fixture.Checkpoint.TaskID,
			RunID:         fixture.Checkpoint.RunID,
			SchemaVersion: checkpoint.SchemaVersion,
			StateJSON:     canonical.JSON, StateSHA256: canonical.StateSHA256,
			PreservedRevision: fixture.Checkpoint.PreservedRevision,
			PreservedRef:      reference,
		},
		repository: storage.Repository{
			ID:            fixture.Checkpoint.RepositoryID,
			CanonicalPath: `C:\codeflux\repository`,
		},
	}
	runner := &recoveryPatchGitStub{
		result: workspace.CommandResult{
			Stdout: []byte(fixture.Checkpoint.PreservedRevision + "\n"),
		},
	}
	locator, err := NewDurableRecoveryPatchLocator(metadata, runner)
	if err != nil {
		t.Fatal(err)
	}
	got, err := locator.LocateCheckpointPatch(
		t.Context(),
		fixture.Checkpoint.TaskID,
		fixture.CheckpointID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Locator != reference || got.Path != "" {
		t.Fatalf("patch location = %#v", got)
	}
	if len(runner.arguments) != 3 ||
		runner.arguments[0] != "rev-parse" ||
		runner.arguments[1] != "--verify" ||
		runner.arguments[2] != reference+"^{commit}" {
		t.Fatalf("patch verification arguments = %v", runner.arguments)
	}
}

func TestDurableRecoveryPatchLocatorDoesNotClaimMissingRef(t *testing.T) {
	t.Parallel()

	fixture := recoveryClassificationFixture(t)
	canonical, err := checkpoint.Canonicalize(fixture.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	metadata := &recoveryPatchMetadataStub{
		checkpoint: checkpoint.PersistedCheckpoint{
			ID: fixture.CheckpointID, TaskID: fixture.Checkpoint.TaskID,
			RunID:         fixture.Checkpoint.RunID,
			SchemaVersion: checkpoint.SchemaVersion,
			StateJSON:     canonical.JSON, StateSHA256: canonical.StateSHA256,
			PreservedRevision: fixture.Checkpoint.PreservedRevision,
			PreservedRef: "refs/codeflux/checkpoints/" +
				fixture.CheckpointID.String(),
		},
		repository: storage.Repository{
			ID:            fixture.Checkpoint.RepositoryID,
			CanonicalPath: `C:\codeflux\repository`,
		},
	}
	locator, err := NewDurableRecoveryPatchLocator(
		metadata,
		&recoveryPatchGitStub{err: errors.New("missing ref")},
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := locator.LocateCheckpointPatch(
		t.Context(),
		fixture.Checkpoint.TaskID,
		fixture.CheckpointID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != (RecoveryPatchLocation{}) {
		t.Fatalf("missing patch ref location = %#v", got)
	}
}

type recoveryActionObservationStub struct {
	value storage.RecoveryActionObservation
	err   error
	calls int
}

type recoveryRuntimeStateStub struct {
	value checkpoint.RuntimeState
	err   error
}

func (stub *recoveryRuntimeStateStub) ReadCheckpointRuntimeState(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (checkpoint.RuntimeState, error) {
	return stub.value, stub.err
}

func (stub *recoveryActionObservationStub) ReadRecoveryActionObservation(
	context.Context,
	domain.TaskID,
	domain.RunID,
	uint64,
) (storage.RecoveryActionObservation, error) {
	stub.calls++
	return stub.value, stub.err
}

type recoveryPatchMetadataStub struct {
	checkpoint checkpoint.PersistedCheckpoint
	repository storage.Repository
}

func (stub *recoveryPatchMetadataStub) LoadCheckpoint(
	context.Context,
	domain.CheckpointID,
) (checkpoint.PersistedCheckpoint, error) {
	return stub.checkpoint, nil
}

func (stub *recoveryPatchMetadataStub) GetRepository(
	context.Context,
	domain.RepositoryID,
) (storage.Repository, error) {
	return stub.repository, nil
}

type recoveryPatchGitStub struct {
	result    workspace.CommandResult
	err       error
	arguments []string
}

func (stub *recoveryPatchGitStub) Run(
	_ context.Context,
	_ string,
	_ string,
	arguments ...string,
) (workspace.CommandResult, error) {
	stub.arguments = append([]string(nil), arguments...)
	return stub.result, stub.err
}
