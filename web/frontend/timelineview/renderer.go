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
		Aria: map[string]string{"label": presentation.Title},
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
		Class: cardClass(tokens, presentation.Tone, attention, props.Selected),
	},
		html.Header(html.Props{Class: headerClass(tokens)},
			html.Div(html.Props{Class: headerLabelClass(tokens)},
				html.P(html.Props{Class: eyebrowClass(tokens)}, html.Text(presentation.Eyebrow)),
				html.H2(html.Props{Class: titleClass(tokens)}, html.Text(presentation.Title)),
			),
			primitives.Badge(primitives.BadgeProps{
				Label: presentation.StatusLabel, Status: presentation.Tone, Mode: props.Mode,
			}),
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
		role := "Message"
		if card.Message != nil && strings.TrimSpace(card.Message.Role) != "" {
			role = humanize(card.Message.Role) + " message"
		}
		return Presentation{"Conversation", role, status, tone}
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
		Class: cardClass(mode.Tokens(), design.StatusWarning, true, false),
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

func cardClass(tokens design.Tokens, tone design.Status, attention, selected bool) string {
	presentation, err := design.StatusPresentationFor(tone, tokens)
	if err != nil {
		presentation, _ = design.StatusPresentationFor(design.StatusNeutral, tokens)
	}
	rules := []css.Rule{
		u.Flex, u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.MD)),
		css.PaddingY(css.Px(tokens.Spacing.MD)),
		css.PaddingX(css.Px(tokens.Spacing.LG)),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Transparent),
		css.BorderLeft(css.Px(2), css.Hex(string(presentation.Foreground))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Shadow(css.ShadowOf(
			css.Zero, css.Px(8), css.Px(24), css.Px(-22), css.RGBA(0, 0, 0, 0.62),
		)),
		css.H(css.Auto), css.MinWidth(css.Zero), css.OverflowY.Visible,
	}
	if attention {
		rules = append(rules,
			css.BorderLeft(css.Px(4), css.Hex(string(presentation.Foreground))),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
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
	return css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.SM))).String()
}

func headerLabelClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.ItemsCenter, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.SM)), css.MinWidth(css.Zero)).String()
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
		css.Font(css.FontStack(tokens.Fonts.Display)),
		css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
		css.FontWeight.Normal,
	).String()
}

func metadataClass(tokens design.Tokens) string {
	return css.New(
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
	).String()
}
