package taskcontrols

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTaskControlPanelLabelsStatePhaseForecastAndActualsHonestly(t *testing.T) {
	props := fixtureProps(t)
	markup := renderPanel(t, props)
	for _, want := range []string{
		`data-component="task-control-panel"`, `data-task-state="running"`,
		`data-phase="editing"`, "Running", "Editing",
		"Provider", "OpenAI", "Model", "gpt-5.6-sol", "Effort", "high",
		"Estimated P50 1 s 250 ms · P90 2 s 500 ms",
		"Estimated P50 1200 · P90 2400",
		"Estimated P50 USD 40 minor units · P90 USD 90 minor units",
		"175 total · input 100 · cached input 25 · cache write 0 · output 30 · reasoning 20 exact provider-reported tokens",
		"USD 25 minor units actual · pricing snapshot prices-2026-07-31",
		"USD 375 minor units remaining of USD 400 minor units",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("observable task controls missing %q: %s", want, markup)
		}
	}
	if !strings.Contains(markup, "not a promise") {
		t.Fatal("forecast was not explicitly labeled as an estimate")
	}
}

func TestTaskControlDisclosureKeepsWorkspaceCompactByDefault(t *testing.T) {
	markup, err := ui.RenderToString(TaskControlDisclosure(fixtureProps(t)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-component="task-control-disclosure"`,
		`data-default-state="collapsed"`,
		"Task details, budget, and controls",
		`data-component="task-control-panel"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("task control disclosure missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `<details open`) {
		t.Fatalf("task controls expanded by default and can displace conversation: %s", markup)
	}
}

func TestTaskControlDisclosureOpensForActiveStopAndBudgetFlows(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Props)
		want   string
	}{
		{
			name: "stop confirmation",
			mutate: func(props *Props) {
				props.StopConfirm = StopConfirmation{Required: true, Open: true, Consequence: "external work may settle"}
			},
			want: `data-component="stop-confirmation"`,
		},
		{
			name: "budget editor",
			mutate: func(props *Props) {
				props.TaskState = domain.TaskStatePaused
				props.BudgetAdjust = BudgetAdjustment{Editing: true, OldLimit: money(t, 400), DraftMinorUnits: "650"}
				props.OnBudgetDraftChange = func(string) {}
				props.OnBudgetPreview = func() {}
				props.OnBudgetCancel = func() {}
			},
			want: `data-component="budget-adjustment-editor"`,
		},
		{
			name: "budget confirmation",
			mutate: func(props *Props) {
				props.BudgetAdjust = BudgetAdjustment{Open: true, OldLimit: money(t, 400), NewLimit: money(t, 650)}
			},
			want: `data-component="budget-adjustment-confirmation"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			props := fixtureProps(t)
			test.mutate(&props)
			markup, err := ui.RenderToString(TaskControlDisclosure(props))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{` open>`, `data-active-flow-open="true"`, test.want} {
				if !strings.Contains(markup, want) {
					t.Fatalf("active flow disclosure missing %q: %s", want, markup)
				}
			}
		})
	}
}

func TestUnknownActualCostNeverRendersAsZero(t *testing.T) {
	props := fixtureProps(t)
	props.Usage.Cost = ActualCostView{UnknownReason: "provider price is delayed"}
	props.Budget.RemainingKnown = false
	props.Budget.Remaining = domain.Money{}
	markup := renderPanel(t, props)
	for _, want := range []string{
		"Unknown — provider price is delayed",
		"Unknown — actual priced spend is incomplete",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("unknown price state missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `data-metric="actual-cost">USD 0`) ||
		strings.Contains(markup, `data-metric="actual-cost">$0`) {
		t.Fatalf("unknown cost was substituted with zero: %s", markup)
	}
}

func TestTaskStateControlsAppearOnlyInApplicableStates(t *testing.T) {
	tests := []struct {
		state       domain.TaskState
		want        []string
		notExpected []string
	}{
		{domain.TaskStateRunning, []string{"Pause task", "Stop task"}, []string{"Resume task", "Adjust budget task"}},
		{domain.TaskStateValidating, []string{"Pause task", "Stop task"}, []string{"Resume task", "Adjust budget task"}},
		{domain.TaskStatePaused, []string{"Resume task", "Stop task", "Adjust budget task"}, []string{"Pause task"}},
		{domain.TaskStateAwaitingReview, nil, []string{"Pause task", "Resume task", "Stop task", "Adjust budget task"}},
		{domain.TaskStateCompleted, nil, []string{"Pause task", "Resume task", "Stop task", "Adjust budget task"}},
	}
	for _, test := range tests {
		t.Run(string(test.state), func(t *testing.T) {
			props := fixtureProps(t)
			props.TaskState = test.state
			markup := renderPanel(t, props)
			for _, label := range test.want {
				if !strings.Contains(markup, `aria-label="`+label+`"`) {
					t.Errorf("state %s missing %s: %s", test.state, label, markup)
				}
			}
			for _, label := range test.notExpected {
				if strings.Contains(markup, `aria-label="`+label+`"`) {
					t.Errorf("state %s unexpectedly exposed %s: %s", test.state, label, markup)
				}
			}
		})
	}
}

func TestBusyAndUnavailableCommandsRetainKeysAndExplainDisabledState(t *testing.T) {
	props := fixtureProps(t)
	props.Controls.Pause = CommandState{Busy: true, IdempotencyKey: "idem-pause-1"}
	props.Controls.Stop = CommandState{DisabledReason: "Sequence certainty is unknown"}
	markup := renderPanel(t, props)
	for _, want := range []string{
		`data-command="pause"`, `data-command-key="idem-pause-1"`, `data-busy="true"`,
		`data-command="stop"`, `data-disabled-reason="Sequence certainty is unknown"`,
		`id="task-command-stop-reason"`, `aria-describedby="task-command-stop-reason"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("command contract missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, `id="task-command-pause"`) != 1 ||
		!buttonOpeningTagContains(markup, "task-command-pause", "disabled") {
		t.Fatalf("busy pause command was not a single disabled control: %s", markup)
	}
}

func TestBusyCommandWithoutRetainedKeyFailsTheComponentContract(t *testing.T) {
	props := fixtureProps(t)
	props.Controls.Pause = CommandState{Busy: true}
	markup := renderPanel(t, props)
	for _, want := range []string{
		`data-component="inline-alert"`, `data-tone="failure"`,
		"Task controls unavailable", "pause busy command requires an idempotency key",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("invalid command contract missing %q: %s", want, markup)
		}
	}
}

func TestBudgetWarningHardCapAndConfirmationAreExactAndNonSpamming(t *testing.T) {
	props := fixtureProps(t)
	props.Budget.WarningReached = true
	props.Budget.HardCapReached = true
	props.Budget.SettlingInFlightKnown = true
	props.Budget.SettlingInFlight = true
	props.Budget.CheckpointKnown = true
	props.Budget.CheckpointedState = "checkpoint cp-17"
	props.BudgetAdjust = BudgetAdjustment{
		Open: true, OldLimit: money(t, 400), NewLimit: money(t, 650),
	}
	markup := renderPanel(t, props)
	if strings.Count(markup, `data-component="budget-warning"`) != 1 {
		t.Fatalf("budget warning was not a single surface: %s", markup)
	}
	for _, want := range []string{
		`data-component="hard-cap-decision"`, "in-flight provider request is settling",
		"Old hard budget: USD 400 minor units", "New hard budget: USD 650 minor units",
		"Confirm exact budget change", "Finishing with current work remains unavailable",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("budget decision missing %q: %s", want, markup)
		}
	}
}

func TestHardCapDoesNotInventMissingSettlementOrCheckpointFacts(t *testing.T) {
	props := fixtureProps(t)
	props.Budget.HardCapReached = true
	markup := renderPanel(t, props)
	for _, want := range []string{
		"Whether a provider request is still settling is unknown",
		"Checkpointed state: Unknown",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("unknown hard-cap fact missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "No provider request is settling") {
		t.Fatalf("hard-cap surface invented a negative settlement fact: %s", markup)
	}
}

func TestBudgetEditorCollectsExactMinorUnitsBeforeConfirmation(t *testing.T) {
	props := fixtureProps(t)
	props.TaskState = domain.TaskStatePaused
	props.BudgetAdjust = BudgetAdjustment{
		Editing: true, DraftMinorUnits: "650", OldLimit: money(t, 400),
		InvalidMessage: "Review the exact amount.",
	}
	props.OnBudgetDraftChange = func(string) {}
	props.OnBudgetPreview = func() {}
	props.OnBudgetCancel = func() {}
	markup := renderPanel(t, props)
	for _, want := range []string{
		`data-component="budget-adjustment-editor"`,
		`id="task-budget-minor-units"`, `type="number"`, `value="650"`,
		"New hard budget (USD minor units)", "Review exact budget change",
		"Cancel budget change", "Review the exact amount.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("budget editor missing %q: %s", want, markup)
		}
	}
}

func TestStopConfirmationAppearsOnlyForNonObviousConsequence(t *testing.T) {
	props := fixtureProps(t)
	markup := renderPanel(t, props)
	if strings.Contains(markup, `data-component="stop-confirmation"`) {
		t.Fatalf("ordinary stop unexpectedly required confirmation: %s", markup)
	}
	props.StopConfirm = StopConfirmation{
		Required: true, Open: true,
		Consequence: "the uncommitted external deployment may still finish",
	}
	markup = renderPanel(t, props)
	for _, want := range []string{
		`data-component="stop-confirmation"`,
		"the uncommitted external deployment may still finish",
		"Confirm stop task", "Keep task running",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("stop consequence confirmation missing %q: %s", want, markup)
		}
	}
}

func TestCalmRecoveryLeadsWithKnownStateAndNeverOffersUnsafeRetry(t *testing.T) {
	props := fixtureProps(t)
	props.TaskState = domain.TaskStateRecoveryRequired
	props.Phase = PhaseRepairing
	props.Recovery = RecoveryView{
		Required: true, KnownState: "checkpoint is durable; provider outcome is unknown",
		LastCheckpointAt:         time.Date(2026, 7, 31, 14, 5, 0, 0, time.UTC),
		LastCheckpointPlanStep:   "validate generated client",
		DivergenceSummary:        "two user-edited files differ from the task worktree",
		Ambiguity:                "external publish may have completed",
		ExternalOutcomeAmbiguous: true,
		SafestRecommendation:     "Reconcile the publish receipt before continuing.",
		SafeResumeVerified:       false, ReconcileRequired: true, PatchPreservable: true,
		Details: []RecoveryDetail{
			{Kind: RecoveryDetailEvent, Identity: "evt-19", Label: "Publish event evt-19"},
			{Kind: RecoveryDetailFile, Identity: "web/client/main.go", Label: "web/client/main.go"},
		},
	}
	markup := renderPanel(t, props)
	for _, want := range []string{
		`data-component="recovery-panel"`, `data-tone="calm"`, `data-known-state-first="true"`,
		"checkpoint is durable; provider outcome is unknown",
		"2026-07-31T14:05:00Z · plan step validate generated client",
		"two user-edited files differ", "External action outcome is ambiguous",
		"Reconcile user edits", "Preserve patch", `aria-label="Recovery actions, safest first"`,
		`aria-label="Recovery events and files"`, "Open recovery event Publish event evt-19",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("recovery surface missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, ">Retry<") || strings.Contains(markup, "Safe resume") {
		t.Fatalf("unsafe recovery offered retry/resume: %s", markup)
	}
	if strings.Contains(markup, "href=") {
		t.Fatalf("recovery details used raw navigation instead of callbacks: %s", markup)
	}
	if strings.Index(markup, "Reconcile user edits") > strings.Index(markup, "Preserve patch") {
		t.Fatalf("safest recovery action was not first: %s", markup)
	}
}

func TestVerifiedSafeResumeIsTheFirstRecoveryAction(t *testing.T) {
	props := fixtureProps(t)
	props.TaskState = domain.TaskStateRecoveryRequired
	props.Recovery = RecoveryView{
		Required: true, KnownState: "checkpoint and worktree match",
		SafestRecommendation: "Resume from the verified checkpoint.",
		SafeResumeVerified:   true, ReconcileRequired: true, PatchPreservable: true,
	}
	markup := renderPanel(t, props)
	resumeAt := strings.Index(markup, "Safe resume")
	reconcileAt := strings.Index(markup, "Reconcile user edits")
	preserveAt := strings.Index(markup, "Preserve patch")
	if resumeAt < 0 || reconcileAt < 0 || preserveAt < 0 ||
		resumeAt > reconcileAt || reconcileAt > preserveAt {
		t.Fatalf("recovery actions are not ordered safest-first: %s", markup)
	}
}

func TestDisconnectedSequenceAndReviewStalenessRemainDistinct(t *testing.T) {
	props := fixtureProps(t)
	props.Delivery = DeliveryView{
		State: DeliveryDisconnected, SequenceCertain: false, TimelineReadable: true,
		Explanation: "Coordinator connection was lost.",
	}
	props.Review = ReviewStaleness{Stale: true, Reasons: []string{
		"Diff revision changed", "Validation revision changed",
	}}
	props.Controls.Stop.DisabledReason = "Sequence certainty is unknown"
	markup := renderPanel(t, props)
	for _, want := range []string{
		`data-ui-connection="disconnected"`, `data-backend-task-state="running"`,
		`data-sequence-certain="false"`, "The timeline remains readable",
		`data-component="review-staleness"`, "Diff revision changed", "Validation revision changed",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("offline/stale distinction missing %q: %s", want, markup)
		}
	}
}

func TestRecoveryDetailIsKeyboardButtonWithTypedCallback(t *testing.T) {
	props := fixtureProps(t)
	props.TaskState = domain.TaskStateRecoveryRequired
	props.Recovery = RecoveryView{
		Required: true, KnownState: "checkpoint valid", SafestRecommendation: "Inspect the event.",
		Details: []RecoveryDetail{{Kind: RecoveryDetailEvent, Identity: "evt-22", Label: "Event 22"}},
	}
	var opened RecoveryDetail
	props.OnOpenDetail = func(detail RecoveryDetail) { opened = detail }
	root := TaskControlPanel(props)
	handler, found := findButtonHandler(root, "Open recovery event Event 22")
	if !found || handler == nil {
		t.Fatal("recovery detail did not render a keyboard button callback")
	}
	handler()
	if opened.Identity != "evt-22" || opened.Kind != RecoveryDetailEvent {
		t.Fatalf("opened recovery detail = %#v", opened)
	}
}

func TestUnsafeRecoveryDetailRemainsVisibleButDisabled(t *testing.T) {
	props := fixtureProps(t)
	props.TaskState = domain.TaskStateRecoveryRequired
	props.Recovery = RecoveryView{
		Required: true, KnownState: "checkpoint valid", SafestRecommendation: "Inspect the event.",
		Details: []RecoveryDetail{{
			Kind: RecoveryDetailFile, Identity: "../secret", Label: "../secret",
			DisabledReason: "Unsafe workspace-relative path.",
		}},
	}
	called := false
	props.OnOpenDetail = func(RecoveryDetail) { called = true }
	root := TaskControlPanel(props)
	handler, found := findButtonHandler(root, "Open recovery file ../secret")
	if !found {
		t.Fatal("unsafe recovery file disappeared instead of remaining inspectable")
	}
	if handler != nil {
		handler()
	}
	if called {
		t.Fatal("unsafe recovery file dispatched its navigation callback")
	}
	markup := renderPanel(t, props)
	if !strings.Contains(markup, `disabled`) || !strings.Contains(markup, "../secret") {
		t.Fatalf("unsafe detail is not visibly disabled: %s", markup)
	}
}

func fixtureProps(t *testing.T) Props {
	t.Helper()
	return Props{
		Mode:      primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable},
		TaskState: domain.TaskStateRunning, Phase: PhaseEditing,
		Delivery:  DeliveryView{State: DeliveryLive, SequenceCertain: true, TimelineReadable: true},
		Selection: SelectionView{Provider: "OpenAI", Model: "gpt-5.6-sol", Effort: "high"},
		Forecast: ForecastView{Range: domain.ForecastRange{
			LatencyKnown: true, LatencyP50Millis: 1250, LatencyP90Millis: 2500,
			TokensKnown: true, TokensP50: 1200, TokensP90: 2400,
			CostKnown: true, CostP50: money(t, 40), CostP90: money(t, 90),
		}, Assumptions: "repository index is warm"},
		Usage: UsageView{
			Tokens: domain.TokenUsage{Known: true, Input: 100, CachedInput: 25, Output: 30, Reasoning: 20},
			Cost:   ActualCostView{Known: true, Value: money(t, 25), PricingSnapshot: "prices-2026-07-31"},
		},
		Budget: BudgetView{
			HardLimitKnown: true, HardLimit: money(t, 400), RemainingKnown: true, Remaining: money(t, 375),
			WarningThresholdKnown: true, WarningThreshold: money(t, 300),
		},
		OnPause: func() {}, OnResume: func() {}, OnStop: func() {},
		OnStopConfirm: func() {}, OnStopCancel: func() {},
		OnBudgetAdjust: func() {}, OnBudgetConfirm: func() {}, OnBudgetCancel: func() {},
		OnBudgetWarningDismiss: func() {},
		OnSafeResume:           func() {}, OnReconcile: func() {}, OnPreservePatch: func() {},
		OnOpenDetail: func(RecoveryDetail) {},
	}
}

func money(t *testing.T, minor int64) domain.Money {
	t.Helper()
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	value, err := domain.NewMoney(usd, minor)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func renderPanel(t *testing.T, props Props) string {
	t.Helper()
	markup, err := ui.RenderToString(TaskControlPanel(props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func buttonOpeningTagContains(markup, id, value string) bool {
	idAt := strings.Index(markup, `id="`+id+`"`)
	if idAt < 0 {
		return false
	}
	start := strings.LastIndex(markup[:idAt], "<button")
	end := strings.Index(markup[idAt:], ">")
	return start >= 0 && end >= 0 && strings.Contains(markup[start:idAt+end], value)
}

func findButtonHandler(node ui.Node, accessibleLabel string) (func(), bool) {
	if node == nil {
		return nil, false
	}
	if label, ok := node.Props["aria-label"].(string); ok && label == accessibleLabel {
		handler, _ := node.Props["onclick"].(func())
		return handler, true
	}
	for _, child := range node.Children {
		childNode, ok := child.(ui.Node)
		if !ok {
			continue
		}
		if handler, found := findButtonHandler(childNode, accessibleLabel); found {
			return handler, true
		}
	}
	return nil, false
}
