package storage

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
)

func TestRecoveryFactsAreImmutableIdempotentAndExactlyBound(t *testing.T) {
	t.Parallel()

	repositories, task := createTaskFixture(t, 1700)
	runID := createToolTestRun(t, repositories, task.ID, 1704)
	runPointer := runID
	checkpoint, err := repositories.CreateCheckpoint(
		t.Context(),
		CreateCheckpoint{
			ID: testCheckpointID(t, 1705), TaskID: task.ID, RunID: &runPointer,
			State:              domain.CheckpointStateReady,
			RepositoryRevision: recoveryRepositoryRevision,
			WorktreeDiffHash:   recoveryObservationSHA,
			EventSequence:      1, IdempotencyKey: "recovery-checkpoint",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	checkpointID := checkpoint.ID
	assessmentInput := RecordRecoveryAssessment{
		ID: "recovery-assessment-1", TaskID: task.ID, RunID: runID,
		CheckpointID:   &checkpointID,
		Classification: RecoveryClassificationSafeResume,
		FindingsJSON:   "[]", DivergencesJSON: "[{}]",
		ObservationSHA256: recoveryObservationSHA,
		PatchAvailable:    true,
		PatchLocator:      "refs/codeflux/checkpoints/recovery-fixture",
		PatchPath:         filepath.Join(t.TempDir(), "checkpoint.patch"),
		IdempotencyKey:    "recovery-assessment-idempotency",
	}
	assessment, err := repositories.RecordRecoveryAssessment(
		t.Context(),
		assessmentInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repositories.RecordRecoveryAssessment(
		t.Context(),
		assessmentInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(assessment, replayed) {
		t.Fatalf("idempotent assessment = %#v, want %#v", replayed, assessment)
	}
	if _, err := repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE tasks SET state = 'recovery-required', revision = revision + 1 WHERE id = ?`,
		task.ID,
	); err != nil {
		t.Fatal(err)
	}
	var recoveryRevision uint64
	if err := repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT revision FROM tasks WHERE id = ?`,
		task.ID,
	).Scan(&recoveryRevision); err != nil {
		t.Fatal(err)
	}
	current, err := repositories.GetCurrentRecoveryAssessment(
		t.Context(), task.ID, recoveryRevision,
	)
	if err != nil || current.ID != assessment.ID {
		t.Fatalf("current recovery assessment = %#v, error=%v", current, err)
	}
	if _, err := repositories.GetCurrentRecoveryAssessment(
		t.Context(), task.ID, recoveryRevision+1,
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale recovery assessment error = %v", err)
	}
	conflict := assessmentInput
	conflict.Classification = RecoveryClassificationReconcile
	if _, err := repositories.RecordRecoveryAssessment(
		t.Context(),
		conflict,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("assessment conflict error = %v", err)
	}

	attemptInput := RecordRecoveryAttempt{
		ID: "recovery-attempt-1", AssessmentID: assessment.ID,
		TaskID: task.ID, RunID: runID, CheckpointID: &checkpointID,
		Action: RecoveryActionResume, Outcome: RecoveryAttemptStarted,
		ReasonRedacted: "user authorized safe resume",
		IdempotencyKey: "recovery-attempt-idempotency",
	}
	attempt, err := repositories.RecordRecoveryAttempt(
		t.Context(),
		attemptInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayedAttempt, err := repositories.RecordRecoveryAttempt(
		t.Context(),
		attemptInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attempt, replayedAttempt) {
		t.Fatalf("idempotent attempt = %#v, want %#v", replayedAttempt, attempt)
	}

	decisionInput := RecordRecoveryDecision{
		ID: "recovery-decision-1", AssessmentID: assessment.ID,
		TaskID: task.ID, RunID: runID, CheckpointID: &checkpointID,
		Actor: RecoveryDecisionActorUser, Action: RecoveryActionResume,
		ReasonRedacted: "resume from the verified checkpoint",
		IdempotencyKey: "recovery-decision-idempotency",
	}
	decision, err := repositories.RecordRecoveryDecision(
		t.Context(),
		decisionInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayedDecision, err := repositories.RecordRecoveryDecision(
		t.Context(),
		decisionInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decision, replayedDecision) {
		t.Fatalf(
			"idempotent decision = %#v, want %#v",
			replayedDecision,
			decision,
		)
	}

	for table, id := range map[string]string{
		"checkpoint_recovery_assessments": assessment.ID,
		"checkpoint_recovery_attempts":    attempt.ID,
		"checkpoint_recovery_decisions":   decision.ID,
	} {
		if _, err := repositories.database.sql.ExecContext(
			t.Context(),
			"UPDATE "+table+" SET id = id WHERE id = ?",
			id,
		); err == nil {
			t.Fatalf("%s recovery fact was mutable", table)
		}
		if _, err := repositories.database.sql.ExecContext(
			t.Context(),
			"DELETE FROM "+table+" WHERE id = ?",
			id,
		); err == nil {
			t.Fatalf("%s recovery fact was deletable", table)
		}
	}
}

func TestRecoveryFactsRejectUnboundAndOpenValues(t *testing.T) {
	t.Parallel()

	repositories, task := createTaskFixture(t, 1720)
	runID := createToolTestRun(t, repositories, task.ID, 1724)
	checkpointID := testCheckpointID(t, 1725)
	input := RecordRecoveryAssessment{
		ID: "recovery-invalid", TaskID: task.ID, RunID: runID,
		CheckpointID:   &checkpointID,
		Classification: RecoveryClassification("automatic-magic"),
		FindingsJSON:   "[]", DivergencesJSON: "[]",
		ObservationSHA256: recoveryObservationSHA,
		IdempotencyKey:    "recovery-invalid",
	}
	if _, err := repositories.RecordRecoveryAssessment(
		t.Context(),
		input,
	); err == nil {
		t.Fatal("open recovery classification was accepted")
	}
	if _, err := repositories.RecordRecoveryAttempt(
		t.Context(),
		RecordRecoveryAttempt{
			ID: "recovery-attempt-invalid", AssessmentID: "missing",
			TaskID: task.ID, RunID: runID, CheckpointID: &checkpointID,
			Action:         RecoveryAction("repeat-ambiguous-action"),
			Outcome:        RecoveryAttemptStarted,
			ReasonRedacted: "invalid action",
			IdempotencyKey: "recovery-attempt-invalid",
		},
	); err == nil {
		t.Fatal("open recovery action was accepted")
	}
}

func TestRecoveryCheckpointCandidatesIncludeRunBeforeFirstCheckpoint(
	t *testing.T,
) {
	t.Parallel()

	repositories, task := createTaskFixture(t, 1740)
	runID := createToolTestRun(t, repositories, task.ID, 1744)
	setTaskRunStates(
		t,
		repositories,
		task.ID,
		runID,
		domain.TaskStateRecoveryRequired,
		domain.RunStateRecoveryRequired,
	)
	candidates, err := repositories.ListRecoveryCheckpointCandidates(
		t.Context(),
		10,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 ||
		candidates[0].TaskID != task.ID ||
		candidates[0].RunID != runID ||
		candidates[0].CheckpointID != nil ||
		candidates[0].SchemaVersion != 0 ||
		candidates[0].StateJSON != "" {
		t.Fatalf("pre-checkpoint recovery candidates = %#v", candidates)
	}
	assessment, err := repositories.RecordRecoveryAssessment(
		t.Context(),
		RecordRecoveryAssessment{
			ID:     "recovery-before-first-checkpoint",
			TaskID: task.ID, RunID: runID,
			Classification:    RecoveryClassificationImpossible,
			FindingsJSON:      `[{"code":"checkpoint-missing"}]`,
			DivergencesJSON:   "[]",
			ObservationSHA256: recoveryObservationSHA,
			IdempotencyKey:    "recovery-before-first-checkpoint",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.CheckpointID != nil ||
		assessment.Classification != RecoveryClassificationImpossible {
		t.Fatalf("pre-checkpoint assessment = %#v", assessment)
	}
}

func TestRecoveryActionObservationTreatsPersistedIntentWithoutResultAsAmbiguous(
	t *testing.T,
) {
	t.Parallel()

	fixture := createBoundAgentRunFixture(t, 1760)
	modelRequest := createAgentModelRequestFixture(t, fixture)
	toolRequest, err := fixture.repositories.RecordAgentToolRequest(
		t.Context(),
		RecordAgentToolRequest{
			ID:     "recovery-tool-intent-without-result",
			TaskID: fixture.task.ID, RunID: fixture.runID,
			PlanRevision:          fixture.plan.Revision,
			PlanStepID:            fixture.plan.Plan.Steps[0].ID,
			ModelRequestID:        modelRequest.ID,
			ToolName:              "workspace.apply-edit",
			ToolSchemaVersion:     1,
			ArgumentsRedactedJSON: `{"path":"internal/widget.go"}`,
			ArgumentsSHA256:       strings.Repeat("d", 64),
			IdempotencyKey:        "recovery-tool-intent-without-result",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := fixture.repositories.ReadRecoveryActionObservation(
		t.Context(),
		fixture.task.ID,
		fixture.runID,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.CompletedActionIDs) != 0 {
		t.Fatalf(
			"completed actions before result = %v",
			observation.CompletedActionIDs,
		)
	}
	byID := make(map[string]checkpoint.AmbiguousExternalAction)
	for _, action := range observation.AmbiguousExternalActions {
		byID[action.ActionID] = action
	}
	if action, ok := byID[modelRequest.ID.String()]; !ok ||
		action.Kind != "provider-request" ||
		action.IntentSHA256 != modelRequest.RequestSHA256 {
		t.Fatalf("planned provider intent observation = %#v", action)
	}
	if action, ok := byID[toolRequest.ID]; !ok ||
		action.Kind != "tool-request" ||
		action.IntentSHA256 != toolRequest.ArgumentsSHA256 ||
		action.ToolRequestID != toolRequest.ID {
		t.Fatalf("unresolved tool intent observation = %#v", action)
	}
}

func TestM15CrashBoundaryRecoveryPersistenceMatrix(t *testing.T) {
	boundaries := []checkpoint.Trigger{
		checkpoint.TriggerPlanApproved,
		checkpoint.TriggerMaterialEditApplied,
		checkpoint.TriggerBeforeRiskyAction,
		checkpoint.TriggerValidationSucceeded,
		checkpoint.TriggerUserPaused,
		checkpoint.TriggerGracefulShutdown,
	}
	for index, boundary := range boundaries {
		t.Run(string(boundary), func(t *testing.T) {
			fixture := createBoundAgentRunFixture(t, 1800+index*20)
			binding, err := fixture.repositories.CreateWorktreeBinding(
				t.Context(),
				CreateWorktreeBinding{
					WorkspaceID:  testWorkspaceID(t, 1801+index*20),
					TaskID:       fixture.task.ID,
					RepositoryID: fixture.task.RepositoryID,
					BaseRevision: fixture.plan.RepositoryRevision,
					HeadRevision: fixture.plan.RepositoryRevision,
					BranchName: "codeflux/task/recovery-boundary-" +
						string(boundary),
					WorktreePath: t.TempDir(),
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.repositories.RecordRunToolSchema(
				t.Context(),
				fixture.runID,
				executor.ToolSchemaVersion,
			); err != nil {
				t.Fatal(err)
			}
			recordCheckpointRunConfiguration(t, fixture)
			runtimeState, err :=
				fixture.repositories.ReadCheckpointRuntimeState(
					t.Context(),
					fixture.task.ID,
					fixture.runID,
				)
			if err != nil {
				t.Fatal(err)
			}
			checkpointID, err := domain.NewCheckpointID()
			if err != nil {
				t.Fatal(err)
			}
			canonical := canonicalCheckpointFixture(
				t,
				fixture,
				binding,
				runtimeState,
				strings.Repeat("9", 40),
			)
			commit := atomicCheckpointFixture(
				checkpointID,
				fixture,
				binding,
				runtimeState,
				canonical,
				"recovery-boundary-"+string(boundary),
			)
			commit.Trigger = boundary
			commit.Attribution = recoveryBoundaryAttribution(t, boundary)
			persisted, created, err :=
				fixture.repositories.CommitCheckpointAndEvent(
					t.Context(),
					commit,
				)
			if err != nil {
				t.Fatal(err)
			}
			if !created {
				t.Fatal("boundary checkpoint was not created")
			}
			var checkpointMicros int64
			if err := fixture.repositories.database.sql.QueryRowContext(
				t.Context(),
				`SELECT created_at_unix_micros
				 FROM checkpoints
				 WHERE id = ?`,
				persisted.ID,
			).Scan(&checkpointMicros); err != nil {
				t.Fatal(err)
			}
			commandID := "recovery-boundary-command-" + string(boundary)
			if _, err := fixture.repositories.database.sql.ExecContext(
				t.Context(),
				`INSERT INTO command_executions (
					id, task_id, run_id, state, command_name,
					arguments_redacted_json, working_directory_scope,
					idempotency_key, exit_code,
					started_at_unix_micros, completed_at_unix_micros,
					created_at_unix_micros, updated_at_unix_micros, revision
				 ) VALUES (
				    ?, ?, ?, 'succeeded', 'recovery-fixture', '[]', '.',
				    ?, 0, ?, ?, ?, ?, 0
				 )`,
				commandID,
				fixture.task.ID,
				fixture.runID,
				commandID,
				checkpointMicros+1,
				checkpointMicros+1,
				checkpointMicros+1,
				checkpointMicros+1,
			); err != nil {
				t.Fatal(err)
			}
			databasePath := fixture.repositories.database.path
			if err := fixture.repositories.database.Close(
				t.Context(),
			); err != nil {
				t.Fatal(err)
			}
			reopenedDatabase, err := Open(
				t.Context(),
				OpenOptions{Path: databasePath},
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = reopenedDatabase.Close(context.Background())
			})
			reopenedRepositories, err := NewRepositories(
				reopenedDatabase,
				time.Now,
			)
			if err != nil {
				t.Fatal(err)
			}
			fixture.repositories = reopenedRepositories
			actionObservation, err :=
				fixture.repositories.ReadRecoveryActionObservation(
					t.Context(),
					fixture.task.ID,
					fixture.runID,
					persisted.CheckpointEventSequence,
				)
			if err != nil {
				t.Fatal(err)
			}
			if len(actionObservation.CompletedActionIDs) != 1 ||
				actionObservation.CompletedActionIDs[0] !=
					"command:"+commandID {
				t.Fatalf(
					"post-boundary replay gates = %#v",
					actionObservation,
				)
			}
			candidates, err :=
				fixture.repositories.ListRecoveryCheckpointCandidates(
					t.Context(),
					10,
				)
			if err != nil {
				t.Fatal(err)
			}
			if len(candidates) != 1 ||
				candidates[0].CheckpointID == nil ||
				*candidates[0].CheckpointID != persisted.ID ||
				candidates[0].CheckpointEventSequence !=
					persisted.CheckpointEventSequence {
				t.Fatalf("boundary recovery candidates = %#v", candidates)
			}
			input := RecordRecoveryAssessment{
				ID:     "recovery-boundary-assessment-" + string(boundary),
				TaskID: fixture.task.ID, RunID: fixture.runID,
				CheckpointID:   candidates[0].CheckpointID,
				Classification: RecoveryClassificationSafeResume,
				FindingsJSON:   "[]", DivergencesJSON: "[]",
				ObservationSHA256: recoveryObservationSHA,
				PatchAvailable:    true,
				PatchLocator:      persisted.PreservedRef,
				IdempotencyKey: "recovery-boundary-assessment-" +
					string(boundary),
			}
			first, err := fixture.repositories.RecordRecoveryAssessment(
				t.Context(),
				input,
			)
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := fixture.repositories.RecordRecoveryAssessment(
				t.Context(),
				input,
			)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first, replayed) {
				t.Fatalf(
					"boundary recovery replay = %#v, want %#v",
					replayed,
					first,
				)
			}
			decisionInput := RecordRecoveryDecision{
				ID:             "recovery-boundary-decision-" + string(boundary),
				AssessmentID:   first.ID,
				TaskID:         first.TaskID,
				RunID:          first.RunID,
				CheckpointID:   first.CheckpointID,
				Actor:          RecoveryDecisionActorUser,
				Action:         RecoveryActionPreservePatch,
				ReasonRedacted: "preserve the checkpoint boundary patch",
				IdempotencyKey: "recovery-boundary-decision-" +
					string(boundary),
			}
			decision, err := fixture.repositories.RecordRecoveryDecision(
				t.Context(),
				decisionInput,
			)
			if err != nil {
				t.Fatal(err)
			}
			replayedDecision, err :=
				fixture.repositories.RecordRecoveryDecision(
					t.Context(),
					decisionInput,
				)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decision, replayedDecision) {
				t.Fatalf(
					"boundary decision replay = %#v, want %#v",
					replayedDecision,
					decision,
				)
			}
			attemptInput := RecordRecoveryAttempt{
				ID:             "recovery-boundary-attempt-" + string(boundary),
				AssessmentID:   first.ID,
				TaskID:         first.TaskID,
				RunID:          first.RunID,
				CheckpointID:   first.CheckpointID,
				Action:         RecoveryActionPreservePatch,
				Outcome:        RecoveryAttemptStarted,
				ReasonRedacted: "preserve the checkpoint boundary patch",
				IdempotencyKey: "recovery-boundary-attempt-" +
					string(boundary),
			}
			attempt, err := fixture.repositories.RecordRecoveryAttempt(
				t.Context(),
				attemptInput,
			)
			if err != nil {
				t.Fatal(err)
			}
			replayedAttempt, err :=
				fixture.repositories.RecordRecoveryAttempt(
					t.Context(),
					attemptInput,
				)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(attempt, replayedAttempt) {
				t.Fatalf(
					"boundary attempt replay = %#v, want %#v",
					replayedAttempt,
					attempt,
				)
			}
		})
	}
}

func recoveryBoundaryAttribution(
	t *testing.T,
	trigger checkpoint.Trigger,
) checkpoint.TriggerAttribution {
	t.Helper()
	var value checkpoint.TriggerAttribution
	switch trigger {
	case checkpoint.TriggerPlanApproved:
		id, err := domain.NewApprovalID()
		if err != nil {
			t.Fatal(err)
		}
		value.ApprovalID = &id
	case checkpoint.TriggerMaterialEditApplied:
		value.ToolRequestID = "recovery-boundary-tool-request"
	case checkpoint.TriggerBeforeRiskyAction:
		value.PermissionDecisionID = "recovery-boundary-permission"
		value.ActionSHA256 = strings.Repeat("7", 64)
	case checkpoint.TriggerValidationSucceeded:
		id, err := domain.NewValidationID()
		if err != nil {
			t.Fatal(err)
		}
		value.ValidationID = &id
	}
	return value
}

const (
	recoveryRepositoryRevision = "1111111111111111111111111111111111111111"
	recoveryObservationSHA     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)
