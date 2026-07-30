package events

import (
	"context"
	"errors"
	"io"
	"sync"

	"codeflux.dev/codeflux/internal/domain"
)

var (
	// ErrSlowSubscriber means a material event could not fit in a subscriber's
	// bounded queue. The subscriber must reconnect from its last sequence.
	ErrSlowSubscriber = errors.New("session subscriber is too slow")
	// ErrSnapshotRequired means the requested replay exceeds the bounded live
	// queue and must be satisfied by a snapshot before joining the stream.
	ErrSnapshotRequired = errors.New("session snapshot is required")
)

// Journal is the committed durable history required to join replay to live
// delivery. Implementations must return events in ascending sequence order.
type Journal interface {
	ReplayCommitted(
		context.Context,
		domain.SessionID,
		uint64,
		int,
	) ([]SessionEvent, error)
	CommittedSequence(context.Context, domain.SessionID) (uint64, error)
}

// SubscriptionQuery selects one session stream and optional thread/task scope.
type SubscriptionQuery struct {
	SessionID     domain.SessionID
	ThreadID      *domain.ThreadID
	TaskID        *domain.TaskID
	AfterSequence uint64
}

// Metrics are cumulative hub counters plus the current active count.
type Metrics struct {
	Active             uint64
	Published          uint64
	Delivered          uint64
	EphemeralCoalesced uint64
	ForcedDisconnects  uint64
}

// Hub joins committed replay to bounded in-process live delivery.
type Hub struct {
	journal  Journal
	capacity int

	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]*Subscription
	global      Metrics
	bySession   map[domain.SessionID]Metrics
}

// NewHub creates a hub with one fixed per-subscriber queue bound.
func NewHub(journal Journal, capacity int) (*Hub, error) {
	if journal == nil {
		return nil, errors.New("event journal must not be nil")
	}
	if capacity < 1 || capacity > 65_536 {
		return nil, errors.New("subscriber capacity must be between 1 and 65536")
	}
	return &Hub{
		journal:     journal,
		capacity:    capacity,
		subscribers: make(map[uint64]*Subscription),
		bySession:   make(map[domain.SessionID]Metrics),
	}, nil
}

// Subscribe establishes a committed boundary, replays through it, and only
// then exposes later live events. Publish is serialized across this boundary.
func (hub *Hub) Subscribe(
	ctx context.Context,
	query SubscriptionQuery,
) (*Subscription, error) {
	if ctx == nil {
		return nil, errors.New("subscription context must not be nil")
	}
	if query.SessionID.IsZero() {
		return nil, errors.New("subscription session ID must not be empty")
	}
	if query.ThreadID != nil && query.ThreadID.IsZero() {
		return nil, errors.New("subscription thread ID must not be empty")
	}
	if query.TaskID != nil && query.TaskID.IsZero() {
		return nil, errors.New("subscription task ID must not be empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	boundary, err := hub.journal.CommittedSequence(ctx, query.SessionID)
	if err != nil {
		return nil, err
	}
	if query.AfterSequence > boundary {
		return nil, errors.New("subscription sequence is ahead of committed history")
	}
	hub.nextID++
	subscription := &Subscription{
		id:        hub.nextID,
		hub:       hub,
		query:     query,
		liveAfter: boundary,
		capacity:  hub.capacity,
		signal:    make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
	after := query.AfterSequence
	for after < boundary {
		limit := hub.capacity
		if remaining := boundary - after; remaining < uint64(limit) {
			limit = int(remaining)
		}
		replayed, replayErr := hub.journal.ReplayCommitted(
			ctx,
			query.SessionID,
			after,
			limit,
		)
		if replayErr != nil {
			return nil, replayErr
		}
		if len(replayed) == 0 {
			return nil, errors.New("committed session history contains a sequence gap")
		}
		for _, event := range replayed {
			if event.Sequence > boundary {
				break
			}
			if !subscription.matches(event) {
				after = event.Sequence
				continue
			}
			delivered, _ := subscription.enqueueLocked(event)
			if !delivered {
				return nil, ErrSnapshotRequired
			}
			after = event.Sequence
		}
	}
	hub.subscribers[subscription.id] = subscription
	hub.global.Active++
	sessionMetrics := hub.bySession[query.SessionID]
	sessionMetrics.Active++
	hub.bySession[query.SessionID] = sessionMetrics
	subscription.stopContext = context.AfterFunc(ctx, func() {
		subscription.close(ctx.Err())
	})
	return subscription, nil
}

// PublishCommitted delivers an event that has already committed to Journal.
// Duplicate publication at or below a subscriber's replay boundary is ignored.
func (hub *Hub) PublishCommitted(event SessionEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.global.Published++
	sessionMetrics := hub.bySession[event.SessionID]
	sessionMetrics.Published++
	for id, subscription := range hub.subscribers {
		if subscription.query.SessionID != event.SessionID ||
			event.Sequence <= subscription.liveAfter ||
			!subscription.matches(event) {
			continue
		}
		subscription.mu.Lock()
		delivered, coalesced := subscription.enqueueLocked(event)
		if delivered {
			subscription.liveAfter = event.Sequence
			hub.global.Delivered++
			sessionMetrics.Delivered++
		}
		if coalesced {
			hub.global.EphemeralCoalesced++
			sessionMetrics.EphemeralCoalesced++
		}
		if !delivered && event.DeliveryClass() == DeliveryMaterial {
			subscription.closeLocked(ErrSlowSubscriber)
			delete(hub.subscribers, id)
			hub.global.Active--
			hub.global.ForcedDisconnects++
			sessionMetrics.Active--
			sessionMetrics.ForcedDisconnects++
		}
		subscription.mu.Unlock()
	}
	hub.bySession[event.SessionID] = sessionMetrics
	return nil
}

// GlobalMetrics returns a race-free global snapshot.
func (hub *Hub) GlobalMetrics() Metrics {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return hub.global
}

// SessionMetrics returns a race-free per-session snapshot.
func (hub *Hub) SessionMetrics(sessionID domain.SessionID) Metrics {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return hub.bySession[sessionID]
}

// Subscription is a bounded, cancellation-aware event cursor.
type Subscription struct {
	id        uint64
	hub       *Hub
	query     SubscriptionQuery
	liveAfter uint64
	capacity  int

	mu          sync.Mutex
	queue       []SessionEvent
	signal      chan struct{}
	done        chan struct{}
	closeErr    error
	closed      bool
	stopContext func() bool
}

// Next returns the next ordered event or the terminal subscription error.
func (subscription *Subscription) Next(ctx context.Context) (SessionEvent, error) {
	if ctx == nil {
		return SessionEvent{}, errors.New("next context must not be nil")
	}
	for {
		subscription.mu.Lock()
		if len(subscription.queue) > 0 {
			event := subscription.queue[0]
			copy(subscription.queue, subscription.queue[1:])
			subscription.queue = subscription.queue[:len(subscription.queue)-1]
			subscription.mu.Unlock()
			return event, nil
		}
		if subscription.closed {
			err := subscription.closeErr
			subscription.mu.Unlock()
			if err == nil {
				return SessionEvent{}, io.EOF
			}
			return SessionEvent{}, err
		}
		signal := subscription.signal
		done := subscription.done
		subscription.mu.Unlock()
		select {
		case <-ctx.Done():
			return SessionEvent{}, ctx.Err()
		case <-done:
		case <-signal:
		}
	}
}

// Close removes the subscriber immediately and is safe to call repeatedly.
func (subscription *Subscription) Close() error {
	subscription.close(nil)
	return nil
}

func (subscription *Subscription) close(reason error) {
	hub := subscription.hub
	hub.mu.Lock()
	subscription.mu.Lock()
	wasOpen := !subscription.closed
	subscription.closeLocked(reason)
	subscription.mu.Unlock()
	if wasOpen {
		delete(hub.subscribers, subscription.id)
		hub.global.Active--
		sessionMetrics := hub.bySession[subscription.query.SessionID]
		sessionMetrics.Active--
		hub.bySession[subscription.query.SessionID] = sessionMetrics
	}
	hub.mu.Unlock()
}

func (subscription *Subscription) closeLocked(reason error) {
	if subscription.closed {
		return
	}
	subscription.closed = true
	subscription.closeErr = reason
	close(subscription.done)
}

func (subscription *Subscription) matches(event SessionEvent) bool {
	if subscription.query.ThreadID != nil &&
		event.ThreadID != *subscription.query.ThreadID {
		return false
	}
	if subscription.query.TaskID != nil &&
		(event.TaskID == nil || *event.TaskID != *subscription.query.TaskID) {
		return false
	}
	return true
}

func (subscription *Subscription) enqueueLocked(
	event SessionEvent,
) (delivered bool, coalesced bool) {
	if subscription.closed {
		return false, false
	}
	if len(subscription.queue) < subscription.capacity {
		subscription.queue = append(subscription.queue, event)
		subscription.notifyLocked()
		return true, false
	}
	if event.DeliveryClass() == DeliveryEphemeralCoalescible {
		for index := len(subscription.queue) - 1; index >= 0; index-- {
			if sameEphemeralStream(subscription.queue[index], event) {
				copy(subscription.queue[index:], subscription.queue[index+1:])
				subscription.queue = subscription.queue[:len(subscription.queue)-1]
				subscription.queue = append(subscription.queue, event)
				subscription.notifyLocked()
				return true, true
			}
		}
		return true, true
	}
	for index, queued := range subscription.queue {
		if queued.DeliveryClass() == DeliveryEphemeralCoalescible {
			copy(subscription.queue[index:], subscription.queue[index+1:])
			subscription.queue = subscription.queue[:len(subscription.queue)-1]
			subscription.queue = append(subscription.queue, event)
			subscription.notifyLocked()
			return true, true
		}
	}
	return false, false
}

func (subscription *Subscription) notifyLocked() {
	select {
	case subscription.signal <- struct{}{}:
	default:
	}
}

func sameEphemeralStream(first, second SessionEvent) bool {
	if first.Kind != second.Kind {
		return false
	}
	switch first.Kind {
	case KindMessageDelta:
		return first.Payload.MessageDelta != nil &&
			second.Payload.MessageDelta != nil &&
			first.Payload.MessageDelta.MessageID ==
				second.Payload.MessageDelta.MessageID
	case KindToolProgress:
		return first.Payload.Tool != nil &&
			second.Payload.Tool != nil &&
			first.Payload.Tool.ExecutionID ==
				second.Payload.Tool.ExecutionID
	default:
		return false
	}
}
