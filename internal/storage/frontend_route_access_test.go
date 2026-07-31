package storage

import (
	"testing"
	"time"
)

func TestReadFrontendRouteAccessReturnsIdentifiersWithoutPaths(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 700)
	repositoryID := testRepositoryID(t, 701)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	workspaceID := testWorkspaceID(t, 704)
	now := time.Now().UTC().UnixMicro()
	if _, err := repositories.database.sql.ExecContext(
		t.Context(),
		`INSERT INTO workspaces (
			id, repository_id, canonical_path, state,
			created_at_unix_micros, updated_at_unix_micros, revision
		) VALUES (?, ?, ?, 'active', ?, ?, 0)`,
		workspaceID, repositoryID, `C:\authorized\workspace`, now, now,
	); err != nil {
		t.Fatal(err)
	}
	activeThread := testThreadID(t, 702)
	archivedThread := testThreadID(t, 703)
	for _, threadID := range []int{702, 703} {
		parsed := testThreadID(t, threadID)
		if _, err := repositories.CreateThread(t.Context(), CreateThread{
			ID: parsed, ProjectID: projectID, RepositoryID: repositoryID, Title: "Route fixture",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repositories.database.sql.ExecContext(
		t.Context(),
		"UPDATE threads SET archived_at_unix_micros = ? WHERE id = ?",
		time.Now().UTC().UnixMicro(),
		archivedThread,
	); err != nil {
		t.Fatal(err)
	}

	access, err := repositories.ReadFrontendRouteAccess(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !access.FirstRunComplete || len(access.AccessibleRepositories) != 1 || access.AccessibleRepositories[0] != repositoryID {
		t.Fatalf("repository access = %#v", access)
	}
	if access.SelectedWorkspaceID != workspaceID {
		t.Fatalf("selected workspace = %s, want %s", access.SelectedWorkspaceID, workspaceID)
	}
	if len(access.AccessibleThreads) != 1 || access.AccessibleThreads[0] != activeThread {
		t.Fatalf("active thread access = %#v", access)
	}
	if len(access.ArchivedThreads) != 1 || access.ArchivedThreads[0] != archivedThread {
		t.Fatalf("archived thread access = %#v", access)
	}
}

func TestReadFrontendRouteAccessDeniesThreadsOfDeletedRepository(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 710)
	repositoryID := testRepositoryID(t, 711)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	threadID := testThreadID(t, 712)
	if _, err := repositories.CreateThread(t.Context(), CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Deleted repository",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		t.Context(),
		"UPDATE repositories SET deleted_at_unix_micros = ? WHERE id = ?",
		time.Now().UTC().UnixMicro(),
		repositoryID,
	); err != nil {
		t.Fatal(err)
	}
	access, err := repositories.ReadFrontendRouteAccess(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if access.FirstRunComplete || len(access.AccessibleRepositories) != 0 || len(access.AccessibleThreads) != 0 || len(access.ArchivedThreads) != 0 {
		t.Fatalf("deleted repository leaked route access: %#v", access)
	}
}
