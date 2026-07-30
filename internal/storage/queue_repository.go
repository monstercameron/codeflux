package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// TaskQueueState is the durable lifecycle of one scheduler request.
type TaskQueueState string

const (
	TaskQueueStateQueued     TaskQueueState = "queued"
	TaskQueueStateDispatched TaskQueueState = "dispatched"
	TaskQueueStateCancelled  TaskQueueState = "cancelled"
	maximumTaskQueuePageSize                = 1001
)

// TaskQueueEntry is one durable visible scheduler position.
type TaskQueueEntry struct {
	ID              string
	TaskID          domain.TaskID
	ProviderKey     string
	State           TaskQueueState
	Reason          string
	Priority        int
	Resuming        bool
	EnqueueSequence uint64
	EnqueuedAt      time.Time
	DispatchedAt    *time.Time
	Revision        uint64
}

type EnqueueTask struct {
	ID              string
	TaskID          domain.TaskID
	ProviderKey     string
	Reason          string
	Priority        int
	Resuming        bool
	EnqueueSequence uint64
}

type RecoverDispatchedWorkerStart struct {
	QueueID string
	TaskID  domain.TaskID
	RunID   domain.RunID
	Reason  string
}

func (repositories *Repositories) EnqueueTask(
	ctx context.Context,
	input EnqueueTask,
) (TaskQueueEntry, error) {
	if input.TaskID.IsZero() || input.EnqueueSequence == 0 ||
		input.Priority < 0 || input.Priority > 1000 {
		return TaskQueueEntry{}, errors.New("queued task identity, priority, and sequence are invalid")
	}
	for label, value := range map[string]string{
		"queue entry ID": input.ID, "queue provider": input.ProviderKey,
		"queue reason": input.Reason,
	} {
		maximum := 255
		if label == "queue reason" {
			maximum = 2048
		}
		if err := validateBounded(label, value, maximum); err != nil {
			return TaskQueueEntry{}, err
		}
	}
	now, micros := repositories.timestamp()
	_, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO task_queue_entries (
			id, task_id, provider_key, state, reason, priority,
			resuming, enqueue_sequence, enqueued_at_unix_micros
		) VALUES (?, ?, ?, 'queued', ?, ?, ?, ?, ?)`,
		input.ID, input.TaskID, input.ProviderKey, input.Reason,
		input.Priority, input.Resuming, input.EnqueueSequence, micros,
	)
	if err != nil {
		return TaskQueueEntry{}, repositoryWriteError("enqueue task", err)
	}
	return TaskQueueEntry{
		ID: input.ID, TaskID: input.TaskID, ProviderKey: input.ProviderKey,
		State:  TaskQueueStateQueued,
		Reason: input.Reason, Priority: input.Priority,
		Resuming:        input.Resuming,
		EnqueueSequence: input.EnqueueSequence, EnqueuedAt: now,
	}, nil
}

func (repositories *Repositories) ListQueuedTasks(
	ctx context.Context,
	limit int,
) ([]TaskQueueEntry, error) {
	if limit < 1 || limit > maximumTaskQueuePageSize {
		return nil, errors.New("queue page limit is outside supported bounds")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, task_id, provider_key, state, reason, priority, resuming,
		        enqueue_sequence, enqueued_at_unix_micros,
		        dispatched_at_unix_micros, revision
		 FROM task_queue_entries WHERE state = 'queued'
		 ORDER BY resuming DESC,
		          CASE WHEN resuming = 1 THEN 0 ELSE priority END DESC,
		          enqueue_sequence, id LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, classify("list queued tasks", err)
	}
	defer rows.Close()
	var entries []TaskQueueEntry
	for rows.Next() {
		entry, err := scanTaskQueueEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate queued tasks", err)
	}
	return entries, nil
}

func (repositories *Repositories) TransitionQueuedTask(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	to TaskQueueState,
) (TaskQueueEntry, error) {
	if err := validateBounded("queue entry ID", id, 255); err != nil {
		return TaskQueueEntry{}, err
	}
	if to != TaskQueueStateDispatched && to != TaskQueueStateCancelled {
		return TaskQueueEntry{}, errors.New("queue transition must dispatch or cancel")
	}
	_, micros := repositories.timestamp()
	dispatched := any(nil)
	if to == TaskQueueStateDispatched {
		dispatched = micros
	}
	return transitionTaskQueueEntry(
		ctx, repositories.database.sql, id, expectedRevision, to, dispatched,
	)
}

func (repositories *Repositories) RecoverDispatchedWorkerStart(
	ctx context.Context,
	input RecoverDispatchedWorkerStart,
) error {
	if input.TaskID.IsZero() || input.RunID.IsZero() {
		return errors.New("worker start recovery task and run are required")
	}
	if err := validateBounded("queue entry ID", input.QueueID, 255); err != nil {
		return err
	}
	if err := validateBounded("worker start recovery reason", input.Reason, 2048); err != nil {
		return err
	}
	_, micros := repositories.timestamp()
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if err := verifyRunBelongsToTask(
			ctx, transaction, input.RunID, input.TaskID,
			"recover dispatched worker start",
		); err != nil {
			return err
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE task_queue_entries
			 SET state = 'cancelled', reason = ?, revision = revision + 1
			 WHERE id = ? AND task_id = ? AND state = 'dispatched'`,
			input.Reason, input.QueueID, input.TaskID,
		)
		if err != nil {
			return repositoryWriteError("cancel failed worker dispatch", err)
		}
		if err := requireOneAffected(result, "cancel failed worker dispatch"); err != nil {
			return err
		}
		result, err = transaction.sql.ExecContext(
			ctx,
			`UPDATE runs
			 SET state = 'recovery-required',
			     updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND task_id = ?
			   AND state NOT IN ('completed','failed','cancelled','recovery-required')`,
			micros, input.RunID, input.TaskID,
		)
		if err != nil {
			return repositoryWriteError("mark failed worker run recovery required", err)
		}
		if err := requireOneAffected(result, "mark failed worker run recovery required"); err != nil {
			return err
		}
		result, err = transaction.sql.ExecContext(
			ctx,
			`UPDATE tasks
			 SET state = 'recovery-required', invalidation_reason = ?,
			     updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ?
			   AND state NOT IN (
			     'completed','cancelled','rolled-back','recovery-required'
			   )`,
			input.Reason, micros, input.TaskID,
		)
		if err != nil {
			return repositoryWriteError("mark failed worker task recovery required", err)
		}
		return requireOneAffected(result, "mark failed worker task recovery required")
	})
}

func transitionTaskQueueEntry(
	ctx context.Context,
	queryer rowQueryer,
	id string,
	expectedRevision uint64,
	to TaskQueueState,
	dispatched any,
) (TaskQueueEntry, error) {
	entry, err := scanTaskQueueEntry(queryer.QueryRowContext(
		ctx,
		`UPDATE task_queue_entries SET state = ?,
			dispatched_at_unix_micros = ?, revision = revision + 1
		 WHERE id = ? AND state = 'queued' AND revision = ?
		 RETURNING id, task_id, provider_key, state, reason, priority,
		           resuming, enqueue_sequence, enqueued_at_unix_micros,
		           dispatched_at_unix_micros, revision`,
		to, dispatched, id, expectedRevision,
	))
	if errors.Is(err, ErrNotFound) {
		return TaskQueueEntry{}, typedError(
			ErrStaleRevision, "transition queued task",
			errors.New("queue entry changed"),
		)
	}
	if err != nil {
		return TaskQueueEntry{}, repositoryWriteError("transition queued task", err)
	}
	return entry, nil
}

func scanTaskQueueEntry(row rowScanner) (TaskQueueEntry, error) {
	var entry TaskQueueEntry
	var enqueued int64
	var dispatched sql.NullInt64
	err := row.Scan(
		&entry.ID, &entry.TaskID, &entry.ProviderKey, &entry.State,
		&entry.Reason, &entry.Priority, &entry.Resuming, &entry.EnqueueSequence,
		&enqueued, &dispatched, &entry.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskQueueEntry{}, typedError(ErrNotFound, "get queued task", err)
	}
	if err != nil {
		return TaskQueueEntry{}, classify("scan queued task", err)
	}
	entry.EnqueuedAt = repositoryTime(enqueued)
	entry.DispatchedAt = nullTimePointer(dispatched)
	return entry, nil
}
