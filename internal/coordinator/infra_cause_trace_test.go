package coordinator

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/providers"
)

// TestTheInfraTraceCarriesTheErrorUnderTheClassification is the diagnosis the
// trace could not support.
//
// providers.Failure formats as "provider <operation> failed: <kind>" and never
// mentions what it wrapped. An infrastructure death therefore traced as
// "transport" — which is also the bucket every unrecognised error falls into —
// while whether it was a connection reset, a TLS failure or an EOF sat one
// Unwrap away. That is the difference between a fact about this machine's
// network and a fact about the provider, and the run that hit it is the only
// one that ever had the answer.
//
// Proven to discriminate: against the previous implementation this renders
// "provider call the model failed: transport" and stops. Four ladder runs on
// 2026-08-04 died on that line and none of them said what closed.
func TestTheInfraTraceCarriesTheErrorUnderTheClassification(t *testing.T) {
	underlying := errors.New("read tcp 10.0.0.2:51234->1.2.3.4:443: " +
		"wsarecv: An existing connection was forcibly closed by the remote host")
	failure := &providers.Failure{
		Operation: "call the model",
		Kind:      providers.FailureTransport,
		Cause:     errors.Join(providers.ErrTransport, underlying),
	}
	wrapped := fmt.Errorf("provider retry budget exhausted: %w", failure)

	rendered := withUnderlyingCause(wrapped)
	if !strings.Contains(rendered, "transport") {
		t.Errorf("the classification the circuit decided on must stay in the "+
			"line, got %q", rendered)
	}
	if !strings.Contains(rendered, "forcibly closed") {
		t.Fatalf("the line names a category and not an error, so nobody can "+
			"tell a dropped socket from a refused request: %q", rendered)
	}
}

// TestARateLimitIsNotATransportFailure is the distinction the question this
// came from turned on.
//
// A 429 is an HTTP response: it classifies as FailureRateLimited, carries its
// own sentinel, and clears on its own. A transport failure is the absence of a
// response. Reporting one as the other would send somebody looking at quota
// when the socket closed, or at the network when the account is throttled.
func TestARateLimitIsNotATransportFailure(t *testing.T) {
	limited := &providers.Failure{
		Operation: "call the model",
		Kind:      providers.FailureRateLimited,
		Cause:     providers.ErrRateLimited,
	}
	if got := providerOutcomeOf(limited); got != "rate-limited" {
		t.Errorf("a 429 must read as a rate limit, got %q", got)
	}

	transport := &providers.Failure{
		Operation: "call the model",
		Kind:      providers.FailureTransport,
		Cause:     providers.ErrTransport,
	}
	if providerOutcomeOf(transport) == "rate-limited" {
		t.Error("a transport failure must not read as a rate limit: they " +
			"clear differently and mean different things")
	}
}

// TestAnErrorThatAlreadySaysItsCauseIsNotRepeated keeps the line readable.
//
// Most errors in this codebase wrap with %w and print what they wrapped. Naming
// the cause again would double every line for the ones that were already fine,
// and a doubled line reads as two failures.
func TestAnErrorThatAlreadySaysItsCauseIsNotRepeated(t *testing.T) {
	root := errors.New("no such host")
	wrapped := fmt.Errorf("resolve the provider: %w", root)
	rendered := withUnderlyingCause(wrapped)
	if strings.Count(rendered, "no such host") != 1 {
		t.Errorf("the cause was appended to a message that already had it: %q",
			rendered)
	}
	if withUnderlyingCause(nil) != "" {
		t.Error("no error is no text")
	}
}
