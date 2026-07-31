package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

// SessionProjectionSnapshot is one transactionally consistent correctness
// base for the browser projection. Optional entity projections are absent only
// when no corresponding durable fact exists through ThroughSequence.
type SessionProjectionSnapshot struct {
	SessionID       domain.SessionID
	ThreadID        domain.ThreadID
	ThroughSequence uint64
	ObservedAt      time.Time
	Task            *Task
	Plan            *events.Plan
	PlanApproval    domain.ApprovalRequestState
	PendingApproval *Approval
	Budget          *BudgetSnapshot
	Tool            *events.Tool
	ToolRevision    uint64
	Validation      *events.Validation
	ValidationRev   uint64
	Checkpoint      *events.Checkpoint
	CheckpointRev   uint64
	CheckpointAt    time.Time
	Recovery        *events.RecoveryRequired
	RecoveryRev     uint64
	Acceptance      *events.ChangeAcceptance
	AcceptanceRev   uint64
	ReviewBindings  *events.RevisionBindings
	ReviewRev       uint64
	GraphRevision   uint64
}

// ReadSessionProjectionSnapshot observes the session cursor and every included
// task entity in one SQLite read transaction. This prevents a GetTask-style
// query result from being paired with an unrelated stream cursor.
func (repositories *Repositories) ReadSessionProjectionSnapshot(
	ctx context.Context,
	sessionID domain.SessionID,
) (SessionProjectionSnapshot, error) {
	if repositories == nil || repositories.database == nil {
		return SessionProjectionSnapshot{}, errors.New("repositories are unavailable")
	}
	if sessionID.IsZero() {
		return SessionProjectionSnapshot{}, errors.New("session ID must not be empty")
	}
	transaction, err := repositories.database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SessionProjectionSnapshot{}, classify("begin session projection snapshot", err)
	}
	defer transaction.Rollback()

	result := SessionProjectionSnapshot{SessionID: sessionID, ObservedAt: repositories.now().UTC()}
	if err := transaction.QueryRowContext(ctx, `
		SELECT thread_id, current_sequence
		FROM sessions WHERE id = ?`, sessionID,
	).Scan(&result.ThreadID, &result.ThroughSequence); errors.Is(err, sql.ErrNoRows) {
		return SessionProjectionSnapshot{}, typedError(ErrNotFound, "read session projection identity", err)
	} else if err != nil {
		return SessionProjectionSnapshot{}, classify("read session projection identity", err)
	}

	task, taskErr := scanTask(transaction.QueryRowContext(ctx, `
		SELECT id, thread_id, repository_id, request_message_id, state,
		       policy_preset, reasoning_effort, risk_level, required_assurance,
		       created_at_unix_micros, updated_at_unix_micros, revision
		FROM tasks WHERE thread_id = ?
		ORDER BY updated_at_unix_micros DESC, id DESC LIMIT 1`, result.ThreadID,
	), "read session projection task")
	if taskErr != nil && !errors.Is(taskErr, ErrNotFound) {
		return SessionProjectionSnapshot{}, taskErr
	}
	if taskErr == nil {
		result.Task = &task
		if err := repositories.readSessionProjectionEntities(ctx, transaction, &result); err != nil {
			return SessionProjectionSnapshot{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return SessionProjectionSnapshot{}, classify("commit session projection snapshot", err)
	}
	return result, nil
}

func (repositories *Repositories) readSessionProjectionEntities(
	ctx context.Context,
	transaction *sql.Tx,
	result *SessionProjectionSnapshot,
) error {
	taskID := result.Task.ID
	var budgetIDRaw string
	if err := transaction.QueryRowContext(ctx, "SELECT id FROM budgets WHERE task_id = ?", taskID).Scan(&budgetIDRaw); err == nil {
		budgetID, parseErr := domain.ParseBudgetID(budgetIDRaw)
		if parseErr != nil {
			return typedError(ErrCorrupt, "read session projection budget identity", parseErr)
		}
		budget, budgetErr := computeBudgetSnapshot(ctx, transaction, budgetID)
		if budgetErr != nil {
			return budgetErr
		}
		result.Budget = &budget
	} else if !errors.Is(err, sql.ErrNoRows) {
		return classify("read session projection budget identity", err)
	}

	var plan events.Plan
	var approvalRequired bool
	if err := transaction.QueryRowContext(ctx, `
		SELECT revision, user_summary, approval_required
		FROM agent_plan_revisions WHERE task_id = ?
		ORDER BY revision DESC LIMIT 1`, taskID,
	).Scan(&plan.Revision, &plan.RedactedSummary, &approvalRequired); err == nil {
		result.Plan = &plan
		result.PlanApproval = domain.ApprovalRequestStateGranted
		if approvalRequired {
			result.PlanApproval = domain.ApprovalRequestStatePending
			_ = transaction.QueryRowContext(ctx, `
				SELECT approval.state
				FROM agent_plan_approval_bindings AS binding
				JOIN approvals AS approval ON approval.id = binding.approval_id
				WHERE binding.task_id = ? AND binding.plan_revision = ?`, taskID, plan.Revision,
			).Scan(&result.PlanApproval)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return classify("read session projection plan", err)
	}

	approval := Approval{}
	var runIDRaw, resolution sql.NullString
	var decided, expires sql.NullInt64
	var requestedMicros int64
	if err := transaction.QueryRowContext(ctx, `
		SELECT id, task_id, run_id, state, scope, request_reason,
		       resolution_reason, idempotency_key, requested_at_unix_micros,
		       decided_at_unix_micros, expires_at_unix_micros, revision
		FROM approvals WHERE task_id = ? AND state = 'pending'
		ORDER BY requested_at_unix_micros DESC, id DESC LIMIT 1`, taskID,
	).Scan(&approval.ID, &approval.TaskID, &runIDRaw, &approval.State, &approval.Scope,
		&approval.RequestReason, &resolution, &approval.IdempotencyKey, &requestedMicros,
		&decided, &expires, &approval.Revision); err == nil {
		approval.RequestedAt = repositoryTime(requestedMicros)
		if runIDRaw.Valid {
			runID, parseErr := domain.ParseRunID(runIDRaw.String)
			if parseErr != nil {
				return typedError(ErrCorrupt, "read session projection approval run", parseErr)
			}
			approval.RunID = &runID
		}
		if resolution.Valid {
			approval.ResolutionReason = &resolution.String
		}
		result.PendingApproval = &approval
	} else if !errors.Is(err, sql.ErrNoRows) {
		return classify("read session projection approval", err)
	}

	rows, err := transaction.QueryContext(ctx, `
		SELECT sequence, thread_id, task_id, timestamp_unix_micros,
		       kind, entity_revision, causation_id, correlation_id,
		       payload_version, payload_json
		FROM session_events
		WHERE session_id = ? AND task_id = ? AND sequence <= ?
		ORDER BY sequence`, result.SessionID, taskID, result.ThroughSequence)
	if err != nil {
		return classify("read session projection events", err)
	}
	defer rows.Close()
	for rows.Next() {
		event, scanErr := scanSessionEvent(rows, result.SessionID)
		if scanErr != nil {
			return scanErr
		}
		switch event.Kind {
		case events.KindToolStarted, events.KindToolProgress, events.KindToolCompleted:
			value := *event.Payload.Tool
			result.Tool, result.ToolRevision = &value, event.Revision
		case events.KindValidationUpdated:
			value := *event.Payload.Validation
			result.Validation, result.ValidationRev = &value, event.Revision
		case events.KindCheckpointCreated:
			value := *event.Payload.Checkpoint
			result.Checkpoint, result.CheckpointRev, result.CheckpointAt = &value, event.Revision, event.Timestamp
		case events.KindRecoveryRequired:
			value := *event.Payload.RecoveryRequired
			result.Recovery, result.RecoveryRev = &value, event.Revision
		case events.KindChangeAcceptanceUpdated:
			value := *event.Payload.ChangeAcceptance
			result.Acceptance, result.AcceptanceRev = &value, event.Revision
			// A change-acceptance event opens or advances the review of one
			// exact diff/plan/validation/evidence/graph bundle. The same durable
			// binding is therefore the authoritative review-staleness base.
			bindings := value.Bindings
			result.ReviewBindings, result.ReviewRev = &bindings, event.Revision
		case events.KindGraphSnapshot, events.KindGraphPatch:
			result.GraphRevision = event.Revision
		}
	}
	if err := rows.Err(); err != nil {
		return classify("iterate session projection events", err)
	}
	return nil
}
