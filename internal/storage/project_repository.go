package storage

import (
	"context"
	"database/sql"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

func (repositories *Repositories) CreateProject(
	ctx context.Context,
	input CreateProject,
) (Project, error) {
	if input.ID.IsZero() {
		return Project{}, errors.New("project ID must not be empty")
	}
	if err := validateBounded("project name", input.Name, 255); err != nil {
		return Project{}, err
	}
	now, micros := repositories.timestamp()
	project := Project{
		ID:        input.ID,
		Name:      input.Name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO projects (
				id, name, created_at_unix_micros, updated_at_unix_micros, revision
			) VALUES (?, ?, ?, ?, 0)`,
			input.ID,
			input.Name,
			micros,
			micros,
		)
		return err
	})
	if err != nil {
		return Project{}, repositoryWriteError("create project", err)
	}
	return project, nil
}

func (repositories *Repositories) GetProject(
	ctx context.Context,
	id domain.ProjectID,
) (Project, error) {
	if id.IsZero() {
		return Project{}, errors.New("project ID must not be empty")
	}
	var (
		project       Project
		createdMicros int64
		updatedMicros int64
	)
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, name, created_at_unix_micros, updated_at_unix_micros, revision
		 FROM projects
		 WHERE id = ? AND deleted_at_unix_micros IS NULL`,
		id,
	).Scan(
		&project.ID,
		&project.Name,
		&createdMicros,
		&updatedMicros,
		&project.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, typedError(ErrNotFound, "get project", err)
	}
	if err != nil {
		return Project{}, classify("get project", err)
	}
	project.CreatedAt = repositoryTime(createdMicros)
	project.UpdatedAt = repositoryTime(updatedMicros)
	return project, nil
}

func (repositories *Repositories) CreateRepository(
	ctx context.Context,
	input CreateRepository,
) (Repository, error) {
	switch {
	case input.ID.IsZero():
		return Repository{}, errors.New("repository ID must not be empty")
	case input.ProjectID.IsZero():
		return Repository{}, errors.New("project ID must not be empty")
	}
	if err := validateBounded("canonical repository path", input.CanonicalPath, 4096); err != nil {
		return Repository{}, err
	}
	if err := validateBounded("Git identity", input.GitIdentity, 512); err != nil {
		return Repository{}, err
	}
	now, micros := repositories.timestamp()
	repository := Repository{
		ID:            input.ID,
		ProjectID:     input.ProjectID,
		CanonicalPath: input.CanonicalPath,
		GitIdentity:   input.GitIdentity,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO repositories (
				id, project_id, canonical_path, git_identity,
				created_at_unix_micros, updated_at_unix_micros, revision
			) VALUES (?, ?, ?, ?, ?, ?, 0)`,
			input.ID,
			input.ProjectID,
			input.CanonicalPath,
			input.GitIdentity,
			micros,
			micros,
		)
		return err
	})
	if err != nil {
		return Repository{}, repositoryWriteError("create repository", err)
	}
	return repository, nil
}

func (repositories *Repositories) GetRepository(
	ctx context.Context,
	id domain.RepositoryID,
) (Repository, error) {
	if id.IsZero() {
		return Repository{}, errors.New("repository ID must not be empty")
	}
	var (
		repository    Repository
		createdMicros int64
		updatedMicros int64
	)
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, project_id, canonical_path, git_identity,
		        created_at_unix_micros, updated_at_unix_micros, revision
		 FROM repositories
		 WHERE id = ? AND deleted_at_unix_micros IS NULL`,
		id,
	).Scan(
		&repository.ID,
		&repository.ProjectID,
		&repository.CanonicalPath,
		&repository.GitIdentity,
		&createdMicros,
		&updatedMicros,
		&repository.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, typedError(ErrNotFound, "get repository", err)
	}
	if err != nil {
		return Repository{}, classify("get repository", err)
	}
	repository.CreatedAt = repositoryTime(createdMicros)
	repository.UpdatedAt = repositoryTime(updatedMicros)
	return repository, nil
}

// UpdateRepositoryGitIdentity resynchronises the stored Git identity to a
// revision actually observed for this repository (AUDIT-011b).
//
// EnsureLocalBootstrap binds git_identity once, at first open, and nothing
// updated it after that: a task's worktree is created from whatever HEAD is
// current at launch time, which drifts from the bootstrap-time value the
// moment the canonical repository gains a single commit. Context selection
// later insists the two match exactly, so every launch after the first
// degraded to the worktree-listing fallback. The caller that actually
// resolves a live revision — task_run_launcher.go's resolveWorktree — is
// expected to call this immediately after, so the column reflects the
// revision the run's own worktree was created from rather than a value from
// whenever the repository happened to be opened.
func (repositories *Repositories) UpdateRepositoryGitIdentity(
	ctx context.Context,
	id domain.RepositoryID,
	gitIdentity string,
) error {
	if id.IsZero() {
		return errors.New("repository ID must not be empty")
	}
	if err := validateBounded("Git identity", gitIdentity, 512); err != nil {
		return err
	}
	_, micros := repositories.timestamp()
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		result, err := transaction.sql.ExecContext(ctx,
			`UPDATE repositories
			 SET git_identity = ?, updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND deleted_at_unix_micros IS NULL`,
			gitIdentity, micros, id,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return sql.ErrNoRows
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return typedError(ErrNotFound, "update repository git identity", err)
	}
	if err != nil {
		return repositoryWriteError("update repository git identity", err)
	}
	return nil
}

func repositoryWriteError(operation string, err error) error {
	switch repositoryConstraintKind(err) {
	case ErrConflict:
		return typedError(ErrConflict, operation, err)
	case ErrConstraint:
		return typedError(ErrConstraint, operation, err)
	}
	return classify(operation, err)
}

// MaximumRepositoryPage bounds one page of repositories.
//
// A list with no bound is a list that eventually returns everything at once,
// which for a workspace picker means a page that takes longer to render the
// longer somebody has used the product.
const MaximumRepositoryPage = 200

// ListRepositories returns the repositories a workspace picker can offer.
//
// Ordering is by canonical path rather than by creation time: a person
// choosing a repository is looking for a name they know, not for the one they
// added most recently.
func (repositories *Repositories) ListRepositories(
	ctx context.Context,
	limit int,
) ([]Repository, error) {
	if limit <= 0 || limit > MaximumRepositoryPage {
		limit = MaximumRepositoryPage
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, project_id, canonical_path, git_identity,
		        created_at_unix_micros, updated_at_unix_micros, revision
		 FROM repositories
		 WHERE deleted_at_unix_micros IS NULL
		 ORDER BY canonical_path, id
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, classify("list repositories", err)
	}
	defer rows.Close()

	listed := make([]Repository, 0, limit)
	for rows.Next() {
		var (
			repository    Repository
			createdMicros int64
			updatedMicros int64
		)
		if err := rows.Scan(
			&repository.ID, &repository.ProjectID, &repository.CanonicalPath,
			&repository.GitIdentity, &createdMicros, &updatedMicros, &repository.Revision,
		); err != nil {
			return nil, classify("list repositories", err)
		}
		repository.CreatedAt = repositoryTime(createdMicros)
		repository.UpdatedAt = repositoryTime(updatedMicros)
		listed = append(listed, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list repositories", err)
	}
	return listed, nil
}
