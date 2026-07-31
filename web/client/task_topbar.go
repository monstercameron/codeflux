package main

import (
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
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
	return "Estimated P50 " + exactMoneyTopBar(value.CostP50) + " · P90 " + exactMoneyTopBar(value.CostP90)
}

func actualTokensTopBar(value domain.TokenUsage) string {
	total, known, err := value.Total()
	if err != nil || !known {
		return "Unknown"
	}
	return strconv.FormatUint(uint64(total), 10) + " exact tokens"
}

func actualCostTopBar(value taskcontrols.ActualCostView) string {
	if !value.Known {
		return "Unknown"
	}
	return exactMoneyTopBar(value.Value) + " actual"
}

func moneyTopBar(known bool, value domain.Money) string {
	if !known {
		return "Unknown"
	}
	return exactMoneyTopBar(value)
}

func exactMoneyTopBar(value domain.Money) string {
	return string(value.Currency) + " " + strconv.FormatInt(value.MinorUnits, 10) + " minor units"
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
