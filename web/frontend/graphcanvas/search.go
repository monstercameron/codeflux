package graphcanvas

import (
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// authoritativeSearchBox lets a person find a graph node by name or source
// path instead of panning and zooming to look for it by eye.
//
// It searches the server's authoritative graph (SearchGraph), not only the
// bounded slice this view currently has loaded, so it can name a match this
// view has never drawn. Choosing a match that is already part of the loaded
// Layout selects and centers it the same way a chat citation would; a match
// outside the loaded slice cannot be placed on screen, and SearchStatus is
// expected to say so rather than the control silently doing nothing.
func authoritativeSearchBox(props AuthoritativeProps) ui.Node {
	tokens := props.VisualMode.Tokens()
	fieldProps := html.PropsOf(html.OnInput(func(event ui.InputEvent) {
		if props.OnSearchQueryChange != nil {
			props.OnSearchQueryChange(event.GetValue())
		}
	}))
	fieldProps.ID = "graph-search"
	fieldProps.Type = "search"
	fieldProps.Value = props.SearchQuery
	fieldProps.Placeholder = "Find a node by name or path"
	fieldProps.Disabled = props.OnSearchQueryChange == nil
	fieldProps.Aria = map[string]string{"label": "Find a graph node by name or source path"}
	fieldProps.Data = map[string]string{"component": "graph-search"}
	fieldProps.Class = graphSearchFieldClass(tokens)

	children := []ui.Node{html.Input(fieldProps)}
	if strings.TrimSpace(props.SearchQuery) != "" {
		children = append(children, authoritativeSearchResults(props))
	}
	return html.Div(html.Props{
		Role: "search",
		Aria: map[string]string{"label": "Find a graph node"},
		Data: map[string]string{"component": "graph-search-box"},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
			css.MinWidth(css.Px(220)), css.MaxWidth(css.Px(320)),
		).String(),
	}, children...)
}

// authoritativeSearchResults renders the loading, empty, and populated states
// of the most recent search — the three states any search box can reach,
// per AGENTS.md's coverage requirement for a data-owning control.
func authoritativeSearchResults(props AuthoritativeProps) ui.Node {
	tokens := props.VisualMode.Tokens()
	statusText := strings.TrimSpace(props.SearchStatus)
	switch {
	case props.SearchLoading:
		return html.P(html.Props{
			Role: "status", Aria: map[string]string{"live": "polite"},
			Data:  map[string]string{"component": "graph-search-status"},
			Class: graphSearchStatusClass(tokens), Text: "Searching the graph.",
		})
	case len(props.SearchResults) == 0:
		text := "No graph node matches " + strings.TrimSpace(props.SearchQuery) + "."
		if statusText != "" {
			text = statusText
		}
		return html.P(html.Props{
			Role: "status", Aria: map[string]string{"live": "polite"},
			Data:  map[string]string{"component": "graph-search-empty"},
			Class: graphSearchStatusClass(tokens), Text: text,
		})
	}
	items := make([]ui.Node, 0, len(props.SearchResults)+1)
	if statusText != "" {
		items = append(items, html.P(html.Props{
			Role: "status", Aria: map[string]string{"live": "polite"},
			Data:  map[string]string{"component": "graph-search-status"},
			Class: graphSearchStatusClass(tokens), Text: statusText,
		}))
	}
	for _, node := range props.SearchResults {
		nodeID := node.ID()
		label := graphClassLabel(node.Class()) + " · " + node.DisplayName()
		items = append(items, html.Div(html.Props{Role: "listitem"},
			primitives.Button(primitives.ButtonProps{
				Label:           label,
				AccessibleLabel: "Go to " + node.DisplayName() + ", " + graphClassLabel(node.Class()) + ", " + graphStatusLabel(node.Status()),
				Mode:            props.VisualMode,
				Quiet:           true,
				Disabled:        props.OnSearchResultSelect == nil,
				OnClick: func() {
					if props.OnSearchResultSelect != nil {
						props.OnSearchResultSelect(nodeID)
					}
				},
			}),
		))
	}
	return html.Div(html.Props{
		Role: "list", Aria: map[string]string{"label": "Graph nodes found"},
		Data:  map[string]string{"component": "graph-search-results"},
		Class: graphSearchResultsListClass(tokens),
	}, items...)
}

func graphSearchFieldClass(tokens design.Tokens) string {
	rules := []css.Rule{
		css.Appearance.None, css.W(css.Full), css.MaxWidth(css.Px(280)),
		css.MinHeight(css.Px(36)), css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	}
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	return css.New(rules...).String()
}

func graphSearchStatusClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero),
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
	).String()
}

func graphSearchResultsListClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(2)),
		css.MaxWidth(css.Px(320)), css.MaxHeight(css.Px(220)), css.Overflow.Auto,
		css.Padding(css.Px(4)), css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
	).String()
}
