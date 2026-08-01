package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// LocalBootstrap is the scope a freshly started coordinator needs to be usable.
//
// A database with no repository row reports first-run to the browser, and the
// browser then has no workspace to attach a session to, so the whole interface
// stays in local preview: sample content, a composer that refuses to send, and
// a graph that says it is not available. Nothing inside the interface could
// create that first row, so a fresh install had no way out of preview.
type LocalBootstrap struct {
	ProjectID    domain.ProjectID
	RepositoryID domain.RepositoryID
	WorkspaceID  domain.WorkspaceID
	ThreadID     domain.ThreadID
	SessionID    domain.SessionID
	// Created records whether this call made the scope or found it. A start
	// that reports creating a repository every time would hide the fact that
	// it is reusing one.
	Created bool
}

// EnsureLocalBootstrap makes one repository, workspace, thread, and session
// usable, and is idempotent on the repository's canonical path.
//
// The authority for this comes from the person who ran the command, on their
// own machine, naming their own directory. That is why it lives here and not
// behind an RPC: a browser client asking a coordinator to take authority over
// an arbitrary path is a different and much larger decision.
func (repositories *Repositories) EnsureLocalBootstrap(
	ctx context.Context,
	canonicalPath string,
	gitIdentity string,
	projectName string,
) (LocalBootstrap, error) {
	if repositories == nil || repositories.database == nil {
		return LocalBootstrap{}, errors.New("repositories are unavailable")
	}
	path := strings.TrimSpace(canonicalPath)
	if path == "" || !filepath.IsAbs(path) {
		return LocalBootstrap{}, errors.New("an absolute repository path is required")
	}
	identity := strings.TrimSpace(gitIdentity)
	if identity == "" {
		return LocalBootstrap{}, errors.New("a Git identity is required to bind changes to a base revision")
	}
	if strings.TrimSpace(projectName) == "" {
		projectName = filepath.Base(path)
	}

	scope, err := repositories.newLocalBootstrapIdentities()
	if err != nil {
		return LocalBootstrap{}, err
	}
	existing, found, err := repositories.findLocalBootstrap(ctx, path)
	if err != nil {
		return LocalBootstrap{}, err
	}
	if found {
		if !existing.ThreadID.IsZero() {
			return existing, nil
		}
		// The repository is here but its first thread is not, which is what a
		// start interrupted between the two writes leaves behind. Returning
		// this scope as-is would hand the browser a workspace with nowhere to
		// talk, so the missing half is made now.
		existing.ThreadID, existing.SessionID = scope.ThreadID, scope.SessionID
		return existing, repositories.ensureBootstrapThread(ctx, existing)
	}
	now := repositories.now().UTC().UnixMicro()
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO projects (id, name, created_at_unix_micros, updated_at_unix_micros, revision)
			 VALUES (?, ?, ?, ?, 1)`,
			scope.ProjectID, projectName, now, now,
		); err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO repositories (
				id, project_id, canonical_path, git_identity,
				created_at_unix_micros, updated_at_unix_micros, revision
			) VALUES (?, ?, ?, ?, ?, ?, 1)`,
			scope.RepositoryID, scope.ProjectID, path, identity, now, now,
		); err != nil {
			return fmt.Errorf("create repository: %w", err)
		}
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO workspaces (
				id, repository_id, canonical_path, state,
				created_at_unix_micros, updated_at_unix_micros, revision
			) VALUES (?, ?, ?, 'active', ?, ?, 1)`,
			scope.WorkspaceID, scope.RepositoryID, path, now, now,
		); err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}
		return nil
	})
	if err != nil {
		return LocalBootstrap{}, classify("ensure local bootstrap", err)
	}
	scope.Created = true
	if err := repositories.ensureBootstrapThread(ctx, scope); err != nil {
		return LocalBootstrap{}, err
	}
	return scope, nil
}

// ensureBootstrapThread creates the scope's first thread.
//
// It goes through CreateThreadCommit rather than an INSERT here because that
// call owns rules this must not bypass: it mints the session in the same
// transaction as the thread, appends the thread-created event that every
// projection reads, and keys the whole thing so a repeated start is a no-op
// rather than a second empty thread.
func (repositories *Repositories) ensureBootstrapThread(
	ctx context.Context,
	scope LocalBootstrap,
) error {
	_, err := repositories.CreateThreadCommit(ctx, CreateThread{
		ID: scope.ThreadID, SessionID: scope.SessionID, ProjectID: scope.ProjectID,
		RepositoryID: scope.RepositoryID, WorkspaceID: scope.WorkspaceID,
		Title:          "First thread",
		IdempotencyKey: "local-bootstrap:" + scope.RepositoryID.String(),
	})
	if err != nil && !errors.Is(err, ErrConflict) {
		return fmt.Errorf("create thread: %w", err)
	}
	return nil
}

// findLocalBootstrap returns an existing scope for a path.
func (repositories *Repositories) findLocalBootstrap(
	ctx context.Context,
	path string,
) (LocalBootstrap, bool, error) {
	var scope LocalBootstrap
	row := repositories.database.sql.QueryRowContext(ctx,
		`SELECT repositories.id, repositories.project_id, workspaces.id
		 FROM repositories
		 JOIN workspaces ON workspaces.repository_id = repositories.id
		 WHERE repositories.canonical_path = ?
		   AND repositories.deleted_at_unix_micros IS NULL
		   AND workspaces.state = 'active'
		 ORDER BY workspaces.created_at_unix_micros
		 LIMIT 1`, path)
	if err := row.Scan(&scope.RepositoryID, &scope.ProjectID, &scope.WorkspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LocalBootstrap{}, false, nil
		}
		return LocalBootstrap{}, false, classify("find local bootstrap", err)
	}
	// An existing repository may have several threads; the earliest is the one
	// a returning start reopens, so the same invocation lands in the same place
	// every time.
	// The session is read through its own table because a thread does not carry
	// one: sessions point at threads, not the other way round.
	thread := repositories.database.sql.QueryRowContext(ctx,
		`SELECT threads.id, sessions.id FROM threads
		 JOIN sessions ON sessions.thread_id = threads.id
		 WHERE threads.repository_id = ? AND threads.deleted_at_unix_micros IS NULL
		 ORDER BY threads.created_at_unix_micros, threads.id
		 LIMIT 1`, scope.RepositoryID)
	if err := thread.Scan(&scope.ThreadID, &scope.SessionID); err != nil &&
		!errors.Is(err, sql.ErrNoRows) {
		return LocalBootstrap{}, false, classify("find local bootstrap thread", err)
	}
	return scope, true, nil
}

// newLocalBootstrapIdentities mints the identities one scope needs.
func (repositories *Repositories) newLocalBootstrapIdentities() (LocalBootstrap, error) {
	projectID, err := domain.NewProjectID()
	if err != nil {
		return LocalBootstrap{}, err
	}
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		return LocalBootstrap{}, err
	}
	workspaceID, err := domain.NewWorkspaceID()
	if err != nil {
		return LocalBootstrap{}, err
	}
	threadID, err := domain.NewThreadID()
	if err != nil {
		return LocalBootstrap{}, err
	}
	sessionID, err := domain.NewSessionID()
	if err != nil {
		return LocalBootstrap{}, err
	}
	return LocalBootstrap{
		ProjectID: projectID, RepositoryID: repositoryID,
		WorkspaceID: workspaceID, ThreadID: threadID, SessionID: sessionID,
	}, nil
}
