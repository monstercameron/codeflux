package storage

import (
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestTaskControlPausePersistsReasonAndResumesNonOverlappingEdits(
	t *testing.T,
) {
	fixture := createRunningTaskControlFixture(t, 7100)
	current, err := fixture.repositories.ReadTaskControl(
		t.Context(),
		fixture.task.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := fixture.repositories.RequestTaskPause(
		t.Context(),
		RequestTaskPause{
			EventID:              testEventID(t, 7141),
			TaskID:               fixture.task.ID,
			RunID:                fixture.runID,
			ExpectedTaskRevision: current.TaskRevision,
			ExpectedRunRevision:  current.RunRevision,
			Reason:               domain.PauseReasonUserRequested,
			ReasonRedacted:       "user requested a safe pause",
			IdempotencyKey:       "task-control-pause/requested",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if requested.Disposition != TaskControlPauseRequested ||
		requested.RunState != domain.RunStatePausing ||
		requested.TaskState != domain.TaskStateRunning {
		t.Fatalf("pause requested = %#v", requested)
	}
	checkpointID, err := domain.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	paused, err := fixture.repositories.CompleteTaskPause(
		t.Context(),
		CompleteTaskPause{
			EventID:              testEventID(t, 7142),
			TaskID:               fixture.task.ID,
			RunID:                fixture.runID,
			ExpectedTaskRevision: requested.TaskRevision,
			ExpectedRunRevision:  requested.RunRevision,
			Reason:               domain.PauseReasonUserRequested,
			ReasonRedacted:       "user requested a safe pause",
			CheckpointID:         checkpointID,
			IdempotencyKey:       "task-control-pause/paused",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Disposition != TaskControlPaused ||
		paused.TaskState != domain.TaskStatePaused ||
		paused.RunState != domain.RunStatePaused ||
		paused.PauseReason != domain.PauseReasonUserRequested {
		t.Fatalf("paused = %#v", paused)
	}
	pauseReplay, err := fixture.repositories.ReadTaskControlReplay(
		t.Context(),
		TaskControlReplayRequest{
			TaskID: fixture.task.ID, Operation: TaskControlReplayPause,
			ExpectedTaskRevision: current.TaskRevision,
			ReasonRedacted:       "user requested a safe pause",
			IdempotencyKey:       "task-control-pause",
		},
	)
	if err != nil || !pauseReplay.Found ||
		pauseReplay.Control.TaskRevision != paused.TaskRevision {
		t.Fatalf("pause replay = %#v, %v", pauseReplay, err)
	}
	resumed, err := fixture.repositories.ResumeControlledTask(
		t.Context(),
		ResumeControlledTask{
			EventID:              testEventID(t, 7143),
			TaskID:               fixture.task.ID,
			RunID:                fixture.runID,
			ExpectedTaskRevision: paused.TaskRevision,
			ExpectedRunRevision:  paused.RunRevision,
			NonOverlappingFiles:  []string{"docs/notes.md"},
			IdempotencyKey:       "task-control-resume/resumed",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Disposition != TaskControlActive ||
		resumed.TaskState != domain.TaskStateRunning ||
		resumed.RunState != domain.RunStateStarting ||
		resumed.PauseReason != "" {
		t.Fatalf("resumed = %#v", resumed)
	}
	resumeReplay, err := fixture.repositories.ReadTaskControlReplay(
		t.Context(),
		TaskControlReplayRequest{
			TaskID: fixture.task.ID, Operation: TaskControlReplayResume,
			ExpectedTaskRevision: paused.TaskRevision,
			IdempotencyKey:       "task-control-resume",
		},
	)
	if err != nil || !resumeReplay.Found ||
		resumeReplay.Control.TaskRevision != resumed.TaskRevision {
		t.Fatalf("resume replay = %#v, %v", resumeReplay, err)
	}
	var payload string
	if err := fixture.repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT payload_json FROM task_events
		  WHERE task_id = ? AND event_type = 'task.resumed'`,
		fixture.task.ID,
	).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, "docs/notes.md") {
		t.Fatalf("resume event payload = %s", payload)
	}
}

func TestTaskControlCancelFromPauseRequestedNeverBecomesFailure(
	t *testing.T,
) {
	fixture := createRunningTaskControlFixture(t, 7200)
	current, err := fixture.repositories.ReadTaskControl(
		t.Context(),
		fixture.task.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := fixture.repositories.RequestTaskPause(
		t.Context(),
		RequestTaskPause{
			EventID:              testEventID(t, 7241),
			TaskID:               fixture.task.ID,
			RunID:                fixture.runID,
			ExpectedTaskRevision: current.TaskRevision,
			ExpectedRunRevision:  current.RunRevision,
			Reason:               domain.PauseReasonUserRequested,
			ReasonRedacted:       "pause before cancellation",
			IdempotencyKey:       "cancel-pausing/requested",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := fixture.repositories.CancelControlledTask(
		t.Context(),
		CancelControlledTask{
			EventID:              testEventID(t, 7242),
			TaskID:               fixture.task.ID,
			RunID:                fixture.runID,
			ExpectedTaskRevision: requested.TaskRevision,
			ExpectedRunRevision:  requested.RunRevision,
			Reason:               domain.CancellationReasonUserRequested,
			ReasonRedacted:       "user stopped the task",
			IdempotencyKey:       "cancel-pausing/cancelled",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Disposition != TaskControlCancelled ||
		cancelled.TaskState != domain.TaskStateCancelled ||
		cancelled.RunState != domain.RunStateCancelled ||
		cancelled.CancellationReason !=
			domain.CancellationReasonUserRequested {
		t.Fatalf("cancelled = %#v", cancelled)
	}
	var failedEvents int
	if err := fixture.repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT COUNT(*) FROM task_events
		  WHERE task_id = ? AND event_type LIKE '%.failed'`,
		fixture.task.ID,
	).Scan(&failedEvents); err != nil {
		t.Fatal(err)
	}
	if failedEvents != 0 {
		t.Fatalf("failure events after cancellation = %d", failedEvents)
	}
	replay, err := fixture.repositories.ReadTaskControlReplay(
		t.Context(),
		TaskControlReplayRequest{
			TaskID: fixture.task.ID, Operation: TaskControlReplayCancel,
			ExpectedTaskRevision: requested.TaskRevision,
			ReasonRedacted:       "user stopped the task",
			IdempotencyKey:       "cancel-pausing/cancelled",
		},
	)
	if err != nil || !replay.Found ||
		replay.Control.TaskRevision != cancelled.TaskRevision {
		t.Fatalf("cancel replay = %#v, %v", replay, err)
	}
	_, err = fixture.repositories.ReadTaskControlReplay(
		t.Context(),
		TaskControlReplayRequest{
			TaskID: fixture.task.ID, Operation: TaskControlReplayCancel,
			ExpectedTaskRevision: requested.TaskRevision,
			ReasonRedacted:       "different request",
			IdempotencyKey:       "cancel-pausing/cancelled",
		},
	)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("reused cancel replay key error = %v", err)
	}
}

func TestTaskControlRejectsStaleRevisionAndReplaysIdempotently(
	t *testing.T,
) {
	fixture := createRunningTaskControlFixture(t, 7300)
	current, err := fixture.repositories.ReadTaskControl(
		t.Context(),
		fixture.task.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	input := RequestTaskPause{
		EventID:              testEventID(t, 7341),
		TaskID:               fixture.task.ID,
		RunID:                fixture.runID,
		ExpectedTaskRevision: current.TaskRevision,
		ExpectedRunRevision:  current.RunRevision,
		Reason:               domain.PauseReasonUserRequested,
		ReasonRedacted:       "idempotent pause",
		IdempotencyKey:       "idempotent-pause/requested",
	}
	first, err := fixture.repositories.RequestTaskPause(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	replay := input
	replay.EventID = testEventID(t, 7342)
	second, err := fixture.repositories.RequestTaskPause(
		t.Context(),
		replay,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.RunRevision != second.RunRevision ||
		first.Disposition != second.Disposition {
		t.Fatalf("idempotent snapshots differ: %#v %#v", first, second)
	}
	stale := input
	stale.EventID = testEventID(t, 7343)
	stale.IdempotencyKey = "stale-pause/requested"
	_, err = fixture.repositories.RequestTaskPause(t.Context(), stale)
	if !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale pause error = %v", err)
	}
}

func createRunningTaskControlFixture(
	t *testing.T,
	base int,
) boundAgentRunFixture {
	t.Helper()
	fixture := createBoundAgentRunFixture(t, base)
	_, micros := fixture.repositories.timestamp()
	if _, err := fixture.repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE runs
		    SET state = 'running', updated_at_unix_micros = ?,
		        revision = revision + 1
		  WHERE id = ? AND state = 'starting'`,
		micros,
		fixture.runID,
	); err != nil {
		t.Fatal(err)
	}
	return fixture
}
