package main

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
)

func TestTaskControlsTopBarPreservesAuthoritativeKnownAndUnknownFacts(t *testing.T) {
	p50, _ := domain.NewMoney("USD", 25)
	p90, _ := domain.NewMoney("USD", 40)
	actual, _ := domain.NewMoney("USD", 18)
	hard, _ := domain.NewMoney("USD", 100)
	remaining, _ := domain.NewMoney("USD", 82)
	warning, _ := domain.NewMoney("USD", 75)
	controls := taskcontrols.Props{
		TaskState: domain.TaskStatePaused,
		Selection: taskcontrols.SelectionView{Provider: "openai", Model: "gpt-5.6-sol", Effort: "high"},
		Forecast:  taskcontrols.ForecastView{Range: domain.ForecastRange{CostKnown: true, CostP50: p50, CostP90: p90}},
		Usage: taskcontrols.UsageView{
			Tokens: domain.TokenUsage{Known: true, Input: 100, Output: 50},
			Cost:   taskcontrols.ActualCostView{Known: true, Value: actual, PricingSnapshot: "price-17"},
		},
		Budget: taskcontrols.BudgetView{
			HardLimitKnown: true, HardLimit: hard, RemainingKnown: true, Remaining: remaining,
			WarningThresholdKnown: true, WarningThreshold: warning, WarningReached: true,
		},
		OnResume: func() {}, OnStop: func() {},
	}
	got := taskControlsTopBar(frontendstate.TopBarView{}, controls)
	for name, value := range map[string]string{
		"provider": got.Provider, "model": got.Model, "effort": got.Effort,
		"forecast": got.ForecastCost, "tokens": got.ActualTokens, "cost": got.ActualCost,
		"pricing": got.PricingSnapshot, "hard": got.HardBudget,
		"remaining": got.RemainingBudget, "warning": got.BudgetWarning,
	} {
		if value == "" || value == "Unknown" {
			t.Errorf("%s = %q", name, value)
		}
	}
	if !got.CanPause || !got.CanStop || got.TaskState != "paused" {
		t.Fatalf("top bar actions/state = %#v", got)
	}

	unknown := taskControlsTopBar(frontendstate.TopBarView{}, taskcontrols.Props{})
	for name, value := range map[string]string{
		"provider": unknown.Provider, "model": unknown.Model, "forecast": unknown.ForecastCost,
		"tokens": unknown.ActualTokens, "cost": unknown.ActualCost,
		"pricing": unknown.PricingSnapshot, "remaining": unknown.RemainingBudget,
	} {
		if value != "Unknown" {
			t.Errorf("unknown %s = %q", name, value)
		}
	}
}
