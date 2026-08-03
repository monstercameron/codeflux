package timelineview

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/pipelineledger"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/readout"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderBody(props Props) ui.Node {
	switch props.Card.Kind {
	case timelinecard.KindMessage:
		return renderMessage(*props.Card.Message, props)
	case timelinecard.KindThreadState:
		return renderThreadState(*props.Card.ThreadState)
	case timelinecard.KindRequirement:
		return renderRequirement(*props.Card.Requirement, props.Mode)
	case timelinecard.KindForecast:
		return renderForecast(*props.Card.Forecast, props.Mode)
	case timelinecard.KindPlan:
		return renderPlan(*props.Card.Plan, props)
	case timelinecard.KindPlanRevision:
		return renderPlanRevision(*props.Card.PlanRevision, props)
	case timelinecard.KindContext:
		return renderContext(*props.Card.Context, props.Mode)
	case timelinecard.KindTool:
		return renderTool(*props.Card.Tool, props)
	case timelinecard.KindApproval:
		return renderApproval(*props.Card.Approval, props)
	case timelinecard.KindCheckpoint:
		return renderCheckpoint(*props.Card.Checkpoint, props.Mode)
	case timelinecard.KindValidation:
		return renderValidation(*props.Card.Validation, props)
	case timelinecard.KindDiff:
		return renderDiff(*props.Card.Diff, props)
	case timelinecard.KindCostBudget:
		return renderCost(*props.Card.CostBudget, props.Mode)
	case timelinecard.KindRecovery:
		return renderRecovery(*props.Card.Recovery, props)
	case timelinecard.KindError:
		return renderError(*props.Card.Error, props)
	case timelinecard.KindCompletion:
		return renderCompletion(*props.Card.Completion, props.Mode)
	case timelinecard.KindTaskState:
		return renderTaskState(*props.Card.TaskState, props.Mode)
	case timelinecard.KindUsage:
		return renderUsage(*props.Card.Usage, props.Mode)
	case timelinecard.KindGraphChange:
		return renderGraphChange(*props.Card.GraphChange, props.Mode)
	case timelinecard.KindUnknown:
		return renderUnknown(*props.Card.Unknown, props)
	default:
		return html.P(html.Props{}, html.Text("No safe presentation is available."))
	}
}

func renderThreadState(state timelinecard.ThreadState) ui.Node {
	detail := state.Title
	if state.Action == "renamed" {
		detail = state.PreviousTitle + " → " + state.Title
	}
	if state.Action == "archived" {
		if state.Archived {
			detail = "Thread archived"
		} else {
			detail = "Thread restored"
		}
	}
	return html.P(html.Props{}, html.Text(detail))
}

func renderMessage(message timelinecard.Message, props Props) ui.Node {
	document, err := timelinecard.ParseMarkdown(message.Body)
	content := ui.Node(html.P(html.Props{}, html.Text(message.Body)))
	if err == nil {
		content = ui.CreateElement(MarkdownView, MarkdownProps{Document: document, Mode: props.Mode, OnCopy: props.Actions.OnCopy})
	}
	children := []ui.Node{content}
	if message.Status == timelinecard.MessageProvisional {
		children = append(children, html.P(html.Props{
			Role: "status", Data: map[string]string{"component": "message-delivery-status"},
		}, html.Text("Response is still in progress and is not yet durable.")))
	} else if message.Status == timelinecard.MessageInterrupted {
		children = append(children, html.P(html.Props{
			Role: "status", Data: map[string]string{"component": "message-delivery-status"},
		}, html.Text("Response was interrupted before a durable final message arrived.")))
	}
	if len(message.NodeIDs) > 0 {
		chips := make([]ui.Node, 0, len(message.NodeIDs))
		for _, nodeID := range message.NodeIDs {
			identity := nodeID
			chips = append(chips, primitives.Button(primitives.ButtonProps{
				Label: "Graph node " + identity, Mode: props.Mode,
				Disabled: props.Actions.OnSelectNode == nil,
				OnClick: func() {
					if props.Actions.OnSelectNode != nil {
						props.Actions.OnSelectNode(identity)
					}
				},
			}))
		}
		children = append(children, html.Div(html.Props{
			Role: "group", Aria: map[string]string{"label": "Related graph nodes"},
			Class: actionRowClass(props.Mode),
		}, chips...))
		if strings.TrimSpace(message.GraphRevision) != "" && !message.GraphRevisionCurrent {
			children = append(children, html.P(html.Props{
				Role: "status",
				Data: map[string]string{
					"component":      "stale-graph-revision",
					"graph-revision": message.GraphRevision,
				},
			}, html.Text(
				"These node links refer to graph revision "+message.GraphRevision+
					", which is no longer current. Activating a node preserves the historical revision context.",
			)))
		}
	}
	if props.Actions.OnCopy != nil {
		// The copy action is drawn by the card header. Kept here it added a
		// forty-four pixel row under every message in the transcript.
		_ = message.Body
	}
	role := messagePresentationRole(message.Role)
	// A durable, agent-authored message is the run's own final word. If the
	// caller has this attempt's skip-audit summary in hand, show it here too
	// -- so a person reading the timeline sees what the run did not do
	// without opening the pipeline ledger separately (PIPE-044). Nothing
	// renders when the caller has not supplied one or the audit could not be
	// computed: this is additive, never a claim manufactured for a message
	// that never carried one.
	if role == "agent" && message.Status == timelinecard.MessageComplete && props.PipelineSkipSummary != nil {
		if caveat := pipelineledger.RunCompletionCaveat(*props.PipelineSkipSummary, props.Mode); caveat != nil {
			children = append(children, caveat)
		}
	}
	return html.Div(html.Props{
		Aria: map[string]string{"label": humanize(role) + " message content"},
		Data: map[string]string{
			"component": "message-card-body", "message-status": string(message.Status), "message-role": role,
		},
		Class: messageBodyClass(props.Mode, role),
	}, children...)
}

func renderRequirement(value timelinecard.Requirement, mode primitives.Mode) ui.Node {
	return stack(mode,
		summaryParagraph(value.Goal, mode),
		secondaryDetails("Constraints and interpretation details", mode,
			namedList("Constraints", value.Constraints, mode),
			namedList("Assumptions", value.Assumptions, mode),
			namedList("Unresolved ambiguity", value.UnresolvedAmbiguities, mode),
			namedList("Explicit files and symbols", value.ExplicitIdentities, mode),
			namedList("Risk cues", value.RiskCues, mode),
		),
	)
}

func renderForecast(value timelinecard.Forecast, mode primitives.Mode) ui.Node {
	rangeValue := value.Range
	latency := "Unknown"
	if rangeValue.LatencyKnown {
		latency = fmt.Sprintf("P50 %d ms; P90 %d ms", rangeValue.LatencyP50Millis, rangeValue.LatencyP90Millis)
	}
	tokens := "Unknown"
	if rangeValue.TokensKnown {
		tokens = fmt.Sprintf("P50 %d; P90 %d", rangeValue.TokensP50, rangeValue.TokensP90)
	}
	cost := "Unknown price"
	if rangeValue.CostKnown {
		cost = "P50 " + moneyText(rangeValue.CostP50) + "; P90 " + moneyText(rangeValue.CostP90)
	}
	return stack(mode,
		forecastMetricStrip(mode,
			definition{"Latency", latency}, definition{"Tokens", tokens}, definition{"Cost", cost},
		),
		forecastContextRow(mode,
			definition{"Model", known(value.Model)}, definition{"Effort", known(value.Effort)},
			definition{"Validation", known(value.ValidationProfile)},
		),
		secondaryDetails("Uncertainty and estimate notes", mode,
			namedList("Uncertainty reasons", value.UncertaintyReasons, mode),
			html.P(html.Props{Class: bodyTextClass(mode)}, html.Text("These ranges are estimates, not promises.")),
		),
	)
}

func renderPlan(value timelinecard.Plan, props Props) ui.Node {
	mode := props.Mode
	steps := make([]ui.Node, 0, len(value.Steps))
	for _, step := range value.Steps {
		steps = append(steps, html.Li(html.Props{
			Data: map[string]string{"step-status": string(step.Status)},
		}, html.Text(fmt.Sprintf("%d. %s — %s", step.Ordinal, step.Title, humanize(string(step.Status))))))
	}
	children := []ui.Node{
		summaryParagraph(value.Summary, mode),
		definitionList(mode,
			definition{"Revision", strconv.FormatUint(value.Revision, 10)},
			definition{"Approval", map[bool]string{true: "Pending", false: "Not pending"}[value.ApprovalPending]},
		),
		nodeList("Ordered steps", steps, mode),
		secondaryDetails("Plan scope, checks, and criteria", mode,
			namedList("Scope", value.Scope, mode),
			namedList("Expected files", value.ExpectedFiles, mode),
			namedList("Planned checks", value.PlannedChecks, mode),
			namedList("Risks", value.Risks, mode),
			namedList("Assumptions", value.Assumptions, mode),
			namedList("Authority needs", value.AuthorityNeeds, mode),
			namedList("Completion criteria", value.CompletionCriteria, mode),
			namedList("Prior revisions", uint64List(value.PriorRevisions), mode),
		),
	}
	actions := make([]ui.Node, 0, 3)
	if value.ApprovalPending && !value.Superseded {
		revision := value.Revision
		command := ActionCommandState{}
		if props.Actions.ApprovePlanCommand != nil {
			command = props.Actions.ApprovePlanCommand(revision)
		}
		var invoke func()
		if props.Actions.OnApprovePlan != nil {
			invoke = func() { props.Actions.OnApprovePlan(revision) }
		}
		actions = append(actions, timelineCommandButton(
			"timeline-plan-approve", "Approve plan", command, mode, invoke,
		))
	}
	if !value.Superseded {
		revision := value.Revision
		command := ActionCommandState{}
		if props.Actions.PlanChangeCommand != nil {
			command = props.Actions.PlanChangeCommand(revision)
		}
		var invoke func()
		if props.Actions.OnRequestPlanChange != nil {
			invoke = func() { props.Actions.OnRequestPlanChange(revision) }
		}
		actions = append(actions, timelineCommandButton(
			"timeline-plan-request-change", "Request plan change", command, mode, invoke,
		))
	}
	if len(value.PriorRevisions) > 0 && props.Actions.OnComparePlanRevision != nil {
		revision := value.Revision
		actions = append(actions, primitives.Button(primitives.ButtonProps{
			Label: "Compare plan revision", Mode: mode, OnClick: func() { props.Actions.OnComparePlanRevision(revision) },
		}))
	}
	if len(actions) > 0 {
		children = append(children, html.Div(html.Props{
			Role: "group", Aria: map[string]string{"label": "Plan actions"}, Class: actionRowClass(mode),
		}, actions...))
	}
	return stack(mode, children...)
}

func renderPlanRevision(value timelinecard.PlanRevision, props Props) ui.Node {
	mode := props.Mode
	children := []ui.Node{
		paragraph("Summary", value.Summary, mode),
		definitionList(mode,
			definition{"Previous revision", uintText(value.PreviousRevision)},
			definition{"Current revision", uintText(value.CurrentRevision)},
			definition{"Approval", map[bool]string{true: "Reset; review required", false: "Unchanged"}[value.ApprovalReset]},
		),
		namedList("Added", value.Added, mode),
		namedList("Removed", value.Removed, mode),
		namedList("Prior revision history", uint64List(value.PriorHistory), mode),
	}
	if props.Actions.OnComparePlanRevision != nil {
		revision := value.CurrentRevision
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: "Compare plan revision", Mode: mode,
			OnClick: func() { props.Actions.OnComparePlanRevision(revision) },
		}))
	}
	return stack(mode, children...)
}

func renderContext(value timelinecard.ContextSelection, mode primitives.Mode) ui.Node {
	items := make([]ui.Node, 0, len(value.Included))
	for _, item := range value.Included {
		items = append(items, html.Li(html.Props{},
			html.Strong(html.Props{}, html.Text(item.Identity)),
			html.Text(" — "+known(item.Reason)+"; revision "+known(item.Revision)),
		))
	}
	return stack(mode,
		definitionList(mode,
			definition{"Repository revision", known(value.RepositoryRevision)},
			definition{"Context budget", fmt.Sprintf("%d of %d", value.BudgetUsed, value.BudgetLimit)},
		),
		nodeList("Selected identities and reasons", items, mode),
		namedList("Exclusions", value.Exclusions, mode),
	)
}

func renderTool(value timelinecard.ToolActivity, props Props) ui.Node {
	return stack(props.Mode,
		summaryParagraph(value.Summary, props.Mode),
		secondaryDetails("Execution details", props.Mode,
			definitionList(props.Mode,
				definition{"Tool", known(value.Tool)}, definition{"Purpose", known(value.Purpose)},
				definition{"Scope", known(value.Scope)}, definition{"State", humanize(value.State)},
				definition{"Duration", durationText(value.Duration)}, definition{"Exit status", known(value.ExitStatus)},
			),
		),
		ui.CreateElement(OutputDisclosure, OutputProps{Output: value.Output, Mode: props.Mode, OnCopy: props.Actions.OnCopy}),
	)
}

func renderApproval(value timelinecard.Approval, props Props) ui.Node {
	command := ApprovalCommandState{}
	if props.Actions.ApprovalCommand != nil {
		command = props.Actions.ApprovalCommand(value.ID)
	}
	children := []ui.Node{
		definitionList(props.Mode,
			definition{"Action", known(value.Action)}, definition{"Safe arguments", known(value.SafeArguments)},
			definition{"Scope", known(value.Scope)}, definition{"Reason", known(value.Reason)},
			definition{"Consequences", known(value.Consequences)}, definition{"State", humanize(value.State)},
			definition{"Expires", timeText(value.ExpiresAt)},
		),
	}
	if value.ActionsAvailable() {
		buttons := make([]ui.Node, 0, 3)
		for _, action := range value.AvailableActions() {
			selected := action
			actionCommand := command
			if props.Actions.ApprovalActionCommand != nil {
				actionCommand = props.Actions.ApprovalActionCommand(value.ID, selected)
			}
			var invoke func()
			if props.Actions.OnApproval != nil {
				invoke = func() { props.Actions.OnApproval(value.ID, selected) }
			}
			buttons = append(buttons, timelineCommandButton(
				ApprovalFocusTargetID(value.ID)+"-"+string(selected),
				approvalActionLabel(selected), actionCommand, props.Mode, invoke,
			))
		}
		children = append(children, html.Div(html.Props{
			Role: "group", Aria: map[string]string{"label": "Resolve approval"}, Class: actionRowClass(props.Mode),
		}, buttons...))
	} else {
		children = append(children, definitionList(props.Mode,
			definition{"Resolved by", known(value.ResolvedBy)}, definition{"Resolved at", timeText(value.ResolvedAt)},
		))
	}
	return html.Div(html.Props{
		ID: ApprovalFocusTargetID(value.ID), TabIndex: -1,
		Aria: map[string]string{"busy": strconv.FormatBool(command.Busy)},
		Data: map[string]string{
			"component": "approval-card-interaction", "approval-id": value.ID,
			"approval-state": value.State,
			"command-state":  map[bool]string{true: "busy", false: "ready"}[command.Busy],
			"command-key":    command.IdempotencyKey, "transport-mode": command.TransportMode,
			"focus-retained": "card-after-resolution",
		},
	}, stack(props.Mode, children...))
}

func timelineCommandButton(
	id, label string,
	command ActionCommandState,
	mode primitives.Mode,
	onClick func(),
) ui.Node {
	reason := strings.TrimSpace(command.DisabledReason)
	if onClick == nil && reason == "" {
		reason = "This command is not currently available."
	}
	reasonID := id + "-reason"
	children := []ui.Node{primitives.Button(primitives.ButtonProps{
		ID: id, Label: label, Busy: command.Busy,
		Disabled:    command.Busy || reason != "" || onClick == nil,
		DescribedBy: map[bool]string{true: reasonID}[reason != ""],
		Mode:        mode, OnClick: onClick,
	})}
	if reason != "" {
		children = append(children, html.P(html.Props{ID: reasonID, Text: reason}))
	}
	return html.Div(html.Props{Data: map[string]string{
		"component": "timeline-command", "command-key": command.IdempotencyKey,
		"transport-mode": command.TransportMode, "disabled-reason": reason,
	}}, children...)
}

func renderCheckpoint(value timelinecard.Checkpoint, mode primitives.Mode) ui.Node {
	return definitionList(mode,
		definition{"Checkpoint", known(value.ID)}, definition{"Task revision", uintText(value.TaskRevision)},
		definition{"Plan step", known(value.PlanStep)}, definition{"Diff summary", known(value.DiffSummary)},
		definition{"Reason", known(value.Reason)},
		definition{"Restore", map[bool]string{true: "Available when requested", false: "Unavailable in current state"}[value.RestoreSafe]},
	)
}

func renderValidation(value timelinecard.Validation, props Props) ui.Node {
	return stack(props.Mode,
		summaryParagraph(value.Summary, props.Mode),
		secondaryDetails("Validation binding and baseline", props.Mode,
			definitionList(props.Mode,
				definition{"Validation", known(value.ID)}, definition{"Status", humanize(string(value.Status))},
				definition{"Requirement", map[bool]string{true: "Required", false: "Advisory"}[value.Required]},
				definition{"Revision binding", known(value.RevisionBinding)}, definition{"Baseline", known(value.Baseline)},
			),
		),
		ui.CreateElement(OutputDisclosure, OutputProps{Output: value.Output, Mode: props.Mode, OnCopy: props.Actions.OnCopy}),
	)
}

func renderDiff(value timelinecard.DiffSummary, props Props) ui.Node {
	mode := props.Mode
	items := make([]ui.Node, 0, len(value.Files))
	for _, file := range value.Files {
		items = append(items, html.Li(html.Props{}, html.Text(fmt.Sprintf(
			"%s — %s; +%d −%d", file.Path, known(file.Category), file.Added, file.Deleted,
		))))
	}
	children := []ui.Node{
		definitionList(mode,
			definition{"Files", strconv.Itoa(len(value.Files))},
			definition{"Additions", strconv.FormatUint(value.Additions, 10)},
			definition{"Deletions", strconv.FormatUint(value.Deletions, 10)},
			definition{"Review identity", known(value.ReviewID)},
		),
		nodeList("Changed files", items, mode),
		namedList("Scope warnings", value.Warnings, mode),
	}
	if strings.TrimSpace(value.ReviewID) != "" && props.Actions.OnOpenReview != nil {
		reviewID := value.ReviewID
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: "Open review", Mode: mode,
			OnClick: func() { props.Actions.OnOpenReview(reviewID) },
		}))
	}
	return stack(mode, children...)
}

func renderCost(value timelinecard.CostBudget, mode primitives.Mode) ui.Node {
	actual := "Unknown price"
	if value.Known {
		actual = moneyText(value.Actual)
	}
	return definitionList(mode,
		definition{"Reason", known(value.Reason)}, definition{"Actual", actual},
		definition{"Reserved", moneyOrUnknown(value.Reserved)}, definition{"Hard limit", moneyOrUnknown(value.HardLimit)},
		definition{"Threshold", known(value.Threshold)},
	)
}

func renderRecovery(value timelinecard.Recovery, props Props) ui.Node {
	children := []ui.Node{
		definitionList(props.Mode,
			definition{"Last verified checkpoint", known(value.CheckpointID)},
			definition{"Why automatic resume is unavailable", known(value.Reason)},
		),
		namedList("Safely known", value.Known, props.Mode),
		namedList("Ambiguous", value.Ambiguous, props.Mode),
	}
	if len(value.Choices) > 0 {
		choices := make([]ui.Node, 0, len(value.Choices))
		for _, choice := range value.Choices {
			selected := choice
			label := selected.Label
			if selected.Destructive {
				label += " (destructive)"
			}
			choices = append(choices, html.Div(html.Props{},
				primitives.Button(primitives.ButtonProps{
					Label: label, Mode: props.Mode,
					Disabled: props.Actions.OnRecoveryChoice == nil,
					OnClick: func() {
						if props.Actions.OnRecoveryChoice != nil {
							props.Actions.OnRecoveryChoice(selected.Kind)
						}
					},
				}),
				html.P(html.Props{}, html.Text(selected.Explanation)),
			))
		}
		children = append(children, html.Div(html.Props{
			Role: "group", Aria: map[string]string{"label": "Recovery choices"},
		}, choices...))
	}
	return stack(props.Mode, children...)
}

func renderError(value timelinecard.Error, props Props) ui.Node {
	children := []ui.Node{
		definitionList(props.Mode,
			definition{"Stable code", known(value.Code)}, definition{"Affected action", known(value.AffectedAction)},
			definition{"Retryability", map[bool]string{true: "Retryable", false: "Not retryable"}[value.Retryable]},
		),
		paragraph("Safe explanation", value.Message, props.Mode),
		namedList("Next steps", value.NextSteps, props.Mode),
	}
	if value.Retryable && props.Actions.OnRetry != nil {
		children = append(children, primitives.Button(primitives.ButtonProps{Label: "Retry affected action", Mode: props.Mode, OnClick: props.Actions.OnRetry}))
	}
	return stack(props.Mode, children...)
}

func renderCompletion(value timelinecard.Completion, mode primitives.Mode) ui.Node {
	validations := make([]string, 0, len(value.Validation))
	for _, validation := range value.Validation {
		validations = append(validations, known(validation.ID)+": "+humanize(string(validation.Status)))
	}
	cost := "Unknown"
	if value.Cost != nil {
		cost = moneyText(*value.Cost)
	}
	return stack(mode,
		definitionList(mode, definition{"Outcome", humanize(string(value.Status))}, definition{"Cost", cost}),
		namedList("Files", value.Files, mode), namedList("Validation", validations, mode),
		namedList("Evidence", value.Evidence, mode), namedList("Assumptions", value.Assumptions, mode),
		namedList("Limitations", value.Limitations, mode), namedList("Memory influence", value.MemoryInfluence, mode),
	)
}

func renderTaskState(value timelinecard.TaskTransition, mode primitives.Mode) ui.Node {
	return definitionList(mode,
		definition{"From", humanize(value.From)}, definition{"To", humanize(value.To)},
		definition{"Approval state", humanize(value.Approval)},
	)
}

func renderUsage(value timelinecard.Usage, mode primitives.Mode) ui.Node {
	total, knownTotal, err := value.Tokens.Total()
	totalText := "Unknown"
	if err == nil && knownTotal {
		totalText = fmt.Sprintf("%d", total)
	}
	return definitionList(mode,
		definition{"Input", fmt.Sprintf("%d", value.Tokens.Input)},
		definition{"Cached input", fmt.Sprintf("%d", value.Tokens.CachedInput)},
		definition{"Output", fmt.Sprintf("%d", value.Tokens.Output)},
		definition{"Reasoning", fmt.Sprintf("%d", value.Tokens.Reasoning)},
		definition{"Total", totalText},
	)
}

func renderGraphChange(value timelinecard.GraphChange, mode primitives.Mode) ui.Node {
	return definitionList(mode,
		definition{"Graph revision", known(value.RevisionID)},
		definition{"Change", map[bool]string{true: "Patch", false: "Snapshot"}[value.Patch]},
		definition{"Encoded bytes", strconv.Itoa(value.ByteCount)},
	)
}

func renderUnknown(value timelinecard.Unknown, props Props) ui.Node {
	children := []ui.Node{
		definitionList(props.Mode,
			definition{"Event kind", known(value.EventKind)},
			definition{"Sequence", strconv.FormatUint(value.Sequence, 10)},
			definition{"Time", timeText(value.OccurredAt)},
		),
		paragraph("Safe details", value.SafeDetails, props.Mode),
	}
	if props.Actions.OnDiagnostics != nil {
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: "Open diagnostics", Mode: props.Mode,
			OnClick: func() { props.Actions.OnDiagnostics(value.DiagnosticsPath) },
		}))
	}
	return stack(props.Mode, children...)
}

type definition struct{ term, value string }

func forecastMetricStrip(mode primitives.Mode, values ...definition) ui.Node {
	children := make([]ui.Node, 0, len(values))
	for _, value := range values {
		children = append(children, html.Div(html.Props{
			Data:  map[string]string{"component": "forecast-metric", "metric": strings.ToLower(value.term)},
			Class: forecastMetricClass(mode),
		},
			html.Tag("dt", html.Props{Class: forecastMetricLabelClass(mode)}, html.Text(value.term)),
			html.Tag("dd", html.Props{Class: forecastMetricValueClass(mode)}, html.Text(known(value.value))),
		))
	}
	return html.Tag("dl", html.Props{
		Data:  map[string]string{"component": "forecast-metrics", "layout": "responsive-grid"},
		Class: forecastMetricGridClass(mode),
	}, children...)
}

func forecastContextRow(mode primitives.Mode, values ...definition) ui.Node {
	children := make([]ui.Node, 0, len(values))
	for _, value := range values {
		children = append(children, html.Div(html.Props{Class: forecastContextItemClass(mode)},
			html.Tag("dt", html.Props{Class: forecastContextTermClass(mode)}, html.Text(value.term)),
			html.Tag("dd", html.Props{Class: forecastContextValueClass(mode)}, html.Text(known(value.value))),
		))
	}
	return html.Tag("dl", html.Props{
		Aria:  map[string]string{"label": "Forecast execution context"},
		Data:  map[string]string{"component": "forecast-context"},
		Class: forecastContextRowClass(mode),
	}, children...)
}

func definitionList(mode primitives.Mode, values ...definition) ui.Node {
	children := make([]ui.Node, 0, len(values)*2)
	for _, value := range values {
		children = append(children,
			html.Tag("dt", html.Props{Class: definitionTermClass(mode)}, html.Text(value.term)),
			html.Tag("dd", html.Props{Class: definitionValueClass(mode)}, html.Text(known(value.value))),
		)
	}
	return html.Tag("dl", html.Props{Class: definitionListClass(mode)}, children...)
}

func paragraph(label, value string, mode primitives.Mode) ui.Node {
	return html.Div(html.Props{},
		html.H3(html.Props{Class: subheadingClass(mode)}, html.Text(label)),
		html.P(html.Props{Class: bodyTextClass(mode)}, html.Text(known(value))),
	)
}

func summaryParagraph(value string, mode primitives.Mode) ui.Node {
	return html.P(html.Props{
		Data:  map[string]string{"component": "card-summary"},
		Class: bodyTextClass(mode),
	}, html.Text(known(value)))
}

func secondaryDetails(label string, mode primitives.Mode, children ...ui.Node) ui.Node {
	return html.Details(html.Props{
		Data:  map[string]string{"component": "card-secondary-details", "default-state": "collapsed"},
		Class: secondaryDetailsClass(mode),
	},
		html.Summary(html.Props{Class: disclosureSummaryClass(mode)}, html.Text(label)),
		html.Div(html.Props{Class: secondaryDetailsBodyClass(mode)}, children...),
	)
}

func namedList(label string, values []string, mode primitives.Mode) ui.Node {
	items := make([]ui.Node, 0, len(values))
	for _, value := range values {
		items = append(items, html.Li(html.Props{}, html.Text(value)))
	}
	return nodeList(label, items, mode)
}

func uint64List(values []uint64) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strconv.FormatUint(value, 10)
	}
	return result
}

func messagePresentationRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return "user"
	case "assistant", "agent":
		return "agent"
	default:
		return "system"
	}
}

func nodeList(label string, items []ui.Node, mode primitives.Mode) ui.Node {
	if len(items) == 0 {
		return paragraph(label, "None", mode)
	}
	return html.Div(html.Props{},
		html.H3(html.Props{Class: subheadingClass(mode)}, html.Text(label)),
		html.Ul(html.Props{Class: listClass(mode)}, items...),
	)
}

func stack(mode primitives.Mode, children ...ui.Node) ui.Node {
	return html.Div(html.Props{Class: stackClass(mode)}, children...)
}

func known(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return value
}

func moneyText(value domain.Money) string {
	if value.Validate() != nil {
		return "Unknown"
	}
	return readout.FormatExactMoneyForReading(value)
}

func moneyOrUnknown(value domain.Money) string { return moneyText(value) }

func durationText(value time.Duration) string {
	if value <= 0 {
		return "Unknown"
	}
	return value.String()
}

func timeText(value time.Time) string {
	if value.IsZero() {
		return "Unknown"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func uintText(value uint64) string {
	if value == 0 {
		return "Unknown"
	}
	return strconv.FormatUint(value, 10)
}

func approvalActionLabel(action timelinecard.ApprovalAction) string {
	switch action {
	case timelinecard.ApprovalAllowOnce:
		return "Allow once"
	case timelinecard.ApprovalAllowForTask:
		return "Allow for this task"
	case timelinecard.ApprovalDeny:
		return "Deny"
	default:
		return "Unknown action"
	}
}

func stackClass(mode primitives.Mode) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(mode.Tokens().Spacing.SM))).String()
}

func actionRowClass(mode primitives.Mode) string {
	return css.New(u.Flex, css.FlexWrap.Wrap, css.Gap(css.Px(mode.Tokens().Spacing.SM))).String()
}

func messageBodyClass(mode primitives.Mode, role string) string {
	tokens := mode.Tokens()
	rules := []css.Rule{
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
		css.MinWidth(css.Zero), css.MaxWidth(css.Percent(100)),
	}
	// A message used to be drawn in a bordered box inside the bordered card
	// that already announced it, one box inside another saying the same thing.
	// What separates a person's words from the agent's is now the entry itself
	// and the spine node beside it.
	_ = role
	return css.New(rules...).String()
}

// definitionListClass lays a card's facts out as label and value.
//
// The label column took its minimum content width, so "Affected action" broke
// across two lines beside a one-word value. It now reserves a readable measure
// and gives the rest to the value.
func definitionListClass(mode primitives.Mode) string {
	return css.New(
		u.Grid,
		css.GridCols(
			css.MinMax(css.TrackLen(css.Px(116)), css.TrackLen(css.Auto)),
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
		),
		css.ColumnGap(css.Px(mode.Tokens().Spacing.MD)),
		css.RowGap(css.Px(mode.Tokens().Spacing.XS)),
		css.Margin(css.Zero),
	).String()
}

func definitionTermClass(mode primitives.Mode) string {
	return css.New(css.FontWeight.Semibold, css.TextColor(css.Hex(string(mode.Tokens().Colors.TextMuted)))).String()
}

func definitionValueClass(mode primitives.Mode) string {
	return css.New(css.Margin(css.Zero), css.MinWidth(css.Zero), css.OverflowWrap.Anywhere).String()
}

func subheadingClass(mode primitives.Mode) string {
	return css.New(css.TextColor(css.Hex(string(mode.Tokens().Colors.TextPrimary))),
		css.Margin(css.Zero), css.FontSize(css.Px(mode.Tokens().Typography.Body.Size)),
		css.LineHeightLen(css.Px(mode.Tokens().Typography.Body.LineHeight)), css.FontWeight.Semibold,
	).String()
}

// bodyTextClass sets a typed card's prose.
//
// This is narration a person reads and judges -- what the agent proposes, what
// it did, what it claims -- so it takes the reading serif and a measured line
// length. A claim set in the interface sans reads like a control label.
func bodyTextClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	return css.New(
		css.Margin(css.Zero), css.OverflowWrap.Anywhere,
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.Body.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
		css.MaxWidth(css.Ch(80)),
	).String()
}

func listClass(mode primitives.Mode) string {
	return css.New(css.Margin(css.Zero), css.PaddingX(css.Px(mode.Tokens().Spacing.LG))).String()
}

func secondaryDetailsClass(mode primitives.Mode) string {
	return css.New(css.MinWidth(css.Zero), css.OverflowY.Visible).String()
}

func secondaryDetailsBodyClass(mode primitives.Mode) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(mode.Tokens().Spacing.SM)),
		css.PaddingY(css.Px(mode.Tokens().Spacing.XS)),
	).String()
}

func disclosureSummaryClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	// A disclosure inside an entry is an aside, not a heading. It keeps the
	// full pointer target and takes the smallest label the system has, so an
	// entry reads as one thing with a way in rather than as two stacked rows.
	rules := []css.Rule{
		u.InlineFlex, u.ItemsCenter,
		css.Gap(css.Px(tokens.Spacing.XS)),
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
		css.FontWeight.Semibold,
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
		css.Cursor.Pointer,
	}
	rules = append(rules, css.Hover(
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	return css.New(rules...).String()
}

func forecastMetricGridClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	return css.New(
		u.Grid,
		css.GridCols(css.RepeatFit(css.MinMax(css.TrackLen(css.Rem(8.5)), css.Fr(1)))),
		css.Gap(css.Px(tokens.Spacing.SM)), css.Margin(css.Zero), css.MinWidth(css.Zero),
	).String()
}

func forecastMetricClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
		css.Padding(css.Px(tokens.Spacing.SM)), css.MinWidth(css.Zero),
		css.Bg(css.Hex(string(tokens.Colors.Surface3))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	).String()
}

func forecastMetricLabelClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	return css.New(
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
	).String()
}

// forecastMetricValueClass sets a measured readout.
//
// A cost, a duration, or a confidence is something the machine measured, so it
// takes the monospace face. Reading it in the same serif as the narration
// beside it would make an estimate look like a statement.
func forecastMetricValueClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	return css.New(
		css.Margin(css.Zero), css.MinWidth(css.Zero), css.OverflowWrap.Anywhere,
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.MetricValue.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.MetricValue.LineHeight)),
		css.FontWeight.Medium,
	).String()
}

func forecastContextRowClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	return css.New(
		u.Flex, u.ItemsCenter, css.FlexWrap.Wrap,
		css.ColumnGap(css.Px(tokens.Spacing.LG)), css.RowGap(css.Px(tokens.Spacing.XS)),
		css.Margin(css.Zero), css.MinWidth(css.Zero),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
	).String()
}

func forecastContextItemClass(mode primitives.Mode) string {
	return css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(mode.Tokens().Spacing.XS)), css.MinWidth(css.Zero)).String()
}

func forecastContextTermClass(mode primitives.Mode) string {
	return css.New(css.FontWeight.Semibold, css.TextColor(css.Hex(string(mode.Tokens().Colors.TextMuted)))).String()
}

func forecastContextValueClass(mode primitives.Mode) string {
	return css.New(css.Margin(css.Zero), css.MinWidth(css.Zero), css.OverflowWrap.Anywhere).String()
}
