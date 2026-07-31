package shell_test

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAppRootMountsAuthoritativeTaskControlsOnlyWhenProvided(t *testing.T) {
	without := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: readySnapshot(), Route: routes.Route{Name: routes.ThreadWorkspace}, Tokens: tokens(t),
	}))
	if strings.Contains(without, `data-component="task-control-panel"`) {
		t.Fatalf("nil task-control seam rendered a synthetic panel: %s", without)
	}

	controls := shellTaskControls(t)
	with := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: readySnapshot(), Route: routes.Route{Name: routes.ThreadWorkspace}, Tokens: tokens(t),
		TaskControls: &controls,
	}))
	for _, want := range []string{
		`data-component="task-observability-region"`,
		`data-component="task-control-panel"`,
		`data-task-state="running"`, `data-phase="editing"`,
		`data-ui-connection="disconnected"`, `data-sequence-certain="false"`,
		"The timeline remains readable", "gpt-5.6-sol", "USD 300 minor units remaining",
		`aria-label="Pause task"`, `aria-label="Stop task"`,
	} {
		if !strings.Contains(with, want) {
			t.Errorf("mounted task-control seam missing %q: %s", want, with)
		}
	}
	if strings.Count(with, `data-component="task-control-panel"`) != 1 {
		t.Fatalf("task controls mounted more than once: %s", with)
	}
}

func TestTaskControlsDoNotLeakIntoNonTaskRoutes(t *testing.T) {
	controls := shellTaskControls(t)
	markup := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: readySnapshot(), Route: routes.Route{Name: routes.Settings}, Tokens: tokens(t),
		TaskControls: &controls,
	}))
	if strings.Contains(markup, `data-component="task-control-panel"`) ||
		strings.Contains(markup, `data-component="task-observability-region"`) {
		t.Fatalf("task controls leaked into settings route: %s", markup)
	}
}

func TestWorkspaceMountsCalmRecoveryAndReviewStalenessFromTypedSeam(t *testing.T) {
	controls := shellTaskControls(t)
	controls.TaskState = domain.TaskStateRecoveryRequired
	controls.Phase = taskcontrols.PhaseRepairing
	controls.Recovery = taskcontrols.RecoveryView{
		Required: true, KnownState: "checkpoint cp-18 is durable",
		LastCheckpointAt:         time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC),
		LastCheckpointPlanStep:   "validate browser controls",
		DivergenceSummary:        "user edits differ in one tracked file",
		Ambiguity:                "external command outcome is not attributable",
		ExternalOutcomeAmbiguous: true,
		SafestRecommendation:     "Reconcile the external outcome before continuing.",
		ReconcileRequired:        true, PatchPreservable: true,
		Details: []taskcontrols.RecoveryDetail{{
			Kind: taskcontrols.RecoveryDetailEvent, Identity: "evt-18", Label: "Event evt-18",
		}},
	}
	controls.Review = taskcontrols.ReviewStaleness{
		Stale: true, Reasons: []string{"Validation revision changed"},
	}
	markup := render(t, ui.CreateElement(shell.TaskWorkspaceShell, shell.TaskWorkspaceProps{
		Snapshot: readySnapshot(), Tokens: tokens(t), TaskControls: &controls,
	}))
	for _, want := range []string{
		`data-component="recovery-panel"`, `data-known-state-first="true"`,
		"checkpoint cp-18 is durable", "validate browser controls",
		`data-component="ambiguous-external-outcome"`,
		"Reconcile user edits", "Preserve patch",
		`data-component="review-staleness"`, "Validation revision changed",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("integrated recovery seam missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, ">Retry<") || strings.Contains(markup, "href=") {
		t.Fatalf("integrated recovery exposed unsafe retry or raw navigation: %s", markup)
	}
}

func shellTaskControls(t *testing.T) taskcontrols.Props {
	t.Helper()
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	money := func(value int64) domain.Money {
		result, moneyErr := domain.NewMoney(usd, value)
		if moneyErr != nil {
			t.Fatal(moneyErr)
		}
		return result
	}
	return taskcontrols.Props{
		Mode:      primitives.Mode{Theme: design.ThemeLight, Density: design.DensityCompact},
		TaskState: domain.TaskStateRunning, Phase: taskcontrols.PhaseEditing,
		Delivery: taskcontrols.DeliveryView{
			State: taskcontrols.DeliveryDisconnected, SequenceCertain: false,
			TimelineReadable: true, Explanation: "Coordinator connection was lost.",
		},
		Selection: taskcontrols.SelectionView{Provider: "OpenAI", Model: "gpt-5.6-sol", Effort: "high"},
		Forecast:  taskcontrols.ForecastView{Range: domain.ForecastRange{}},
		Usage: taskcontrols.UsageView{
			Tokens: domain.TokenUsage{},
			Cost:   taskcontrols.ActualCostView{UnknownReason: "pricing has not arrived"},
		},
		Budget: taskcontrols.BudgetView{
			HardLimitKnown: true, HardLimit: money(400), RemainingKnown: true, Remaining: money(300),
			WarningThresholdKnown: true, WarningThreshold: money(320),
		},
		Controls: taskcontrols.ControlState{
			Pause: taskcontrols.CommandState{DisabledReason: "Sequence certainty is unknown"},
			Stop:  taskcontrols.CommandState{DisabledReason: "Sequence certainty is unknown"},
		},
		OnPause: func() {}, OnStop: func() {}, OnBudgetAdjust: func() {},
		OnReconcile: func() {}, OnPreservePatch: func() {},
		OnOpenDetail: func(taskcontrols.RecoveryDetail) {},
	}
}
