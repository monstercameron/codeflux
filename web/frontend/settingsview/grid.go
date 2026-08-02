package settingsview

import (
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// section is one band of the sheet: an eyebrow, a hairline, and its rows.
//
// A rule rather than a panel, because a panel fragments the vertical axis the
// values are read against, and reading them against each other is the whole
// purpose of this page.
func section(props Props, title, note string, rows []ui.Node) ui.Node {
	tokens := props.Mode.Tokens()
	header := []ui.Node{
		html.Span(html.Props{
			Text: strings.ToUpper(title),
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
				css.FontWeight.Semibold,
				css.Tracking(css.Ems(0.12)),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			).String(),
		}),
	}
	if note != "" {
		header = append(header, html.Span(html.Props{
			Text: note,
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
		}))
	}
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": title},
			Data: map[string]string{"component": "settings-section", "section": title},
			Class: css.New(
				u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
				css.MinWidth(css.Zero),
			).String(),
		},
		html.Div(
			html.Props{
				Class: css.New(
					u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.MD)),
					css.Custom("padding-bottom", strconv.Itoa(tokens.Spacing.XS)+"px"),
					css.BorderBottom(
						css.Px(tokens.Geometry.BorderWidth),
						css.Hex(string(tokens.Colors.BorderStrong)),
					),
				).String(),
			},
			header...,
		),
		html.Div(
			html.Props{
				Class: css.New(u.Flex, u.FlexCol, css.MinWidth(css.Zero)).String(),
			},
			rows...,
		),
	)
}

// rowProps is one line of the sheet.
type rowProps struct {
	Name string
	// Value sits on the shared axis. It is a node rather than text because a
	// changed value shows what it was as well as what it will be.
	Value ui.Node
	// Trailing is the control, set after the value so the axis is never pushed
	// around by a control's width.
	Trailing ui.Node
	// Rationale is the sentence that explains what the setting costs. It is
	// prose and is set in the reading face.
	Rationale string
	// Detail is the row's own state line: a bound, a default marker, a
	// credential's reference.
	Detail ui.Node
	// Below carries anything that cannot sit on one line, like an input a
	// credential is typed into.
	Below ui.Node
	// Marked is the row a search result sent somebody to. It is held until the
	// next search rather than faded on a timer, because a mark that vanishes
	// while somebody is still reading the page they were sent to is a mark
	// that answered a question they had already stopped asking.
	Marked bool
	Data   map[string]string
}

// row lays one setting against the value axis.
//
// The grid is name, value, control. The name column takes the remaining space
// so long labels wrap rather than pushing the axis, and the value column is
// fixed so every value on the page lines up.
func row(props Props, line rowProps) ui.Node {
	tokens := props.Mode.Tokens()
	data := map[string]string{"component": "settings-row"}
	for key, value := range line.Data {
		data[key] = value
	}
	nameStack := []ui.Node{
		html.Span(html.Props{
			Text: line.Name,
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Body.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			).String(),
		}),
	}
	if line.Rationale != "" {
		nameStack = append(nameStack, html.P(html.Props{
			Text:  line.Rationale,
			Class: proseClass(props, tokens.Colors.TextMuted),
		}))
	}
	cells := []ui.Node{
		html.Div(
			html.Props{
				Class: css.New(
					u.Flex, u.FlexCol, css.Gap(css.Px(2)),
					css.MinWidth(css.Zero),
				).String(),
			},
			nameStack...,
		),
		html.Div(
			html.Props{
				Class: css.New(
					u.Flex, u.FlexCol, u.ItemsEnd, css.Gap(css.Px(2)),
					css.MinWidth(css.Zero),
				).String(),
			},
			valueOrNothing(props, line),
		),
	}
	columns := []css.Track{
		css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
		css.TrackLen(css.Px(valueColumnWidth)),
		css.TrackLen(css.Px(controlColumnWidth)),
	}
	trailing := line.Trailing
	if trailing == nil {
		trailing = html.Span(html.Props{})
	}
	cells = append(cells, html.Div(
		html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyEnd,
				css.Gap(css.Px(tokens.Spacing.XS)),
				css.MinWidth(css.Zero),
			).String(),
		},
		trailing,
	))
	children := []ui.Node{html.Div(
		html.Props{
			Class: css.New(
				u.Grid,
				css.GridCols(columns...),
				css.Gap(css.Px(tokens.Spacing.MD)),
				u.ItemsStart,
				css.MinWidth(css.Zero),
			).String(),
		},
		cells...,
	)}
	if line.Below != nil {
		children = append(children, line.Below)
	}
	rules := []css.Rule{
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
		css.PaddingY(css.Px(tokens.Spacing.SM)),
		css.BorderBottom(
			css.Px(tokens.Geometry.BorderWidth),
			css.Hex(string(tokens.Colors.BorderSubtle)),
		),
		css.MinWidth(css.Zero),
	}
	if line.Marked {
		rules = append(rules,
			css.BorderLeft(css.Px(2), css.Hex(string(tokens.Colors.Accent))),
			css.Custom("padding-left", strconv.Itoa(tokens.Spacing.MD)+"px"),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		)
	}
	return html.Div(
		html.Props{Data: data, Class: css.New(rules...).String()},
		children...,
	)
}

// valueOrNothing renders the value cell and its detail line.
func valueOrNothing(props Props, line rowProps) ui.Node {
	children := []ui.Node{}
	if line.Value != nil {
		children = append(children, line.Value)
	}
	if line.Detail != nil {
		children = append(children, line.Detail)
	}
	if len(children) == 0 {
		return html.Span(html.Props{})
	}
	return html.Div(
		html.Props{
			Class: css.New(
				u.Flex, u.FlexCol, u.ItemsEnd, css.Gap(css.Px(2)),
			).String(),
		},
		children...,
	)
}

// groupLine names one of the engine's own groups inside a section.
func groupLine(props Props, group string) ui.Node {
	tokens := props.Mode.Tokens()
	return html.Div(
		html.Props{
			Data: map[string]string{"component": "settings-flow-group", "group": group},
			Class: css.New(
				css.Custom("padding-top", strconv.Itoa(tokens.Spacing.LG)+"px"),
				css.Custom("padding-bottom", strconv.Itoa(tokens.Spacing.XS)+"px"),
			).String(),
		},
		html.Span(html.Props{
			Text: group,
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
				css.FontWeight.Semibold,
				css.Tracking(css.Ems(0.08)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
		}),
	)
}

// noteLine is a sentence about a whole section rather than about one row.
func noteLine(props Props, text string) ui.Node {
	tokens := props.Mode.Tokens()
	return html.P(html.Props{
		Text: text,
		Class: css.New(
			css.Font(css.FontStack(tokens.Fonts.Reading)),
			css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)),
			css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			css.MaxWidth(css.Px(rationaleMeasure)),
			css.Custom("padding-top", strconv.Itoa(tokens.Spacing.SM)+"px"),
			css.Margin(css.Zero),
		).String(),
	})
}

// emptyLine says a section has nothing in it and why.
func emptyLine(props Props, text string) ui.Node {
	tokens := props.Mode.Tokens()
	return html.P(html.Props{
		Text: text,
		Class: css.New(
			css.Font(css.FontStack(tokens.Fonts.Reading)),
			css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)),
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			css.MaxWidth(css.Px(rationaleMeasure)),
			css.PaddingY(css.Px(tokens.Spacing.MD)),
			css.Margin(css.Zero),
		).String(),
	})
}

// proseClass sets the reading face at a readable measure.
func proseClass(props Props, color design.Color) string {
	tokens := props.Mode.Tokens()
	return css.New(
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)),
		css.TextColor(css.Hex(string(color))),
		css.MaxWidth(css.Px(rationaleMeasure)),
		css.Margin(css.Zero),
	).String()
}

// valueTextClass sets a value on the axis in the machine's voice.
//
// It is set at the size of the name beside it, in the code face rather than the
// interface face. The mono face is wider and more even, so at the same size it
// still reads as the value and the name still reads as the subject; set two
// steps larger, as a dashboard metric, sixty of them shout at once and the page
// stops looking like a document.
func valueTextClass(props Props, known bool) string {
	tokens := props.Mode.Tokens()
	color := tokens.Colors.TextPrimary
	if !known {
		color = tokens.Colors.TextMuted
	}
	return css.New(
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.Body.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
		css.TextColor(css.Hex(string(color))),
		css.TextAlign.Right,
	).String()
}

// contractValueClass sets the four facts that open the sheet.
//
// They are the one place the page raises its voice, because they are the answer
// to the question somebody opened it with. Everything below them is read
// against them, which only works if they are the largest thing on the page and
// nothing else competes.
func contractValueClass(props Props, known bool) string {
	tokens := props.Mode.Tokens()
	color := tokens.Colors.TextPrimary
	if !known {
		color = tokens.Colors.TextMuted
	}
	return css.New(
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
		css.TextColor(css.Hex(string(color))),
		css.TextAlign.Right,
	).String()
}

// detailClass sets the small line under a value: a bound, a default marker.
func detailClass(props Props, color design.Color) string {
	tokens := props.Mode.Tokens()
	return css.New(
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
		css.TextColor(css.Hex(string(color))),
		css.TextAlign.Right,
	).String()
}

// readOnlyRow reports a value nothing on this page can change.
func readOnlyRow(props Props, name, value string) ui.Node {
	return row(props, rowProps{
		Name:  name,
		Value: html.Span(html.Props{Text: value, Class: valueTextClass(props, value != "")}),
		Data:  map[string]string{"readonly": "true"},
	})
}

// countOf renders a count with wording that agrees with it.
func countOf(count int, singular, plural string) string {
	noun := plural
	if count == 1 {
		noun = singular
	}
	return strconv.Itoa(count) + " " + noun
}

// availabilityWord says whether the coordinator could use something now.
func availabilityWord(available bool) string {
	if available {
		return "usable"
	}
	return "no credential"
}

var _ = primitives.Mode{}
