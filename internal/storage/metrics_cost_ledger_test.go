package storage

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// TestM22_094_CostMetricsMeasureRecordedProviderSpend proves the cost query
// reads the ledger production actually writes.
//
// The empty-database test in metrics_repository_test.go cannot distinguish a
// query that measures nothing from a query that measures the wrong table,
// which is how CostMetrics came to aggregate usage_records: a table with no
// writer anywhere in the repository. Every window returned a confident zero
// while real spend accumulated in provider_attempt_accounting.
//
// The fixture also pins the two properties that motivated the change.
// Accounting rows are append-only and an attempt carries one row per evidence
// improvement, so the settled row must supersede the estimate before it. And a
// call costing two-thirds of one minor unit is real spend, so the exact
// subtotal must survive even though its whole-minor-unit view is zero.
func TestM22_094_CostMetricsMeasureRecordedProviderSpend(t *testing.T) {
	repositories, task := createTaskFixture(t, 9400)
	providerID, configuration, pricing := seedProviderRequestDependencies(
		t, repositories, 9401,
	)
	request := planAndStartProviderRequest(
		t, repositories, task.ID, providerID, configuration, pricing, 4,
	)
	usd := mustCurrencyCode(t, "USD")

	// The priced attempt records an estimate first and the provider's own
	// report second. Only the second describes what was bought.
	priced := settleProviderAttemptForMetrics(
		t, repositories, request.ID, "metrics-priced-attempt", 1,
		time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
	)
	appendMetricsAccounting(t, repositories, AppendProviderAttemptAccounting{
		ID: "metrics-accounting-estimate", AttemptID: priced.ID,
		Sequence: 1, Source: "estimated",
		Usage:             domain.TokenUsage{Known: true, Input: 1, Output: 3},
		PricingRevisionID: &pricing.ID,
		Cost: &ExactMinorCost{
			Numerator: 1, Denominator: 3, Currency: usd,
		},
		ProvenanceJSON: `{"source":"estimated"}`,
	})
	appendMetricsAccounting(t, repositories, AppendProviderAttemptAccounting{
		ID: "metrics-accounting-final", AttemptID: priced.ID,
		Sequence: 2, Source: "provider-final",
		Usage:             domain.TokenUsage{Known: true, Input: 2, Output: 5},
		PricingRevisionID: &pricing.ID,
		Cost: &ExactMinorCost{
			Numerator: 2, Denominator: 3, Currency: usd,
		},
		ProvenanceJSON: `{"source":"provider-final"}`,
	})

	// The unpriced attempt is the one a total must never absorb as zero.
	unpriced := settleProviderAttemptForMetrics(
		t, repositories, request.ID, "metrics-unpriced-attempt", 2,
		time.Date(2026, 7, 31, 9, 5, 0, 0, time.UTC),
	)
	appendMetricsAccounting(t, repositories, AppendProviderAttemptAccounting{
		ID: "metrics-accounting-unknown", AttemptID: unpriced.ID,
		Sequence: 1, Source: "provider-final",
		Usage:          domain.TokenUsage{},
		ProvenanceJSON: `{"source":"provider-final","usage":"absent"}`,
	})

	result, err := repositories.CostMetrics(t.Context(), metricsWindowFixture())
	if err != nil {
		t.Fatalf("cost metrics: %v", err)
	}

	// 2, not 3: an estimate superseded by the provider's own report is not
	// extra spend, and adding both rows reports tokens nobody bought.
	if result.InputTokens != knownCount(2) {
		t.Errorf("input tokens = %+v, want 2 from the settled row alone",
			result.InputTokens)
	}
	if result.OutputTokens != knownCount(5) {
		t.Errorf("output tokens = %+v, want 5 from the settled row alone",
			result.OutputTokens)
	}
	if result.ProviderAttempts != knownCount(2) {
		t.Errorf("provider attempts = %+v, want 2", result.ProviderAttempts)
	}
	if result.UsageUnknownCount != knownCount(1) {
		t.Errorf("usage unknown count = %+v, want 1", result.UsageUnknownCount)
	}
	if result.CostUnknownCount != knownCount(1) {
		t.Errorf("cost unknown count = %+v, want 1", result.CostUnknownCount)
	}

	wantCost := ExactMinorCost{Numerator: 2, Denominator: 3, Currency: usd}
	if result.KnownCost != wantCost {
		t.Errorf("known cost = %+v, want %+v", result.KnownCost, wantCost)
	}
	if result.Currency != "USD" {
		t.Errorf("currency = %q, want USD", result.Currency)
	}
	// Two-thirds of a minor unit truncates to zero, which is exactly why the
	// exact figure is carried separately and is the one a person is shown.
	if result.CostMinorUnits != knownCount(0) {
		t.Errorf("cost minor units = %+v, want the truncated 0",
			result.CostMinorUnits)
	}
}

// TestM22_094_MixedCurrencySpendIsUnknownRatherThanSummed proves a window whose
// priced calls disagree on currency reports no total.
//
// Minor units of different currencies are not the same unit. Adding them
// yields a number that looks authoritative and means nothing, which is worse
// for a budget decision than reporting that the window cannot be totalled.
func TestM22_094_MixedCurrencySpendIsUnknownRatherThanSummed(t *testing.T) {
	repositories, task := createTaskFixture(t, 9500)

	for index, code := range []string{"USD", "EUR"} {
		currency := mustCurrencyCode(t, code)
		providerID := seedProviderAccountingProvider(t, repositories, 9501+index)
		configuration := seedProviderConfiguration(
			t, repositories, providerID, 9501+index,
		)
		pricing, err := repositories.CreateProviderPricingRevision(
			t.Context(),
			CreateProviderPricingRevision{
				ID:         "mixed-currency-pricing-" + code,
				ProviderID: providerID, ModelIdentifier: "fixture-model",
				ModelVersion: "fixture-model-v1", PricingKnown: true,
				Currency:    &currency,
				EffectiveAt: time.Date(2026, 7, 31, 10, index, 0, 0, time.UTC),
				Components: []ProviderPriceComponent{
					{UsageKind: "input", MinorNumerator: 1, TokenDenominator: 3},
					{UsageKind: "output", MinorNumerator: 0, TokenDenominator: 1},
				},
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		request := planAndStartProviderRequest(
			t, repositories, task.ID, providerID, configuration, pricing, index,
		)
		attempt := settleProviderAttemptForMetrics(
			t, repositories, request.ID, "mixed-currency-attempt-"+code, 1,
			time.Date(2026, 7, 31, 10, 30+index, 0, 0, time.UTC),
		)
		appendMetricsAccounting(t, repositories, AppendProviderAttemptAccounting{
			ID:        "mixed-currency-accounting-" + code,
			AttemptID: attempt.ID, Sequence: 1, Source: "provider-final",
			Usage:             domain.TokenUsage{Known: true, Input: 3, Output: 1},
			PricingRevisionID: &pricing.ID,
			Cost: &ExactMinorCost{
				Numerator: 1, Denominator: 1, Currency: currency,
			},
			ProvenanceJSON: `{"source":"provider-final"}`,
		})
	}

	result, err := repositories.CostMetrics(t.Context(), metricsWindowFixture())
	if err != nil {
		t.Fatalf("cost metrics: %v", err)
	}
	if result.CostMinorUnits.Known {
		t.Errorf("cost minor units = %+v, want unknown across two currencies",
			result.CostMinorUnits)
	}
	if result.Currency != "" {
		t.Errorf("currency = %q, want none across two currencies",
			result.Currency)
	}
	if result.KnownCost != (ExactMinorCost{}) {
		t.Errorf("known cost = %+v, want none across two currencies",
			result.KnownCost)
	}
	// The tokens remain countable; only the money does not add up.
	if result.InputTokens != knownCount(6) {
		t.Errorf("input tokens = %+v, want 6", result.InputTokens)
	}
}

func settleProviderAttemptForMetrics(
	t *testing.T,
	repositories *Repositories,
	requestID domain.ModelRequestID,
	attemptID string,
	attemptNumber uint64,
	observedAt time.Time,
) ProviderRequestAttempt {
	t.Helper()
	attempt, err := repositories.CreateProviderRequestAttempt(
		t.Context(),
		CreateProviderRequestAttempt{
			ID: attemptID, LogicalRequestID: requestID,
			AttemptNumber: attemptNumber,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	attempt = transitionProviderAttemptForTest(
		t, repositories, attempt, ProviderRequestAttemptStarted,
		ProviderRequestEffectNone, false, observedAt,
	)
	settled, err := repositories.TransitionProviderRequestAttempt(
		t.Context(),
		TransitionProviderRequestAttempt{
			ID: attempt.ID, ExpectedRevision: attempt.Revision,
			From:         ProviderRequestAttemptStarted,
			To:           ProviderRequestAttemptSucceeded,
			EffectStatus: ProviderRequestEffectNone,
			ObservedAt:   observedAt.Add(time.Second),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return settled
}

func appendMetricsAccounting(
	t *testing.T,
	repositories *Repositories,
	input AppendProviderAttemptAccounting,
) {
	t.Helper()
	if _, err := repositories.AppendProviderAttemptAccounting(
		t.Context(), input,
	); err != nil {
		t.Fatalf("append accounting %s: %v", input.ID, err)
	}
}
