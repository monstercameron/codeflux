package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// workspaceApplication answers the workspace service's questions.
//
// It exists because those questions have two different authorities. Which
// repositories exist and what they are called is the database's answer; what
// branch one is on and whether it is dirty is Git's, and only Git's. Reading
// the second from stored columns is how a top bar comes to say "main /
// uncommitted changes" about a tree that has since moved on.
//
// It also translates. The transport package is an adapter and may not depend
// on the storage package, so the two describe a repository with their own
// types and this composition root converts between them.
type workspaceApplication struct {
	repositories *storage.Repositories
	worktrees    *gitwork.Service
}

// newWorkspaceApplication composes the two authorities.
func newWorkspaceApplication(
	repositories *storage.Repositories,
	worktrees *gitwork.Service,
) (workspaceApplication, error) {
	if repositories == nil {
		return workspaceApplication{}, errors.New("repositories are required")
	}
	return workspaceApplication{repositories: repositories, worktrees: worktrees}, nil
}

// ListRepositories returns the repositories a person can choose between.
func (application workspaceApplication) ListRepositories(
	ctx context.Context,
	limit int,
) ([]transport.RepositoryRecord, error) {
	listed, err := application.repositories.ListRepositories(ctx, limit)
	if err != nil {
		return nil, translateWorkspaceError(err)
	}
	records := make([]transport.RepositoryRecord, 0, len(listed))
	for _, repository := range listed {
		records = append(records, repositoryRecord(repository))
	}
	return records, nil
}

// GetRepository returns one repository.
func (application workspaceApplication) GetRepository(
	ctx context.Context,
	id domain.RepositoryID,
) (transport.RepositoryRecord, error) {
	repository, err := application.repositories.GetRepository(ctx, id)
	if err != nil {
		return transport.RepositoryRecord{}, translateWorkspaceError(err)
	}
	return repositoryRecord(repository), nil
}

// GetWorkspaceScope returns one workspace.
func (application workspaceApplication) GetWorkspaceScope(
	ctx context.Context,
	id domain.WorkspaceID,
) (transport.WorkspaceRecord, error) {
	scope, err := application.repositories.GetWorkspaceScope(ctx, id)
	if err != nil {
		return transport.WorkspaceRecord{}, translateWorkspaceError(err)
	}
	return transport.WorkspaceRecord{
		WorkspaceID: scope.WorkspaceID, RepositoryID: scope.RepositoryID,
	}, nil
}

// ReadRepositoryState reports what Git says about a repository right now.
func (application workspaceApplication) ReadRepositoryState(
	ctx context.Context,
	repository transport.RepositoryRecord,
) (transport.RepositoryGitState, error) {
	if application.worktrees == nil {
		// Without a Git service there is nothing live to report. Returning an
		// error rather than a zero state matters: a zero state would be read as
		// "clean, no branch", which is an answer, and this is the absence of
		// one.
		return transport.RepositoryGitState{}, errors.New("no Git service is available")
	}
	state, err := application.worktrees.ReadRepositoryState(ctx, repository.CanonicalPath)
	if err != nil {
		return transport.RepositoryGitState{}, err
	}
	return transport.RepositoryGitState{
		Branch:           state.Branch,
		HeadRevision:     state.HeadRevision,
		Detached:         state.Detached,
		Dirty:            state.Dirty,
		ChangedPathCount: state.ChangedPathCount,
	}, nil
}

// repositoryRecord converts one stored repository.
func repositoryRecord(repository storage.Repository) transport.RepositoryRecord {
	return transport.RepositoryRecord{
		ID:            repository.ID,
		CanonicalPath: repository.CanonicalPath,
		GitIdentity:   repository.GitIdentity,
		Revision:      repository.Revision,
	}
}

// translateWorkspaceError maps the store's absence onto the service's.
//
// Without this the service would have to recognize a storage error, which is
// exactly the dependency the boundary forbids, and a missing repository would
// have surfaced to a browser as an internal failure.
func translateWorkspaceError(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return transport.ErrWorkspaceTargetNotFound
	}
	return err
}

var _ transport.WorkspaceApplication = workspaceApplication{}
