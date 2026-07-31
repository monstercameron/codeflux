package agent

import (
	"errors"
	"math"
	"math/big"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

type costAccumulator struct {
	currency string
	value    *big.Rat
	known    bool
}

const absoluteMaximumCostUnits int64 = 1_000_000

func newCostAccumulator(limit providers.ExactAmount) (costAccumulator, error) {
	if !limit.Known || len(limit.Currency) != 3 ||
		limit.Currency != strings.ToUpper(limit.Currency) ||
		limit.Numerator < 0 || limit.Denominator < 1 {
		return costAccumulator{}, errors.New(
			"agent maximum cost must be an exact known non-negative amount",
		)
	}
	return costAccumulator{
		currency: limit.Currency,
		value:    new(big.Rat),
		known:    true,
	}, nil
}

func costWithinAbsoluteMaximum(value providers.ExactAmount) bool {
	if !value.Known || value.Numerator < 0 || value.Denominator < 1 {
		return false
	}
	amount := new(big.Rat).SetFrac(
		big.NewInt(value.Numerator),
		big.NewInt(value.Denominator),
	)
	maximum := new(big.Rat).SetInt64(absoluteMaximumCostUnits)
	return amount.Cmp(maximum) <= 0
}

func (accumulator *costAccumulator) add(
	value providers.ExactAmount,
) error {
	if accumulator == nil || !accumulator.known {
		return errors.New("agent cost accumulator is unavailable")
	}
	if !value.Known {
		return errors.New("agent model cost accounting is unknown")
	}
	if value.Currency != accumulator.currency ||
		value.Numerator < 0 || value.Denominator < 1 {
		return errors.New("agent model cost is invalid")
	}
	accumulator.value.Add(
		accumulator.value,
		new(big.Rat).SetFrac(
			big.NewInt(value.Numerator),
			big.NewInt(value.Denominator),
		),
	)
	return nil
}

func (accumulator costAccumulator) exceeds(
	limit providers.ExactAmount,
) bool {
	if !accumulator.known || accumulator.value == nil {
		return true
	}
	maximum := new(big.Rat).SetFrac(
		big.NewInt(limit.Numerator),
		big.NewInt(limit.Denominator),
	)
	return accumulator.value.Cmp(maximum) > 0
}

func (accumulator costAccumulator) amount() providers.ExactAmount {
	if !accumulator.known || accumulator.value == nil ||
		!accumulator.value.Num().IsInt64() ||
		!accumulator.value.Denom().IsInt64() {
		return providers.UnknownAmount(accumulator.currency)
	}
	value, err := providers.NewExactAmount(
		accumulator.currency,
		accumulator.value.Num().Int64(),
		accumulator.value.Denom().Int64(),
	)
	if err != nil {
		return providers.UnknownAmount(accumulator.currency)
	}
	return value
}

func usageTokenCount(usage providers.Usage) (domain.TokenCount, error) {
	if !usage.Known {
		return 0, errors.New("agent model token accounting is unknown")
	}
	specific, err := providers.NormalizeProviderSpecificUsage(
		usage.ProviderSpecific,
	)
	if err != nil {
		return 0, err
	}
	counts := []int64{
		usage.InputTokens,
		usage.CachedInputTokens,
		usage.CacheWriteTokens,
		usage.OutputTokens,
		usage.ReasoningTokens,
	}
	for _, count := range specific {
		counts = append(counts, count)
	}
	var total uint64
	for _, count := range counts {
		if count < 0 || uint64(count) > math.MaxUint64-total {
			return 0, errors.New("agent model token accounting overflows")
		}
		total += uint64(count)
	}
	return domain.TokenCount(total), nil
}

func addTokens(
	total domain.TokenCount,
	addition domain.TokenCount,
) (domain.TokenCount, error) {
	if uint64(addition) > math.MaxUint64-uint64(total) {
		return 0, errors.New("agent cumulative token accounting overflows")
	}
	return total + addition, nil
}
