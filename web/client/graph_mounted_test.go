package main

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
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

func TestGraphExplanationDraftPreservesExistingUserText(t *testing.T) {
	if got := graphExplanationDraft("keep this", "node explanation"); got != "keep this\n\nnode explanation" {
		t.Fatalf("draft = %q", got)
	}
	if got := graphExplanationDraft("keep this", "   "); got != "keep this" {
		t.Fatalf("empty explanation changed draft: %q", got)
	}
}
