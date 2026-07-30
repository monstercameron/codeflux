package coordinator

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const maximumQueuedTasks = 1000

// ScheduleRequest is one visible bounded-concurrency task request.
type ScheduleRequest struct {
	TaskID      domain.TaskID
	ProviderKey string
	QueueReason string
	Priority    int
	Resuming    bool
	EnqueuedAt  time.Time
	Sequence    uint64
}

// QueuePosition explains why a task has not started.
type QueuePosition struct {
	Position int
	Reason   string
}

// Scheduler keeps worker and provider concurrency limits separate. The
// durable wrapper may provisionally admit a request while holding its outer
// lock, but no dispatch can observe it until SQLite persistence succeeds.
type Scheduler struct {
	mu             sync.Mutex
	workerLimit    int
	providerLimits map[string]int
	active         map[domain.TaskID]string
	providerActive map[string]int
	queue          []ScheduleRequest
	closed         bool
}

func NewScheduler(workerLimit int, providerLimits map[string]int) (*Scheduler, error) {
	if workerLimit < 1 || workerLimit > 64 {
		return nil, errors.New("worker concurrency limit is outside supported bounds")
	}
	limits := make(map[string]int, len(providerLimits))
	for provider, limit := range providerLimits {
		if provider == "" || limit < 1 || limit > 64 {
			return nil, errors.New("provider concurrency limit is invalid")
		}
		limits[provider] = limit
	}
	return &Scheduler{
		workerLimit: workerLimit, providerLimits: limits,
		active:         make(map[domain.TaskID]string),
		providerActive: make(map[string]int),
	}, nil
}

func (scheduler *Scheduler) Enqueue(request ScheduleRequest) (QueuePosition, error) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if err := scheduler.validateEnqueueLocked(request); err != nil {
		return QueuePosition{}, err
	}
	scheduler.queue = append(scheduler.queue, request)
	scheduler.sortQueue()
	return scheduler.position(request.TaskID), nil
}

func (scheduler *Scheduler) RemoveQueued(taskID domain.TaskID) error {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	for index, request := range scheduler.queue {
		if request.TaskID == taskID {
			scheduler.queue = append(
				scheduler.queue[:index],
				scheduler.queue[index+1:]...,
			)
			return nil
		}
	}
	return errors.New("task is not queued")
}

func (scheduler *Scheduler) validateEnqueueLocked(request ScheduleRequest) error {
	if scheduler.closed {
		return errors.New("scheduler is shutting down")
	}
	if len(scheduler.queue) >= maximumQueuedTasks {
		return errors.New("scheduler queue capacity is full")
	}
	if request.TaskID.IsZero() || request.ProviderKey == "" ||
		request.Priority < 0 || request.Priority > 1000 ||
		request.Sequence == 0 || request.EnqueuedAt.IsZero() {
		return errors.New("schedule request is invalid")
	}
	if _, exists := scheduler.active[request.TaskID]; exists {
		return errors.New("task is already active")
	}
	for _, queued := range scheduler.queue {
		if queued.TaskID == request.TaskID {
			return errors.New("task is already queued")
		}
	}
	return nil
}

// Dispatch selects the oldest highest-priority provider-eligible task. A busy
// provider cannot starve eligible work behind it.
func (scheduler *Scheduler) Dispatch() (ScheduleRequest, bool) {
	return scheduler.DispatchEligible(func(ScheduleRequest) bool { return true })
}

// DispatchEligible selects the next provider-eligible task that also satisfies
// the caller's in-memory execution prerequisite. The predicate must not call
// back into the scheduler.
func (scheduler *Scheduler) DispatchEligible(
	eligible func(ScheduleRequest) bool,
) (ScheduleRequest, bool) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if eligible == nil || scheduler.closed ||
		len(scheduler.active) >= scheduler.workerLimit {
		return ScheduleRequest{}, false
	}
	for index, request := range scheduler.queue {
		if !eligible(request) {
			continue
		}
		limit := scheduler.providerLimits[request.ProviderKey]
		if limit == 0 {
			limit = scheduler.workerLimit
		}
		if scheduler.providerActive[request.ProviderKey] >= limit {
			continue
		}
		scheduler.queue = append(scheduler.queue[:index], scheduler.queue[index+1:]...)
		scheduler.active[request.TaskID] = request.ProviderKey
		scheduler.providerActive[request.ProviderKey]++
		return request, true
	}
	return ScheduleRequest{}, false
}

func (scheduler *Scheduler) Complete(taskID domain.TaskID) error {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	provider, exists := scheduler.active[taskID]
	if !exists {
		return errors.New("task is not active")
	}
	delete(scheduler.active, taskID)
	scheduler.providerActive[provider]--
	return nil
}

func (scheduler *Scheduler) Position(taskID domain.TaskID) QueuePosition {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.position(taskID)
}

// Shutdown rejects new work and returns queued tasks in stable cancellation
// order. Active tasks remain owned until their workers checkpoint and exit.
func (scheduler *Scheduler) Shutdown() []ScheduleRequest {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.closed = true
	queued := append([]ScheduleRequest(nil), scheduler.queue...)
	scheduler.queue = nil
	return queued
}

func (scheduler *Scheduler) position(taskID domain.TaskID) QueuePosition {
	for index, request := range scheduler.queue {
		if request.TaskID == taskID {
			reason := strings.TrimSpace(request.QueueReason)
			limit := scheduler.providerLimits[request.ProviderKey]
			if limit != 0 && scheduler.providerActive[request.ProviderKey] >= limit {
				reason = "waiting for provider concurrency capacity"
			} else if reason == "" {
				reason = "waiting for an available worker"
			}
			return QueuePosition{Position: index + 1, Reason: reason}
		}
	}
	return QueuePosition{}
}

func (scheduler *Scheduler) sortQueue() {
	slices.SortStableFunc(scheduler.queue, func(left, right ScheduleRequest) int {
		if left.Resuming != right.Resuming {
			if left.Resuming {
				return -1
			}
			return 1
		}
		// A resumed task represents work that was paused or waiting for
		// approval. Keep that class FIFO so newly resumed high-priority work
		// cannot repeatedly jump an older blocked task.
		if !left.Resuming && left.Priority != right.Priority {
			return right.Priority - left.Priority
		}
		if left.Sequence < right.Sequence {
			return -1
		}
		if left.Sequence > right.Sequence {
			return 1
		}
		return strings.Compare(left.TaskID.String(), right.TaskID.String())
	})
}
