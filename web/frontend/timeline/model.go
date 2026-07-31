// Package timeline owns deterministic, browser-independent timeline reducers.
package timeline

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"time"

	"codeflux.dev/codeflux/internal/events"
)

// ItemKey is a stable presentation identity. Messages use their durable
// message identity; grouped activity uses its durable activity identity; all
// other events use their durable sequence.
type ItemKey string

// MessageProjection is one streamed or durable message presentation.
type MessageProjection struct {
	ID          string
	Role        string
	Body        string
	Provisional bool
	Final       bool
}

// ToolProjection updates in place for one execution identity.
type ToolProjection struct {
	ExecutionID string
	Command     string
	State       string
	Summary     string
}

// ApprovalProjection updates in place without losing its attributable result.
type ApprovalProjection struct {
	ID     string
	State  string
	Scope  string
	Reason string
}

// Item groups related events without changing the order established by the
// first durable sequence.
type Item struct {
	Key           ItemKey
	FirstSequence uint64
	LastSequence  uint64
	Timestamp     time.Time
	Kinds         []events.Kind
	Sequences     []uint64
	Message       *MessageProjection
	Tool          *ToolProjection
	Approval      *ApprovalProjection
	Events        []events.SessionEvent
}

// SequenceGap is one unresolved inclusive sequence range.
type SequenceGap struct {
	After  uint64
	Before uint64
}

// State is the immutable reducer output for one selected thread.
type State struct {
	Events           []events.SessionEvent
	Items            []Item
	Gaps             []SequenceGap
	ReachedBeginning bool
	HasOlder         bool
}

// Page is one bounded page or replay segment. Events may arrive in any order;
// durable sequence is authoritative.
type Page struct {
	Events           []events.SessionEvent
	HasOlder         bool
	ReachedBeginning bool
}

// MergeThreadPage joins pagination and replay, rejects conflicting duplicates,
// and deterministically rebuilds presentation groups.
func MergeThreadPage(current State, page Page) (State, error) {
	bySequence := make(map[uint64]events.SessionEvent, len(current.Events)+len(page.Events))
	var session, thread string
	merge := func(event events.SessionEvent) error {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("timeline event %d: %w", event.Sequence, err)
		}
		if session == "" {
			session = event.SessionID.String()
			thread = event.ThreadID.String()
		} else if event.SessionID.String() != session || event.ThreadID.String() != thread {
			return fmt.Errorf("timeline event %d belongs to another session or thread", event.Sequence)
		}
		if existing, ok := bySequence[event.Sequence]; ok {
			if !reflect.DeepEqual(existing, event) {
				return fmt.Errorf("conflicting event at durable sequence %d", event.Sequence)
			}
			return nil
		}
		bySequence[event.Sequence] = event
		return nil
	}
	for _, event := range current.Events {
		if err := merge(event); err != nil {
			return current, err
		}
	}
	for _, event := range page.Events {
		if err := merge(event); err != nil {
			return current, err
		}
	}

	merged := make([]events.SessionEvent, 0, len(bySequence))
	for _, event := range bySequence {
		merged = append(merged, event)
	}
	sort.Slice(merged, func(left, right int) bool {
		return merged[left].Sequence < merged[right].Sequence
	})
	items, err := GroupEvents(merged)
	if err != nil {
		return current, err
	}
	reachedBeginning := current.ReachedBeginning || page.ReachedBeginning
	return State{
		Events:           merged,
		Items:            items,
		Gaps:             findGaps(merged),
		ReachedBeginning: reachedBeginning,
		// Replay segments do not own the pagination cursor and commonly omit
		// HasOlder. Preserve an existing older-page affordance until the server
		// explicitly confirms the beginning of this thread.
		HasOlder: !reachedBeginning && (current.HasOlder || page.HasOlder),
	}, nil
}

// ApplyMessageDelta is the pure single-event reducer used by live delivery.
func ApplyMessageDelta(current State, event events.SessionEvent) (State, error) {
	if event.Kind != events.KindMessageDelta {
		return current, fmt.Errorf("message delta reducer received %q", event.Kind)
	}
	return MergeThreadPage(current, Page{Events: []events.SessionEvent{event}, HasOlder: current.HasOlder})
}

// FinalizeMessage replaces accumulated provisional text with durable content.
func FinalizeMessage(current State, event events.SessionEvent) (State, error) {
	if event.Kind != events.KindMessageFinal {
		return current, fmt.Errorf("message final reducer received %q", event.Kind)
	}
	return MergeThreadPage(current, Page{Events: []events.SessionEvent{event}, HasOlder: current.HasOlder})
}

// GroupEvents preserves durable order while folding message, tool, and
// approval lifecycles into one stable presentation item.
func GroupEvents(ordered []events.SessionEvent) ([]Item, error) {
	eventsCopy := slices.Clone(ordered)
	sort.Slice(eventsCopy, func(left, right int) bool {
		return eventsCopy[left].Sequence < eventsCopy[right].Sequence
	})
	items := make([]Item, 0, len(eventsCopy))
	index := make(map[ItemKey]int, len(eventsCopy))
	lastSequence := uint64(0)
	for _, event := range eventsCopy {
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("group timeline event %d: %w", event.Sequence, err)
		}
		if event.Sequence == lastSequence {
			return nil, fmt.Errorf("duplicate sequence %d must be resolved before grouping", event.Sequence)
		}
		lastSequence = event.Sequence
		key := presentationKey(event)
		position, exists := index[key]
		if !exists {
			position = len(items)
			index[key] = position
			items = append(items, Item{
				Key: key, FirstSequence: event.Sequence, Timestamp: event.Timestamp,
			})
		}
		item := &items[position]
		item.LastSequence = event.Sequence
		item.Kinds = append(item.Kinds, event.Kind)
		item.Sequences = append(item.Sequences, event.Sequence)
		item.Events = append(item.Events, event)
		applyProjection(item, event)
	}
	return items, nil
}

func presentationKey(event events.SessionEvent) ItemKey {
	switch event.Kind {
	case events.KindMessageDelta:
		return ItemKey("message:" + event.Payload.MessageDelta.MessageID.String())
	case events.KindMessageFinal:
		return ItemKey("message:" + event.Payload.MessageFinal.MessageID.String())
	case events.KindToolStarted, events.KindToolProgress, events.KindToolCompleted:
		return ItemKey("tool:" + event.Payload.Tool.ExecutionID)
	case events.KindApprovalRequested, events.KindApprovalResolved:
		return ItemKey("approval:" + event.Payload.Approval.ApprovalID.String())
	case events.KindForecastUpdated:
		return ItemKey("forecast:current")
	case events.KindUsageUpdated:
		return ItemKey("usage:current")
	case events.KindCostUpdated, events.KindBudgetUpdated:
		return ItemKey("cost-budget:current")
	case events.KindValidationUpdated:
		return ItemKey("validation:" + event.Payload.Validation.ValidationID.String())
	case events.KindGraphSnapshot, events.KindGraphPatch:
		return ItemKey("graph:" + event.Payload.Graph.RevisionID.String())
	case events.KindCheckpointCreated:
		return ItemKey("checkpoint:" + event.Payload.Checkpoint.CheckpointID.String())
	default:
		return ItemKey("sequence:" + strconv.FormatUint(event.Sequence, 10))
	}
}

func applyProjection(item *Item, event events.SessionEvent) {
	switch event.Kind {
	case events.KindMessageDelta:
		payload := event.Payload.MessageDelta
		if item.Message == nil {
			item.Message = &MessageProjection{ID: payload.MessageID.String()}
		}
		item.Message.Body += payload.RedactedDelta
		item.Message.Provisional = true
		item.Message.Final = false
	case events.KindMessageFinal:
		payload := event.Payload.MessageFinal
		item.Message = &MessageProjection{
			ID: payload.MessageID.String(), Role: payload.Role,
			Body: payload.RedactedBody, Final: true,
		}
	case events.KindToolStarted, events.KindToolProgress, events.KindToolCompleted:
		payload := event.Payload.Tool
		item.Tool = &ToolProjection{
			ExecutionID: payload.ExecutionID, Command: payload.CommandName,
			State: payload.State, Summary: payload.RedactedSummary,
		}
	case events.KindApprovalRequested, events.KindApprovalResolved:
		payload := event.Payload.Approval
		item.Approval = &ApprovalProjection{
			ID: payload.ApprovalID.String(), State: string(payload.State),
			Scope: payload.Scope, Reason: payload.RedactedReason,
		}
	}
}

func findGaps(ordered []events.SessionEvent) []SequenceGap {
	var gaps []SequenceGap
	for index := 1; index < len(ordered); index++ {
		previous := ordered[index-1].Sequence
		current := ordered[index].Sequence
		if current > previous+1 {
			gaps = append(gaps, SequenceGap{After: previous, Before: current})
		}
	}
	return gaps
}

// BeginningVisible reports whether a truthful beginning marker can be shown.
func (state State) BeginningVisible() bool {
	// Durable sequence is session-wide, so the first event belonging to a
	// thread is not required to have sequence one. The server's bounded page
	// result is the authority for whether the thread beginning was reached.
	return state.ReachedBeginning && len(state.Events) > 0
}
