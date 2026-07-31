package coordinator

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

type editorWorkspaceStoreStub struct {
	scope storage.WorkspaceScope
	err   error
}

func (stub editorWorkspaceStoreStub) GetWorkspaceScope(
	context.Context,
	domain.WorkspaceID,
) (storage.WorkspaceScope, error) {
	return stub.scope, stub.err
}

func TestEditorWorkspaceResolverPreservesAuthoritativeBinding(t *testing.T) {
	workspaceID, _ := domain.NewWorkspaceID()
	repositoryID, _ := domain.NewRepositoryID()
	projectID, _ := domain.NewProjectID()
	resolver, err := NewEditorWorkspaceResolver(editorWorkspaceStoreStub{scope: storage.WorkspaceScope{
		WorkspaceID: workspaceID, RepositoryID: repositoryID, ProjectID: projectID,
		CanonicalPath: `C:\repo`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := resolver.ResolveEditorWorkspace(context.Background(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.WorkspaceID != workspaceID || workspace.RepositoryRoot != `C:\repo` {
		t.Fatalf("editor workspace = %+v", workspace)
	}
}

func TestEditorWorkspaceResolverPropagatesStoreFailure(t *testing.T) {
	storeErr := errors.New("workspace unavailable")
	resolver, err := NewEditorWorkspaceResolver(editorWorkspaceStoreStub{err: storeErr})
	if err != nil {
		t.Fatal(err)
	}
	workspaceID, _ := domain.NewWorkspaceID()
	_, err = resolver.ResolveEditorWorkspace(context.Background(), workspaceID)
	if !errors.Is(err, storeErr) {
		t.Fatalf("error = %v", err)
	}
}
