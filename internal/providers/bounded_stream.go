package providers

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	defaultModelStreamBuffer = 16
	maximumModelStreamBuffer = 256
)

// BoundedModelStream is a cancellation-safe ordered ModelStream shared by
// adapters. Cancellation discards queued late events, while a non-cancellation
// terminal error is retained independently of the bounded event buffer.
type BoundedModelStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	now    func() time.Time
	events chan StreamEvent

	mu                sync.Mutex
	sequence          int64
	canceled          bool
	cancelCause       error
	terminal          error
	terminalDelivered bool
	finished          bool

	cancelOnce    sync.Once
	finishOnce    sync.Once
	lifecycleOnce sync.Once
	canceledDone  chan struct{}
	finishedDone  chan struct{}
	lifecycleDone chan struct{}
}

// NewBoundedModelStream creates one owned stream. The supplied cancel function
// must cancel the adapter's in-flight transport request.
func NewBoundedModelStream(
	ctx context.Context,
	cancel context.CancelFunc,
	now func() time.Time,
	buffer int,
) (*BoundedModelStream, error) {
	if ctx == nil || cancel == nil {
		return nil, errors.New("model stream context and cancel function are required")
	}
	if buffer == 0 {
		buffer = defaultModelStreamBuffer
	}
	if buffer < 1 || buffer > maximumModelStreamBuffer {
		return nil, errors.New("model stream buffer is outside supported bounds")
	}
	if now == nil {
		now = time.Now
	}
	stream := &BoundedModelStream{
		ctx: ctx, cancel: cancel, now: now,
		events:        make(chan StreamEvent, buffer),
		canceledDone:  make(chan struct{}),
		finishedDone:  make(chan struct{}),
		lifecycleDone: make(chan struct{}),
	}
	go stream.observeOwner()
	return stream, nil
}

// Emit appends one ordered event unless cancellation has already won.
func (stream *BoundedModelStream) Emit(
	ctx context.Context,
	event StreamEvent,
) error {
	stream.mu.Lock()
	if stream.canceled {
		failure := stream.cancellationFailureLocked()
		stream.mu.Unlock()
		return failure
	}
	if stream.finished {
		stream.mu.Unlock()
		return errors.New("model stream emitted after finish")
	}
	stream.sequence++
	event.Sequence = stream.sequence
	event.ObservedAt = stream.now().UTC()
	stream.mu.Unlock()

	select {
	case stream.events <- event:
		stream.mu.Lock()
		canceled := stream.canceled
		failure := stream.cancellationFailureLocked()
		stream.mu.Unlock()
		if canceled {
			return failure
		}
		return nil
	case <-stream.canceledDone:
		stream.mu.Lock()
		failure := stream.cancellationFailureLocked()
		stream.mu.Unlock()
		return failure
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Finish closes producer delivery and retains one terminal error without
// competing for bounded event-buffer capacity.
func (stream *BoundedModelStream) Finish(terminal error) {
	stream.finishOnce.Do(func() {
		stream.mu.Lock()
		stream.terminal = terminal
		stream.finished = true
		close(stream.finishedDone)
		stream.mu.Unlock()
		stream.endLifecycle()
	})
}

func (stream *BoundedModelStream) Recv(
	ctx context.Context,
) (StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return StreamEvent{}, err
	}
	if failure := stream.ownerFailureIfDone(); failure != nil {
		return StreamEvent{}, failure
	}
	stream.mu.Lock()
	if stream.canceled {
		failure := stream.cancellationFailureLocked()
		stream.mu.Unlock()
		return StreamEvent{}, failure
	}
	stream.mu.Unlock()
	select {
	case <-ctx.Done():
		return StreamEvent{}, ctx.Err()
	case <-stream.ctx.Done():
		stream.cancelWithCause(stream.ctx.Err())
		stream.mu.Lock()
		failure := stream.cancellationFailureLocked()
		stream.mu.Unlock()
		return StreamEvent{}, failure
	case <-stream.canceledDone:
		stream.mu.Lock()
		failure := stream.cancellationFailureLocked()
		stream.mu.Unlock()
		return StreamEvent{}, failure
	case event := <-stream.events:
		stream.mu.Lock()
		if stream.canceled {
			failure := stream.cancellationFailureLocked()
			stream.mu.Unlock()
			return StreamEvent{}, failure
		}
		stream.mu.Unlock()
		if failure := stream.ownerFailureIfDone(); failure != nil {
			return StreamEvent{}, failure
		}
		return event, nil
	case <-stream.finishedDone:
		if failure := stream.ownerFailureIfDone(); failure != nil {
			return StreamEvent{}, failure
		}
		select {
		case event := <-stream.events:
			stream.mu.Lock()
			if stream.canceled {
				failure := stream.cancellationFailureLocked()
				stream.mu.Unlock()
				return StreamEvent{}, failure
			}
			stream.mu.Unlock()
			if failure := stream.ownerFailureIfDone(); failure != nil {
				return StreamEvent{}, failure
			}
			return event, nil
		default:
		}
		stream.mu.Lock()
		if stream.terminal != nil && !stream.terminalDelivered {
			stream.terminalDelivered = true
			terminal := stream.terminal
			stream.mu.Unlock()
			return StreamEvent{}, terminal
		}
		stream.mu.Unlock()
		return StreamEvent{}, io.EOF
	}
}

func (stream *BoundedModelStream) ownerFailureIfDone() error {
	if err := stream.ctx.Err(); err != nil {
		stream.cancelWithCause(err)
		stream.mu.Lock()
		failure := stream.cancellationFailureLocked()
		stream.mu.Unlock()
		return failure
	}
	return nil
}

func (stream *BoundedModelStream) Close() error {
	stream.cancelWithCause(context.Canceled)
	return nil
}

func (stream *BoundedModelStream) observeOwner() {
	select {
	case <-stream.ctx.Done():
		stream.cancelWithCause(stream.ctx.Err())
	case <-stream.lifecycleDone:
	}
}

func (stream *BoundedModelStream) cancelWithCause(cause error) {
	stream.cancelOnce.Do(func() {
		stream.mu.Lock()
		stream.canceled = true
		stream.cancelCause = cause
		close(stream.canceledDone)
		stream.mu.Unlock()
		stream.cancel()
		stream.endLifecycle()
	})
}

func (stream *BoundedModelStream) endLifecycle() {
	stream.lifecycleOnce.Do(func() {
		close(stream.lifecycleDone)
	})
}

func (stream *BoundedModelStream) cancellationFailureLocked() error {
	kind := FailureCanceled
	sentinel := ErrCanceled
	if errors.Is(stream.cancelCause, context.DeadlineExceeded) {
		kind = FailureTimeout
		sentinel = ErrTimeout
	}
	return &Failure{
		Kind: kind, Operation: "receive provider stream",
		Cause: errors.Join(sentinel, stream.cancelCause),
	}
}

var _ ModelStream = (*BoundedModelStream)(nil)
