package primitives

import (
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// IconChipProps configures the small, solid, per-kind marker that color-codes
// a conversation card, graph node summary, or list row by its typed content
// category (design.Kind) rather than by its workflow Status. Kind color is
// always paired with a redundant icon glyph and an explicit Label so the
// distinction never depends on color alone.
type IconChipProps struct {
	Label string
	Kind  design.Kind
	Mode  Mode
}

// IconChip renders the solid kind-colored chip used as the primary visual
// hierarchy device on typed timeline cards.
func IconChip(props IconChipProps) ui.Node {
	if strings.TrimSpace(props.Label) == "" {
		return contractError("icon-chip", "label is required")
	}
	tokens := props.Mode.Tokens()
	presentation, err := design.KindPresentationFor(props.Kind, tokens)
	if err != nil {
		return contractError("icon-chip", err.Error())
	}
	return html.Span(
		html.Props{
			Title: props.Label,
			Data: map[string]string{
				"component": "icon-chip",
				"kind":      string(props.Kind),
			},
			Class: css.New(
				u.InlineFlex,
				u.ItemsCenter,
				css.Gap(css.Px(tokens.Spacing.XS)),
				css.PaddingX(css.Px(tokens.Spacing.SM)),
				css.PaddingY(css.Px(2)),
				css.Bg(css.Hex(string(presentation.Background))),
				css.TextColor(css.Hex(string(presentation.Foreground))),
				css.Rounded(css.Px(tokens.Geometry.PillRadius)),
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
				css.FontWeight.Semibold,
			).String(),
		},
		html.Span(
			html.Props{Aria: map[string]string{"hidden": "true"}},
			html.Text(presentation.Icon),
		),
		html.Text(props.Label),
	)
}
