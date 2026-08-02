package coordinator

import (
	"context"

	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// ReadSpendSummary reports what one window of recorded work cost and where the
// money went.
//
// The window is passed through rather than widened or defaulted. A summary that
// silently covered more than it was asked for would be read as the period the
// caller requested, which is how a figure comes to be trusted for a decision it
// does not describe.
func (application settingsApplication) ReadSpendSummary(
	ctx context.Context,
	query transport.SpendSummaryQuery,
) (transport.SpendSummaryRecord, error) {
	attribution, err := application.repositories.AttributeSpend(
		ctx,
		storage.MetricsWindow{From: query.Since, To: query.Until},
	)
	if err != nil {
		return transport.SpendSummaryRecord{}, err
	}
	record := transport.SpendSummaryRecord{
		Total:        spendSliceRecord(attribution.Total),
		Unattributed: spendSliceRecord(attribution.Unattributed),
		// Stage attribution matches call times against stage records because
		// a provider call does not record the stage that made it. The flag
		// stays true until it does.
		StageAttributionApproximate: true,
	}
	for _, phase := range attribution.ByPhase {
		record.ByPhase = append(record.ByPhase, transport.PhaseSpendRecord{
			Phase: string(phase.Phase), Spend: spendSliceRecord(phase.Spend),
		})
	}
	for _, stage := range attribution.ByStage {
		record.ByStage = append(record.ByStage, transport.StageSpendRecord{
			Stage: uint32(stage.Stage), Name: stage.Name,
			Phase: string(stage.Phase), Spend: spendSliceRecord(stage.Spend),
		})
	}
	for _, model := range attribution.ByModel {
		record.ByModel = append(record.ByModel, transport.ModelSpendRecord{
			ProviderID: model.ProviderID, Model: model.Model,
			ModelVersion: model.ModelVersion,
			Spend:        spendSliceRecord(model.Spend),
		})
	}
	return record, nil
}

// spendSliceRecord converts one storage slice to its transport shape.
//
// The counts are non-negative by database constraint, so the conversion to
// unsigned is safe; a negative would mean the ledger itself was violated and is
// clamped to zero rather than wrapped to an enormous positive.
func spendSliceRecord(slice storage.SpendSlice) transport.SpendSliceRecord {
	record := transport.SpendSliceRecord{
		Calls:             unsignedCount(slice.Calls),
		InputTokens:       unsignedCount(slice.InputTokens),
		CachedInputTokens: unsignedCount(slice.CachedInputTokens),
		CacheWriteTokens:  unsignedCount(slice.CacheWriteTokens),
		OutputTokens:      unsignedCount(slice.OutputTokens),
		ReasoningTokens:   unsignedCount(slice.ReasoningTokens),
		UsageUnknownCalls: unsignedCount(slice.UsageUnknownCount),
		CostUnknownCalls:  unsignedCount(slice.CostUnknownCount),
	}
	// A slice with no priced call reports a known zero cost but names no
	// currency, and there is nothing for a client to render. Leaving CostKnown
	// false there keeps "nothing was priced" distinct from "this cost zero in
	// a named currency".
	if slice.CostKnown && slice.KnownCost.Denominator > 0 {
		record.CostKnown = true
		record.CostCurrency = string(slice.KnownCost.Currency)
		record.CostNumerator = slice.KnownCost.Numerator
		record.CostDenominator = slice.KnownCost.Denominator
	}
	return record
}

func unsignedCount(count storage.Count) uint64 {
	if !count.Known || count.Value < 0 {
		return 0
	}
	return uint64(count.Value)
}
