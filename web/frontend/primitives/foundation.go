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
	return controlVariantClass(tokens, primary, false, false)
}

// controlVariantClass styles the three weights a control can carry: the filled
// key that commits an action, the outlined default, and the quiet one that
// belongs to its surface.
//
// compact drops the pointer-target floor, for an action that sits inside a
// dense readout beside values rather than standing on its own. The floor is
// sized for a fingertip; in a row of settings it makes the one action half
// again as tall as everything around it, which reads as the loudest thing on a
// page it is not the subject of.
func controlVariantClass(tokens design.Tokens, primary, quiet, compact bool) string {
	if quiet {
		return quietControlClass(tokens)
	}
	background := tokens.Colors.Surface2
	hoverBackground := tokens.Colors.Surface3
	pressedBackground := tokens.Colors.SurfaceInset
	foreground := tokens.Colors.TextPrimary
	border := tokens.Colors.BorderSubtle
	if primary {
		background = tokens.Colors.Accent
		hoverBackground = tokens.Colors.AccentHover
		pressedBackground = tokens.Colors.AccentPressed
		foreground = tokens.Colors.OnAccent
		border = tokens.Colors.Accent
	}
	floor := tokens.Interaction.MinimumPointerTarget
	if compact {
		floor = 30
	}
	rules := []css.Rule{
		u.InlineFlex,
		u.ItemsCenter,
		u.JustifyCenter,
		css.Gap(css.Px(tokens.Spacing.SM)),
		css.MinHeight(css.Px(floor)),
		css.MinWidth(css.Px(floor)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(background))),
		css.TextColor(css.Hex(string(foreground))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(border))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.ControlLabel.LineHeight)),
		css.FontWeight.Medium,
		css.Cursor.Pointer,
	}
	if tokens.Motion.Control > 0 {
		rules = append(rules, css.Transition(
			css.TransitionProps(css.PropColors, css.PropTransform, css.PropShadow),
			css.Ms(int(tokens.Motion.Control.Milliseconds())),
			css.EaseOut,
		))
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(hoverBackground))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
	)...)
	if !tokens.ReducedMotion {
		rules = append(rules, css.Hover(
			css.Transform(css.TranslateY(css.Px(-1))),
			css.Shadow(elevationShadow(tokens, tokens.Elevation.Resting)),
		)...)
	}
	activeRules := []css.Rule{css.Bg(css.Hex(string(pressedBackground)))}
	if !tokens.ReducedMotion {
		activeRules = append(activeRules, css.Transform(css.TranslateY(css.Zero)))
	}
	rules = append(rules, css.Active(activeRules...)...)
	rules = append(rules, css.Disabled(
		css.OpacityNum(css.Num(0.5)), css.Cursor.NotAllowed, css.Shadow(css.ShadowNone), css.Transform(css.TranslateY(css.Zero)),
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
	)...)
	return css.New(rules...).String()
}

// segmentClass styles one option inside a segmented control.
//
// Three filters drawn as three full-weight buttons read as three unrelated
// commands. As segments they read as one control with one option chosen, which
// is what they are.
// segmentClass styles one option of a segmented control.
//
// compact drops the pointer-target floor. The floor is sized for a fingertip
// and is right wherever a control stands on its own; inside a dense readout it
// makes every control half again as tall as the text it sits beside, which is
// what a mouse-driven document does not need and what makes a page of them look
// oversized. The declared height is the same either way.
func segmentClass(tokens design.Tokens, pressed bool, compact bool) string {
	background := css.Transparent
	foreground := tokens.Colors.TextMuted
	weight := css.FontWeight.Medium
	if pressed {
		background = css.Hex(string(tokens.Colors.Surface3))
		foreground = tokens.Colors.TextPrimary
		weight = css.FontWeight.Semibold
	}
	floor := tokens.Interaction.MinimumPointerTarget
	if compact {
		floor = 30
	}
	rules := []css.Rule{
		u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
		css.MinHeight(css.Px(floor)),
		css.H(css.Px(30)),
		css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.Bg(background),
		css.TextColor(css.Hex(string(foreground))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Transparent),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		weight,
		css.Cursor.Pointer,
	}
	if tokens.Motion.Control > 0 {
		rules = append(rules, css.Transition(
			css.TransitionProps(css.PropColors),
			css.Ms(int(tokens.Motion.Control.Milliseconds())),
			css.EaseOut,
		))
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
	)...)
	rules = append(rules, css.Disabled(
		css.OpacityNum(css.Num(0.5)), css.Cursor.NotAllowed,
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(-tokens.Geometry.FocusRingOffset)),
	)...)
	return css.New(rules...).String()
}

func quietControlClass(tokens design.Tokens) string {
	rules := []css.Rule{
		u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
		css.Gap(css.Px(tokens.Spacing.XS)),
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.MinWidth(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Transparent),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Transparent),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.FontWeight.Semibold,
		css.Cursor.Pointer,
	}
	if tokens.Motion.Control > 0 {
		rules = append(rules, css.Transition(
			css.TransitionProps(css.PropColors),
			css.Ms(int(tokens.Motion.Control.Milliseconds())),
			css.EaseOut,
		))
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(tokens.Colors.Surface3))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
	)...)
	rules = append(rules, css.Active(css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))))...)
	rules = append(rules, css.Disabled(
		css.OpacityNum(css.Num(0.5)), css.Cursor.NotAllowed,
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	return css.New(rules...).String()
}

func surfaceClass(tokens design.Tokens) string {
	return surfaceClassAt(tokens, tokens.Elevation.Resting)
}

func surfaceClassAt(tokens design.Tokens, elevation design.Elevation) string {
	rules := []css.Rule{
		u.Flex,
		u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.MD)),
		css.MaxWidth(css.Percent(100)),
		css.Padding(css.Px(tokens.Rhythm.PanelInset)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.Shadow(elevationShadow(tokens, elevation)),
	}
	rules = append(rules, css.Media(
		css.MaxW(799),
		css.Padding(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	)...)
	return css.New(rules...).String()
}

func elevationShadow(tokens design.Tokens, elevation design.Elevation) css.ShadowToken {
	red, green, blue := 3, 12, 20
	if tokens.Theme == design.ThemeLight {
		red, green, blue = 34, 57, 71
	}
	return css.ShadowOf(
		css.Zero, css.Px(elevation.OffsetY), css.Px(elevation.Blur), css.Px(elevation.Spread),
		css.RGBA(red, green, blue, elevation.Opacity),
	)
}
