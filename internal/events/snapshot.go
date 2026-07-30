package events

import (
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

var (
	// ErrStaleEntityRevision means the client or replay base predates an
	// already-applied correctness-bearing entity revision.
	ErrStaleEntityRevision = errors.New("session entity revision is stale")
	// ErrEntityRevisionGap means durable history skipped an entity revision.
	ErrEntityRevisionGap = errors.New("session entity revision contains a gap")
)

// SessionSnapshot is one immutable reconnect base through a durable sequence.
type SessionSnapshot struct {
	SessionID       domain.SessionID `json:"session_id"`
	ThreadID        domain.ThreadID  `json:"thread_id"`
	ThroughSequence uint64           `json:"through_sequence"`
	TaskID          *domain.TaskID   `json:"task_id,omitempty"`
	TaskState       domain.TaskState `json:"task_state,omitempty"`
	TaskRevision    uint64           `json:"task_revision"`
	SnapshotVersion uint32           `json:"snapshot_version"`
	CreatedAt       time.Time        `json:"created_at"`
}

// Validate checks that a snapshot is canonical and versioned.
func (snapshot SessionSnapshot) Validate() error {
	switch {
	case snapshot.SessionID.IsZero():
		return errors.New("session snapshot session ID must not be empty")
	case snapshot.ThreadID.IsZero():
		return errors.New("session snapshot thread ID must not be empty")
	case snapshot.SnapshotVersion != 1:
		return errors.New("session snapshot version is unsupported")
	case snapshot.CreatedAt.IsZero():
		return errors.New("session snapshot timestamp must not be zero")
	case snapshot.CreatedAt.Location() != time.UTC:
		return errors.New("session snapshot timestamp must be UTC")
	case snapshot.TaskID == nil:
		if snapshot.TaskState != "" || snapshot.TaskRevision != 0 {
			return errors.New("session snapshot task fields are incomplete")
		}
	case snapshot.TaskID.IsZero():
		return errors.New("session snapshot task ID must not be empty")
	case !snapshot.TaskState.IsValid():
		return errors.New("session snapshot task state is invalid")
	}
	return nil
}

// ReduceTaskEvents deterministically advances one task snapshot. It rejects
// duplicate, stale, gapped, mismatched, or out-of-order correctness facts.
func ReduceTaskEvents(
	base SessionSnapshot,
	stream []SessionEvent,
) (SessionSnapshot, error) {
	if err := base.Validate(); err != nil {
		return SessionSnapshot{}, fmt.Errorf("validate replay base: %w", err)
	}
	result := base
	for _, event := range stream {
		if err := event.Validate(); err != nil {
			return SessionSnapshot{}, err
		}
		if event.SessionID != result.SessionID ||
			event.ThreadID != result.ThreadID {
			return SessionSnapshot{}, errors.New("session replay identity mismatch")
		}
		if event.Sequence <= result.ThroughSequence {
			return SessionSnapshot{}, fmt.Errorf(
				"%w: event sequence %d is not after %d",
				ErrStaleEntityRevision,
				event.Sequence,
				result.ThroughSequence,
			)
		}
		if event.Sequence != result.ThroughSequence+1 {
			return SessionSnapshot{}, errors.New("session replay sequence contains a gap")
		}
		result.ThroughSequence = event.Sequence
		if event.Kind != KindTaskStateChanged {
			continue
		}
		if event.TaskID == nil {
			return SessionSnapshot{}, errors.New("task transition event has no task ID")
		}
		if result.TaskID == nil {
			taskID := *event.TaskID
			result.TaskID = &taskID
			result.TaskState = event.Payload.TaskStateChanged.From
		} else if *event.TaskID != *result.TaskID {
			return SessionSnapshot{}, errors.New("session replay task identity mismatch")
		}
		if event.Revision <= result.TaskRevision {
			return SessionSnapshot{}, fmt.Errorf(
				"%w: event revision %d is not after %d",
				ErrStaleEntityRevision,
				event.Revision,
				result.TaskRevision,
			)
		}
		if event.Revision != result.TaskRevision+1 {
			return SessionSnapshot{}, fmt.Errorf(
				"%w: event revision %d does not follow %d",
				ErrEntityRevisionGap,
				event.Revision,
				result.TaskRevision,
			)
		}
		if event.Payload.TaskStateChanged.From != result.TaskState {
			return SessionSnapshot{}, errors.New("task transition replay state mismatch")
		}
		result.TaskState = event.Payload.TaskStateChanged.To
		result.TaskRevision = event.Revision
	}
	return result, nil
}

// DetectStaleRevision rejects a client revision older or newer than the
// authoritative revision while allowing an exact match.
func DetectStaleRevision(client, authoritative uint64) error {
	if client == authoritative {
		return nil
	}
	if client < authoritative {
		return fmt.Errorf(
			"%w: client revision %d, authoritative revision %d",
			ErrStaleEntityRevision,
			client,
			authoritative,
		)
	}
	return fmt.Errorf(
		"%w: client revision %d exceeds authoritative revision %d",
		ErrEntityRevisionGap,
		client,
		authoritative,
	)
}
