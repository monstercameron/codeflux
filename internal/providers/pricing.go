package providers

import (
	"errors"
	"math/big"
	"strings"
	"time"
)

// ExactAmount is an exact rational quantity in one ISO-4217 currency.
type ExactAmount struct {
	Currency    string
	Numerator   int64
	Denominator int64
	Known       bool
}

func NewExactAmount(currency string, numerator, denominator int64) (ExactAmount, error) {
	if currency != strings.ToUpper(currency) || len(currency) != 3 {
		return ExactAmount{}, errors.New("currency must be a three-letter uppercase code")
	}
	if numerator < 0 || denominator < 1 {
		return ExactAmount{}, errors.New("exact amount must be non-negative with a positive denominator")
	}
	common := new(big.Int).GCD(
		nil, nil, big.NewInt(numerator), big.NewInt(denominator),
	)
	divisor := common.Int64()
	if divisor == 0 {
		divisor = 1
	}
	return ExactAmount{
		Currency: currency, Numerator: numerator / divisor,
		Denominator: denominator / divisor, Known: true,
	}, nil
}

func UnknownAmount(currency string) ExactAmount {
	return ExactAmount{Currency: currency}
}

// TokenPrice is the exact price for one million tokens of each category.
type TokenPrice struct {
	Input            ExactAmount
	CachedInput      ExactAmount
	CacheWrite       ExactAmount
	Output           ExactAmount
	Reasoning        ExactAmount
	ProviderSpecific map[string]ExactAmount
}

// PriceSnapshot is immutable evidence, distinct from mutable current pricing.
type PriceSnapshot struct {
	ID          string
	Model       ModelIdentity
	Price       TokenPrice
	EffectiveAt time.Time
	CapturedAt  time.Time
	Source      string
}

// Cost computes exact total cost without floating point. Unknown required
// prices produce an explicitly unknown amount.
func (snapshot PriceSnapshot) Cost(usage Usage) ExactAmount {
	if !usage.Known {
		return UnknownAmount(snapshot.Price.Input.Currency)
	}
	parts := []struct {
		tokens int64
		price  ExactAmount
	}{
		{usage.InputTokens, snapshot.Price.Input},
		{usage.CachedInputTokens, snapshot.Price.CachedInput},
		{usage.CacheWriteTokens, snapshot.Price.CacheWrite},
		{usage.OutputTokens, snapshot.Price.Output},
		{usage.ReasoningTokens, snapshot.Price.Reasoning},
	}
	providerSpecific, err := NormalizeProviderSpecificUsage(
		usage.ProviderSpecific,
	)
	if err != nil {
		return UnknownAmount(snapshot.Price.Input.Currency)
	}
	for category, tokens := range providerSpecific {
		price, found := snapshot.Price.ProviderSpecific[category]
		if !found {
			return UnknownAmount(snapshot.Price.Input.Currency)
		}
		parts = append(parts, struct {
			tokens int64
			price  ExactAmount
		}{tokens: tokens, price: price})
	}
	var currency string
	total := new(big.Rat)
	for _, part := range parts {
		if part.tokens == 0 {
			continue
		}
		if !part.price.Known {
			return UnknownAmount(currency)
		}
		if currency == "" {
			currency = part.price.Currency
		}
		if part.price.Currency != currency {
			return UnknownAmount(currency)
		}
		value := new(big.Rat).SetFrac(
			new(big.Int).Mul(big.NewInt(part.tokens), big.NewInt(part.price.Numerator)),
			new(big.Int).Mul(big.NewInt(1_000_000), big.NewInt(part.price.Denominator)),
		)
		total.Add(total, value)
	}
	if currency == "" {
		currency = snapshot.Price.Input.Currency
	}
	if !total.Num().IsInt64() || !total.Denom().IsInt64() {
		return UnknownAmount(currency)
	}
	amount, err := NewExactAmount(currency, total.Num().Int64(), total.Denom().Int64())
	if err != nil {
		return UnknownAmount(currency)
	}
	return amount
}
