package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"slices"
	"time"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
)

// RecoveryClassification is the persisted spelling of the four M15 recovery
// outcomes. Coordinator policy owns classification; storage validates and
// preserves the resulting fact.
type RecoveryClassification string

const (
	RecoveryClassificationSafeResume RecoveryClassification = "safe-resume"
	RecoveryClassificationReconcile  RecoveryClassification = "reconcile-required"
	RecoveryClassificationPatchOnly  RecoveryClassification = "patch-preservation-only"
	RecoveryClassificationImpossible RecoveryClassification = "unrecoverable"
)

// RecoveryCheckpointCandidate is the latest versioned checkpoint for one
// incomplete run. StateJSON remains opaque to storage.
type RecoveryCheckpointCandidate struct {
	CheckpointID            *domain.CheckpointID
	TaskID                  domain.TaskID
	RunID                   domain.RunID
	SchemaVersion           uint64
	StateJSON               string
	StateSHA256             string
	CheckpointEventSequence uint64
}

// RecoveryActionObservation is the durable replay gate for completed and
// outcome-unknown model, tool, and command operations.
type RecoveryActionObservation struct {
	CompletedActionIDs       []string
	AmbiguousExternalActions []checkpoint.AmbiguousExternalAction
}

// RecoveryAssessmentRecord is one immutable, structured startup or resume
// comparison.
type RecoveryAssessmentRecord struct {
	ID                string
	TaskID            domain.TaskID
	RunID             domain.RunID
	CheckpointID      *domain.CheckpointID
	Classification    RecoveryClassification
	FindingsJSON      string
	DivergencesJSON   string
	ObservationSHA256 string
	PatchAvailable    bool
	PatchLocator      string
	PatchPath         string
	IdempotencyKey    string
	CreatedAt         time.Time
}

// RecordRecoveryAssessment declares one idempotent immutable comparison.
type RecordRecoveryAssessment struct {
	ID                string
	TaskID            domain.TaskID
	RunID             domain.RunID
	CheckpointID      *domain.CheckpointID
	Classification    RecoveryClassification
	FindingsJSON      string
	DivergencesJSON   string
	ObservationSHA256 string
	PatchAvailable    bool
	PatchLocator      string
	PatchPath         string
	IdempotencyKey    string
}

// RecoveryAction is one closed user-visible recovery choice.
type RecoveryAction string

const (
	RecoveryActionResume        RecoveryAction = "resume"
	RecoveryActionReconcile     RecoveryAction = "reconcile"
	RecoveryActionPreservePatch RecoveryAction = "preserve-patch"
	RecoveryActionAbandon       RecoveryAction = "abandon"
)

// RecoveryAttemptOutcome is one immutable attributable operation result.
type RecoveryAttemptOutcome string

const (
	RecoveryAttemptStarted        RecoveryAttemptOutcome = "started"
	RecoveryAttemptSucceeded      RecoveryAttemptOutcome = "succeeded"
	RecoveryAttemptFailed         RecoveryAttemptOutcome = "failed"
	RecoveryAttemptCancelled      RecoveryAttemptOutcome = "cancelled"
	RecoveryAttemptOutcomeUnknown RecoveryAttemptOutcome = "outcome-unknown"
)

// RecoveryAttemptRecord attributes one immutable attempt fact to its
// assessment and checkpoint.
type RecoveryAttemptRecord struct {
	ID             string
	AssessmentID   string
	TaskID         domain.TaskID
	RunID          domain.RunID
	CheckpointID   *domain.CheckpointID
	Action         RecoveryAction
	Outcome        RecoveryAttemptOutcome
	ReasonRedacted string
	IdempotencyKey string
	CreatedAt      time.Time
}

// RecordRecoveryAttempt declares one idempotent immutable attempt fact.
type RecordRecoveryAttempt struct {
	ID             string
	AssessmentID   string
	TaskID         domain.TaskID
	RunID          domain.RunID
	CheckpointID   *domain.CheckpointID
	Action         RecoveryAction
	Outcome        RecoveryAttemptOutcome
	ReasonRedacted string
	IdempotencyKey string
}

// RecoveryDecisionActor identifies who committed a recovery choice.
type RecoveryDecisionActor string

const (
	RecoveryDecisionActorUser   RecoveryDecisionActor = "user"
	RecoveryDecisionActorSystem RecoveryDecisionActor = "system"
)

// RecoveryDecisionRecord is one immutable user or system choice made from a
// persisted recovery assessment.
type RecoveryDecisionRecord struct {
	ID             string
	AssessmentID   string
	TaskID         domain.TaskID
	RunID          domain.RunID
	CheckpointID   *domain.CheckpointID
	Actor          RecoveryDecisionActor
	Action         RecoveryAction
	ReasonRedacted string
	IdempotencyKey string
	CreatedAt      time.Time
}

// RecordRecoveryDecision declares one idempotent immutable recovery choice.
type RecordRecoveryDecision struct {
	ID             string
	AssessmentID   string
	TaskID         domain.TaskID
	RunID          domain.RunID
	CheckpointID   *domain.CheckpointID
	Actor          RecoveryDecisionActor
	Action         RecoveryAction
	ReasonRedacted string
	IdempotencyKey string
}

// BeginRecoveryReconciliation atomically binds a user's reconcile authority
// and the started-attempt fact to the exact recovery-required task revision.
// Neither record is committed when the assessment or task control changed.
type BeginRecoveryReconciliation struct {
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	Decision             RecordRecoveryDecision
	Started              RecordRecoveryAttempt
}

// RecoveryReconciliationStart is the durable authority established before
// checkpoint/Git preservation begins.
type RecoveryReconciliationStart struct {
	Decision RecoveryDecisionRecord
	Started  RecoveryAttemptRecord
}

// RecoveryOperations groups latest-checkpoint discovery with immutable
// assessments, attempts, and decisions.
type RecoveryOperations interface {
	ListRecoveryCheckpointCandidates(
		context.Context,
		int,
	) ([]RecoveryCheckpointCandidate, error)
	RecordRecoveryAssessment(
		context.Context,
		RecordRecoveryAssessment,
	) (RecoveryAssessmentRecord, error)
	RecordRecoveryAttempt(
		context.Context,
		RecordRecoveryAttempt,
	) (RecoveryAttemptRecord, error)
	RecordRecoveryDecision(
		context.Context,
		RecordRecoveryDecision,
	) (RecoveryDecisionRecord, error)
	GetRecoveryAssessment(
		context.Context,
		string,
	) (RecoveryAssessmentRecord, error)
	GetCurrentRecoveryAssessment(
		context.Context,
		domain.TaskID,
		uint64,
	) (RecoveryAssessmentRecord, error)
	ReadRecoveryActionObservation(
		context.Context,
		domain.TaskID,
		domain.RunID,
		uint64,
	) (RecoveryActionObservation, error)
	BeginRecoveryReconciliation(
		context.Context,
		BeginRecoveryReconciliation,
	) (RecoveryReconciliationStart, error)
}

// BeginRecoveryReconciliation records authority and its started attempt in
// one transaction after verifying the exact recovery-required task/run and
// reconcile-required assessment. This is intentionally stricter than the
// general immutable decision writers because reconciliation can preserve a
// new Git checkpoint as an external effect.
func (repositories *Repositories) BeginRecoveryReconciliation(
	ctx context.Context,
	input BeginRecoveryReconciliation,
) (RecoveryReconciliationStart, error) {
	if input.ExpectedTaskRevision == 0 || input.ExpectedRunRevision == 0 {
		return RecoveryReconciliationStart{}, errors.New(
			"recovery reconciliation revisions are required",
		)
	}
	if err := validateRecordRecoveryDecision(input.Decision); err != nil {
		return RecoveryReconciliationStart{}, err
	}
	if err := validateRecordRecoveryAttempt(input.Started); err != nil {
		return RecoveryReconciliationStart{}, err
	}
	if input.Decision.Action != RecoveryActionReconcile ||
		input.Started.Action != RecoveryActionReconcile ||
		input.Started.Outcome != RecoveryAttemptStarted ||
		input.Decision.AssessmentID != input.Started.AssessmentID ||
		input.Decision.TaskID != input.Started.TaskID ||
		input.Decision.RunID != input.Started.RunID ||
		!sameRecoveryCheckpointID(
			input.Decision.CheckpointID,
			input.Started.CheckpointID,
		) {
		return RecoveryReconciliationStart{}, errors.New(
			"recovery reconciliation authority identities are inconsistent",
		)
	}
	_, micros := repositories.timestamp()
	decision := recoveryDecisionRecord(input.Decision, micros)
	started := recoveryAttemptRecord(input.Started, micros)
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := scanTaskControl(
			transaction.sql.QueryRowContext(
				ctx,
				taskControlSelect+` WHERE task.id = ? AND run.id = ?`,
				input.Decision.TaskID,
				input.Decision.RunID,
			),
			"begin recovery reconciliation",
		)
		if err != nil {
			return err
		}
		if current.TaskRevision != input.ExpectedTaskRevision ||
			current.RunRevision != input.ExpectedRunRevision {
			return typedError(
				ErrStaleRevision,
				"begin recovery reconciliation",
				errors.New("task or run revision changed"),
			)
		}
		if current.TaskState != domain.TaskStateRecoveryRequired ||
			current.RunState != domain.RunStateRecoveryRequired {
			return typedError(
				ErrConflict,
				"begin recovery reconciliation",
				errors.New("task and run are not recovery-required"),
			)
		}
		assessment, err := scanRecoveryAssessment(transaction.sql.QueryRowContext(
			ctx,
			`SELECT id, task_id, run_id, checkpoint_id, classification,
			        findings_redacted_json, divergences_redacted_json,
			        observation_sha256, patch_available, patch_locator,
			        patch_path, idempotency_key, created_at_unix_micros
			 FROM checkpoint_recovery_assessments WHERE id = ?`,
			input.Decision.AssessmentID,
		))
		if err != nil {
			return err
		}
		if assessment.TaskID != input.Decision.TaskID ||
			assessment.RunID != input.Decision.RunID ||
			!sameRecoveryCheckpointID(assessment.CheckpointID, input.Decision.CheckpointID) ||
			assessment.Classification != RecoveryClassificationReconcile {
			return typedError(
				ErrConflict,
				"begin recovery reconciliation",
				errors.New("assessment is not the exact reconcile-required recovery assessment"),
			)
		}
		existingDecision, decisionFound, err := findRecoveryDecisionByIdempotency(
			ctx, transaction.sql, input.Decision.TaskID, input.Decision.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		existingStarted, startedFound, err := findRecoveryAttemptByIdempotency(
			ctx, transaction.sql, input.Started.TaskID, input.Started.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if decisionFound || startedFound {
			if !decisionFound || !startedFound ||
				!sameRecoveryDecision(existingDecision, decision) ||
				!sameRecoveryAttempt(existingStarted, started) {
				return typedError(
					ErrConflict,
					"begin recovery reconciliation",
					errors.New("reconciliation idempotency identity was reused"),
				)
			}
			decision, started = existingDecision, existingStarted
			return nil
		}
		if err := insertRecoveryDecision(ctx, transaction.sql, input.Decision, micros); err != nil {
			return err
		}
		return insertRecoveryAttempt(ctx, transaction.sql, input.Started, micros)
	})
	return RecoveryReconciliationStart{Decision: decision, Started: started}, err
}

// GetCurrentRecoveryAssessment resolves the newest immutable assessment only
// while the task remains recovery-required at the caller's exact revision.
func (repositories *Repositories) GetCurrentRecoveryAssessment(
	ctx context.Context,
	taskID domain.TaskID,
	expectedTaskRevision uint64,
) (RecoveryAssessmentRecord, error) {
	if taskID.IsZero() || expectedTaskRevision == 0 {
		return RecoveryAssessmentRecord{}, errors.New("recovery task and expected revision are required")
	}
	record, err := scanRecoveryAssessment(repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT assessment.id, assessment.task_id, assessment.run_id,
		        assessment.checkpoint_id, assessment.classification,
		        assessment.findings_redacted_json, assessment.divergences_redacted_json,
		        assessment.observation_sha256, assessment.patch_available,
		        assessment.patch_locator, assessment.patch_path,
		        assessment.idempotency_key, assessment.created_at_unix_micros
		 FROM checkpoint_recovery_assessments AS assessment
		 JOIN tasks AS task ON task.id = assessment.task_id
		 WHERE assessment.task_id = ?
		   AND task.state = 'recovery-required'
		   AND task.revision = ?
		 ORDER BY assessment.created_at_unix_micros DESC, assessment.id DESC
		 LIMIT 1`,
		taskID,
		expectedTaskRevision,
	))
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RecoveryAssessmentRecord{}, err
	}
	var state string
	var revision uint64
	if taskErr := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT state, revision FROM tasks WHERE id = ?`,
		taskID,
	).Scan(&state, &revision); taskErr != nil {
		if errors.Is(taskErr, sql.ErrNoRows) {
			return RecoveryAssessmentRecord{}, typedError(ErrNotFound, "get current recovery assessment", taskErr)
		}
		return RecoveryAssessmentRecord{}, classify("read recovery task", taskErr)
	}
	if revision != expectedTaskRevision {
		return RecoveryAssessmentRecord{}, typedError(
			ErrStaleRevision,
			"get current recovery assessment",
			errors.New("task revision changed"),
		)
	}
	if state != string(domain.TaskStateRecoveryRequired) {
		return RecoveryAssessmentRecord{}, typedError(
			ErrConflict,
			"get current recovery assessment",
			errors.New("task is not recovery-required"),
		)
	}
	return RecoveryAssessmentRecord{}, typedError(
		ErrNotFound,
		"get current recovery assessment",
		sql.ErrNoRows,
	)
}

// GetRecoveryAssessment loads one immutable assessment by its exact identity.
func (repositories *Repositories) GetRecoveryAssessment(
	ctx context.Context,
	id string,
) (RecoveryAssessmentRecord, error) {
	if err := validateBounded("recovery assessment ID", id, 255); err != nil {
		return RecoveryAssessmentRecord{}, err
	}
	return scanRecoveryAssessment(repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, task_id, run_id, checkpoint_id, classification,
		        findings_redacted_json, divergences_redacted_json,
		        observation_sha256, patch_available, patch_locator, patch_path,
		        idempotency_key, created_at_unix_micros
		 FROM checkpoint_recovery_assessments
		 WHERE id = ?`,
		id,
	))
}

// ReadRecoveryActionObservation returns durable replay gates for one run.
func (repositories *Repositories) ReadRecoveryActionObservation(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	checkpointEventSequence uint64,
) (RecoveryActionObservation, error) {
	if taskID.IsZero() || runID.IsZero() || checkpointEventSequence == 0 {
		return RecoveryActionObservation{},
			errors.New(
				"recovery action task, run, and checkpoint event are required",
			)
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT action_id
		 FROM (
		     SELECT 'provider:' || id AS action_id
		     FROM provider_logical_requests
		     WHERE task_id = ? AND run_id = ?
		       AND state IN (
		           'succeeded', 'failed', 'cancelled', 'retry-exhausted'
		       )
		       AND completed_at_unix_micros >= (
		           SELECT created_at_unix_micros
		           FROM task_events
		           WHERE task_id = ? AND sequence = ?
		       )
		     UNION
		     SELECT 'tool:' || result.tool_request_id AS action_id
		     FROM agent_tool_results AS result
		     JOIN agent_tool_requests AS request
		       ON request.id = result.tool_request_id
		     WHERE request.task_id = ? AND request.run_id = ?
		       AND result.state IN ('succeeded', 'failed', 'cancelled')
		       AND result.created_at_unix_micros >= (
		           SELECT created_at_unix_micros
		           FROM task_events
		           WHERE task_id = ? AND sequence = ?
		       )
		     UNION
		     SELECT 'command:' || id AS action_id
		     FROM command_executions
		     WHERE task_id = ? AND run_id = ?
		       AND state IN ('succeeded', 'failed', 'cancelled')
		       AND completed_at_unix_micros >= (
		           SELECT created_at_unix_micros
		           FROM task_events
		           WHERE task_id = ? AND sequence = ?
		       )
		 )
		 ORDER BY action_id`,
		taskID,
		runID,
		taskID,
		checkpointEventSequence,
		taskID,
		runID,
		taskID,
		checkpointEventSequence,
		taskID,
		runID,
		taskID,
		checkpointEventSequence,
	)
	if err != nil {
		return RecoveryActionObservation{}, classify(
			"list completed recovery actions",
			err,
		)
	}
	var completed []string
	for rows.Next() {
		var actionID string
		if err := rows.Scan(&actionID); err != nil {
			rows.Close()
			return RecoveryActionObservation{}, classify(
				"scan completed recovery action",
				err,
			)
		}
		completed = append(completed, actionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return RecoveryActionObservation{}, classify(
			"iterate completed recovery actions",
			err,
		)
	}
	if err := rows.Close(); err != nil {
		return RecoveryActionObservation{}, classify(
			"close completed recovery actions",
			err,
		)
	}
	ambiguous, err := listRecoveryAmbiguousActions(
		ctx,
		repositories.database.sql,
		taskID,
		runID,
	)
	if err != nil {
		return RecoveryActionObservation{}, err
	}
	return RecoveryActionObservation{
		CompletedActionIDs:       completed,
		AmbiguousExternalActions: ambiguous,
	}, nil
}

func listRecoveryAmbiguousActions(
	ctx context.Context,
	queries checkpointQueryer,
	taskID domain.TaskID,
	runID domain.RunID,
) ([]checkpoint.AmbiguousExternalAction, error) {
	rows, err := queries.QueryContext(
		ctx,
		`SELECT action_id, kind, intent_sha256, tool_request_id
		 FROM (
		     SELECT request.id AS action_id,
		            'provider-request' AS kind,
		            request.request_sha256 AS intent_sha256,
		            '' AS tool_request_id
		     FROM provider_logical_requests AS request
		     WHERE request.task_id = ? AND request.run_id = ?
		       AND request.state IN ('planned', 'in-flight', 'outcome-unknown')
		     UNION ALL
		     SELECT request.id AS action_id,
		            'tool-request' AS kind,
		            request.arguments_sha256 AS intent_sha256,
		            request.id AS tool_request_id
		     FROM agent_tool_requests AS request
		     LEFT JOIN agent_tool_results AS result
		       ON result.tool_request_id = request.id
		     WHERE request.task_id = ? AND request.run_id = ?
		       AND (result.id IS NULL OR result.state = 'outcome-unknown')
		 )
		 ORDER BY action_id, kind`,
		taskID,
		runID,
		taskID,
		runID,
	)
	if err != nil {
		return nil, classify("list ambiguous recovery actions", err)
	}
	var values []checkpoint.AmbiguousExternalAction
	for rows.Next() {
		var value checkpoint.AmbiguousExternalAction
		if err := rows.Scan(
			&value.ActionID,
			&value.Kind,
			&value.IntentSHA256,
			&value.ToolRequestID,
		); err != nil {
			rows.Close()
			return nil, classify("scan ambiguous recovery action", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, classify("iterate ambiguous recovery actions", err)
	}
	if err := rows.Close(); err != nil {
		return nil, classify("close ambiguous recovery actions", err)
	}
	commandRows, err := queries.QueryContext(
		ctx,
		`SELECT id, command_name, arguments_redacted_json,
		        working_directory_scope
		 FROM command_executions
		 WHERE task_id = ? AND run_id = ?
		   AND state IN (
		       'pending', 'awaiting-authority', 'authorized', 'running',
		       'outcome-unknown'
		   )
		 ORDER BY id`,
		taskID,
		runID,
	)
	if err != nil {
		return nil, classify("list ambiguous recovery commands", err)
	}
	for commandRows.Next() {
		var actionID, commandName, arguments, workingDirectory string
		if err := commandRows.Scan(
			&actionID,
			&commandName,
			&arguments,
			&workingDirectory,
		); err != nil {
			commandRows.Close()
			return nil, classify("scan ambiguous recovery command", err)
		}
		digest := sha256.Sum256([]byte(
			commandName + "\x00" + arguments + "\x00" + workingDirectory,
		))
		values = append(values, checkpoint.AmbiguousExternalAction{
			ActionID:     actionID,
			Kind:         "command-execution",
			IntentSHA256: hex.EncodeToString(digest[:]),
		})
	}
	if err := commandRows.Err(); err != nil {
		commandRows.Close()
		return nil, classify("iterate ambiguous recovery commands", err)
	}
	if err := commandRows.Close(); err != nil {
		return nil, classify("close ambiguous recovery commands", err)
	}
	slices.SortFunc(
		values,
		func(left, right checkpoint.AmbiguousExternalAction) int {
			if left.ActionID < right.ActionID {
				return -1
			}
			if left.ActionID > right.ActionID {
				return 1
			}
			if left.Kind < right.Kind {
				return -1
			}
			if left.Kind > right.Kind {
				return 1
			}
			return 0
		},
	)
	return values, nil
}

// ListRecoveryCheckpointCandidates returns at most one latest versioned
// checkpoint for each incomplete run. It never changes task or run state.
func (repositories *Repositories) ListRecoveryCheckpointCandidates(
	ctx context.Context,
	limit int,
) ([]RecoveryCheckpointCandidate, error) {
	if limit < 1 || limit > maximumUnownedRunCandidates {
		return nil, errors.New("recovery checkpoint limit is outside supported bounds")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT checkpoint.id, task.id, run.id,
		        checkpoint.schema_version, checkpoint.canonical_state_json,
		        checkpoint.state_sha256, checkpoint.event_sequence
		 FROM tasks AS task
		 JOIN runs AS run ON run.task_id = task.id
		 LEFT JOIN checkpoints AS checkpoint
		   ON checkpoint.id = (
		       SELECT latest.id
		       FROM checkpoints AS latest
		       WHERE latest.task_id = task.id
		         AND latest.run_id = run.id
		         AND latest.schema_version IS NOT NULL
		         AND latest.state = 'ready'
		       ORDER BY latest.event_sequence DESC,
		                latest.created_at_unix_micros DESC,
		                latest.id DESC
		       LIMIT 1
		   )
		 WHERE task.state IN (
		         'running', 'paused', 'awaiting-authority', 'validating',
		         'awaiting-review', 'failed', 'recovery-required'
		       )
		   AND run.state IN (
		         'pending', 'starting', 'running', 'pausing', 'paused',
		         'validating', 'recovery-required'
		       )
		 ORDER BY task.created_at_unix_micros, task.id, run.attempt, run.id
		 LIMIT ?`,
		limit+1,
	)
	if err != nil {
		return nil, classify("list recovery checkpoint candidates", err)
	}
	defer rows.Close()
	var candidates []RecoveryCheckpointCandidate
	for rows.Next() {
		var (
			candidate     RecoveryCheckpointCandidate
			checkpointRaw sql.NullString
			schemaRaw     sql.NullInt64
			stateJSONRaw  sql.NullString
			stateSHARaw   sql.NullString
			eventSequence sql.NullInt64
		)
		if err := rows.Scan(
			&checkpointRaw,
			&candidate.TaskID,
			&candidate.RunID,
			&schemaRaw,
			&stateJSONRaw,
			&stateSHARaw,
			&eventSequence,
		); err != nil {
			return nil, classify("scan recovery checkpoint candidate", err)
		}
		if checkpointRaw.Valid {
			parsed, err := domain.ParseCheckpointID(checkpointRaw.String)
			if err != nil {
				return nil, replayCorruption(
					"recovery checkpoint identity is invalid",
				)
			}
			candidate.CheckpointID = &parsed
			if !schemaRaw.Valid || !stateJSONRaw.Valid ||
				!stateSHARaw.Valid || !eventSequence.Valid ||
				schemaRaw.Int64 < 1 || eventSequence.Int64 < 1 {
				return nil, replayCorruption(
					"recovery checkpoint metadata is incomplete",
				)
			}
			candidate.SchemaVersion = uint64(schemaRaw.Int64)
			candidate.StateJSON = stateJSONRaw.String
			candidate.StateSHA256 = stateSHARaw.String
			candidate.CheckpointEventSequence = uint64(eventSequence.Int64)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate recovery checkpoint candidates", err)
	}
	if len(candidates) > limit {
		return nil, errors.New("recovery checkpoint candidate count exceeds bound")
	}
	return candidates, nil
}

// RecordRecoveryAssessment persists one immutable, structured comparison.
func (repositories *Repositories) RecordRecoveryAssessment(
	ctx context.Context,
	input RecordRecoveryAssessment,
) (RecoveryAssessmentRecord, error) {
	if err := validateRecordRecoveryAssessment(input); err != nil {
		return RecoveryAssessmentRecord{}, err
	}
	_, micros := repositories.timestamp()
	now := repositoryTime(micros)
	record := RecoveryAssessmentRecord{
		ID: input.ID, TaskID: input.TaskID, RunID: input.RunID,
		CheckpointID:      input.CheckpointID,
		Classification:    input.Classification,
		FindingsJSON:      input.FindingsJSON,
		DivergencesJSON:   input.DivergencesJSON,
		ObservationSHA256: input.ObservationSHA256,
		PatchAvailable:    input.PatchAvailable,
		PatchLocator:      input.PatchLocator, PatchPath: input.PatchPath,
		IdempotencyKey: input.IdempotencyKey, CreatedAt: now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findRecoveryAssessmentByIdempotency(
			ctx,
			transaction.sql,
			input.TaskID,
			input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if !sameRecoveryAssessment(existing, record) {
				return typedError(
					ErrConflict,
					"record recovery assessment",
					errors.New("idempotency key belongs to another assessment"),
				)
			}
			record = existing
			return nil
		}
		if err := verifyRecoveryCheckpointBinding(
			ctx,
			transaction.sql,
			input.TaskID,
			input.RunID,
			input.CheckpointID,
		); err != nil {
			return err
		}
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO checkpoint_recovery_assessments (
				id, task_id, run_id, checkpoint_id, classification,
				findings_redacted_json, divergences_redacted_json,
				observation_sha256, patch_available, patch_locator, patch_path,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID,
			input.TaskID,
			input.RunID,
			nullableRecoveryCheckpointID(input.CheckpointID),
			input.Classification,
			input.FindingsJSON,
			input.DivergencesJSON,
			input.ObservationSHA256,
			input.PatchAvailable,
			nullableRecoveryPatchValue(input.PatchLocator),
			nullableRecoveryPatchValue(input.PatchPath),
			input.IdempotencyKey,
			micros,
		)
		return repositoryWriteError("record recovery assessment", err)
	})
	return record, err
}

// RecordRecoveryAttempt persists one immutable attempt outcome. A caller uses
// distinct idempotency identities for started and terminal facts.
func (repositories *Repositories) RecordRecoveryAttempt(
	ctx context.Context,
	input RecordRecoveryAttempt,
) (RecoveryAttemptRecord, error) {
	if err := validateRecordRecoveryAttempt(input); err != nil {
		return RecoveryAttemptRecord{}, err
	}
	_, micros := repositories.timestamp()
	record := recoveryAttemptRecord(input, micros)
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findRecoveryAttemptByIdempotency(
			ctx,
			transaction.sql,
			input.TaskID,
			input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if !sameRecoveryAttempt(existing, record) {
				return typedError(
					ErrConflict,
					"record recovery attempt",
					errors.New("idempotency key belongs to another attempt"),
				)
			}
			record = existing
			return nil
		}
		if err := verifyRecoveryAssessmentBinding(
			ctx,
			transaction.sql,
			input.AssessmentID,
			input.TaskID,
			input.RunID,
			input.CheckpointID,
		); err != nil {
			return err
		}
		return insertRecoveryAttempt(ctx, transaction.sql, input, micros)
	})
	return record, err
}

// RecordRecoveryDecision persists one immutable user or system choice.
func (repositories *Repositories) RecordRecoveryDecision(
	ctx context.Context,
	input RecordRecoveryDecision,
) (RecoveryDecisionRecord, error) {
	if err := validateRecordRecoveryDecision(input); err != nil {
		return RecoveryDecisionRecord{}, err
	}
	_, micros := repositories.timestamp()
	record := recoveryDecisionRecord(input, micros)
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findRecoveryDecisionByIdempotency(
			ctx,
			transaction.sql,
			input.TaskID,
			input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if !sameRecoveryDecision(existing, record) {
				return typedError(
					ErrConflict,
					"record recovery decision",
					errors.New("idempotency key belongs to another decision"),
				)
			}
			record = existing
			return nil
		}
		if err := verifyRecoveryAssessmentBinding(
			ctx,
			transaction.sql,
			input.AssessmentID,
			input.TaskID,
			input.RunID,
			input.CheckpointID,
		); err != nil {
			return err
		}
		return insertRecoveryDecision(ctx, transaction.sql, input, micros)
	})
	return record, err
}

func recoveryAttemptRecord(input RecordRecoveryAttempt, micros int64) RecoveryAttemptRecord {
	return RecoveryAttemptRecord{
		ID: input.ID, AssessmentID: input.AssessmentID,
		TaskID: input.TaskID, RunID: input.RunID,
		CheckpointID: input.CheckpointID, Action: input.Action,
		Outcome: input.Outcome, ReasonRedacted: input.ReasonRedacted,
		IdempotencyKey: input.IdempotencyKey, CreatedAt: repositoryTime(micros),
	}
}

func recoveryDecisionRecord(input RecordRecoveryDecision, micros int64) RecoveryDecisionRecord {
	return RecoveryDecisionRecord{
		ID: input.ID, AssessmentID: input.AssessmentID,
		TaskID: input.TaskID, RunID: input.RunID,
		CheckpointID: input.CheckpointID, Actor: input.Actor,
		Action: input.Action, ReasonRedacted: input.ReasonRedacted,
		IdempotencyKey: input.IdempotencyKey, CreatedAt: repositoryTime(micros),
	}
}

type recoveryExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertRecoveryAttempt(
	ctx context.Context,
	queries recoveryExecer,
	input RecordRecoveryAttempt,
	micros int64,
) error {
	_, err := queries.ExecContext(
		ctx,
		`INSERT INTO checkpoint_recovery_attempts (
			id, assessment_id, task_id, run_id, checkpoint_id,
			action, outcome, reason_redacted, idempotency_key,
			created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID, input.AssessmentID, input.TaskID, input.RunID,
		nullableRecoveryCheckpointID(input.CheckpointID),
		input.Action, input.Outcome, input.ReasonRedacted,
		input.IdempotencyKey, micros,
	)
	return repositoryWriteError("record recovery attempt", err)
}

func insertRecoveryDecision(
	ctx context.Context,
	queries recoveryExecer,
	input RecordRecoveryDecision,
	micros int64,
) error {
	_, err := queries.ExecContext(
		ctx,
		`INSERT INTO checkpoint_recovery_decisions (
			id, assessment_id, task_id, run_id, checkpoint_id,
			actor, action, reason_redacted, idempotency_key,
			created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID, input.AssessmentID, input.TaskID, input.RunID,
		nullableRecoveryCheckpointID(input.CheckpointID),
		input.Actor, input.Action, input.ReasonRedacted,
		input.IdempotencyKey, micros,
	)
	return repositoryWriteError("record recovery decision", err)
}

func validateRecordRecoveryAssessment(input RecordRecoveryAssessment) error {
	switch {
	case input.TaskID.IsZero() || input.RunID.IsZero():
		return errors.New("recovery assessment task and run are required")
	case !input.Classification.isValid():
		return errors.New("recovery assessment classification is invalid")
	case input.PatchAvailable !=
		(input.PatchLocator != "" || input.PatchPath != ""):
		return errors.New("recovery assessment patch availability is inconsistent")
	}
	for label, value := range map[string]string{
		"recovery assessment ID":              input.ID,
		"recovery assessment idempotency key": input.IdempotencyKey,
	} {
		if err := validateBounded(label, value, 255); err != nil {
			return err
		}
	}
	if err := validateJSONArray(
		"recovery assessment findings",
		input.FindingsJSON,
		64<<10,
	); err != nil {
		return err
	}
	if err := validateJSONArray(
		"recovery assessment divergences",
		input.DivergencesJSON,
		64<<10,
	); err != nil {
		return err
	}
	if err := validateSHA256(
		"recovery assessment observation",
		input.ObservationSHA256,
	); err != nil {
		return err
	}
	if input.PatchLocator != "" {
		if err := validateBounded(
			"recovery patch locator",
			input.PatchLocator,
			512,
		); err != nil {
			return err
		}
	}
	if input.PatchPath != "" {
		return validateBounded("recovery patch path", input.PatchPath, 4096)
	}
	return nil
}

func validateRecordRecoveryAttempt(input RecordRecoveryAttempt) error {
	if input.TaskID.IsZero() || input.RunID.IsZero() {
		return errors.New("recovery attempt task and run are required")
	}
	if !input.Action.isValid() || !input.Outcome.isValid() {
		return errors.New("recovery attempt action or outcome is invalid")
	}
	for label, value := range map[string]string{
		"recovery attempt ID":              input.ID,
		"recovery attempt assessment ID":   input.AssessmentID,
		"recovery attempt idempotency key": input.IdempotencyKey,
	} {
		if err := validateBounded(label, value, 255); err != nil {
			return err
		}
	}
	return validateBounded(
		"recovery attempt reason",
		input.ReasonRedacted,
		4096,
	)
}

func validateRecordRecoveryDecision(input RecordRecoveryDecision) error {
	if input.TaskID.IsZero() || input.RunID.IsZero() {
		return errors.New("recovery decision task and run are required")
	}
	if !input.Actor.isValid() || !input.Action.isValid() {
		return errors.New("recovery decision actor or action is invalid")
	}
	for label, value := range map[string]string{
		"recovery decision ID":              input.ID,
		"recovery decision assessment ID":   input.AssessmentID,
		"recovery decision idempotency key": input.IdempotencyKey,
	} {
		if err := validateBounded(label, value, 255); err != nil {
			return err
		}
	}
	return validateBounded(
		"recovery decision reason",
		input.ReasonRedacted,
		4096,
	)
}

func findRecoveryAssessmentByIdempotency(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	idempotencyKey string,
) (RecoveryAssessmentRecord, bool, error) {
	record, err := scanRecoveryAssessment(queries.QueryRowContext(
		ctx,
		`SELECT id, task_id, run_id, checkpoint_id, classification,
		        findings_redacted_json, divergences_redacted_json,
		        observation_sha256, patch_available, patch_locator, patch_path,
		        idempotency_key, created_at_unix_micros
		 FROM checkpoint_recovery_assessments
		 WHERE task_id = ? AND idempotency_key = ?`,
		taskID,
		idempotencyKey,
	))
	if errors.Is(err, ErrNotFound) {
		return RecoveryAssessmentRecord{}, false, nil
	}
	return record, err == nil, err
}

func findRecoveryAttemptByIdempotency(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	idempotencyKey string,
) (RecoveryAttemptRecord, bool, error) {
	record, err := scanRecoveryAttempt(queries.QueryRowContext(
		ctx,
		`SELECT id, assessment_id, task_id, run_id, checkpoint_id,
		        action, outcome, reason_redacted, idempotency_key,
		        created_at_unix_micros
		 FROM checkpoint_recovery_attempts
		 WHERE task_id = ? AND idempotency_key = ?`,
		taskID,
		idempotencyKey,
	))
	if errors.Is(err, ErrNotFound) {
		return RecoveryAttemptRecord{}, false, nil
	}
	return record, err == nil, err
}

func findRecoveryDecisionByIdempotency(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	idempotencyKey string,
) (RecoveryDecisionRecord, bool, error) {
	record, err := scanRecoveryDecision(queries.QueryRowContext(
		ctx,
		`SELECT id, assessment_id, task_id, run_id, checkpoint_id,
		        actor, action, reason_redacted, idempotency_key,
		        created_at_unix_micros
		 FROM checkpoint_recovery_decisions
		 WHERE task_id = ? AND idempotency_key = ?`,
		taskID,
		idempotencyKey,
	))
	if errors.Is(err, ErrNotFound) {
		return RecoveryDecisionRecord{}, false, nil
	}
	return record, err == nil, err
}

func scanRecoveryAssessment(row rowScanner) (RecoveryAssessmentRecord, error) {
	var (
		record        RecoveryAssessmentRecord
		checkpointRaw sql.NullString
		patchLocator  sql.NullString
		patchPath     sql.NullString
		createdMicros int64
	)
	if err := row.Scan(
		&record.ID,
		&record.TaskID,
		&record.RunID,
		&checkpointRaw,
		&record.Classification,
		&record.FindingsJSON,
		&record.DivergencesJSON,
		&record.ObservationSHA256,
		&record.PatchAvailable,
		&patchLocator,
		&patchPath,
		&record.IdempotencyKey,
		&createdMicros,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RecoveryAssessmentRecord{}, typedError(
				ErrNotFound,
				"find recovery assessment",
				err,
			)
		}
		return RecoveryAssessmentRecord{}, classify("scan recovery assessment", err)
	}
	if patchLocator.Valid {
		record.PatchLocator = patchLocator.String
	}
	if patchPath.Valid {
		record.PatchPath = patchPath.String
	}
	record.CreatedAt = repositoryTime(createdMicros)
	if err := parseOptionalRecoveryCheckpoint(
		checkpointRaw,
		&record.CheckpointID,
	); err != nil {
		return RecoveryAssessmentRecord{}, err
	}
	return record, nil
}

func scanRecoveryAttempt(row rowScanner) (RecoveryAttemptRecord, error) {
	var record RecoveryAttemptRecord
	var checkpointRaw sql.NullString
	var createdMicros int64
	if err := row.Scan(
		&record.ID,
		&record.AssessmentID,
		&record.TaskID,
		&record.RunID,
		&checkpointRaw,
		&record.Action,
		&record.Outcome,
		&record.ReasonRedacted,
		&record.IdempotencyKey,
		&createdMicros,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RecoveryAttemptRecord{}, typedError(
				ErrNotFound,
				"find recovery attempt",
				err,
			)
		}
		return RecoveryAttemptRecord{}, classify("scan recovery attempt", err)
	}
	record.CreatedAt = repositoryTime(createdMicros)
	if err := parseOptionalRecoveryCheckpoint(
		checkpointRaw,
		&record.CheckpointID,
	); err != nil {
		return RecoveryAttemptRecord{}, err
	}
	return record, nil
}

func scanRecoveryDecision(row rowScanner) (RecoveryDecisionRecord, error) {
	var record RecoveryDecisionRecord
	var checkpointRaw sql.NullString
	var createdMicros int64
	if err := row.Scan(
		&record.ID,
		&record.AssessmentID,
		&record.TaskID,
		&record.RunID,
		&checkpointRaw,
		&record.Actor,
		&record.Action,
		&record.ReasonRedacted,
		&record.IdempotencyKey,
		&createdMicros,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RecoveryDecisionRecord{}, typedError(
				ErrNotFound,
				"find recovery decision",
				err,
			)
		}
		return RecoveryDecisionRecord{}, classify("scan recovery decision", err)
	}
	record.CreatedAt = repositoryTime(createdMicros)
	if err := parseOptionalRecoveryCheckpoint(
		checkpointRaw,
		&record.CheckpointID,
	); err != nil {
		return RecoveryDecisionRecord{}, err
	}
	return record, nil
}

func verifyRecoveryCheckpointBinding(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	runID domain.RunID,
	checkpointID *domain.CheckpointID,
) error {
	var count int
	if checkpointID == nil {
		if err := queries.QueryRowContext(
			ctx,
			`SELECT count(*)
			 FROM tasks AS task
			 JOIN runs AS run ON run.task_id = task.id
			 WHERE task.id = ? AND run.id = ?
			   AND NOT EXISTS (
			       SELECT 1
			       FROM checkpoints AS checkpoint
			       WHERE checkpoint.task_id = task.id
			         AND checkpoint.run_id = run.id
			         AND checkpoint.schema_version IS NOT NULL
			         AND checkpoint.state = 'ready'
			   )`,
			taskID,
			runID,
		).Scan(&count); err != nil {
			return classify("verify recovery run without checkpoint", err)
		}
		if count != 1 {
			return typedError(
				ErrConstraint,
				"verify recovery run without checkpoint",
				errors.New(
					"run is missing or already has a versioned checkpoint",
				),
			)
		}
		return nil
	}
	if err := queries.QueryRowContext(
		ctx,
		`SELECT count(*)
		 FROM checkpoints
		 WHERE id = ? AND task_id = ? AND run_id = ? AND state = 'ready'`,
		*checkpointID,
		taskID,
		runID,
	).Scan(&count); err != nil {
		return classify("verify recovery checkpoint binding", err)
	}
	if count != 1 {
		return typedError(
			ErrConstraint,
			"verify recovery checkpoint binding",
			errors.New("checkpoint does not belong to recovery task and run"),
		)
	}
	return nil
}

func verifyRecoveryAssessmentBinding(
	ctx context.Context,
	queries queryRower,
	assessmentID string,
	taskID domain.TaskID,
	runID domain.RunID,
	checkpointID *domain.CheckpointID,
) error {
	var count int
	if err := queries.QueryRowContext(
		ctx,
		`SELECT count(*)
		 FROM checkpoint_recovery_assessments
		 WHERE id = ? AND task_id = ? AND run_id = ? AND checkpoint_id IS ?`,
		assessmentID,
		taskID,
		runID,
		nullableRecoveryCheckpointID(checkpointID),
	).Scan(&count); err != nil {
		return classify("verify recovery assessment binding", err)
	}
	if count != 1 {
		return typedError(
			ErrConstraint,
			"verify recovery assessment binding",
			errors.New("assessment does not belong to recovery identities"),
		)
	}
	return nil
}

func sameRecoveryAssessment(
	left RecoveryAssessmentRecord,
	right RecoveryAssessmentRecord,
) bool {
	return left.ID == right.ID &&
		left.TaskID == right.TaskID &&
		left.RunID == right.RunID &&
		sameRecoveryCheckpointID(left.CheckpointID, right.CheckpointID) &&
		left.Classification == right.Classification &&
		left.FindingsJSON == right.FindingsJSON &&
		left.DivergencesJSON == right.DivergencesJSON &&
		left.ObservationSHA256 == right.ObservationSHA256 &&
		left.PatchAvailable == right.PatchAvailable &&
		left.PatchLocator == right.PatchLocator &&
		left.PatchPath == right.PatchPath &&
		left.IdempotencyKey == right.IdempotencyKey
}

func sameRecoveryAttempt(left, right RecoveryAttemptRecord) bool {
	return left.ID == right.ID &&
		left.AssessmentID == right.AssessmentID &&
		left.TaskID == right.TaskID &&
		left.RunID == right.RunID &&
		sameRecoveryCheckpointID(left.CheckpointID, right.CheckpointID) &&
		left.Action == right.Action &&
		left.Outcome == right.Outcome &&
		left.ReasonRedacted == right.ReasonRedacted &&
		left.IdempotencyKey == right.IdempotencyKey
}

func sameRecoveryDecision(left, right RecoveryDecisionRecord) bool {
	return left.ID == right.ID &&
		left.AssessmentID == right.AssessmentID &&
		left.TaskID == right.TaskID &&
		left.RunID == right.RunID &&
		sameRecoveryCheckpointID(left.CheckpointID, right.CheckpointID) &&
		left.Actor == right.Actor &&
		left.Action == right.Action &&
		left.ReasonRedacted == right.ReasonRedacted &&
		left.IdempotencyKey == right.IdempotencyKey
}

func nullableRecoveryPatchValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableRecoveryCheckpointID(value *domain.CheckpointID) any {
	if value == nil {
		return nil
	}
	return *value
}

func sameRecoveryCheckpointID(
	left *domain.CheckpointID,
	right *domain.CheckpointID,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func parseOptionalRecoveryCheckpoint(
	raw sql.NullString,
	target **domain.CheckpointID,
) error {
	if !raw.Valid {
		*target = nil
		return nil
	}
	parsed, err := domain.ParseCheckpointID(raw.String)
	if err != nil {
		return replayCorruption("recovery checkpoint identity is invalid")
	}
	*target = &parsed
	return nil
}

func (value RecoveryClassification) isValid() bool {
	switch value {
	case RecoveryClassificationSafeResume,
		RecoveryClassificationReconcile,
		RecoveryClassificationPatchOnly,
		RecoveryClassificationImpossible:
		return true
	default:
		return false
	}
}

func (value RecoveryAction) isValid() bool {
	switch value {
	case RecoveryActionResume,
		RecoveryActionReconcile,
		RecoveryActionPreservePatch,
		RecoveryActionAbandon:
		return true
	default:
		return false
	}
}

func (value RecoveryAttemptOutcome) isValid() bool {
	switch value {
	case RecoveryAttemptStarted,
		RecoveryAttemptSucceeded,
		RecoveryAttemptFailed,
		RecoveryAttemptCancelled,
		RecoveryAttemptOutcomeUnknown:
		return true
	default:
		return false
	}
}

func (value RecoveryDecisionActor) isValid() bool {
	switch value {
	case RecoveryDecisionActorUser, RecoveryDecisionActorSystem:
		return true
	default:
		return false
	}
}

var _ RecoveryOperations = (*Repositories)(nil)
