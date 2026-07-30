package events

import (
	"errors"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestReduceTaskEventsReconstructsExactStateAndRejectsStaleRevision(t *testing.T) {
	ids := newEventTestIDs(t)
	base := SessionSnapshot{
		SessionID:       ids.session,
		ThreadID:        ids.thread,
		TaskID:          &ids.task,
		TaskState:       domain.TaskStateDraft,
		SnapshotVersion: 1,
		CreatedAt:       time.UnixMicro(1).UTC(),
	}
	transition := SessionEvent{
		Sequence:       1,
		SessionID:      ids.session,
		ThreadID:       ids.thread,
		TaskID:         &ids.task,
		Timestamp:      time.UnixMicro(2).UTC(),
		Kind:           KindTaskStateChanged,
		Revision:       1,
		PayloadVersion: 1,
		Payload: Payload{TaskStateChanged: &TaskStateChanged{
			From: domain.TaskStateDraft,
			To:   domain.TaskStateForecasting,
		}},
	}
	message := hubMessageFinal(t, ids, 2, "after transition")
	reduced, err := ReduceTaskEvents(base, []SessionEvent{transition, message})
	if err != nil {
		t.Fatal(err)
	}
	if reduced.ThroughSequence != 2 ||
		reduced.TaskState != domain.TaskStateForecasting ||
		reduced.TaskRevision != 1 {
		t.Fatalf("reduced snapshot = %#v", reduced)
	}
	if _, err := ReduceTaskEvents(reduced, []SessionEvent{message}); !errors.Is(
		err,
		ErrStaleEntityRevision,
	) {
		t.Fatalf("stale replay error = %v", err)
	}
	if err := DetectStaleRevision(0, 1); !errors.Is(err, ErrStaleEntityRevision) {
		t.Fatalf("client stale error = %v", err)
	}
	if err := DetectStaleRevision(2, 1); !errors.Is(err, ErrEntityRevisionGap) {
		t.Fatalf("client ahead error = %v", err)
	}
}

func TestReduceTaskEventsRejectsRevisionAndSequenceGaps(t *testing.T) {
	ids := newEventTestIDs(t)
	base := SessionSnapshot{
		SessionID:       ids.session,
		ThreadID:        ids.thread,
		TaskID:          &ids.task,
		TaskState:       domain.TaskStateDraft,
		SnapshotVersion: 1,
		CreatedAt:       time.UnixMicro(1).UTC(),
	}
	gapped := SessionEvent{
		Sequence:       2,
		SessionID:      ids.session,
		ThreadID:       ids.thread,
		TaskID:         &ids.task,
		Timestamp:      time.UnixMicro(2).UTC(),
		Kind:           KindTaskStateChanged,
		Revision:       2,
		PayloadVersion: 1,
		Payload: Payload{TaskStateChanged: &TaskStateChanged{
			From: domain.TaskStateDraft,
			To:   domain.TaskStateForecasting,
		}},
	}
	if _, err := ReduceTaskEvents(base, []SessionEvent{gapped}); err == nil {
		t.Fatal("sequence gap was accepted")
	}
	gapped.Sequence = 1
	if _, err := ReduceTaskEvents(base, []SessionEvent{gapped}); !errors.Is(
		err,
		ErrEntityRevisionGap,
	) {
		t.Fatalf("revision gap error = %v", err)
	}
}
