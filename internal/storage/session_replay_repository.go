package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

func (repositories *Repositories) StoreSessionSnapshot(
	ctx context.Context,
	snapshot events.SessionSnapshot,
) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal session snapshot: %w", err)
	}
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO session_snapshots (
				session_id, through_sequence, thread_id, task_id,
				task_state, task_revision, snapshot_version, state_json,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(session_id, through_sequence) DO NOTHING`,
			snapshot.SessionID,
			snapshot.ThroughSequence,
			snapshot.ThreadID,
			nullableSessionTaskID(snapshot.TaskID),
			nullableTaskState(snapshot.TaskID, snapshot.TaskState),
			snapshot.TaskRevision,
			snapshot.SnapshotVersion,
			string(encoded),
			snapshot.CreatedAt.UnixMicro(),
		)
		if err != nil {
			return repositoryWriteError("store session snapshot", err)
		}
		var existing string
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT state_json
			 FROM session_snapshots
			 WHERE session_id = ? AND through_sequence = ?`,
			snapshot.SessionID,
			snapshot.ThroughSequence,
		).Scan(&existing); err != nil {
			return classify("read stored session snapshot", err)
		}
		if existing != string(encoded) {
			return typedError(
				ErrConflict,
				"store session snapshot",
				errors.New("snapshot sequence belongs to different state"),
			)
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE sessions
			 SET compacted_through_sequence = CASE
			         WHEN compacted_through_sequence < ? THEN ?
			         ELSE compacted_through_sequence
			     END,
			     updated_at_unix_micros = CASE
			         WHEN updated_at_unix_micros < ? THEN ?
			         ELSE updated_at_unix_micros
			     END
			 WHERE id = ? AND current_sequence >= ?`,
			snapshot.ThroughSequence,
			snapshot.ThroughSequence,
			snapshot.CreatedAt.UnixMicro(),
			snapshot.CreatedAt.UnixMicro(),
			snapshot.SessionID,
			snapshot.ThroughSequence,
		)
		if err != nil {
			return repositoryWriteError("advance session snapshot boundary", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(
				ErrNotFound,
				"advance session snapshot boundary",
				errors.New("session does not contain snapshot sequence"),
			)
		}
		return nil
	})
}

func (repositories *Repositories) ReplaySession(
	ctx context.Context,
	input ReplaySessionEvents,
) (SessionReplay, error) {
	if input.SessionID.IsZero() {
		return SessionReplay{}, errors.New("session ID must not be empty")
	}
	if input.Limit == 0 {
		input.Limit = 256
	}
	if input.Limit < 1 || input.Limit > 1000 {
		return SessionReplay{}, errors.New("session replay limit must be between 1 and 1000")
	}
	var replay SessionReplay
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var compacted uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT current_sequence, compacted_through_sequence
			 FROM sessions
			 WHERE id = ?`,
			input.SessionID,
		).Scan(&replay.Boundary, &compacted); errors.Is(err, sql.ErrNoRows) {
			return typedError(ErrNotFound, "read session replay boundary", err)
		} else if err != nil {
			return classify("read session replay boundary", err)
		}
		if input.AfterSequence > replay.Boundary {
			return typedError(
				ErrStaleRevision,
				"replay session",
				errors.New("requested sequence exceeds committed history"),
			)
		}
		after := input.AfterSequence
		if after < compacted {
			snapshot, err := loadSessionSnapshot(
				ctx,
				transaction,
				input.SessionID,
				compacted,
			)
			if err != nil {
				return err
			}
			replay.Snapshot = &snapshot
			after = snapshot.ThroughSequence
		}
		eventsAfter, err := replaySessionEventsTransaction(
			ctx,
			transaction,
			input.SessionID,
			after,
			replay.Boundary,
			input.Limit,
		)
		if err != nil {
			return err
		}
		replay.Events = eventsAfter
		return nil
	})
	if err != nil {
		return SessionReplay{}, err
	}
	return replay, nil
}

func (repositories *Repositories) ExecuteSessionCommand(
	ctx context.Context,
	command SessionCommand,
	operation SessionCommandOperation,
) (SessionCommandResult, error) {
	switch {
	case command.SessionID.IsZero():
		return SessionCommandResult{}, errors.New("command session ID must not be empty")
	case operation == nil:
		return SessionCommandResult{}, errors.New("session command operation must not be nil")
	}
	if err := validateBounded(
		"session command idempotency key",
		command.IdempotencyKey,
		255,
	); err != nil {
		return SessionCommandResult{}, err
	}
	if !validSHA256(command.RequestSHA256) {
		return SessionCommandResult{}, errors.New("session command request SHA-256 is invalid")
	}
	now, micros := repositories.timestamp()
	result := SessionCommandResult{
		SessionID:      command.SessionID,
		IdempotencyKey: command.IdempotencyKey,
		RequestSHA256:  command.RequestSHA256,
		CreatedAt:      now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var createdMicros int64
		err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT request_sha256, result_json, final_sequence,
			        created_at_unix_micros
			 FROM session_commands
			 WHERE session_id = ? AND idempotency_key = ?`,
			command.SessionID,
			command.IdempotencyKey,
		).Scan(
			&result.RequestSHA256,
			&result.ResultJSON,
			&result.FinalSequence,
			&createdMicros,
		)
		if err == nil {
			if result.RequestSHA256 != command.RequestSHA256 {
				return typedError(
					ErrConflict,
					"execute idempotent session command",
					errors.New("idempotency key belongs to a different request"),
				)
			}
			result.CreatedAt = repositoryTime(createdMicros)
			result.Replayed = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return classify("find idempotent session command", err)
		}
		resultJSON, finalSequence, err := operation(transaction)
		if err != nil {
			return err
		}
		if err := validateJSONObject(resultJSON, 1024*1024); err != nil {
			return err
		}
		result.ResultJSON = resultJSON
		result.FinalSequence = finalSequence
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO session_commands (
				session_id, idempotency_key, request_sha256, result_json,
				final_sequence, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?)`,
			command.SessionID,
			command.IdempotencyKey,
			command.RequestSHA256,
			result.ResultJSON,
			result.FinalSequence,
			micros,
		)
		if err != nil {
			return repositoryWriteError("persist session command result", err)
		}
		return nil
	})
	if err != nil {
		return SessionCommandResult{}, err
	}
	return result, nil
}

func loadSessionSnapshot(
	ctx context.Context,
	transaction *Transaction,
	sessionID domain.SessionID,
	through uint64,
) (events.SessionSnapshot, error) {
	var (
		encoded         string
		throughSequence uint64
		threadID        domain.ThreadID
		taskID          sql.NullString
		taskState       sql.NullString
		taskRevision    uint64
		snapshotVersion uint32
		createdMicros   int64
	)
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT through_sequence, thread_id, task_id, task_state,
		        task_revision, snapshot_version, state_json,
		        created_at_unix_micros
		 FROM session_snapshots
		 WHERE session_id = ? AND through_sequence <= ?
		 ORDER BY through_sequence DESC
		 LIMIT 1`,
		sessionID,
		through,
	).Scan(
		&throughSequence,
		&threadID,
		&taskID,
		&taskState,
		&taskRevision,
		&snapshotVersion,
		&encoded,
		&createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return events.SessionSnapshot{}, typedError(
			ErrCorrupt,
			"load compacted session snapshot",
			err,
		)
	}
	if err != nil {
		return events.SessionSnapshot{}, classify("load session snapshot", err)
	}
	var snapshot events.SessionSnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return events.SessionSnapshot{}, fmt.Errorf("decode session snapshot: %w", err)
	}
	if err := snapshot.Validate(); err != nil {
		return events.SessionSnapshot{}, err
	}
	if snapshot.SessionID != sessionID ||
		snapshot.ThroughSequence != throughSequence ||
		snapshot.ThreadID != threadID ||
		snapshot.TaskRevision != taskRevision ||
		snapshot.SnapshotVersion != snapshotVersion ||
		snapshot.CreatedAt != repositoryTime(createdMicros) ||
		!sameNullableSnapshotTask(snapshot, taskID, taskState) {
		return events.SessionSnapshot{}, typedError(
			ErrCorrupt,
			"verify session snapshot columns",
			errors.New("snapshot JSON differs from indexed columns"),
		)
	}
	return snapshot, nil
}

func replaySessionEventsTransaction(
	ctx context.Context,
	transaction *Transaction,
	sessionID domain.SessionID,
	after uint64,
	through uint64,
	limit int,
) ([]events.SessionEvent, error) {
	rows, err := transaction.sql.QueryContext(
		ctx,
		`SELECT sequence, thread_id, task_id, timestamp_unix_micros,
		        kind, entity_revision, causation_id, correlation_id,
		        payload_version, payload_json
		 FROM session_events
		 WHERE session_id = ? AND sequence > ? AND sequence <= ?
		 ORDER BY sequence
		 LIMIT ?`,
		sessionID,
		after,
		through,
		limit,
	)
	if err != nil {
		return nil, classify("replay session transaction", err)
	}
	defer rows.Close()
	result := make([]events.SessionEvent, 0, limit)
	for rows.Next() {
		event, err := scanSessionEvent(rows, sessionID)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate session transaction replay", err)
	}
	return result, nil
}

func nullableTaskState(
	taskID *domain.TaskID,
	state domain.TaskState,
) any {
	if taskID == nil {
		return nil
	}
	return state
}

func sameNullableSnapshotTask(
	snapshot events.SessionSnapshot,
	taskID sql.NullString,
	taskState sql.NullString,
) bool {
	if snapshot.TaskID == nil {
		return !taskID.Valid && !taskState.Valid
	}
	return taskID.Valid && taskState.Valid &&
		snapshot.TaskID.String() == taskID.String &&
		string(snapshot.TaskState) == taskState.String
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 ||
		value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateJSONObject(value string, maximum int) error {
	if len(value) == 0 || len(value) > maximum {
		return errors.New("session command result JSON has invalid length")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &object); err != nil || object == nil {
		return errors.New("session command result must be one JSON object")
	}
	return nil
}
