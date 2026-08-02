// Package timelineview renders typed timeline cards with GoWebComponents.
package timelineview

import (
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type Actions struct {
	OnCopy                func(string)
	OnApproval            func(string, timelinecard.ApprovalAction)
	OnApprovePlan         func(uint64)
	OnRequestPlanChange   func(uint64)
	OnComparePlanRevision func(uint64)
	OnOpenReview          func(string)
	OnDiagnostics         func(string)
	OnSelectNode          func(string)
	OnRecoveryChoice      func(string)
	OnRetry               func()
	ApprovalCommand       func(string) ApprovalCommandState
	ApprovalActionCommand func(string, timelinecard.ApprovalAction) ApprovalCommandState
	ApprovePlanCommand    func(uint64) ActionCommandState
	PlanChangeCommand     func(uint64) ActionCommandState
}

type ActionCommandState struct {
	Busy           bool
	IdempotencyKey string
	TransportMode  string
	DisabledReason string
}

type ApprovalCommandState = ActionCommandState

func ApprovalFocusTargetID(approvalID string) string {
	return focusTargetID("timeline-approval-", approvalID)
}

// CardFocusTargetID maps a stable timeline key to a deterministic DOM focus
// target without allowing the key to become markup.
func CardFocusTargetID(stableKey string) string {
	return focusTargetID("timeline-card-", stableKey)
}

func focusTargetID(prefix, identity string) string {
	var builder strings.Builder
	builder.WriteString(prefix)
	for _, value := range identity {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
			value >= '0' && value <= '9' || value == '-' || value == '_' {
			builder.WriteRune(value)
		} else {
			builder.WriteByte('-')
		}
	}
	return builder.String()
}

type Props struct {
	Card     timelinecard.Card
	Mode     primitives.Mode
	Actions  Actions
	Selected bool
}

// Renderer is the exhaustive GWC card renderer. Unsupported or malformed
// input remains visible as a safe fallback instead of disappearing.
func Renderer(props Props) ui.Node {
	if err := props.Card.Validate(); err != nil {
		return invalidCard(props.Mode, err)
	}
	presentation := PresentationFor(props.Card)
	tokens := props.Mode.Tokens()
	attention := cardNeedsAttention(props.Card, presentation)
	body := renderBody(props)
	return html.Article(html.Props{
		ID: CardFocusTargetID(props.Card.StableKey), TabIndex: -1,
		Aria: map[string]string{"label": presentation.AccessibleName()},
		Data: map[string]string{
			"component":  "timeline-card",
			"card-kind":  string(props.Card.Kind),
			"attention":  strconv.FormatBool(attention),
			"density":    "compact",
			"overflow-y": "visible",
			"sequence":   strconv.FormatUint(props.Card.Sequence, 10),
			"stable-key": props.Card.StableKey,
			"status":     presentation.StatusLabel,
			"selected":   strconv.FormatBool(props.Selected),
		},
		Class: cardClass(tokens, presentation.Tone, attention, props.Selected, cardSpeaksAsAgent(props.Card)),
	},
		html.Header(html.Props{Class: headerClass(tokens)},
			html.Div(html.Props{Class: headerLabelClass(tokens)},
				cardEyebrow(presentation.Eyebrow, tokens),
				html.H2(html.Props{Class: titleClass(tokens)}, html.Text(presentation.Title)),
			),
			cardCopyAction(props),
			// Every card keeps its badge: status must never be carried by
			// colour alone, and the badge is where the shape and the word live.
			// The settled ones are quiet by treatment rather than absent.
			cardStatusBadge(presentation, props.Mode),
		),
		body,
		renderMetadata(props.Card, props.Mode),
	)
}

type Presentation struct {
	Eyebrow     string
	Title       string
	StatusLabel string
	Tone        design.Status
}

// AccessibleName keeps the status audible even when no badge is drawn for it.
func (presentation Presentation) AccessibleName() string {
	name := presentation.Title
	if strings.TrimSpace(presentation.StatusLabel) != "" {
		name += ", " + presentation.StatusLabel
	}
	return name
}

// PresentationFor gives every card text, shape/icon-backed tone, and status.
func PresentationFor(card timelinecard.Card) Presentation {
	switch card.Kind {
	case timelinecard.KindMessage:
		status := "Complete"
		tone := design.StatusNeutral
		if card.Message != nil {
			switch card.Message.Status {
			case timelinecard.MessageProvisional:
				status, tone = "In progress", design.StatusActive
			case timelinecard.MessageInterrupted:
				status, tone = "Interrupted", design.StatusWarning
			}
		}
		// A transcript names its speakers the way a person would. "Conversation
		// / User message" labelled the obvious twice in the vocabulary of the
		// event log rather than of the exchange.
		role := "Message"
		if card.Message != nil && strings.TrimSpace(card.Message.Role) != "" {
			switch strings.ToLower(strings.TrimSpace(card.Message.Role)) {
			case "user", "human":
				role = "You"
			case "assistant", "agent", "codeflux":
				role = "Codeflux"
			default:
				role = humanize(card.Message.Role)
			}
		}
		return Presentation{"", role, status, tone}
	case timelinecard.KindThreadState:
		status := "Updated"
		if card.ThreadState != nil {
			status = humanize(card.ThreadState.Action)
		}
		return Presentation{"Conversation", "Thread state", status, design.StatusNeutral}
	case timelinecard.KindRequirement:
		return Presentation{"Intent", "Requirement and ambiguity", "Needs review", design.StatusAccent}
	case timelinecard.KindForecast:
		return Presentation{"Estimate", "Forecast", "Estimate", design.StatusNeutral}
	case timelinecard.KindPlan:
		status := "Current"
		if card.Plan != nil && card.Plan.Superseded {
			status = "Superseded"
		}
		return Presentation{"Approach", "Plan", status, design.StatusPlan}
	case timelinecard.KindPlanRevision:
		return Presentation{"Approach", "Plan revision", "Approval reset", design.StatusWarning}
	case timelinecard.KindContext:
		return Presentation{"Inputs", "Context selection", "Bounded", design.StatusNeutral}
	case timelinecard.KindTool:
		status := "Unknown"
		if card.Tool != nil {
			status = humanize(card.Tool.State)
		}
		return Presentation{"Execution", "Tool activity", status, toneForState(status)}
	case timelinecard.KindApproval:
		status := "Pending"
		if card.Approval != nil {
			status = humanize(card.Approval.State)
		}
		return Presentation{"Authority", "Approval request", status, toneForState(status)}
	case timelinecard.KindCheckpoint:
		return Presentation{"Recovery", "Checkpoint", "Recorded", design.StatusEvidence}
	case timelinecard.KindValidation:
		status := "Unknown"
		if card.Validation != nil {
			status = humanize(string(card.Validation.Status))
		}
		return Presentation{"Correctness", "Validation", status, toneForState(status)}
	case timelinecard.KindDiff:
		return Presentation{"Changes", "Diff summary", "Review required", design.StatusAccent}
	case timelinecard.KindCostBudget:
		return Presentation{"Budget", "Cost and budget", "Decision relevant", design.StatusWarning}
	case timelinecard.KindRecovery:
		return Presentation{"Recovery", "Recovery choice", "Action required", design.StatusBlocked}
	case timelinecard.KindError:
		return Presentation{"Failure", "Error", "Failed", design.StatusFailure}
	case timelinecard.KindCompletion:
		status := "Unknown"
		if card.Completion != nil {
			status = humanize(string(card.Completion.Status))
		}
		return Presentation{"Outcome", "Completion summary", status, toneForState(status)}
	case timelinecard.KindTaskState:
		status := "Updated"
		if card.TaskState != nil {
			status = humanize(card.TaskState.To)
		}
		return Presentation{"Task", "Task state changed", status, toneForState(status)}
	case timelinecard.KindUsage:
		return Presentation{"Usage", "Token usage", "Measured", design.StatusNeutral}
	case timelinecard.KindGraphChange:
		return Presentation{"Graph", "Graph revision", "Updated", design.StatusNeutral}
	case timelinecard.KindUnknown:
		return Presentation{"Compatibility", "Unsupported event", "Inspect", design.StatusWarning}
	default:
		return Presentation{"Timeline", "Unsupported card", "Inspect", design.StatusWarning}
	}
}

func renderMetadata(card timelinecard.Card, mode primitives.Mode) ui.Node {
	timestamp := "Unknown time"
	if !card.OccurredAt.IsZero() {
		timestamp = card.OccurredAt.UTC().Format(time.RFC3339Nano)
	}
	return html.Details(html.Props{
		Data:  map[string]string{"component": "event-metadata", "default-state": "collapsed"},
		Class: metadataClass(mode.Tokens()),
	},
		html.Summary(html.Props{Class: disclosureSummaryClass(mode)}, html.Text("Event details")),
		definitionList(mode,
			definition{"Sequence", strconv.FormatUint(card.Sequence, 10)},
			definition{"Stable key", card.StableKey},
			definition{"Time", timestamp},
		),
	)
}

func invalidCard(mode primitives.Mode, err error) ui.Node {
	return html.Article(html.Props{
		Role:  "status",
		Data:  map[string]string{"component": "timeline-card", "card-kind": "invalid", "status": "invalid"},
		Class: cardClass(mode.Tokens(), design.StatusWarning, true, false, false),
	},
		html.H2(html.Props{Class: design.HeadingClass(mode.Tokens(), design.HeadingSection)}, html.Text("Timeline item unavailable")),
		html.P(html.Props{}, html.Text("This item could not be presented safely.")),
		html.Details(html.Props{},
			html.Summary(html.Props{}, html.Text("Diagnostic detail")),
			html.Code(html.Props{}, html.Text(err.Error())),
		),
	)
}

func humanize(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "-", " "))
	if value == "" {
		return "Unknown"
	}
	runes := []rune(value)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

func toneForState(value string) design.Status {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "passed", "complete", "completed", "accepted", "validated", "reviewed", "granted", "recorded":
		return design.StatusSuccess
	case "failed", "rejected", "denied":
		return design.StatusFailure
	case "waived", "skipped", "unavailable", "interrupted", "warning", "expired":
		return design.StatusWarning
	case "stale", "invalidated", "superseded", "rolled back":
		return design.StatusInvalidated
	case "running", "active", "in progress", "forecasting", "validating":
		return design.StatusActive
	case "pending", "awaiting authority", "awaiting plan approval":
		return design.StatusPending
	case "blocked", "recovery required", "action required":
		return design.StatusBlocked
	default:
		return design.StatusNeutral
	}
}

func cardNeedsAttention(card timelinecard.Card, presentation Presentation) bool {
	switch card.Kind {
	case timelinecard.KindRequirement, timelinecard.KindPlanRevision,
		timelinecard.KindCostBudget, timelinecard.KindRecovery, timelinecard.KindError:
		return true
	case timelinecard.KindApproval:
		return card.Approval != nil && card.Approval.ActionsAvailable()
	}
	switch presentation.Tone {
	case design.StatusWarning, design.StatusFailure, design.StatusBlocked:
		return true
	default:
		return false
	}
}

// cardSpeaksAsAgent reports an entry that is the agent talking to the person.
//
// What the agent says is prose to read, so it is set on the transcript itself
// with nothing drawn around it. What a person sent, and every machine record —
// a tool call, a validation, a plan — is a discrete artifact, and keeps a
// surface of its own.
func cardSpeaksAsAgent(card timelinecard.Card) bool {
	if card.Kind != timelinecard.KindMessage || card.Message == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(card.Message.Role)) {
	case "assistant", "agent", "codeflux":
		return true
	default:
		return false
	}
}

func cardClass(tokens design.Tokens, tone design.Status, attention, selected, plain bool) string {
	presentation, err := design.StatusPresentationFor(tone, tokens)
	if err != nil {
		presentation, _ = design.StatusPresentationFor(design.StatusNeutral, tokens)
	}
	// The card lost its box. Its kind and its place in the sequence are carried
	// by the spine it hangs from, so repeating them as a coloured left border
	// under a drop shadow made every entry shout at the same volume. What is
	// left is a quiet surface and a hairline; only an entry asking for a
	// decision is allowed to raise its edge.
	surface := tokens.Colors.Surface2
	border := tokens.Colors.BorderSubtle
	if plain {
		surface, border = "", ""
	}
	rules := []css.Rule{
		u.Flex, u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.SM)),
		css.PaddingY(css.Px(tokens.Spacing.SM)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.H(css.Auto), css.MinWidth(css.Zero), css.OverflowY.Visible,
	}
	if surface != "" {
		rules = append(rules,
			css.PaddingY(css.Px(tokens.Spacing.MD)),
			css.Bg(css.Hex(string(surface))),
			css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(border))),
		)
	}
	if attention {
		rules = append(rules,
			css.BorderLeft(css.Px(3), css.Hex(string(presentation.Foreground))),
			css.Bg(css.Hex(string(tokens.Colors.Surface3))),
		)
	}
	if selected {
		rules = append(rules,
			css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
			css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
		)
	}
	return css.New(rules...).String()
}

func headerClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
		css.MinHeight(css.Px(28)),
	).String()
}

func headerLabelClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.ItemsCenter, css.FlexWrap.Wrap,
		css.Gap(css.Px(tokens.Spacing.SM)),
		css.MinWidth(css.Zero),
		css.FlexGrow(css.Num(1)),
	).String()
}

func eyebrowClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
		css.FontWeight.Semibold,
		css.Tracking(css.Ems(0.07)),
		css.TextTransform.Uppercase,
	).String()
}

// titleClass names a typed timeline card: Forecast, Plan, Execution,
// Validation, Evidence.
//
// The card names a kind of claim, so it takes the serif with the claim itself
// rather than the sans used for the controls beside it.
func titleClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Margin(css.Zero),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.ControlLabel.LineHeight)),
		css.FontWeight.Semibold,
		css.Tracking(css.Ems(0.01)),
	).String()
}

// cardEyebrow names the phase an entry belongs to, and renders nothing when the
// entry speaks for itself.
func cardEyebrow(label string, tokens design.Tokens) ui.Node {
	if strings.TrimSpace(label) == "" {
		return nil
	}
	return html.P(html.Props{Class: eyebrowClass(tokens)}, html.Text(label))
}

// cardCopyAction offers the entry's text where the entry is named.
func cardCopyAction(props Props) ui.Node {
	if props.Card.Message == nil || props.Actions.OnCopy == nil {
		return nil
	}
	body := props.Card.Message.Body
	return primitives.Button(primitives.ButtonProps{
		Label: "Copy", AccessibleLabel: "Copy message", Quiet: true, Mode: props.Mode,
		OnClick: func() { props.Actions.OnCopy(body) },
	})
}

// cardStatusBadge draws the state of one entry.
func cardStatusBadge(presentation Presentation, mode primitives.Mode) ui.Node {
	return primitives.Badge(primitives.BadgeProps{
		Label: presentation.StatusLabel, Status: presentation.Tone, Mode: mode,
	})
}

func metadataClass(tokens design.Tokens) string {
	return css.New(
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
	).String()
}
