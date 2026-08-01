package design

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// SpecimenProps configures the development-only token reference component.
type SpecimenProps struct {
	Development bool
	Options     Options
}

// TokenSpecimen renders a deterministic, non-networked development reference
// for colors, typography, geometry, status redundancy, and motion.
func TokenSpecimen(props SpecimenProps) ui.Node {
	if !props.Development {
		return html.Div(html.Props{
			Hidden: true,
			Data: map[string]string{
				"component": "token-specimen",
				"state":     "development-disabled",
			},
		})
	}
	tokens, err := TokensFor(props.Options)
	if err != nil {
		return html.Div(
			html.Props{
				Role: "alert",
				Data: map[string]string{
					"component": "token-specimen",
					"state":     "invalid",
				},
			},
			html.Text("Token specimen unavailable: "+err.Error()),
		)
	}
	colorNames := tokenColorMap(tokens.Colors)
	names := make([]string, 0, len(colorNames))
	for name := range colorNames {
		names = append(names, name)
	}
	sort.Strings(names)
	swatches := make([]ui.Node, 0, len(names))
	for _, name := range names {
		color := colorNames[name]
		swatches = append(swatches, html.Li(
			html.Props{
				Key:   name,
				Title: name + " " + string(color),
				Class: specimenSwatchClass(tokens, color),
			},
			html.Span(
				html.Props{Class: specimenSwatchChipClass(color)},
				html.Text(" "),
			),
			html.Strong(html.Props{}, html.Text(name)),
			html.Code(html.Props{}, html.Text(string(color))),
		))
	}
	statuses := []Status{
		StatusAccent, StatusSuccess, StatusWarning, StatusFailure,
		StatusActive, StatusBlocked, StatusInvalidated, StatusPending,
		StatusPlan, StatusEvidence,
	}
	statusNodes := make([]ui.Node, 0, len(statuses))
	for _, status := range statuses {
		presentation, presentationErr := StatusPresentationFor(status, tokens)
		if presentationErr != nil {
			continue
		}
		statusNodes = append(statusNodes, html.Li(
			html.Props{
				Key:   string(status),
				Class: specimenStatusClass(presentation),
				Data: map[string]string{
					"status": string(status),
					"shape":  presentation.Shape,
				},
			},
			html.Span(
				html.Props{Aria: map[string]string{"hidden": "true"}},
				html.Text(presentation.Icon),
			),
			html.Text(presentation.Label),
		))
	}
	return html.Main(
		html.Props{
			ID:   "codeflux-token-specimen",
			Aria: map[string]string{"label": "Codeflux design token specimen"},
			Data: map[string]string{
				"component":       "token-specimen",
				"state":           "ready",
				"theme":           string(tokens.Theme),
				"density":         string(tokens.Density),
				"reduced-motion":  strconv.FormatBool(tokens.ReducedMotion),
				"external-assets": "none",
			},
			Class: specimenRootClass(tokens),
		},
		html.Header(
			html.Props{Class: specimenStackClass(tokens.Spacing.SM)},
			html.H1(
				html.Props{Class: specimenHeadingClass(tokens)},
				html.Text("Codeflux token specimen"),
			),
			html.P(
				html.Props{Class: specimenBodyClass(tokens)},
				html.Text(fmt.Sprintf(
					"%s theme · %s density · reduced motion %t",
					tokens.Theme,
					tokens.Density,
					tokens.ReducedMotion,
				)),
			),
		),
		specimenSection(
			tokens,
			"Semantic colors",
			html.Ul(
				html.Props{
					Aria:  map[string]string{"label": "Semantic color tokens"},
					Class: specimenGridClass(tokens),
				},
				swatches...,
			),
		),
		specimenSection(
			tokens,
			"Status redundancy",
			html.Ul(
				html.Props{
					Aria: map[string]string{
						"label": "Statuses with text, icon, shape, and color",
					},
					Class: specimenGridClass(tokens),
				},
				statusNodes...,
			),
		),
		specimenSection(
			tokens,
			"Typography and rhythm",
			html.Div(
				html.Props{Class: specimenStackClass(tokens.Spacing.SM)},
				specimenTypeRow(tokens, "Workspace title", tokens.Typography.WorkspaceTitle),
				specimenTypeRow(tokens, "Task title", tokens.Typography.TaskTitle),
				specimenTypeRow(tokens, "Section title", tokens.Typography.SectionTitle),
				specimenTypeRow(tokens, "Panel heading", tokens.Typography.PanelHeading),
				specimenTypeRow(tokens, "Body", tokens.Typography.Body),
				specimenTypeRow(tokens, "Compact metadata", tokens.Typography.CompactBody),
				specimenTypeRow(tokens, "Control label", tokens.Typography.ControlLabel),
				specimenTypeRow(tokens, "Metric 12,345.67", tokens.Typography.MetricValue),
				specimenTypeRow(tokens, "Code: go test ./...", tokens.Typography.Code),
			),
		),
		specimenSection(
			tokens,
			"Interaction",
			html.P(
				html.Props{Class: specimenBodyClass(tokens)},
				html.Text(fmt.Sprintf(
					"Pointer target %dpx · row %dpx · focus ring %dpx + %dpx offset · control %s",
					tokens.Interaction.MinimumPointerTarget,
					tokens.Rhythm.RowHeight,
					tokens.Geometry.FocusRingWidth,
					tokens.Geometry.FocusRingOffset,
					tokens.Motion.Control,
				)),
			),
		),
		specimenSection(
			tokens,
			"Elevation hierarchy",
			html.P(
				html.Props{Class: specimenBodyClass(tokens)},
				html.Text(fmt.Sprintf(
					"Resting %dpx blur · floating %dpx blur · modal %dpx blur",
					tokens.Elevation.Resting.Blur,
					tokens.Elevation.Floating.Blur,
					tokens.Elevation.Modal.Blur,
				)),
			),
		),
	)
}

func specimenSection(tokens Tokens, heading string, child ui.Node) ui.Node {
	return html.Section(
		html.Props{Class: specimenSectionClass(tokens)},
		html.H2(
			html.Props{Class: specimenPanelHeadingClass(tokens)},
			html.Text(heading),
		),
		child,
	)
}

func specimenTypeRow(tokens Tokens, label string, style TypeStyle) ui.Node {
	rules := []css.Rule{
		css.Margin(css.Zero),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.FontSize(css.Px(style.Size)),
		css.LineHeightLen(css.Px(style.LineHeight)),
		specimenFontWeight(style.Weight),
	}
	if style.Tabular {
		rules = append(rules, css.FontVariantNumeric.TabularNums)
	}
	if style == tokens.Typography.Code {
		rules = append(rules, css.Font(css.FontStack(tokens.Fonts.Code)))
	} else {
		rules = append(rules, css.Font(css.FontStack(tokens.Fonts.UI)))
	}
	return html.P(html.Props{Class: css.New(rules...).String()}, html.Text(label))
}

func specimenRootClass(tokens Tokens) string {
	return css.New(
		u.Flex,
		u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.LG)),
		css.MinHeight(css.Vh(100)),
		css.Padding(css.Px(tokens.Spacing.XL)),
		css.Bg(css.Hex(string(tokens.Colors.Canvas))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.UI)),
	).String()
}

func specimenSectionClass(tokens Tokens) string {
	return css.New(
		u.Flex,
		u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.MD)),
		css.Padding(css.Px(tokens.Rhythm.PanelInset)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.Border(
			css.Px(tokens.Geometry.BorderWidth),
			css.Hex(string(tokens.Colors.BorderSubtle)),
		),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
	).String()
}

func specimenGridClass(tokens Tokens) string {
	return css.New(
		u.Flex,
		u.FlexWrap.Wrap,
		css.Gap(css.Px(tokens.Spacing.SM)),
		css.Margin(css.Zero),
		css.Padding(css.Zero),
	).String()
}

func specimenSwatchClass(tokens Tokens, _ Color) string {
	return css.New(
		u.Flex,
		u.ItemsCenter,
		css.Gap(css.Px(tokens.Spacing.SM)),
		css.MinWidth(css.Px(260)),
		css.Padding(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	).String()
}

func specimenSwatchChipClass(color Color) string {
	return css.New(
		css.W(css.Px(24)),
		css.H(css.Px(24)),
		css.Bg(css.Hex(string(color))),
		css.Border(css.Px(1), css.Hex("#ffffff")),
		css.Rounded(css.Px(4)),
	).String()
}

func specimenStatusClass(presentation StatusPresentation) string {
	return css.New(
		u.Flex,
		u.ItemsCenter,
		css.Gap(css.Px(8)),
		css.PaddingX(css.Px(12)),
		css.PaddingY(css.Px(8)),
		css.Bg(css.Hex(string(presentation.Background))),
		css.TextColor(css.Hex(string(presentation.Foreground))),
		css.Border(css.Px(2), css.Hex(string(presentation.Foreground))),
		css.Rounded(css.Px(999)),
	).String()
}

func specimenStackClass(gap int) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(gap))).String()
}

func specimenHeadingClass(tokens Tokens) string {
	style := tokens.Typography.TaskTitle
	return css.New(
		css.Margin(css.Zero),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.Display)),
		css.FontSize(css.Px(style.Size)),
		css.LineHeightLen(css.Px(style.LineHeight)),
		specimenFontWeight(style.Weight),
	).String()
}

func specimenPanelHeadingClass(tokens Tokens) string {
	style := tokens.Typography.PanelHeading
	return css.New(
		css.Margin(css.Zero),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.FontSize(css.Px(style.Size)),
		css.LineHeightLen(css.Px(style.LineHeight)),
		specimenFontWeight(style.Weight),
	).String()
}

func specimenBodyClass(tokens Tokens) string {
	style := tokens.Typography.Body
	return css.New(
		css.Margin(css.Zero),
		css.MaxWidth(css.Ch(85)),
		css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		css.FontSize(css.Px(style.Size)),
		css.LineHeightLen(css.Px(style.LineHeight)),
	).String()
}

func specimenFontWeight(weight int) css.Rule {
	switch weight {
	case 500:
		return css.FontWeight.Medium
	case 600:
		return css.FontWeight.Semibold
	case 700:
		return css.FontWeight.Bold
	default:
		return css.FontWeight.Normal
	}
}
