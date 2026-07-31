package coordinator

import (
	"context"
	"errors"
	"path/filepath"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/workspace"
)

type storedCheckpointPlanRevisionSource struct {
	repositories *storage.Repositories
}

func (source storedCheckpointPlanRevisionSource) CurrentCheckpointPlanRevision(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) (uint64, error) {
	binding, err := source.repositories.GetRunPlanBinding(ctx, runID)
	if err != nil {
		return 0, err
	}
	if binding.TaskID != taskID {
		return 0, errors.New(
			"run plan binding belongs to another task",
		)
	}
	return binding.PlanRevision, nil
}

type applicationCheckpointing struct {
	checkpoints *LaneACheckpointAdapter
	worktrees   *gitwork.Service
	redactor    *redact.Pipeline
	close       func() error
}

func newApplicationCheckpointing(
	databasePath string,
	repositories *storage.Repositories,
) (applicationCheckpointing, error) {
	worktrees, err := gitwork.NewService(
		filepath.Join(filepath.Dir(databasePath), "worktrees"),
		repositories,
		gitwork.ExecRunner{},
		nil,
	)
	if err != nil {
		return applicationCheckpointing{}, err
	}
	worktrees.SetCheckpointRepository(repositories)
	pipeline, err := redact.NewPipeline(
		nil,
		redact.Limits{
			MaximumInputBytes:  4 << 20,
			MaximumOutputBytes: 4 << 20,
		},
	)
	if err != nil {
		return applicationCheckpointing{}, err
	}
	guard, err := checkpoint.NewRedactionSecretGuard(pipeline)
	if err != nil {
		pipeline.Close()
		return applicationCheckpointing{}, err
	}
	captures, err := checkpoint.NewService(
		worktrees,
		repositories,
		repositories,
		guard,
	)
	if err != nil {
		pipeline.Close()
		return applicationCheckpointing{}, err
	}
	adapter, err := NewLaneACheckpointAdapter(
		captures,
		storedCheckpointPlanRevisionSource{
			repositories: repositories,
		},
	)
	if err != nil {
		pipeline.Close()
		return applicationCheckpointing{}, err
	}
	return applicationCheckpointing{
		checkpoints: adapter,
		worktrees:   worktrees,
		redactor:    pipeline,
		close: func() error {
			pipeline.Close()
			return nil
		},
	}, nil
}

func newApplicationTaskControls(
	repositories *storage.Repositories,
	supervisor *Supervisor,
	checkpointing applicationCheckpointing,
) (*TaskControlService, error) {
	if checkpointing.checkpoints == nil ||
		checkpointing.worktrees == nil {
		return nil, errors.New(
			"default task control checkpointing is unavailable",
		)
	}
	facts, err := NewStoredTaskControlFacts(repositories)
	if err != nil {
		return nil, err
	}
	workers, err := NewTaskControlWorkerAdapter(supervisor)
	if err != nil {
		return nil, err
	}
	compatibility, err := NewDurableRecoveryCompatibilitySource(
		repositories,
	)
	if err != nil {
		return nil, err
	}
	actions, err := NewDurableRecoveryActionSource(repositories)
	if err != nil {
		return nil, err
	}
	observations, err := NewDurableRecoveryObservationSource(
		repositories,
		checkpointing.worktrees,
		compatibility,
		actions,
		workspace.ExecRunner{},
	)
	if err != nil {
		return nil, err
	}
	resume, err := NewRecoveryResumeVerifier(
		repositories,
		observations,
		repositories,
	)
	if err != nil {
		return nil, err
	}
	return NewTaskControlService(
		TaskControlDependencies{
			Store:       repositories,
			Actions:     NewActiveActionRegistry(),
			Workers:     workers,
			Checkpoints: checkpointing.checkpoints,
			Facts:       facts,
			Resume:      resume,
		},
	)
}
