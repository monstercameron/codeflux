package gitwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

const editBatchAppliedEventType = "edit_batch_applied"

// TaskEventAppender is the narrow durable task-event repository boundary.
type TaskEventAppender interface {
	AppendTaskEvent(context.Context, storage.AppendTaskEvent) (storage.TaskEvent, error)
}

// StorageEditEventRecorder persists redacted edit summaries in the ordered task
// event journal.
type StorageEditEventRecorder struct {
	events TaskEventAppender
}

func NewStorageEditEventRecorder(events TaskEventAppender) (*StorageEditEventRecorder, error) {
	if events == nil {
		return nil, errors.New("task event appender is required")
	}
	return &StorageEditEventRecorder{events: events}, nil
}

func (recorder *StorageEditEventRecorder) RecordEditSummary(
	ctx context.Context,
	summary RedactedEditSummary,
) error {
	if summary.TaskID.IsZero() || len(summary.BatchSHA256) != 64 {
		return errors.New("edit summary is incomplete")
	}
	payload, err := json.Marshal(struct {
		BatchSHA256 string `json:"batch_sha256"`
		Created     int    `json:"created"`
		Updated     int    `json:"updated"`
		Renamed     int    `json:"renamed"`
		Deleted     int    `json:"deleted"`
		FileCount   int    `json:"file_count"`
	}{
		BatchSHA256: summary.BatchSHA256,
		Created:     summary.Created,
		Updated:     summary.Updated,
		Renamed:     summary.Renamed,
		Deleted:     summary.Deleted,
		FileCount:   summary.FileCount,
	})
	if err != nil {
		return fmt.Errorf("encode edit summary event: %w", err)
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		return fmt.Errorf("create edit summary event identity: %w", err)
	}
	_, err = recorder.events.AppendTaskEvent(ctx, storage.AppendTaskEvent{
		ID:             eventID,
		TaskID:         summary.TaskID,
		EventType:      editBatchAppliedEventType,
		PayloadJSON:    string(payload),
		IdempotencyKey: "edit-batch/" + summary.BatchSHA256,
	})
	return err
}
