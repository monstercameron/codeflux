package storage

import (
	"context"
	"database/sql"
	"errors"

	"codeflux.dev/codeflux/internal/events"
)

// AppendMessageAndDraftTask commits the user message and optional draft task
// together, including on idempotent retries. A task failure rolls back the
// message, attachments, and thread revision advance.
func (repositories *Repositories) AppendMessageAndDraftTask(
	ctx context.Context,
	input AppendMessageAndDraftTask,
) (AppendedMessageAndDraftTask, error) {
	if err := validateAppendMessageInput(input.Message); err != nil {
		return AppendedMessageAndDraftTask{}, err
	}
	if input.DraftTask != nil {
		if err := validateCreateTaskInput(*input.DraftTask); err != nil {
			return AppendedMessageAndDraftTask{}, err
		}
		if input.DraftTask.ThreadID != input.Message.ThreadID ||
			input.DraftTask.IdempotencyKey != input.Message.IdempotencyKey {
			return AppendedMessageAndDraftTask{}, errors.New("draft task must share the message thread and idempotency key")
		}
	}
	now, micros := repositories.timestamp()
	var result AppendedMessageAndDraftTask
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, messageFound, err := findMessageByIdempotency(
			ctx, transaction, input.Message.ThreadID, input.Message.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if messageFound {
			_, taskFound, err := findTaskByIdempotency(
				ctx, transaction, input.Message.ThreadID, input.Message.IdempotencyKey,
			)
			if err != nil {
				return err
			}
			if (input.DraftTask != nil) != taskFound {
				return typedError(ErrConflict, "replay message and draft task",
					errors.New("idempotency key belongs to a different draft-task choice"))
			}
		} else {
			var revision uint64
			err := transaction.sql.QueryRowContext(ctx, `SELECT revision FROM threads
				WHERE id = ? AND deleted_at_unix_micros IS NULL
				  AND archived_at_unix_micros IS NULL`, input.Message.ThreadID).Scan(&revision)
			if errors.Is(err, sql.ErrNoRows) {
				return typedError(ErrNotFound, "check thread before message", err)
			}
			if err != nil {
				return classify("check thread before message", err)
			}
			if revision != input.ExpectedRevision {
				return typedError(ErrStaleRevision, "check thread before message",
					errors.New("expected revision does not match"))
			}
		}
		message, err := appendMessageTransaction(ctx, transaction, input.Message, now, micros)
		if err != nil {
			return err
		}
		result.Message = message
		if input.DraftTask == nil {
			if messageFound {
				return nil
			}
			return repositories.appendMessageFinalEvent(ctx, transaction, message, &result)
		}
		draftInput := *input.DraftTask
		draftInput.RequestMessageID = &message.ID
		task, err := createTaskTransaction(ctx, transaction, draftInput, now, micros)
		if err != nil {
			return err
		}
		result.DraftTask = &task
		if messageFound {
			return nil
		}
		return repositories.appendMessageFinalEvent(ctx, transaction, message, &result)
	})
	return result, err
}

func (repositories *Repositories) appendMessageFinalEvent(
	ctx context.Context,
	transaction *Transaction,
	message Message,
	result *AppendedMessageAndDraftTask,
) error {
	sessionID, err := sessionIDForThreadTransaction(ctx, transaction, message.ThreadID)
	if err != nil {
		return err
	}
	event, err := repositories.AppendSessionEvent(ctx, transaction, events.NewSessionEvent{
		SessionID: sessionID, ThreadID: message.ThreadID, Kind: events.KindMessageFinal,
		Revision: message.Sequence, PayloadVersion: 1,
		Payload: events.Payload{MessageFinal: &events.MessageFinal{
			MessageID: message.ID, Role: string(message.Role), RedactedBody: message.BodyRedacted,
		}},
	})
	if err != nil {
		return err
	}
	result.Events = append(result.Events, event)
	return nil
}
