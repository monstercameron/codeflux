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

func repositoryWriteError(operation string, err error) error {
	switch repositoryConstraintKind(err) {
	case ErrConflict:
		return typedError(ErrConflict, operation, err)
	case ErrConstraint:
		return typedError(ErrConstraint, operation, err)
	}
	return classify(operation, err)
}
