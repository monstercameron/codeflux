package forecast

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

// ActualResult is the completed-run evidence compared with a forecast.
type ActualResult struct {
	LatencyMillis      domain.Milliseconds   `json:"latency_ms"`
	Usage              providers.Usage       `json:"usage"`
	Cost               providers.ExactAmount `json:"cost"`
	ToolCalls          uint32                `json:"tool_calls"`
	RepairRounds       uint32                `json:"repair_rounds"`
	HumanInterventions uint32                `json:"human_interventions"`
	Accepted           bool                  `json:"accepted"`
}

// Coverage records whether the actual result fell below each percentile.
type Coverage struct {
	Known     bool `json:"known"`
	WithinP50 bool `json:"within_p50"`
	WithinP90 bool `json:"within_p90"`
}

// Comparison is transparent forecast-versus-actual completion evidence.
type Comparison struct {
	Latency   Coverage     `json:"latency"`
	Tokens    Coverage     `json:"tokens"`
	Cost      Coverage     `json:"cost"`
	ToolCalls Coverage     `json:"tool_calls"`
	Actual    ActualResult `json:"actual"`
}

// Compare evaluates interval coverage without treating the forecast as a gate.
func Compare(value Forecast, actual ActualResult) (Comparison, error) {
	if value.AlgorithmVersion != AlgorithmVersion ||
		value.EstimateNotice != EstimateNotice ||
		!value.AdvisoryOnly {
		return Comparison{}, fmt.Errorf("%w: malformed forecast", ErrInvalidForecastInput)
	}
	if actual.LatencyMillis < 0 {
		return Comparison{}, fmt.Errorf("%w: actual latency is negative", ErrInvalidForecastInput)
	}
	if err := providers.ValidateUsage(actual.Usage); err != nil {
		return Comparison{}, fmt.Errorf("%w: actual usage: %v", ErrInvalidForecastInput, err)
	}
	total, totalKnown, err := totalUsage(actual.Usage)
	if err != nil {
		return Comparison{}, err
	}
	if actual.Cost.Known {
		if err := validateExactAmount(actual.Cost); err != nil {
			return Comparison{}, err
		}
	} else if actual.Cost.Numerator != 0 || actual.Cost.Denominator != 0 {
		return Comparison{}, fmt.Errorf("%w: unknown actual cost carries a numeric value", ErrInvalidForecastInput)
	}
	comparison := Comparison{
		Latency: Coverage{
			Known:     true,
			WithinP50: actual.LatencyMillis <= value.Latency.P50Millis,
			WithinP90: actual.LatencyMillis <= value.Latency.P90Millis,
		},
		ToolCalls: Coverage{
			Known:     true,
			WithinP50: actual.ToolCalls <= value.ToolCalls.P50,
			WithinP90: actual.ToolCalls <= value.ToolCalls.P90,
		},
		Actual: actual,
	}
	if totalKnown {
		comparison.Tokens = Coverage{
			Known:     true,
			WithinP50: total <= value.Tokens.P50,
			WithinP90: total <= value.Tokens.P90,
		}
	}
	if value.Cost.Known && actual.Cost.Known {
		if actual.Cost.Currency != value.Cost.P50.Currency ||
			actual.Cost.Currency != value.Cost.P90.Currency {
			return Comparison{}, fmt.Errorf("%w: actual and forecast currencies differ", ErrInvalidForecastInput)
		}
		actualRat := new(big.Rat).SetFrac(
			big.NewInt(actual.Cost.Numerator),
			big.NewInt(actual.Cost.Denominator),
		)
		p50Rat := new(big.Rat).SetFrac(
			big.NewInt(value.Cost.P50.Numerator),
			big.NewInt(value.Cost.P50.Denominator),
		)
		p90Rat := new(big.Rat).SetFrac(
			big.NewInt(value.Cost.P90.Numerator),
			big.NewInt(value.Cost.P90.Denominator),
		)
		comparison.Cost = Coverage{
			Known:     true,
			WithinP50: actualRat.Cmp(p50Rat) <= 0,
			WithinP90: actualRat.Cmp(p90Rat) <= 0,
		}
	}
	return comparison, nil
}

func totalUsage(usage providers.Usage) (domain.TokenCount, bool, error) {
	if !usage.Known {
		return 0, false, nil
	}
	specific, err := providers.NormalizeProviderSpecificUsage(usage.ProviderSpecific)
	if err != nil {
		return 0, false, fmt.Errorf("%w: actual provider-specific usage: %v", ErrInvalidForecastInput, err)
	}
	var total int64
	for _, count := range []int64{
		usage.InputTokens,
		usage.CachedInputTokens,
		usage.CacheWriteTokens,
		usage.OutputTokens,
		usage.ReasoningTokens,
	} {
		if count > math.MaxInt64-total {
			return 0, false, fmt.Errorf("%w: actual token total overflows", ErrInvalidForecastInput)
		}
		total += count
	}
	for _, count := range specific {
		if count > math.MaxInt64-total {
			return 0, false, fmt.Errorf("%w: actual token total overflows", ErrInvalidForecastInput)
		}
		total += count
	}
	return domain.TokenCount(total), true, nil
}

func validateExactAmount(amount providers.ExactAmount) error {
	if amount.Currency != strings.ToUpper(amount.Currency) ||
		len(amount.Currency) != 3 ||
		amount.Numerator < 0 ||
		amount.Denominator < 1 {
		return fmt.Errorf("%w: actual cost is malformed", ErrInvalidForecastInput)
	}
	return nil
}
