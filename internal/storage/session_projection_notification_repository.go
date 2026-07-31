package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

type sessionProjectionBackfillCandidate struct {
	TaskID   domain.TaskID
	Entity   string
	Revision uint64
	Source   string
}

// ReconcileAllSessionProjectionInvalidations repairs every session before a
// restarted coordinator begins serving requests.
func (repositories *Repositories) ReconcileAllSessionProjectionInvalidations(
	ctx context.Context,
	publisher CommittedEventPublisher,
) error {
	if publisher == nil {
		return errors.New("session projection reconciliation publisher is required")
	}
	rows, err := repositories.database.sql.QueryContext(ctx, "SELECT id FROM sessions ORDER BY id")
	if err != nil {
		return classify("list sessions for projection reconciliation", err)
	}
	var sessionIDs []domain.SessionID
	for rows.Next() {
		var sessionID domain.SessionID
		if err := rows.Scan(&sessionID); err != nil {
			rows.Close()
			return classify("scan session for projection reconciliation", err)
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return classify("iterate sessions for projection reconciliation", err)
	}
	if err := rows.Close(); err != nil {
		return classify("close sessions for projection reconciliation", err)
	}
	for _, sessionID := range sessionIDs {
		if err := repositories.ReconcileSessionProjectionInvalidations(ctx, sessionID, publisher); err != nil {
			return err
		}
	}
	return nil
}

// ReconcileSessionProjectionInvalidations closes the crash boundary between
// legacy normalized mutations and the post-commit session notification. Every
// candidate has a deterministic key, so repeated startup/reconnect scans emit
// at most one durable session sequence per normalized mutation.
func (repositories *Repositories) ReconcileSessionProjectionInvalidations(
	ctx context.Context,
	sessionID domain.SessionID,
	publisher CommittedEventPublisher,
) error {
	if sessionID.IsZero() || publisher == nil {
		return errors.New("session projection reconciliation dependencies are invalid")
	}
	rows, err := repositories.database.sql.QueryContext(ctx, `
		SELECT task_id, entity, entity_revision, source_identity
		FROM (
			SELECT event.task_id AS task_id, 'task' AS entity,
			       coalesce(
			           CAST(json_extract(event.payload_json, '$.task_revision') AS INTEGER),
			           CAST(json_extract(event.payload_json, '$.to_task_revision') AS INTEGER),
			           event.sequence
			       ) AS entity_revision,
			       'task-event:' || event.id AS source_identity
			FROM task_events AS event
			UNION ALL
			SELECT budget.task_id, 'budget', revision.revision,
			       'budget-limit:' || budget.id || ':' || revision.revision
			FROM budget_limit_revisions AS revision
			JOIN budgets AS budget ON budget.id = revision.budget_id
			UNION ALL
			SELECT approval.task_id, 'approval', approval.revision,
			       'approval:' || approval.id || ':' || approval.revision
			FROM approvals AS approval
			UNION ALL
			SELECT validation.task_id, 'validation', validation.revision,
			       'validation:' || validation.id || ':' || validation.revision
			FROM validations AS validation
			UNION ALL
			SELECT plan.task_id, 'plan', plan.revision,
			       'plan:' || plan.task_id || ':' || plan.revision
			FROM agent_plan_revisions AS plan
			UNION ALL
			SELECT checkpoint.task_id, 'checkpoint', checkpoint.revision,
			       'checkpoint:' || checkpoint.id || ':' || checkpoint.revision
			FROM checkpoints AS checkpoint
			UNION ALL
			SELECT attempt.task_id, 'recovery', 0,
			       'recovery-attempt:' || attempt.id
			FROM checkpoint_recovery_attempts AS attempt
		) AS candidate
		JOIN tasks AS task ON task.id = candidate.task_id
		JOIN sessions AS session ON session.thread_id = task.thread_id
		WHERE session.id = ?
		  AND NOT EXISTS (
		      SELECT 1 FROM session_projection_notifications AS notification
		      WHERE notification.task_id = candidate.task_id
		        AND notification.entity = candidate.entity
		        AND notification.entity_revision = candidate.entity_revision
		  )
		ORDER BY source_identity`, sessionID)
	if err != nil {
		return classify("list session projection reconciliation candidates", err)
	}
	var candidates []sessionProjectionBackfillCandidate
	for rows.Next() {
		var candidate sessionProjectionBackfillCandidate
		if err := rows.Scan(&candidate.TaskID, &candidate.Entity, &candidate.Revision, &candidate.Source); err != nil {
			rows.Close()
			return classify("scan session projection reconciliation candidate", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return classify("iterate session projection reconciliation candidates", err)
	}
	if err := rows.Close(); err != nil {
		return classify("close session projection reconciliation candidates", err)
	}
	for _, candidate := range candidates {
		digest := sha256.Sum256([]byte(candidate.Source))
		_, err := repositories.RecordAndPublishSessionProjectionInvalidation(ctx, RecordSessionProjectionInvalidation{
			TaskID: candidate.TaskID, Entity: candidate.Entity, EntityRevision: candidate.Revision,
			IdempotencyKey: fmt.Sprintf("projection-backfill-%x", digest[:]),
		}, publisher)
		if err != nil {
			return fmt.Errorf("reconcile session projection candidate: %w", err)
		}
	}
	return nil
}

type RecordSessionProjectionInvalidation struct {
	TaskID         domain.TaskID
	Entity         string
	EntityRevision uint64
	IdempotencyKey string
}

// RecordAndPublishSessionProjectionInvalidation durably appends exactly one
// ordered repair signal for a committed normalized task mutation. A retry
// republishes the same durable event and never allocates another sequence.
func (repositories *Repositories) RecordAndPublishSessionProjectionInvalidation(
	ctx context.Context,
	input RecordSessionProjectionInvalidation,
	publisher CommittedEventPublisher,
) (events.SessionEvent, error) {
	if input.TaskID.IsZero() || publisher == nil || strings.TrimSpace(input.Entity) == "" ||
		input.Entity != strings.TrimSpace(input.Entity) || len(input.Entity) > 64 {
		return events.SessionEvent{}, errors.New("session projection invalidation is invalid")
	}
	if err := validateBounded("session projection invalidation key", input.IdempotencyKey, 255); err != nil {
		return events.SessionEvent{}, err
	}
	var event events.SessionEvent
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var sessionID domain.SessionID
		var sequence uint64
		err := transaction.sql.QueryRowContext(ctx, `
			SELECT session_id, session_sequence
			FROM session_projection_notifications
			WHERE task_id = ? AND (
				idempotency_key = ? OR (entity = ? AND entity_revision = ?)
			)
			ORDER BY CASE WHEN idempotency_key = ? THEN 0 ELSE 1 END
			LIMIT 1`, input.TaskID, input.IdempotencyKey, input.Entity,
			input.EntityRevision, input.IdempotencyKey,
		).Scan(&sessionID, &sequence)
		if err == nil {
			row := transaction.sql.QueryRowContext(ctx, `
				SELECT sequence, thread_id, task_id, timestamp_unix_micros,
				       kind, entity_revision, causation_id, correlation_id,
				       payload_version, payload_json
				FROM session_events WHERE session_id = ? AND sequence = ?`, sessionID, sequence)
			var scanErr error
			event, scanErr = scanSessionEvent(row, sessionID)
			return scanErr
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return classify("find session projection invalidation", err)
		}
		var threadID domain.ThreadID
		if err := transaction.sql.QueryRowContext(ctx, `
			SELECT session.id, task.thread_id
			FROM tasks AS task
			JOIN sessions AS session ON session.thread_id = task.thread_id
			WHERE task.id = ?`, input.TaskID,
		).Scan(&sessionID, &threadID); errors.Is(err, sql.ErrNoRows) {
			return typedError(ErrNotFound, "bind session projection invalidation", err)
		} else if err != nil {
			return classify("bind session projection invalidation", err)
		}
		event, err = repositories.AppendSessionEvent(ctx, transaction, events.NewSessionEvent{
			SessionID: sessionID, ThreadID: threadID, TaskID: &input.TaskID,
			Kind: events.KindTaskProjectionInvalidated, Revision: input.EntityRevision,
			PayloadVersion: 1, Payload: events.Payload{TaskProjectionInvalidated: &events.TaskProjectionInvalidated{
				Entity: input.Entity, Revision: input.EntityRevision,
			}},
		})
		if err != nil {
			return err
		}
		_, err = transaction.sql.ExecContext(ctx, `
			INSERT INTO session_projection_notifications (
				task_id, session_id, session_sequence, entity, entity_revision,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?)`, input.TaskID, sessionID, event.Sequence,
			input.Entity, input.EntityRevision, input.IdempotencyKey, event.Timestamp.UnixMicro())
		if err != nil {
			return repositoryWriteError("record session projection invalidation", err)
		}
		return nil
	})
	if err != nil {
		return events.SessionEvent{}, err
	}
	if err := publisher.PublishCommitted(event); err != nil {
		return event, err
	}
	return event, nil
}
