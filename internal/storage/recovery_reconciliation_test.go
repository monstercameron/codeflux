package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestRecoveryReconciliationIsRevisionBoundAndCommitsAtomically(t *testing.T) {
	t.Parallel()
	repositories, task := createTaskFixture(t, 2100)
	runID := createToolTestRun(t, repositories, task.ID, 2104)
	setTaskRunStates(t, repositories, task.ID, runID, domain.TaskStateRecoveryRequired, domain.RunStateRecoveryRequired)
	if _, err := repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE tasks SET revision = revision + 1 WHERE id = ?`,
		task.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE runs SET revision = revision + 1 WHERE id = ?`,
		runID,
	); err != nil {
		t.Fatal(err)
	}
	runPointer := runID
	oldCheckpoint, err := repositories.CreateCheckpoint(t.Context(), CreateCheckpoint{
		ID: testCheckpointID(t, 2105), TaskID: task.ID, RunID: &runPointer,
		State: domain.CheckpointStateReady, RepositoryRevision: recoveryRepositoryRevision,
		WorktreeDiffHash: recoveryObservationSHA, EventSequence: 1,
		IdempotencyKey: "reconcile-old-checkpoint",
	})
	if err != nil {
		t.Fatal(err)
	}
	newCheckpoint, err := repositories.CreateCheckpoint(t.Context(), CreateCheckpoint{
		ID: testCheckpointID(t, 2106), TaskID: task.ID, RunID: &runPointer,
		State: domain.CheckpointStateReady, RepositoryRevision: recoveryRepositoryRevision,
		WorktreeDiffHash: recoveryObservationSHA, EventSequence: 2,
		IdempotencyKey: "reconcile-new-checkpoint",
	})
	if err != nil {
		t.Fatal(err)
	}
	oldID := oldCheckpoint.ID
	assessment, err := repositories.RecordRecoveryAssessment(t.Context(), RecordRecoveryAssessment{
		ID: "reconcile-assessment", TaskID: task.ID, RunID: runID,
		CheckpointID: &oldID, Classification: RecoveryClassificationReconcile,
		FindingsJSON: `[{"code":"worktree-head-changed"}]`, DivergencesJSON: `[{}]`,
		ObservationSHA256: recoveryObservationSHA,
		IdempotencyKey:    "reconcile-assessment",
	})
	if err != nil {
		t.Fatal(err)
	}
	control, err := repositories.ReadTaskControl(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	begin := BeginRecoveryReconciliation{
		ExpectedTaskRevision: control.TaskRevision, ExpectedRunRevision: control.RunRevision,
		Decision: RecordRecoveryDecision{
			ID: "reconcile-decision", AssessmentID: assessment.ID,
			TaskID: task.ID, RunID: runID, CheckpointID: &oldID,
			Actor: RecoveryDecisionActorUser, Action: RecoveryActionReconcile,
			ReasonRedacted: "adopt descendant user commit", IdempotencyKey: "reconcile/decision",
		},
		Started: RecordRecoveryAttempt{
			ID: "reconcile-started", AssessmentID: assessment.ID,
			TaskID: task.ID, RunID: runID, CheckpointID: &oldID,
			Action: RecoveryActionReconcile, Outcome: RecoveryAttemptStarted,
			ReasonRedacted: "adopt descendant user commit", IdempotencyKey: "reconcile/started",
		},
	}
	if _, err := repositories.BeginRecoveryReconciliation(t.Context(), BeginRecoveryReconciliation{
		ExpectedTaskRevision: control.TaskRevision + 1, ExpectedRunRevision: control.RunRevision,
		Decision: begin.Decision, Started: begin.Started,
	}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale begin error = %v", err)
	}
	if _, err := repositories.BeginRecoveryReconciliation(t.Context(), begin); err != nil {
		t.Fatal(err)
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := repositories.CommitRecoveryReconciliation(t.Context(), CommitRecoveryReconciliation{
		EventID: eventID, TaskID: task.ID, RunID: runID,
		ExpectedTaskRevision: control.TaskRevision, ExpectedRunRevision: control.RunRevision,
		AssessmentID: assessment.ID, PreviousCheckpointID: oldID,
		ReconciledCheckpointID: newCheckpoint.ID,
		ReasonRedacted:         "adopt descendant user commit",
		IdempotencyKey:         "reconcile/committed",
		TerminalAttemptID:      "reconcile-terminal",
		TerminalIdempotencyKey: "reconcile/terminal",
	})
	if err != nil {
		t.Fatal(err)
	}
	if committed.TaskState != domain.TaskStatePaused || committed.RunState != domain.RunStatePaused ||
		committed.TaskRevision != control.TaskRevision+1 || committed.RunRevision != control.RunRevision+1 {
		t.Fatalf("committed recovery control = %#v", committed)
	}
	var terminalCount, eventCount int
	if err := repositories.database.sql.QueryRowContext(t.Context(),
		`SELECT count(*) FROM checkpoint_recovery_attempts WHERE id = ? AND outcome = 'succeeded'`,
		"reconcile-terminal",
	).Scan(&terminalCount); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(t.Context(),
		`SELECT count(*) FROM task_events WHERE task_id = ? AND event_type = 'task.recovery-reconciled'`,
		task.ID,
	).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if terminalCount != 1 || eventCount != 1 {
		t.Fatalf("terminal=%d event=%d", terminalCount, eventCount)
	}
}

func TestSafeResumeRecoveryAuthorizationAndReplayAreDurable(t *testing.T) {
	t.Parallel()
	repositories, task := createTaskFixture(t, 2200)
	runID := createToolTestRun(t, repositories, task.ID, 2204)
	setTaskRunStates(t, repositories, task.ID, runID, domain.TaskStateRecoveryRequired, domain.RunStateRecoveryRequired)
	if _, err := repositories.database.sql.ExecContext(t.Context(), `UPDATE tasks SET revision = revision + 1 WHERE id = ?`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(t.Context(), `UPDATE runs SET revision = revision + 1 WHERE id = ?`, runID); err != nil {
		t.Fatal(err)
	}
	runPointer := runID
	checkpointValue, err := repositories.CreateCheckpoint(t.Context(), CreateCheckpoint{
		ID: testCheckpointID(t, 2205), TaskID: task.ID, RunID: &runPointer,
		State: domain.CheckpointStateReady, RepositoryRevision: recoveryRepositoryRevision,
		WorktreeDiffHash: recoveryObservationSHA, EventSequence: 1,
		IdempotencyKey: "safe-resume-checkpoint",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpointID := checkpointValue.ID
	assessment, err := repositories.RecordRecoveryAssessment(t.Context(), RecordRecoveryAssessment{
		ID: "safe-resume-assessment", TaskID: task.ID, RunID: runID,
		CheckpointID: &checkpointID, Classification: RecoveryClassificationSafeResume,
		FindingsJSON: `[]`, DivergencesJSON: `[{}]`, ObservationSHA256: recoveryObservationSHA,
		IdempotencyKey: "safe-resume-assessment",
	})
	if err != nil {
		t.Fatal(err)
	}
	control, err := repositories.ReadTaskControl(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	eventID, _ := domain.NewEventID()
	reason := "resume verified recovery checkpoint"
	key := "safe-resume-recovery-key"
	paused, err := repositories.AuthorizeSafeResumeRecovery(t.Context(), AuthorizeSafeResumeRecovery{
		EventID: eventID, TaskID: task.ID, RunID: runID,
		ExpectedTaskRevision: control.TaskRevision, ExpectedRunRevision: control.RunRevision,
		AssessmentID: assessment.ID, CheckpointID: checkpointID,
		ReasonRedacted: reason, IdempotencyKey: key,
		Decision: RecordRecoveryDecision{
			ID: "safe-resume-decision", AssessmentID: assessment.ID,
			TaskID: task.ID, RunID: runID, CheckpointID: &checkpointID,
			Actor: RecoveryDecisionActorUser, Action: RecoveryActionResume,
			ReasonRedacted: reason, IdempotencyKey: key + "/decision",
		},
		Started: RecordRecoveryAttempt{
			ID: "safe-resume-started", AssessmentID: assessment.ID,
			TaskID: task.ID, RunID: runID, CheckpointID: &checkpointID,
			Action: RecoveryActionResume, Outcome: RecoveryAttemptStarted,
			ReasonRedacted: reason, IdempotencyKey: key + "/started",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if paused.Disposition != TaskControlPaused {
		t.Fatalf("authorized control = %#v", paused)
	}
	if _, err := repositories.RecordRecoveryAttempt(t.Context(), RecordRecoveryAttempt{
		ID: "safe-resume-terminal", AssessmentID: assessment.ID,
		TaskID: task.ID, RunID: runID, CheckpointID: &checkpointID,
		Action: RecoveryActionResume, Outcome: RecoveryAttemptSucceeded,
		ReasonRedacted: reason, IdempotencyKey: key + "/terminal",
	}); err != nil {
		t.Fatal(err)
	}
	replay, err := repositories.ReadTaskControlReplay(t.Context(), TaskControlReplayRequest{
		TaskID: task.ID, Operation: TaskControlReplaySafeResumeRecovery,
		ExpectedTaskRevision: control.TaskRevision, ReasonRedacted: reason,
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Found || replay.AssessmentID != assessment.ID || replay.CheckpointID == nil || *replay.CheckpointID != checkpointID {
		t.Fatalf("safe resume replay = %#v", replay)
	}
}
