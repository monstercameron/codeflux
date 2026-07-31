package gitwork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// CreateCheckpoint preserves current task changes through an isolated Git
// index, pins a private recovery ref without moving HEAD or cleaning/staging
// the worktree, and persists the exact diff identity.
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

	snapshot, err := service.CaptureCheckpointWorktreeState(
		ctx,
		input.TaskID,
		input.ID,
	)
	if err != nil {
		return storage.Checkpoint{}, err
	}
	checkpoint, err := service.checkpoints.CreateCheckpoint(
		ctx,
		storage.CreateCheckpoint{
			ID: input.ID, TaskID: input.TaskID, RunID: input.RunID,
			State:              domain.CheckpointStateReady,
			RepositoryRevision: snapshot.PreservedRevision,
			WorktreeDiffHash:   snapshot.DiffSHA256,
			EventSequence:      input.EventSequence, IdempotencyKey: input.IdempotencyKey,
		},
	)
	if err != nil {
		cleanupContext, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()
		_ = service.RemoveCheckpointWorktreePreservation(
			cleanupContext,
			input.TaskID,
			input.ID,
			snapshot.PreservedRef,
			snapshot.PreservedRevision,
		)
		return storage.Checkpoint{}, err
	}
	return checkpoint, nil
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
