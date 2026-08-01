package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

func (repositories *Repositories) CreateThread(
	ctx context.Context,
	input CreateThread,
) (Thread, error) {
	commit, err := repositories.CreateThreadCommit(ctx, input)
	return commit.Thread, err
}

func (repositories *Repositories) CreateThreadCommit(
	ctx context.Context,
	input CreateThread,
) (ThreadCommit, error) {
	switch {
	case input.ID.IsZero():
		return ThreadCommit{}, errors.New("thread ID must not be empty")
	case input.ProjectID.IsZero():
		return ThreadCommit{}, errors.New("project ID must not be empty")
	case input.RepositoryID.IsZero():
		return ThreadCommit{}, errors.New("repository ID must not be empty")
	case input.WorkspaceID.IsZero() != (input.IdempotencyKey == ""):
		return ThreadCommit{}, errors.New("workspace ID and thread idempotency key must be supplied together")
	case input.WorkspaceID.IsZero() != input.SessionID.IsZero():
		return ThreadCommit{}, errors.New("workspace thread creation requires a session ID")
	}
	if err := validateBounded("thread title", input.Title, 512); err != nil {
		return ThreadCommit{}, err
	}
	now, micros := repositories.timestamp()
	thread := Thread{
		ID:           input.ID,
		ProjectID:    input.ProjectID,
		RepositoryID: input.RepositoryID,
		WorkspaceID:  input.WorkspaceID,
		SessionID:    input.SessionID,
		Title:        input.Title,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	var committedEvents []events.SessionEvent
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if !input.WorkspaceID.IsZero() {
			existing, found, err := findThreadByCreateKey(ctx, transaction, input.WorkspaceID, input.IdempotencyKey)
			if err != nil {
				return err
			}
			if found {
				if existing.ProjectID != input.ProjectID || existing.RepositoryID != input.RepositoryID || existing.Title != input.Title {
					return typedError(ErrConflict, "create idempotent thread", errors.New("idempotency key belongs to different thread content"))
				}
				thread = existing
				return nil
			}
			result, err := transaction.sql.ExecContext(ctx, `INSERT INTO threads (
				id, project_id, repository_id, workspace_id, title,
				create_idempotency_key, created_at_unix_micros,
				updated_at_unix_micros, revision
			)
			SELECT ?, repositories.project_id, workspaces.repository_id, workspaces.id, ?, ?, ?, ?, 0
			FROM workspaces
			JOIN repositories ON repositories.id = workspaces.repository_id
			WHERE workspaces.id = ? AND workspaces.repository_id = ?
			  AND repositories.project_id = ? AND workspaces.state = 'active'
			  AND repositories.deleted_at_unix_micros IS NULL`,
				input.ID, input.Title, input.IdempotencyKey, micros, micros,
				input.WorkspaceID, input.RepositoryID, input.ProjectID,
			)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return typedError(ErrConstraint, "create thread", errors.New("workspace does not authorize repository"))
			}
			session := Session{ID: input.SessionID, ThreadID: input.ID, CreatedAt: now, UpdatedAt: now}
			if err := createSessionTransaction(ctx, transaction, session, micros); err != nil {
				return err
			}
			workspaceID := input.WorkspaceID
			event, err := repositories.AppendSessionEvent(ctx, transaction, events.NewSessionEvent{
				SessionID: input.SessionID, ThreadID: input.ID, Kind: events.KindThreadCreated,
				Revision: 0, PayloadVersion: 1, Payload: events.Payload{ThreadCreated: &events.ThreadCreated{
					WorkspaceID: &workspaceID, Title: input.Title,
				}},
			})
			if err != nil {
				return err
			}
			committedEvents = append(committedEvents, event)
			return nil
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO threads (
				id, project_id, repository_id, title,
				created_at_unix_micros, updated_at_unix_micros, revision
			)
			SELECT ?, ?, ?, ?, ?, ?, 0
			FROM repositories
			WHERE id = ? AND project_id = ? AND deleted_at_unix_micros IS NULL`,
			input.ID,
			input.ProjectID,
			input.RepositoryID,
			input.Title,
			micros,
			micros,
			input.RepositoryID,
			input.ProjectID,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(
				ErrConstraint,
				"create thread",
				errors.New("repository does not belong to project"),
			)
		}
		return nil
	})
	if err != nil {
		return ThreadCommit{}, repositoryWriteError("create thread", err)
	}
	return ThreadCommit{Thread: thread, Events: committedEvents}, nil
}

func findThreadByCreateKey(ctx context.Context, transaction *Transaction, workspaceID domain.WorkspaceID, key string) (Thread, bool, error) {
	thread, err := scanThread(transaction.sql.QueryRowContext(ctx, threadSelect+`
		WHERE threads.workspace_id = ? AND threads.create_idempotency_key = ?`, workspaceID, key), "find idempotent thread")
	if errors.Is(err, ErrNotFound) {
		return Thread{}, false, nil
	}
	return thread, err == nil, err
}

func (repositories *Repositories) ListThreads(
	ctx context.Context,
	input ListThreads,
) (ThreadPage, error) {
	if input.RepositoryID.IsZero() {
		return ThreadPage{}, errors.New("repository ID must not be empty")
	}
	if !input.WorkspaceID.IsZero() {
		var authorized int
		if err := repositories.database.sql.QueryRowContext(ctx,
			`SELECT count(*) FROM workspaces WHERE id = ? AND repository_id = ? AND state = 'active'`,
			input.WorkspaceID, input.RepositoryID,
		).Scan(&authorized); err != nil {
			return ThreadPage{}, classify("authorize thread workspace", err)
		}
		if authorized != 1 {
			return ThreadPage{}, typedError(ErrNotFound, "authorize thread workspace", sql.ErrNoRows)
		}
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Limit < 1 || input.Limit > 100 {
		return ThreadPage{}, errors.New("thread page limit must be between 1 and 100")
	}
	query := threadSelect + ` WHERE threads.repository_id = ? AND threads.deleted_at_unix_micros IS NULL`
	arguments := []any{input.RepositoryID}
	if !input.WorkspaceID.IsZero() {
		query += ` AND threads.workspace_id = ?`
		arguments = append(arguments, input.WorkspaceID)
	}
	if !input.IncludeArchived {
		query += ` AND threads.archived_at_unix_micros IS NULL`
	}
	if input.Before != nil {
		if input.Before.ID.IsZero() || input.Before.UpdatedAt.IsZero() {
			return ThreadPage{}, errors.New("thread cursor must be complete")
		}
		micros := input.Before.UpdatedAt.UTC().UnixMicro()
		query += ` AND (
			updated_at_unix_micros < ?
			OR (updated_at_unix_micros = ? AND id < ?)
		)`
		arguments = append(arguments, micros, micros, input.Before.ID)
	}
	query += ` ORDER BY updated_at_unix_micros DESC, id DESC LIMIT ?`
	arguments = append(arguments, input.Limit+1)
	rows, err := repositories.database.sql.QueryContext(ctx, query, arguments...)
	if err != nil {
		return ThreadPage{}, classify("list threads", err)
	}
	defer rows.Close()
	threads := make([]Thread, 0, input.Limit+1)
	for rows.Next() {
		thread, err := scanThreadValues(rows)
		if err != nil {
			return ThreadPage{}, classify("scan thread page", err)
		}
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return ThreadPage{}, classify("iterate thread page", err)
	}
	page := ThreadPage{Threads: threads}
	if len(threads) > input.Limit {
		page.Threads = threads[:input.Limit]
		last := page.Threads[len(page.Threads)-1]
		page.Next = &ThreadCursor{UpdatedAt: last.UpdatedAt, ID: last.ID}
	}
	return page, nil
}

func (repositories *Repositories) AppendMessage(
	ctx context.Context,
	input AppendMessage,
) (Message, error) {
	if err := validateAppendMessageInput(input); err != nil {
		return Message{}, err
	}
	now, micros := repositories.timestamp()
	var message Message
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var err error
		message, err = appendMessageTransaction(ctx, transaction, input, now, micros)
		return err
	})
	return message, err
}

func validateAppendMessageInput(input AppendMessage) error {
	switch {
	case input.ID.IsZero():
		return errors.New("message ID must not be empty")
	case input.ThreadID.IsZero():
		return errors.New("thread ID must not be empty")
	case !input.Role.IsValid():
		return errors.New("message role is invalid")
	}
	if len(input.BodyRedacted) > MaximumMessageBodyBytes {
		return errors.New("message body exceeds maximum length")
	}
	if err := validateBounded("message idempotency key", input.IdempotencyKey, 255); err != nil {
		return err
	}
	return nil
}

func appendMessageTransaction(
	ctx context.Context,
	transaction *Transaction,
	input AppendMessage,
	now time.Time,
	micros int64,
) (Message, error) {
	existing, found, err := findMessageByIdempotency(ctx, transaction, input.ThreadID, input.IdempotencyKey)
	if err != nil {
		return Message{}, err
	}
	if found {
		// The message ID is deliberately NOT compared: a client mints a fresh
		// ID per attempt, so the idempotency key is the deduplication identity
		// and the stored row is returned as canonical. Content mismatch is the
		// conflict, because that is a different message wearing the same key.
		if existing.Role != input.Role || existing.BodyRedacted != input.BodyRedacted ||
			!sameArtifactIDs(existing.AttachmentIDs, input.AttachmentIDs) {
			return Message{}, typedError(ErrConflict, "append idempotent message",
				errors.New("idempotency key belongs to different message content"))
		}
		return existing, nil
	}
	var sequence uint64
	if err := transaction.sql.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE thread_id = ?`,
		input.ThreadID).Scan(&sequence); err != nil {
		return Message{}, classify("allocate message sequence", err)
	}
	result, err := transaction.sql.ExecContext(ctx, `INSERT INTO messages (
		id, thread_id, sequence, role, body_redacted,
		idempotency_key, created_at_unix_micros
	)
	SELECT ?, ?, ?, ?, ?, ?, ?
	FROM threads
	WHERE id = ? AND deleted_at_unix_micros IS NULL
	  AND archived_at_unix_micros IS NULL`, input.ID, input.ThreadID, sequence,
		input.Role, input.BodyRedacted, input.IdempotencyKey, micros, input.ThreadID)
	if err != nil {
		return Message{}, repositoryWriteError("append message", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Message{}, err
	}
	if affected != 1 {
		return Message{}, typedError(ErrNotFound, "append message", sql.ErrNoRows)
	}
	for ordinal, artifactID := range input.AttachmentIDs {
		if artifactID.IsZero() {
			return Message{}, errors.New("attachment artifact ID must not be empty")
		}
		result, err := transaction.sql.ExecContext(ctx, `INSERT INTO message_attachments (
			message_id, ordinal, artifact_id
		)
		SELECT ?, ?, artifacts.id
		FROM artifacts
		JOIN threads ON threads.id = ?
		WHERE artifacts.id = ? AND artifacts.project_id = threads.project_id
		  AND artifacts.deleted_at_unix_micros IS NULL`, input.ID, ordinal, input.ThreadID, artifactID)
		if err != nil {
			return Message{}, repositoryWriteError("attach message artifact", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return Message{}, err
		}
		if affected != 1 {
			return Message{}, typedError(ErrConstraint, "attach message artifact",
				errors.New("artifact does not belong to thread project"))
		}
	}
	if _, err := transaction.sql.ExecContext(ctx, `UPDATE threads
		SET updated_at_unix_micros = ?, revision = revision + 1
		WHERE id = ?`, micros, input.ThreadID); err != nil {
		return Message{}, repositoryWriteError("advance thread after message", err)
	}
	return Message{
		ID: input.ID, ThreadID: input.ThreadID, Sequence: sequence, Role: input.Role,
		BodyRedacted: input.BodyRedacted, AttachmentIDs: append([]domain.ArtifactID(nil), input.AttachmentIDs...),
		IdempotencyKey: input.IdempotencyKey, CreatedAt: now,
	}, nil
}

func findMessageByIdempotency(
	ctx context.Context,
	transaction *Transaction,
	threadID domain.ThreadID,
	key string,
) (Message, bool, error) {
	var (
		message       Message
		createdMicros int64
	)
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT id, thread_id, sequence, role, body_redacted,
		        idempotency_key, created_at_unix_micros
		 FROM messages
		 WHERE thread_id = ? AND idempotency_key = ?`,
		threadID,
		key,
	).Scan(
		&message.ID,
		&message.ThreadID,
		&message.Sequence,
		&message.Role,
		&message.BodyRedacted,
		&message.IdempotencyKey,
		&createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, classify("find idempotent message", err)
	}
	message.CreatedAt = repositoryTime(createdMicros)
	attachments, err := listMessageAttachmentIDs(ctx, transaction.sql, message.ID)
	if err != nil {
		return Message{}, false, err
	}
	message.AttachmentIDs = attachments
	return message, true, nil
}

func sameArtifactIDs(left, right []domain.ArtifactID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
