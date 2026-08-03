package providers

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"time"
)

const (
	defaultMaximumProviderAttempts      = 3
	defaultProviderRetryDelay           = 250 * time.Millisecond
	defaultMaximumProviderBackoff       = 5 * time.Second
	defaultMaximumProviderRetryWait     = 15 * time.Second
	defaultAttemptObserverTimeout       = 5 * time.Second
	defaultMaximumProviderConcurrency   = 4
	maximumSupportedProviderConcurrency = 64
)

var (
	ErrInvalidRequestIdentity  = errors.New("provider request identity is invalid")
	ErrRetryBudgetExhausted    = errors.New("provider retry budget exhausted")
	ErrRetryUnsafeAfterPartial = errors.New("provider retry is unsafe after partial observable effects")
)

// RetryPolicy bounds physical attempts and cumulative provider-directed wait.
type RetryPolicy struct {
	MaximumAttempts       int
	InitialDelay          time.Duration
	MaximumBackoff        time.Duration
	MaximumCumulativeWait time.Duration
	ObserverTimeout       time.Duration
	// MaximumConcurrency is the configured ceiling on physical provider
	// requests in flight at once through this executor's single admission
	// point. It defaults to defaultMaximumProviderConcurrency rather than to
	// unbounded, so the ceiling is always a setting and never an accident of
	// however wide the caller's own fan-out happens to be.
	MaximumConcurrency int
	Now                func() time.Time
	Sleep              func(context.Context, time.Duration) error
	// Random returns a value in [0, 1) used to jitter computed backoff delays.
	// It is injectable so a test can assert an exact schedule; production
	// callers leave it nil and get a real random source.
	Random func() float64
}

// AttemptDisposition is the persisted outcome of one physical request attempt.
type AttemptDisposition string

const (
	AttemptSucceeded          AttemptDisposition = "succeeded"
	AttemptFailed             AttemptDisposition = "failed"
	AttemptRetryScheduled     AttemptDisposition = "retry-scheduled"
	AttemptRetryExhausted     AttemptDisposition = "retry-exhausted"
	AttemptRetryUnsafePartial AttemptDisposition = "retry-unsafe-partial"
	AttemptCanceled           AttemptDisposition = "canceled"
)

// PhysicalAttempt identifies one physical request for an immutable logical
// request.
type PhysicalAttempt struct {
	Identity  RequestIdentity
	Number    int
	StartedAt time.Time
}

// AttemptOutcome describes the adapter-observed result of one physical
// request. RetryAfter is provider guidance, not permission to exceed policy.
type AttemptOutcome struct {
	Err               error
	RetryAfter        time.Duration
	PartialEvidence   *PartialEffectEvidence
	ProviderRequestID string
	SafeMetadata      *RedactedProviderMetadata
	FirstResponseAt   time.Time
}

// AttemptRecord is safe lifecycle evidence for one physical provider request.
// Recorders must map Err through the normalized failure boundary before
// persistence rather than storing arbitrary error text.
type AttemptRecord struct {
	PhysicalAttempt
	FinishedAt         time.Time
	Disposition        AttemptDisposition
	ProviderRetryAfter time.Duration
	RetryDelay         time.Duration
	Err                error
	PartialEvidence    *PartialEffectEvidence
	ProviderRequestID  string
	SafeMetadata       *RedactedProviderMetadata
	FirstResponseAt    time.Time
}

// AttemptRecorder prepares each physical request durably before external I/O,
// then completes its terminal accounting record afterward.
type AttemptRecorder interface {
	PrepareProviderAttempt(context.Context, PhysicalAttempt) error
	CompleteProviderAttempt(context.Context, AttemptRecord) error
}

// AttemptRecorderFunctions adapts explicit prepare and complete functions to
// AttemptRecorder. Both functions are required.
type AttemptRecorderFunctions struct {
	Prepare  func(context.Context, PhysicalAttempt) error
	Complete func(context.Context, AttemptRecord) error
}

func (functions AttemptRecorderFunctions) PrepareProviderAttempt(
	ctx context.Context,
	attempt PhysicalAttempt,
) error {
	if functions.Prepare == nil {
		return errors.New("prepare provider attempt function is required")
	}
	return functions.Prepare(ctx, attempt)
}

func (functions AttemptRecorderFunctions) CompleteProviderAttempt(
	ctx context.Context,
	attempt AttemptRecord,
) error {
	if functions.Complete == nil {
		return errors.New("complete provider attempt function is required")
	}
	return functions.Complete(ctx, attempt)
}

// RetryResult summarizes the bounded physical work for one logical request.
type RetryResult struct {
	Identity       RequestIdentity
	Attempts       int
	CumulativeWait time.Duration
	LastOutcome    AttemptOutcome
}

// RetryExecutor owns bounded retry without changing provider, model, version,
// logical request, or idempotency identity. It is also the single admission
// point every physical attempt passes through: one *RetryExecutor holds one
// *AdmissionController, so every caller that shares this executor instance
// (as production does, through providerExecutionService) shares its
// concurrency ceiling rather than each caller imposing its own.
type RetryExecutor struct {
	policy    RetryPolicy
	recorder  AttemptRecorder
	admission *AdmissionController
}

// MaximumAttempts returns the normalized physical-attempt limit enforced by
// Execute. Coordinators use this value when reserving the complete retry bound
// before any provider I/O begins.
func (executor *RetryExecutor) MaximumAttempts() int {
	if executor == nil {
		return 0
	}
	return executor.policy.MaximumAttempts
}

// ConcurrencyCeiling returns the admission point's current configured
// maximum concurrency. It narrows under sustained rate limiting (PIPE-069)
// and never widens back within one executor's lifetime.
func (executor *RetryExecutor) ConcurrencyCeiling() int {
	if executor == nil || executor.admission == nil {
		return 0
	}
	return executor.admission.Ceiling()
}

// NewRetryExecutor validates a retry policy and requires an attempt recorder so
// no physical request can escape attribution.
func NewRetryExecutor(
	policy RetryPolicy,
	recorder AttemptRecorder,
) (*RetryExecutor, error) {
	if policy.MaximumAttempts == 0 {
		policy.MaximumAttempts = defaultMaximumProviderAttempts
	}
	if policy.InitialDelay == 0 {
		policy.InitialDelay = defaultProviderRetryDelay
	}
	if policy.MaximumBackoff == 0 {
		policy.MaximumBackoff = defaultMaximumProviderBackoff
	}
	if policy.MaximumCumulativeWait == 0 {
		policy.MaximumCumulativeWait = defaultMaximumProviderRetryWait
	}
	if policy.ObserverTimeout == 0 {
		policy.ObserverTimeout = defaultAttemptObserverTimeout
	}
	if policy.MaximumConcurrency == 0 {
		policy.MaximumConcurrency = defaultMaximumProviderConcurrency
	}
	switch {
	case policy.MaximumAttempts < 1 || policy.MaximumAttempts > 10:
		return nil, errors.New("provider maximum attempts is outside supported bounds")
	case policy.InitialDelay < time.Millisecond ||
		policy.InitialDelay > maximumProviderRetryAfter:
		return nil, errors.New("provider initial retry delay is outside supported bounds")
	case policy.MaximumBackoff < policy.InitialDelay ||
		policy.MaximumBackoff > maximumProviderRetryAfter:
		return nil, errors.New("provider maximum backoff is outside supported bounds")
	case policy.MaximumCumulativeWait < policy.InitialDelay ||
		policy.MaximumCumulativeWait > maximumProviderRetryAfter:
		return nil, errors.New("provider cumulative retry wait is outside supported bounds")
	case policy.ObserverTimeout < time.Millisecond ||
		policy.ObserverTimeout > time.Minute:
		return nil, errors.New("provider attempt observer timeout is outside supported bounds")
	case policy.MaximumConcurrency < 1 ||
		policy.MaximumConcurrency > maximumSupportedProviderConcurrency:
		return nil, errors.New("provider maximum concurrency is outside supported bounds")
	case recorder == nil:
		return nil, errors.New("provider attempt recorder is required")
	}
	if policy.Now == nil {
		policy.Now = time.Now
	}
	if policy.Sleep == nil {
		policy.Sleep = sleepWithContext
	}
	if policy.Random == nil {
		policy.Random = rand.Float64
	}
	admission, err := NewAdmissionController(policy.MaximumConcurrency)
	if err != nil {
		return nil, fmt.Errorf("build provider admission point: %w", err)
	}
	return &RetryExecutor{policy: policy, recorder: recorder, admission: admission}, nil
}

// Execute runs one immutable logical request with bounded, attributable
// physical retries.
func (executor *RetryExecutor) Execute(
	ctx context.Context,
	identity RequestIdentity,
	attempt func(context.Context, PhysicalAttempt) AttemptOutcome,
) (RetryResult, error) {
	result := RetryResult{Identity: identity}
	if err := ValidateRequestIdentity(identity); err != nil {
		return result, err
	}
	if attempt == nil {
		return result, errors.New("provider attempt function is required")
	}
	for number := 1; number <= executor.policy.MaximumAttempts; number++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		physical := PhysicalAttempt{
			Identity:  identity,
			Number:    number,
			StartedAt: executor.policy.Now().UTC(),
		}
		if err := executor.prepare(ctx, physical); err != nil {
			return result, err
		}
		// Every physical attempt is admitted through the one shared ceiling
		// before it reaches the provider (PIPE-066). The slot is held only for
		// the provider call itself, not the backoff sleep that may follow, so a
		// request backing off does not hold capacity another logical request
		// could use.
		release, admitErr := executor.admission.Acquire(ctx)
		if admitErr != nil {
			return result, fmt.Errorf("admit physical provider attempt: %w", admitErr)
		}
		outcome := attempt(ctx, physical)
		release()
		outcome = normalizeAttemptOutcome(outcome)
		finishedAt := executor.policy.Now().UTC()
		result.Attempts = number
		result.LastOutcome = outcome

		disposition, delay, terminalErr := executor.decide(
			ctx,
			identity,
			number,
			result.CumulativeWait,
			outcome,
		)
		// PIPE-069: a rate-limit or overload response is sustained pressure the
		// provider itself reported, not a transient blip, so the admission
		// point narrows for the remainder of this executor's life rather than
		// retrying the next logical request at full width into the same limit.
		if disposition == AttemptRetryScheduled && isProviderBackpressureFailure(outcome.Err) {
			executor.admission.Narrow(executor.admission.Ceiling() - 1)
		}
		record := AttemptRecord{
			PhysicalAttempt:    physical,
			FinishedAt:         finishedAt,
			Disposition:        disposition,
			ProviderRetryAfter: outcome.RetryAfter,
			RetryDelay:         delay,
			Err:                outcome.Err,
			PartialEvidence:    outcome.PartialEvidence,
			ProviderRequestID:  outcome.ProviderRequestID,
			SafeMetadata:       cloneRedactedProviderMetadata(outcome.SafeMetadata),
			FirstResponseAt:    outcome.FirstResponseAt,
		}
		recordErr := executor.complete(ctx, record)
		if recordErr != nil {
			if outcome.Err != nil {
				return result, errors.Join(outcome.Err, recordErr)
			}
			return result, recordErr
		}
		if terminalErr != nil {
			return result, terminalErr
		}
		if disposition == AttemptSucceeded {
			return result, nil
		}
		if disposition == AttemptFailed || disposition == AttemptCanceled {
			return result, outcome.Err
		}

		result.CumulativeWait += delay
		if err := executor.policy.Sleep(ctx, delay); err != nil {
			return result, err
		}
	}
	return result, ErrRetryBudgetExhausted
}

// ValidateRequestIdentity requires complete, internally consistent attribution
// before any physical provider request may begin.
func ValidateRequestIdentity(identity RequestIdentity) error {
	if identity.ModelRequestID.IsZero() {
		return fmt.Errorf("%w: model request ID is required", ErrInvalidRequestIdentity)
	}
	required := []struct {
		name  string
		value string
	}{
		{"adapter", identity.Provider.Adapter},
		{"adapter version", identity.Provider.AdapterVersion},
		{"provider", identity.Provider.Provider},
		{"provider version", identity.Provider.ProviderVersion},
		{"model", identity.Model.Model},
		{"model revision", identity.Model.Revision},
		{"request hash", identity.RequestHash},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) != field.value ||
			field.value == "" ||
			len(field.value) > 255 {
			return fmt.Errorf(
				"%w: %s must be non-empty, trimmed, and at most 255 bytes",
				ErrInvalidRequestIdentity,
				field.name,
			)
		}
	}
	if !reflect.DeepEqual(identity.Provider, identity.Model.Provider) {
		return fmt.Errorf(
			"%w: model provider does not match request provider",
			ErrInvalidRequestIdentity,
		)
	}
	if !validLowerSHA256(identity.RequestHash) {
		return fmt.Errorf(
			"%w: request hash must be a lowercase SHA-256 digest",
			ErrInvalidRequestIdentity,
		)
	}
	if len(identity.Idempotency.Key) > 255 ||
		len(identity.Idempotency.ProviderScope) > 255 ||
		len(identity.IdempotencyKey) > 255 {
		return fmt.Errorf(
			"%w: idempotency fields exceed 255 bytes",
			ErrInvalidRequestIdentity,
		)
	}
	if strings.TrimSpace(identity.IdempotencyKey) != identity.IdempotencyKey ||
		identity.IdempotencyKey == "" {
		return fmt.Errorf(
			"%w: logical idempotency key is required",
			ErrInvalidRequestIdentity,
		)
	}
	if identity.Idempotency.ProviderSupported &&
		(strings.TrimSpace(identity.Idempotency.Key) == "" ||
			strings.TrimSpace(identity.Idempotency.ProviderScope) == "") {
		return fmt.Errorf(
			"%w: provider idempotency requires a key and provider scope",
			ErrInvalidRequestIdentity,
		)
	}
	if identity.Idempotency.ProviderSupported &&
		identity.IdempotencyKey != identity.Idempotency.Key {
		return fmt.Errorf(
			"%w: idempotency keys disagree",
			ErrInvalidRequestIdentity,
		)
	}
	if !identity.Idempotency.ProviderSupported &&
		(identity.Idempotency.Key != "" ||
			identity.Idempotency.ProviderScope != "") {
		return fmt.Errorf(
			"%w: unsupported provider idempotency must not carry provider fields",
			ErrInvalidRequestIdentity,
		)
	}
	return nil
}

func validLowerSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character >= '0' && character <= '9' ||
			character >= 'a' && character <= 'f' {
			continue
		}
		return false
	}
	return true
}

func (executor *RetryExecutor) decide(
	ctx context.Context,
	identity RequestIdentity,
	number int,
	cumulativeWait time.Duration,
	outcome AttemptOutcome,
) (AttemptDisposition, time.Duration, error) {
	if outcome.Err == nil {
		return AttemptSucceeded, 0, nil
	}
	if ctx.Err() != nil || errors.Is(outcome.Err, context.Canceled) {
		return AttemptCanceled, 0, outcome.Err
	}
	if !retryableProviderFailure(outcome.Err) {
		return AttemptFailed, 0, outcome.Err
	}
	if hasObservablePartialEffect(outcome.PartialEvidence) ||
		outcome.PartialEvidence != nil &&
			outcome.PartialEvidence.ProviderAck &&
			!identity.Idempotency.ProviderSupported {
		return AttemptRetryUnsafePartial, 0, fmt.Errorf(
			"%w: %w",
			ErrRetryUnsafeAfterPartial,
			outcome.Err,
		)
	}
	if number >= executor.policy.MaximumAttempts {
		return AttemptRetryExhausted, 0, fmt.Errorf(
			"%w: %w",
			ErrRetryBudgetExhausted,
			outcome.Err,
		)
	}
	delay := outcome.RetryAfter
	if delay < 0 {
		return AttemptFailed, 0, errors.New("provider retry-after must not be negative")
	}
	if delay == 0 {
		delay = executor.backoff(number)
	}
	if delay > maximumProviderRetryAfter ||
		delay > executor.policy.MaximumCumulativeWait-cumulativeWait {
		return AttemptRetryExhausted, 0, fmt.Errorf(
			"%w: provider retry-after exceeds remaining wait budget: %w",
			ErrRetryBudgetExhausted,
			outcome.Err,
		)
	}
	if deadline, present := ctx.Deadline(); present &&
		!executor.policy.Now().Add(delay).Before(deadline) {
		return AttemptRetryExhausted, 0, fmt.Errorf(
			"%w: provider retry delay exceeds request deadline: %w",
			ErrRetryBudgetExhausted,
			outcome.Err,
		)
	}
	return AttemptRetryScheduled, delay, nil
}

// backoff computes the exponential base for a failed attempt and jitters it,
// so that many logical requests failing at once do not all wake and retry on
// the same tick and drive the provider straight back into the limit that just
// backed them off.
func (executor *RetryExecutor) backoff(failedAttempt int) time.Duration {
	return executor.jitter(executor.deterministicBackoff(failedAttempt))
}

// deterministicBackoff is the pre-jitter exponential schedule: it doubles
// from InitialDelay and saturates at MaximumBackoff.
func (executor *RetryExecutor) deterministicBackoff(failedAttempt int) time.Duration {
	delay := executor.policy.InitialDelay
	for index := 1; index < failedAttempt; index++ {
		if delay >= executor.policy.MaximumBackoff/2 {
			return executor.policy.MaximumBackoff
		}
		delay *= 2
	}
	if delay > executor.policy.MaximumBackoff {
		return executor.policy.MaximumBackoff
	}
	return delay
}

// jitter applies equal jitter to base: the result is uniformly distributed
// over [base/2, base]. Equal rather than full jitter, so a narrowed admission
// ceiling is never chased by attempts that jittered all the way down to
// near-zero wait and reproduced the same burst that caused the narrowing.
func (executor *RetryExecutor) jitter(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	half := base / 2
	fraction := executor.policy.Random()
	if fraction < 0 {
		fraction = 0
	}
	if fraction >= 1 {
		fraction = 1
	}
	return half + time.Duration(float64(half)*fraction)
}

func (executor *RetryExecutor) prepare(
	ctx context.Context,
	attempt PhysicalAttempt,
) error {
	if err := executor.recorder.PrepareProviderAttempt(ctx, attempt); err != nil {
		return fmt.Errorf("prepare physical provider attempt: %w", err)
	}
	return nil
}

func (executor *RetryExecutor) complete(
	ctx context.Context,
	attempt AttemptRecord,
) error {
	recordContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		executor.policy.ObserverTimeout,
	)
	defer cancel()
	if err := executor.recorder.CompleteProviderAttempt(recordContext, attempt); err != nil {
		return fmt.Errorf("complete physical provider attempt: %w", err)
	}
	return nil
}

// isProviderBackpressureFailure reports whether err is the provider itself
// signalling it is overloaded — a 429 or an overload response — rather than
// an ordinary transient transport failure. Only this narrower class narrows
// the admission ceiling: a generic transport hiccup says nothing about the
// provider's rate limit and narrowing on it would shrink concurrency for a
// problem backoff alone already answers.
func isProviderBackpressureFailure(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrUnavailable)
}

func retryableProviderFailure(err error) bool {
	var failure *Failure
	if errors.As(err, &failure) && !failure.Retryable {
		return false
	}
	return errors.Is(err, ErrTransport) ||
		errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrUnavailable)
}

func normalizeAttemptOutcome(outcome AttemptOutcome) AttemptOutcome {
	var failure *Failure
	if !errors.As(outcome.Err, &failure) {
		return outcome
	}
	if outcome.RetryAfter == 0 {
		outcome.RetryAfter = failure.RetryAfter
	}
	if outcome.PartialEvidence == nil &&
		hasPartialEffectEvidence(&failure.PartialEffect) {
		partial := failure.PartialEffect
		outcome.PartialEvidence = &partial
	}
	return outcome
}

func hasPartialEffectEvidence(evidence *PartialEffectEvidence) bool {
	return evidence != nil &&
		(evidence.StreamedOutput ||
			evidence.ToolCall ||
			evidence.ProviderAck ||
			evidence.LastSequence != 0)
}

func hasObservablePartialEffect(evidence *PartialEffectEvidence) bool {
	return evidence != nil &&
		(evidence.StreamedOutput ||
			evidence.ToolCall ||
			evidence.LastSequence != 0)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
