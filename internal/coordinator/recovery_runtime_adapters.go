package coordinator

import (
	"context"
	"errors"
	"strings"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/workspace"
)

type recoveryActionObservationReader interface {
	ReadRecoveryActionObservation(
		context.Context,
		domain.TaskID,
		domain.RunID,
		uint64,
	) (storage.RecoveryActionObservation, error)
}

type recoveryRuntimeStateReader interface {
	ReadCheckpointRuntimeState(
		context.Context,
		domain.TaskID,
		domain.RunID,
	) (checkpoint.RuntimeState, error)
}

// DurableRecoveryCompatibilitySource reads the same authoritative,
// revision-bound runtime projection used to create checkpoints.
type DurableRecoveryCompatibilitySource struct {
	runtime recoveryRuntimeStateReader
}

func NewDurableRecoveryCompatibilitySource(
	runtime recoveryRuntimeStateReader,
) (*DurableRecoveryCompatibilitySource, error) {
	if runtime == nil {
		return nil, errors.New("recovery runtime state repository is required")
	}
	return &DurableRecoveryCompatibilitySource{runtime: runtime}, nil
}

func (source *DurableRecoveryCompatibilitySource) ObserveRecoveryCompatibility(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) (RecoveryCompatibilityFacts, error) {
	if source == nil {
		return RecoveryCompatibilityFacts{}, errors.New(
			"durable recovery compatibility source is unavailable",
		)
	}
	if taskID.IsZero() || runID.IsZero() {
		return RecoveryCompatibilityFacts{}, errors.New(
			"recovery compatibility task and run identities are required",
		)
	}
	state, err := source.runtime.ReadCheckpointRuntimeState(ctx, taskID, runID)
	if err != nil {
		return RecoveryCompatibilityFacts{}, err
	}
	return RecoveryCompatibilityFacts{
		Policy: state.Policy, Provider: state.Provider, Tools: state.Tools,
	}, nil
}

// DurableRecoveryActionSource projects the immutable operation journal into
// replay blockers. The checkpoint event sequence is intentionally not used as
// a query cutoff: outcomes committed after the checkpoint must also block
// accidental repetition.
type DurableRecoveryActionSource struct {
	observations recoveryActionObservationReader
}

func NewDurableRecoveryActionSource(
	observations recoveryActionObservationReader,
) (*DurableRecoveryActionSource, error) {
	if observations == nil {
		return nil, errors.New("recovery action observation repository is required")
	}
	return &DurableRecoveryActionSource{observations: observations}, nil
}

func (source *DurableRecoveryActionSource) ObserveRecoveryActions(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	checkpointEventSequence uint64,
) (RecoveryActionFacts, error) {
	if source == nil {
		return RecoveryActionFacts{}, errors.New(
			"durable recovery action source is unavailable",
		)
	}
	if taskID.IsZero() || runID.IsZero() || checkpointEventSequence == 0 {
		return RecoveryActionFacts{}, errors.New(
			"recovery action task, run, and checkpoint event are required",
		)
	}
	value, err := source.observations.ReadRecoveryActionObservation(
		ctx,
		taskID,
		runID,
		checkpointEventSequence,
	)
	if err != nil {
		return RecoveryActionFacts{}, err
	}
	return RecoveryActionFacts{
		CompletedActionIDs: append(
			[]string(nil),
			value.CompletedActionIDs...,
		),
		AmbiguousExternalActions: append(
			[]checkpoint.AmbiguousExternalAction(nil),
			value.AmbiguousExternalActions...,
		),
	}, nil
}

type recoveryPatchMetadataSource interface {
	LoadCheckpoint(
		context.Context,
		domain.CheckpointID,
	) (checkpoint.PersistedCheckpoint, error)
	GetRepository(
		context.Context,
		domain.RepositoryID,
	) (storage.Repository, error)
}

// DurableRecoveryPatchLocator verifies the immutable preservation ref in the
// shared repository. It remains usable when the task worktree is absent and
// performs no export or Git mutation during startup assessment.
type DurableRecoveryPatchLocator struct {
	metadata recoveryPatchMetadataSource
	git      workspace.CommandRunner
}

func NewDurableRecoveryPatchLocator(
	metadata recoveryPatchMetadataSource,
	git workspace.CommandRunner,
) (*DurableRecoveryPatchLocator, error) {
	if metadata == nil || git == nil {
		return nil, errors.New(
			"recovery patch metadata and Git runner are required",
		)
	}
	return &DurableRecoveryPatchLocator{metadata: metadata, git: git}, nil
}

func (locator *DurableRecoveryPatchLocator) LocateCheckpointPatch(
	ctx context.Context,
	taskID domain.TaskID,
	checkpointID domain.CheckpointID,
) (RecoveryPatchLocation, error) {
	if locator == nil {
		return RecoveryPatchLocation{}, errors.New(
			"durable recovery patch locator is unavailable",
		)
	}
	if taskID.IsZero() || checkpointID.IsZero() {
		return RecoveryPatchLocation{}, errors.New(
			"recovery patch task and checkpoint identities are required",
		)
	}
	persisted, err := locator.metadata.LoadCheckpoint(ctx, checkpointID)
	if err != nil {
		return RecoveryPatchLocation{}, err
	}
	if persisted.TaskID != taskID {
		return RecoveryPatchLocation{}, errors.New(
			"recovery checkpoint belongs to another task",
		)
	}
	state, err := checkpoint.DecodeCanonicalState(
		persisted.StateJSON,
		persisted.StateSHA256,
	)
	if err != nil {
		return RecoveryPatchLocation{}, nil
	}
	if state.Snapshot.TaskID != taskID ||
		state.Snapshot.PreservedRevision != persisted.PreservedRevision ||
		strings.TrimSpace(persisted.PreservedRef) != persisted.PreservedRef ||
		persisted.PreservedRef == "" {
		return RecoveryPatchLocation{}, nil
	}
	repository, err := locator.metadata.GetRepository(
		ctx,
		state.Snapshot.RepositoryID,
	)
	if err != nil {
		return RecoveryPatchLocation{}, err
	}
	result, err := locator.git.Run(
		ctx,
		repository.CanonicalPath,
		"git",
		"rev-parse",
		"--verify",
		persisted.PreservedRef+"^{commit}",
	)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return RecoveryPatchLocation{}, ctxErr
		}
		return RecoveryPatchLocation{}, nil
	}
	if strings.TrimSpace(string(result.Stdout)) != persisted.PreservedRevision {
		return RecoveryPatchLocation{}, nil
	}
	return RecoveryPatchLocation{
		Available: true,
		Locator:   persisted.PreservedRef,
	}, nil
}

var _ RecoveryActionSource = (*DurableRecoveryActionSource)(nil)
var _ RecoveryCompatibilitySource = (*DurableRecoveryCompatibilitySource)(nil)
var _ RecoveryPatchLocator = (*DurableRecoveryPatchLocator)(nil)
