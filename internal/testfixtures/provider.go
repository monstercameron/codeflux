package testfixtures

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// FixtureCredentialMaterial is the ONLY credential-shaped value in this
// package (M22-014). It is structurally plausible and functionally dead: it
// matches no provider, and internal/redact will redact it wherever it
// appears. Tests needing "a credential" must use this rather than inventing
// one that might resemble something real.
const FixtureCredentialMaterial = "codeflux-fixture-not-a-real-credential-0000"

// ScriptedTurn is one scripted model exchange (M22-004, M22-005).
//
// A turn is either text, a tool call, or a failure. Making that a closed set
// rather than free-form means a fixture cannot describe an exchange the real
// provider port could not produce.
type ScriptedTurn struct {
	// Text is the assistant's textual output for this turn.
	Text string
	// ToolName and ToolArgumentsJSON, when set, script a tool call
	// (M22-005) instead of text.
	ToolName          string
	ToolArgumentsJSON string
	// Err, when set, makes this turn fail. Scripting failure is as
	// important as scripting success: most correctness bugs live in the
	// failure path.
	Err error
	// Usage is the fake token accounting for this turn (M22-006).
	Usage FixtureUsage
}

// Validate rejects a turn that is not exactly one of the three kinds.
func (turn ScriptedTurn) Validate() error {
	kinds := 0
	if turn.Text != "" {
		kinds++
	}
	if turn.ToolName != "" {
		kinds++
	}
	if turn.Err != nil {
		kinds++
	}
	if kinds != 1 {
		return errors.New("a scripted turn must be exactly one of: text, tool call, or failure")
	}
	if turn.ToolName != "" && turn.ToolArgumentsJSON == "" {
		return errors.New("a scripted tool call must carry its arguments")
	}
	return nil
}

// FixtureUsage is deterministic token accounting (M22-006).
//
// Every field is explicit. There is no "estimated" or "unknown" usage here:
// a fixture that guessed usage would make cost assertions meaningless, and
// docs/plan.md requires unknown usage stay unknown rather than become zero.
type FixtureUsage struct {
	UncachedInputTokens uint64
	CacheReadTokens     uint64
	CacheWriteTokens    uint64
	OutputTokens        uint64
	ReasoningTokens     uint64
}

// Total returns every token counted, so a test can assert a budget without
// re-deriving the sum and getting it subtly wrong.
func (usage FixtureUsage) Total() uint64 {
	return usage.UncachedInputTokens + usage.CacheReadTokens +
		usage.CacheWriteTokens + usage.OutputTokens + usage.ReasoningTokens
}

// FixturePricing is deterministic pricing in exact minor units per million
// tokens (M22-006). Integer minor units, never floats: docs/plan.md forbids
// binary floating point for currency, and a fixture that used it would make
// every cost assertion approximate.
type FixturePricing struct {
	CurrencyCode                 string
	UncachedInputMinorPerMillion int64
	CacheReadMinorPerMillion     int64
	CacheWriteMinorPerMillion    int64
	OutputMinorPerMillion        int64
}

// DefaultFixturePricing is a round, obviously-synthetic price table. The
// values are deliberately not any real provider's, so a test asserting a
// cost cannot accidentally encode a belief about real pricing.
func DefaultFixturePricing() FixturePricing {
	return FixturePricing{
		CurrencyCode:                 "USD",
		UncachedInputMinorPerMillion: 100,
		CacheReadMinorPerMillion:     10,
		CacheWriteMinorPerMillion:    125,
		OutputMinorPerMillion:        600,
	}
}

// CostMinorUnits computes exact cost in minor units using integer
// arithmetic, rounding half away from zero at the final division only.
func (pricing FixturePricing) CostMinorUnits(usage FixtureUsage) int64 {
	const perMillion = 1_000_000
	numerator := int64(usage.UncachedInputTokens)*pricing.UncachedInputMinorPerMillion +
		int64(usage.CacheReadTokens)*pricing.CacheReadMinorPerMillion +
		int64(usage.CacheWriteTokens)*pricing.CacheWriteMinorPerMillion +
		int64(usage.OutputTokens+usage.ReasoningTokens)*pricing.OutputMinorPerMillion
	if numerator == 0 {
		return 0
	}
	return (numerator + perMillion/2) / perMillion
}

// ScriptedProvider is the deterministic fake model provider (M22-004).
//
// It replays a fixed script and refuses to improvise: asking for more turns
// than were scripted is an error, not a silent empty response. A fake that
// invented a reply would let a test pass for a reason the real system could
// never reproduce.
type ScriptedProvider struct {
	mutex   sync.Mutex
	turns   []ScriptedTurn
	served  int
	pricing FixturePricing
}

// NewScriptedProvider validates and freezes a script.
func NewScriptedProvider(pricing FixturePricing, turns ...ScriptedTurn) (*ScriptedProvider, error) {
	if len(turns) == 0 {
		return nil, errors.New("a scripted provider requires at least one turn")
	}
	for index, turn := range turns {
		if err := turn.Validate(); err != nil {
			return nil, fmt.Errorf("turn %d: %w", index, err)
		}
	}
	if pricing.CurrencyCode == "" {
		pricing = DefaultFixturePricing()
	}
	frozen := make([]ScriptedTurn, len(turns))
	copy(frozen, turns)
	return &ScriptedProvider{turns: frozen, pricing: pricing}, nil
}

// Next returns the next scripted turn, or an error once the script is spent.
func (provider *ScriptedProvider) Next(ctx context.Context) (ScriptedTurn, error) {
	if err := ctx.Err(); err != nil {
		return ScriptedTurn{}, err
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if provider.served >= len(provider.turns) {
		return ScriptedTurn{}, fmt.Errorf(
			"scripted provider exhausted after %d turns: the code under test asked for more model output than the fixture describes",
			len(provider.turns),
		)
	}
	turn := provider.turns[provider.served]
	provider.served++
	if turn.Err != nil {
		return turn, turn.Err
	}
	return turn, nil
}

// Served reports how many turns were consumed, so a test can assert the code
// under test made exactly the calls it should have.
func (provider *ScriptedProvider) Served() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.served
}

// Remaining reports unconsumed turns. A test that scripted a failure and
// never reached it should fail loudly rather than pass quietly.
func (provider *ScriptedProvider) Remaining() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.turns) - provider.served
}

// TotalUsage sums the usage of every turn actually served.
func (provider *ScriptedProvider) TotalUsage() FixtureUsage {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var total FixtureUsage
	for _, turn := range provider.turns[:provider.served] {
		total.UncachedInputTokens += turn.Usage.UncachedInputTokens
		total.CacheReadTokens += turn.Usage.CacheReadTokens
		total.CacheWriteTokens += turn.Usage.CacheWriteTokens
		total.OutputTokens += turn.Usage.OutputTokens
		total.ReasoningTokens += turn.Usage.ReasoningTokens
	}
	return total
}

// TotalCostMinorUnits returns the exact cost of what was served.
func (provider *ScriptedProvider) TotalCostMinorUnits() int64 {
	return provider.pricing.CostMinorUnits(provider.TotalUsage())
}
