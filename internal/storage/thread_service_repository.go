package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

const threadSelect = `SELECT threads.id, threads.project_id, threads.repository_id,
	COALESCE(threads.workspace_id, ''),
	COALESCE((SELECT sessions.id FROM sessions WHERE sessions.thread_id = threads.id), ''),
	COALESCE((SELECT tasks.id FROM tasks WHERE tasks.thread_id = threads.id
		ORDER BY tasks.updated_at_unix_micros DESC, tasks.id DESC LIMIT 1), ''),
	COALESCE((SELECT tasks.state FROM tasks WHERE tasks.thread_id = threads.id
		ORDER BY tasks.updated_at_unix_micros DESC, tasks.id DESC LIMIT 1), ''),
	threads.title,
	CASE WHEN threads.archived_at_unix_micros IS NULL THEN 0 ELSE 1 END,
	threads.created_at_unix_micros, threads.updated_at_unix_micros, threads.revision
	FROM threads`

type threadRowScanner interface{ Scan(...any) error }

func scanThread(scanner threadRowScanner, operation string) (Thread, error) {
	thread, err := scanThreadValues(scanner)
	if errors.Is(err, sql.ErrNoRows) {
		return Thread{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return Thread{}, classify(operation, err)
	}
	return thread, nil
}

func scanThreadValues(scanner threadRowScanner) (Thread, error) {
	var thread Thread
	var workspace string
	var session string
	var task string
	var taskState string
	var archived int
	var createdMicros, updatedMicros int64
	if err := scanner.Scan(
		&thread.ID, &thread.ProjectID, &thread.RepositoryID, &workspace, &session, &task, &taskState,
		&thread.Title, &archived, &createdMicros, &updatedMicros, &thread.Revision,
	); err != nil {
		return Thread{}, err
	}
	if workspace != "" {
		parsed, err := domain.ParseWorkspaceID(workspace)
		if err != nil {
			return Thread{}, err
		}
		thread.WorkspaceID = parsed
	}
	if session != "" {
		parsed, err := domain.ParseSessionID(session)
		if err != nil {
			return Thread{}, err
		}
		thread.SessionID = parsed
	}
	if task != "" {
		parsed, err := domain.ParseTaskID(task)
		if err != nil {
			return Thread{}, err
		}
		thread.TaskID = parsed
		thread.TaskState = domain.TaskState(taskState)
		if !thread.TaskState.IsValid() {
			return Thread{}, domain.ErrUnknownState
		}
	}
	thread.Archived = archived == 1
	thread.CreatedAt = repositoryTime(createdMicros)
	thread.UpdatedAt = repositoryTime(updatedMicros)
	return thread, nil
}

// GetWorkspaceScope resolves the sole repository authority carried by a workspace.
func (repositories *Repositories) GetWorkspaceScope(ctx context.Context, workspaceID domain.WorkspaceID) (WorkspaceScope, error) {
	if workspaceID.IsZero() {
		return WorkspaceScope{}, errors.New("workspace ID must not be empty")
	}
	var scope WorkspaceScope
	err := repositories.database.sql.QueryRowContext(ctx, `SELECT workspaces.id,
		repositories.id, repositories.project_id, workspaces.canonical_path
		FROM workspaces
		JOIN repositories ON repositories.id = workspaces.repository_id
		WHERE workspaces.id = ? AND workspaces.state = 'active'
		  AND repositories.deleted_at_unix_micros IS NULL`, workspaceID).Scan(
		&scope.WorkspaceID, &scope.RepositoryID, &scope.ProjectID, &scope.CanonicalPath,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceScope{}, typedError(ErrNotFound, "get workspace scope", err)
	}
	if err != nil {
		return WorkspaceScope{}, classify("get workspace scope", err)
	}
	return scope, nil
}

// GetThread returns one non-deleted thread projection.
func (repositories *Repositories) GetThread(ctx context.Context, threadID domain.ThreadID) (Thread, error) {
	if threadID.IsZero() {
		return Thread{}, errors.New("thread ID must not be empty")
	}
	return scanThread(repositories.database.sql.QueryRowContext(ctx,
		threadSelect+` WHERE threads.id = ? AND threads.deleted_at_unix_micros IS NULL`, threadID), "get thread")
}

// ListMessages returns a bounded newest-first immutable message page.
func (repositories *Repositories) ListMessages(ctx context.Context, input ListMessages) (MessagePage, error) {
	if input.ThreadID.IsZero() {
		return MessagePage{}, errors.New("thread ID must not be empty")
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Limit < 1 || input.Limit > 100 {
		return MessagePage{}, errors.New("message page limit must be between 1 and 100")
	}
	query := `SELECT messages.id, messages.thread_id, messages.sequence, messages.role,
		messages.body_redacted, messages.idempotency_key, messages.created_at_unix_micros
		FROM messages JOIN threads ON threads.id = messages.thread_id
		WHERE messages.thread_id = ? AND threads.deleted_at_unix_micros IS NULL`
	arguments := []any{input.ThreadID}
	if input.Before != nil {
		if input.Before.BeforeSequence < 1 {
			return MessagePage{}, errors.New("message cursor must be complete")
		}
		query += ` AND messages.sequence < ?`
		arguments = append(arguments, input.Before.BeforeSequence)
	}
	query += ` ORDER BY messages.sequence DESC LIMIT ?`
	arguments = append(arguments, input.Limit+1)
	rows, err := repositories.database.sql.QueryContext(ctx, query, arguments...)
	if err != nil {
		return MessagePage{}, classify("list messages", err)
	}
	messages := make([]Message, 0, input.Limit+1)
	for rows.Next() {
		var message Message
		var createdMicros int64
		if err := rows.Scan(&message.ID, &message.ThreadID, &message.Sequence, &message.Role,
			&message.BodyRedacted, &message.IdempotencyKey, &createdMicros); err != nil {
			_ = rows.Close()
			return MessagePage{}, classify("scan message page", err)
		}
		message.CreatedAt = repositoryTime(createdMicros)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return MessagePage{}, classify("iterate message page", err)
	}
	if err := rows.Close(); err != nil {
		return MessagePage{}, classify("close message page", err)
	}
	page := MessagePage{Messages: messages}
	if len(messages) > input.Limit {
		page.Messages = messages[:input.Limit]
		page.Next = &MessageCursor{BeforeSequence: page.Messages[len(page.Messages)-1].Sequence}
	}
	for index := range page.Messages {
		attachments, err := listMessageAttachmentIDs(ctx, repositories.database.sql, page.Messages[index].ID)
		if err != nil {
			return MessagePage{}, err
		}
		page.Messages[index].AttachmentIDs = attachments
	}
	return page, nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listMessageAttachmentIDs(ctx context.Context, database queryer, messageID domain.MessageID) ([]domain.ArtifactID, error) {
	rows, err := database.QueryContext(ctx, `SELECT artifact_id FROM message_attachments
		WHERE message_id = ? ORDER BY ordinal`, messageID)
	if err != nil {
		return nil, classify("list message attachments", err)
	}
	defer rows.Close()
	var result []domain.ArtifactID
	for rows.Next() {
		var id domain.ArtifactID
		if err := rows.Scan(&id); err != nil {
			return nil, classify("scan message attachment", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate message attachments", err)
	}
	return result, nil
}

// RenameThread atomically applies an optimistic idempotent title mutation.
func (repositories *Repositories) RenameThread(ctx context.Context, input RenameThread) (Thread, error) {
	commit, err := repositories.RenameThreadCommit(ctx, input)
	return commit.Thread, err
}

func (repositories *Repositories) RenameThreadCommit(ctx context.Context, input RenameThread) (ThreadCommit, error) {
	if input.ThreadID.IsZero() {
		return ThreadCommit{}, errors.New("thread ID must not be empty")
	}
	if err := validateBounded("thread title", input.Title, 512); err != nil {
		return ThreadCommit{}, err
	}
	return repositories.mutateThread(ctx, input.ThreadID, input.ExpectedRevision,
		input.IdempotencyKey, "rename", input.Title, nil)
}

// ArchiveThread atomically applies an optimistic idempotent archive mutation.
func (repositories *Repositories) ArchiveThread(ctx context.Context, input ArchiveThread) (Thread, error) {
	commit, err := repositories.ArchiveThreadCommit(ctx, input)
	return commit.Thread, err
}

func (repositories *Repositories) ArchiveThreadCommit(ctx context.Context, input ArchiveThread) (ThreadCommit, error) {
	if input.ThreadID.IsZero() {
		return ThreadCommit{}, errors.New("thread ID must not be empty")
	}
	return repositories.mutateThread(ctx, input.ThreadID, input.ExpectedRevision,
		input.IdempotencyKey, "archive", "", &input.Archived)
}

func (repositories *Repositories) mutateThread(
	ctx context.Context,
	threadID domain.ThreadID,
	expectedRevision uint64,
	key, operation, title string,
	archived *bool,
) (ThreadCommit, error) {
	if err := validateBounded("thread mutation idempotency key", key, 255); err != nil {
		return ThreadCommit{}, err
	}
	fingerprint := threadMutationFingerprint(operation, expectedRevision, title, archived)
	now, micros := repositories.timestamp()
	var result Thread
	var committedEvents []events.SessionEvent
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, existingFingerprint, err := findThreadMutation(ctx, transaction, threadID, operation, key)
		if err != nil {
			return err
		}
		if found {
			if existingFingerprint != fingerprint {
				return typedError(ErrConflict, "replay thread mutation", errors.New("idempotency key belongs to different mutation"))
			}
			result = existing
			return nil
		}
		current, err := scanThread(transaction.sql.QueryRowContext(ctx,
			threadSelect+` WHERE threads.id = ? AND threads.deleted_at_unix_micros IS NULL`, threadID), "get thread for mutation")
		if err != nil {
			return err
		}
		if current.WorkspaceID.IsZero() {
			return typedError(ErrConstraint, "mutate thread", errors.New("thread has no workspace authority"))
		}
		if current.Revision != expectedRevision {
			return typedError(ErrStaleRevision, "mutate thread", errors.New("expected revision does not match"))
		}
		result = current
		result.UpdatedAt = now
		result.Revision++
		switch operation {
		case "rename":
			result.Title = strings.TrimSpace(title)
		case "archive":
			result.Archived = archived != nil && *archived
		default:
			return errors.New("unsupported thread mutation")
		}
		archiveValue := any(nil)
		if result.Archived {
			archiveValue = micros
		}
		update, err := transaction.sql.ExecContext(ctx, `UPDATE threads
			SET title = ?, archived_at_unix_micros = ?, updated_at_unix_micros = ?, revision = revision + 1
			WHERE id = ? AND revision = ? AND deleted_at_unix_micros IS NULL`,
			result.Title, archiveValue, micros, threadID, expectedRevision)
		if err != nil {
			return repositoryWriteError("mutate thread", err)
		}
		affected, err := update.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(ErrStaleRevision, "mutate thread", errors.New("thread changed concurrently"))
		}
		_, err = transaction.sql.ExecContext(ctx, `INSERT INTO thread_mutations (
			workspace_id, thread_id, operation, idempotency_key, request_fingerprint,
			result_title, result_archived, result_revision,
			result_updated_at_unix_micros, created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, current.WorkspaceID, threadID,
			operation, key, fingerprint, result.Title, result.Archived,
			result.Revision, micros, micros)
		if err := repositoryWriteError("record thread mutation", err); err != nil {
			return err
		}
		sessionID, err := sessionIDForThreadTransaction(ctx, transaction, threadID)
		if err != nil {
			return err
		}
		newEvent := events.NewSessionEvent{SessionID: sessionID, ThreadID: threadID,
			Revision: result.Revision, PayloadVersion: 1}
		switch operation {
		case "rename":
			newEvent.Kind = events.KindThreadRenamed
			newEvent.Payload = events.Payload{ThreadRenamed: &events.ThreadRenamed{
				PreviousTitle: current.Title, Title: result.Title,
			}}
		case "archive":
			newEvent.Kind = events.KindThreadArchived
			newEvent.Payload = events.Payload{ThreadArchived: &events.ThreadArchived{Archived: result.Archived}}
		}
		event, err := repositories.AppendSessionEvent(ctx, transaction, newEvent)
		if err != nil {
			return err
		}
		committedEvents = append(committedEvents, event)
		return nil
	})
	return ThreadCommit{Thread: result, Events: committedEvents}, err
}

func sessionIDForThreadTransaction(ctx context.Context, transaction *Transaction, threadID domain.ThreadID) (domain.SessionID, error) {
	var sessionID domain.SessionID
	err := transaction.sql.QueryRowContext(ctx, `SELECT id FROM sessions WHERE thread_id = ?`, threadID).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SessionID{}, typedError(ErrNotFound, "find thread session", err)
	}
	if err != nil {
		return domain.SessionID{}, classify("find thread session", err)
	}
	return sessionID, nil
}

func threadMutationFingerprint(operation string, revision uint64, title string, archived *bool) string {
	archive := ""
	if archived != nil {
		archive = strconv.FormatBool(*archived)
	}
	sum := sha256.Sum256([]byte(operation + "\x00" + strconv.FormatUint(revision, 10) + "\x00" + strings.TrimSpace(title) + "\x00" + archive))
	return hex.EncodeToString(sum[:])
}

func findThreadMutation(ctx context.Context, transaction *Transaction, threadID domain.ThreadID, operation, key string) (Thread, bool, string, error) {
	var thread Thread
	var archived int
	var createdMicros, updatedMicros int64
	var fingerprint string
	err := transaction.sql.QueryRowContext(ctx, `SELECT threads.id, threads.project_id,
		threads.repository_id, thread_mutations.workspace_id, sessions.id,
		thread_mutations.result_title, thread_mutations.result_archived,
		threads.created_at_unix_micros, thread_mutations.result_updated_at_unix_micros,
		thread_mutations.result_revision, thread_mutations.request_fingerprint
		FROM thread_mutations JOIN threads ON threads.id = thread_mutations.thread_id
		JOIN sessions ON sessions.thread_id = threads.id
		WHERE thread_mutations.thread_id = ? AND thread_mutations.operation = ?
		  AND thread_mutations.idempotency_key = ?`, threadID, operation, key).Scan(
		&thread.ID, &thread.ProjectID, &thread.RepositoryID, &thread.WorkspaceID,
		&thread.SessionID, &thread.Title, &archived, &createdMicros, &updatedMicros,
		&thread.Revision, &fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return Thread{}, false, "", nil
	}
	if err != nil {
		return Thread{}, false, "", classify("find thread mutation", err)
	}
	thread.CreatedAt = repositoryTime(createdMicros)
	thread.UpdatedAt = repositoryTime(updatedMicros)
	thread.Archived = archived == 1
	return thread, true, fingerprint, nil
}
