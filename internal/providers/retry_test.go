package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestRetryExecutorPreservesLogicalIdentityAndRecordsPhysicalAttempts(t *testing.T) {
	var (
		mu          sync.Mutex
		prepared    []PhysicalAttempt
		observed    []PhysicalAttempt
		records     []AttemptRecord
		sleepDelays []time.Duration
	)
	clock := newRetryTestClock()
	executor, err := NewRetryExecutor(RetryPolicy{
		MaximumAttempts:       4,
		InitialDelay:          10 * time.Millisecond,
		MaximumBackoff:        40 * time.Millisecond,
		MaximumCumulativeWait: 100 * time.Millisecond,
		Now:                   clock.Now,
		Sleep: func(_ context.Context, delay time.Duration) error {
			sleepDelays = append(sleepDelays, delay)
			clock.Advance(delay)
			return nil
		},
	}, AttemptRecorderFunctions{
		Prepare: func(_ context.Context, attempt PhysicalAttempt) error {
			mu.Lock()
			defer mu.Unlock()
			prepared = append(prepared, attempt)
			return nil
		},
		Complete: func(_ context.Context, record AttemptRecord) error {
			mu.Lock()
			defer mu.Unlock()
			records = append(records, record)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := providerRetryIdentity(t, true)
	result, err := executor.Execute(
		t.Context(),
		identity,
		func(_ context.Context, attempt PhysicalAttempt) AttemptOutcome {
			mu.Lock()
			preparedBeforeIO := len(prepared) == attempt.Number
			mu.Unlock()
			if !preparedBeforeIO {
				t.Fatalf("attempt %d began before durable preparation", attempt.Number)
			}
			observed = append(observed, attempt)
			if attempt.Number < 3 {
				return AttemptOutcome{Err: ErrTransport}
			}
			return AttemptOutcome{}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 3 ||
		result.CumulativeWait != 30*time.Millisecond ||
		!reflect.DeepEqual(sleepDelays, []time.Duration{
			10 * time.Millisecond,
			20 * time.Millisecond,
		}) {
		t.Fatalf("retry result=%#v sleep=%v", result, sleepDelays)
	}
	if len(prepared) != 3 || len(observed) != 3 || len(records) != 3 {
		t.Fatalf(
			"prepared=%d attempts=%d records=%d",
			len(prepared), len(observed), len(records),
		)
	}
	for _, attempt := range observed {
		if !reflect.DeepEqual(attempt.Identity, identity) {
			t.Fatalf("logical identity changed: %#v", attempt.Identity)
		}
	}
	wantDispositions := []AttemptDisposition{
		AttemptRetryScheduled,
		AttemptRetryScheduled,
		AttemptSucceeded,
	}
	for index, want := range wantDispositions {
		if records[index].Disposition != want {
			t.Fatalf("record %d disposition=%q want=%q", index, records[index].Disposition, want)
		}
	}
}

func TestRetryExecutorUsesScriptedHTTPRetryAfterWithoutChangingRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		requests++
		if request.Header.Get("X-Logical-Request") != strings.Repeat("a", 64) {
			t.Errorf("logical request header = %q", request.Header.Get("X-Logical-Request"))
		}
		if requests == 1 {
			writer.Header().Set("Retry-After", "2")
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
		prepared []int
		finished []AttemptDisposition
		guidance time.Duration
		slept    time.Duration
	)
	executor, err := NewRetryExecutor(
		RetryPolicy{
			MaximumAttempts:       2,
			InitialDelay:          time.Second,
			MaximumBackoff:        2 * time.Second,
			MaximumCumulativeWait: 3 * time.Second,
			Sleep: func(_ context.Context, delay time.Duration) error {
				slept += delay
				return nil
			},
		},
		AttemptRecorderFunctions{
			Prepare: func(_ context.Context, attempt PhysicalAttempt) error {
				prepared = append(prepared, attempt.Number)
				return nil
			},
			Complete: func(_ context.Context, record AttemptRecord) error {
				finished = append(finished, record.Disposition)
				if record.ProviderRetryAfter > 0 {
					guidance = record.ProviderRetryAfter
				}
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	identity := providerRetryIdentity(t, true)
	result, err := executor.Execute(
		t.Context(),
		identity,
		func(ctx context.Context, attempt PhysicalAttempt) AttemptOutcome {
			var response struct {
				Result string `json:"result"`
			}
			_, requestErr := transport.DoJSON(ctx, HTTPRequest{
				Method:   http.MethodPost,
				Endpoint: server.URL,
				Header: http.Header{
					"X-Logical-Request": []string{attempt.Identity.RequestHash},
				},
				Body: []byte(`{}`),
			}, &response)
			if requestErr == nil {
				if response.Result != "accepted" {
					t.Errorf("response = %#v", response)
				}
				return AttemptOutcome{}
			}
			var status *HTTPStatusError
			if !errors.As(requestErr, &status) {
				return AttemptOutcome{Err: requestErr}
			}
			return AttemptOutcome{Err: &Failure{
				Kind:       FailureRateLimited,
				Retryable:  true,
				RetryAfter: status.RetryAfter,
			}}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 ||
		result.Attempts != 2 ||
		slept != 2*time.Second ||
		guidance != 2*time.Second ||
		!reflect.DeepEqual(prepared, []int{1, 2}) ||
		!reflect.DeepEqual(finished, []AttemptDisposition{
			AttemptRetryScheduled,
			AttemptSucceeded,
		}) {
		t.Fatalf(
			"requests=%d result=%#v slept=%v guidance=%v prepared=%v finished=%v",
			requests, result, slept, guidance, prepared, finished,
		)
	}
}

func TestRetryExecutorDoesNotRetryNonTransientProviderFailures(t *testing.T) {
	for _, failure := range []error{
		ErrAuthentication,
		ErrInvalidRequest,
		ErrSafety,
		errors.New("unclassified provider failure"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			attempts := 0
			var disposition AttemptDisposition
			executor := retryExecutorFixture(t, RetryPolicy{}, func(
				_ context.Context,
				record AttemptRecord,
			) error {
				disposition = record.Disposition
				return nil
			})
			result, err := executor.Execute(
				t.Context(),
				providerRetryIdentity(t, false),
				func(context.Context, PhysicalAttempt) AttemptOutcome {
					attempts++
					return AttemptOutcome{Err: failure}
				},
			)
			if !errors.Is(err, failure) || attempts != 1 || result.Attempts != 1 ||
				disposition != AttemptFailed {
				t.Fatalf(
					"err=%v attempts=%d result=%#v disposition=%q",
					err, attempts, result, disposition,
				)
			}
		})
	}
}

func TestRetryExecutorBlocksPartialRetryWithoutProviderIdempotency(t *testing.T) {
	attempts := 0
	var record AttemptRecord
	executor := retryExecutorFixture(t, RetryPolicy{}, func(
		_ context.Context,
		value AttemptRecord,
	) error {
		record = value
		return nil
	})
	partial := &PartialEffectEvidence{StreamedOutput: true, LastSequence: 1}
	result, err := executor.Execute(
		t.Context(),
		providerRetryIdentity(t, false),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempts++
			return AttemptOutcome{
				Err: ErrTransport, PartialEvidence: partial,
			}
		},
	)
	if !errors.Is(err, ErrRetryUnsafeAfterPartial) || attempts != 1 ||
		result.Attempts != 1 ||
		record.Disposition != AttemptRetryUnsafePartial ||
		record.PartialEvidence != partial {
		t.Fatalf("result=%#v record=%#v err=%v", result, record, err)
	}
}

func TestRetryExecutorAllowsPartialRetryWithProviderIdempotency(t *testing.T) {
	attempts := 0
	executor := retryExecutorFixture(t, RetryPolicy{}, func(
		context.Context,
		AttemptRecord,
	) error {
		return nil
	})
	result, err := executor.Execute(
		t.Context(),
		providerRetryIdentity(t, true),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempts++
			if attempts == 1 {
				return AttemptOutcome{
					Err: ErrTimeout,
					PartialEvidence: &PartialEffectEvidence{
						ProviderAck: true,
					},
				}
			}
			return AttemptOutcome{}
		},
	)
	if err != nil || result.Attempts != 2 {
		t.Fatalf("result=%#v attempts=%d err=%v", result, attempts, err)
	}
}

func TestRetryExecutorBlocksObservableRetryEvenWithProviderIdempotency(
	t *testing.T,
) {
	identity := providerRetryIdentity(t, true)
	var records []AttemptRecord
	executor := retryExecutorFixture(
		t,
		RetryPolicy{MaximumAttempts: 2},
		func(_ context.Context, record AttemptRecord) error {
			records = append(records, record)
			return nil
		},
	)
	attempts := 0
	_, err := executor.Execute(
		context.Background(),
		identity,
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempts++
			return AttemptOutcome{
				Err: ErrTransport,
				PartialEvidence: &PartialEffectEvidence{
					StreamedOutput: true,
					ToolCall:       true,
					ProviderAck:    true,
					LastSequence:   1,
				},
			}
		},
	)
	if !errors.Is(err, ErrRetryUnsafeAfterPartial) || attempts != 1 {
		t.Fatalf(
			"observable idempotent retry attempts=%d err=%v",
			attempts,
			err,
		)
	}
	if len(records) != 1 ||
		records[0].Disposition != AttemptRetryUnsafePartial {
		t.Fatalf("observable retry records = %#v", records)
	}
}

func TestRetryExecutorUsesNormalizedFailureHintsAndRetryability(t *testing.T) {
	var slept time.Duration
	executor := retryExecutorFixture(t, RetryPolicy{
		MaximumAttempts:       3,
		InitialDelay:          10 * time.Millisecond,
		MaximumBackoff:        20 * time.Millisecond,
		MaximumCumulativeWait: 50 * time.Millisecond,
		Sleep: func(_ context.Context, delay time.Duration) error {
			slept += delay
			return nil
		},
	}, func(context.Context, AttemptRecord) error { return nil })
	attempts := 0
	result, err := executor.Execute(
		t.Context(),
		providerRetryIdentity(t, true),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempts++
			if attempts == 1 {
				return AttemptOutcome{Err: &Failure{
					Kind:       FailureRateLimited,
					Retryable:  true,
					RetryAfter: 30 * time.Millisecond,
				}}
			}
			return AttemptOutcome{}
		},
	)
	if err != nil || result.Attempts != 2 || slept != 30*time.Millisecond {
		t.Fatalf("result=%#v attempts=%d slept=%v err=%v", result, attempts, slept, err)
	}

	attempts = 0
	slept = 0
	_, err = executor.Execute(
		t.Context(),
		providerRetryIdentity(t, false),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempts++
			return AttemptOutcome{Err: &Failure{
				Kind:      FailureTransport,
				Retryable: false,
			}}
		},
	)
	if err == nil || attempts != 1 || slept != 0 {
		t.Fatalf("nonretryable failure attempts=%d slept=%v err=%v", attempts, slept, err)
	}
}

func TestRetryExecutorDerivesPartialEvidenceFromNormalizedFailure(t *testing.T) {
	executor := retryExecutorFixture(t, RetryPolicy{}, func(
		context.Context,
		AttemptRecord,
	) error {
		return nil
	})
	_, err := executor.Execute(
		t.Context(),
		providerRetryIdentity(t, false),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			return AttemptOutcome{Err: &Failure{
				Kind:      FailureTransport,
				Retryable: true,
				PartialEffect: PartialEffectEvidence{
					ToolCall: true,
				},
			}}
		},
	)
	if !errors.Is(err, ErrRetryUnsafeAfterPartial) {
		t.Fatalf("partial failure error = %v", err)
	}
}

func TestRetryExecutorRespectsRetryAfterAndRemainingBudget(t *testing.T) {
	var slept time.Duration
	executor := retryExecutorFixture(t, RetryPolicy{
		MaximumAttempts:       3,
		InitialDelay:          10 * time.Millisecond,
		MaximumBackoff:        20 * time.Millisecond,
		MaximumCumulativeWait: 50 * time.Millisecond,
		Sleep: func(_ context.Context, delay time.Duration) error {
			slept += delay
			return nil
		},
	}, func(context.Context, AttemptRecord) error { return nil })
	attempts := 0
	result, err := executor.Execute(
		t.Context(),
		providerRetryIdentity(t, false),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempts++
			if attempts == 1 {
				return AttemptOutcome{
					Err: ErrRateLimited, RetryAfter: 40 * time.Millisecond,
				}
			}
			return AttemptOutcome{}
		},
	)
	if err != nil || result.Attempts != 2 || slept != 40*time.Millisecond {
		t.Fatalf("result=%#v slept=%v err=%v", result, slept, err)
	}

	attempts = 0
	slept = 0
	result, err = executor.Execute(
		t.Context(),
		providerRetryIdentity(t, false),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempts++
			return AttemptOutcome{
				Err: ErrRateLimited, RetryAfter: 51 * time.Millisecond,
			}
		},
	)
	if !errors.Is(err, ErrRetryBudgetExhausted) || attempts != 1 ||
		slept != 0 || result.Attempts != 1 {
		t.Fatalf("result=%#v attempts=%d slept=%v err=%v", result, attempts, slept, err)
	}
}

func TestRetryExecutorRecordsCanceledAttemptWithIndependentContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	recorded := false
	executor := retryExecutorFixture(t, RetryPolicy{}, func(
		recordContext context.Context,
		record AttemptRecord,
	) error {
		if recordContext.Err() != nil {
			t.Fatalf("attempt record context inherited cancellation: %v", recordContext.Err())
		}
		recorded = true
		if record.Disposition != AttemptCanceled {
			t.Fatalf("canceled disposition = %q", record.Disposition)
		}
		return nil
	})
	result, err := executor.Execute(
		ctx,
		providerRetryIdentity(t, false),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			cancel()
			return AttemptOutcome{Err: context.Canceled}
		},
	)
	if !errors.Is(err, context.Canceled) || result.Attempts != 1 || !recorded {
		t.Fatalf("result=%#v recorded=%t err=%v", result, recorded, err)
	}
}

func TestRetryExecutorStopsWhenAttemptCannotBeRecorded(t *testing.T) {
	attempts := 0
	recordFailure := errors.New("storage unavailable")
	executor := retryExecutorFixture(t, RetryPolicy{}, func(
		context.Context,
		AttemptRecord,
	) error {
		return recordFailure
	})
	result, err := executor.Execute(
		t.Context(),
		providerRetryIdentity(t, false),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempts++
			return AttemptOutcome{Err: ErrTransport}
		},
	)
	if !errors.Is(err, recordFailure) || !errors.Is(err, ErrTransport) ||
		attempts != 1 || result.Attempts != 1 ||
		!strings.Contains(err.Error(), "complete physical provider attempt") {
		t.Fatalf("result=%#v attempts=%d err=%v", result, attempts, err)
	}
}

func TestRetryExecutorPreparationFailurePreventsExternalIO(t *testing.T) {
	prepareFailure := errors.New("durable prepare unavailable")
	executor, err := NewRetryExecutor(
		RetryPolicy{},
		AttemptRecorderFunctions{
			Prepare: func(context.Context, PhysicalAttempt) error {
				return prepareFailure
			},
			Complete: func(context.Context, AttemptRecord) error {
				t.Fatal("unstarted attempt was completed")
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	attempted := false
	result, err := executor.Execute(
		t.Context(),
		providerRetryIdentity(t, false),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempted = true
			return AttemptOutcome{}
		},
	)
	if !errors.Is(err, prepareFailure) ||
		!strings.Contains(err.Error(), "prepare physical provider attempt") ||
		attempted ||
		result.Attempts != 0 {
		t.Fatalf("result=%#v attempted=%t err=%v", result, attempted, err)
	}
}

func TestRetryExecutorExhaustionReturnsPauseBoundary(t *testing.T) {
	var records []AttemptRecord
	executor := retryExecutorFixture(t, RetryPolicy{
		MaximumAttempts: 2,
	}, func(_ context.Context, record AttemptRecord) error {
		records = append(records, record)
		return nil
	})
	result, err := executor.Execute(
		t.Context(),
		providerRetryIdentity(t, false),
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			return AttemptOutcome{Err: ErrUnavailable}
		},
	)
	if !errors.Is(err, ErrRetryBudgetExhausted) ||
		result.Attempts != 2 ||
		len(records) != 2 ||
		records[1].Disposition != AttemptRetryExhausted {
		t.Fatalf("result=%#v records=%#v err=%v", result, records, err)
	}
}

func TestRetryExecutorRejectsUnattributableIdentityBeforeIO(t *testing.T) {
	executor := retryExecutorFixture(t, RetryPolicy{}, func(
		context.Context,
		AttemptRecord,
	) error {
		t.Fatal("invalid identity was recorded")
		return nil
	})
	attempted := false
	_, err := executor.Execute(
		t.Context(),
		RequestIdentity{},
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempted = true
			return AttemptOutcome{}
		},
	)
	if !errors.Is(err, ErrInvalidRequestIdentity) || attempted {
		t.Fatalf("attempted=%t err=%v", attempted, err)
	}

	identity := providerRetryIdentity(t, true)
	identity.Model.Provider.Provider = "silently-switched-provider"
	_, err = executor.Execute(
		t.Context(),
		identity,
		func(context.Context, PhysicalAttempt) AttemptOutcome {
			attempted = true
			return AttemptOutcome{}
		},
	)
	if !errors.Is(err, ErrInvalidRequestIdentity) || attempted {
		t.Fatalf("provider/model mismatch attempted=%t err=%v", attempted, err)
	}
}

func retryExecutorFixture(
	t *testing.T,
	policy RetryPolicy,
	record func(context.Context, AttemptRecord) error,
) *RetryExecutor {
	t.Helper()
	if policy.Sleep == nil {
		policy.Sleep = func(context.Context, time.Duration) error { return nil }
	}
	executor, err := NewRetryExecutor(policy, AttemptRecorderFunctions{
		Prepare:  func(context.Context, PhysicalAttempt) error { return nil },
		Complete: record,
	})
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func providerRetryIdentity(
	t *testing.T,
	providerIdempotency bool,
) RequestIdentity {
	t.Helper()
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	provider := ProviderIdentity{
		Adapter: "scripted", AdapterVersion: "1",
		Provider: "fixture", ProviderVersion: "2026-07-30",
	}
	model := ModelIdentity{
		Provider: provider, Model: "fixture-model", Revision: "revision-1",
	}
	identity := RequestIdentity{
		ModelRequestID: requestID,
		Provider:       provider,
		Model:          model,
		IdempotencyKey: "synthetic-logical-idempotency-key",
		RequestHash:    strings.Repeat("a", 64),
	}
	if providerIdempotency {
		identity.Idempotency = RequestIdempotency{
			ProviderSupported: true,
			Key:               "synthetic-idempotency-key",
			ProviderScope:     "fixture",
		}
		identity.IdempotencyKey = identity.Idempotency.Key
	}
	return identity
}

type retryTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newRetryTestClock() *retryTestClock {
	return &retryTestClock{
		now: time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC),
	}
}

func (clock *retryTestClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *retryTestClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}
