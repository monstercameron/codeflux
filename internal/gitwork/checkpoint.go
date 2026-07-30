package gitwork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

const checkpointReferencePrefix = "refs/codeflux/checkpoints/"

// CheckpointRepository is the durable recovery-checkpoint boundary.
type CheckpointRepository interface {
	CreateCheckpoint(context.Context, storage.CreateCheckpoint) (storage.Checkpoint, error)
	GetCheckpoint(context.Context, domain.CheckpointID) (storage.Checkpoint, error)
}

// CreateCheckpointInput binds a checkpoint to the current task event boundary.
type CreateCheckpointInput struct {
	ID             domain.CheckpointID
	TaskID         domain.TaskID
	RunID          *domain.RunID
	EventSequence  uint64
	IdempotencyKey string
}

// RestoreCheckpointInput is explicit authority to discard post-checkpoint
// worktree changes when requested.
type RestoreCheckpointInput struct {
	TaskID                 domain.TaskID
	CheckpointID           domain.CheckpointID
	DiscardCurrentApproved bool
}

// SetCheckpointRepository binds durable checkpoint persistence.
func (service *Service) SetCheckpointRepository(repository CheckpointRepository) {
	service.checkpoints = repository
}

// CreateCheckpoint commits current task changes without hooks, advances the
// expected Codeflux-owned HEAD, pins a private recovery ref, and persists the
// exact diff identity.
func (service *Service) CreateCheckpoint(
	ctx context.Context,
	input CreateCheckpointInput,
) (storage.Checkpoint, error) {
	if service.checkpoints == nil {
		return storage.Checkpoint{}, errors.New("checkpoint repository is required")
	}
	if input.ID.IsZero() || input.TaskID.IsZero() {
		return storage.Checkpoint{}, errors.New("checkpoint and task IDs are required")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 255 {
		return storage.Checkpoint{}, errors.New("checkpoint idempotency key is invalid")
	}
	if existing, err := service.checkpoints.GetCheckpoint(ctx, input.ID); err == nil {
		if existing.TaskID != input.TaskID ||
			existing.IdempotencyKey != input.IdempotencyKey {
			return storage.Checkpoint{}, storage.ErrConflict
		}
		return existing, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return storage.Checkpoint{}, err
	}

	verification, err := service.VerifyTaskWorktree(ctx, input.TaskID)
	if err != nil {
		return storage.Checkpoint{}, err
	}
	binding := verification.Binding
	previousHead := binding.HeadRevision
	head := previousHead
	advanced := false
	if verification.Dirty {
		if _, err := service.runner.Run(
			ctx,
			binding.WorktreePath,
			"git",
			"add",
			"-A",
			"--",
		); err != nil {
			return storage.Checkpoint{}, fmt.Errorf("stage checkpoint changes: %w", err)
		}
		if _, err := service.runner.Run(
			ctx,
			binding.WorktreePath,
			"git",
			"-c",
			"core.hooksPath="+service.disabledHooksPath(),
			"-c",
			"commit.gpgsign=false",
			"-c",
			"user.name=Codeflux Checkpoint",
			"-c",
			"user.email=checkpoint@codeflux.invalid",
			"commit",
			"--no-verify",
			"-m",
			"Codeflux checkpoint "+input.ID.String(),
		); err != nil {
			_, _ = service.runner.Run(
				context.Background(),
				binding.WorktreePath,
				"git",
				"reset",
				"--mixed",
				previousHead,
			)
			return storage.Checkpoint{}, fmt.Errorf("commit checkpoint changes: %w", err)
		}
		head, err = service.gitText(ctx, binding.WorktreePath, "rev-parse", "HEAD")
		if err != nil {
			return storage.Checkpoint{}, err
		}
		binding, err = service.bindings.AdvanceWorktreeBinding(
			ctx,
			storage.AdvanceWorktreeBinding{
				TaskID: input.TaskID, ExpectedRevision: binding.Revision,
				ExpectedHead: previousHead, HeadRevision: head,
			},
		)
		if err != nil {
			_, _ = service.runner.Run(
				context.Background(),
				verification.Binding.WorktreePath,
				"git",
				"reset",
				"--mixed",
				previousHead,
			)
			return storage.Checkpoint{}, err
		}
		advanced = true
	}
	reference := checkpointReferencePrefix + input.ID.String()
	if _, err := service.runner.Run(
		ctx,
		binding.WorktreePath,
		"git",
		"update-ref",
		reference,
		head,
	); err != nil {
		service.rollbackCheckpointCreation(binding, previousHead, head, reference, advanced)
		return storage.Checkpoint{}, fmt.Errorf("pin checkpoint revision: %w", err)
	}
	diff, err := service.GetTaskDiff(ctx, TaskDiffQuery{TaskID: input.TaskID})
	if err != nil {
		service.rollbackCheckpointCreation(binding, previousHead, head, reference, advanced)
		return storage.Checkpoint{}, err
	}
	checkpoint, err := service.checkpoints.CreateCheckpoint(
		ctx,
		storage.CreateCheckpoint{
			ID: input.ID, TaskID: input.TaskID, RunID: input.RunID,
			State:              domain.CheckpointStateReady,
			RepositoryRevision: head, WorktreeDiffHash: diff.Identity,
			EventSequence: input.EventSequence, IdempotencyKey: input.IdempotencyKey,
		},
	)
	if err != nil {
		service.rollbackCheckpointCreation(binding, previousHead, head, reference, advanced)
		return storage.Checkpoint{}, err
	}
	return checkpoint, nil
}

func (service *Service) rollbackCheckpointCreation(
	binding storage.WorktreeBinding,
	previousHead string,
	head string,
	reference string,
	advanced bool,
) {
	_, _ = service.runner.Run(
		context.Background(),
		binding.WorktreePath,
		"git",
		"update-ref",
		"-d",
		reference,
	)
	if !advanced {
		return
	}
	_, _ = service.bindings.AdvanceWorktreeBinding(
		context.Background(),
		storage.AdvanceWorktreeBinding{
			TaskID: binding.TaskID, ExpectedRevision: binding.Revision,
			ExpectedHead: head, HeadRevision: previousHead,
		},
	)
	_, _ = service.runner.Run(
		context.Background(),
		binding.WorktreePath,
		"git",
		"reset",
		"--mixed",
		previousHead,
	)
}

// RestoreCheckpoint verifies the private ref and explicit discard authority
// before moving only the task branch back to a recorded checkpoint.
func (service *Service) RestoreCheckpoint(
	ctx context.Context,
	input RestoreCheckpointInput,
) (storage.WorktreeBinding, error) {
	if service.checkpoints == nil {
		return storage.WorktreeBinding{}, errors.New("checkpoint repository is required")
	}
	checkpoint, err := service.checkpoints.GetCheckpoint(ctx, input.CheckpointID)
	if err != nil {
		return storage.WorktreeBinding{}, err
	}
	if checkpoint.TaskID != input.TaskID ||
		checkpoint.State != domain.CheckpointStateReady {
		return storage.WorktreeBinding{}, errors.New("checkpoint is not a ready checkpoint for this task")
	}
	verification, err := service.VerifyTaskWorktree(ctx, input.TaskID)
	if err != nil {
		return storage.WorktreeBinding{}, err
	}
	if verification.Dirty && !input.DiscardCurrentApproved {
		return storage.WorktreeBinding{}, ErrApprovalRequired
	}
	binding := verification.Binding
	reference := checkpointReferencePrefix + input.CheckpointID.String()
	pinned, err := service.gitText(ctx, binding.WorktreePath, "rev-parse", "--verify", reference)
	if err != nil || pinned != checkpoint.RepositoryRevision {
		return storage.WorktreeBinding{}, errors.New("checkpoint revision is not pinned to its durable record")
	}
	untracked, err := service.untrackedPaths(ctx, binding.WorktreePath)
	if err != nil {
		return storage.WorktreeBinding{}, err
	}
	if _, err := service.runner.Run(
		ctx,
		binding.WorktreePath,
		"git",
		"reset",
		"--hard",
		checkpoint.RepositoryRevision,
	); err != nil {
		return storage.WorktreeBinding{}, fmt.Errorf("restore checkpoint revision: %w", err)
	}
	for _, untrackedPath := range untracked {
		resolved, resolveErr := ResolveTaskPath(binding, untrackedPath)
		if resolveErr != nil {
			return storage.WorktreeBinding{}, resolveErr
		}
		if removeErr := os.Remove(resolved.Absolute); removeErr != nil &&
			!errors.Is(removeErr, os.ErrNotExist) {
			return storage.WorktreeBinding{}, fmt.Errorf("remove post-checkpoint file: %w", removeErr)
		}
		removeEmptyParents(binding.WorktreePath, filepath.Dir(resolved.Absolute))
	}
	if checkpoint.RepositoryRevision == binding.HeadRevision {
		return binding, nil
	}
	return service.bindings.AdvanceWorktreeBinding(
		ctx,
		storage.AdvanceWorktreeBinding{
			TaskID: input.TaskID, ExpectedRevision: binding.Revision,
			ExpectedHead: binding.HeadRevision,
			HeadRevision: checkpoint.RepositoryRevision,
		},
	)
}

func (service *Service) untrackedPaths(
	ctx context.Context,
	worktreePath string,
) ([]string, error) {
	result, err := service.runner.Run(
		ctx,
		worktreePath,
		"git",
		"ls-files",
		"--others",
		"--exclude-standard",
		"-z",
	)
	if err != nil {
		return nil, fmt.Errorf("list post-checkpoint files: %w", err)
	}
	return splitNULPaths(result.Stdout), nil
}

func removeEmptyParents(root, directory string) {
	for pathWithin(root, directory) && directory != root {
		if err := os.Remove(directory); err != nil {
			return
		}
		directory = filepath.Dir(directory)
	}
}
