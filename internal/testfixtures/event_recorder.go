package testfixtures

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// RecordedEvent is one event observed by the recorder (M22-109).
type RecordedEvent struct {
	Sequence uint64
	Kind     string
	// CausationID names the event that caused this one, empty for a root.
	CausationID string
	// ID is this event's own identity, so causation can be resolved.
	ID string
	// TransactionID groups events committed together. Events sharing a
	// transaction must become visible together or not at all.
	TransactionID string
	// Published records whether the event reached subscribers, as distinct
	// from having been committed. The gap between the two is where duplicate
	// and lost deliveries live.
	Published bool
	At        time.Time
}

// EventRecorder observes an event stream and answers the questions a test
// actually needs to ask about it (M22-109).
//
// It exists because "the event happened" is the weakest possible assertion.
// The recorder can additionally establish that events arrived in order, that
// every non-root event has a cause that was itself recorded, that a
// transaction became visible atomically, and that a wait for an expected event
// fails with a useful message instead of hanging.
type EventRecorder struct {
	mutex    sync.Mutex
	events   []RecordedEvent
	byID     map[string]int
	waiters  []eventWaiter
	clock    Clock
	nextSeq  uint64
	finished bool
}

type eventWaiter struct {
	predicate func(RecordedEvent) bool
	notify    chan RecordedEvent
}

// NewEventRecorder builds a recorder stamped by clock.
func NewEventRecorder(clock Clock) *EventRecorder {
	if clock == nil {
		clock = NewFixedClock()
	}
	return &EventRecorder{byID: map[string]int{}, clock: clock}
}

// Record appends one observed event, assigning the next sequence.
func (recorder *EventRecorder) Record(event RecordedEvent) (RecordedEvent, error) {
	if strings.TrimSpace(event.ID) == "" {
		return RecordedEvent{}, errors.New("a recorded event requires an identity")
	}
	if strings.TrimSpace(event.Kind) == "" {
		return RecordedEvent{}, errors.New("a recorded event requires a kind")
	}
	recorder.mutex.Lock()
	if recorder.finished {
		recorder.mutex.Unlock()
		return RecordedEvent{}, errors.New("recorder is finished and accepts no further events")
	}
	if _, duplicate := recorder.byID[event.ID]; duplicate {
		recorder.mutex.Unlock()
		return RecordedEvent{}, fmt.Errorf("event %q was recorded twice", event.ID)
	}
	recorder.nextSeq++
	event.Sequence = recorder.nextSeq
	event.At = recorder.clock.Now()
	recorder.byID[event.ID] = len(recorder.events)
	recorder.events = append(recorder.events, event)

	// Notify waiters outside the critical section's data mutation but while
	// still holding the lock, so a waiter cannot miss an event recorded
	// between its predicate check and its subscription.
	remaining := recorder.waiters[:0]
	for _, waiter := range recorder.waiters {
		if waiter.predicate(event) {
			waiter.notify <- event
			close(waiter.notify)
			continue
		}
		remaining = append(remaining, waiter)
	}
	recorder.waiters = remaining
	recorder.mutex.Unlock()
	return event, nil
}

// Events returns everything recorded, in order.
func (recorder *EventRecorder) Events() []RecordedEvent {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	events := make([]RecordedEvent, len(recorder.events))
	copy(events, recorder.events)
	return events
}

// AssertSequenceIsContiguous checks the recorded sequence has no gap or
// repeat. A gap means an event was lost; a repeat means one was delivered
// twice. docs/plan.md sets the acceptable count of both to zero.
func (recorder *EventRecorder) AssertSequenceIsContiguous() error {
	for index, event := range recorder.Events() {
		expected := uint64(index + 1)
		if event.Sequence != expected {
			return fmt.Errorf(
				"event %q has sequence %d at position %d: the stream has a gap or a repeat",
				event.ID, event.Sequence, expected)
		}
	}
	return nil
}

// AssertCausationIsResolvable checks every non-root event names a cause that
// was itself recorded, and that no event causes itself.
//
// An unresolvable causation chain is how a timeline ends up showing an effect
// with no visible reason, which is precisely the confusion the product exists
// to prevent.
func (recorder *EventRecorder) AssertCausationIsResolvable() error {
	events := recorder.Events()
	positions := make(map[string]int, len(events))
	for index, event := range events {
		positions[event.ID] = index
	}
	for index, event := range events {
		if event.CausationID == "" {
			continue
		}
		if event.CausationID == event.ID {
			return fmt.Errorf("event %q causes itself", event.ID)
		}
		cause, ok := positions[event.CausationID]
		if !ok {
			return fmt.Errorf("event %q names cause %q, which was never recorded",
				event.ID, event.CausationID)
		}
		if cause >= index {
			return fmt.Errorf("event %q is caused by %q, which came after it",
				event.ID, event.CausationID)
		}
	}
	return nil
}

// AssertTransactionIsAtomic checks every event of a transaction is either all
// published or none published.
//
// A partially published transaction is the shape of a durable write that a
// subscriber saw half of, which is how a client ends up with state the server
// never committed.
func (recorder *EventRecorder) AssertTransactionIsAtomic(transactionID string) error {
	if strings.TrimSpace(transactionID) == "" {
		return errors.New("a transaction assertion requires a transaction identity")
	}
	var published, total int
	for _, event := range recorder.Events() {
		if event.TransactionID != transactionID {
			continue
		}
		total++
		if event.Published {
			published++
		}
	}
	if total == 0 {
		return fmt.Errorf("transaction %q recorded no events", transactionID)
	}
	if published != 0 && published != total {
		return fmt.Errorf(
			"transaction %q published %d of %d events: a subscriber saw part of a transaction",
			transactionID, published, total)
	}
	return nil
}

// AssertReplayMatches checks a replayed stream reproduces the recorded one
// exactly, by kind and order.
//
// Identity is deliberately not compared: a replay may legitimately re-issue
// events under new identities. Order and kind are what a user's timeline is
// made of, and are what must survive.
func (recorder *EventRecorder) AssertReplayMatches(replayed []RecordedEvent) error {
	original := recorder.Events()
	if len(replayed) != len(original) {
		return fmt.Errorf("replay produced %d events, recorded stream had %d",
			len(replayed), len(original))
	}
	for index := range original {
		if original[index].Kind != replayed[index].Kind {
			return fmt.Errorf("replay position %d is %q, recorded stream had %q",
				index, replayed[index].Kind, original[index].Kind)
		}
		if original[index].Sequence != replayed[index].Sequence {
			return fmt.Errorf("replay position %d has sequence %d, recorded stream had %d",
				index, replayed[index].Sequence, original[index].Sequence)
		}
	}
	return nil
}

// ErrWaitTimeout is returned when an awaited event never arrives.
var ErrWaitTimeout = errors.New("timed out waiting for an event")

// WaitFor blocks until an event satisfying predicate is recorded, or the
// context ends.
//
// The timeout path reports what WAS recorded, because "timed out waiting for
// an event" without that list is the least useful failure message a test suite
// can produce: it says something is wrong and nothing about what.
func (recorder *EventRecorder) WaitFor(
	ctx context.Context,
	description string,
	predicate func(RecordedEvent) bool,
) (RecordedEvent, error) {
	if predicate == nil {
		return RecordedEvent{}, errors.New("a wait requires a predicate")
	}
	recorder.mutex.Lock()
	for _, event := range recorder.events {
		if predicate(event) {
			recorder.mutex.Unlock()
			return event, nil
		}
	}
	notify := make(chan RecordedEvent, 1)
	recorder.waiters = append(recorder.waiters, eventWaiter{predicate: predicate, notify: notify})
	recorder.mutex.Unlock()

	select {
	case event := <-notify:
		return event, nil
	case <-ctx.Done():
		return RecordedEvent{}, fmt.Errorf("%w: %s; recorded so far: %s",
			ErrWaitTimeout, description, recorder.summary())
	}
}

func (recorder *EventRecorder) summary() string {
	events := recorder.Events()
	if len(events) == 0 {
		return "(nothing)"
	}
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, fmt.Sprintf("%d:%s", event.Sequence, event.Kind))
	}
	return strings.Join(kinds, ", ")
}

// Finish stops the recorder and fails any outstanding waiter, so a test that
// forgot to satisfy a wait fails rather than hanging until the suite timeout.
func (recorder *EventRecorder) Finish() {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.finished = true
	for _, waiter := range recorder.waiters {
		close(waiter.notify)
	}
	recorder.waiters = nil
}
