package coordinator

import (
	"errors"
	"math/big"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

func exactMinorCostFromAmount(
	amount providers.ExactAmount,
) (storage.ExactMinorCost, error) {
	if !amount.Known {
		return storage.ExactMinorCost{}, errors.New("provider price is unknown")
	}
	currency, err := domain.ParseCurrencyCode(amount.Currency)
	if err != nil {
		return storage.ExactMinorCost{}, err
	}
	return normalizeExactMinorCost(storage.ExactMinorCost{
		Numerator: amount.Numerator, Denominator: amount.Denominator,
		Currency: currency,
	})
}

func multiplyExactMinorCost(
	value storage.ExactMinorCost,
	factor int,
) (storage.ExactMinorCost, error) {
	normalized, err := normalizeExactMinorCost(value)
	if err != nil {
		return storage.ExactMinorCost{}, err
	}
	if factor < 1 {
		return storage.ExactMinorCost{}, errors.New("cost multiplier must be positive")
	}
	numerator := new(big.Int).Mul(
		big.NewInt(normalized.Numerator),
		big.NewInt(int64(factor)),
	)
	return exactMinorCostFromBig(
		numerator,
		big.NewInt(normalized.Denominator),
		normalized.Currency,
	)
}

func addExactMinorCosts(
	left storage.ExactMinorCost,
	right storage.ExactMinorCost,
) (storage.ExactMinorCost, error) {
	left, err := normalizeExactMinorCost(left)
	if err != nil {
		return storage.ExactMinorCost{}, err
	}
	right, err = normalizeExactMinorCost(right)
	if err != nil {
		return storage.ExactMinorCost{}, err
	}
	if left.Currency != right.Currency {
		return storage.ExactMinorCost{}, errors.New("cost currencies do not match")
	}
	numerator := new(big.Int).Add(
		new(big.Int).Mul(
			big.NewInt(left.Numerator),
			big.NewInt(right.Denominator),
		),
		new(big.Int).Mul(
			big.NewInt(right.Numerator),
			big.NewInt(left.Denominator),
		),
	)
	denominator := new(big.Int).Mul(
		big.NewInt(left.Denominator),
		big.NewInt(right.Denominator),
	)
	return exactMinorCostFromBig(numerator, denominator, left.Currency)
}

func compareExactMinorCosts(
	left storage.ExactMinorCost,
	right storage.ExactMinorCost,
) (int, error) {
	left, err := normalizeExactMinorCost(left)
	if err != nil {
		return 0, err
	}
	right, err = normalizeExactMinorCost(right)
	if err != nil {
		return 0, err
	}
	if left.Currency != right.Currency {
		return 0, errors.New("cost currencies do not match")
	}
	leftScaled := new(big.Int).Mul(
		big.NewInt(left.Numerator),
		big.NewInt(right.Denominator),
	)
	rightScaled := new(big.Int).Mul(
		big.NewInt(right.Numerator),
		big.NewInt(left.Denominator),
	)
	return leftScaled.Cmp(rightScaled), nil
}

func normalizeExactMinorCost(
	value storage.ExactMinorCost,
) (storage.ExactMinorCost, error) {
	currency, err := domain.ParseCurrencyCode(string(value.Currency))
	if err != nil || currency != value.Currency {
		return storage.ExactMinorCost{}, err
	}
	if value.Numerator < 0 || value.Denominator < 1 {
		return storage.ExactMinorCost{},
			errors.New("exact cost must be non-negative with a positive denominator")
	}
	return exactMinorCostFromBig(
		big.NewInt(value.Numerator),
		big.NewInt(value.Denominator),
		value.Currency,
	)
}

func exactMinorCostFromBig(
	numerator *big.Int,
	denominator *big.Int,
	currency domain.CurrencyCode,
) (storage.ExactMinorCost, error) {
	if numerator == nil || denominator == nil ||
		numerator.Sign() < 0 || denominator.Sign() < 1 {
		return storage.ExactMinorCost{},
			errors.New("exact cost must be non-negative with a positive denominator")
	}
	common := new(big.Int).GCD(nil, nil, numerator, denominator)
	if common.Sign() == 0 {
		common.SetInt64(1)
	}
	reducedNumerator := new(big.Int).Quo(new(big.Int).Set(numerator), common)
	reducedDenominator := new(big.Int).Quo(new(big.Int).Set(denominator), common)
	if !reducedNumerator.IsInt64() || !reducedDenominator.IsInt64() {
		return storage.ExactMinorCost{}, errors.New("exact cost exceeds durable integer range")
	}
	return storage.ExactMinorCost{
		Numerator:   reducedNumerator.Int64(),
		Denominator: reducedDenominator.Int64(),
		Currency:    currency,
	}, nil
}
