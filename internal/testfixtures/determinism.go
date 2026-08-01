package testfixtures

import (
	"fmt"
	"sync"
	"time"
)

// FixtureEpoch is the instant every deterministic clock starts from
// (M22-003). It is fixed, in UTC, and deliberately not "now": a fixture that
// drifts with wall-clock time produces failures nobody can reproduce.
var FixtureEpoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// Clock is the seam a component takes so tests can control time.
type Clock interface {
	Now() time.Time
}

// FixedClock returns FixtureEpoch forever. Use it where the passage of time
// must not affect the result at all.
type FixedClock struct {
	Instant time.Time
}

// NewFixedClock returns a clock pinned to FixtureEpoch.
func NewFixedClock() *FixedClock {
	return &FixedClock{Instant: FixtureEpoch}
}

// Now returns the pinned instant.
func (clock *FixedClock) Now() time.Time {
	if clock.Instant.IsZero() {
		return FixtureEpoch
	}
	return clock.Instant
}

// SteppingClock advances by a fixed step on every read, so ordering is
// observable and reproducible without depending on real elapsed time.
type SteppingClock struct {
	mutex   sync.Mutex
	current time.Time
	step    time.Duration
}

// NewSteppingClock returns a clock starting at FixtureEpoch that advances by
// step on each Now. A non-positive step is rejected rather than silently
// turned into a fixed clock, because a caller asking for ordering and
// getting none would be misled.
func NewSteppingClock(step time.Duration) (*SteppingClock, error) {
	if step <= 0 {
		return nil, fmt.Errorf("stepping clock requires a positive step, got %v", step)
	}
	return &SteppingClock{current: FixtureEpoch, step: step}, nil
}

// Now returns the current instant and advances.
func (clock *SteppingClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	instant := clock.current
	clock.current = clock.current.Add(clock.step)
	return instant
}

// SequenceIDGenerator produces stable, ordered identifiers for tests
// (M22-003). Two runs of the same test produce the same identifiers, so a
// failure message names the same thing every time.
type SequenceIDGenerator struct {
	mutex  sync.Mutex
	prefix string
	next   uint64
}

// NewSequenceIDGenerator returns a generator emitting prefix-000001 upward.
func NewSequenceIDGenerator(prefix string) (*SequenceIDGenerator, error) {
	if prefix == "" {
		return nil, fmt.Errorf("sequence ID prefix must not be empty")
	}
	return &SequenceIDGenerator{prefix: prefix}, nil
}

// Next returns the next identifier in the sequence.
func (generator *SequenceIDGenerator) Next() string {
	generator.mutex.Lock()
	defer generator.mutex.Unlock()
	generator.next++
	return fmt.Sprintf("%s-%06d", generator.prefix, generator.next)
}

// Count reports how many identifiers have been issued, so a test can assert
// how many things were created rather than inferring it.
func (generator *SequenceIDGenerator) Count() uint64 {
	generator.mutex.Lock()
	defer generator.mutex.Unlock()
	return generator.next
}
