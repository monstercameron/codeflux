package coordinator

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestSchedulerSeparatesWorkerAndProviderLimitsWithoutStarvation(t *testing.T) {
	scheduler, err := NewScheduler(2, map[string]int{"slow": 1, "fast": 2})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	slowOne := scheduleFixture(t, "slow", 10, 1, now)
	slowTwo := scheduleFixture(t, "slow", 10, 2, now)
	fast := scheduleFixture(t, "fast", 10, 3, now)
	for _, request := range []ScheduleRequest{slowOne, slowTwo, fast} {
		if _, err := scheduler.Enqueue(request); err != nil {
			t.Fatal(err)
		}
	}
	first, ok := scheduler.Dispatch()
	if !ok || first.TaskID != slowOne.TaskID {
		t.Fatalf("first dispatch = %#v, %t", first, ok)
	}
	second, ok := scheduler.Dispatch()
	if !ok || second.TaskID != fast.TaskID {
		t.Fatalf("provider-blocked work starved eligible task: %#v, %t", second, ok)
	}
	position := scheduler.Position(slowTwo.TaskID)
	if position.Position != 1 ||
		position.Reason != "waiting for provider concurrency capacity" {
		t.Fatalf("queue position = %#v", position)
	}
	if err := scheduler.Complete(slowOne.TaskID); err != nil {
		t.Fatal(err)
	}
	third, ok := scheduler.Dispatch()
	if !ok || third.TaskID != slowTwo.TaskID {
		t.Fatalf("third dispatch = %#v, %t", third, ok)
	}
}

func TestSchedulerPriorityStabilityAndShutdown(t *testing.T) {
	scheduler, err := NewScheduler(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	low := scheduleFixture(t, "provider", 1, 1, now)
	highOne := scheduleFixture(t, "provider", 10, 2, now)
	highTwo := scheduleFixture(t, "provider", 10, 3, now)
	for _, request := range []ScheduleRequest{low, highOne, highTwo} {
		if _, err := scheduler.Enqueue(request); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := scheduler.Dispatch()
	if first.TaskID != highOne.TaskID {
		t.Fatalf("priority dispatch = %#v", first)
	}
	cancelled := scheduler.Shutdown()
	if len(cancelled) != 2 || cancelled[0].TaskID != highTwo.TaskID ||
		cancelled[1].TaskID != low.TaskID {
		t.Fatalf("shutdown queue = %#v", cancelled)
	}
	if _, err := scheduler.Enqueue(
		scheduleFixture(t, "provider", 1, 4, now),
	); err == nil {
		t.Fatal("shutdown scheduler accepted new work")
	}
}

func TestSchedulerResumedWorkCannotBeStarvedByNewTasks(t *testing.T) {
	scheduler, err := NewScheduler(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	olderResumed := scheduleFixture(t, "provider-a", 1, 1, now)
	olderResumed.Resuming = true
	olderResumed.QueueReason = "resuming after approval"
	if position, err := scheduler.Enqueue(olderResumed); err != nil {
		t.Fatal(err)
	} else if position.Position != 1 ||
		position.Reason != olderResumed.QueueReason {
		t.Fatalf("resumed position = %#v", position)
	}
	newerResumed := scheduleFixture(t, "provider-a", 1000, 2, now)
	newerResumed.Resuming = true
	if _, err := scheduler.Enqueue(newerResumed); err != nil {
		t.Fatal(err)
	}
	fresh := scheduleFixture(t, "provider-a", 1000, 3, now)
	if _, err := scheduler.Enqueue(fresh); err != nil {
		t.Fatal(err)
	}
	dispatched, ok := scheduler.Dispatch()
	if !ok || dispatched.TaskID != olderResumed.TaskID {
		t.Fatalf("dispatched = %#v, %t", dispatched, ok)
	}
}

func TestSchedulerBoundsQueuedWorkAndCopiesProviderLimits(t *testing.T) {
	limits := map[string]int{"provider": 1}
	scheduler, err := NewScheduler(2, limits)
	if err != nil {
		t.Fatal(err)
	}
	limits["provider"] = 2
	now := time.Now()
	for sequence := 1; sequence <= maximumQueuedTasks; sequence++ {
		if _, err := scheduler.Enqueue(scheduleFixture(
			t, "provider", 1, uint64(sequence), now,
		)); err != nil {
			t.Fatalf("enqueue %d: %v", sequence, err)
		}
	}
	if _, err := scheduler.Enqueue(scheduleFixture(
		t, "provider", 1, maximumQueuedTasks+1, now,
	)); err == nil {
		t.Fatal("scheduler accepted work beyond its bounded queue capacity")
	}

	first, ok := scheduler.Dispatch()
	if !ok {
		t.Fatal("scheduler did not dispatch first task")
	}
	if _, ok := scheduler.Dispatch(); ok {
		t.Fatal("mutating caller provider limits changed scheduler policy")
	}
	if err := scheduler.Complete(first.TaskID); err != nil {
		t.Fatal(err)
	}
}

func scheduleFixture(
	t *testing.T,
	provider string,
	priority int,
	sequence uint64,
	now time.Time,
) ScheduleRequest {
	t.Helper()
	id, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	return ScheduleRequest{
		TaskID: id, ProviderKey: provider, Priority: priority,
		Sequence: sequence, EnqueuedAt: now,
	}
}
