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
	SessionID                domain.SessionID  `json:"session_id"`
	ThreadID                 domain.ThreadID   `json:"thread_id"`
	ThroughSequence          uint64            `json:"through_sequence"`
	TaskID                   *domain.TaskID    `json:"task_id,omitempty"`
	TaskState                domain.TaskState  `json:"task_state,omitempty"`
	TaskRevision             uint64            `json:"task_revision"`
	Checkpoint               *Checkpoint       `json:"checkpoint,omitempty"`
	CheckpointRevision       uint64            `json:"checkpoint_revision"`
	Validation               *Validation       `json:"validation,omitempty"`
	ValidationRevision       uint64            `json:"validation_revision"`
	ChangeAcceptance         *ChangeAcceptance `json:"change_acceptance,omitempty"`
	ChangeAcceptanceRevision uint64            `json:"change_acceptance_revision"`
	SnapshotVersion          uint32            `json:"snapshot_version"`
	CreatedAt                time.Time         `json:"created_at"`
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
	if snapshot.Checkpoint != nil {
		if snapshot.CheckpointRevision == 0 || validateCheckpoint(snapshot.Checkpoint) != nil ||
			snapshot.Checkpoint.TaskRevision > snapshot.TaskRevision {
			return errors.New("session snapshot checkpoint is invalid")
		}
	} else if snapshot.CheckpointRevision != 0 {
		return errors.New("session snapshot checkpoint revision has no projection")
	}
	if snapshot.Validation != nil {
		if snapshot.ValidationRevision == 0 || validateValidation(snapshot.Validation) != nil {
			return errors.New("session snapshot validation is invalid")
		}
	} else if snapshot.ValidationRevision != 0 {
		return errors.New("session snapshot validation revision has no projection")
	}
	if snapshot.ChangeAcceptance != nil {
		if snapshot.ChangeAcceptanceRevision == 0 || validateChangeAcceptance(snapshot.ChangeAcceptance) != nil {
			return errors.New("session snapshot change acceptance is invalid")
		}
	} else if snapshot.ChangeAcceptanceRevision != 0 {
		return errors.New("session snapshot change acceptance revision has no projection")
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
	result := cloneSessionSnapshot(base)
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
		if err := applyServerProjectionEvent(&result, event); err != nil {
			return SessionSnapshot{}, err
		}
		result.ThroughSequence = event.Sequence
	}
	return result, nil
}

func cloneSessionSnapshot(source SessionSnapshot) SessionSnapshot {
	result := source
	if source.TaskID != nil {
		value := *source.TaskID
		result.TaskID = &value
	}
	if source.Checkpoint != nil {
		value := *source.Checkpoint
		result.Checkpoint = &value
	}
	if source.Validation != nil {
		value := *source.Validation
		result.Validation = &value
	}
	if source.ChangeAcceptance != nil {
		value := *source.ChangeAcceptance
		result.ChangeAcceptance = &value
	}
	return result
}

func applyServerProjectionEvent(snapshot *SessionSnapshot, event SessionEvent) error {
	switch event.Kind {
	case KindTaskStateChanged:
		if err := bindSnapshotTask(snapshot, event, event.Payload.TaskStateChanged.From); err != nil {
			return err
		}
		if err := requireNextEntityRevision(snapshot.TaskRevision, event.Revision); err != nil {
			return err
		}
		if event.Payload.TaskStateChanged.From != snapshot.TaskState {
			return errors.New("task transition replay state mismatch")
		}
		snapshot.TaskState = event.Payload.TaskStateChanged.To
		snapshot.TaskRevision = event.Revision
	case KindCheckpointCreated:
		if err := bindSnapshotTask(snapshot, event, snapshot.TaskState); err != nil {
			return err
		}
		if err := requireNextEntityRevision(snapshot.CheckpointRevision, event.Revision); err != nil {
			return err
		}
		if event.Payload.Checkpoint.TaskRevision > snapshot.TaskRevision {
			return errors.New("checkpoint task revision exceeds current task")
		}
		value := *event.Payload.Checkpoint
		snapshot.Checkpoint = &value
		snapshot.CheckpointRevision = event.Revision
	case KindValidationUpdated:
		if err := bindSnapshotTask(snapshot, event, snapshot.TaskState); err != nil {
			return err
		}
		if err := requireNextEntityRevision(snapshot.ValidationRevision, event.Revision); err != nil {
			return err
		}
		incoming := event.Payload.Validation
		if snapshot.Validation != nil {
			if !snapshot.Validation.State.IsTerminal() && snapshot.Validation.ValidationID != incoming.ValidationID {
				return errors.New("running validation identity changed")
			}
			if snapshot.Validation.ValidationID == incoming.ValidationID {
				if err := domain.ValidateValidationTransition(snapshot.Validation.State, incoming.State); err != nil {
					return err
				}
			} else if incoming.State != domain.ValidationStatePending && incoming.State != domain.ValidationStateRunning {
				return errors.New("new validation did not start pending or running")
			}
		} else if incoming.State != domain.ValidationStatePending && incoming.State != domain.ValidationStateRunning {
			return errors.New("initial validation did not start pending or running")
		}
		value := *incoming
		snapshot.Validation = &value
		snapshot.ValidationRevision = event.Revision
	case KindChangeAcceptanceUpdated:
		if err := bindSnapshotTask(snapshot, event, snapshot.TaskState); err != nil {
			return err
		}
		if err := requireNextEntityRevision(snapshot.ChangeAcceptanceRevision, event.Revision); err != nil {
			return err
		}
		incoming := event.Payload.ChangeAcceptance
		if snapshot.ChangeAcceptance == nil {
			if incoming.State != domain.ChangeAcceptanceStatePending {
				return errors.New("initial change acceptance is not pending")
			}
		} else {
			bindingsChangedForRepair := snapshot.ChangeAcceptance.State == domain.ChangeAcceptanceStateRepairRequested &&
				incoming.State == domain.ChangeAcceptanceStatePending
			if incoming.Bindings != snapshot.ChangeAcceptance.Bindings && !bindingsChangedForRepair {
				return errors.New("change acceptance bindings changed within review")
			}
			if err := domain.ValidateChangeAcceptanceTransition(snapshot.ChangeAcceptance.State, incoming.State); err != nil {
				return err
			}
		}
		value := *incoming
		snapshot.ChangeAcceptance = &value
		snapshot.ChangeAcceptanceRevision = event.Revision
	}
	return nil
}

func bindSnapshotTask(snapshot *SessionSnapshot, event SessionEvent, initial domain.TaskState) error {
	if event.TaskID == nil {
		return errors.New("task projection event has no task ID")
	}
	if snapshot.TaskID == nil {
		taskID := *event.TaskID
		snapshot.TaskID = &taskID
		snapshot.TaskState = initial
		return nil
	}
	if *event.TaskID != *snapshot.TaskID {
		return errors.New("session replay task identity mismatch")
	}
	return nil
}

func requireNextEntityRevision(current, incoming uint64) error {
	if incoming <= current {
		return fmt.Errorf("%w: event revision %d is not after %d", ErrStaleEntityRevision, incoming, current)
	}
	if incoming != current+1 {
		return fmt.Errorf("%w: event revision %d does not follow %d", ErrEntityRevisionGap, incoming, current)
	}
	return nil
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
