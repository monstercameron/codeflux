//go:build integration

package storage

import (
	"errors"
	"testing"
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

func TestWorktreeBindingRepositoryMarksRecoveryAtomically(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repositories, task := createTaskFixture(t, 440)
	input := CreateWorktreeBinding{
		WorkspaceID:  testWorkspaceID(t, 445),
		TaskID:       task.ID,
		RepositoryID: task.RepositoryID,
		BaseRevision: "1111111111111111111111111111111111111111",
		HeadRevision: "1111111111111111111111111111111111111111",
		BranchName:   "codeflux/task/recovery",
		WorktreePath: "/fixture/worktrees/recovery",
	}
	binding, err := repositories.CreateWorktreeBinding(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	runID := createToolTestRun(t, repositories, task.ID, 446)

	active, err := repositories.ListActiveWorktreeBindings(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0] != binding {
		t.Fatalf("active bindings = %#v, want %#v", active, binding)
	}

	const reason = "task worktree HEAD diverged from durable metadata"
	recovered, err := repositories.MarkWorktreeRecoveryRequired(
		ctx,
		task.ID,
		binding.Revision,
		reason,
	)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != WorktreeBindingRecoveryRequired ||
		recovered.Revision != binding.Revision+1 {
		t.Fatalf("recovered binding = %#v", recovered)
	}

	var workspaceState, taskState, runState, invalidationReason string
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT workspace.state, task.state, run.state, task.invalidation_reason
		 FROM workspaces workspace
		 JOIN worktree_bindings binding ON binding.workspace_id = workspace.id
		 JOIN tasks task ON task.id = binding.task_id
		 JOIN runs run ON run.task_id = task.id
		 WHERE task.id = ? AND run.id = ?`,
		task.ID,
		runID,
	).Scan(&workspaceState, &taskState, &runState, &invalidationReason); err != nil {
		t.Fatal(err)
	}
	if workspaceState != "recovery-required" ||
		taskState != "recovery-required" ||
		runState != "recovery-required" ||
		invalidationReason != reason {
		t.Fatalf(
			"recovery state = workspace %q, task %q, run %q, reason %q",
			workspaceState,
			taskState,
			runState,
			invalidationReason,
		)
	}
	active, err = repositories.ListActiveWorktreeBindings(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active bindings after recovery = %#v", active)
	}
	if _, err := repositories.MarkWorktreeRecoveryRequired(
		ctx,
		task.ID,
		binding.Revision,
		reason,
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale recovery transition error = %v", err)
	}
}

func TestWorktreeBindingRecoveryRollsBackForTerminalTask(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repositories, task := createTaskFixture(t, 460)
	input := CreateWorktreeBinding{
		WorkspaceID:  testWorkspaceID(t, 465),
		TaskID:       task.ID,
		RepositoryID: task.RepositoryID,
		BaseRevision: "1111111111111111111111111111111111111111",
		HeadRevision: "1111111111111111111111111111111111111111",
		BranchName:   "codeflux/task/terminal",
		WorktreePath: "/fixture/worktrees/terminal",
	}
	binding, err := repositories.CreateWorktreeBinding(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE tasks SET state = 'completed' WHERE id = ?`,
		task.ID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := repositories.MarkWorktreeRecoveryRequired(
		ctx,
		task.ID,
		binding.Revision,
		"task worktree is missing or inaccessible",
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("terminal task recovery error = %v", err)
	}
	loaded, err := repositories.GetWorktreeBinding(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != WorktreeBindingActive || loaded.Revision != binding.Revision {
		t.Fatalf("rolled-back binding = %#v, want active revision %d", loaded, binding.Revision)
	}
	var workspaceState string
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT state FROM workspaces WHERE id = ?`,
		input.WorkspaceID,
	).Scan(&workspaceState); err != nil {
		t.Fatal(err)
	}
	if workspaceState != "active" {
		t.Fatalf("rolled-back workspace state = %q", workspaceState)
	}
}

func TestListActiveWorktreeBindingsValidatesBound(t *testing.T) {
	t.Parallel()

	repositories := openTestRepositories(t)
	for _, limit := range []int{0, 1001} {
		if _, err := repositories.ListActiveWorktreeBindings(t.Context(), limit); err == nil {
			t.Fatalf("worktree page limit %d was accepted", limit)
		}
	}
}
