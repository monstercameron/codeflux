package coordinator

import (
	"testing"
	"time"
)

// TestOneRunsOutageInformsTheNext covers the shared half of the circuit.
//
// The per-run circuit is right and insufficient: twenty tasks discovering the
// same dead provider one full timeout at a time is twenty times the wait and
// twenty times the spend for one fact.
func TestOneRunsOutageInformsTheNext(t *testing.T) {
	health := &providerHealth{
		unavailableUntil: map[string]time.Time{},
		lastReason:       map[string]string{},
	}
	now := time.Now()
	if _, known := health.recentlyExhausted("gpt-x", now); known {
		t.Error("a provider nobody has failed against was reported unavailable")
	}

	health.recordExhausted("gpt-x", "the provider exhausted its own retries", now)
	why, known := health.recentlyExhausted("gpt-x", now.Add(time.Second))
	if !known || why == "" {
		t.Error("an exhausted provider was not remembered for the next run")
	}
	if _, other := health.recentlyExhausted("gpt-y", now); other {
		t.Error("one provider's outage was reported against another")
	}
}

// TestTheWindowExpiresOnItsOwn keeps this a note about the last thing that
// happened rather than a policy about availability.
func TestTheWindowExpiresOnItsOwn(t *testing.T) {
	health := &providerHealth{
		unavailableUntil: map[string]time.Time{},
		lastReason:       map[string]string{},
	}
	now := time.Now()
	health.recordExhausted("gpt-x", "exhausted", now)
	if _, known := health.recentlyExhausted(
		"gpt-x", now.Add(providerHealthWindow+time.Second),
	); known {
		t.Error("the window never expired, so one outage speaks forever")
	}
}

// TestSuccessClearsItImmediately: a provider that recovers in five seconds must
// not be described as dead for thirty.
func TestSuccessClearsItImmediately(t *testing.T) {
	health := &providerHealth{
		unavailableUntil: map[string]time.Time{},
		lastReason:       map[string]string{},
	}
	now := time.Now()
	health.recordExhausted("gpt-x", "exhausted", now)
	health.clear("gpt-x")
	if _, known := health.recentlyExhausted("gpt-x", now.Add(time.Second)); known {
		t.Error("a provider that answered again was still reported unavailable")
	}
}
