package shell

import (
	"strconv"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// firstRunCard is one step of the setup sequence.
//
// It replaces a generic region wrapped around a separate content block, which
// produced a tall box with a heading at the top, a small glyph square below it,
// and a lot of nothing in between. Everything a step needs — where you are in
// the sequence, what it is, whether it is done, what it means, and what to do —
// now reads as one thing.
type firstRunCard struct {
	// Step is the position in the sequence. The numbering is kept because
	// setup genuinely is ordered: you cannot pick a repository before the
	// coordinator exists. It is set small and quiet rather than as a display
	// number, because the position matters less than the name.
	Step  int
	Icon  primitives.IconName
	Title string
	// Status is the short state word, and Tone colours it. A step that is done
	// and a step that is blocked must not read the same.
	Status string
	Tone   design.Status
	Body   string
	// ActionLabel and Path are the one thing to do here, and are empty for a
	// step that needs nothing.
	ActionLabel string
	Path        string
	Primary     bool
}

// renderFirstRunCard draws one step.
func renderFirstRunCard(props FirstRunProps, card firstRunCard) ui.Node {
	tokens := props.Mode.Tokens()
	children := []ui.Node{
		// Header: sequence position, icon, name, and state, on one line.
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.MD)),
			).String(),
		},
			html.Span(html.Props{
				Aria: map[string]string{"hidden": "true"},
				Class: css.New(
					u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
					css.W(css.Px(34)), css.H(css.Px(34)),
					css.FlexShrink(css.Num(0)),
					css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
					css.Bg(css.Hex(string(tokens.Colors.Selection))),
					css.TextColor(css.Hex(string(tokens.Colors.Accent))),
				).String(),
			}, primitives.Icon(primitives.IconProps{Name: card.Icon, Size: 17})),
			html.Div(html.Props{
				Class: css.New(
					u.Flex, u.FlexCol, css.MinWidth(css.Zero), css.FlexGrow(css.Num(1)),
				).String(),
			},
				html.Span(html.Props{
					Class: css.New(
						css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
						css.Font(css.FontStack(tokens.Fonts.Code)),
						css.FontSize(css.Px(11)),
						css.Tracking(css.Ems(0.12)),
					).String(),
					Text: "STEP " + strconv.Itoa(card.Step),
				}),
				html.H2(html.Props{
					Class: design.HeadingClass(tokens, design.HeadingSection),
					Text:  card.Title,
				}),
			),
			firstRunStatusPill(tokens, card.Status, card.Tone),
		),
		html.P(html.Props{
			Class: design.ProseClass(tokens),
			Text:  card.Body,
		}),
	}
	if card.ActionLabel != "" {
		target := card.Path
		children = append(children,
			// The action sits at the bottom of the card on its own row, so a
			// column of cards has its buttons on one line regardless of how
			// long each description runs.
			html.Div(html.Props{
				Class: css.New(u.Flex, css.PaddingY(css.Px(tokens.Spacing.XS))).String(),
			},
				primitives.Button(primitives.ButtonProps{
					Label: card.ActionLabel, Primary: card.Primary,
					Mode: props.Mode, Disabled: props.OnNavigatePath == nil,
					OnClick: func() {
						if props.OnNavigatePath != nil {
							props.OnNavigatePath(target)
						}
					},
				}),
			))
	}
	return html.Section(html.Props{
		DataAttr: html.DataAttribute{Name: "region", Value: firstRunRegionName(card.Title)},
		Aria:     map[string]string{"label": card.Title},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.Gap(css.Px(tokens.Spacing.MD)),
			css.Padding(css.Px(tokens.Rhythm.PanelInset)),
			css.MinWidth(css.Zero),
			css.Rounded(css.Px(tokens.Geometry.DialogRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(
				css.Px(tokens.Geometry.BorderWidth),
				css.Hex(string(tokens.Colors.BorderSubtle)),
			),
		).String(),
	}, children...)
}

// firstRunStatusPill renders the step's state.
//
// The tone is carried by colour and by the word together. Colour alone would
// make "Ready" and "Review required" indistinguishable to a reader who cannot
// separate them.
func firstRunStatusPill(tokens design.Tokens, label string, tone design.Status) ui.Node {
	presentation, err := design.StatusPresentationFor(tone, tokens)
	if err != nil {
		// An unrecognised tone falls back to neutral rather than to nothing: a
		// step whose state cannot be coloured must still say what it is.
		presentation, _ = design.StatusPresentationFor(design.StatusNeutral, tokens)
	}
	return html.Span(html.Props{
		Data: map[string]string{"component": "first-run-status", "tone": string(tone)},
		Class: css.New(
			u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
			css.H(css.Px(24)),
			css.PaddingX(css.Px(tokens.Spacing.SM)),
			css.Rounded(css.Px(tokens.Geometry.PillRadius)),
			css.Bg(css.Hex(string(presentation.Background))),
			css.TextColor(css.Hex(string(presentation.Foreground))),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.FontWeight.Semibold,
			css.WhiteSpace.NoWrap,
		).String(),
		Text: label,
	})
}

// firstRunRegionName derives a stable region identity from a step title, so
// existing route and browser checks keep finding the same regions.
func firstRunRegionName(title string) string {
	switch title {
	case "Local boundary":
		return "local-promise"
	case "Model provider":
		return "provider"
	case "Repository":
		return "repository"
	case "Boundaries":
		return "worktree-permissions"
	default:
		return "first-thread"
	}
}

// firstRunSubject names what a step's data state is about.
//
// The wording is the user's, not the system's: a person reading "provider
// setup is unavailable" knows what is missing, where "region 2 is unavailable"
// tells them nothing.
func firstRunSubject(title string) string {
	switch title {
	case "Local boundary":
		return "local-first setup"
	case "Model provider":
		return "provider setup"
	case "Repository":
		return "repository setup"
	case "Boundaries":
		return "worktree permissions"
	default:
		return "first Thread setup"
	}
}
