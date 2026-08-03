package coordinator

import (
	"context"
	"time"
)

// finalisationReserve is the wall clock a run keeps back to finish properly.
//
// What finishing costs is knowable: restore the checkpoint, rebuild, re-run the
// suite, check the acceptance examples, write the stage ledger and the terminal
// state. On a generated program that is seconds; the reserve is generous
// because the alternative failure is total. A run that spends its last minute
// on a model call it cannot complete has no result, no terminal state, and a
// record that says nothing — while a verified revision sits on disk.
const finalisationReserve = 60 * time.Second

// tooLateToStartAnAttempt reports whether beginning another model attempt would
// threaten the run's ability to finish.
//
// Only when the caller set a deadline. A run with no deadline has nothing to
// reserve against, and inventing one would end runs that were entitled to keep
// going — which is the opposite mistake and just as expensive.
func tooLateToStartAnAttempt(ctx context.Context) (bool, time.Duration) {
	deadline, set := ctx.Deadline()
	if !set {
		return false, 0
	}
	remaining := time.Until(deadline)
	return remaining < finalisationReserve, remaining
}
