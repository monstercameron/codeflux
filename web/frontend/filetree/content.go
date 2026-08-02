package filetree

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

// contentRegion is the file a person opened.
func contentRegion(props Props, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Open file"},
		Data: map[string]string{
			"component": "file-content",
			"state":     string(stateOrUnavailable(props.ContentState)),
			"path":      props.SelectedPath,
		},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.H(css.Full), css.Overflow.Hidden,
			css.Padding(css.RawLength("0 0 0 20px")),
			css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		).String(),
	}, contentBody(props, mode)...)
}

func contentBody(props Props, mode primitives.Mode) []ui.Node {
	tokens := props.Tokens
	switch stateOrUnavailable(props.ContentState) {
	case LoadLoading:
		return []ui.Node{primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Reading the file", Tone: design.StatusNeutral, Mode: mode,
			Message: "Reading it from the working tree at this revision.",
		})}
	case LoadFailed:
		return []ui.Node{primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "File unavailable", Tone: design.StatusFailure, Mode: mode,
			Message: failureBody(props.ContentError),
		})}
	}
	if props.Content == nil {
		return []ui.Node{html.P(html.Props{
			Class: proseClass(tokens),
			Text:  "Choose a file to read it.",
		})}
	}
	content := *props.Content
	children := []ui.Node{fileHeading(content, tokens)}
	if len(content.Declarations) > 0 {
		children = append(children, declarationStrip(props, content, tokens))
	}
	children = append(children, sourceView(props, content, tokens))
	if content.Truncated {
		children = append(children, html.P(html.Props{
			Class: proseClass(tokens),
			Text: "This file continues past what was read. The read stops so a large " +
				"file cannot be handed to the browser whole.",
		}))
	}
	return children
}

func fileHeading(content Content, tokens design.Tokens) ui.Node {
	facts := []string{countPhrase(int(content.Lines), "line")}
	if content.File.ImportPath != "" {
		facts = append(facts, content.File.ImportPath)
	}
	if content.File.Generated {
		facts = append(facts, "generated")
	}
	return html.Div(html.Props{Class: css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(2)), css.FlexShrink(css.Num(0)),
	).String()},
		html.H2(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
				css.FontWeight.Normal,
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.OverflowWrap.Anywhere,
			).String(),
			Text: content.File.Path,
		}),
		html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: strings.Join(facts, " · "),
		}),
	)
}

// declarationStrip lists what the file declares, in source order, as a way to
// jump into it. An atom is marked, because that is the declaration a reader is
// most often looking for.
func declarationStrip(props Props, content Content, tokens design.Tokens) ui.Node {
	chips := make([]ui.Node, 0, len(content.Declarations))
	for _, declaration := range content.Declarations {
		entry := declaration
		buttonProps := html.PropsOf(html.OnClick(func() {
			if props.OnSelectLine != nil {
				props.OnSelectLine(entry.Line)
			}
		}))
		buttonProps.Type = "button"
		buttonProps.Title = entry.Kind + " at line " + strconv.FormatUint(uint64(entry.Line), 10)
		buttonProps.Data = map[string]string{
			"component": "file-declaration", "declaration": entry.Name,
			"atom": boolText(entry.Atom),
		}
		rules := []css.Rule{
			u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(4)),
			css.PaddingX(css.Px(8)), css.PaddingY(css.Px(2)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.PillRadius)),
			css.Font(css.FontStack(tokens.Fonts.Code)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			css.Cursor.Pointer,
		}
		if entry.Atom {
			rules = append(rules,
				css.BorderColor(css.Hex(string(tokens.Colors.Active))),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			)
		}
		rules = append(rules, css.Hover(css.Bg(css.Hex(string(tokens.Colors.Surface2))))...)
		buttonProps.Class = css.New(rules...).String()
		chips = append(chips, html.Button(buttonProps, html.Text(declarationLabel(entry))))
	}
	return html.Div(html.Props{
		Role: "group",
		Aria: map[string]string{"label": "Declarations in this file"},
		Data: map[string]string{"component": "file-declarations"},
		Class: css.New(
			u.Flex, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.XS)),
			// The strip keeps its own height and the source pane below it
			// absorbs what is left. Without this the column squeezes the
			// strip instead, and the chips are drawn clipped in half.
			css.FlexShrink(css.Num(0)),
			css.MaxHeight(css.Px(96)), css.OverflowY.Auto,
		).String(),
	}, chips...)
}

func declarationLabel(declaration Declaration) string {
	if receiver := strings.TrimSpace(declaration.Receiver); receiver != "" {
		return "(" + receiver + ") " + declaration.Name
	}
	return declaration.Name
}

// sourceView draws the file with its own line numbers, and marks the line a
// person jumped to so the click has somewhere to land.
func sourceView(props Props, content Content, tokens design.Tokens) ui.Node {
	syntax := design.SyntaxColorsFor(tokens)
	lines := highlightsFor(content.File.Kind, content.File.Path, content.Text)
	rows := make([]ui.Node, 0, len(lines))
	for index, spans := range lines {
		number := uint32(index + 1)
		background := css.Transparent
		if props.SelectedLine == number && number > 0 {
			background = css.Hex(string(tokens.Colors.Surface2))
		}
		rows = append(rows, html.Div(html.Props{
			Data: map[string]string{"line": strconv.FormatUint(uint64(number), 10)},
			Class: css.New(
				u.Flex, css.Gap(css.Px(tokens.Spacing.SM)),
				css.Bg(background),
			).String(),
		},
			html.Span(html.Props{
				Aria: map[string]string{"hidden": "true"},
				Class: css.New(
					css.FlexShrink(css.Num(0)), css.MinWidth(css.Px(40)),
					css.TextAlign.Right,
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.UserSelect.None,
				).String(),
				Text: strconv.FormatUint(uint64(number), 10),
			}),
			sourceLine(spans, syntax),
		))
	}
	return html.Div(html.Props{
		Data: map[string]string{"component": "file-source"},
		Class: css.New(
			css.Padding(css.RawLength("8px 10px")),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Font(css.FontStack(tokens.Fonts.Code)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
			css.FlexGrow(css.Num(1)), css.MinHeight(css.Zero),
			css.OverflowY.Auto, css.OverflowX.Auto,
		).String(),
	}, rows...)
}

// sourceLine draws one line as its coloured runs.
//
// Adjacent runs of the same class are drawn as one element, because a line of
// ordinary code is mostly plain and one element for it beats twenty.
func sourceLine(spans []Span, syntax design.SyntaxColors) ui.Node {
	pieces := make([]ui.Node, 0, len(spans))
	for index := 0; index < len(spans); {
		class := spans[index].Class
		text := spans[index].Text
		index++
		for index < len(spans) && spans[index].Class == class {
			text += spans[index].Text
			index++
		}
		pieces = append(pieces, html.Span(html.Props{
			Data:  map[string]string{"token": string(class)},
			Class: tokenClass(class, syntax),
			Text:  text,
		}))
	}
	return html.Span(html.Props{
		Class: css.New(css.WhiteSpace.Pre, css.MinWidth(css.Zero)).String(),
	}, pieces...)
}

func tokenClass(class TokenClass, syntax design.SyntaxColors) string {
	rules := []css.Rule{css.WhiteSpace.Pre}
	switch class {
	case ClassComment:
		// Dimmed but upright. Atom documentation means a well-documented file
		// is more comment than code, and a screen of italics is tiring to read
		// where a screen of quiet grey is not.
		rules = append(rules, css.TextColor(css.Hex(string(syntax.Comment))))
	case ClassKeyword:
		rules = append(rules,
			css.TextColor(css.Hex(string(syntax.Keyword))), css.FontWeight.Medium)
	case ClassLiteral:
		rules = append(rules, css.TextColor(css.Hex(string(syntax.Literal))))
	case ClassBuiltin:
		rules = append(rules, css.TextColor(css.Hex(string(syntax.Builtin))))
	case ClassDeclared:
		rules = append(rules,
			css.TextColor(css.Hex(string(syntax.Declared))), css.FontWeight.Semibold)
	case ClassFunction:
		rules = append(rules, css.TextColor(css.Hex(string(syntax.Function))))
	case ClassPunctuation:
		rules = append(rules, css.TextColor(css.Hex(string(syntax.Punctuation))))
	default:
		rules = append(rules, css.TextColor(css.Hex(string(syntax.Plain))))
	}
	return css.New(rules...).String()
}

func proseClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero),
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
	).String()
}
