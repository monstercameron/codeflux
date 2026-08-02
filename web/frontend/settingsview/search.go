package settingsview

import (
	"sort"
	"strings"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Search finds one setting on a sheet with too many to scan.
//
// It jumps rather than filters. Filtering would answer "which settings match
// this word" and leave somebody reading a page whose shape changed under them;
// jumping answers the question they actually have — where is the one I came
// for — and puts them in front of it with the rest of the configuration still
// around it, which is the context that makes a setting worth changing.

// searchResultLimit bounds the list. Past a handful, a result list is another
// thing to scan rather than an answer.
const searchResultLimit = 6

// SearchHit is one setting a query named.
type SearchHit struct {
	Key     string
	Label   string
	Group   string
	Section string
	// Matched says where the query was found, so a result that matched on the
	// rationale rather than the name says so instead of looking arbitrary.
	Matched string
	rank    int
}

// searchBox renders the query field and whatever it found.
func searchBox(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	fieldProps := html.PropsOf(html.OnInput(func(event ui.InputEvent) {
		if props.OnSearch != nil {
			props.OnSearch(event.GetValue())
		}
	}))
	fieldProps.ID = "settings-search"
	fieldProps.Type = "search"
	fieldProps.Value = props.Search
	fieldProps.Placeholder = "Find a setting"
	fieldProps.Disabled = props.OnSearch == nil
	fieldProps.Aria = map[string]string{
		"label": "Find a setting by name, group, or what it does",
	}
	fieldProps.Data = map[string]string{"component": "settings-search"}
	fieldProps.Class = css.New(
		css.Appearance.None,
		css.W(css.Full), css.MaxWidth(css.Px(420)),
		css.MinHeight(css.Px(fieldHeight)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.Code.Size)),
		css.Border(
			css.Px(tokens.Geometry.BorderWidth),
			css.Hex(string(tokens.Colors.BorderSubtle)),
		),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	).String()
	children := []ui.Node{html.Input(fieldProps)}
	if strings.TrimSpace(props.Search) != "" {
		children = append(children, searchResults(props))
	}
	return html.Div(
		html.Props{
			Role: "search",
			Class: css.New(
				u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
				css.MinWidth(css.Zero),
			).String(),
		},
		children...,
	)
}

// searchResults lists what the query found, or says it found nothing.
func searchResults(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	hits := SearchSettings(props, props.Search)
	if len(hits) == 0 {
		return html.P(html.Props{
			Text: "No setting matches " + strings.TrimSpace(props.Search) + ".",
			Aria: map[string]string{"live": "polite"},
			Data: map[string]string{"component": "settings-search-empty"},
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Reading)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.Margin(css.Zero),
			).String(),
		})
	}
	items := make([]ui.Node, 0, len(hits))
	for _, hit := range hits {
		key := hit.Key
		buttonProps := html.PropsOf(html.OnClick(func() {
			if props.OnJump != nil {
				props.OnJump(key)
			}
		}))
		buttonProps.Type = "button"
		buttonProps.Aria = map[string]string{
			"label": "Go to " + hit.Label + " in " + hit.Section,
		}
		buttonProps.Data = map[string]string{
			"component": "settings-search-result", "setting": hit.Key,
		}
		buttonProps.Disabled = props.OnJump == nil
		buttonProps.Class = searchResultClass(props)
		// A div carrying list semantics rather than a list element: the
		// marker on a real list item is drawn by the browser's own stylesheet
		// and outranks anything set here, and a bullet beside a place to go
		// reads as a bullet point.
		items = append(items, html.Div(html.Props{Role: "listitem"},
			html.Button(buttonProps,
				html.Span(html.Props{
					Text: hit.Section + " · " + hit.Group,
					Class: css.New(
						css.Font(css.FontStack(tokens.Fonts.Code)),
						css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
						css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					).String(),
				}),
				html.Span(html.Props{
					Text: hit.Label,
					Class: css.New(
						css.Font(css.FontStack(tokens.Fonts.UI)),
						css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
						css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)),
						css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					).String(),
				}),
				html.Span(html.Props{
					Text: hit.Matched,
					Class: css.New(
						css.Font(css.FontStack(tokens.Fonts.Code)),
						css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
						css.TextColor(css.Hex(string(tokens.Colors.TextDisabled))),
					).String(),
				}),
			),
		))
	}
	return html.Div(
		html.Props{
			Role: "list",
			Aria: map[string]string{"label": "Settings found"},
			Data: map[string]string{"component": "settings-search-results"},
			Class: css.New(
				u.Flex, u.FlexCol, css.Gap(css.Px(2)),
				css.MaxWidth(css.Px(420)),
				css.Padding(css.Px(4)),
				css.Bg(css.Hex(string(tokens.Colors.Surface2))),
				css.Border(
					css.Px(tokens.Geometry.BorderWidth),
					css.Hex(string(tokens.Colors.BorderSubtle)),
				),
				css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
			).String(),
		},
		items...,
	)
}

// searchResultClass styles one result as a row that can be entered.
func searchResultClass(props Props) string {
	tokens := props.Mode.Tokens()
	return css.New(
		u.Flex, u.FlexCol, u.ItemsStart, css.Gap(css.Px(1)),
		css.W(css.Full),
		css.MinHeight(css.Px(fieldHeight)),
		css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.PaddingY(css.Px(6)),
		css.Bg(css.Transparent),
		css.Border(css.Zero, css.Transparent),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
		css.TextAlign.Left,
		css.Cursor.Pointer,
	).String()
}

// SearchSettings ranks the settings a query names.
//
// A name match beats a group match, which beats a match inside the rationale,
// because somebody typing "attempts" wants the attempt ceiling, not every
// setting whose explanation mentions attempts.
func SearchSettings(props Props, query string) []SearchHit {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil
	}
	hits := []SearchHit{}
	for _, setting := range props.Flow {
		hit, found := matchSetting(setting, needle)
		if found {
			hits = append(hits, hit)
		}
	}
	sort.SliceStable(hits, func(first, second int) bool {
		if hits[first].rank != hits[second].rank {
			return hits[first].rank < hits[second].rank
		}
		return hits[first].Label < hits[second].Label
	})
	if len(hits) > searchResultLimit {
		hits = hits[:searchResultLimit]
	}
	return hits
}

// matchSetting reports whether and where one setting matches.
func matchSetting(setting FlowSetting, needle string) (SearchHit, bool) {
	hit := SearchHit{
		Key: setting.Key, Label: setting.Label, Group: setting.Group,
		Section: "Run behaviour",
	}
	label := strings.ToLower(setting.Label)
	switch {
	case strings.HasPrefix(label, needle):
		hit.rank, hit.Matched = 0, "name"
	case strings.Contains(label, needle):
		hit.rank, hit.Matched = 1, "name"
	case strings.Contains(strings.ToLower(setting.Key), needle):
		hit.rank, hit.Matched = 2, setting.Key
	case strings.Contains(strings.ToLower(setting.Group), needle):
		hit.rank, hit.Matched = 3, "group"
	case strings.Contains(strings.ToLower(setting.Help), needle):
		hit.rank, hit.Matched = 4, "what it does"
	default:
		return SearchHit{}, false
	}
	return hit, true
}

var _ = primitives.Mode{}
