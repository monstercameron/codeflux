package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestWorktreeBindingRepositoryLifecycle(t *testing.T) {
	t.Parallel()

	repositories, task := createTaskFixture(t, 400)
	input := CreateWorktreeBinding{
		WorkspaceID:  testWorkspaceID(t, 405),
		TaskID:       task.ID,
		RepositoryID: task.RepositoryID,
		BaseRevision: "1111111111111111111111111111111111111111",
		HeadRevision: "1111111111111111111111111111111111111111",
		BranchName:   "codeflux/task/fixture",
		WorktreePath: "/fixture/worktrees/task",
	}
	created, err := repositories.CreateWorktreeBinding(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repositories.GetWorktreeBinding(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != created {
		t.Fatalf("loaded binding = %#v, want %#v", loaded, created)
	}

	advanced, err := repositories.AdvanceWorktreeBinding(t.Context(), AdvanceWorktreeBinding{
		TaskID:           task.ID,
		ExpectedRevision: created.Revision,
		ExpectedHead:     created.HeadRevision,
		HeadRevision:     "2222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Revision != 1 ||
		advanced.HeadRevision != "2222222222222222222222222222222222222222" {
		t.Fatalf("advanced binding = %#v", advanced)
	}
	if _, err := repositories.AdvanceWorktreeBinding(t.Context(), AdvanceWorktreeBinding{
		TaskID:           task.ID,
		ExpectedRevision: created.Revision,
		ExpectedHead:     created.HeadRevision,
		HeadRevision:     "3333333333333333333333333333333333333333",
	}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale advance error = %v", err)
	}

	released, err := repositories.TransitionWorktreeBinding(
		t.Context(),
		TransitionWorktreeBinding{
			TaskID:           task.ID,
			ExpectedRevision: advanced.Revision,
			From:             WorktreeBindingActive,
			To:               WorktreeBindingReleased,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if released.State != WorktreeBindingReleased || released.Revision != 2 {
		t.Fatalf("released binding = %#v", released)
	}
	var workspaceState string
	if err := repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT state FROM workspaces WHERE id = ?`,
		input.WorkspaceID,
	).Scan(&workspaceState); err != nil {
		t.Fatal(err)
	}
	if workspaceState != "closed" {
		t.Fatalf("workspace state = %q, want closed", workspaceState)
	}
}

func TestWorktreeBindingRepositoryRollsBackAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	repositories, task := createTaskFixture(t, 420)
	input := CreateWorktreeBinding{
		WorkspaceID:  testWorkspaceID(t, 425),
		TaskID:       task.ID,
		RepositoryID: task.RepositoryID,
		BaseRevision: "1111111111111111111111111111111111111111",
		HeadRevision: "1111111111111111111111111111111111111111",
		BranchName:   "codeflux/task/fixture",
		WorktreePath: "/fixture/worktrees/task",
	}
	if _, err := repositories.CreateWorktreeBinding(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	input.WorkspaceID = testWorkspaceID(t, 426)
	if _, err := repositories.CreateWorktreeBinding(t.Context(), input); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate task binding error = %v", err)
	}
	var workspaceCount int
	if err := repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT count(*) FROM workspaces WHERE id = ?`,
		input.WorkspaceID,
	).Scan(&workspaceCount); err != nil {
		t.Fatal(err)
	}
	if workspaceCount != 0 {
		t.Fatalf("failed binding left %d workspace rows", workspaceCount)
	}
}

func testWorkspaceID(t *testing.T, number int) domain.WorkspaceID {
	t.Helper()
	id, err := domain.ParseWorkspaceID("wsp_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
