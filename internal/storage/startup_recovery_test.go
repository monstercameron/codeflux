//go:build integration

package storage

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestRecoverUnownedTaskRunsAtomicallyRequiresRecovery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repositories, task := createTaskFixture(t, 1500)
	runID := createToolTestRun(t, repositories, task.ID, 1504)
	setTaskRunStates(
		t,
		repositories,
		task.ID,
		runID,
		domain.TaskStateRunning,
		domain.RunStateRunning,
	)

	candidates, err := repositories.RecoverUnownedTaskRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 ||
		candidates[0].TaskID != task.ID ||
		candidates[0].RunID != runID ||
		candidates[0].PreviousTaskState != domain.TaskStateRunning ||
		candidates[0].PreviousRunState != domain.RunStateRunning ||
		candidates[0].Reason != TaskRunRecoveryMissingOwnership {
		t.Fatalf("unowned recovery candidates = %#v", candidates)
	}
	var taskState, runState, reason string
	var taskRevision, runRevision uint64
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT task.state, task.revision, task.invalidation_reason,
		        run.state, run.revision
		 FROM tasks task
		 JOIN runs run ON run.task_id = task.id
		 WHERE task.id = ? AND run.id = ?`,
		task.ID,
		runID,
	).Scan(
		&taskState,
		&taskRevision,
		&reason,
		&runState,
		&runRevision,
	); err != nil {
		t.Fatal(err)
	}
	if taskState != "recovery-required" ||
		runState != "recovery-required" ||
		taskRevision != 1 ||
		runRevision != 1 ||
		reason != TaskRunRecoveryMissingOwnership {
		t.Fatalf(
			"recovered task/run = %s@%d/%s@%d reason=%q",
			taskState,
			taskRevision,
			runState,
			runRevision,
			reason,
		)
	}
	repeated, err := repositories.RecoverUnownedTaskRuns(ctx)
	if err != nil || len(repeated) != 0 {
		t.Fatalf("repeated unowned recovery = %#v, %v", repeated, err)
	}
}

func TestRecoverUnownedTaskRunsMarksEveryUncertainRunOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repositories, task := createTaskFixture(t, 1520)
	firstRun := createToolTestRun(t, repositories, task.ID, 1524)
	secondRun, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO runs (
			id, task_id, state, attempt, task_revision, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, 'starting', 2, 0, ?, 2, 2)`,
		secondRun,
		task.ID,
		"second-unowned-run-"+secondRun.String(),
	); err != nil {
		t.Fatal(err)
	}
	setTaskRunStates(
		t,
		repositories,
		task.ID,
		firstRun,
		domain.TaskStateRunning,
		domain.RunStateRunning,
	)

	candidates, err := repositories.RecoverUnownedTaskRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 ||
		candidates[0].RunID != firstRun ||
		candidates[1].RunID != secondRun {
		t.Fatalf("multi-run recovery candidates = %#v", candidates)
	}
	var taskRevision uint64
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT revision FROM tasks WHERE id = ?`,
		task.ID,
	).Scan(&taskRevision); err != nil {
		t.Fatal(err)
	}
	if taskRevision != 1 {
		t.Fatalf("task recovery revision = %d, want one transition", taskRevision)
	}
	var recoveredRuns int
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT count(*) FROM runs
		 WHERE task_id = ? AND state = 'recovery-required' AND revision = 1`,
		task.ID,
	).Scan(&recoveredRuns); err != nil {
		t.Fatal(err)
	}
	if recoveredRuns != 2 {
		t.Fatalf("recovered runs = %d, want 2", recoveredRuns)
	}
}

func TestRecoverUnownedTaskRunsDoesNotTrustStaleQueueForRunningWork(t *testing.T) {
	t.Parallel()

	repositories, task := createTaskFixture(t, 1530)
	runID := createToolTestRun(t, repositories, task.ID, 1534)
	setTaskRunStates(
		t,
		repositories,
		task.ID,
		runID,
		domain.TaskStateRunning,
		domain.RunStateRunning,
	)
	if _, err := repositories.EnqueueTask(t.Context(), EnqueueTask{
		ID: "queue-stale-running", TaskID: task.ID,
		ProviderKey: "openai", Reason: "stale queue fixture",
		Priority: 10, EnqueueSequence: 1,
	}); err != nil {
		t.Fatal(err)
	}

	candidates, err := repositories.RecoverUnownedTaskRuns(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].RunID != runID {
		t.Fatalf("stale queue recovery candidates = %#v", candidates)
	}
}

func TestRecoverUnownedTaskRunsDoesNotTreatActiveWorktreeAsWorkerOwnership(
	t *testing.T,
) {
	t.Parallel()

	repositories, task := createTaskFixture(t, 1540)
	runID := createToolTestRun(t, repositories, task.ID, 1544)
	setTaskRunStates(
		t,
		repositories,
		task.ID,
		runID,
		domain.TaskStateRunning,
		domain.RunStateStarting,
	)
	if _, err := repositories.CreateWorktreeBinding(
		t.Context(),
		CreateWorktreeBinding{
			WorkspaceID:  testWorkspaceID(t, 1545),
			TaskID:       task.ID,
			RepositoryID: task.RepositoryID,
			BaseRevision: "1111111111111111111111111111111111111111",
			HeadRevision: "1111111111111111111111111111111111111111",
			BranchName:   "codeflux/task/unowned-active",
			WorktreePath: "/fixture/worktrees/unowned-active",
		},
	); err != nil {
		t.Fatal(err)
	}
	entry, err := repositories.EnqueueTask(t.Context(), EnqueueTask{
		ID: "queue-crash-before-lease", TaskID: task.ID,
		ProviderKey: "openai", Reason: "dispatching worker",
		Priority: 10, EnqueueSequence: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.TransitionQueuedTask(
		t.Context(), entry.ID, entry.Revision, TaskQueueStateDispatched,
	); err != nil {
		t.Fatal(err)
	}
	candidates, err := repositories.RecoverUnownedTaskRuns(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].RunID != runID {
		t.Fatalf("active-worktree recovery candidates = %#v", candidates)
	}
}

func TestRecoverUnownedTaskRunsExcludesSafeAndOwnedStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		taskState domain.TaskState
		runState  domain.RunState
		arrange   func(*testing.T, *Repositories, Task, domain.RunID)
	}{
		{
			name:      "draft task metadata",
			taskState: domain.TaskStateDraft,
			runState:  domain.RunStatePending,
		},
		{
			name:      "pre-execution ready task",
			taskState: domain.TaskStateReady,
			runState:  domain.RunStatePending,
		},
		{
			name:      "durably queued resume",
			taskState: domain.TaskStatePaused,
			runState:  domain.RunStatePaused,
			arrange: func(
				t *testing.T,
				repositories *Repositories,
				task Task,
				_ domain.RunID,
			) {
				if _, err := repositories.EnqueueTask(t.Context(), EnqueueTask{
					ID: "queue-safe-resume", TaskID: task.ID,
					ProviderKey: "openai", Reason: "resuming after pause",
					Priority: 10, Resuming: true, EnqueueSequence: 1,
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:      "active worker ownership",
			taskState: domain.TaskStateRunning,
			runState:  domain.RunStateRunning,
			arrange: func(
				t *testing.T,
				repositories *Repositories,
				task Task,
				runID domain.RunID,
			) {
				if _, err := repositories.AcquireWorkerLease(
					t.Context(),
					AcquireWorkerLease{
						ID: "lease-safe-owned", TaskID: task.ID, RunID: runID,
						ProtocolVersion: 1, ToolSchemaVersion: 1,
						WorktreePath:       "/fixture/owned",
						Endpoint:           "http://127.0.0.1:43117",
						SessionTokenSHA256: workerTokenHash,
					},
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:      "terminal run",
			taskState: domain.TaskStateFailed,
			runState:  domain.RunStateFailed,
		},
		{
			name:      "terminal task",
			taskState: domain.TaskStateCompleted,
			runState:  domain.RunStateRunning,
		},
	}
	for index, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repositories, task := createTaskFixture(t, 1540+index*20)
			runID := createToolTestRun(t, repositories, task.ID, 1544+index*20)
			setTaskRunStates(
				t,
				repositories,
				task.ID,
				runID,
				test.taskState,
				test.runState,
			)
			if test.arrange != nil {
				test.arrange(t, repositories, task, runID)
			}
			candidates, err := repositories.RecoverUnownedTaskRuns(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(candidates) != 0 {
				t.Fatalf("safe state produced recovery candidates: %#v", candidates)
			}
			var taskState, runState string
			if err := repositories.database.sql.QueryRowContext(
				t.Context(),
				`SELECT task.state, run.state
				 FROM tasks task JOIN runs run ON run.task_id = task.id
				 WHERE task.id = ? AND run.id = ?`,
				task.ID,
				runID,
			).Scan(&taskState, &runState); err != nil {
				t.Fatal(err)
			}
			if taskState != string(test.taskState) || runState != string(test.runState) {
				t.Fatalf(
					"safe task/run changed = %s/%s, want %s/%s",
					taskState,
					runState,
					test.taskState,
					test.runState,
				)
			}
		})
	}
}
