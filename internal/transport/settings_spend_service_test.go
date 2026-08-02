package transport

import (
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestSpendSummaryRendersSubMinorUnitCostWithoutRoundingItToZero is the
// load-bearing check for the money conversion.
//
// The whole surface exists because a single model call costs a fraction of one
// minor unit. If the wire carried whole minor units, every individual call
// would render as free and the summary would be worse than no summary at all.
func TestSpendSummaryRendersSubMinorUnitCostWithoutRoundingItToZero(t *testing.T) {
	// Two thirds of one minor unit: exactly the figure the ledger stores for a
	// small call, and one that no whole-minor-unit field can express.
	view := spendCostToProto(SpendSliceRecord{
		CostKnown: true, CostCurrency: "USD",
		CostNumerator: 2, CostDenominator: 3,
	})
	if view.GetAmount() == nil {
		t.Fatal("a known cost produced no amount")
	}
	if got := view.GetAmount().GetMinorUnits(); got != 666667 {
		t.Errorf("minor units = %d, want 666667 micro-minor-units", got)
	}
	if got := view.GetAmount().GetDecimalPlaces(); got != spendCostScale {
		t.Errorf("decimal places = %d, want %d", got, spendCostScale)
	}
	// Two thirds does not divide evenly at this scale, so the value must not
	// claim to be exact.
	if view.GetExact() {
		t.Error("a rounded cost reported itself as exact")
	}

	// A value that does divide evenly reports exact, so a caller can tell the
	// two apart rather than assuming every figure is approximate.
	half := spendCostToProto(SpendSliceRecord{
		CostKnown: true, CostCurrency: "USD",
		CostNumerator: 1, CostDenominator: 2,
	})
	if !half.GetExact() || half.GetAmount().GetMinorUnits() != 500000 {
		t.Errorf("half a minor unit = %+v, want an exact 500000", half.GetAmount())
	}
}

// TestUnknownSpendCostIsAbsentRatherThanZero proves an unpriced slice carries
// no amount.
//
// Money's contract is that absence means unknown, and AGENTS.md forbids
// displaying an unknown cost as zero. A proto3 zero would be indistinguishable
// from a call that genuinely cost nothing, which is the reading that turns an
// unpriced run into a free one.
func TestUnknownSpendCostIsAbsentRatherThanZero(t *testing.T) {
	for name, record := range map[string]SpendSliceRecord{
		"unpriced":     {CostKnown: false},
		"no currency":  {CostKnown: true, CostNumerator: 5, CostDenominator: 1},
		"no denominat": {CostKnown: true, CostCurrency: "USD", CostNumerator: 5},
	} {
		if amount := spendCostToProto(record).GetAmount(); amount != nil {
			t.Errorf("%s produced amount %+v, want none", name, amount)
		}
	}
}

// TestSpendSummaryRequiresAnExplicitWindow proves neither bound defaults.
//
// A summary that silently covered all time would be read as the period the
// caller asked for, and a spend figure believed to describe today while
// describing every run ever made is worse than an error.
func TestSpendSummaryRequiresAnExplicitWindow(t *testing.T) {
	since := timestamppb.New(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	until := timestamppb.New(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))

	if _, err := spendSummaryQueryFromProto(
		&codefluxv1.GetSpendSummaryRequest{Until: until},
	); err == nil {
		t.Error("a request with no start was accepted")
	}
	if _, err := spendSummaryQueryFromProto(
		&codefluxv1.GetSpendSummaryRequest{Since: since},
	); err == nil {
		t.Error("a request with no end was accepted")
	}
	if _, err := spendSummaryQueryFromProto(
		&codefluxv1.GetSpendSummaryRequest{Since: until, Until: since},
	); err == nil {
		t.Error("a window ending before it starts was accepted")
	}
	query, err := spendSummaryQueryFromProto(
		&codefluxv1.GetSpendSummaryRequest{Since: since, Until: until},
	)
	if err != nil {
		t.Fatalf("a valid window was refused: %v", err)
	}
	if !query.Since.Equal(since.AsTime()) || !query.Until.Equal(until.AsTime()) {
		t.Errorf("query = %+v, want the requested bounds", query)
	}
}
