package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	crashHelperDatabase = "CODEFLUX_TEST_CRASH_DATABASE"
	crashHelperReady    = "CODEFLUX_TEST_CRASH_READY"
	crashHelperTask     = "CODEFLUX_TEST_CRASH_TASK"
	crashHelperEventOne = "CODEFLUX_TEST_CRASH_EVENT_ONE"
	crashHelperEventTwo = "CODEFLUX_TEST_CRASH_EVENT_TWO"
)

func TestReplayTaskEventsReconstructPersistedState(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 500)
	transitions := []struct {
		from domain.TaskState
		to   domain.TaskState
	}{
		{domain.TaskStateDraft, domain.TaskStateForecasting},
		{domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval},
		{domain.TaskStateAwaitingPlanApproval, domain.TaskStateReady},
	}
	for index, transition := range transitions {
		changed, err := repositories.TransitionTask(ctx, TransitionTask{
			EventID:          testEventID(t, 510+index),
			TaskID:           task.ID,
			ExpectedRevision: task.Revision,
			From:             transition.from,
			To:               transition.to,
			IdempotencyKey:   "replay-transition-" + string(rune('a'+index)),
		})
		if err != nil {
			t.Fatal(err)
		}
		task = changed.Task
		if index == 0 {
			if _, err := repositories.AppendTaskEvent(ctx, AppendTaskEvent{
				ID:             testEventID(t, 520),
				TaskID:         task.ID,
				EventType:      "task.progress",
				PayloadJSON:    `{"message":"bounded progress"}`,
				IdempotencyKey: "replay-progress",
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	replayed, err := repositories.ReplayTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State != task.State ||
		replayed.Revision != task.Revision ||
		replayed.EventCount != 4 {
		t.Fatalf("replay = %#v, persisted task = %#v", replayed, task)
	}
}

func TestReplayTaskRejectsMissingEventSequence(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 530)
	for index, transition := range []struct {
		from domain.TaskState
		to   domain.TaskState
	}{
		{domain.TaskStateDraft, domain.TaskStateForecasting},
		{domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval},
	} {
		changed, err := repositories.TransitionTask(ctx, TransitionTask{
			EventID:          testEventID(t, 540+index),
			TaskID:           task.ID,
			ExpectedRevision: task.Revision,
			From:             transition.from,
			To:               transition.to,
			IdempotencyKey:   "gap-transition-" + string(rune('a'+index)),
		})
		if err != nil {
			t.Fatal(err)
		}
		task = changed.Task
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`DELETE FROM task_events WHERE task_id = ? AND sequence = 1`,
		task.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.ReplayTask(ctx, task.ID); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("replay error = %v, want corruption", err)
	}
}

func TestForcedTerminationDuringRepresentativeWrites(t *testing.T) {
	if os.Getenv(crashHelperDatabase) != "" {
		runCrashWriteHelper(t)
		return
	}

	ctx := context.Background()
	root := t.TempDir()
	databasePath := filepath.Join(root, "codeflux.sqlite3")
	database, err := Open(ctx, OpenOptions{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Migrate(ctx, MigrationOptions{
		ApplicationVersion: "crash-gate-test",
		BackupDirectory:    filepath.Join(root, "backups"),
		AvailableBytes: func(string) (uint64, error) {
			return ^uint64(0), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := NewRepositories(database, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	task := createCrashTaskFixture(t, repositories)
	if err := database.Close(ctx); err != nil {
		t.Fatal(err)
	}

	eventOne, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	eventTwo, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(root, "transaction-ready")
	command := exec.Command(
		os.Args[0],
		"-test.run=^TestForcedTerminationDuringRepresentativeWrites$",
	)
	command.Env = append(os.Environ(),
		crashHelperDatabase+"="+databasePath,
		crashHelperReady+"="+readyPath,
		crashHelperTask+"="+task.ID.String(),
		crashHelperEventOne+"="+eventOne.String(),
		crashHelperEventTwo+"="+eventTwo.String(),
	)
	var childOutput bytes.Buffer
	command.Stdout = &childOutput
	command.Stderr = &childOutput
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatalf("crash helper did not enter its open transaction: %s", childOutput.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("crash helper exited successfully, want forced termination")
	}

	reopened, err := Open(ctx, OpenOptions{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(ctx)
	if err := reopened.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if err := reopened.ValidateNoOrphans(ctx); err != nil {
		t.Fatal(err)
	}
	reopenedRepositories, err := NewRepositories(reopened, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopenedRepositories.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != domain.TaskStateForecasting || persisted.Revision != 1 {
		t.Fatalf("task after crash = %#v", persisted)
	}
	replayed, err := reopenedRepositories.ReplayTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State != persisted.State ||
		replayed.Revision != persisted.Revision ||
		replayed.EventCount != 1 {
		t.Fatalf("replay after crash = %#v, task = %#v", replayed, persisted)
	}
}

func runCrashWriteHelper(t *testing.T) {
	ctx := context.Background()
	taskID, err := domain.ParseTaskID(os.Getenv(crashHelperTask))
	if err != nil {
		t.Fatal(err)
	}
	eventOne, err := domain.ParseEventID(os.Getenv(crashHelperEventOne))
	if err != nil {
		t.Fatal(err)
	}
	eventTwo, err := domain.ParseEventID(os.Getenv(crashHelperEventTwo))
	if err != nil {
		t.Fatal(err)
	}
	database, err := Open(ctx, OpenOptions{Path: os.Getenv(crashHelperDatabase)})
	if err != nil {
		t.Fatal(err)
	}
	repositories, err := NewRepositories(database, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.TransitionTask(ctx, TransitionTask{
		EventID:          eventOne,
		TaskID:           taskID,
		ExpectedRevision: 0,
		From:             domain.TaskStateDraft,
		To:               domain.TaskStateForecasting,
		IdempotencyKey:   "crash-committed-transition",
	}); err != nil {
		t.Fatal(err)
	}
	err = database.RunInTransaction(ctx, func(transaction *Transaction) error {
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE tasks
			 SET state = 'awaiting-plan-approval', revision = revision + 1
			 WHERE id = ? AND state = 'forecasting' AND revision = 1`,
			taskID,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return errors.New("crash helper did not update exactly one task")
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO task_events (
				id, task_id, sequence, event_type, payload_json,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, 2, 'task.state-transition', ?, ?, ?)`,
			eventTwo,
			taskID,
			`{"from":"forecasting","to":"awaiting-plan-approval"}`,
			"crash-uncommitted-transition",
			time.Now().UTC().UnixMicro(),
		); err != nil {
			return err
		}
		if err := os.WriteFile(os.Getenv(crashHelperReady), []byte("ready"), 0o600); err != nil {
			return err
		}
		time.Sleep(10 * time.Minute)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash helper was not terminated")
}

func createCrashTaskFixture(t *testing.T, repositories *Repositories) Task {
	t.Helper()
	projectID, err := domain.NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateProject(context.Background(), CreateProject{
		ID:   projectID,
		Name: "Crash gate fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(context.Background(), CreateRepository{
		ID:            repositoryID,
		ProjectID:     projectID,
		CanonicalPath: "/crash-gate-fixture",
		GitIdentity:   "crash-gate-fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(context.Background(), CreateThread{
		ID:           threadID,
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		Title:        "Crash gate",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := repositories.CreateTask(context.Background(), CreateTask{
		ID:                taskID,
		ThreadID:          threadID,
		RepositoryID:      repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey:    "crash-gate-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}
