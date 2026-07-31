package timelineview

import (
	"strconv"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type MarkdownProps struct {
	Document timelinecard.Markdown
	Mode     primitives.Mode
	OnCopy   func(string)
}

// MarkdownView renders only the safe structural nodes produced by
// timelinecard.ParseMarkdown. It has no raw-HTML path.
func MarkdownView(props MarkdownProps) ui.Node {
	blocks := make([]ui.Node, 0, len(props.Document.Blocks))
	for index, block := range props.Document.Blocks {
		switch block.Kind {
		case timelinecard.BlockParagraph:
			blocks = append(blocks, html.P(html.Props{
				Key: indexKey("paragraph", index), Class: markdownParagraphClass(props.Mode),
			}, renderInlines(block.Inlines)...))
		case timelinecard.BlockCode:
			children := []ui.Node{
				html.Pre(html.Props{
					TabIndex: html.TabIndexZero,
					Aria:     map[string]string{"label": "Code block"},
					Data: map[string]string{
						"component": "timeline-code-block", "overflow": string(block.Overflow),
						"language": block.Language,
					},
					Class: codeClass(props.Mode, block.Overflow),
				}, html.Code(html.Props{}, html.Text(block.Code))),
			}
			if props.OnCopy != nil {
				copyText := block.CopyText
				children = append(children, primitives.Button(primitives.ButtonProps{
					Label: "Copy code", Mode: props.Mode,
					OnClick: func() { props.OnCopy(copyText) },
				}))
			}
			blocks = append(blocks, html.Div(html.Props{Key: indexKey("code", index)}, children...))
		}
	}
	return html.Div(html.Props{
		Data: map[string]string{
			"component": "safe-markdown", "blocked-links": strconv.Itoa(props.Document.BlockedLinks),
		},
		Class: stackClass(props.Mode),
	}, blocks...)
}

func renderInlines(inlines []timelinecard.Inline) []ui.Node {
	children := make([]ui.Node, 0, len(inlines))
	for _, inline := range inlines {
		switch inline.Kind {
		case timelinecard.InlineText:
			children = append(children, html.Text(inline.Text))
		case timelinecard.InlineCode:
			children = append(children, html.Code(html.Props{}, html.Text(inline.Text)))
		case timelinecard.InlineLink:
			if inline.Link == nil {
				children = append(children, html.Text(inline.Text))
				continue
			}
			children = append(children, html.A(html.Props{
				Href: inline.Link.URL, Target: "_blank", Rel: "noopener noreferrer",
			}, html.Text(inline.Text)))
		}
	}
	return children
}

type OutputProps struct {
	Output timelinecard.RedactedOutput
	Mode   primitives.Mode
	OnCopy func(string)
}

// OutputDisclosure keeps redacted raw output absent from the DOM until the
// user explicitly expands it, then loads bounded pages one at a time.
func OutputDisclosure(props OutputProps) ui.Node {
	expanded := ui.UseState(false)
	loadedPages := ui.UseState(1)
	regionID := "timeline-output-" + ui.UseId()
	if props.Output.PageCount == 0 {
		return html.Div(html.Props{
			Data: map[string]string{"component": "redacted-output", "state": "empty"},
		}, html.P(html.Props{}, html.Text(known(props.Output.Summary))))
	}
	view := props.Output
	if expanded.Get() {
		view = view.Expand()
		for view.LoadedPages < loadedPages.Get() {
			view = view.LoadNext()
		}
	}
	children := []ui.Node{
		html.P(html.Props{}, html.Text(known(props.Output.Summary))),
		primitives.Button(primitives.ButtonProps{
			Label:    map[bool]string{true: "Collapse redacted output", false: "Show redacted output"}[expanded.Get()],
			Expanded: boolPointer(expanded.Get()), Controls: regionID, Mode: props.Mode,
			OnClick: func() { expanded.Update(func(current bool) bool { return !current }) },
		}),
	}
	if expanded.Get() {
		pages := view.VisiblePages()
		pageNodes := make([]ui.Node, 0, len(pages))
		for _, page := range pages {
			pageNodes = append(pageNodes, html.Pre(html.Props{
				Key:      indexKey("output", page.Number),
				TabIndex: html.TabIndexZero,
				Aria:     map[string]string{"label": "Redacted output page " + strconv.Itoa(page.Number)},
				Data:     map[string]string{"page": strconv.Itoa(page.Number)},
				Class:    outputPageClass(props.Mode),
			}, html.Text(page.Text)))
		}
		regionChildren := []ui.Node{html.Div(html.Props{}, pageNodes...)}
		if view.LoadedPages < view.PageCount {
			regionChildren = append(regionChildren, primitives.Button(primitives.ButtonProps{
				Label: "Load next redacted output page", Mode: props.Mode,
				OnClick: func() { loadedPages.Update(func(current int) int { return current + 1 }) },
			}))
		}
		if props.OnCopy != nil {
			copyText := view.CopyText()
			regionChildren = append(regionChildren, primitives.Button(primitives.ButtonProps{
				Label: "Copy disclosed output", Mode: props.Mode,
				OnClick: func() { props.OnCopy(copyText) },
			}))
		}
		if view.Truncated {
			regionChildren = append(regionChildren, html.P(html.Props{
				Role: "status", Data: map[string]string{"state": "truncated"},
			}, html.Text("Output was truncated at the configured safe limit.")))
		}
		children = append(children, html.Div(html.Props{
			ID: regionID, Data: map[string]string{"component": "redacted-output-pages"},
		}, regionChildren...))
	}
	return html.Div(html.Props{
		Data: map[string]string{
			"component": "redacted-output",
			"state":     map[bool]string{true: "expanded", false: "collapsed"}[expanded.Get()],
			"pages":     strconv.Itoa(props.Output.PageCount),
		},
		Class: disclosureClass(props.Mode),
	}, children...)
}

func boolPointer(value bool) *bool { return &value }

func indexKey(prefix string, index int) string { return prefix + "-" + strconv.Itoa(index) }

func markdownParagraphClass(mode primitives.Mode) string {
	return css.New(css.Margin(css.Zero), css.OverflowWrap.Anywhere).String()
}

func codeClass(mode primitives.Mode, overflow timelinecard.CodeOverflow) string {
	rules := []css.Rule{
		css.Margin(css.Zero), css.Padding(css.Px(mode.Tokens().Spacing.MD)),
		css.Bg(css.Hex(string(mode.Tokens().Colors.Surface3))),
		css.Rounded(css.Px(mode.Tokens().Geometry.ControlRadius)),
		css.MaxWidth(css.Full),
	}
	if overflow == timelinecard.CodeWrap {
		rules = append(rules, css.WhiteSpace.PreWrap, css.OverflowWrap.Anywhere)
	} else {
		rules = append(rules, css.OverflowX.Auto)
	}
	return css.New(rules...).String()
}

func disclosureClass(mode primitives.Mode) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(mode.Tokens().Spacing.XS)),
		css.MinWidth(css.Zero), css.OverflowY.Visible,
	).String()
}

func outputPageClass(mode primitives.Mode) string {
	return css.New(
		css.Margin(css.Zero), css.Padding(css.Px(mode.Tokens().Spacing.MD)),
		css.Bg(css.Hex(string(mode.Tokens().Colors.Surface3))), css.OverflowX.Auto,
		css.MaxWidth(css.Full),
	).String()
}
