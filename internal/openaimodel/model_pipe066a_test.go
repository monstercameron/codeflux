package openaimodel

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/providers"
)

// --- PIPE-066a: the real model path is routed through the admission point. ---
//
// Before this change, ObserveThink issued a bare model.options.Client.Do
// against the network with no retry, no backoff, no Retry-After handling,
// and no concurrency ceiling — internal/providers.RetryExecutor and its
// AdmissionController existed, fully tested, but the only production caller
// of either was cmd/codeflux-dev/live.go's one-shot smoke gate. These tests
// drive the actual production entry point, ObserveThink, exactly as
// newDefaultAgentModel builds it for a real run: a real *Model, a real
// providers.RetryExecutor, a real providers.HTTPTransport. The only thing
// scripted is the far end of the wire (an httptest.Server), never a real
// provider — no test here can spend the user's money.

// testPricedOptions is the minimal Options a Model can be built with,
// pointed at a local httptest server rather than the real OpenAI endpoint.
func testPricedOptions(server *httptest.Server) Options {
	amount, err := providers.NewExactAmount("USD", 1, 1_000_000)
	if err != nil {
		panic(err)
	}
	return Options{
		APIKey:   "test-key",
		Model:    "test-model",
		Endpoint: server.URL,
		Client:   server.Client(),
		Price:    providers.TokenPrice{Input: amount, Output: amount},
	}
}

const minimalResponsesReplyBody = `{"id":"resp-1","output":[],` +
	`"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`

// TestObserveThink_RetriesRateLimitedResponsesHonouringRetryAfter is the
// discriminating proof for PIPE-067/PIPE-066a reaching the real model port:
// a server that answers 429 with Retry-After once and then succeeds must be
// retried by ObserveThink itself, not merely by RetryExecutor in isolation.
// Deleting model.retries.Execute in favour of a bare transport.DoJSON call
// makes this fail with a 429 status error on the first request rather than
// succeeding on the second.
func TestObserveThink_RetriesRateLimitedResponsesHonouringRetryAfter(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		count := atomic.AddInt32(&requests, 1)
		if count == 1 {
			writer.Header().Set("Retry-After", "0")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":{"message":"synthetic rate limit"}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(minimalResponsesReplyBody))
	}))
	defer server.Close()

	model, err := New(testPricedOptions(server))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	turn, err := model.ObserveThink(t.Context(), agent.ModelInput{})
	if err != nil {
		t.Fatalf("ObserveThink: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("server saw %d request(s), want exactly 2 (one 429, one retry "+
			"that succeeded) — ObserveThink is not retrying a rate-limited "+
			"response", got)
	}
	if turn.ModelRequestID.IsZero() {
		t.Error("a successful turn must carry the request identity retried against")
	}
	if !turn.Usage.Known || turn.Usage.InputTokens != 1 || turn.Usage.OutputTokens != 1 {
		t.Errorf("turn usage = %+v, want the second (successful) response's usage", turn.Usage)
	}
}

// TestObserveThink_ExhaustsRetryBudgetOnSustainedRateLimitingRatherThanHanging
// proves ObserveThink surfaces the typed, bounded retry-exhaustion failure
// providers.RetryExecutor produces rather than either retrying forever or
// returning a bare, unclassified HTTP error — a caller further up (the
// convergence tracker in agent_execution.go) needs to tell "the provider is
// rate limiting us" apart from an ordinary failure.
func TestObserveThink_ExhaustsRetryBudgetOnSustainedRateLimitingRatherThanHanging(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		atomic.AddInt32(&requests, 1)
		writer.Header().Set("Retry-After", "0")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"message":"synthetic sustained rate limit"}}`))
	}))
	defer server.Close()

	options := testPricedOptions(server)
	options.RetryPolicy = providers.RetryPolicy{
		MaximumAttempts: 2, InitialDelay: time.Millisecond,
		MaximumBackoff: 2 * time.Millisecond, MaximumCumulativeWait: 50 * time.Millisecond,
	}
	model, err := New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = model.ObserveThink(t.Context(), agent.ModelInput{})
	if err == nil {
		t.Fatal("ObserveThink against a server that always rate-limits must fail, not succeed")
	}
	if !errors.Is(err, providers.ErrRetryBudgetExhausted) {
		t.Fatalf("ObserveThink error = %v, want it to wrap providers.ErrRetryBudgetExhausted "+
			"so a caller can tell a rate limit from an ordinary failure", err)
	}
	if !errors.Is(err, providers.ErrRateLimited) {
		t.Fatalf("ObserveThink error = %v, want it to wrap providers.ErrRateLimited", err)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("server saw %d request(s), want exactly the configured MaximumAttempts (2)", got)
	}
}

// TestObserveThink_BoundsConcurrentRequestsThroughTheSharedAdmissionPoint is
// PIPE-066's own discriminating case (TestAdmissionControllerBoundsConcurrentAcquisitions
// in internal/providers) reproduced against the real production call site:
// concurrent ObserveThink calls on one *Model must never exceed the
// configured concurrency ceiling in flight against the provider at once.
// Before PIPE-066a this had no ceiling at all — every concurrent round fired
// straight at the network — so this is exactly the property that changed.
func TestObserveThink_BoundsConcurrentRequestsThroughTheSharedAdmissionPoint(t *testing.T) {
	const ceiling = 2
	const rounds = 6
	var inFlight, maxSeen int64
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		current := atomic.AddInt64(&inFlight, 1)
		for {
			previous := atomic.LoadInt64(&maxSeen)
			if current <= previous || atomic.CompareAndSwapInt64(&maxSeen, previous, current) {
				break
			}
		}
		// Held open long enough that, without a real gate, all six rounds would
		// overlap inside the handler at once — the same margin
		// TestAdmissionControllerBoundsConcurrentAcquisitions uses.
		time.Sleep(75 * time.Millisecond)
		atomic.AddInt64(&inFlight, -1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(minimalResponsesReplyBody))
	}))
	defer server.Close()

	options := testPricedOptions(server)
	options.RetryPolicy = providers.RetryPolicy{MaximumConcurrency: ceiling}
	model, err := New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, rounds)
	for index := 0; index < rounds; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := model.ObserveThink(t.Context(), agent.ModelInput{}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("ObserveThink: %v", err)
	}
	if observed := atomic.LoadInt64(&maxSeen); observed != ceiling {
		t.Fatalf("max concurrent ObserveThink calls in flight at once against the "+
			"provider = %d, want exactly the configured ceiling %d (below it never "+
			"exercised real contention; above it means the round call site has no "+
			"admission gate)", observed, ceiling)
	}
}
