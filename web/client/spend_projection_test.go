package main

import (
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
)

// TestSpendMoneyRendersAtTheScaleItWasSentAt is the load-bearing check for the
// figure a person reads.
//
// The coordinator deliberately sends a scale finer than whole minor units,
// because a single model call costs a fraction of one. A renderer that assumed
// two decimal places would turn every one of those calls into zero and make
// the page report the run as free.
func TestSpendMoneyRendersAtTheScaleItWasSentAt(t *testing.T) {
	cases := map[string]struct {
		amount *codefluxv1.Money
		want   string
	}{
		"sub-minor-unit": {
			amount: &codefluxv1.Money{
				CurrencyCode: "USD", MinorUnits: 666667, DecimalPlaces: 6,
			},
			want: "USD 0.666667",
		},
		"round amount keeps two places": {
			amount: &codefluxv1.Money{
				CurrencyCode: "USD", MinorUnits: 500000, DecimalPlaces: 6,
			},
			want: "USD 0.50",
		},
		"whole minor units": {
			amount: &codefluxv1.Money{
				CurrencyCode: "USD", MinorUnits: 1234, DecimalPlaces: 0,
			},
			want: "USD 1234",
		},
		"many minor units at scale": {
			amount: &codefluxv1.Money{
				CurrencyCode: "EUR", MinorUnits: 12_400_000, DecimalPlaces: 6,
			},
			want: "EUR 12.40",
		},
	}
	for name, testCase := range cases {
		if got := formatSpendMoney(testCase.amount); got != testCase.want {
			t.Errorf("%s = %q, want %q", name, got, testCase.want)
		}
	}
}

// TestProjectedSpendKeepsUnpricedCallsOutOfTheAmount proves an unpriced slice
// produces no rendered cost.
//
// A proto3 zero and a genuine zero are the same bytes, so the projection must
// key off the amount's absence. Rendering "0.00" for a call nobody could price
// is the reading this whole surface exists to prevent.
func TestProjectedSpendKeepsUnpricedCallsOutOfTheAmount(t *testing.T) {
	panel := projectSpendSummary(&codefluxv1.GetSpendSummaryResponse{
		Total: &codefluxv1.SpendSliceView{
			Calls: 3, InputTokens: 1200, OutputTokens: 400,
			CostUnknownCalls: 3,
			// No KnownCost at all: nothing here could be priced.
		},
		ByPhase: []*codefluxv1.PhaseSpendView{
			{
				Phase: "atoms",
				Spend: &codefluxv1.SpendSliceView{
					Calls: 3, InputTokens: 1200, OutputTokens: 400,
				},
			},
		},
		StageAttributionApproximate: true,
	}, "the last 30 days")

	if !panel.Known {
		t.Fatal("an answered summary produced an unknown panel")
	}
	if panel.Total.CostKnown || panel.Total.Cost != "" {
		t.Errorf("total cost = %q known=%v, want no rendered amount",
			panel.Total.Cost, panel.Total.CostKnown)
	}
	if panel.UnpricedCalls != 3 {
		t.Errorf("unpriced calls = %d, want 3", panel.UnpricedCalls)
	}
	if len(panel.Phases) != 1 || panel.Phases[0].Label != "Atoms" {
		t.Fatalf("phases = %+v, want one labelled Atoms", panel.Phases)
	}
	if panel.Phases[0].CostKnown {
		t.Error("an unpriced phase reported a known cost")
	}
	// The detail still says how much work happened, because the tokens are
	// measured even when the money is not.
	if panel.Phases[0].Detail == "" {
		t.Error("an unpriced phase reported no usage at all")
	}
}

// TestProjectedSpendMarksRoundedAmountsAsApproximate proves the exact flag
// survives the projection.
func TestProjectedSpendMarksRoundedAmountsAsApproximate(t *testing.T) {
	panel := projectSpendSummary(&codefluxv1.GetSpendSummaryResponse{
		Total: &codefluxv1.SpendSliceView{
			Calls: 1, InputTokens: 10, OutputTokens: 2,
			KnownCost: &codefluxv1.SpendCost{
				Amount: &codefluxv1.Money{
					CurrencyCode: "USD", MinorUnits: 666667, DecimalPlaces: 6,
				},
				Exact: false,
			},
		},
		ByModel: []*codefluxv1.ModelSpendView{
			{
				Model: "gpt-5.6-sol", ModelVersion: "2026-07-09",
				Spend: &codefluxv1.SpendSliceView{
					Calls: 1, InputTokens: 10, OutputTokens: 2,
					KnownCost: &codefluxv1.SpendCost{
						Amount: &codefluxv1.Money{
							CurrencyCode: "USD", MinorUnits: 500000,
							DecimalPlaces: 6,
						},
						Exact: true,
					},
				},
			},
		},
	}, "today")

	if !panel.Total.Approximate {
		t.Error("a rounded total was not marked approximate")
	}
	if panel.Total.Cost != "USD 0.666667" {
		t.Errorf("total = %q, want USD 0.666667", panel.Total.Cost)
	}
	if len(panel.Models) != 1 {
		t.Fatalf("models = %+v, want one", panel.Models)
	}
	if panel.Models[0].Approximate {
		t.Error("an exact model cost was marked approximate")
	}
	if panel.Models[0].Label != "gpt-5.6-sol 2026-07-09" {
		t.Errorf("model label = %q, want the model and its version",
			panel.Models[0].Label)
	}
}

// TestAnAbsentSpendAnswerLeavesThePanelUnknown proves a failed read does not
// become a zero.
func TestAnAbsentSpendAnswerLeavesThePanelUnknown(t *testing.T) {
	panel := projectSpendSummary(nil, "the last 30 days")
	if panel.Known {
		t.Error("a missing answer produced a known panel")
	}
	if panel.Total.CostKnown || panel.Total.Cost != "" {
		t.Errorf("a missing answer produced cost %q", panel.Total.Cost)
	}
}
