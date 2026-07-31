// Package primitives provides the small, accessible GoWebComponents controls
// shared by Codeflux application shells.
package primitives

import (
	"fmt"
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Mode carries the visual preferences every primitive must honor.
type Mode struct {
	Theme         design.Theme
	Density       design.Density
	HighContrast  bool
	ReducedMotion bool
}

// Tokens resolves Mode into validated semantic design tokens.
func (m Mode) Tokens() design.Tokens {
	theme := m.Theme
	if m.HighContrast {
		theme = design.ThemeHighContrast
	}
	tokens, err := design.TokensFor(design.Options{
		Theme:         theme,
		Density:       m.Density,
		ReducedMotion: m.ReducedMotion,
	})
	if err != nil {
		tokens, _ = design.TokensFor(design.Options{})
	}
	return tokens
}

// InteractionState is the cross-primitive state contract.
type InteractionState struct {
	Disabled bool
	Busy     bool
}

// Enabled reports whether a control may invoke its action.
func (s InteractionState) Enabled() bool { return !s.Disabled && !s.Busy }

// AccessibleName returns the explicit accessible label when supplied, otherwise
// the visible label.
func AccessibleName(visibleLabel, accessibleLabel string) string {
	if strings.TrimSpace(accessibleLabel) != "" {
		return strings.TrimSpace(accessibleLabel)
	}
	return strings.TrimSpace(visibleLabel)
}

// HasAccessibleName validates the naming contract shared by interactive controls.
func HasAccessibleName(visibleLabel, accessibleLabel string) bool {
	return AccessibleName(visibleLabel, accessibleLabel) != ""
}

// TechnicalLabel preserves a long label's full accessible and hover text even
// when VisibleLabel is a shortened rendering.
type TechnicalLabelProps struct {
	FullLabel    string
	VisibleLabel string
	Mode         Mode
}

func TechnicalLabel(props TechnicalLabelProps) ui.Node {
	full := strings.TrimSpace(props.FullLabel)
	visible := strings.TrimSpace(props.VisibleLabel)
	if visible == "" {
		visible = full
	}
	if full == "" {
		return contractError("technical-label", "full label is required")
	}
	tokens := props.Mode.Tokens()
	return html.Span(
		html.Props{
			Title: full,
			Aria:  map[string]string{"label": full},
			Data: map[string]string{
				"component":  "technical-label",
				"full-label": full,
			},
			Class: css.New(
				css.MinWidth(css.Zero),
				css.MaxWidth(css.Percent(100)),
				css.WhiteSpace.NoWrap,
				css.Overflow.Hidden,
				css.TextOverflowEllipsis(),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			).String(),
		},
		html.Text(visible),
	)
}

func contractError(component, message string) ui.Node {
	return html.Span(
		html.Props{
			Role: "alert",
			Data: map[string]string{
				"component": component,
				"state":     "invalid-contract",
			},
		},
		html.Text(fmt.Sprintf("%s unavailable: %s", component, message)),
	)
}

func stateName(state InteractionState) string {
	switch {
	case state.Busy:
		return "busy"
	case state.Disabled:
		return "disabled"
	default:
		return "ready"
	}
}

func boolARIA(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func controlClass(tokens design.Tokens, primary bool) string {
	background := tokens.Colors.Surface2
	foreground := tokens.Colors.TextPrimary
	border := tokens.Colors.BorderStrong
	if primary {
		background = tokens.Colors.Accent
		foreground = tokens.Colors.OnAccent
		border = tokens.Colors.Accent
	}
	rules := []css.Rule{
		u.InlineFlex,
		u.ItemsCenter,
		u.JustifyCenter,
		css.Gap(css.Px(tokens.Spacing.SM)),
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.MinWidth(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(background))),
		css.TextColor(css.Hex(string(foreground))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(border))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.Body.Size)),
		css.FontWeight.Medium,
		css.Cursor.Pointer,
	}
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	return css.New(rules...).String()
}

func surfaceClass(tokens design.Tokens) string {
	return css.New(
		u.Flex,
		u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.MD)),
		css.MaxWidth(css.Percent(100)),
		css.Padding(css.Px(tokens.Rhythm.PanelInset)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
	).String()
}
