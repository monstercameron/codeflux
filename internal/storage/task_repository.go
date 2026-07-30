package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func (repositories *Repositories) CreateTask(
	ctx context.Context,
	input CreateTask,
) (Task, error) {
	switch {
	case input.ID.IsZero():
		return Task{}, errors.New("task ID must not be empty")
	case input.ThreadID.IsZero():
		return Task{}, errors.New("thread ID must not be empty")
	case input.RepositoryID.IsZero():
		return Task{}, errors.New("repository ID must not be empty")
	case !input.PolicyPreset.IsValid():
		return Task{}, errors.New("policy preset is invalid")
	case !input.ReasoningEffort.IsValid():
		return Task{}, errors.New("reasoning effort is invalid")
	case !input.RiskLevel.IsValid():
		return Task{}, errors.New("risk level is invalid")
	case !input.RequiredAssurance.IsValid() ||
		input.RequiredAssurance == domain.AssuranceLevelInvalidated:
		return Task{}, errors.New("required assurance is invalid")
	}
	if err := validateBounded("task idempotency key", input.IdempotencyKey, 255); err != nil {
		return Task{}, err
	}
	now, micros := repositories.timestamp()
	task := Task{
		ID:                input.ID,
		ThreadID:          input.ThreadID,
		RepositoryID:      input.RepositoryID,
		RequestMessageID:  input.RequestMessageID,
		State:             domain.TaskStateDraft,
		PolicyPreset:      input.PolicyPreset,
		ReasoningEffort:   input.ReasoningEffort,
		RiskLevel:         input.RiskLevel,
		RequiredAssurance: input.RequiredAssurance,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findTaskByIdempotency(
			ctx,
			transaction,
			input.ThreadID,
			input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID ||
				existing.RepositoryID != input.RepositoryID ||
				existing.PolicyPreset != input.PolicyPreset ||
				existing.ReasoningEffort != input.ReasoningEffort ||
				existing.RiskLevel != input.RiskLevel ||
				existing.RequiredAssurance != input.RequiredAssurance ||
				!sameMessageID(existing.RequestMessageID, input.RequestMessageID) {
				return typedError(
					ErrConflict,
					"create idempotent task",
					errors.New("idempotency key belongs to different task"),
				)
			}
			task = existing
			return nil
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO tasks (
				id, thread_id, repository_id, request_message_id, state,
				policy_preset, reasoning_effort, risk_level, required_assurance,
				idempotency_key, created_at_unix_micros,
				updated_at_unix_micros, revision
			)
			SELECT ?, ?, ?, ?, 'draft', ?, ?, ?, ?, ?, ?, ?, 0
			FROM threads
			WHERE id = ? AND repository_id = ? AND deleted_at_unix_micros IS NULL
			  AND (
				? IS NULL
				OR EXISTS (
					SELECT 1 FROM messages
					WHERE id = ? AND thread_id = ?
				)
			  )`,
			input.ID,
			input.ThreadID,
			input.RepositoryID,
			nullableMessageID(input.RequestMessageID),
			input.PolicyPreset,
			input.ReasoningEffort,
			input.RiskLevel,
			input.RequiredAssurance,
			input.IdempotencyKey,
			micros,
			micros,
			input.ThreadID,
			input.RepositoryID,
			nullableMessageID(input.RequestMessageID),
			nullableMessageID(input.RequestMessageID),
			input.ThreadID,
		)
		if err != nil {
			return repositoryWriteError("create task", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(
				ErrConstraint,
				"create task",
				errors.New("thread does not belong to repository"),
			)
		}
		return nil
	})
	return task, err
}

func (repositories *Repositories) GetTask(
	ctx context.Context,
	id domain.TaskID,
) (Task, error) {
	if id.IsZero() {
		return Task{}, errors.New("task ID must not be empty")
	}
	return scanTask(repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, thread_id, repository_id, request_message_id, state,
		        policy_preset, reasoning_effort, risk_level, required_assurance,
		        created_at_unix_micros, updated_at_unix_micros, revision
		 FROM tasks WHERE id = ?`,
		id,
	), "get task")
}

func (repositories *Repositories) TransitionTask(
	ctx context.Context,
	input TransitionTask,
) (TransitionedTask, error) {
	switch {
	case input.EventID.IsZero():
		return TransitionedTask{}, errors.New("event ID must not be empty")
	case input.TaskID.IsZero():
		return TransitionedTask{}, errors.New("task ID must not be empty")
	}
	if err := validateBounded("task transition idempotency key", input.IdempotencyKey, 255); err != nil {
		return TransitionedTask{}, err
	}
	if err := domain.ValidateTaskTransition(domain.TaskTransition{
		From:     input.From,
		To:       input.To,
		Approval: input.Approval,
	}); err != nil {
		return TransitionedTask{}, err
	}
	payload, err := json.Marshal(struct {
		From     domain.TaskState            `json:"from"`
		To       domain.TaskState            `json:"to"`
		Approval domain.ApprovalRequestState `json:"approval,omitempty"`
	}{
		From:     input.From,
		To:       input.To,
		Approval: input.Approval,
	})
	if err != nil {
		return TransitionedTask{}, err
	}
	now, micros := repositories.timestamp()
	var transitioned TransitionedTask
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findTaskEventByIdempotency(
			ctx,
			transaction,
			input.TaskID,
			input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.EventID ||
				existing.EventType != "task.state-transition" ||
				existing.PayloadJSON != string(payload) ||
				!sameRunID(existing.RunID, input.RunID) {
				return typedError(
					ErrConflict,
					"retry task transition",
					errors.New("idempotency key belongs to different transition"),
				)
			}
			task, err := scanTask(transaction.sql.QueryRowContext(
				ctx,
				`SELECT id, thread_id, repository_id, request_message_id, state,
				        policy_preset, reasoning_effort, risk_level, required_assurance,
				        created_at_unix_micros, updated_at_unix_micros, revision
				 FROM tasks WHERE id = ?`,
				input.TaskID,
			), "read idempotent transitioned task")
			if err != nil {
				return err
			}
			transitioned = TransitionedTask{Task: task, Event: existing}
			return nil
		}

		task, err := scanTask(transaction.sql.QueryRowContext(
			ctx,
			`SELECT id, thread_id, repository_id, request_message_id, state,
			        policy_preset, reasoning_effort, risk_level, required_assurance,
			        created_at_unix_micros, updated_at_unix_micros, revision
			 FROM tasks WHERE id = ?`,
			input.TaskID,
		), "read task for transition")
		if err != nil {
			return err
		}
		if task.Revision != input.ExpectedRevision || task.State != input.From {
			return typedError(
				ErrStaleRevision,
				"transition task",
				errors.New("task state or revision changed"),
			)
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE tasks
			 SET state = ?, updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND state = ? AND revision = ?`,
			input.To,
			micros,
			input.TaskID,
			input.From,
			input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("transition task", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(
				ErrStaleRevision,
				"transition task",
				errors.New("task revision changed during transition"),
			)
		}
		event, err := appendTaskEventTransaction(ctx, transaction, AppendTaskEvent{
			ID:             input.EventID,
			TaskID:         input.TaskID,
			RunID:          input.RunID,
			EventType:      "task.state-transition",
			PayloadJSON:    string(payload),
			IdempotencyKey: input.IdempotencyKey,
		}, now, micros)
		if err != nil {
			return err
		}
		task.State = input.To
		task.UpdatedAt = now
		task.Revision++
		transitioned = TransitionedTask{Task: task, Event: event}
		return nil
	})
	return transitioned, err
}

func (repositories *Repositories) AppendTaskEvent(
	ctx context.Context,
	input AppendTaskEvent,
) (TaskEvent, error) {
	if err := validateAppendTaskEvent(input); err != nil {
		return TaskEvent{}, err
	}
	now, micros := repositories.timestamp()
	var event TaskEvent
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var err error
		event, err = appendTaskEventTransaction(ctx, transaction, input, now, micros)
		return err
	})
	return event, err
}

// ReplayTask reconstructs task state exclusively from its ordered immutable
// events. Task creation establishes the implicit draft state.
func (repositories *Repositories) ReplayTask(
	ctx context.Context,
	taskID domain.TaskID,
) (TaskReplay, error) {
	if taskID.IsZero() {
		return TaskReplay{}, errors.New("task ID must not be empty")
	}
	var exists int
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT 1 FROM tasks WHERE id = ?`,
		taskID,
	).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskReplay{}, typedError(ErrNotFound, "replay task", err)
		}
		return TaskReplay{}, classify("find task for replay", err)
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT sequence, event_type, payload_json
		 FROM task_events
		 WHERE task_id = ?
		 ORDER BY sequence`,
		taskID,
	)
	if err != nil {
		return TaskReplay{}, classify("read task events for replay", err)
	}
	defer rows.Close()

	replay := TaskReplay{State: domain.TaskStateDraft}
	var expectedSequence uint64 = 1
	for rows.Next() {
		var (
			sequence    uint64
			eventType   string
			payloadJSON string
		)
		if err := rows.Scan(&sequence, &eventType, &payloadJSON); err != nil {
			return TaskReplay{}, classify("scan task event for replay", err)
		}
		if sequence != expectedSequence {
			return TaskReplay{}, replayCorruption("task event sequence is not contiguous")
		}
		expectedSequence++
		replay.EventCount++
		if eventType != "task.state-transition" {
			continue
		}
		var payload struct {
			From     domain.TaskState            `json:"from"`
			To       domain.TaskState            `json:"to"`
			Approval domain.ApprovalRequestState `json:"approval,omitempty"`
		}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return TaskReplay{}, replayCorruption("task transition payload is invalid")
		}
		if payload.From != replay.State {
			return TaskReplay{}, replayCorruption("task transition source does not match replay state")
		}
		if err := domain.ValidateTaskTransition(domain.TaskTransition{
			From:     payload.From,
			To:       payload.To,
			Approval: payload.Approval,
		}); err != nil {
			return TaskReplay{}, replayCorruption("task transition violates the state machine")
		}
		replay.State = payload.To
		replay.Revision++
	}
	if err := rows.Err(); err != nil {
		return TaskReplay{}, classify("iterate task events for replay", err)
	}
	return replay, nil
}

func replayCorruption(message string) error {
	return typedError(ErrCorrupt, "replay task", errors.New(message))
}

func appendTaskEventTransaction(
	ctx context.Context,
	transaction *Transaction,
	input AppendTaskEvent,
	now time.Time,
	micros int64,
) (TaskEvent, error) {
	if err := validateAppendTaskEvent(input); err != nil {
		return TaskEvent{}, err
	}
	existing, found, err := findTaskEventByIdempotency(
		ctx,
		transaction,
		input.TaskID,
		input.IdempotencyKey,
	)
	if err != nil {
		return TaskEvent{}, err
	}
	if found {
		if existing.ID != input.ID ||
			existing.EventType != input.EventType ||
			existing.PayloadJSON != input.PayloadJSON ||
			!sameRunID(existing.RunID, input.RunID) {
			return TaskEvent{}, typedError(
				ErrConflict,
				"append idempotent task event",
				errors.New("idempotency key belongs to different event"),
			)
		}
		return existing, nil
	}
	if input.RunID != nil {
		if err := verifyRunBelongsToTask(
			ctx,
			transaction,
			*input.RunID,
			input.TaskID,
			"append task event",
		); err != nil {
			return TaskEvent{}, err
		}
	}
	var sequence uint64
	if err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM task_events WHERE task_id = ?`,
		input.TaskID,
	).Scan(&sequence); err != nil {
		return TaskEvent{}, classify("allocate task event sequence", err)
	}
	_, err = transaction.sql.ExecContext(
		ctx,
		`INSERT INTO task_events (
			id, task_id, run_id, sequence, event_type, payload_json,
			idempotency_key, created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID,
		input.TaskID,
		nullableRunID(input.RunID),
		sequence,
		input.EventType,
		input.PayloadJSON,
		input.IdempotencyKey,
		micros,
	)
	if err != nil {
		return TaskEvent{}, repositoryWriteError("append task event", err)
	}
	return TaskEvent{
		ID:             input.ID,
		TaskID:         input.TaskID,
		RunID:          input.RunID,
		Sequence:       sequence,
		EventType:      input.EventType,
		PayloadJSON:    input.PayloadJSON,
		IdempotencyKey: input.IdempotencyKey,
		CreatedAt:      now,
	}, nil
}

func validateAppendTaskEvent(input AppendTaskEvent) error {
	switch {
	case input.ID.IsZero():
		return errors.New("event ID must not be empty")
	case input.TaskID.IsZero():
		return errors.New("task ID must not be empty")
	case !json.Valid([]byte(input.PayloadJSON)):
		return errors.New("task event payload must be valid JSON")
	}
	if err := validateBounded("task event type", input.EventType, 255); err != nil {
		return err
	}
	return validateBounded("task event idempotency key", input.IdempotencyKey, 255)
}

type rowScanner interface {
	Scan(...any) error
}

func scanTask(row rowScanner, operation string) (Task, error) {
	var (
		task              Task
		requestMessageRaw sql.NullString
		createdMicros     int64
		updatedMicros     int64
	)
	err := row.Scan(
		&task.ID,
		&task.ThreadID,
		&task.RepositoryID,
		&requestMessageRaw,
		&task.State,
		&task.PolicyPreset,
		&task.ReasoningEffort,
		&task.RiskLevel,
		&task.RequiredAssurance,
		&createdMicros,
		&updatedMicros,
		&task.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return Task{}, classify(operation, err)
	}
	if requestMessageRaw.Valid {
		id, err := domain.ParseMessageID(requestMessageRaw.String)
		if err != nil {
			return Task{}, typedError(ErrCorrupt, operation, err)
		}
		task.RequestMessageID = &id
	}
	task.CreatedAt = repositoryTime(createdMicros)
	task.UpdatedAt = repositoryTime(updatedMicros)
	return task, nil
}

func findTaskByIdempotency(
	ctx context.Context,
	transaction *Transaction,
	threadID domain.ThreadID,
	key string,
) (Task, bool, error) {
	task, err := scanTask(transaction.sql.QueryRowContext(
		ctx,
		`SELECT id, thread_id, repository_id, request_message_id, state,
		        policy_preset, reasoning_effort, risk_level, required_assurance,
		        created_at_unix_micros, updated_at_unix_micros, revision
		 FROM tasks WHERE thread_id = ? AND idempotency_key = ?`,
		threadID,
		key,
	), "find idempotent task")
	if errors.Is(err, ErrNotFound) {
		return Task{}, false, nil
	}
	return task, err == nil, err
}

func findTaskEventByIdempotency(
	ctx context.Context,
	transaction *Transaction,
	taskID domain.TaskID,
	key string,
) (TaskEvent, bool, error) {
	var (
		event         TaskEvent
		runRaw        sql.NullString
		createdMicros int64
	)
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT id, task_id, run_id, sequence, event_type, payload_json,
		        idempotency_key, created_at_unix_micros
		 FROM task_events WHERE task_id = ? AND idempotency_key = ?`,
		taskID,
		key,
	).Scan(
		&event.ID,
		&event.TaskID,
		&runRaw,
		&event.Sequence,
		&event.EventType,
		&event.PayloadJSON,
		&event.IdempotencyKey,
		&createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskEvent{}, false, nil
	}
	if err != nil {
		return TaskEvent{}, false, classify("find idempotent task event", err)
	}
	if runRaw.Valid {
		id, err := domain.ParseRunID(runRaw.String)
		if err != nil {
			return TaskEvent{}, false, typedError(ErrCorrupt, "parse event run ID", err)
		}
		event.RunID = &id
	}
	event.CreatedAt = repositoryTime(createdMicros)
	return event, true, nil
}

func nullableMessageID(id *domain.MessageID) any {
	if id == nil {
		return nil
	}
	return *id
}

func nullableRunID(id *domain.RunID) any {
	if id == nil {
		return nil
	}
	return *id
}

func sameMessageID(left, right *domain.MessageID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameRunID(left, right *domain.RunID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func verifyRunBelongsToTask(
	ctx context.Context,
	transaction *Transaction,
	runID domain.RunID,
	taskID domain.TaskID,
	operation string,
) error {
	var runTaskID domain.TaskID
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT task_id FROM runs WHERE id = ?`,
		runID,
	).Scan(&runTaskID)
	if errors.Is(err, sql.ErrNoRows) {
		return typedError(ErrConstraint, operation, err)
	}
	if err != nil {
		return classify(operation, err)
	}
	if runTaskID != taskID {
		return typedError(
			ErrConstraint,
			operation,
			errors.New("run does not belong to task"),
		)
	}
	return nil
}
