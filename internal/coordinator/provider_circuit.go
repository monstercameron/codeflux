package coordinator

import (
	"context"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/providers"
)

// circuitDisposition is what a run does about a provider that would not answer.
//
// Four outcomes, not one boolean. "The circuit is open" says the run should
// stop and says nothing about what stopping means, and the three ways of
// stopping are genuinely different: a run holding verified work finishes on it,
// a run holding an unverified draft preserves it for somebody to resume, and a
// run whose credentials are wrong is not waiting for anything.
type circuitDisposition string

const (
	// circuitContinue is the ordinary answer: try once more.
	circuitContinue circuitDisposition = "continue"
	// circuitRestoreAndFinish ends the run on the revision it verified.
	circuitRestoreAndFinish circuitDisposition = "restore-and-finish"
	// circuitRecoveryRequired preserves an unverified draft for a person.
	//
	// Not "failed". A draft that nobody could check because the provider went
	// away is not bad work; it is unfinished work, and calling it failed loses
	// the distinction that decides whether anyone looks at it again.
	circuitRecoveryRequired circuitDisposition = "recovery-required"
	// circuitFailConfiguration ends immediately: retrying cannot help.
	circuitFailConfiguration circuitDisposition = "fail-configuration"
)

// circuitDecision is one ruling about one provider failure.
type circuitDecision struct {
	Open        bool
	Disposition circuitDisposition
	Reason      string
	RetryAfter  time.Duration
}

// infrastructureBudget is what a run may spend on the machinery failing, kept
// apart from what it may spend on the work.
//
// Attempts lost to a provider are refunded to the work's budget, which is
// right: the run learned nothing and produced nothing, and charging it would
// mean a run two gates from finished could die of a socket. What was missing is
// the other half — a refund with no separate ceiling is an unbounded licence to
// keep asking, and ladder rung 6 spent 165 seconds on one exhausted provider
// and was about to spend another 90.
//
// So the outer allowance decreases and is never refunded. It is the thing that
// guarantees termination.
type infrastructureBudget struct {
	// AttemptsRemaining is how many more outer retries the machinery gets.
	AttemptsRemaining int
	// Deadline is when the run must stop retrying whatever else is true.
	Deadline time.Time
	// Consecutive counts identical failures in a row, for the trace.
	Consecutive int
}

// newInfrastructureBudget is the default allowance.
//
// One outer retry, and twenty-five seconds. The provider adapter already owns
// network retries with its own backoff; this is the layer above it, and its
// only job is to notice that the layer below has given up. A generous outer
// allowance duplicates work already done and delays the answer.
func newInfrastructureBudget(now time.Time) *infrastructureBudget {
	return &infrastructureBudget{
		AttemptsRemaining: 1,
		Deadline:          now.Add(25 * time.Second),
	}
}

// decideCircuit rules on one provider failure.
//
// The checkpoint changes what recovery means, not whether the breaker opens.
// Tying the two together is what let rung 6 keep going: with no verified
// revision the circuit stayed shut and the run kept asking an unavailable
// provider, which is the case where stopping matters most, because there is
// nothing to fall back to and every further second is spent making that worse.
func decideCircuit(
	failure error,
	budget *infrastructureBudget,
	haveCheckpoint bool,
	now time.Time,
) circuitDecision {
	stop := func(reason string) circuitDecision {
		if haveCheckpoint {
			return circuitDecision{Open: true,
				Disposition: circuitRestoreAndFinish, Reason: reason}
		}
		return circuitDecision{Open: true,
			Disposition: circuitRecoveryRequired, Reason: reason}
	}

	switch {
	// Cancellation outranks everything. A run told to stop stops, and the
	// reason it stopped is not "the provider was unavailable".
	case errors.Is(failure, context.Canceled),
		errors.Is(failure, providers.ErrCanceled):
		return circuitDecision{Open: true,
			Disposition: circuitRecoveryRequired,
			Reason:      "the run was cancelled"}

	// Credentials and configuration cannot be retried into working.
	case errors.Is(failure, providers.ErrAuthentication),
		errors.Is(failure, providers.ErrInvalidRequest),
		errors.Is(failure, providers.ErrSafety):
		return circuitDecision{Open: true,
			Disposition: circuitFailConfiguration,
			Reason: "the provider refused the request itself, which no " +
				"number of retries changes"}

	// An exhausted retry budget is the provider's own retry policy reporting
	// that it has already tried and given up. Asking again asks the same
	// question of the same unavailable provider and pays another full timeout
	// to hear the same answer.
	case errors.Is(failure, providers.ErrRetryBudgetExhausted):
		return stop("the provider exhausted its own retries")

	case budget.AttemptsRemaining <= 0:
		return stop("the outer retry allowance is spent")

	case !budget.Deadline.IsZero() && now.After(budget.Deadline):
		return stop("the time allowed for provider trouble is spent")

	// A raw transport error may be one closed socket. One outer retry, charged
	// to an allowance that never refills.
	default:
		budget.AttemptsRemaining--
		budget.Consecutive++
		retryAfter := time.Duration(0)
		if errors.Is(failure, providers.ErrRateLimited) {
			retryAfter = 2 * time.Second
		}
		return circuitDecision{Open: false, Disposition: circuitContinue,
			Reason:     "a transport failure, which may be transient",
			RetryAfter: retryAfter}
	}
}

// narrate is what a person is told, decided before anything is said.
//
// The message used to promise another attempt and the circuit then finalised
// the run on the next line, so the last thing a reader saw was "Trying again"
// from a run that never tried again.
func (decision circuitDecision) narrate(attempt int) string {
	switch decision.Disposition {
	case circuitRestoreAndFinish:
		return "Attempt " + itoaPlan(attempt) + " could not reach the model: " +
			decision.Reason + ". Refinement stops here and the run finishes " +
			"from its verified revision."
	case circuitRecoveryRequired:
		return "Attempt " + itoaPlan(attempt) + " could not reach the model: " +
			decision.Reason + ". Nothing here has been verified, so the draft " +
			"is preserved as it stands and the task is left for someone to " +
			"resume rather than reported as failed work."
	case circuitFailConfiguration:
		return "Attempt " + itoaPlan(attempt) + " was refused by the " +
			"provider: " + decision.Reason + "."
	default:
		return "Attempt " + itoaPlan(attempt) + " could not reach the model: " +
			decision.Reason + ". Trying once more."
	}
}
