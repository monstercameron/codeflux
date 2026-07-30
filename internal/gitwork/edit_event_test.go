package gitwork

import (
	"context"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/storage"
)

func TestStorageEditEventRecorderPersistsOnlyRedactedSummary(t *testing.T) {
	t.Parallel()

	appender := &capturingTaskEventAppender{}
	recorder, err := NewStorageEditEventRecorder(appender)
	if err != nil {
		t.Fatal(err)
	}
	summary := RedactedEditSummary{
		TaskID: fixtureTaskID(t, 100), BatchSHA256: strings.Repeat("a", 64),
		Created: 1, Updated: 2, Renamed: 3, Deleted: 4, FileCount: 10,
	}
	if err := recorder.RecordEditSummary(t.Context(), summary); err != nil {
		t.Fatal(err)
	}
	if appender.input.TaskID != summary.TaskID ||
		appender.input.EventType != editBatchAppliedEventType ||
		appender.input.IdempotencyKey != "edit-batch/"+summary.BatchSHA256 {
		t.Fatalf("event input = %#v", appender.input)
	}
	if strings.Contains(appender.input.PayloadJSON, "path") ||
		strings.Contains(appender.input.PayloadJSON, "content") {
		t.Fatalf("event payload retained repository data: %s", appender.input.PayloadJSON)
	}
}

type capturingTaskEventAppender struct {
	input storage.AppendTaskEvent
}

func (appender *capturingTaskEventAppender) AppendTaskEvent(
	_ context.Context,
	input storage.AppendTaskEvent,
) (storage.TaskEvent, error) {
	appender.input = input
	return storage.TaskEvent{}, nil
}
