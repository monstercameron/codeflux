package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/review"
	"codeflux.dev/codeflux/internal/storage"
)

// EditorWorkspaceStore resolves the active workspace record that binds an
// editor request to one repository root.
type EditorWorkspaceStore interface {
	GetWorkspaceScope(context.Context, domain.WorkspaceID) (storage.WorkspaceScope, error)
}

// EditorWorkspaceResolver adapts authoritative workspace storage to the review
// service without exposing storage operations to the transport layer.
type EditorWorkspaceResolver struct {
	store EditorWorkspaceStore
}

// NewEditorWorkspaceResolver validates the editor workspace authority port.
func NewEditorWorkspaceResolver(store EditorWorkspaceStore) (*EditorWorkspaceResolver, error) {
	if store == nil {
		return nil, errors.New("editor workspace store is required")
	}
	return &EditorWorkspaceResolver{store: store}, nil
}

// ResolveEditorWorkspace returns only the workspace identity and canonical
// repository root needed by the editor-open application function.
func (resolver *EditorWorkspaceResolver) ResolveEditorWorkspace(
	ctx context.Context,
	workspaceID domain.WorkspaceID,
) (review.EditorWorkspace, error) {
	scope, err := resolver.store.GetWorkspaceScope(ctx, workspaceID)
	if err != nil {
		return review.EditorWorkspace{}, err
	}
	return review.EditorWorkspace{
		WorkspaceID: scope.WorkspaceID, RepositoryRoot: scope.CanonicalPath,
	}, nil
}
