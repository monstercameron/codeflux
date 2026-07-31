package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// TaskControlDisposition is the durable execution gate reconstructed from the
// task and its latest run.
type TaskControlDisposition string

const (
	TaskControlActive         TaskControlDisposition = "active"
	TaskControlPauseRequested TaskControlDisposition = "pause-requested"
	TaskControlPaused         TaskControlDisposition = "paused"
	TaskControlCancelled      TaskControlDisposition = "cancelled"
	TaskControlRecovery       TaskControlDisposition = "recovery-required"
)

// TaskControlSnapshot is the complete optimistic-control identity for one
// task and its latest run.
type TaskControlSnapshot struct {
	TaskID             domain.TaskID
	RunID              domain.RunID
	TaskState          domain.TaskState
	RunState           domain.RunState
	TaskRevision       uint64
	RunRevision        uint64
	RunAttempt         uint64
	Disposition        TaskControlDisposition
	PauseReason        domain.PauseReason
	CancellationReason domain.CancellationReason
	UpdatedAt          time.Time
}

// TaskControlReplayOperation identifies one externally retriable control RPC.
type TaskControlReplayOperation string

const (
	TaskControlReplayPause              TaskControlReplayOperation = "pause"
	TaskControlReplayResume             TaskControlReplayOperation = "resume"
	TaskControlReplayCancel             TaskControlReplayOperation = "cancel"
	TaskControlReplaySafeResumeRecovery TaskControlReplayOperation = "safe-resume-recovery"
)

// TaskControlReplayRequest is the canonical transport request identity used
// to resolve a completed mutation before optimistic revision validation.
type TaskControlReplayRequest struct {
	TaskID               domain.TaskID
	Operation            TaskControlReplayOperation
	ExpectedTaskRevision uint64
	ReasonRedacted       string
	IdempotencyKey       string
}

// TaskControlReplay is a durable completed RPC result. Blocked is set for a
// resume that was durably classified as requiring reconciliation.
type TaskControlReplay struct {
	Found        bool
	Blocked      bool
	Control      TaskControlSnapshot
	AssessmentID string
	CheckpointID *domain.CheckpointID
}

// RequestTaskPause durably closes the new-action gate before in-flight work is
// cancelled or allowed to finish.
type RequestTaskPause struct {
	EventID              domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	Reason               domain.PauseReason
	ReasonRedacted       string
	IdempotencyKey       string
}

// CompleteTaskPause records the checkpoint-backed paused state after the
// coordinator has applied the active-action policy.
type CompleteTaskPause struct {
	EventID              domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	Reason               domain.PauseReason
	ReasonRedacted       string
	CheckpointID         domain.CheckpointID
	IdempotencyKey       string
}

// CancelControlledTask records terminal cancellation without rewriting any
// already committed action result.
type CancelControlledTask struct {
	EventID              domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	Reason               domain.CancellationReason
	ReasonRedacted       string
	IdempotencyKey       string
}

// ResumeControlledTask starts a new execution attempt only after the
// coordinator has verified compatibility and classified paused edits.
type ResumeControlledTask struct {
	EventID              domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	NonOverlappingFiles  []string
	IdempotencyKey       string
}

// AuthorizeSafeResumeRecovery atomically binds a user recovery decision and
// started attempt to the exact safe-resume assessment, transitions recovery
// state to a paused execution boundary, and appends its durable event.
type AuthorizeSafeResumeRecovery struct {
	EventID              domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	AssessmentID         string
	CheckpointID         domain.CheckpointID
	ReasonRedacted       string
	IdempotencyKey       string
	Decision             RecordRecoveryDecision
	Started              RecordRecoveryAttempt
}

// CommitRecoveryReconciliation atomically records the successful terminal
// recovery attempt, transitions the exact recovery-required task/run to a
// paused boundary, and appends the revision-bound recovery event.
type CommitRecoveryReconciliation struct {
	EventID                domain.EventID
	TaskID                 domain.TaskID
	RunID                  domain.RunID
	ExpectedTaskRevision   uint64
	ExpectedRunRevision    uint64
	AssessmentID           string
	PreviousCheckpointID   domain.CheckpointID
	ReconciledCheckpointID domain.CheckpointID
	ReasonRedacted         string
	IdempotencyKey         string
	TerminalAttemptID      string
	TerminalIdempotencyKey string
}

// RecordTaskResumeBlocked persists why direct resume requires reconciliation
// or a new plan revision while leaving the paused state unchanged.
type RecordTaskResumeBlocked struct {
	EventID              domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	ReasonRedacted       string
	ChangedFiles         []string
	ConflictFiles        []string
	IdempotencyKey       string
}

// MarkTaskControlRecoveryRequired compensates a resume whose worker could not
// be reacquired after the durable resume transition.
type MarkTaskControlRecoveryRequired struct {
	EventID              domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	ReasonRedacted       string
	IdempotencyKey       string
}

// TaskControlOperations is the durable pause, cancel, and resume repository
// boundary.
type TaskControlOperations interface {
	ReadTaskControl(context.Context, domain.TaskID) (TaskControlSnapshot, error)
	RequestTaskPause(context.Context, RequestTaskPause) (TaskControlSnapshot, error)
	CompleteTaskPause(context.Context, CompleteTaskPause) (TaskControlSnapshot, error)
	CancelControlledTask(context.Context, CancelControlledTask) (TaskControlSnapshot, error)
	ResumeControlledTask(context.Context, ResumeControlledTask) (TaskControlSnapshot, error)
	AuthorizeSafeResumeRecovery(context.Context, AuthorizeSafeResumeRecovery) (TaskControlSnapshot, error)
	RecordTaskResumeBlocked(context.Context, RecordTaskResumeBlocked) (TaskEvent, error)
	MarkTaskControlRecoveryRequired(context.Context, MarkTaskControlRecoveryRequired) (TaskControlSnapshot, error)
	ReadTaskControlReplay(context.Context, TaskControlReplayRequest) (TaskControlReplay, error)
}

// ReadTaskControlReplay resolves a completed command by durable event key
// before callers compare a now-stale optimistic revision.
func (repositories *Repositories) ReadTaskControlReplay(
	ctx context.Context,
	input TaskControlReplayRequest,
) (TaskControlReplay, error) {
	if input.TaskID.IsZero() || input.ExpectedTaskRevision == 0 ||
		strings.TrimSpace(input.IdempotencyKey) != input.IdempotencyKey ||
		input.IdempotencyKey == "" {
		return TaskControlReplay{}, errors.New(
			"task control replay identity is invalid",
		)
	}
	var key, eventType string
	switch input.Operation {
	case TaskControlReplayPause:
		key, eventType = input.IdempotencyKey+"/requested",
			"task.pause-requested"
	case TaskControlReplayResume:
		return repositories.readResumeControlReplay(ctx, input)
	case TaskControlReplaySafeResumeRecovery:
		return repositories.readSafeResumeRecoveryReplay(ctx, input)
	case TaskControlReplayCancel:
		key, eventType = input.IdempotencyKey, "task.cancelled"
	default:
		return TaskControlReplay{}, errors.New(
			"task control replay operation is invalid",
		)
	}
	payload, found, err := repositories.readControlReplayEvent(
		ctx, input.TaskID, key, eventType,
	)
	if err != nil || !found {
		return TaskControlReplay{}, err
	}
	if err := validateTaskControlReplayPayload(
		payload,
		input.ExpectedTaskRevision,
		input.ReasonRedacted,
		input.Operation != TaskControlReplayResume,
	); err != nil {
		return TaskControlReplay{}, err
	}
	if input.Operation == TaskControlReplayPause {
		_, completed, err := repositories.readControlReplayEvent(
			ctx,
			input.TaskID,
			input.IdempotencyKey+"/paused",
			"task.paused",
		)
		if err != nil || !completed {
			return TaskControlReplay{}, err
		}
	}
	current, err := repositories.ReadTaskControl(ctx, input.TaskID)
	if err != nil {
		return TaskControlReplay{}, err
	}
	return TaskControlReplay{Found: true, Control: current}, nil
}

func (repositories *Repositories) readSafeResumeRecoveryReplay(
	ctx context.Context,
	input TaskControlReplayRequest,
) (TaskControlReplay, error) {
	payload, found, err := repositories.readControlReplayEvent(
		ctx, input.TaskID, input.IdempotencyKey+"/authorized",
		"task.recovery-safe-resume-authorized",
	)
	if err != nil || !found {
		return TaskControlReplay{}, err
	}
	var value struct {
		TaskRevision   uint64              `json:"task_revision"`
		ReasonRedacted string              `json:"reason_redacted"`
		AssessmentID   string              `json:"assessment_id"`
		CheckpointID   domain.CheckpointID `json:"checkpoint_id"`
	}
	if err := json.Unmarshal([]byte(payload), &value); err != nil ||
		value.TaskRevision != input.ExpectedTaskRevision ||
		value.ReasonRedacted != input.ReasonRedacted ||
		value.AssessmentID == "" || value.CheckpointID.IsZero() {
		return TaskControlReplay{}, typedError(
			ErrConflict, "read safe recovery resume replay",
			errors.New("safe recovery resume idempotency key was reused"),
		)
	}
	attempt, terminalFound, err := findRecoveryAttemptByIdempotency(
		ctx, repositories.database.sql, input.TaskID,
		input.IdempotencyKey+"/terminal",
	)
	if err != nil {
		return TaskControlReplay{}, err
	}
	if !terminalFound {
		return TaskControlReplay{}, typedError(
			ErrConflict, "read safe recovery resume replay",
			errors.New("safe recovery resume outcome is not yet durable"),
		)
	}
	if attempt.Outcome != RecoveryAttemptSucceeded ||
		attempt.AssessmentID != value.AssessmentID ||
		attempt.CheckpointID == nil || *attempt.CheckpointID != value.CheckpointID {
		return TaskControlReplay{}, typedError(
			ErrConflict, "read safe recovery resume replay",
			errors.New("safe recovery resume previously did not succeed"),
		)
	}
	current, err := repositories.ReadTaskControl(ctx, input.TaskID)
	if err != nil {
		return TaskControlReplay{}, err
	}
	checkpointID := value.CheckpointID
	return TaskControlReplay{
		Found: true, Control: current, AssessmentID: value.AssessmentID,
		CheckpointID: &checkpointID,
	}, nil
}

func (repositories *Repositories) readResumeControlReplay(
	ctx context.Context,
	input TaskControlReplayRequest,
) (TaskControlReplay, error) {
	for _, candidate := range []struct {
		suffix  string
		kind    string
		blocked bool
	}{
		{suffix: "/resumed", kind: "task.resumed"},
		{suffix: "/blocked", kind: "task.resume-blocked", blocked: true},
	} {
		payload, found, err := repositories.readControlReplayEvent(
			ctx,
			input.TaskID,
			input.IdempotencyKey+candidate.suffix,
			candidate.kind,
		)
		if err != nil {
			return TaskControlReplay{}, err
		}
		if !found {
			continue
		}
		if err := validateTaskControlReplayPayload(
			payload,
			input.ExpectedTaskRevision,
			"",
			false,
		); err != nil {
			return TaskControlReplay{}, err
		}
		current, err := repositories.ReadTaskControl(ctx, input.TaskID)
		if err != nil {
			return TaskControlReplay{}, err
		}
		return TaskControlReplay{
			Found: true, Blocked: candidate.blocked, Control: current,
		}, nil
	}
	return TaskControlReplay{}, nil
}

func (repositories *Repositories) readControlReplayEvent(
	ctx context.Context,
	taskID domain.TaskID,
	key string,
	eventType string,
) (string, bool, error) {
	var payload, observedType string
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT event_type, payload_json
		   FROM task_events
		  WHERE task_id = ? AND idempotency_key = ?`,
		taskID,
		key,
	).Scan(&observedType, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, classify("read task control replay", err)
	}
	if observedType != eventType {
		return "", false, typedError(
			ErrConflict,
			"read task control replay",
			errors.New("task control idempotency key was reused"),
		)
	}
	return payload, true, nil
}

func validateTaskControlReplayPayload(
	payload string,
	expectedTaskRevision uint64,
	reason string,
	checkReason bool,
) error {
	var value struct {
		TaskRevision   uint64 `json:"task_revision"`
		ReasonRedacted string `json:"reason_redacted"`
	}
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return typedError(
			ErrCorrupt,
			"read task control replay",
			errors.New("task control event payload is invalid"),
		)
	}
	if value.TaskRevision != expectedTaskRevision ||
		(checkReason && value.ReasonRedacted != reason) {
		return typedError(
			ErrConflict,
			"read task control replay",
			errors.New("task control idempotency key was reused"),
		)
	}
	return nil
}

func (repositories *Repositories) ReadTaskControl(
	ctx context.Context,
	taskID domain.TaskID,
) (TaskControlSnapshot, error) {
	if taskID.IsZero() {
		return TaskControlSnapshot{}, errors.New("task control task is required")
	}
	return scanTaskControl(repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT task.id, run.id, task.state, run.state,
		        task.revision, run.revision, run.attempt,
		        task.pause_reason, task.cancellation_reason,
		        task.updated_at_unix_micros
		   FROM tasks AS task
		   JOIN runs AS run ON run.task_id = task.id
		  WHERE task.id = ?
		  ORDER BY run.attempt DESC
		  LIMIT 1`,
		taskID,
	), "read task control")
}

func (repositories *Repositories) RequestTaskPause(
	ctx context.Context,
	input RequestTaskPause,
) (TaskControlSnapshot, error) {
	if err := validatePauseRequest(input); err != nil {
		return TaskControlSnapshot{}, err
	}
	payload := mustControlJSON(map[string]any{
		"reason": input.Reason, "reason_redacted": input.ReasonRedacted,
		"task_revision": input.ExpectedTaskRevision,
		"run_revision":  input.ExpectedRunRevision,
	})
	return repositories.mutateTaskControl(
		ctx,
		controlMutation{
			eventID: input.EventID, taskID: input.TaskID, runID: input.RunID,
			expectedTaskRevision: input.ExpectedTaskRevision,
			expectedRunRevision:  input.ExpectedRunRevision,
			eventType:            "task.pause-requested", payloadJSON: payload,
			idempotencyKey: input.IdempotencyKey,
			apply: func(
				ctx context.Context,
				transaction *Transaction,
				_ TaskControlSnapshot,
				micros int64,
			) error {
				result, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE runs
					    SET state = 'pausing', updated_at_unix_micros = ?,
					        revision = revision + 1
					  WHERE id = ? AND task_id = ? AND revision = ?
					    AND state IN ('running','validating')`,
					micros, input.RunID, input.TaskID,
					input.ExpectedRunRevision,
				)
				if err != nil {
					return repositoryWriteError("request task pause", err)
				}
				return requireControlAffected(result, "request task pause")
			},
		},
	)
}

func (repositories *Repositories) CompleteTaskPause(
	ctx context.Context,
	input CompleteTaskPause,
) (TaskControlSnapshot, error) {
	if err := validateCompletePause(input); err != nil {
		return TaskControlSnapshot{}, err
	}
	payload := mustControlJSON(map[string]any{
		"reason": input.Reason, "reason_redacted": input.ReasonRedacted,
		"checkpoint_id": input.CheckpointID,
	})
	return repositories.mutateTaskControl(
		ctx,
		controlMutation{
			eventID: input.EventID, taskID: input.TaskID, runID: input.RunID,
			expectedTaskRevision: input.ExpectedTaskRevision,
			expectedRunRevision:  input.ExpectedRunRevision,
			eventType:            "task.paused", payloadJSON: payload,
			idempotencyKey: input.IdempotencyKey,
			apply: func(
				ctx context.Context,
				transaction *Transaction,
				current TaskControlSnapshot,
				micros int64,
			) error {
				if current.RunState != domain.RunStatePausing {
					return typedError(
						ErrStaleRevision, "complete task pause",
						errors.New("run is not pause-requested"),
					)
				}
				runResult, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE runs
					    SET state = 'paused', updated_at_unix_micros = ?,
					        revision = revision + 1
					  WHERE id = ? AND task_id = ? AND revision = ?
					    AND state = 'pausing'`,
					micros, input.RunID, input.TaskID,
					input.ExpectedRunRevision,
				)
				if err != nil {
					return repositoryWriteError("complete paused run", err)
				}
				if err := requireControlAffected(
					runResult, "complete paused run",
				); err != nil {
					return err
				}
				taskResult, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE tasks
					    SET state = 'paused', pause_reason = ?,
					        updated_at_unix_micros = ?, revision = revision + 1
					  WHERE id = ? AND revision = ?
					    AND state IN ('running','validating')`,
					input.Reason, micros, input.TaskID,
					input.ExpectedTaskRevision,
				)
				if err != nil {
					return repositoryWriteError("complete paused task", err)
				}
				return requireControlAffected(taskResult, "complete paused task")
			},
		},
	)
}

func (repositories *Repositories) CancelControlledTask(
	ctx context.Context,
	input CancelControlledTask,
) (TaskControlSnapshot, error) {
	if err := validateCancelTask(input); err != nil {
		return TaskControlSnapshot{}, err
	}
	payload := mustControlJSON(map[string]any{
		"reason": input.Reason, "reason_redacted": input.ReasonRedacted,
		"task_revision": input.ExpectedTaskRevision,
		"run_revision":  input.ExpectedRunRevision,
	})
	return repositories.mutateTaskControl(
		ctx,
		controlMutation{
			eventID: input.EventID, taskID: input.TaskID, runID: input.RunID,
			expectedTaskRevision: input.ExpectedTaskRevision,
			expectedRunRevision:  input.ExpectedRunRevision,
			eventType:            "task.cancelled", payloadJSON: payload,
			idempotencyKey: input.IdempotencyKey,
			apply: func(
				ctx context.Context,
				transaction *Transaction,
				_ TaskControlSnapshot,
				micros int64,
			) error {
				runResult, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE runs
					    SET state = 'cancelled', updated_at_unix_micros = ?,
					        revision = revision + 1
					  WHERE id = ? AND task_id = ? AND revision = ?
					    AND state NOT IN ('completed','failed','cancelled')`,
					micros, input.RunID, input.TaskID,
					input.ExpectedRunRevision,
				)
				if err != nil {
					return repositoryWriteError("cancel controlled run", err)
				}
				if err := requireControlAffected(
					runResult, "cancel controlled run",
				); err != nil {
					return err
				}
				taskResult, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE tasks
					    SET state = 'cancelled', cancellation_reason = ?,
					        updated_at_unix_micros = ?, revision = revision + 1
					  WHERE id = ? AND revision = ?
					    AND state NOT IN (
					      'completed','cancelled','rolled-back'
					    )`,
					input.Reason, micros, input.TaskID,
					input.ExpectedTaskRevision,
				)
				if err != nil {
					return repositoryWriteError("cancel controlled task", err)
				}
				return requireControlAffected(taskResult, "cancel controlled task")
			},
		},
	)
}

func (repositories *Repositories) ResumeControlledTask(
	ctx context.Context,
	input ResumeControlledTask,
) (TaskControlSnapshot, error) {
	if err := validateResumeTask(input); err != nil {
		return TaskControlSnapshot{}, err
	}
	payload := mustControlJSON(map[string]any{
		"non_overlapping_files": input.NonOverlappingFiles,
		"task_revision":         input.ExpectedTaskRevision,
		"run_revision":          input.ExpectedRunRevision,
	})
	return repositories.mutateTaskControl(
		ctx,
		controlMutation{
			eventID: input.EventID, taskID: input.TaskID, runID: input.RunID,
			expectedTaskRevision: input.ExpectedTaskRevision,
			expectedRunRevision:  input.ExpectedRunRevision,
			eventType:            "task.resumed", payloadJSON: payload,
			idempotencyKey: input.IdempotencyKey,
			apply: func(
				ctx context.Context,
				transaction *Transaction,
				current TaskControlSnapshot,
				micros int64,
			) error {
				if current.TaskState != domain.TaskStatePaused ||
					current.RunState != domain.RunStatePaused {
					return typedError(
						ErrStaleRevision, "resume controlled task",
						errors.New("task and run are not paused"),
					)
				}
				runResult, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE runs
					    SET state = 'starting', updated_at_unix_micros = ?,
					        revision = revision + 1
					  WHERE id = ? AND task_id = ? AND revision = ?
					    AND state = 'paused'`,
					micros, input.RunID, input.TaskID,
					input.ExpectedRunRevision,
				)
				if err != nil {
					return repositoryWriteError("resume controlled run", err)
				}
				if err := requireControlAffected(
					runResult, "resume controlled run",
				); err != nil {
					return err
				}
				taskResult, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE tasks
					    SET state = 'running', pause_reason = NULL,
					        updated_at_unix_micros = ?, revision = revision + 1
					  WHERE id = ? AND revision = ? AND state = 'paused'`,
					micros, input.TaskID, input.ExpectedTaskRevision,
				)
				if err != nil {
					return repositoryWriteError("resume controlled task", err)
				}
				return requireControlAffected(taskResult, "resume controlled task")
			},
		},
	)
}

// CommitRecoveryReconciliation never reports success unless the new
// checkpoint, terminal attempt, state transition, and ordered task event all
// commit in the same SQLite transaction.
func (repositories *Repositories) CommitRecoveryReconciliation(
	ctx context.Context,
	input CommitRecoveryReconciliation,
) (TaskControlSnapshot, error) {
	if input.EventID.IsZero() || input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.PreviousCheckpointID.IsZero() || input.ReconciledCheckpointID.IsZero() {
		return TaskControlSnapshot{}, errors.New("recovery reconciliation identities are required")
	}
	for label, value := range map[string]string{
		"recovery reconciliation assessment":       input.AssessmentID,
		"recovery reconciliation reason":           input.ReasonRedacted,
		"recovery reconciliation idempotency key":  input.IdempotencyKey,
		"recovery reconciliation terminal attempt": input.TerminalAttemptID,
		"recovery reconciliation terminal key":     input.TerminalIdempotencyKey,
	} {
		if err := validateControlText(label, value, 2048); err != nil {
			return TaskControlSnapshot{}, err
		}
	}
	previous := input.PreviousCheckpointID
	terminal := RecordRecoveryAttempt{
		ID: input.TerminalAttemptID, AssessmentID: input.AssessmentID,
		TaskID: input.TaskID, RunID: input.RunID, CheckpointID: &previous,
		Action: RecoveryActionReconcile, Outcome: RecoveryAttemptSucceeded,
		ReasonRedacted: input.ReasonRedacted,
		IdempotencyKey: input.TerminalIdempotencyKey,
	}
	if err := validateRecordRecoveryAttempt(terminal); err != nil {
		return TaskControlSnapshot{}, err
	}
	payload := mustControlJSON(map[string]any{
		"assessment_id":            input.AssessmentID,
		"previous_checkpoint_id":   input.PreviousCheckpointID,
		"reconciled_checkpoint_id": input.ReconciledCheckpointID,
		"reason_redacted":          input.ReasonRedacted,
		"task_revision":            input.ExpectedTaskRevision,
		"run_revision":             input.ExpectedRunRevision,
	})
	return repositories.mutateTaskControl(ctx, controlMutation{
		eventID: input.EventID, taskID: input.TaskID, runID: input.RunID,
		expectedTaskRevision: input.ExpectedTaskRevision,
		expectedRunRevision:  input.ExpectedRunRevision,
		eventType:            "task.recovery-reconciled", payloadJSON: payload,
		idempotencyKey: input.IdempotencyKey,
		apply: func(
			ctx context.Context,
			transaction *Transaction,
			current TaskControlSnapshot,
			micros int64,
		) error {
			if current.TaskState != domain.TaskStateRecoveryRequired ||
				current.RunState != domain.RunStateRecoveryRequired {
				return typedError(
					ErrStaleRevision, "commit recovery reconciliation",
					errors.New("task and run are not recovery-required"),
				)
			}
			if err := verifyRecoveryCheckpointBinding(
				ctx, transaction.sql, input.TaskID, input.RunID,
				&input.ReconciledCheckpointID,
			); err != nil {
				return err
			}
			if err := verifyRecoveryAssessmentBinding(
				ctx, transaction.sql, input.AssessmentID, input.TaskID,
				input.RunID, &previous,
			); err != nil {
				return err
			}
			var decisionCount int
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT count(*) FROM checkpoint_recovery_decisions
				 WHERE assessment_id = ? AND task_id = ? AND run_id = ?
				   AND checkpoint_id = ? AND actor = 'user' AND action = 'reconcile'`,
				input.AssessmentID, input.TaskID, input.RunID,
				input.PreviousCheckpointID,
			).Scan(&decisionCount); err != nil {
				return classify("verify recovery reconciliation authority", err)
			}
			if decisionCount != 1 {
				return typedError(
					ErrConstraint, "verify recovery reconciliation authority",
					errors.New("durable reconcile decision is missing"),
				)
			}
			existing, found, err := findRecoveryAttemptByIdempotency(
				ctx, transaction.sql, input.TaskID, input.TerminalIdempotencyKey,
			)
			if err != nil {
				return err
			}
			if found {
				if !sameRecoveryAttempt(existing, recoveryAttemptRecord(terminal, micros)) {
					return typedError(ErrConflict, "commit recovery reconciliation", errors.New("terminal attempt identity was reused"))
				}
			} else if err := insertRecoveryAttempt(ctx, transaction.sql, terminal, micros); err != nil {
				return err
			}
			runResult, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE runs SET state = 'paused', updated_at_unix_micros = ?, revision = revision + 1
				 WHERE id = ? AND task_id = ? AND revision = ? AND state = 'recovery-required'`,
				micros, input.RunID, input.TaskID, input.ExpectedRunRevision,
			)
			if err != nil {
				return repositoryWriteError("pause reconciled run", err)
			}
			if err := requireControlAffected(runResult, "pause reconciled run"); err != nil {
				return err
			}
			taskResult, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE tasks SET state = 'paused', pause_reason = ?, invalidation_reason = NULL,
				 updated_at_unix_micros = ?, revision = revision + 1
				 WHERE id = ? AND revision = ? AND state = 'recovery-required'`,
				domain.PauseReasonRecoveryUncertain, micros,
				input.TaskID, input.ExpectedTaskRevision,
			)
			if err != nil {
				return repositoryWriteError("pause reconciled task", err)
			}
			return requireControlAffected(taskResult, "pause reconciled task")
		},
	})
}

// AuthorizeSafeResumeRecovery commits the authority boundary before the
// coordinator reacquires a worker. Its event is also the durable replay key
// for a response lost after a later successful terminal attempt.
func (repositories *Repositories) AuthorizeSafeResumeRecovery(
	ctx context.Context,
	input AuthorizeSafeResumeRecovery,
) (TaskControlSnapshot, error) {
	if input.EventID.IsZero() || input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.CheckpointID.IsZero() || input.AssessmentID == "" {
		return TaskControlSnapshot{}, errors.New("safe recovery resume identities are required")
	}
	if input.Decision.Action != RecoveryActionResume ||
		input.Started.Action != RecoveryActionResume ||
		input.Started.Outcome != RecoveryAttemptStarted ||
		input.Decision.AssessmentID != input.AssessmentID ||
		input.Started.AssessmentID != input.AssessmentID ||
		input.Decision.TaskID != input.TaskID || input.Started.TaskID != input.TaskID ||
		input.Decision.RunID != input.RunID || input.Started.RunID != input.RunID ||
		input.Decision.CheckpointID == nil || input.Started.CheckpointID == nil ||
		*input.Decision.CheckpointID != input.CheckpointID ||
		*input.Started.CheckpointID != input.CheckpointID {
		return TaskControlSnapshot{}, errors.New("safe recovery resume authority is inconsistent")
	}
	if err := validateRecordRecoveryDecision(input.Decision); err != nil {
		return TaskControlSnapshot{}, err
	}
	if err := validateRecordRecoveryAttempt(input.Started); err != nil {
		return TaskControlSnapshot{}, err
	}
	if err := validateControlText("safe recovery resume reason", input.ReasonRedacted, 2048); err != nil {
		return TaskControlSnapshot{}, err
	}
	payload := mustControlJSON(map[string]any{
		"assessment_id":   input.AssessmentID,
		"checkpoint_id":   input.CheckpointID,
		"reason_redacted": input.ReasonRedacted,
		"task_revision":   input.ExpectedTaskRevision,
		"run_revision":    input.ExpectedRunRevision,
	})
	return repositories.mutateTaskControl(ctx, controlMutation{
		eventID: input.EventID, taskID: input.TaskID, runID: input.RunID,
		expectedTaskRevision: input.ExpectedTaskRevision,
		expectedRunRevision:  input.ExpectedRunRevision,
		eventType:            "task.recovery-safe-resume-authorized", payloadJSON: payload,
		idempotencyKey: input.IdempotencyKey + "/authorized",
		apply: func(
			ctx context.Context,
			transaction *Transaction,
			current TaskControlSnapshot,
			micros int64,
		) error {
			if current.TaskState != domain.TaskStateRecoveryRequired ||
				current.RunState != domain.RunStateRecoveryRequired {
				return typedError(ErrStaleRevision, "authorize safe recovery resume", errors.New("task and run are not recovery-required"))
			}
			assessment, err := scanRecoveryAssessment(transaction.sql.QueryRowContext(
				ctx,
				`SELECT id, task_id, run_id, checkpoint_id, classification,
				        findings_redacted_json, divergences_redacted_json,
				        observation_sha256, patch_available, patch_locator,
				        patch_path, idempotency_key, created_at_unix_micros
				 FROM checkpoint_recovery_assessments WHERE id = ?`,
				input.AssessmentID,
			))
			if err != nil {
				return err
			}
			if assessment.TaskID != input.TaskID || assessment.RunID != input.RunID ||
				assessment.CheckpointID == nil || *assessment.CheckpointID != input.CheckpointID ||
				assessment.Classification != RecoveryClassificationSafeResume {
				return typedError(ErrConflict, "authorize safe recovery resume", errors.New("assessment is not the exact safe-resume assessment"))
			}
			decisionRecord := recoveryDecisionRecord(input.Decision, micros)
			existingDecision, decisionFound, err := findRecoveryDecisionByIdempotency(
				ctx, transaction.sql, input.TaskID, input.Decision.IdempotencyKey,
			)
			if err != nil {
				return err
			}
			startedRecord := recoveryAttemptRecord(input.Started, micros)
			existingStarted, startedFound, err := findRecoveryAttemptByIdempotency(
				ctx, transaction.sql, input.TaskID, input.Started.IdempotencyKey,
			)
			if err != nil {
				return err
			}
			if decisionFound || startedFound {
				if !decisionFound || !startedFound ||
					!sameRecoveryDecision(existingDecision, decisionRecord) ||
					!sameRecoveryAttempt(existingStarted, startedRecord) {
					return typedError(ErrConflict, "authorize safe recovery resume", errors.New("safe recovery resume identity was reused"))
				}
			} else {
				if err := insertRecoveryDecision(ctx, transaction.sql, input.Decision, micros); err != nil {
					return err
				}
				if err := insertRecoveryAttempt(ctx, transaction.sql, input.Started, micros); err != nil {
					return err
				}
			}
			runResult, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE runs SET state = 'paused', updated_at_unix_micros = ?, revision = revision + 1
				 WHERE id = ? AND task_id = ? AND revision = ? AND state = 'recovery-required'`,
				micros, input.RunID, input.TaskID, input.ExpectedRunRevision,
			)
			if err != nil {
				return repositoryWriteError("authorize safe recovery run", err)
			}
			if err := requireControlAffected(runResult, "authorize safe recovery run"); err != nil {
				return err
			}
			taskResult, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE tasks SET state = 'paused', pause_reason = ?, invalidation_reason = NULL,
				 updated_at_unix_micros = ?, revision = revision + 1
				 WHERE id = ? AND revision = ? AND state = 'recovery-required'`,
				domain.PauseReasonRecoveryUncertain, micros, input.TaskID, input.ExpectedTaskRevision,
			)
			if err != nil {
				return repositoryWriteError("authorize safe recovery task", err)
			}
			return requireControlAffected(taskResult, "authorize safe recovery task")
		},
	})
}

func (repositories *Repositories) RecordTaskResumeBlocked(
	ctx context.Context,
	input RecordTaskResumeBlocked,
) (TaskEvent, error) {
	if input.EventID.IsZero() || input.TaskID.IsZero() ||
		input.RunID.IsZero() {
		return TaskEvent{}, errors.New(
			"resume-blocked task, run, and event are required",
		)
	}
	if err := validateControlText(
		"resume-blocked reason", input.ReasonRedacted, 2048,
	); err != nil {
		return TaskEvent{}, err
	}
	if !normalizedControlPaths(input.ChangedFiles, 256) ||
		!normalizedControlPaths(input.ConflictFiles, 256) {
		return TaskEvent{}, errors.New(
			"resume-blocked changed files are not canonical",
		)
	}
	payload := mustControlJSON(map[string]any{
		"reason_redacted": input.ReasonRedacted,
		"changed_files":   input.ChangedFiles,
		"conflict_files":  input.ConflictFiles,
		"task_revision":   input.ExpectedTaskRevision,
		"run_revision":    input.ExpectedRunRevision,
	})
	return repositories.AppendTaskEvent(ctx, AppendTaskEvent{
		ID: input.EventID, TaskID: input.TaskID, RunID: &input.RunID,
		EventType: "task.resume-blocked", PayloadJSON: payload,
		IdempotencyKey: input.IdempotencyKey,
	})
}

func (repositories *Repositories) MarkTaskControlRecoveryRequired(
	ctx context.Context,
	input MarkTaskControlRecoveryRequired,
) (TaskControlSnapshot, error) {
	if input.EventID.IsZero() || input.TaskID.IsZero() ||
		input.RunID.IsZero() {
		return TaskControlSnapshot{}, errors.New(
			"task-control recovery identities are required",
		)
	}
	if err := validateControlText(
		"task-control recovery reason", input.ReasonRedacted, 2048,
	); err != nil {
		return TaskControlSnapshot{}, err
	}
	payload := mustControlJSON(map[string]any{
		"reason_redacted": input.ReasonRedacted,
	})
	return repositories.mutateTaskControl(
		ctx,
		controlMutation{
			eventID: input.EventID, taskID: input.TaskID, runID: input.RunID,
			expectedTaskRevision: input.ExpectedTaskRevision,
			expectedRunRevision:  input.ExpectedRunRevision,
			eventType:            "task.control-recovery-required",
			payloadJSON:          payload, idempotencyKey: input.IdempotencyKey,
			apply: func(
				ctx context.Context,
				transaction *Transaction,
				_ TaskControlSnapshot,
				micros int64,
			) error {
				runResult, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE runs
					    SET state = 'recovery-required',
					        updated_at_unix_micros = ?,
					        revision = revision + 1
					  WHERE id = ? AND task_id = ? AND revision = ?
					    AND state NOT IN (
					      'completed','failed','cancelled','recovery-required'
					    )`,
					micros, input.RunID, input.TaskID,
					input.ExpectedRunRevision,
				)
				if err != nil {
					return repositoryWriteError(
						"mark control run recovery required", err,
					)
				}
				if err := requireControlAffected(
					runResult, "mark control run recovery required",
				); err != nil {
					return err
				}
				taskResult, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE tasks
					    SET state = 'recovery-required',
					        invalidation_reason = ?,
					        updated_at_unix_micros = ?,
					        revision = revision + 1
					  WHERE id = ? AND revision = ?
					    AND state NOT IN (
					      'completed','cancelled','rolled-back',
					      'recovery-required'
					    )`,
					input.ReasonRedacted, micros, input.TaskID,
					input.ExpectedTaskRevision,
				)
				if err != nil {
					return repositoryWriteError(
						"mark control task recovery required", err,
					)
				}
				return requireControlAffected(
					taskResult, "mark control task recovery required",
				)
			},
		},
	)
}

type controlMutation struct {
	eventID              domain.EventID
	taskID               domain.TaskID
	runID                domain.RunID
	expectedTaskRevision uint64
	expectedRunRevision  uint64
	eventType            string
	payloadJSON          string
	idempotencyKey       string
	apply                func(
		context.Context,
		*Transaction,
		TaskControlSnapshot,
		int64,
	) error
}

func (repositories *Repositories) mutateTaskControl(
	ctx context.Context,
	mutation controlMutation,
) (TaskControlSnapshot, error) {
	if err := validateControlText(
		"task control idempotency key", mutation.idempotencyKey, 255,
	); err != nil {
		return TaskControlSnapshot{}, err
	}
	var snapshot TaskControlSnapshot
	err := repositories.database.RunInTransaction(
		ctx,
		func(transaction *Transaction) error {
			existing, found, err := findTaskEventByIdempotency(
				ctx, transaction, mutation.taskID,
				mutation.idempotencyKey,
			)
			if err != nil {
				return err
			}
			if found {
				if existing.EventType != mutation.eventType ||
					existing.PayloadJSON != mutation.payloadJSON ||
					existing.RunID == nil ||
					*existing.RunID != mutation.runID {
					return typedError(
						ErrConflict, "retry task control",
						errors.New(
							"idempotency key belongs to another control",
						),
					)
				}
				snapshot, err = scanTaskControl(
					transaction.sql.QueryRowContext(
						ctx,
						taskControlSelect+
							` WHERE task.id = ? AND run.id = ?`,
						mutation.taskID, mutation.runID,
					),
					"read idempotent task control",
				)
				return err
			}
			current, err := scanTaskControl(
				transaction.sql.QueryRowContext(
					ctx,
					taskControlSelect+
						` WHERE task.id = ? AND run.id = ?`,
					mutation.taskID, mutation.runID,
				),
				"read task control for mutation",
			)
			if err != nil {
				return err
			}
			if current.TaskRevision != mutation.expectedTaskRevision ||
				current.RunRevision != mutation.expectedRunRevision {
				return typedError(
					ErrStaleRevision, "mutate task control",
					errors.New("task or run revision changed"),
				)
			}
			now, micros := repositories.timestamp()
			if err := mutation.apply(
				ctx, transaction, current, micros,
			); err != nil {
				return err
			}
			if _, err := appendTaskEventTransaction(
				ctx,
				transaction,
				AppendTaskEvent{
					ID: mutation.eventID, TaskID: mutation.taskID,
					RunID: &mutation.runID, EventType: mutation.eventType,
					PayloadJSON:    mutation.payloadJSON,
					IdempotencyKey: mutation.idempotencyKey,
				},
				now,
				micros,
			); err != nil {
				return err
			}
			snapshot, err = scanTaskControl(
				transaction.sql.QueryRowContext(
					ctx,
					taskControlSelect+
						` WHERE task.id = ? AND run.id = ?`,
					mutation.taskID, mutation.runID,
				),
				"read mutated task control",
			)
			return err
		},
	)
	return snapshot, err
}

const taskControlSelect = `SELECT task.id, run.id, task.state, run.state,
       task.revision, run.revision, run.attempt,
       task.pause_reason, task.cancellation_reason,
       task.updated_at_unix_micros
  FROM tasks AS task
  JOIN runs AS run ON run.task_id = task.id`

func scanTaskControl(
	row rowScanner,
	operation string,
) (TaskControlSnapshot, error) {
	var (
		value              TaskControlSnapshot
		pauseReason        sql.NullString
		cancellationReason sql.NullString
		updatedMicros      int64
	)
	if err := row.Scan(
		&value.TaskID, &value.RunID,
		&value.TaskState, &value.RunState,
		&value.TaskRevision, &value.RunRevision, &value.RunAttempt,
		&pauseReason, &cancellationReason, &updatedMicros,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskControlSnapshot{},
				typedError(ErrNotFound, operation, err)
		}
		return TaskControlSnapshot{}, classify(operation, err)
	}
	if pauseReason.Valid {
		value.PauseReason = domain.PauseReason(pauseReason.String)
	}
	if cancellationReason.Valid {
		value.CancellationReason =
			domain.CancellationReason(cancellationReason.String)
	}
	value.UpdatedAt = repositoryTime(updatedMicros)
	value.Disposition = taskControlDisposition(value)
	return value, nil
}

func taskControlDisposition(
	value TaskControlSnapshot,
) TaskControlDisposition {
	switch {
	case value.TaskState == domain.TaskStateCancelled ||
		value.RunState == domain.RunStateCancelled:
		return TaskControlCancelled
	case value.TaskState == domain.TaskStateRecoveryRequired ||
		value.RunState == domain.RunStateRecoveryRequired:
		return TaskControlRecovery
	case value.RunState == domain.RunStatePausing:
		return TaskControlPauseRequested
	case value.TaskState == domain.TaskStatePaused ||
		value.RunState == domain.RunStatePaused:
		return TaskControlPaused
	default:
		return TaskControlActive
	}
}

func validatePauseRequest(input RequestTaskPause) error {
	if input.EventID.IsZero() || input.TaskID.IsZero() ||
		input.RunID.IsZero() || !input.Reason.IsValid() {
		return errors.New("pause request identities and reason are required")
	}
	if err := validateControlText(
		"pause reason", input.ReasonRedacted, 2048,
	); err != nil {
		return err
	}
	return validateControlText(
		"pause idempotency key", input.IdempotencyKey, 255,
	)
}

func validateCompletePause(input CompleteTaskPause) error {
	if err := validatePauseRequest(RequestTaskPause{
		EventID: input.EventID, TaskID: input.TaskID, RunID: input.RunID,
		ExpectedTaskRevision: input.ExpectedTaskRevision,
		ExpectedRunRevision:  input.ExpectedRunRevision,
		Reason:               input.Reason, ReasonRedacted: input.ReasonRedacted,
		IdempotencyKey: input.IdempotencyKey,
	}); err != nil {
		return err
	}
	if input.CheckpointID.IsZero() {
		return errors.New("pause checkpoint is required")
	}
	return nil
}

func validateCancelTask(input CancelControlledTask) error {
	if input.EventID.IsZero() || input.TaskID.IsZero() ||
		input.RunID.IsZero() || !input.Reason.IsValid() {
		return errors.New(
			"cancel request identities and reason are required",
		)
	}
	if err := validateControlText(
		"cancellation reason", input.ReasonRedacted, 2048,
	); err != nil {
		return err
	}
	return validateControlText(
		"cancel idempotency key", input.IdempotencyKey, 255,
	)
}

func validateResumeTask(input ResumeControlledTask) error {
	if input.EventID.IsZero() || input.TaskID.IsZero() ||
		input.RunID.IsZero() {
		return errors.New("resume request identities are required")
	}
	if !normalizedControlPaths(input.NonOverlappingFiles, 256) {
		return errors.New("resume non-overlapping files are not canonical")
	}
	return validateControlText(
		"resume idempotency key", input.IdempotencyKey, 255,
	)
}

func validateControlText(label, value string, maximum int) error {
	if strings.TrimSpace(value) != value || value == "" ||
		len(value) > maximum || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s is invalid", label)
	}
	return nil
}

func normalizedControlPaths(values []string, maximum int) bool {
	if len(values) > maximum {
		return false
	}
	previous := ""
	for index, value := range values {
		if value == "" || strings.TrimSpace(value) != value ||
			strings.Contains(value, "\\") ||
			strings.HasPrefix(value, "/") ||
			strings.Contains(value, "/../") ||
			value == ".." || strings.HasPrefix(value, "../") ||
			(index > 0 && value <= previous) {
			return false
		}
		previous = value
	}
	return true
}

func requireControlAffected(
	result sql.Result,
	operation string,
) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return typedError(
			ErrStaleRevision, operation,
			errors.New("task control state or revision changed"),
		)
	}
	return nil
}

func mustControlJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

var _ TaskControlOperations = (*Repositories)(nil)
