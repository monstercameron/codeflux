package storage

import (
	"context"
	"database/sql"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

func (repositories *Repositories) CreateThread(
	ctx context.Context,
	input CreateThread,
) (Thread, error) {
	switch {
	case input.ID.IsZero():
		return Thread{}, errors.New("thread ID must not be empty")
	case input.ProjectID.IsZero():
		return Thread{}, errors.New("project ID must not be empty")
	case input.RepositoryID.IsZero():
		return Thread{}, errors.New("repository ID must not be empty")
	}
	if err := validateBounded("thread title", input.Title, 512); err != nil {
		return Thread{}, err
	}
	now, micros := repositories.timestamp()
	thread := Thread{
		ID:           input.ID,
		ProjectID:    input.ProjectID,
		RepositoryID: input.RepositoryID,
		Title:        input.Title,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
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
		return Thread{}, repositoryWriteError("create thread", err)
	}
	return thread, nil
}

func (repositories *Repositories) ListThreads(
	ctx context.Context,
	input ListThreads,
) (ThreadPage, error) {
	if input.RepositoryID.IsZero() {
		return ThreadPage{}, errors.New("repository ID must not be empty")
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Limit < 1 || input.Limit > 100 {
		return ThreadPage{}, errors.New("thread page limit must be between 1 and 100")
	}
	query := `SELECT id, project_id, repository_id, title,
	                 created_at_unix_micros, updated_at_unix_micros, revision
	          FROM threads
	          WHERE repository_id = ? AND deleted_at_unix_micros IS NULL`
	arguments := []any{input.RepositoryID}
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
		var (
			thread        Thread
			createdMicros int64
			updatedMicros int64
		)
		if err := rows.Scan(
			&thread.ID,
			&thread.ProjectID,
			&thread.RepositoryID,
			&thread.Title,
			&createdMicros,
			&updatedMicros,
			&thread.Revision,
		); err != nil {
			return ThreadPage{}, classify("scan thread page", err)
		}
		thread.CreatedAt = repositoryTime(createdMicros)
		thread.UpdatedAt = repositoryTime(updatedMicros)
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
	switch {
	case input.ID.IsZero():
		return Message{}, errors.New("message ID must not be empty")
	case input.ThreadID.IsZero():
		return Message{}, errors.New("thread ID must not be empty")
	case !input.Role.IsValid():
		return Message{}, errors.New("message role is invalid")
	}
	if err := validateBounded("message idempotency key", input.IdempotencyKey, 255); err != nil {
		return Message{}, err
	}
	now, micros := repositories.timestamp()
	var message Message
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findMessageByIdempotency(
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
				existing.Role != input.Role ||
				existing.BodyRedacted != input.BodyRedacted {
				return typedError(
					ErrConflict,
					"append idempotent message",
					errors.New("idempotency key belongs to different message content"),
				)
			}
			message = existing
			return nil
		}
		var sequence uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE thread_id = ?`,
			input.ThreadID,
		).Scan(&sequence); err != nil {
			return classify("allocate message sequence", err)
		}
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO messages (
				id, thread_id, sequence, role, body_redacted,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			input.ID,
			input.ThreadID,
			sequence,
			input.Role,
			input.BodyRedacted,
			input.IdempotencyKey,
			micros,
		)
		if err != nil {
			return repositoryWriteError("append message", err)
		}
		message = Message{
			ID:             input.ID,
			ThreadID:       input.ThreadID,
			Sequence:       sequence,
			Role:           input.Role,
			BodyRedacted:   input.BodyRedacted,
			IdempotencyKey: input.IdempotencyKey,
			CreatedAt:      now,
		}
		return nil
	})
	return message, err
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
	return message, true, nil
}
