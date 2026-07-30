package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestTaskQueueIsVisibleStableAndRevisionChecked(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 1440)
	entry, err := repositories.EnqueueTask(ctx, EnqueueTask{
		ID: "queue-1440", TaskID: task.ID, ProviderKey: "openai",
		Reason: "worker capacity is full", Priority: 10, EnqueueSequence: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.EnqueueTask(ctx, EnqueueTask{
		ID: "queue-1441", TaskID: task.ID, ProviderKey: "openai",
		Reason: "duplicate", Priority: 20, EnqueueSequence: 1,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate live queue entry = %v", err)
	}
	listed, err := repositories.ListQueuedTasks(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != entry.ID {
		t.Fatalf("queued tasks = %#v", listed)
	}
	dispatched, err := repositories.TransitionQueuedTask(
		ctx, entry.ID, entry.Revision, TaskQueueStateDispatched,
	)
	if err != nil {
		t.Fatal(err)
	}
	if dispatched.State != TaskQueueStateDispatched ||
		dispatched.DispatchedAt == nil {
		t.Fatalf("dispatched entry = %#v", dispatched)
	}
	if _, err := repositories.TransitionQueuedTask(
		ctx, entry.ID, entry.Revision, TaskQueueStateCancelled,
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale queue transition = %v", err)
	}
}

func TestRecoverDispatchedWorkerStartAtomicallyRequiresRecovery(t *testing.T) {
	repositories, task := createTaskFixture(t, 1460)
	runID := createToolTestRun(t, repositories, task.ID, 1464)
	setTaskRunStates(
		t, repositories, task.ID, runID,
		domain.TaskStateRunning, domain.RunStateStarting,
	)
	entry, err := repositories.EnqueueTask(t.Context(), EnqueueTask{
		ID: "queue-start-failure", TaskID: task.ID, ProviderKey: "openai",
		Reason: "ready to dispatch", Priority: 10, EnqueueSequence: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.TransitionQueuedTask(
		t.Context(), entry.ID, entry.Revision, TaskQueueStateDispatched,
	); err != nil {
		t.Fatal(err)
	}
	reason := "worker executable could not start; recovery choice required"
	if err := repositories.RecoverDispatchedWorkerStart(
		t.Context(),
		RecoverDispatchedWorkerStart{
			QueueID: entry.ID, TaskID: task.ID, RunID: runID, Reason: reason,
		},
	); err != nil {
		t.Fatal(err)
	}
	var queueState, queueReason, taskState, taskReason, runState string
	if err := repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT queued.state, queued.reason, task.state,
		        task.invalidation_reason, run.state
		 FROM task_queue_entries queued
		 JOIN tasks task ON task.id = queued.task_id
		 JOIN runs run ON run.task_id = task.id
		 WHERE queued.id = ? AND run.id = ?`,
		entry.ID, runID,
	).Scan(
		&queueState, &queueReason, &taskState, &taskReason, &runState,
	); err != nil {
		t.Fatal(err)
	}
	if queueState != "cancelled" || queueReason != reason ||
		taskState != "recovery-required" || taskReason != reason ||
		runState != "recovery-required" {
		t.Fatalf(
			"failed-start recovery = %s/%q %s/%q %s",
			queueState, queueReason, taskState, taskReason, runState,
		)
	}
}

func TestTaskQueueOrderingReasonAndCancellationSurviveRestart(t *testing.T) {
	ctx := context.Background()
	repositories, firstTask := createTaskFixture(t, 1450)
	secondTask := createSiblingTask(t, repositories, firstTask, 1454)
	thirdTask := createSiblingTask(t, repositories, firstTask, 1455)
	inputs := []EnqueueTask{
		{
			ID: "queue-fresh-high", TaskID: firstTask.ID,
			ProviderKey: "openai", Reason: "worker capacity is full",
			Priority: 100, EnqueueSequence: 1,
		},
		{
			ID: "queue-resuming", TaskID: secondTask.ID,
			ProviderKey: "openai", Reason: "resuming after approval",
			Priority: 1, Resuming: true, EnqueueSequence: 2,
		},
		{
			ID: "queue-fresh-low", TaskID: thirdTask.ID,
			ProviderKey: "openai", Reason: "worker capacity is full",
			Priority: 1, EnqueueSequence: 3,
		},
	}
	for _, input := range inputs {
		if _, err := repositories.EnqueueTask(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	cancelled, err := repositories.TransitionQueuedTask(
		ctx, "queue-fresh-low", 0, TaskQueueStateCancelled,
	)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != TaskQueueStateCancelled ||
		cancelled.DispatchedAt != nil {
		t.Fatalf("cancelled queue entry = %#v", cancelled)
	}

	path := repositories.database.Path()
	if err := repositories.database.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reopened.Close(context.Background())
	})
	restarted, err := NewRepositories(reopened, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := restarted.ListQueuedTasks(ctx, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 ||
		listed[0].ID != "queue-resuming" ||
		listed[0].Reason != "resuming after approval" ||
		listed[1].ID != "queue-fresh-high" {
		t.Fatalf("restored queue order = %#v", listed)
	}
}

func TestTaskQueueConcurrentOwnershipAndInputBounds(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 1460)
	const attempts = 12
	var (
		waitGroup sync.WaitGroup
		results   = make(chan error, attempts)
	)
	for index := 0; index < attempts; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			_, err := repositories.EnqueueTask(ctx, EnqueueTask{
				ID:     fmt.Sprintf("queue-race-%02d", index),
				TaskID: task.ID, ProviderKey: "openai",
				Reason:   "concurrent scheduling",
				Priority: 1, EnqueueSequence: uint64(index + 1),
			})
			results <- err
		}(index)
	}
	waitGroup.Wait()
	close(results)
	var successes int
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("concurrent enqueue error = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent enqueues = %d, want 1", successes)
	}
	for _, limit := range []int{0, 1002} {
		if _, err := repositories.ListQueuedTasks(ctx, limit); err == nil {
			t.Fatalf("queue list accepted limit %d", limit)
		}
	}
}

func createSiblingTask(
	t *testing.T,
	repositories *Repositories,
	sibling Task,
	number int,
) Task {
	t.Helper()
	task, err := repositories.CreateTask(context.Background(), CreateTask{
		ID:                testTaskID(t, number),
		ThreadID:          sibling.ThreadID,
		RepositoryID:      sibling.RepositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey:    fmt.Sprintf("task-sibling-%d", number),
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}
