package testfixtures

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// M22-107 requires the scripted provider cover every step a real provider can
// produce, not only success. These are the failure and timing steps; the
// success steps (text, tool call, usage) live in provider.go.
var (
	// ErrFixtureRateLimited is a provider refusing for capacity reasons. It is
	// retryable: the request was never accepted, so retrying duplicates
	// nothing.
	ErrFixtureRateLimited = errors.New("fixture provider rate limited")
	// ErrFixtureAuthentication is a rejected credential. It is NOT retryable,
	// and a system that retries it will loop forever against a wrong key.
	ErrFixtureAuthentication = errors.New("fixture provider rejected the credential")
	// ErrFixtureCancelled is work stopped by the caller.
	ErrFixtureCancelled = errors.New("fixture provider call was cancelled")
	// ErrFixturePartialStream is a stream that produced output and then broke.
	// This is the dangerous one: output already reached the caller, so the
	// outcome is genuinely ambiguous rather than simply failed.
	ErrFixturePartialStream = errors.New("fixture provider stream ended mid-response")
)

// StepKind names one scripted provider behaviour (M22-107).
type StepKind string

const (
	StepText          StepKind = "text"
	StepToolCall      StepKind = "tool-call"
	StepUsageOnly     StepKind = "usage-only"
	StepPartialStream StepKind = "partial-stream"
	StepDelay         StepKind = "delay"
	StepRateLimit     StepKind = "rate-limit"
	StepAuthFailure   StepKind = "authentication-failure"
	StepCancellation  StepKind = "cancellation"
)

// AllStepKinds returns every scripted behaviour, so a test can assert it
// covered the whole surface rather than the parts it remembered.
func AllStepKinds() []StepKind {
	return []StepKind{
		StepText, StepToolCall, StepUsageOnly, StepPartialStream,
		StepDelay, StepRateLimit, StepAuthFailure, StepCancellation,
	}
}

// Retryable reports whether repeating a call that ended this way is safe.
//
// The classification is the point of the type. A rate limit may be retried; a
// rejected credential may not, because retrying it is an infinite loop; and a
// partial stream may not be retried blindly, because the first attempt already
// produced output that may have had effects.
func (kind StepKind) Retryable() bool {
	switch kind {
	case StepRateLimit, StepDelay:
		return true
	case StepAuthFailure, StepCancellation, StepPartialStream:
		return false
	default:
		return true
	}
}

// Terminal reports whether this step ends the exchange.
func (kind StepKind) Terminal() bool {
	switch kind {
	case StepAuthFailure, StepCancellation, StepPartialStream:
		return true
	default:
		return false
	}
}

// Valid reports whether a kind is one of the declared set.
func (kind StepKind) Valid() bool {
	for _, candidate := range AllStepKinds() {
		if candidate == kind {
			return true
		}
	}
	return false
}

// ProviderStep is one scripted behaviour with the data that behaviour needs.
type ProviderStep struct {
	Kind StepKind
	// Text is the assistant output for StepText, and the output emitted
	// BEFORE the break for StepPartialStream.
	Text string
	// ToolName and ToolArgumentsJSON apply to StepToolCall.
	ToolName          string
	ToolArgumentsJSON string
	// Usage is the token accounting this step reports. A step that failed can
	// still have consumed tokens, and a fixture that zeroed it would let a
	// budget test pass while real spend continued.
	Usage FixtureUsage
	// Delay applies to StepDelay: how long the provider takes before
	// answering. It is honoured against the fixture clock, never slept.
	Delay time.Duration
	// RetryAfter applies to StepRateLimit.
	RetryAfter time.Duration
}

// Validate rejects a step whose data does not match its kind.
func (step ProviderStep) Validate() error {
	if !step.Kind.Valid() {
		return fmt.Errorf("unknown provider step kind %q", step.Kind)
	}
	switch step.Kind {
	case StepText:
		if strings.TrimSpace(step.Text) == "" {
			return errors.New("a text step must carry text")
		}
	case StepToolCall:
		if strings.TrimSpace(step.ToolName) == "" || strings.TrimSpace(step.ToolArgumentsJSON) == "" {
			return errors.New("a tool-call step must carry a tool name and arguments")
		}
	case StepPartialStream:
		if strings.TrimSpace(step.Text) == "" {
			return errors.New(
				"a partial-stream step must carry the output emitted before the break, " +
					"or it is indistinguishable from a plain failure")
		}
	case StepDelay:
		if step.Delay <= 0 {
			return errors.New("a delay step must carry a positive delay")
		}
	case StepRateLimit:
		if step.RetryAfter <= 0 {
			return errors.New("a rate-limit step must state when a retry becomes acceptable")
		}
	}
	return nil
}

// Err returns the error this step produces, or nil for a successful step.
func (step ProviderStep) Err() error {
	switch step.Kind {
	case StepPartialStream:
		return ErrFixturePartialStream
	case StepRateLimit:
		return fmt.Errorf("%w: retry after %s", ErrFixtureRateLimited, step.RetryAfter)
	case StepAuthFailure:
		return ErrFixtureAuthentication
	case StepCancellation:
		return ErrFixtureCancelled
	default:
		return nil
	}
}

// StepProvider replays a ProviderStep script (M22-107).
//
// It advances a fixture clock instead of sleeping, so a scripted delay costs a
// test nothing in wall time while still being observable. A fake that slept
// would make the suite slow in exchange for no additional coverage.
type StepProvider struct {
	mutex    sync.Mutex
	steps    []ProviderStep
	served   int
	clock    *SteppingClock
	elapsed  time.Duration
	observed []StepKind
}

// NewStepProvider validates and freezes a step script.
func NewStepProvider(steps ...ProviderStep) (*StepProvider, error) {
	if len(steps) == 0 {
		return nil, errors.New("a step provider requires at least one step")
	}
	for index, step := range steps {
		if err := step.Validate(); err != nil {
			return nil, fmt.Errorf("step %d: %w", index, err)
		}
	}
	clock, err := NewSteppingClock(time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("build fixture clock: %w", err)
	}
	frozen := make([]ProviderStep, len(steps))
	copy(frozen, steps)
	return &StepProvider{steps: frozen, clock: clock}, nil
}

// Next serves the next step. A cancelled context wins over the script, because
// the real provider port must honour cancellation regardless of what it was
// about to return.
func (provider *StepProvider) Next(ctx context.Context) (ProviderStep, error) {
	if err := ctx.Err(); err != nil {
		return ProviderStep{}, err
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if provider.served >= len(provider.steps) {
		return ProviderStep{}, fmt.Errorf(
			"step provider exhausted after %d steps: the code under test asked for more than the fixture describes",
			len(provider.steps))
	}
	step := provider.steps[provider.served]
	provider.served++
	provider.observed = append(provider.observed, step.Kind)
	if step.Kind == StepDelay {
		// Advance the fixture clock rather than sleeping. The delay is real to
		// anything reading the clock and free to the suite.
		provider.elapsed += step.Delay
	}
	return step, step.Err()
}

// Elapsed reports the fixture time consumed by scripted delays.
func (provider *StepProvider) Elapsed() time.Duration {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.elapsed
}

// Observed returns the step kinds actually served, in order. A test asserting
// coverage uses this rather than assuming the script ran.
func (provider *StepProvider) Observed() []StepKind {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	observed := make([]StepKind, len(provider.observed))
	copy(observed, provider.observed)
	return observed
}

// Remaining reports unconsumed steps.
func (provider *StepProvider) Remaining() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.steps) - provider.served
}

// UsageServed sums the usage of every step served, including failed ones.
func (provider *StepProvider) UsageServed() FixtureUsage {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var total FixtureUsage
	for _, step := range provider.steps[:provider.served] {
		total.UncachedInputTokens += step.Usage.UncachedInputTokens
		total.CacheReadTokens += step.Usage.CacheReadTokens
		total.CacheWriteTokens += step.Usage.CacheWriteTokens
		total.OutputTokens += step.Usage.OutputTokens
		total.ReasoningTokens += step.Usage.ReasoningTokens
	}
	return total
}

// FullCoverageScript returns one script exercising every declared step kind,
// so a harness can prove it handles the whole provider surface.
func FullCoverageScript() []ProviderStep {
	return []ProviderStep{
		{Kind: StepText, Text: "I will inspect the failing test first.",
			Usage: FixtureUsage{UncachedInputTokens: 1200, OutputTokens: 90}},
		{Kind: StepToolCall, ToolName: "read-file",
			ToolArgumentsJSON: `{"path":"internal/server/health.go"}`,
			Usage:             FixtureUsage{UncachedInputTokens: 300, OutputTokens: 40}},
		{Kind: StepUsageOnly,
			Usage: FixtureUsage{CacheReadTokens: 800, OutputTokens: 10}},
		{Kind: StepDelay, Delay: 2 * time.Second,
			Usage: FixtureUsage{UncachedInputTokens: 50}},
		{Kind: StepRateLimit, RetryAfter: 30 * time.Second},
		{Kind: StepPartialStream, Text: "Applying the fix to",
			Usage: FixtureUsage{UncachedInputTokens: 400, OutputTokens: 25}},
		{Kind: StepAuthFailure},
		{Kind: StepCancellation},
	}
}
