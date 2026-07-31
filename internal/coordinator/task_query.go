package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TaskQueryStore owns the one SQLite snapshot used by TaskService.GetTask.
type TaskQueryStore interface {
	ReadTaskServiceSnapshot(
		context.Context,
		domain.TaskID,
	) (storage.TaskServiceSnapshot, error)
}

// TaskQueryService adapts authoritative SQLite projections to the transport
// query port without introducing a second task-state authority.
type TaskQueryService struct {
	store TaskQueryStore
}

func NewTaskQueryService(store TaskQueryStore) (*TaskQueryService, error) {
	if store == nil {
		return nil, errors.New("task query store is required")
	}
	return &TaskQueryService{store: store}, nil
}

func (service *TaskQueryService) GetTaskQuery(
	ctx context.Context,
	taskID domain.TaskID,
) (transport.TaskQueryView, error) {
	snapshot, err := service.store.ReadTaskServiceSnapshot(ctx, taskID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return transport.TaskQueryView{}, transport.ErrTaskQueryNotFound
		}
		return transport.TaskQueryView{}, err
	}
	view := transport.TaskQueryView{
		TaskID:                  snapshot.Task.ID,
		ThreadID:                snapshot.Task.ThreadID,
		SessionID:               snapshot.SessionID,
		State:                   snapshot.Task.State,
		Revision:                snapshot.Task.Revision,
		PlanRevision:            snapshot.PlanRevision,
		SummaryRedacted:         snapshot.SummaryRedacted,
		SummaryOriginalBytes:    snapshot.SummaryOriginalBytes,
		SummaryTruncated:        snapshot.SummaryTruncated,
		UpdatedAt:               snapshot.Task.UpdatedAt,
		SettlingProviderRequest: snapshot.SettlingProviderRequest,
	}
	if snapshot.LatestCheckpoint != nil {
		checkpointID := snapshot.LatestCheckpoint.ID
		view.LatestCheckpointID = &checkpointID
		view.LatestCheckpointState = snapshot.LatestCheckpoint.State
		view.LatestCheckpointPlanStep = snapshot.LatestCheckpoint.PlanStep
	}
	if snapshot.Policy != nil {
		view.SelectedProvider = snapshot.Policy.Model.Provider.Provider
		view.SelectedModel = snapshot.Policy.Model.Model
		view.SelectedEffort = string(snapshot.Policy.Reasoning)
	}
	if snapshot.Forecast != nil {
		value := snapshot.Forecast
		rangeValue := domain.ForecastRange{
			LatencyKnown:     true,
			LatencyP50Millis: value.Latency.P50Millis,
			LatencyP90Millis: value.Latency.P90Millis,
			TokensKnown:      true,
			TokensP50:        value.Tokens.P50,
			TokensP90:        value.Tokens.P90,
		}
		if value.Cost.Known {
			currency, currencyErr := domain.ParseCurrencyCode(value.Cost.P50.Currency)
			if currencyErr != nil || value.Cost.P90.Currency != value.Cost.P50.Currency {
				return transport.TaskQueryView{}, errors.New("forecast cost currency is invalid")
			}
			p50, p50Err := integralTaskMoney(storage.ExactMinorCost{
				Currency:    currency,
				Numerator:   value.Cost.P50.Numerator,
				Denominator: value.Cost.P50.Denominator,
			})
			p90, p90Err := integralTaskMoney(storage.ExactMinorCost{
				Currency:    currency,
				Numerator:   value.Cost.P90.Numerator,
				Denominator: value.Cost.P90.Denominator,
			})
			if p50Err != nil || p90Err != nil {
				return transport.TaskQueryView{}, errors.Join(p50Err, p90Err)
			}
			if p50 != nil && p90 != nil {
				rangeValue.CostKnown = true
				rangeValue.CostP50 = *p50
				rangeValue.CostP90 = *p90
			}
		}
		reasons := make([]string, len(value.UncertaintyReasons))
		for index, reason := range value.UncertaintyReasons {
			reasons[index] = string(reason)
		}
		view.Forecast = &transport.TaskForecastQueryView{
			Range: rangeValue, AlgorithmVersion: value.AlgorithmVersion,
			EstimateNotice:     value.EstimateNotice,
			PriceSnapshotID:    value.PriceAssumptions.SnapshotID,
			PriceSource:        value.PriceAssumptions.Source,
			PriceCapturedAt:    value.PriceAssumptions.CapturedAt,
			UncertaintyReasons: reasons, Revision: snapshot.ForecastRevision,
		}
	}
	end := snapshot.ObservedAt
	if snapshot.Task.State.IsTerminal() {
		end = snapshot.Task.UpdatedAt
	}
	if end.Before(snapshot.Task.CreatedAt) {
		return transport.TaskQueryView{}, errors.New("task snapshot time range is invalid")
	}
	view.Elapsed = end.Sub(snapshot.Task.CreatedAt)
	if snapshot.Budget == nil {
		return view, nil
	}
	view.BudgetRevision = snapshot.Budget.Revision
	view.HardBudget, err = integralTaskMoney(snapshot.Budget.HardCost)
	if err != nil {
		return transport.TaskQueryView{}, err
	}
	view.RemainingHardBudget, err = integralOptionalTaskMoney(snapshot.Budget.RemainingCost)
	if err != nil {
		return transport.TaskQueryView{}, err
	}
	view.WarningThreshold, err = integralTaskMoney(snapshot.Budget.WarningCost)
	if err != nil {
		return transport.TaskQueryView{}, err
	}
	view.WarningReached = snapshot.Budget.WarningReached
	view.HardCapReached = snapshot.Budget.HardCapReached
	view.ActualPricingSnapshotIDs = append([]string(nil), snapshot.ActualPricingSnapshotIDs...)
	if !snapshot.Budget.CostAccountingUnknown && len(view.ActualPricingSnapshotIDs) > 0 {
		view.ActualCost, err = integralTaskMoney(snapshot.Budget.ActualKnownCost)
		if err != nil {
			return transport.TaskQueryView{}, err
		}
	}
	if !snapshot.Budget.TokenAccountingUnknown {
		actual := snapshot.Budget.ActualTokens
		view.ActualTokens = &actual
	}
	return view, nil
}

func integralOptionalTaskMoney(value *storage.ExactMinorCost) (*domain.Money, error) {
	if value == nil {
		return nil, nil
	}
	return integralTaskMoney(*value)
}

func integralTaskMoney(value storage.ExactMinorCost) (*domain.Money, error) {
	if value.Denominator <= 0 || value.Numerator < 0 {
		return nil, errors.New("task money exact ratio is invalid")
	}
	if _, err := domain.ParseCurrencyCode(string(value.Currency)); err != nil {
		return nil, err
	}
	if value.Denominator != 1 {
		// The v1 Money wire type carries only integral minor units. Omitting a
		// non-integral rational is exact; rounding it would fabricate money.
		return nil, nil
	}
	money, err := domain.NewMoney(value.Currency, value.Numerator)
	if err != nil {
		return nil, err
	}
	if money.MinorUnits < 0 {
		return nil, errors.New("task money must not be negative")
	}
	return &money, nil
}

var _ transport.TaskQueryApplication = (*TaskQueryService)(nil)
