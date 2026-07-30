package coordinator

import (
	"math"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

func TestProviderBudgetMathKeepsExactRationalRetryBound(t *testing.T) {
	amount, err := providers.NewExactAmount("USD", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	cost, err := exactMinorCostFromAmount(amount)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := multiplyExactMinorCost(cost, 3)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Numerator != 1 || bound.Denominator != 1 ||
		bound.Currency != domain.CurrencyCode("USD") {
		t.Fatalf("retry bound = %#v", bound)
	}
	total, err := addExactMinorCosts(
		storage.ExactMinorCost{
			Numerator: 1, Denominator: 3, Currency: domain.CurrencyCode("USD"),
		},
		storage.ExactMinorCost{
			Numerator: 1, Denominator: 6, Currency: domain.CurrencyCode("USD"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if total.Numerator != 1 || total.Denominator != 2 {
		t.Fatalf("exact total = %#v", total)
	}
	comparison, err := compareExactMinorCosts(total, bound)
	if err != nil {
		t.Fatal(err)
	}
	if comparison >= 0 {
		t.Fatalf("comparison = %d, want total below retry bound", comparison)
	}
}

func TestProviderBudgetMathRejectsUnknownMismatchAndOverflow(t *testing.T) {
	if _, err := exactMinorCostFromAmount(
		providers.UnknownAmount("USD"),
	); err == nil {
		t.Fatal("unknown price unexpectedly authorized")
	}
	_, err := addExactMinorCosts(
		storage.ExactMinorCost{
			Numerator: 1, Denominator: 1, Currency: domain.CurrencyCode("USD"),
		},
		storage.ExactMinorCost{
			Numerator: 1, Denominator: 1, Currency: domain.CurrencyCode("EUR"),
		},
	)
	if err == nil {
		t.Fatal("currency mismatch unexpectedly accepted")
	}
	_, err = multiplyExactMinorCost(
		storage.ExactMinorCost{
			Numerator: math.MaxInt64, Denominator: 1,
			Currency: domain.CurrencyCode("USD"),
		},
		2,
	)
	if err == nil {
		t.Fatal("overflow unexpectedly accepted")
	}
}
