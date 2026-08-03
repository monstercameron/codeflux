package coordinator

import (
	"sync"
	"time"
)

// providerHealth is what the process as a whole knows about a provider that
// stopped answering.
//
// The circuit is per-run and has to be: one task's outage is not another's
// verdict, and a run that stops early on somebody else's evidence has been
// denied its attempt. But the opposite is worse at scale — twenty tasks
// discovering the same dead provider one ninety-second timeout at a time is
// twenty times the wait and twenty times the spend for one fact.
//
// So a run that exhausts a provider records it, and a run that starts while
// that is still fresh is told before it begins rather than after it waits. The
// window is short and it half-opens on its own: nothing here refuses to try,
// it only refuses to pretend the last attempt did not happen.
type providerHealth struct {
	mutex sync.Mutex
	// unavailableUntil is when this provider may be tried again without
	// comment, keyed by model identity.
	unavailableUntil map[string]time.Time
	// lastReason is why, for the trace and for the run that is about to wait.
	lastReason map[string]string
}

// providerHealthWindow is how long one exhaustion speaks for.
//
// Half a minute. Long enough that a burst of tasks does not each pay the full
// timeout, short enough that a provider which recovers is tried again almost
// immediately. A longer window would make this a policy about availability
// rather than a note about the last thing that happened.
const providerHealthWindow = 30 * time.Second

// sharedProviderHealth is process-wide because the thing it describes is.
var sharedProviderHealth = &providerHealth{
	unavailableUntil: map[string]time.Time{},
	lastReason:       map[string]string{},
}

// recordExhausted notes that a provider gave up.
func (health *providerHealth) recordExhausted(model, reason string, now time.Time) {
	if model == "" {
		return
	}
	health.mutex.Lock()
	defer health.mutex.Unlock()
	health.unavailableUntil[model] = now.Add(providerHealthWindow)
	health.lastReason[model] = reason
	tracef("infra", "provider %s marked unavailable for %s — %s",
		model, providerHealthWindow, reason)
}

// recentlyExhausted reports whether a provider gave up inside the window, and
// says why.
//
// Advisory by construction. It returns what is known and decides nothing: the
// caller may still try, and for the first attempt of a run it should — the
// alternative is a task that never starts because an unrelated one failed.
func (health *providerHealth) recentlyExhausted(
	model string, now time.Time,
) (string, bool) {
	if model == "" {
		return "", false
	}
	health.mutex.Lock()
	defer health.mutex.Unlock()
	until, known := health.unavailableUntil[model]
	if !known || now.After(until) {
		return "", false
	}
	return health.lastReason[model], true
}

// clear is what a successful call reports: the provider is answering again.
//
// Called on success rather than expiring only by time, so a provider that
// recovers in five seconds is not treated as dead for thirty.
func (health *providerHealth) clear(model string) {
	if model == "" {
		return
	}
	health.mutex.Lock()
	defer health.mutex.Unlock()
	if _, known := health.unavailableUntil[model]; !known {
		return
	}
	delete(health.unavailableUntil, model)
	delete(health.lastReason, model)
	tracef("infra", "provider %s answered again", model)
}
