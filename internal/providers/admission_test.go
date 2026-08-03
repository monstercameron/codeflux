package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAdmissionControllerBoundsConcurrentAcquisitions proves the admission
// point actually gates concurrency rather than merely bookkeeping it. Six
// goroutines race to acquire a two-slot controller and each holds its slot
// open for long enough that, without a real gate, all six would overlap.
//
// This is the discriminating case PIPE-066 asks for: comment out the
// AdmissionController.Acquire call in RetryExecutor.Execute (or lower this
// test to call the bare attempt closures directly with no controller) and
// maxSeen becomes 6, not 2 — an observable overrun, not merely a slower
// pass.
func TestAdmissionControllerBoundsConcurrentAcquisitions(t *testing.T) {
	const ceiling = 2
	const goroutines = 6
	controller, err := NewAdmissionController(ceiling)
	if err != nil {
		t.Fatal(err)
	}
	var (
		inFlight int64
		maxSeen  int64
		wg       sync.WaitGroup
	)
	start := make(chan struct{})
	for index := 0; index < goroutines; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, acquireErr := controller.Acquire(context.Background())
			if acquireErr != nil {
				t.Errorf("acquire: %v", acquireErr)
				return
			}
			defer release()
			current := atomic.AddInt64(&inFlight, 1)
			for {
				previous := atomic.LoadInt64(&maxSeen)
				if current <= previous {
					break
				}
				if atomic.CompareAndSwapInt64(&maxSeen, previous, current) {
					break
				}
			}
			// Hold the slot open long enough that every one of the six
			// goroutines has had time to attempt admission while this one is
			// still inside, so an ungated implementation would show up here.
			time.Sleep(75 * time.Millisecond)
			atomic.AddInt64(&inFlight, -1)
		}()
	}
	close(start)
	wg.Wait()
	if observed := atomic.LoadInt64(&maxSeen); observed != ceiling {
		t.Fatalf(
			"max concurrent admissions = %d, want exactly the ceiling %d "+
				"(below it never exercised real contention; above it means "+
				"the gate overran)",
			observed, ceiling,
		)
	}
}

// TestAdmissionControllerNarrowRetiresSlotsRatherThanWidening pins the two
// properties PIPE-069 depends on: a narrow that cannot immediately preempt
// checked-out slots still converges to the new ceiling as they are released,
// and Narrow never widens the ceiling back up.
func TestAdmissionControllerNarrowRetiresSlotsRatherThanWidening(t *testing.T) {
	controller, err := NewAdmissionController(4)
	if err != nil {
		t.Fatal(err)
	}
	var checkedOut []func()
	for index := 0; index < 4; index++ {
		release, acquireErr := controller.Acquire(context.Background())
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		checkedOut = append(checkedOut, release)
	}

	// All four slots are held; narrowing to 2 cannot preempt them, so it must
	// register as debt rather than an immediate reduction it cannot deliver.
	controller.Narrow(2)
	if got := controller.Ceiling(); got != 2 {
		t.Fatalf("ceiling after narrow = %d, want 2", got)
	}

	// Narrow is monotonically decreasing: a later call naming a higher number
	// is a no-op, not a widening.
	controller.Narrow(10)
	if got := controller.Ceiling(); got != 2 {
		t.Fatalf("ceiling widened by Narrow(10) to %d, want unchanged 2", got)
	}

	for _, release := range checkedOut {
		release()
	}

	// Two of the four released slots pay down the narrowing debt instead of
	// returning to the pool, so exactly two — not four — are available now.
	admittedContext, admittedCancel := context.WithTimeout(
		context.Background(), 500*time.Millisecond,
	)
	defer admittedCancel()
	firstRelease, err := controller.Acquire(admittedContext)
	if err != nil {
		t.Fatalf("first post-narrow acquire: %v", err)
	}
	secondRelease, err := controller.Acquire(admittedContext)
	if err != nil {
		t.Fatalf("second post-narrow acquire: %v", err)
	}
	blockedContext, blockedCancel := context.WithTimeout(
		context.Background(), 75*time.Millisecond,
	)
	defer blockedCancel()
	if _, blockedErr := controller.Acquire(blockedContext); !errors.Is(
		blockedErr, context.DeadlineExceeded,
	) {
		t.Fatalf(
			"third post-narrow acquire = %v, want it to block on the narrowed ceiling of 2",
			blockedErr,
		)
	}
	firstRelease()
	secondRelease()
}

// TestRetryExecutorAppliesEqualJitterWithinBounds proves backoff() is a
// function of the injected Random source, not a fixed schedule. Against the
// pre-jitter implementation this test fails outright: backoff(1) would
// return InitialDelay regardless of Random, so the r=0 and r=1 cases would be
// equal instead of spanning [half, base].
func TestRetryExecutorAppliesEqualJitterWithinBounds(t *testing.T) {
	var fraction float64
	executor := retryExecutorFixture(t, RetryPolicy{
		InitialDelay:   100 * time.Millisecond,
		MaximumBackoff: 100 * time.Millisecond,
		Random:         func() float64 { return fraction },
	}, func(context.Context, AttemptRecord) error { return nil })

	base := 100 * time.Millisecond
	half := base / 2
	for _, testFraction := range []float64{0, 0.25, 0.5, 0.75, 1} {
		fraction = testFraction
		got := executor.backoff(1)
		want := half + time.Duration(float64(half)*testFraction)
		if got != want {
			t.Fatalf("backoff(1) with random=%v = %v, want %v", testFraction, got, want)
		}
		if got < half || got > base {
			t.Fatalf("backoff(1) = %v outside the equal-jitter bounds [%v, %v]", got, half, base)
		}
	}
	// The two extremes must differ: a schedule that ignores Random entirely
	// (the pre-jitter behavior) would make this equality hold and the test
	// would pass for the wrong reason without it.
	fraction = 0
	low := executor.backoff(1)
	fraction = 1
	high := executor.backoff(1)
	if low == high {
		t.Fatalf("backoff(1) did not vary with the injected random source: %v == %v", low, high)
	}
}

// TestRetryExecutorAdmitsEveryPhysicalAttemptThroughOneCeiling proves
// PIPE-066 end to end through the real public entry point every provider
// request uses: three logical requests, each configured to attempt twice,
// share one executor whose ceiling is 2. Every physical attempt records how
// many attempts were concurrently inside the provider call at once; the
// observed maximum must never exceed the configured ceiling of 2, even
// though 6 physical attempts run across the 3 logical requests.
func TestRetryExecutorAdmitsEveryPhysicalAttemptThroughOneCeiling(t *testing.T) {
	const ceiling = 2
	const logicalRequests = 3
	executor := retryExecutorFixture(t, RetryPolicy{
		MaximumAttempts:    1,
		MaximumConcurrency: ceiling,
	}, func(context.Context, AttemptRecord) error { return nil })

	// Identities are built on the main test goroutine: providerRetryIdentity
	// can call t.Fatal, and FailNow is only safe from the goroutine running
	// the test itself.
	identities := make([]RequestIdentity, logicalRequests)
	for index := range identities {
		identities[index] = providerRetryIdentity(t, false)
	}

	var (
		inFlight int64
		maxSeen  int64
		wg       sync.WaitGroup
	)
	start := make(chan struct{})
	for index := 0; index < logicalRequests; index++ {
		wg.Add(1)
		go func(identity RequestIdentity) {
			defer wg.Done()
			<-start
			_, execErr := executor.Execute(
				context.Background(),
				identity,
				func(context.Context, PhysicalAttempt) AttemptOutcome {
					current := atomic.AddInt64(&inFlight, 1)
					for {
						previous := atomic.LoadInt64(&maxSeen)
						if current <= previous {
							break
						}
						if atomic.CompareAndSwapInt64(&maxSeen, previous, current) {
							break
						}
					}
					time.Sleep(75 * time.Millisecond)
					atomic.AddInt64(&inFlight, -1)
					return AttemptOutcome{}
				},
			)
			if execErr != nil {
				t.Errorf("execute: %v", execErr)
			}
		}(identities[index])
	}
	close(start)
	wg.Wait()
	if observed := atomic.LoadInt64(&maxSeen); observed > ceiling {
		t.Fatalf(
			"max concurrent physical attempts = %d, exceeds the configured "+
				"ceiling %d: every model request was supposed to share one "+
				"admission point",
			observed, ceiling,
		)
	} else if observed != ceiling {
		t.Fatalf(
			"max concurrent physical attempts = %d, want exactly %d "+
				"(the test never exercised real contention)",
			observed, ceiling,
		)
	}
}

// TestRetryExecutorRateLimitBackpressure is PIPE-071: a scripted provider
// fake returning 429 with and without Retry-After. It asserts, against the
// single shared executor and admission point built for PIPE-066/067/069:
//   - the backoff schedule honors Retry-After exactly when the provider
//     sends one, and falls back to jittered exponential backoff when it does
//     not;
//   - the admission ceiling narrows by one for each rate-limited
//     retry-scheduled disposition, and never widens back;
//   - no attempt is ever recorded AttemptFailed because of a rate limit —
//     "we asked and were told to wait" is a different ledger fact from "we
//     could not ask" or "the request itself was invalid", and collapsing
//     them would put a false verdict in an immutable ledger.
func TestRetryExecutorRateLimitBackpressure(t *testing.T) {
	t.Run("WithRetryAfter", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(
			writer http.ResponseWriter, _ *http.Request,
		) {
			requests++
			if requests == 1 {
				writer.Header().Set("Retry-After", "1")
				writer.WriteHeader(http.StatusTooManyRequests)
				_, _ = writer.Write([]byte(`{"error":"synthetic rate limit"}`))
				return
			}
			_, _ = writer.Write([]byte(`{"result":"accepted"}`))
		}))
		defer server.Close()
		transport, err := NewHTTPTransport(TransportOptions{HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}

		var (
			records []AttemptRecord
			slept   []time.Duration
		)
		executor, err := NewRetryExecutor(RetryPolicy{
			MaximumAttempts:       2,
			InitialDelay:          10 * time.Millisecond,
			MaximumBackoff:        50 * time.Millisecond,
			MaximumCumulativeWait: 3 * time.Second,
			MaximumConcurrency:    4,
			Sleep: func(_ context.Context, delay time.Duration) error {
				slept = append(slept, delay)
				return nil
			},
		}, AttemptRecorderFunctions{
			Prepare: func(context.Context, PhysicalAttempt) error { return nil },
			Complete: func(_ context.Context, record AttemptRecord) error {
				records = append(records, record)
				return nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := executor.ConcurrencyCeiling(); got != 4 {
			t.Fatalf("initial ceiling = %d, want 4", got)
		}

		_, err = executor.Execute(
			t.Context(),
			providerRetryIdentity(t, false),
			func(ctx context.Context, _ PhysicalAttempt) AttemptOutcome {
				var response struct {
					Result string `json:"result"`
				}
				_, requestErr := transport.DoJSON(ctx, HTTPRequest{
					Method: http.MethodPost, Endpoint: server.URL, Body: []byte(`{}`),
				}, &response)
				if requestErr == nil {
					return AttemptOutcome{}
				}
				var status *HTTPStatusError
				if !errors.As(requestErr, &status) {
					return AttemptOutcome{Err: requestErr}
				}
				return AttemptOutcome{Err: &Failure{
					Kind: FailureRateLimited, Retryable: true,
					RetryAfter: status.RetryAfter,
				}}
			},
		)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if requests != 2 {
			t.Fatalf("requests = %d, want 2", requests)
		}
		if len(records) != 2 ||
			records[0].Disposition != AttemptRetryScheduled ||
			records[1].Disposition != AttemptSucceeded {
			t.Fatalf("dispositions = %#v, want [retry-scheduled succeeded]", records)
		}
		for _, record := range records {
			if record.Disposition == AttemptFailed {
				t.Fatalf("a rate-limited attempt was recorded failed: %#v", record)
			}
		}
		if len(slept) != 1 || slept[0] != time.Second {
			t.Fatalf("slept = %v, want exactly the provider's Retry-After of 1s "+
				"(no jitter applied to explicit provider guidance)", slept)
		}
		if got := executor.ConcurrencyCeiling(); got != 3 {
			t.Fatalf("ceiling after one rate-limited retry = %d, want 3 (narrowed by one)", got)
		}
	})

	t.Run("WithoutRetryAfter", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(
			writer http.ResponseWriter, _ *http.Request,
		) {
			requests++
			if requests <= 2 {
				writer.WriteHeader(http.StatusTooManyRequests)
				_, _ = writer.Write([]byte(`{"error":"synthetic rate limit, no retry-after"}`))
				return
			}
			_, _ = writer.Write([]byte(`{"result":"accepted"}`))
		}))
		defer server.Close()
		transport, err := NewHTTPTransport(TransportOptions{HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}

		var (
			records []AttemptRecord
			slept   []time.Duration
		)
		executor, err := NewRetryExecutor(RetryPolicy{
			MaximumAttempts:       3,
			InitialDelay:          20 * time.Millisecond,
			MaximumBackoff:        20 * time.Millisecond,
			MaximumCumulativeWait: 3 * time.Second,
			MaximumConcurrency:    4,
			Random:                func() float64 { return 0.5 },
			Sleep: func(_ context.Context, delay time.Duration) error {
				slept = append(slept, delay)
				return nil
			},
		}, AttemptRecorderFunctions{
			Prepare: func(context.Context, PhysicalAttempt) error { return nil },
			Complete: func(_ context.Context, record AttemptRecord) error {
				records = append(records, record)
				return nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = executor.Execute(
			t.Context(),
			providerRetryIdentity(t, false),
			func(ctx context.Context, _ PhysicalAttempt) AttemptOutcome {
				var response struct {
					Result string `json:"result"`
				}
				_, requestErr := transport.DoJSON(ctx, HTTPRequest{
					Method: http.MethodPost, Endpoint: server.URL, Body: []byte(`{}`),
				}, &response)
				if requestErr == nil {
					return AttemptOutcome{}
				}
				var status *HTTPStatusError
				if !errors.As(requestErr, &status) {
					return AttemptOutcome{Err: requestErr}
				}
				return AttemptOutcome{Err: &Failure{
					Kind: FailureRateLimited, Retryable: true,
					RetryAfter: status.RetryAfter,
				}}
			},
		)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if requests != 3 {
			t.Fatalf("requests = %d, want 3", requests)
		}
		if len(records) != 3 ||
			records[0].Disposition != AttemptRetryScheduled ||
			records[1].Disposition != AttemptRetryScheduled ||
			records[2].Disposition != AttemptSucceeded {
			t.Fatalf(
				"dispositions = %#v, want [retry-scheduled retry-scheduled succeeded]",
				records,
			)
		}
		for _, record := range records {
			if record.Disposition == AttemptFailed {
				t.Fatalf("a rate-limited attempt was recorded failed: %#v", record)
			}
		}
		// InitialDelay=MaximumBackoff=20ms and Random pinned at 0.5, so every
		// jittered backoff is half + half*0.5 = 15ms regardless of attempt
		// number, since deterministicBackoff saturates immediately.
		want := []time.Duration{15 * time.Millisecond, 15 * time.Millisecond}
		if len(slept) != len(want) || slept[0] != want[0] || slept[1] != want[1] {
			t.Fatalf("slept = %v, want %v (jittered exponential backoff, no "+
				"provider guidance available)", slept, want)
		}
		if got := executor.ConcurrencyCeiling(); got != 2 {
			t.Fatalf(
				"ceiling after two rate-limited retries = %d, want 2 "+
					"(narrowed by one each time, sustained pressure)",
				got,
			)
		}
	})
}
