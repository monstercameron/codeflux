package graphinspector

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ContextInspector renders the selected graph node as a bounded read-only
// explanation surface. Authoritative graph and command state remain in props.
func ContextInspector(props Props) ui.Node {
	if err := props.Validate(); err != nil {
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Node inspector unavailable", Message: err.Error(),
			Tone: design.StatusFailure, Mode: props.Mode,
		})
	}
	tokens := props.Mode.Tokens()
	node := props.Node
	return html.Aside(html.Props{
		Aria: map[string]string{"label": "Node inspector: " + node.DisplayName()},
		Data: map[string]string{
			"component": "graph-node-inspector", "node-id": node.ID().String(),
			"graph-revision-id": props.RevisionID.String(), "node-class": string(node.Class()),
			"node-status": string(node.Status()), "bounded": "true", "read-only": "true",
		},
		Class: inspectorClass(tokens),
	},
		inspectorHeader(props),
		identitySection(props),
		contractSection(props),
		evidenceSection(props),
		attributionSection(props),
		relatedSection(props),
		sourceSection(props),
		actionsSection(props),
	)
}

func inspectorHeader(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	return html.Header(html.Props{Class: headerClass(tokens)},
		html.Div(html.Props{Class: titleClass(tokens)},
			html.P(html.Props{Class: eyebrowClass(tokens), Text: "Selected graph node"}),
			html.H2(html.Props{ID: "graph-node-inspector-title", Class: headingClass(tokens)},
				primitives.TechnicalLabel(primitives.TechnicalLabelProps{
					FullLabel: props.Node.DisplayName(), VisibleLabel: props.Node.DisplayName(), Mode: props.Mode,
				}),
			),
		),
		primitives.Badge(primitives.BadgeProps{
			Label: humanize(string(props.Node.Status())), Status: statusTone(props.Node.Status()), Mode: props.Mode,
		}),
	)
}

func identitySection(props Props) ui.Node {
	return inspectorSection(props.Mode, "Identity and state", "identity",
		definitionList(props.Mode,
			definition{"Stable node identity", props.Node.ID().String(), "node-id"},
			definition{"Graph revision", props.RevisionID.String(), "revision-id"},
			definition{"Node class", humanize(string(props.Node.Class())), "node-class"},
			definition{"Execution status", humanize(string(props.Node.Status())), "node-status"},
		),
	)
}

func contractSection(props Props) ui.Node {
	contract := props.Node.Contract()
	purpose := contract.Purpose()
	if contract.IsZero() || strings.TrimSpace(purpose) == "" {
		purpose = "Unknown — contract purpose is not modeled."
	}
	return inspectorSection(props.Mode, "Contract", "contract",
		definitionList(props.Mode, definition{"Summary", purpose, "contract-summary"}),
		contractList(props.Mode, "Known inputs", "inputs", contract.Inputs()),
		contractList(props.Mode, "Known outputs", "outputs", contract.Outputs()),
		contractList(props.Mode, "Known effects", "effects", contract.Effects()),
	)
}

func contractList(mode primitives.Mode, title, kind string, values []string) ui.Node {
	children := []ui.Node{html.H4(html.Props{Class: subheadingClass(mode.Tokens()), Text: title})}
	if len(values) == 0 {
		children = append(children, unknownParagraph(mode, "not modeled"))
	} else {
		items := make([]ui.Node, 0, len(values))
		for _, value := range values {
			items = append(items, html.Li(html.Props{Text: value}))
		}
		children = append(children, html.Ul(html.Props{
			Data:  map[string]string{"inspector-list": kind, "count": strconv.Itoa(len(values))},
			Class: listClass(mode.Tokens()),
		}, items...))
	}
	return html.Div(html.Props{Data: map[string]string{"component": "contract-" + kind}}, children...)
}

func evidenceSection(props Props) ui.Node {
	evidence := props.Evidence
	if !evidence.Known {
		return inspectorSection(props.Mode, "Evidence and guarantee", "evidence",
			unknownParagraph(props.Mode, evidence.UnknownReason),
		)
	}
	guarantee := "Unknown"
	if evidence.GuaranteeKnown {
		guarantee = humanize(string(evidence.Guarantee))
		if reason := strings.TrimSpace(evidence.GuaranteeReason); reason != "" {
			guarantee += " — " + reason
		}
	} else if reason := strings.TrimSpace(evidence.GuaranteeReason); reason != "" {
		guarantee += " — " + reason
	}
	children := []ui.Node{definitionList(props.Mode, definition{"Guarantee level", guarantee, "guarantee-level"})}
	if len(evidence.SupportingItems) == 0 {
		children = append(children, html.P(html.Props{
			Data:  map[string]string{"evidence-state": "known-empty"},
			Class: secondaryTextClass(props.Mode.Tokens()),
			Text:  "Known — no supporting evidence items are attached.",
		}))
	} else {
		items := make([]ui.Node, 0, len(evidence.SupportingItems))
		for _, item := range evidence.SupportingItems {
			items = append(items, html.Li(html.Props{
				Data:  map[string]string{"evidence-id": item.ID.String(), "validation-state": string(item.State)},
				Class: itemClass(props.Mode.Tokens()),
			},
				html.Div(html.Props{Class: itemHeaderClass(props.Mode.Tokens())},
					html.Strong(html.Props{Text: item.Label}),
					primitives.Badge(primitives.BadgeProps{Label: humanize(string(item.State)), Status: validationTone(item.State), Mode: props.Mode}),
				),
				html.P(html.Props{Class: identityTextClass(props.Mode.Tokens()), Text: item.ID.String()}),
				html.P(html.Props{Class: secondaryTextClass(props.Mode.Tokens()), Text: knownText(item.Summary, "summary not supplied")}),
			))
		}
		children = append(children, html.Ul(html.Props{
			Data:  map[string]string{"inspector-list": "supporting-evidence", "count": strconv.Itoa(len(items))},
			Class: listClass(props.Mode.Tokens()),
		}, items...))
	}
	return inspectorSection(props.Mode, "Evidence and guarantee", "evidence", children...)
}

func attributionSection(props Props) ui.Node {
	return inspectorSection(props.Mode, "Attribution", "attribution",
		definitionList(props.Mode,
			definition{"Elapsed time", durationText(props.Duration), "time"},
			definition{"Provider tokens", tokenText(props.Tokens), "tokens"},
			definition{"Monetary cost", costText(props.Cost), "cost"},
		),
	)
}

func relatedSection(props Props) ui.Node {
	children := []ui.Node{
		relatedIdentityList(props.Mode, "Related messages", "messages", messageStrings(props.RelatedMessages)),
		relatedIdentityList(props.Mode, "Related events", "events", eventStrings(props.Node.Sources().EventIDs())),
		relatedIdentityList(props.Mode, "Related plan steps", "plan-steps", planStepStrings(props.Node.Sources().PlanSteps())),
	}
	return inspectorSection(props.Mode, "Related identities", "related", children...)
}

func relatedIdentityList(mode primitives.Mode, title, kind string, values []string) ui.Node {
	children := []ui.Node{html.H4(html.Props{Class: subheadingClass(mode.Tokens()), Text: title})}
	if len(values) == 0 {
		children = append(children, unknownParagraph(mode, "no related "+kind+" are attached"))
	} else {
		items := make([]ui.Node, 0, len(values))
		for _, value := range values {
			items = append(items, html.Li(html.Props{Class: identityTextClass(mode.Tokens()), Text: value}))
		}
		children = append(children, html.Ul(html.Props{
			Data:  map[string]string{"inspector-list": kind, "count": strconv.Itoa(len(values))},
			Class: listClass(mode.Tokens()),
		}, items...))
	}
	return html.Div(html.Props{Data: map[string]string{"component": "related-" + kind}}, children...)
}

func sourceSection(props Props) ui.Node {
	children := make([]ui.Node, 0, len(props.SourceLocations)+1)
	if len(props.SourceLocations) == 0 {
		children = append(children,
			unknownParagraph(props.Mode, "no validated repository source location is attached"),
			actionControl(props, "open-in-editor", "Open in editor", props.Actions.OpenInEditor, 0, true),
		)
	} else {
		items := make([]ui.Node, 0, len(props.SourceLocations))
		for index, location := range props.SourceLocations {
			index, location := index, location
			label := sourceLocationText(location)
			items = append(items, html.Li(html.Props{
				Data: map[string]string{
					"source-path": location.RelativePath(), "repository-id": location.RepositoryID().String(),
					"repository-revision": location.RepositoryRevision(), "source-index": strconv.Itoa(index),
				},
				Class: itemClass(props.Mode.Tokens()),
			},
				html.P(html.Props{Class: identityTextClass(props.Mode.Tokens()), Text: label}),
				actionControl(props, "open-in-editor-"+strconv.Itoa(index), "Open in editor", props.Actions.OpenInEditor, index, false),
			))
		}
		children = append(children, html.Ul(html.Props{
			Data:  map[string]string{"inspector-list": "source-locations", "count": strconv.Itoa(len(items))},
			Class: listClass(props.Mode.Tokens()),
		}, items...))
	}
	return inspectorSection(props.Mode, "Source locations", "sources", children...)
}

func actionsSection(props Props) ui.Node {
	actions := []struct {
		kind         ActionKind
		label        string
		availability ActionAvailability
	}{
		{ActionExplainInChat, "Explain in chat", props.Actions.ExplainInChat},
		{ActionExpandNeighbors, "Expand neighbors", props.Actions.ExpandNeighbors},
		{ActionDependencyCone, "Isolate dependency cone", props.Actions.DependencyCone},
		{ActionEvidenceCone, "Isolate evidence cone", props.Actions.EvidenceCone},
		{ActionCompareRevision, "Compare revision", props.Actions.CompareRevision},
	}
	children := make([]ui.Node, 0, len(actions))
	for _, action := range actions {
		action := action
		children = append(children, actionControl(props, string(action.kind), action.label, action.availability, 0, false))
	}
	return inspectorSection(props.Mode, "Explore", "actions",
		html.Div(html.Props{
			Role: "group", Aria: map[string]string{"label": "Graph node exploration actions"},
			Data:  map[string]string{"component": "graph-inspector-actions", "keyboard": "native-buttons"},
			Class: actionGridClass(props.Mode.Tokens()),
		}, children...),
	)
}

func actionControl(props Props, id, label string, availability ActionAvailability, sourceIndex int, forceDisabled bool) ui.Node {
	reason := strings.TrimSpace(availability.DisabledReason)
	disabled := !availability.Enabled || availability.Busy || forceDisabled
	if forceDisabled && reason == "" {
		reason = "No validated repository source location is available."
	}
	reasonID := "graph-inspector-action-" + id + "-reason"
	onClick := func() {
		if strings.HasPrefix(id, "open-in-editor") {
			ActivateSource(props, sourceIndex)
			return
		}
		ActivateAction(props, ActionKind(id))
	}
	children := []ui.Node{primitives.Button(primitives.ButtonProps{
		ID: "graph-inspector-action-" + id, Label: label,
		AccessibleLabel: label + " for " + props.Node.DisplayName(),
		Disabled:        disabled, Busy: availability.Busy,
		DescribedBy: map[bool]string{true: reasonID}[reason != ""],
		Mode:        props.Mode, OnClick: onClick,
	})}
	if reason != "" {
		children = append(children, html.P(html.Props{
			ID: reasonID, Data: map[string]string{"disabled-reason": id},
			Class: reasonClass(props.Mode.Tokens()), Text: reason,
		}))
	}
	return html.Div(html.Props{
		Data: map[string]string{
			"component": "graph-inspector-action", "action": id,
			"enabled": strconv.FormatBool(availability.Enabled), "busy": strconv.FormatBool(availability.Busy),
		}, Class: actionClass(props.Mode.Tokens()),
	}, children...)
}

type definition struct{ label, value, kind string }

func definitionList(mode primitives.Mode, definitions ...definition) ui.Node {
	children := make([]ui.Node, 0, len(definitions)*2)
	for _, item := range definitions {
		children = append(children,
			html.Tag("dt", html.Props{Class: definitionLabelClass(mode.Tokens()), Text: item.label}),
			html.Tag("dd", html.Props{
				Data: map[string]string{"inspector-field": item.kind}, Class: definitionValueClass(mode.Tokens()),
				Text: knownText(item.value, "not supplied"),
			}),
		)
	}
	return html.Tag("dl", html.Props{Class: definitionGridClass(mode.Tokens())}, children...)
}

func inspectorSection(mode primitives.Mode, title, kind string, children ...ui.Node) ui.Node {
	content := []ui.Node{html.H3(html.Props{Class: sectionHeadingClass(mode.Tokens()), Text: title})}
	content = append(content, children...)
	return html.Section(html.Props{
		Aria: map[string]string{"label": title}, Data: map[string]string{"inspector-section": kind},
		Class: sectionClass(mode.Tokens()),
	}, content...)
}

func unknownParagraph(mode primitives.Mode, reason string) ui.Node {
	return html.P(html.Props{
		Data: map[string]string{"known": "false"}, Class: secondaryTextClass(mode.Tokens()),
		Text: knownText("", reason),
	})
}

func knownText(value, unknownReason string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	if unknownReason = strings.TrimSpace(unknownReason); unknownReason != "" {
		return "Unknown — " + unknownReason + "."
	}
	return "Unknown"
}

func durationText(value DurationAttribution) string {
	if !value.Known {
		return knownText("", value.UnknownReason)
	}
	return value.Value.String() + " attributable elapsed time"
}

func tokenText(value TokenAttribution) string {
	if !value.Usage.Known {
		return knownText("", value.UnknownReason)
	}
	total, _, err := value.Usage.Total()
	if err != nil {
		return "Unknown — token attribution is invalid."
	}
	text := fmt.Sprintf("%d total — input %d, cached input %d, cache write %d, output %d, reasoning %d",
		total, value.Usage.Input, value.Usage.CachedInput, value.Usage.CacheWrite, value.Usage.Output, value.Usage.Reasoning)
	if len(value.Usage.ProviderSpecific) > 0 {
		keys := make([]string, 0, len(value.Usage.ProviderSpecific))
		for key := range value.Usage.ProviderSpecific {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			text += fmt.Sprintf(", %s %d", key, value.Usage.ProviderSpecific[key])
		}
	}
	return text + " exact provider-reported tokens"
}

func costText(value CostAttribution) string {
	if !value.Known {
		return knownText("", value.UnknownReason)
	}
	return fmt.Sprintf("%s %d minor units — pricing snapshot %s",
		value.Value.Currency, value.Value.MinorUnits, value.PricingSnapshot)
}

func messageStrings(values []domain.MessageID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func eventStrings(values []domain.EventID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func planStepStrings(values []taskgraph.PlanStepLink) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = fmt.Sprintf("Plan revision %d · step %s", value.PlanRevision, value.StepID)
	}
	return result
}

func sourceLocationText(location SourceLocation) string {
	return fmt.Sprintf("%s:%d:%d–%d:%d · repository revision %s",
		location.RelativePath(), location.StartLine(), location.StartColumn(),
		location.EndLine(), location.EndColumn(), location.RepositoryRevision())
}

func humanize(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "-", " ")
	if value == "" {
		return "Unknown"
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func statusTone(status taskgraph.NodeStatus) design.Status {
	switch status {
	case taskgraph.NodeStatusActive:
		return design.StatusActive
	case taskgraph.NodeStatusPassed:
		return design.StatusSuccess
	case taskgraph.NodeStatusWarning:
		return design.StatusWarning
	case taskgraph.NodeStatusFailed:
		return design.StatusFailure
	case taskgraph.NodeStatusBlocked:
		return design.StatusBlocked
	case taskgraph.NodeStatusInvalidated:
		return design.StatusInvalidated
	default:
		return design.StatusPending
	}
}

func validationTone(status domain.ValidationState) design.Status {
	switch status {
	case domain.ValidationStateRunning:
		return design.StatusActive
	case domain.ValidationStatePassed:
		return design.StatusSuccess
	case domain.ValidationStateFailed:
		return design.StatusFailure
	case domain.ValidationStateWaived, domain.ValidationStateSkipped:
		return design.StatusWarning
	case domain.ValidationStateInvalidated:
		return design.StatusInvalidated
	default:
		return design.StatusPending
	}
}

func inspectorClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Rhythm.PanelGap)),
		css.MaxWidth(css.Percent(100)), css.Padding(css.Px(tokens.Rhythm.PanelInset)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)), css.Font(css.FontStack(tokens.Fonts.UI)),
	).String()
}

func sectionClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
		css.Padding(css.Px(tokens.Spacing.MD)), css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
	).String()
}

func headerClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.MD))).String()
}

func titleClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.MinWidth(css.Zero)).String()
}

func headingClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)), css.LineHeightLen(css.Px(tokens.Typography.PanelHeading.LineHeight)), css.FontWeight.Semibold).String()
}

func sectionHeadingClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)), css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)), css.FontWeight.Semibold).String()
}

func subheadingClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)), css.FontWeight.Semibold).String()
}

func eyebrowClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)), css.FontWeight.Semibold).String()
}

func secondaryTextClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight))).String()
}

func identityTextClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.Font(css.FontStack(tokens.Fonts.Code)), css.FontSize(css.Px(tokens.Typography.Code.Size)), css.LineHeightLen(css.Px(tokens.Typography.Code.LineHeight)), css.OverflowWrap.Anywhere).String()
}

func definitionGridClass(tokens design.Tokens) string {
	return css.New(u.Grid, css.GridCols(css.Repeat(2, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))), css.Gap(css.Px(tokens.Spacing.SM)), css.Margin(css.Zero)).String()
}

func definitionLabelClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)), css.FontWeight.Semibold).String()
}

func definitionValueClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)), css.OverflowWrap.Anywhere).String()
}

func listClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)), css.Margin(css.Zero), css.Padding(css.Px(tokens.Spacing.SM))).String()
}

func itemClass(tokens design.Tokens) string {
	return css.New(css.Padding(css.Px(tokens.Spacing.SM)), css.Bg(css.Hex(string(tokens.Colors.Surface3))), css.Rounded(css.Px(tokens.Geometry.RadiusSmall))).String()
}

func itemHeaderClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.SM))).String()
}

func actionGridClass(tokens design.Tokens) string {
	return css.New(u.Grid, css.GridCols(css.Repeat(2, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))), css.Gap(css.Px(tokens.Spacing.SM))).String()
}

func actionClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.MinWidth(css.Zero)).String()
}

func reasonClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight))).String()
}
