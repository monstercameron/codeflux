package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

func (repositories *Repositories) CreateSession(
	ctx context.Context,
	input CreateSession,
) (Session, error) {
	switch {
	case input.ID.IsZero():
		return Session{}, errors.New("session ID must not be empty")
	case input.ThreadID.IsZero():
		return Session{}, errors.New("session thread ID must not be empty")
	}
	now, micros := repositories.timestamp()
	session := Session{
		ID:        input.ID,
		ThreadID:  input.ThreadID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		return createSessionTransaction(ctx, transaction, session, micros)
	})
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func createSessionTransaction(
	ctx context.Context,
	transaction *Transaction,
	session Session,
	micros int64,
) error {
	result, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO sessions (
				id, thread_id, current_sequence, compacted_through_sequence,
				created_at_unix_micros, updated_at_unix_micros
			)
			SELECT ?, ?, 0, 0, ?, ?
			FROM threads
			WHERE id = ? AND deleted_at_unix_micros IS NULL`,
		session.ID,
		session.ThreadID,
		micros,
		micros,
		session.ThreadID,
	)
	if err != nil {
		return repositoryWriteError("create session", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return typedError(
			ErrNotFound,
			"create session",
			errors.New("thread does not exist"),
		)
	}
	return nil
}

// GetThreadSession returns the sole authoritative event stream for a thread.
func (repositories *Repositories) GetThreadSession(ctx context.Context, threadID domain.ThreadID) (Session, error) {
	if threadID.IsZero() {
		return Session{}, errors.New("session thread ID must not be empty")
	}
	var session Session
	var createdMicros, updatedMicros int64
	err := repositories.database.sql.QueryRowContext(ctx, `SELECT id, thread_id,
		current_sequence, compacted_through_sequence,
		created_at_unix_micros, updated_at_unix_micros
		FROM sessions WHERE thread_id = ?`, threadID).Scan(
		&session.ID, &session.ThreadID, &session.CurrentSequence,
		&session.CompactedThroughSequence, &createdMicros, &updatedMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, typedError(ErrNotFound, "get thread session", err)
	}
	if err != nil {
		return Session{}, classify("get thread session", err)
	}
	session.CreatedAt = repositoryTime(createdMicros)
	session.UpdatedAt = repositoryTime(updatedMicros)
	return session, nil
}

// AppendSessionEvent allocates sequence and persists the event in the supplied
// transaction. Callers must publish only after that transaction commits.
func (repositories *Repositories) AppendSessionEvent(
	ctx context.Context,
	transaction *Transaction,
	input events.NewSessionEvent,
) (events.SessionEvent, error) {
	if transaction == nil || transaction.sql == nil {
		return events.SessionEvent{}, errors.New("session event transaction must not be nil")
	}
	now, micros := repositories.timestamp()
	if _, err := input.Build(1, now); err != nil {
		return events.SessionEvent{}, fmt.Errorf("validate new session event: %w", err)
	}
	var sequence uint64
	if err := transaction.sql.QueryRowContext(
		ctx,
		`UPDATE sessions
		 SET current_sequence = current_sequence + 1,
		     updated_at_unix_micros = ?
		 WHERE id = ? AND thread_id = ?
		 RETURNING current_sequence`,
		micros,
		input.SessionID,
		input.ThreadID,
	).Scan(&sequence); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return events.SessionEvent{}, typedError(
				ErrNotFound,
				"allocate session event sequence",
				err,
			)
		}
		return events.SessionEvent{}, classify("allocate session event sequence", err)
	}
	event, err := input.Build(sequence, now)
	if err != nil {
		return events.SessionEvent{}, fmt.Errorf("build session event: %w", err)
	}
	payload, err := events.MarshalPayload(event.Payload)
	if err != nil {
		return events.SessionEvent{}, err
	}
	_, err = transaction.sql.ExecContext(
		ctx,
		`INSERT INTO session_events (
			session_id, sequence, thread_id, task_id,
			timestamp_unix_micros, kind, entity_revision,
			causation_id, correlation_id, payload_version, payload_json,
			delivery_class, correctness_bearing
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.SessionID,
		event.Sequence,
		event.ThreadID,
		nullableSessionTaskID(event.TaskID),
		event.Timestamp.UnixMicro(),
		event.Kind,
		event.Revision,
		nullableSessionEventID(event.CausationID),
		nullableSessionEventID(event.CorrelationID),
		event.PayloadVersion,
		string(payload),
		event.DeliveryClass(),
		boolInteger(event.CorrectnessBearing()),
	)
	if err != nil {
		return events.SessionEvent{}, repositoryWriteError("append session event", err)
	}
	return event, nil
}

// PersistSessionEvent returns only after the append transaction commits.
func (repositories *Repositories) PersistSessionEvent(
	ctx context.Context,
	input events.NewSessionEvent,
) (events.SessionEvent, error) {
	var event events.SessionEvent
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var err error
		event, err = repositories.AppendSessionEvent(ctx, transaction, input)
		return err
	})
	if err != nil {
		return events.SessionEvent{}, err
	}
	return event, nil
}

// PersistAndPublishSessionEvent makes the durability boundary explicit:
// publication is attempted only after PersistSessionEvent has committed.
func (repositories *Repositories) PersistAndPublishSessionEvent(
	ctx context.Context,
	input events.NewSessionEvent,
	publisher CommittedEventPublisher,
) (events.SessionEvent, error) {
	if publisher == nil {
		return events.SessionEvent{}, errors.New("committed event publisher must not be nil")
	}
	event, err := repositories.PersistSessionEvent(ctx, input)
	if err != nil {
		return events.SessionEvent{}, err
	}
	if err := publisher.PublishCommitted(event); err != nil {
		return event, fmt.Errorf("publish committed session event: %w", err)
	}
	return event, nil
}

func (repositories *Repositories) ReplaySessionEvents(
	ctx context.Context,
	input ReplaySessionEvents,
) ([]events.SessionEvent, error) {
	if input.SessionID.IsZero() {
		return nil, errors.New("session ID must not be empty")
	}
	if input.Limit == 0 {
		input.Limit = 256
	}
	if input.Limit < 1 || input.Limit > 1000 {
		return nil, errors.New("session replay limit must be between 1 and 1000")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT sequence, thread_id, task_id, timestamp_unix_micros,
		        kind, entity_revision, causation_id, correlation_id,
		        payload_version, payload_json
		 FROM session_events
		 WHERE session_id = ? AND sequence > ?
		 ORDER BY sequence
		 LIMIT ?`,
		input.SessionID,
		input.AfterSequence,
		input.Limit,
	)
	if err != nil {
		return nil, classify("replay session events", err)
	}
	defer rows.Close()
	result := make([]events.SessionEvent, 0, input.Limit)
	for rows.Next() {
		event, err := scanSessionEvent(rows, input.SessionID)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate session event replay", err)
	}
	return result, nil
}

func (repositories *Repositories) CurrentSessionSequence(
	ctx context.Context,
	sessionID domain.SessionID,
) (uint64, error) {
	if sessionID.IsZero() {
		return 0, errors.New("session ID must not be empty")
	}
	var sequence uint64
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT current_sequence FROM sessions WHERE id = ?`,
		sessionID,
	).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, typedError(ErrNotFound, "read current session sequence", err)
	}
	if err != nil {
		return 0, classify("read current session sequence", err)
	}
	return sequence, nil
}

// ReplayCommitted adapts the repository to the events.Journal port.
func (repositories *Repositories) ReplayCommitted(
	ctx context.Context,
	sessionID domain.SessionID,
	afterSequence uint64,
	limit int,
) ([]events.SessionEvent, error) {
	return repositories.ReplaySessionEvents(ctx, ReplaySessionEvents{
		SessionID:     sessionID,
		AfterSequence: afterSequence,
		Limit:         limit,
	})
}

// CommittedSequence adapts the repository to the events.Journal port.
func (repositories *Repositories) CommittedSequence(
	ctx context.Context,
	sessionID domain.SessionID,
) (uint64, error) {
	return repositories.CurrentSessionSequence(ctx, sessionID)
}

type sessionEventScanner interface {
	Scan(...any) error
}

func scanSessionEvent(
	scanner sessionEventScanner,
	sessionID domain.SessionID,
) (events.SessionEvent, error) {
	var (
		event           events.SessionEvent
		taskID          sql.NullString
		causationID     sql.NullString
		correlationID   sql.NullString
		timestampMicros int64
		payloadJSON     string
	)
	event.SessionID = sessionID
	if err := scanner.Scan(
		&event.Sequence,
		&event.ThreadID,
		&taskID,
		&timestampMicros,
		&event.Kind,
		&event.Revision,
		&causationID,
		&correlationID,
		&event.PayloadVersion,
		&payloadJSON,
	); err != nil {
		return events.SessionEvent{}, classify("scan session event", err)
	}
	var err error
	if taskID.Valid {
		parsed, parseErr := domain.ParseTaskID(taskID.String)
		if parseErr != nil {
			return events.SessionEvent{}, fmt.Errorf("parse replay task ID: %w", parseErr)
		}
		event.TaskID = &parsed
	}
	if causationID.Valid {
		parsed, parseErr := domain.ParseEventID(causationID.String)
		if parseErr != nil {
			return events.SessionEvent{}, fmt.Errorf("parse replay causation ID: %w", parseErr)
		}
		event.CausationID = &parsed
	}
	if correlationID.Valid {
		parsed, parseErr := domain.ParseEventID(correlationID.String)
		if parseErr != nil {
			return events.SessionEvent{}, fmt.Errorf("parse replay correlation ID: %w", parseErr)
		}
		event.CorrelationID = &parsed
	}
	event.Timestamp = repositoryTime(timestampMicros)
	event.Payload, err = events.UnmarshalPayload(event.Kind, []byte(payloadJSON))
	if err != nil {
		return events.SessionEvent{}, err
	}
	if err := event.Validate(); err != nil {
		return events.SessionEvent{}, fmt.Errorf("validate replayed session event: %w", err)
	}
	return event, nil
}

func nullableSessionTaskID(id *domain.TaskID) any {
	if id == nil {
		return nil
	}
	return *id
}

func nullableSessionEventID(id *domain.EventID) any {
	if id == nil {
		return nil
	}
	return *id
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
