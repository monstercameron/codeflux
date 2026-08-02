package main

import (
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/readout"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
)

func taskControlsTopBar(current frontendstate.TopBarView, controls taskcontrols.Props) frontendstate.TopBarView {
	current.TaskSummary = controls.TaskSummary
	current.TaskState = string(controls.TaskState)
	current.Provider = knownTopBarValue(controls.Selection.Provider)
	current.Model = knownTopBarValue(controls.Selection.Model)
	current.Effort = knownTopBarValue(controls.Selection.Effort)
	current.ForecastCost = forecastCostTopBar(controls.Forecast.Range)
	current.ActualTokens = actualTokensTopBar(controls.Usage.Tokens)
	current.ActualCost = actualCostTopBar(controls.Usage.Cost)
	current.PricingSnapshot = knownTopBarValue(controls.Usage.Cost.PricingSnapshot)
	current.HardBudget = moneyTopBar(controls.Budget.HardLimitKnown, controls.Budget.HardLimit)
	current.RemainingBudget = moneyTopBar(controls.Budget.RemainingKnown, controls.Budget.Remaining)
	current.BudgetWarning = budgetWarningTopBar(controls.Budget)
	current.SpentFraction = spentFractionTopBar(controls.Usage.Cost, controls.Budget)
	current.BudgetWarned = controls.Budget.WarningReached || controls.Budget.HardCapReached
	current.CanPause = controls.OnPause != nil || controls.OnResume != nil
	current.CanStop = controls.OnStop != nil
	return current
}

func knownTopBarValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	return value
}

func forecastCostTopBar(value domain.ForecastRange) string {
	if !value.CostKnown {
		return "Unknown"
	}
	return readout.FormatExactMoneyRangeForReading(value.CostP50, value.CostP90)
}

func actualTokensTopBar(value domain.TokenUsage) string {
	total, known, err := value.Total()
	if err != nil || !known {
		return "Unknown"
	}
	return readout.FormatExactTokenCountForReading(uint64(total))
}

func actualCostTopBar(value taskcontrols.ActualCostView) string {
	if !value.Known {
		return "Unknown"
	}
	return exactMoneyTopBar(value.Value)
}

func moneyTopBar(known bool, value domain.Money) string {
	if !known {
		return "Unknown"
	}
	return exactMoneyTopBar(value)
}

func exactMoneyTopBar(value domain.Money) string {
	return readout.FormatExactMoneyForReading(value)
}

// spentFractionTopBar derives how much of the cap has been spent for the header
// meter, and refuses to derive one at all unless both figures were measured in
// the same currency. A meter fed a guess is worse than no meter: it is read
// without being questioned.
func spentFractionTopBar(cost taskcontrols.ActualCostView, budget taskcontrols.BudgetView) float64 {
	if !cost.Known || !budget.HardLimitKnown ||
		cost.Value.Currency != budget.HardLimit.Currency ||
		budget.HardLimit.MinorUnits <= 0 {
		return -1
	}
	if budget.HardCapReached {
		return 1
	}
	spent := float64(cost.Value.MinorUnits) / float64(budget.HardLimit.MinorUnits)
	if spent < 0 {
		return 0
	}
	if spent > 1 {
		return 1
	}
	return spent
}

func budgetWarningTopBar(value taskcontrols.BudgetView) string {
	if value.HardCapReached {
		return "Hard cap reached"
	}
	if value.WarningReached {
		if value.WarningThresholdKnown {
			return "Warning reached at " + exactMoneyTopBar(value.WarningThreshold)
		}
		return "Warning reached"
	}
	if value.WarningThresholdKnown {
		return "Below warning threshold " + exactMoneyTopBar(value.WarningThreshold)
	}
	return "Unknown"
}
