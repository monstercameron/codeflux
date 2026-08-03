package main

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graphlayout"
	"codeflux.dev/codeflux/web/frontend/threadrail"
)

func TestSelectedGraphResourceScopeRequiresAuthoritativeProjectAndTask(t *testing.T) {
	projectID, _ := domain.NewProjectID()
	taskID, _ := domain.NewTaskID()
	threadID, _ := domain.NewThreadID()
	repositoryID, _ := domain.NewRepositoryID()
	workspaceID, _ := domain.NewWorkspaceID()
	thread, err := threadrail.NewThread(threadrail.ThreadInput{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, WorkspaceID: workspaceID,
		TaskID: taskID, Title: "Graph scope", TaskState: threadrail.TaskStateRunning,
		Attention: threadrail.AttentionNone, Revision: 1, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := selectedGraphResourceScope(thread)
	if err != nil || scope.ProjectID != projectID || scope.TaskID != taskID {
		t.Fatalf("scope = %#v, %v", scope, err)
	}
}

func TestMountedGraphHasPlacementFindsOnlyLoadedNodes(t *testing.T) {
	loaded, _ := domain.NewNodeID()
	notLoaded, _ := domain.NewNodeID()
	layout := graphlayout.Layout{Nodes: []graphlayout.Placement{{NodeID: loaded}}}
	if !mountedGraphHasPlacement(layout, loaded) {
		t.Fatal("expected a loaded node ID to be found")
	}
	if mountedGraphHasPlacement(layout, notLoaded) {
		t.Fatal("expected a node ID outside the loaded layout to be reported absent")
	}
	if mountedGraphHasPlacement(layout, domain.NodeID{}) {
		t.Fatal("expected a zero node ID to be reported absent")
	}
}

func TestGraphExplanationDraftPreservesExistingUserText(t *testing.T) {
	if got := graphExplanationDraft("keep this", "node explanation"); got != "keep this\n\nnode explanation" {
		t.Fatalf("draft = %q", got)
	}
	if got := graphExplanationDraft("keep this", "   "); got != "keep this" {
		t.Fatalf("empty explanation changed draft: %q", got)
	}
}
