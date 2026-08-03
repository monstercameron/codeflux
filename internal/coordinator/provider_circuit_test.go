package coordinator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/providers"
)

// TestRetryExhaustionStopsAtOnceWhateverIsOnDisk is the ruling rung 6 needed.
//
// The checkpoint changes what recovery means, not whether the breaker opens.
// Tying the two together meant the case with no verified work — the one where
// stopping matters most, because there is nothing to fall back on — was the
// case that kept asking an unavailable provider.
func TestRetryExhaustionStopsAtOnceWhateverIsOnDisk(t *testing.T) {
	now := time.Now()

	withWork := decideCircuit(providers.ErrRetryBudgetExhausted,
		newInfrastructureBudget(now), true, now)
	if !withWork.Open || withWork.Disposition != circuitRestoreAndFinish {
		t.Errorf("with a checkpoint: open=%t disposition=%s, wanted an "+
			"immediate restore-and-finish", withWork.Open, withWork.Disposition)
	}

	without := decideCircuit(providers.ErrRetryBudgetExhausted,
		newInfrastructureBudget(now), false, now)
	if !without.Open || without.Disposition != circuitRecoveryRequired {
		t.Errorf("with no checkpoint: open=%t disposition=%s, wanted an "+
			"immediate recovery-required", without.Open, without.Disposition)
	}
}

// TestOneTransportFailureIsRetriedAndTwoAreNot covers the bounded allowance.
func TestOneTransportFailureIsRetriedAndTwoAreNot(t *testing.T) {
	now := time.Now()
	budget := newInfrastructureBudget(now)

	first := decideCircuit(providers.ErrTransport, budget, true, now)
	if first.Open || first.Disposition != circuitContinue {
		t.Errorf("the first transport failure opened the circuit: %s",
			first.Disposition)
	}
	if budget.AttemptsRemaining != 0 {
		t.Errorf("the allowance is %d after one retry, wanted 0",
			budget.AttemptsRemaining)
	}

	second := decideCircuit(providers.ErrTransport, budget, true, now)
	if !second.Open || second.Disposition != circuitRestoreAndFinish {
		t.Errorf("the second transport failure did not stop the run: %s",
			second.Disposition)
	}
}

// TestTheAllowanceIsNeverRefunded is what guarantees termination.
//
// The work's budget is refunded for an attempt lost to the machinery, which is
// right — the run learned nothing. A refund with no separate ceiling is an
// unbounded licence to keep asking, and rung 6 spent 165 seconds on one
// exhausted provider and was about to spend another ninety.
func TestTheAllowanceIsNeverRefunded(t *testing.T) {
	now := time.Now()
	budget := newInfrastructureBudget(now)
	for round := 0; round < 8; round++ {
		decideCircuit(providers.ErrTransport, budget, true, now)
	}
	if budget.AttemptsRemaining > 0 {
		t.Errorf("the allowance grew back to %d across eight failures",
			budget.AttemptsRemaining)
	}
	final := decideCircuit(providers.ErrTransport, budget, true, now)
	if !final.Open {
		t.Error("a spent allowance did not stop the run")
	}
}

// TestAuthenticationIsNotRetried covers a failure retrying cannot fix.
func TestAuthenticationIsNotRetried(t *testing.T) {
	now := time.Now()
	budget := newInfrastructureBudget(now)
	decision := decideCircuit(providers.ErrAuthentication, budget, true, now)
	if !decision.Open || decision.Disposition != circuitFailConfiguration {
		t.Errorf("an authentication failure was treated as transient: %s",
			decision.Disposition)
	}
	if budget.AttemptsRemaining != 1 {
		t.Error("an unretryable failure spent the transport allowance")
	}
}

// TestCancellationOutranksProviderRecovery keeps the reason honest: a run told
// to stop did not stop because the provider was unavailable.
func TestCancellationOutranksProviderRecovery(t *testing.T) {
	now := time.Now()
	decision := decideCircuit(
		errors.Join(context.Canceled, providers.ErrRetryBudgetExhausted),
		newInfrastructureBudget(now), true, now)
	if decision.Disposition != circuitRecoveryRequired {
		t.Errorf("a cancelled run was dispositioned %s", decision.Disposition)
	}
	if decision.Reason != "the run was cancelled" {
		t.Errorf("the reason reads %q", decision.Reason)
	}
}

// TestTheTimeAllowanceEndsTheRetrying covers the wall-clock half.
func TestTheTimeAllowanceEndsTheRetrying(t *testing.T) {
	now := time.Now()
	budget := newInfrastructureBudget(now)
	budget.AttemptsRemaining = 99
	decision := decideCircuit(
		providers.ErrTransport, budget, true, now.Add(time.Hour))
	if !decision.Open {
		t.Error("an expired time allowance kept retrying")
	}
}

// TestWhatIsSaidMatchesWhatIsDone is the narration rule: decide, then speak.
func TestWhatIsSaidMatchesWhatIsDone(t *testing.T) {
	finish := circuitDecision{Open: true, Disposition: circuitRestoreAndFinish,
		Reason: "the provider exhausted its own retries"}
	if said := finish.narrate(5); !strings.Contains(said, "Refinement stops here") ||
		strings.Contains(said, "Trying") {
		t.Errorf("a run that is stopping said: %s", said)
	}
	keep := circuitDecision{Open: true, Disposition: circuitRecoveryRequired,
		Reason: "the provider exhausted its own retries"}
	if said := keep.narrate(5); !strings.Contains(said, "draft is preserved") {
		t.Errorf("a run preserving a draft said: %s", said)
	}
	again := circuitDecision{Disposition: circuitContinue,
		Reason: "a transport failure, which may be transient"}
	if said := again.narrate(2); !strings.Contains(said, "Trying once more") {
		t.Errorf("a run that will retry said: %s", said)
	}
}
