package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
)

func (repositories *Repositories) CreateWorktreeBinding(
	ctx context.Context,
	input CreateWorktreeBinding,
) (WorktreeBinding, error) {
	if err := validateCreateWorktreeBinding(input); err != nil {
		return WorktreeBinding{}, err
	}
	now, micros := repositories.timestamp()
	binding := WorktreeBinding{
		WorkspaceID:  input.WorkspaceID,
		TaskID:       input.TaskID,
		RepositoryID: input.RepositoryID,
		BaseRevision: input.BaseRevision,
		HeadRevision: input.HeadRevision,
		BranchName:   input.BranchName,
		WorktreePath: input.WorktreePath,
		State:        WorktreeBindingActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO workspaces (
				id, repository_id, canonical_path, state,
				created_at_unix_micros, updated_at_unix_micros, revision
			) VALUES (?, ?, ?, 'active', ?, ?, 0)`,
			input.WorkspaceID,
			input.RepositoryID,
			input.WorktreePath,
			micros,
			micros,
		); err != nil {
			return repositoryWriteError("create task workspace", err)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO worktree_bindings (
				workspace_id, repository_id, base_revision, branch_name,
				worktree_path, state, created_at_unix_micros,
				updated_at_unix_micros, revision, task_id, head_revision
			) VALUES (?, ?, ?, ?, ?, 'active', ?, ?, 0, ?, ?)`,
			input.WorkspaceID,
			input.RepositoryID,
			input.BaseRevision,
			input.BranchName,
			input.WorktreePath,
			micros,
			micros,
			input.TaskID,
			input.HeadRevision,
		); err != nil {
			return repositoryWriteError("create worktree binding", err)
		}
		return nil
	})
	if err != nil {
		return WorktreeBinding{}, err
	}
	return binding, nil
}

func (repositories *Repositories) GetWorktreeBinding(
	ctx context.Context,
	taskID domain.TaskID,
) (WorktreeBinding, error) {
	if taskID.IsZero() {
		return WorktreeBinding{}, errors.New("task ID must not be empty")
	}
	return scanWorktreeBinding(repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT workspace_id, task_id, repository_id, base_revision,
		        head_revision, branch_name, worktree_path, state,
		        created_at_unix_micros, updated_at_unix_micros, revision
		 FROM worktree_bindings
		 WHERE task_id = ?`,
		taskID,
	), "get worktree binding")
}

func (repositories *Repositories) AdvanceWorktreeBinding(
	ctx context.Context,
	input AdvanceWorktreeBinding,
) (WorktreeBinding, error) {
	if input.TaskID.IsZero() {
		return WorktreeBinding{}, errors.New("task ID must not be empty")
	}
	if err := validateGitObjectID("expected worktree HEAD", input.ExpectedHead); err != nil {
		return WorktreeBinding{}, err
	}
	if err := validateGitObjectID("new worktree HEAD", input.HeadRevision); err != nil {
		return WorktreeBinding{}, err
	}
	_, micros := repositories.timestamp()
	result, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE worktree_bindings
		 SET head_revision = ?, updated_at_unix_micros = ?, revision = revision + 1
		 WHERE task_id = ? AND state = 'active' AND revision = ?
		   AND head_revision = ?`,
		input.HeadRevision,
		micros,
		input.TaskID,
		input.ExpectedRevision,
		input.ExpectedHead,
	)
	if err != nil {
		return WorktreeBinding{}, repositoryWriteError("advance worktree binding", err)
	}
	if err := requireOneAffected(result, "advance worktree binding"); err != nil {
		return WorktreeBinding{}, err
	}
	return repositories.GetWorktreeBinding(ctx, input.TaskID)
}

func (repositories *Repositories) TransitionWorktreeBinding(
	ctx context.Context,
	input TransitionWorktreeBinding,
) (WorktreeBinding, error) {
	if input.TaskID.IsZero() {
		return WorktreeBinding{}, errors.New("task ID must not be empty")
	}
	if !input.From.isValid() || !input.To.isValid() || input.From == input.To {
		return WorktreeBinding{}, errors.New("worktree state transition is invalid")
	}
	if input.From != WorktreeBindingActive {
		return WorktreeBinding{}, errors.New("only active worktrees may transition")
	}
	workspaceState := "closed"
	if input.To == WorktreeBindingRecoveryRequired {
		workspaceState = "recovery-required"
	}
	_, micros := repositories.timestamp()
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE worktree_bindings
			 SET state = ?, updated_at_unix_micros = ?, revision = revision + 1
			 WHERE task_id = ? AND state = ? AND revision = ?`,
			input.To,
			micros,
			input.TaskID,
			input.From,
			input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("transition worktree binding", err)
		}
		if err := requireOneAffected(result, "transition worktree binding"); err != nil {
			return err
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE workspaces
			 SET state = ?, updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = (
			     SELECT workspace_id FROM worktree_bindings WHERE task_id = ?
			 )`,
			workspaceState,
			micros,
			input.TaskID,
		); err != nil {
			return repositoryWriteError("transition task workspace", err)
		}
		return nil
	})
	if err != nil {
		return WorktreeBinding{}, err
	}
	return repositories.GetWorktreeBinding(ctx, input.TaskID)
}

func validateCreateWorktreeBinding(input CreateWorktreeBinding) error {
	switch {
	case input.WorkspaceID.IsZero():
		return errors.New("workspace ID must not be empty")
	case input.TaskID.IsZero():
		return errors.New("task ID must not be empty")
	case input.RepositoryID.IsZero():
		return errors.New("repository ID must not be empty")
	}
	if err := validateGitObjectID("base revision", input.BaseRevision); err != nil {
		return err
	}
	if err := validateGitObjectID("head revision", input.HeadRevision); err != nil {
		return err
	}
	if err := validateBounded("worktree branch name", input.BranchName, 255); err != nil {
		return err
	}
	return validateBounded("worktree path", input.WorktreePath, 4096)
}

func validateGitObjectID(label, value string) error {
	if len(value) != 40 && len(value) != 64 {
		return errors.New(label + " must be a Git object ID")
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return errors.New(label + " must be a lowercase Git object ID")
		}
	}
	return nil
}

func (state WorktreeBindingState) isValid() bool {
	switch state {
	case WorktreeBindingActive, WorktreeBindingReleased, WorktreeBindingRecoveryRequired:
		return true
	default:
		return false
	}
}

func scanWorktreeBinding(
	row rowScanner,
	operation string,
) (WorktreeBinding, error) {
	var (
		binding       WorktreeBinding
		createdMicros int64
		updatedMicros int64
	)
	err := row.Scan(
		&binding.WorkspaceID,
		&binding.TaskID,
		&binding.RepositoryID,
		&binding.BaseRevision,
		&binding.HeadRevision,
		&binding.BranchName,
		&binding.WorktreePath,
		&binding.State,
		&createdMicros,
		&updatedMicros,
		&binding.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WorktreeBinding{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return WorktreeBinding{}, classify(operation, err)
	}
	binding.CreatedAt = repositoryTime(createdMicros)
	binding.UpdatedAt = repositoryTime(updatedMicros)
	return binding, nil
}

func requireOneAffected(result sql.Result, operation string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return classify(operation, err)
	}
	if affected != 1 {
		return typedError(
			ErrStaleRevision,
			operation,
			fmt.Errorf("updated %d rows, want 1", affected),
		)
	}
	return nil
}
