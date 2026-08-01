package taskcontrols

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TaskControlDisclosure keeps the conversation workspace reachable while the
// complete authoritative task surface remains available on demand.
func TaskControlDisclosure(props Props) ui.Node {
	activeFlowOpen := props.StopConfirm.Open || props.BudgetAdjust.Editing || props.BudgetAdjust.Open
	return html.Details(html.Props{
		Open: activeFlowOpen,
		Data: map[string]string{
			"component": "task-control-disclosure", "default-state": "collapsed",
			"active-flow-open": strconv.FormatBool(activeFlowOpen),
		},
		Class: sectionClass(props.Mode.Tokens()),
	},
		html.Summary(html.Props{
			Class: css.New(
				css.FontWeight.Semibold,
				css.TextColor(css.Hex(string(props.Mode.Tokens().Colors.TextPrimary))),
			).String(),
			Text: "Task details, budget, and controls",
		}),
		ui.CreateElement(TaskControlPanel, props),
	)
}

func TaskControlPanel(props Props) ui.Node {
	if err := props.Validate(); err != nil {
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Task controls unavailable", Message: err.Error(),
			Tone: design.StatusFailure, Mode: props.Mode,
		})
	}
	tokens := props.Mode.Tokens()
	children := []ui.Node{
		taskIdentity(props),
		deliveryStatus(props),
		metrics(props),
		budgetPresentation(props),
	}
	if notice := strings.TrimSpace(props.CommandNotice); notice != "" {
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Task command status", Message: notice, Tone: design.StatusWarning, Mode: props.Mode,
		}))
	}
	children = append(children, commandControls(props))
	if props.Review.Stale {
		children = append(children, reviewStaleness(props))
	}
	if props.Recovery.Required {
		children = append(children, RecoveryPanel(props))
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Task status, cost, budget, and controls"},
		Data: map[string]string{
			"component": "task-control-panel", "task-state": string(props.TaskState),
			"phase": string(props.Phase), "delivery": string(props.Delivery.State),
			"sequence-certain": strconv.FormatBool(props.Delivery.SequenceCertain),
			"task-revision":    strconv.FormatUint(props.TaskRevision, 10),
		},
		Class: panelClass(tokens),
	}, children...)
}

func taskIdentity(props Props) ui.Node {
	label, status := taskStatePresentation(props.TaskState)
	return html.Header(html.Props{Class: headerClass(props.Mode.Tokens())},
		html.Div(html.Props{},
			html.P(html.Props{Class: eyebrowClass(props.Mode.Tokens()), Text: "Backend task state"}),
			primitives.Badge(primitives.BadgeProps{Label: label, Status: status, Mode: props.Mode}),
		),
		html.Div(html.Props{},
			html.P(html.Props{Class: eyebrowClass(props.Mode.Tokens()), Text: "Current phase"}),
			primitives.Badge(primitives.BadgeProps{
				Label: humanize(string(props.Phase)), Status: phaseStatus(props.Phase), Mode: props.Mode,
			}),
		),
	)
}

func deliveryStatus(props Props) ui.Node {
	tone := design.StatusSuccess
	title := "Session live"
	if props.Delivery.State == DeliveryDisconnected {
		tone = design.StatusWarning
		title = "UI disconnected"
	} else if props.Delivery.State == DeliveryDegraded {
		tone = design.StatusWarning
		title = "Delivery degraded"
	}
	certainty := "Ordered sequence is current."
	if !props.Delivery.SequenceCertain {
		certainty = "Sequence certainty is unknown; unsafe mutations stay disabled."
	}
	timeline := "The timeline remains readable."
	if !props.Delivery.TimelineReadable {
		timeline = "Timeline data is not currently readable."
	}
	message := strings.TrimSpace(props.Delivery.Explanation)
	if message != "" {
		message += " "
	}
	message += certainty + " " + timeline
	return html.Div(html.Props{Data: map[string]string{
		"component": "delivery-status", "ui-connection": string(props.Delivery.State),
		"backend-task-state": string(props.TaskState),
		"sequence-certain":   strconv.FormatBool(props.Delivery.SequenceCertain),
	}}, primitives.InlineAlert(primitives.InlineAlertProps{
		Title: title, Message: message, Tone: tone, Mode: props.Mode,
	}))
}

func metrics(props Props) ui.Node {
	forecast := props.Forecast.Range
	items := []metric{
		{label: "Provider", value: known(props.Selection.Provider), kind: "provider"},
		{label: "Model", value: known(props.Selection.Model), kind: "model"},
		{label: "Effort", value: known(props.Selection.Effort), kind: "effort"},
		{label: "Estimated time (not a promise)", value: forecastTime(forecast), kind: "forecast-time"},
		{label: "Estimated tokens (not a promise)", value: forecastTokens(forecast), kind: "forecast-tokens"},
		{label: "Estimated cost (not a promise)", value: forecastCost(forecast), kind: "forecast-cost"},
		{label: "Actual tokens", value: tokenUsageText(props.Usage.Tokens), kind: "actual-tokens"},
		{label: "Actual cost", value: actualCostText(props.Usage.Cost), kind: "actual-cost"},
		{label: "Remaining hard budget", value: remainingBudgetText(props.Budget), kind: "remaining-budget"},
	}
	children := make([]ui.Node, 0, len(items)*2)
	for _, item := range items {
		children = append(children,
			html.Tag("dt", html.Props{Class: metricLabelClass(props.Mode.Tokens())}, html.Text(item.label)),
			html.Tag("dd", html.Props{
				Data: map[string]string{"metric": item.kind}, Class: metricValueClass(props.Mode.Tokens()),
			}, html.Text(item.value)),
		)
	}
	assumptions := strings.TrimSpace(props.Forecast.Assumptions)
	if assumptions == "" {
		assumptions = "No forecast assumptions were supplied."
	}
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Task forecast and actual usage"},
		Data:  map[string]string{"component": "task-metrics", "estimate-labeling": "explicit"},
		Class: sectionClass(props.Mode.Tokens()),
	},
		html.H2(html.Props{Class: headingClass(props.Mode.Tokens()), Text: "Forecast and actuals"}),
		html.Tag("dl", html.Props{Class: metricGridClass(props.Mode.Tokens())}, children...),
		html.P(html.Props{Class: secondaryTextClass(props.Mode.Tokens()), Text: "Forecast assumptions: " + assumptions}),
	)
}

type metric struct{ label, value, kind string }

func budgetPresentation(props Props) ui.Node {
	children := []ui.Node{
		html.H2(html.Props{Class: headingClass(props.Mode.Tokens()), Text: "Hard budget"}),
		html.P(html.Props{
			Data: map[string]string{"budget-limit": "exact"}, Class: metricValueClass(props.Mode.Tokens()),
			Text: hardBudgetText(props.Budget),
		}),
	}
	if props.Budget.WarningReached && !props.Budget.WarningAcknowledged {
		warning := "Budget warning threshold reached at " + moneyText(props.Budget.WarningThreshold) + "."
		children = append(children, html.Div(html.Props{
			Data: map[string]string{"component": "budget-warning", "non-spamming": "single"},
		},
			primitives.InlineAlert(primitives.InlineAlertProps{
				Title: "Budget warning", Message: warning, Tone: design.StatusWarning, Mode: props.Mode,
			}),
			optionalButton("budget-warning-dismiss", "Acknowledge budget warning", CommandState{}, props.Mode, props.OnBudgetWarningDismiss),
		))
	}
	if props.Budget.HardCapReached {
		settling := "Whether a provider request is still settling is unknown."
		if props.Budget.SettlingInFlightKnown && props.Budget.SettlingInFlight {
			settling = "An in-flight provider request is settling; no new paid work will start."
		} else if props.Budget.SettlingInFlightKnown {
			settling = "No provider request is settling."
		}
		checkpoint := "Unknown"
		if props.Budget.CheckpointKnown {
			checkpoint = known(props.Budget.CheckpointedState)
		}
		children = append(children, html.Div(html.Props{
			Role: "alert", Data: map[string]string{"component": "hard-cap-decision"},
			Class: warningSurfaceClass(props.Mode.Tokens()),
		},
			html.Strong(html.Props{Text: "Hard budget reached"}),
			html.P(html.Props{Text: settling + " Checkpointed state: " + checkpoint + "."}),
			html.P(html.Props{Text: "Raise the exact limit or stop. Finishing with current work remains unavailable until the coordinator exposes a typed command."}),
		))
	}
	if props.BudgetAdjust.Open {
		children = append(children, budgetConfirmation(props))
	} else if props.BudgetAdjust.Editing {
		children = append(children, budgetEditor(props))
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Hard budget status"},
		Data: map[string]string{
			"component": "budget-status", "warning": strconv.FormatBool(props.Budget.WarningReached),
			"hard-cap": strconv.FormatBool(props.Budget.HardCapReached),
		},
		Class: sectionClass(props.Mode.Tokens()),
	}, children...)
}

func commandControls(props Props) ui.Node {
	children := make([]ui.Node, 0, 4)
	if isPausable(props.TaskState) {
		children = append(children, commandButton("pause", "Pause", props.Controls.Pause, props.Mode, props.OnPause))
	}
	if isResumable(props.TaskState) {
		children = append(children, commandButton("resume", "Resume", props.Controls.Resume, props.Mode, props.OnResume))
	}
	if isActive(props.TaskState) {
		stopAction := props.OnStop
		if props.StopConfirm.Required && !props.StopConfirm.Open {
			stopAction = props.OnStopConfirm
		}
		children = append(children, commandButton("stop", "Stop", props.Controls.Stop, props.Mode, stopAction))
	}
	if isBudgetAdjustable(props.TaskState) {
		children = append(children, commandButton(
			"adjust-budget", "Adjust budget", props.Controls.AdjustBudget, props.Mode, props.OnBudgetAdjust,
		))
	}
	if props.StopConfirm.Open {
		children = append(children, stopConfirmation(props))
	}
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Task commands"},
		Data:  map[string]string{"component": "task-command-controls"},
		Class: sectionClass(props.Mode.Tokens()),
	},
		html.H2(html.Props{Class: headingClass(props.Mode.Tokens()), Text: "Task controls"}),
		html.Div(html.Props{Role: "group", Aria: map[string]string{"label": "Available task actions"}, Class: actionRowClass(props.Mode.Tokens())}, children...),
	)
}

func commandButton(id, label string, command CommandState, mode primitives.Mode, onClick func()) ui.Node {
	reasonID := "task-command-" + id + "-reason"
	reason := strings.TrimSpace(command.DisabledReason)
	if onClick == nil && reason == "" {
		reason = "This action is not currently available."
	}
	disabled := command.Busy || reason != "" || onClick == nil
	children := []ui.Node{primitives.Button(primitives.ButtonProps{
		ID: "task-command-" + id, Label: label, AccessibleLabel: label + " task",
		Disabled: disabled, Busy: command.Busy, DescribedBy: map[bool]string{true: reasonID}[reason != ""],
		Mode: mode, OnClick: onClick,
	})}
	if reason != "" {
		children = append(children, html.P(html.Props{
			ID: reasonID, Class: reasonClass(mode.Tokens()), Text: reason,
		}))
	}
	return html.Div(html.Props{Data: map[string]string{
		"component": "task-command", "command": id, "command-key": command.IdempotencyKey,
		"busy": strconv.FormatBool(command.Busy), "disabled-reason": reason,
	}, Class: commandClass(mode.Tokens())}, children...)
}

func optionalButton(id, label string, command CommandState, mode primitives.Mode, onClick func()) ui.Node {
	if onClick == nil {
		return nil
	}
	return commandButton(id, label, command, mode, onClick)
}

func stopConfirmation(props Props) ui.Node {
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Confirm task stop"},
		Data:  map[string]string{"component": "stop-confirmation", "required": "true"},
		Class: warningSurfaceClass(props.Mode.Tokens()),
	},
		html.P(html.Props{Text: "Stopping consequence: " + props.StopConfirm.Consequence}),
		primitives.Button(primitives.ButtonProps{
			Label: "Confirm stop", AccessibleLabel: "Confirm stop task", Primary: true,
			Disabled: props.Controls.Stop.Busy || props.OnStop == nil, Busy: props.Controls.Stop.Busy,
			Mode: props.Mode, OnClick: props.OnStop,
		}),
		primitives.Button(primitives.ButtonProps{
			Label: "Keep task running", Mode: props.Mode, Disabled: props.OnStopCancel == nil,
			OnClick: props.OnStopCancel,
		}),
	)
}

func budgetConfirmation(props Props) ui.Node {
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Confirm exact budget adjustment"},
		Data:  map[string]string{"component": "budget-adjustment-confirmation"},
		Class: warningSurfaceClass(props.Mode.Tokens()),
	},
		html.P(html.Props{Text: "Old hard budget: " + moneyText(props.BudgetAdjust.OldLimit)}),
		html.P(html.Props{Text: "New hard budget: " + moneyText(props.BudgetAdjust.NewLimit)}),
		primitives.Button(primitives.ButtonProps{
			Label: "Confirm exact budget change", Primary: true,
			Disabled: props.BudgetAdjust.Command.Busy || props.OnBudgetConfirm == nil,
			Busy:     props.BudgetAdjust.Command.Busy, Mode: props.Mode, OnClick: props.OnBudgetConfirm,
		}),
		primitives.Button(primitives.ButtonProps{
			Label: "Cancel budget change", Disabled: props.OnBudgetCancel == nil,
			Mode: props.Mode, OnClick: props.OnBudgetCancel,
		}),
	)
}

func budgetEditor(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	inputProps := html.PropsOf(html.OnInput(func(event ui.InputEvent) {
		if props.OnBudgetDraftChange != nil {
			props.OnBudgetDraftChange(event.GetValue())
		}
	}))
	inputProps.ID = "task-budget-minor-units"
	inputProps.Type = "number"
	inputProps.Value = props.BudgetAdjust.DraftMinorUnits
	inputProps.Min = "0"
	inputProps.Step = "1"
	inputProps.Disabled = props.BudgetAdjust.Command.Busy || props.OnBudgetDraftChange == nil
	inputProps.Aria = map[string]string{
		"label":   "New hard budget in exact " + string(props.Budget.HardLimit.Currency) + " minor units",
		"invalid": strconv.FormatBool(strings.TrimSpace(props.BudgetAdjust.InvalidMessage) != ""),
	}
	if strings.TrimSpace(props.BudgetAdjust.InvalidMessage) != "" {
		inputProps.Aria["errormessage"] = "task-budget-minor-units-error"
	}
	inputProps.Class = css.New(
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	).String()
	children := []ui.Node{
		html.Label(html.Props{For: inputProps.ID, Text: "New hard budget (" + string(props.Budget.HardLimit.Currency) + " minor units)"}),
		html.Input(inputProps),
	}
	if message := strings.TrimSpace(props.BudgetAdjust.InvalidMessage); message != "" {
		children = append(children, html.P(html.Props{ID: "task-budget-minor-units-error", Role: "alert", Text: message}))
	}
	children = append(children,
		primitives.Button(primitives.ButtonProps{
			Label: "Review exact budget change", Primary: true,
			Disabled: props.OnBudgetPreview == nil || props.BudgetAdjust.Command.Busy,
			Mode:     props.Mode, OnClick: props.OnBudgetPreview,
		}),
		primitives.Button(primitives.ButtonProps{
			Label: "Cancel budget change", Disabled: props.OnBudgetCancel == nil || props.BudgetAdjust.Command.Busy,
			Mode: props.Mode, OnClick: props.OnBudgetCancel,
		}),
	)
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Enter exact budget adjustment"},
		Data:  map[string]string{"component": "budget-adjustment-editor"},
		Class: warningSurfaceClass(tokens),
	}, children...)
}

func RecoveryPanel(props Props) ui.Node {
	recovery := props.Recovery
	children := []ui.Node{
		html.Div(html.Props{Class: headerClass(props.Mode.Tokens())},
			html.Div(html.Props{},
				html.P(html.Props{Class: eyebrowClass(props.Mode.Tokens()), Text: "Recovery required"}),
				html.H2(html.Props{Class: headingClass(props.Mode.Tokens()), Text: "Safest known next step"}),
			),
			primitives.Badge(primitives.BadgeProps{Label: "Needs recovery", Status: design.StatusWarning, Mode: props.Mode}),
		),
		html.P(html.Props{Text: recovery.SafestRecommendation}),
		definitionList(props.Mode,
			metric{label: "Known task state", value: recovery.KnownState},
			metric{label: "Last valid checkpoint", value: checkpointText(recovery)},
			metric{label: "Repository/worktree divergence", value: known(recovery.DivergenceSummary)},
			metric{label: "What remains ambiguous", value: known(recovery.Ambiguity)},
		),
	}
	if recovery.ExternalOutcomeAmbiguous {
		children = append(children, html.Div(html.Props{
			Role: "alert", Data: map[string]string{"component": "ambiguous-external-outcome"},
			Class: warningSurfaceClass(props.Mode.Tokens()),
		},
			html.Strong(html.Props{Text: "External action outcome is ambiguous"}),
			html.P(html.Props{Text: "CodeFlux will not label an unsafe auto-repeat as Retry. Reconcile the external result first."}),
		))
	}
	actions := make([]ui.Node, 0, 3)
	if recovery.SafeResumeVerified {
		actions = append(actions, commandButton("safe-resume", "Safe resume", recovery.SafeResume, props.Mode, props.OnSafeResume))
	}
	if recovery.ReconcileRequired {
		actions = append(actions, commandButton("reconcile", "Reconcile user edits", recovery.Reconcile, props.Mode, props.OnReconcile))
	}
	if recovery.PatchPreservable {
		actions = append(actions, commandButton("preserve-patch", "Preserve patch", recovery.PreservePatch, props.Mode, props.OnPreservePatch))
	}
	children = append(children, html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Recovery actions, safest first"},
		Data:  map[string]string{"component": "recovery-actions", "order": "safest-first"},
		Class: actionRowClass(props.Mode.Tokens()),
	}, actions...))
	if len(recovery.Details) > 0 {
		details := make([]ui.Node, 0, len(recovery.Details))
		for _, detail := range recovery.Details {
			detail := detail
			disabled := props.OnOpenDetail == nil || strings.TrimSpace(detail.DisabledReason) != ""
			details = append(details, primitives.Button(primitives.ButtonProps{
				Label: detail.Label, AccessibleLabel: "Open recovery " + string(detail.Kind) + " " + detail.Label,
				Disabled: disabled, Mode: props.Mode,
				OnClick: func() {
					if props.OnOpenDetail != nil && !disabled {
						props.OnOpenDetail(detail)
					}
				},
			}))
		}
		children = append(children, html.Nav(html.Props{
			Aria:  map[string]string{"label": "Recovery events and files"},
			Data:  map[string]string{"component": "recovery-details"},
			Class: actionRowClass(props.Mode.Tokens()),
		}, details...))
	}
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Recovery required"},
		Data:  map[string]string{"component": "recovery-panel", "tone": "calm", "known-state-first": "true"},
		Class: recoveryClass(props.Mode.Tokens()),
	}, children...)
}

func reviewStaleness(props Props) ui.Node {
	reasons := props.Review.Reasons
	if len(reasons) == 0 {
		reasons = []string{"A review-bound revision changed."}
	}
	items := make([]ui.Node, 0, len(reasons))
	for _, reason := range reasons {
		items = append(items, html.Li(html.Props{Text: reason}))
	}
	return html.Div(html.Props{
		Role: "status", Data: map[string]string{"component": "review-staleness", "stale": "true"},
		Class: warningSurfaceClass(props.Mode.Tokens()),
	},
		html.Strong(html.Props{Text: "Review is stale"}),
		html.P(html.Props{Text: "Acceptance remains disabled until these revisions are reviewed:"}),
		html.Ul(html.Props{}, items...),
	)
}

func definitionList(mode primitives.Mode, values ...metric) ui.Node {
	children := make([]ui.Node, 0, len(values)*2)
	for _, value := range values {
		children = append(children,
			html.Tag("dt", html.Props{Class: metricLabelClass(mode.Tokens())}, html.Text(value.label)),
			html.Tag("dd", html.Props{Class: metricValueClass(mode.Tokens())}, html.Text(known(value.value))),
		)
	}
	return html.Tag("dl", html.Props{Class: metricGridClass(mode.Tokens())}, children...)
}

func taskStatePresentation(state domain.TaskState) (string, design.Status) {
	switch state {
	case domain.TaskStateRunning, domain.TaskStateValidating:
		return humanize(string(state)), design.StatusActive
	case domain.TaskStateCompleted:
		return "Completed", design.StatusSuccess
	case domain.TaskStateFailed:
		return "Failed", design.StatusFailure
	case domain.TaskStateRecoveryRequired:
		return "Needs recovery", design.StatusWarning
	case domain.TaskStateAwaitingAuthority, domain.TaskStateAwaitingPlanApproval,
		domain.TaskStateAwaitingReview, domain.TaskStatePaused:
		return humanize(string(state)), design.StatusPending
	case domain.TaskStateCancelled, domain.TaskStateRolledBack:
		return humanize(string(state)), design.StatusNeutral
	default:
		return humanize(string(state)), design.StatusPlan
	}
}

func phaseStatus(phase Phase) design.Status {
	switch phase {
	case PhaseEditing, PhaseValidating, PhaseRepairing:
		return design.StatusActive
	case PhaseReviewing:
		return design.StatusPending
	default:
		return design.StatusPlan
	}
}

func actualCostText(cost ActualCostView) string {
	if !cost.Known {
		reason := strings.TrimSpace(cost.UnknownReason)
		if reason == "" {
			reason = "pricing or provider usage has not arrived"
		}
		return "Unknown — " + reason
	}
	return moneyText(cost.Value) + " actual · pricing snapshot " + cost.PricingSnapshot
}

func forecastTime(value domain.ForecastRange) string {
	if !value.LatencyKnown {
		return "Unknown"
	}
	return "Estimated P50 " + durationText(value.LatencyP50Millis) + " · P90 " + durationText(value.LatencyP90Millis)
}

func forecastTokens(value domain.ForecastRange) string {
	if !value.TokensKnown {
		return "Unknown"
	}
	return "Estimated P50 " + strconv.FormatUint(uint64(value.TokensP50), 10) +
		" · P90 " + strconv.FormatUint(uint64(value.TokensP90), 10)
}

func forecastCost(value domain.ForecastRange) string {
	if !value.CostKnown {
		return "Unknown — no price estimate"
	}
	return "Estimated P50 " + moneyText(value.CostP50) + " · P90 " + moneyText(value.CostP90)
}

func remainingBudgetText(value BudgetView) string {
	if !value.HardLimitKnown {
		return "Unknown — no hard budget was supplied"
	}
	if !value.RemainingKnown {
		return "Unknown — actual priced spend is incomplete"
	}
	return moneyText(value.Remaining) + " remaining of " + moneyText(value.HardLimit)
}

func hardBudgetText(value BudgetView) string {
	if !value.HardLimitKnown {
		return "Unknown — no hard budget was supplied"
	}
	return moneyText(value.HardLimit)
}

func moneyText(value domain.Money) string {
	return string(value.Currency) + " " + strconv.FormatInt(value.MinorUnits, 10) + " minor units"
}

func tokenUsageText(value domain.TokenUsage) string {
	total, knownValue, _ := value.Total()
	if !knownValue {
		return "Unknown — provider usage has not arrived"
	}
	parts := []string{
		strconv.FormatUint(uint64(total), 10) + " total",
		"input " + strconv.FormatUint(uint64(value.Input), 10),
		"cached input " + strconv.FormatUint(uint64(value.CachedInput), 10),
		"cache write " + strconv.FormatUint(uint64(value.CacheWrite), 10),
		"output " + strconv.FormatUint(uint64(value.Output), 10),
		"reasoning " + strconv.FormatUint(uint64(value.Reasoning), 10),
	}
	keys := make([]string, 0, len(value.ProviderSpecific))
	for key := range value.ProviderSpecific {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+" "+strconv.FormatUint(uint64(value.ProviderSpecific[key]), 10))
	}
	return strings.Join(parts, " · ") + " exact provider-reported tokens"
}

func durationText(value domain.Milliseconds) string {
	milliseconds := int64(value)
	seconds := milliseconds / 1000
	remainder := milliseconds % 1000
	if remainder == 0 {
		return strconv.FormatInt(seconds, 10) + " s"
	}
	return fmt.Sprintf("%d s %d ms", seconds, remainder)
}

func checkpointText(recovery RecoveryView) string {
	when := "Unknown time"
	if !recovery.LastCheckpointAt.IsZero() {
		when = recovery.LastCheckpointAt.UTC().Format(time.RFC3339)
	}
	return when + " · plan step " + known(recovery.LastCheckpointPlanStep)
}

func known(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return strings.TrimSpace(value)
}

func humanize(value string) string {
	words := strings.Fields(strings.ReplaceAll(value, "-", " "))
	for index, word := range words {
		if word != "" {
			words[index] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

func panelClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
		css.Padding(css.Px(tokens.Spacing.MD)), css.MinWidth(css.Zero),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
	).String()
}

func sectionClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
		css.Padding(css.Px(tokens.Spacing.MD)), css.MinWidth(css.Zero),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	).String()
}

func headerClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, u.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.MD))).String()
}

func headingClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)), css.FontWeight.Semibold,
	).String()
}

func eyebrowClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.TextTransform.Uppercase,
	).String()
}

func metricGridClass(tokens design.Tokens) string {
	return css.New(
		u.Grid, css.GridCols(css.RepeatFit(css.MinMax(css.TrackLen(css.Rem(12)), css.Fr(1)))),
		css.Gap(css.Px(tokens.Spacing.SM)), css.Margin(css.Zero), css.MinWidth(css.Zero),
	).String()
}

func metricLabelClass(tokens design.Tokens) string {
	return css.New(css.FontWeight.Semibold, css.TextColor(css.Hex(string(tokens.Colors.TextMuted)))).String()
}

func metricValueClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero), css.MinWidth(css.Zero), css.OverflowWrap.Anywhere,
		css.FontVariantNumeric.TabularNums, css.Font(css.FontStack(tokens.Fonts.UI)),
	).String()
}

func secondaryTextClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextSecondary)))).String()
}

func actionRowClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexWrap.Wrap, u.ItemsStart, css.Gap(css.Px(tokens.Spacing.SM))).String()
}

func commandClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.MinWidth(css.Zero)).String()
}

func reasonClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero), css.MaxWidth(css.Ch(34)),
		css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
	).String()
}

func warningSurfaceClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
		css.Padding(css.Px(tokens.Spacing.MD)),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.Warning))),
		css.BorderLeft(css.Px(tokens.Geometry.BorderStrongWidth), css.Hex(string(tokens.Colors.Warning))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	).String()
}

func recoveryClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
		css.Padding(css.Px(tokens.Spacing.LG)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.Warning))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
	).String()
}
