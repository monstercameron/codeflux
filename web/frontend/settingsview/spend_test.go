package settingsview_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/settingsview"
)

func spentSheet() settingsview.Props {
	props := answeredSheet()
	props.Spend = settingsview.SpendPanel{
		Known:       true,
		WindowLabel: "the last 30 days",
		Total: settingsview.SpendLine{
			Label: "Total", Detail: "9 calls · 12.4k in · 3.1k out",
			Cost: "USD 0.041234", CostKnown: true,
		},
		Phases: []settingsview.SpendLine{
			{
				Label: "Atoms", Detail: "6 calls · 9.0k in · 2.0k out",
				Cost: "USD 0.030000", CostKnown: true, Approximate: true,
			},
			{
				Label: "Molecules", Detail: "2 calls · 2.4k in · 800 out",
				Cost: "USD 0.008000", CostKnown: true, Approximate: true,
			},
			{
				// A phase whose calls were never priced. It must not render as
				// a zero amount.
				Label: "Program", Detail: "1 call · 1.0k in · 300 out",
			},
		},
		Models: []settingsview.SpendLine{
			{
				Label: "gpt-5.6-sol", Detail: "9 calls · 12.4k in · 3.1k out",
				Cost: "USD 0.041234", CostKnown: true,
			},
		},
		StageAttributionApproximate: true,
		UnpricedCalls:               1,
	}
	return props
}

// TestSpendSectionShowsWhatEachPhaseCost is the load-bearing check for the
// readout a person actually reads.
func TestSpendSectionShowsWhatEachPhaseCost(t *testing.T) {
	markup := renderSettings(t, settingsview.Sheet(spentSheet()))
	for _, want := range []string{
		"SPEND",
		"Atoms",
		"Molecules",
		"Program",
		"USD 0.041234",
		"USD 0.030000",
		"gpt-5.6-sol",
		"the last 30 days",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("the spend readout does not mention %q", want)
		}
	}
}

// TestAnUnpricedPhaseSaysSoInsteadOfShowingZero is the check that keeps this
// surface honest.
//
// AGENTS.md forbids displaying an unknown cost as zero, and the reason is
// specific: a run whose calls could not be priced would otherwise read as a
// run that was free, which is the one conclusion the evidence does not
// support.
func TestAnUnpricedPhaseSaysSoInsteadOfShowingZero(t *testing.T) {
	markup := renderSettings(t, settingsview.Sheet(spentSheet()))
	if !strings.Contains(markup, "not priced") {
		t.Error("an unpriced phase did not say so")
	}
	for _, forbidden := range []string{"USD 0.00<", "USD 0.000000"} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("an unpriced phase rendered as %q", forbidden)
		}
	}
	// The estimate has to be labelled, or a reader takes an inferred number for
	// a measured one.
	if !strings.Contains(markup, "estimate") {
		t.Error("approximate phase costs were not labelled as estimates")
	}
	// And the calls excluded from every amount must be named, so the totals
	// read as a floor rather than as a total.
	if !strings.Contains(markup, "floor") {
		t.Error("the unpriced calls were not declared as excluded")
	}
}

// TestSpendSectionBeforeAnAnswerSaysNothingRatherThanZero proves the panel is
// empty, not zero, before the coordinator has answered.
func TestSpendSectionBeforeAnAnswerSaysNothingRatherThanZero(t *testing.T) {
	props := answeredSheet()
	props.Spend = settingsview.SpendPanel{}
	markup := renderSettings(t, settingsview.Sheet(props))
	if !strings.Contains(markup, "No spend has been recorded yet") {
		t.Error("an unanswered spend panel did not say it had no figure")
	}
	if strings.Contains(markup, "0.00") {
		t.Error("an unanswered spend panel rendered an amount")
	}
}

func TestFormatTokenCountStaysReadableBesideMoney(t *testing.T) {
	for tokens, want := range map[uint64]string{
		0:         "0",
		999:       "999",
		1000:      "1.0k",
		12_400:    "12.4k",
		1_500_000: "1.50M",
	} {
		if got := settingsview.FormatTokenCount(tokens); got != want {
			t.Errorf("FormatTokenCount(%d) = %q, want %q", tokens, got, want)
		}
	}
}
