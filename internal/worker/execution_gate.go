package worker

import (
	"context"
	"errors"
	"sync"
)

var ErrExecutionCancelled = errors.New("worker execution cancelled")

// ExecutionGate shares coordinator pause and cancellation state with task and
// mediated-tool work. Heartbeat delivery does not wait on this gate.
type ExecutionGate struct {
	mu        sync.Mutex
	changed   chan struct{}
	paused    bool
	cancelled bool
}

func NewExecutionGate() *ExecutionGate {
	return &ExecutionGate{changed: make(chan struct{})}
}

// Wait blocks new task work while paused and fails permanently after cancel.
func (gate *ExecutionGate) Wait(ctx context.Context) error {
	if gate == nil {
		return errors.New("worker execution gate is required")
	}
	for {
		gate.mu.Lock()
		if gate.cancelled {
			gate.mu.Unlock()
			return ErrExecutionCancelled
		}
		if !gate.paused {
			gate.mu.Unlock()
			return nil
		}
		changed := gate.changeChannelLocked()
		gate.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (gate *ExecutionGate) Pause() {
	if gate == nil {
		return
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.cancelled || gate.paused {
		return
	}
	gate.paused = true
	gate.signalLocked()
}

func (gate *ExecutionGate) Resume() {
	if gate == nil {
		return
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.cancelled || !gate.paused {
		return
	}
	gate.paused = false
	gate.signalLocked()
}

func (gate *ExecutionGate) Cancel() {
	if gate == nil {
		return
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.cancelled {
		return
	}
	gate.cancelled = true
	gate.signalLocked()
}

func (gate *ExecutionGate) Paused() bool {
	if gate == nil {
		return false
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.paused && !gate.cancelled
}

func (gate *ExecutionGate) changeChannelLocked() chan struct{} {
	if gate.changed == nil {
		gate.changed = make(chan struct{})
	}
	return gate.changed
}

func (gate *ExecutionGate) signalLocked() {
	close(gate.changeChannelLocked())
	gate.changed = make(chan struct{})
}
