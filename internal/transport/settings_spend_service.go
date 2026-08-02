package transport

import (
	"context"
	"math/big"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
)

// spendCostScale is how many extra decimal places a reported cost carries
// beyond the currency's own minor unit.
//
// A single model call routinely costs a small fraction of one minor unit, so
// reporting whole minor units would round most calls to free — which is the
// defect this surface exists to correct. Six places put a call costing
// two-thirds of a cent on the wire as 666667 micro-cents, and the boundary
// caps decimal_places at nine.
const spendCostScale = 6

// SpendSliceRecord is usage and cost for one grouping key, as the application
// layer reports it.
//
// Cost is carried as the exact rational the ledger stored rather than as a
// rendered number, so the conversion to the wire's fixed scale happens once,
// here, where the rounding can be detected and declared.
type SpendSliceRecord struct {
	Calls             uint64
	InputTokens       uint64
	CachedInputTokens uint64
	CacheWriteTokens  uint64
	OutputTokens      uint64
	ReasoningTokens   uint64
	UsageUnknownCalls uint64
	CostUnknownCalls  uint64
	// CostKnown is false when this slice's calls disagreed on currency or
	// carried no price. A false value must reach the client as an absent
	// amount, never as a zero.
	CostKnown       bool
	CostCurrency    string
	CostNumerator   int64
	CostDenominator int64
}

// PhaseSpendRecord is what one movement of the flow cost.
type PhaseSpendRecord struct {
	Phase string
	Spend SpendSliceRecord
}

// StageSpendRecord is what one stage cost, carrying its phase so a client can
// group without a second lookup.
type StageSpendRecord struct {
	Stage uint32
	Name  string
	Phase string
	Spend SpendSliceRecord
}

// ModelSpendRecord is what one model cost across every stage that used it.
type ModelSpendRecord struct {
	ProviderID   string
	Model        string
	ModelVersion string
	Spend        SpendSliceRecord
}

// SpendSummaryRecord is one window's recorded spend, sliced by flow phase,
// stage, and model.
type SpendSummaryRecord struct {
	Total   SpendSliceRecord
	ByPhase []PhaseSpendRecord
	ByStage []StageSpendRecord
	ByModel []ModelSpendRecord
	// Unattributed is the calls no stage claimed. It is the documented
	// difference between Total and the phases.
	Unattributed SpendSliceRecord
	// StageAttributionApproximate is true while stage attribution is derived
	// from call and stage timestamps rather than from a stage recorded on the
	// call itself. A client showing phase costs must label them estimates.
	StageAttributionApproximate bool
}

// SpendSummaryQuery bounds a summary to an explicit closed window.
type SpendSummaryQuery struct {
	Since time.Time
	Until time.Time
}

// SpendSummaryApplication reads recorded spend.
type SpendSummaryApplication interface {
	// ReadSpendSummary reports one window's recorded provider spend.
	ReadSpendSummary(context.Context, SpendSummaryQuery) (SpendSummaryRecord, error)
}

// GetSpendSummary answers what a window of work cost and where the money went.
func (service *SettingsService) GetSpendSummary(
	ctx context.Context,
	request *codefluxv1.GetSpendSummaryRequest,
) (*codefluxv1.GetSpendSummaryResponse, error) {
	query, err := spendSummaryQueryFromProto(request)
	if err != nil {
		return nil, err
	}
	summary, err := service.configuration.ReadSpendSummary(ctx, query)
	if err != nil {
		return nil, err
	}
	response := &codefluxv1.GetSpendSummaryResponse{
		Total:                       spendSliceToProto(summary.Total),
		Unattributed:                spendSliceToProto(summary.Unattributed),
		StageAttributionApproximate: summary.StageAttributionApproximate,
	}
	for _, phase := range summary.ByPhase {
		response.ByPhase = append(response.ByPhase, &codefluxv1.PhaseSpendView{
			Phase: phase.Phase, Spend: spendSliceToProto(phase.Spend),
		})
	}
	for _, stage := range summary.ByStage {
		response.ByStage = append(response.ByStage, &codefluxv1.StageSpendView{
			StageNumber: stage.Stage, StageName: stage.Name, Phase: stage.Phase,
			Spend: spendSliceToProto(stage.Spend),
		})
	}
	for _, model := range summary.ByModel {
		response.ByModel = append(response.ByModel, &codefluxv1.ModelSpendView{
			ProviderId: model.ProviderID, Model: model.Model,
			ModelVersion: model.ModelVersion,
			Spend:        spendSliceToProto(model.Spend),
		})
	}
	return response, nil
}

// spendSummaryQueryFromProto validates the requested window.
//
// Both bounds are required. Defaulting an absent bound to "all time" would let
// a client that forgot a field receive a figure covering a period it never
// asked about and would present as the current one.
func spendSummaryQueryFromProto(
	request *codefluxv1.GetSpendSummaryRequest,
) (SpendSummaryQuery, error) {
	since := request.GetSince()
	until := request.GetUntil()
	if since == nil {
		return SpendSummaryQuery{}, &RequestValidationError{
			Field: "since", Reason: "is required",
		}
	}
	if until == nil {
		return SpendSummaryQuery{}, &RequestValidationError{
			Field: "until", Reason: "is required",
		}
	}
	if err := since.CheckValid(); err != nil {
		return SpendSummaryQuery{}, &RequestValidationError{
			Field: "since", Reason: "must be a valid timestamp",
		}
	}
	if err := until.CheckValid(); err != nil {
		return SpendSummaryQuery{}, &RequestValidationError{
			Field: "until", Reason: "must be a valid timestamp",
		}
	}
	query := SpendSummaryQuery{
		Since: since.AsTime().UTC(), Until: until.AsTime().UTC(),
	}
	if query.Until.Before(query.Since) {
		return SpendSummaryQuery{}, &RequestValidationError{
			Field: "until", Reason: "must not precede since",
		}
	}
	return query, nil
}

func spendSliceToProto(record SpendSliceRecord) *codefluxv1.SpendSliceView {
	return &codefluxv1.SpendSliceView{
		Calls:             record.Calls,
		InputTokens:       record.InputTokens,
		CachedInputTokens: record.CachedInputTokens,
		CacheWriteTokens:  record.CacheWriteTokens,
		OutputTokens:      record.OutputTokens,
		ReasoningTokens:   record.ReasoningTokens,
		UsageUnknownCalls: record.UsageUnknownCalls,
		CostUnknownCalls:  record.CostUnknownCalls,
		KnownCost:         spendCostToProto(record),
	}
}

// spendCostToProto renders the exact rational cost at the wire's fixed scale.
//
// An unknown cost produces a SpendCost with no amount rather than an amount of
// zero, so a client cannot render "$0.00" for a call nobody could price. When
// the rational does not divide evenly at this scale the value is rounded to
// nearest and exact is false, because a figure presented as exact that is not
// is the failure this whole path is built to avoid.
func spendCostToProto(record SpendSliceRecord) *codefluxv1.SpendCost {
	if !record.CostKnown || record.CostCurrency == "" {
		return &codefluxv1.SpendCost{}
	}
	if record.CostDenominator <= 0 {
		return &codefluxv1.SpendCost{}
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(spendCostScale), nil)
	scaled := new(big.Int).Mul(big.NewInt(record.CostNumerator), scale)
	denominator := big.NewInt(record.CostDenominator)
	quotient, remainder := new(big.Int).QuoRem(scaled, denominator, new(big.Int))
	exact := remainder.Sign() == 0
	if !exact {
		// Round half away from zero, so a reported cost never sits below the
		// amount actually incurred by more than half a unit of this scale.
		doubled := new(big.Int).Abs(new(big.Int).Mul(remainder, big.NewInt(2)))
		if doubled.Cmp(denominator) >= 0 {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return &codefluxv1.SpendCost{}
	}
	return &codefluxv1.SpendCost{
		Amount: &codefluxv1.Money{
			CurrencyCode:  record.CostCurrency,
			MinorUnits:    quotient.Int64(),
			DecimalPlaces: spendCostScale,
		},
		Exact: exact,
	}
}
