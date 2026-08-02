// Package appearanceview renders the appearance controls in Settings: the
// theme, the information density, and whether motion is reduced. It owns no
// state and talks to no transport; every choice is a caller-supplied callback.
package appearanceview

import (
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Props configures the appearance controls.
//
// SystemReducesMotion is what the operating system asked for. It is carried
// separately from ReduceMotion so the "follow the system" choice can say what
// following it currently means, rather than leaving a person to guess whether
// the setting is doing anything.
type Props struct {
	Mode                primitives.Mode
	Theme               design.Theme
	Density             design.Density
	ReduceMotion        bool
	MotionFollowsSystem bool
	SystemReducesMotion bool

	// Axis lays each choice against a shared column instead of above its own
	// options. The page these controls sit on owns that geometry and passes it
	// in; a component that guessed at the widths would line up only by accident.
	Axis Axis

	OnTheme        func(design.Theme)
	OnDensity      func(design.Density)
	OnMotionFollow func()
	OnMotionReduce func(bool)
}

// Axis is the column geometry of the page holding these controls: the measure
// the label and its hint are set to, the column the options are set against,
// and the width reserved to the right of it. A zero Value stacks the label
// above its options, which is what a narrow column wants.
type Axis struct {
	Measure int
	Value   int
	Rail    int
}

// stacked reports whether the label belongs above its own options.
func (axis Axis) stacked() bool { return axis.Value <= 0 }

// Component renders the appearance controls.
//
// This section used to be the sentence "Theme, density, and motion
// preferences." with nothing under it, while the theme could only be cycled
// blindly from the top bar, compact density existed in the token system and
// nothing could select it, and reduced motion was read from the operating
// system with no way to override it. These are the three choices, named.
func Component(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	// On an axis each row carries its own rule and padding, so a gap between
	// them would double the separation the rules already draw.
	gap := tokens.Spacing.LG
	if !props.Axis.stacked() {
		gap = 0
	}
	return html.Div(html.Props{
		Data:  map[string]string{"component": "appearance-settings"},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(gap))).String(),
	},
		choiceRow(props, "Theme", "Dark is the default: a run is watched for long stretches.",
			[]choice{
				{label: "Dark", active: props.Theme == design.ThemeDark, act: themeSetter(props, design.ThemeDark)},
				{label: "Light", active: props.Theme == design.ThemeLight, act: themeSetter(props, design.ThemeLight)},
				{label: "High contrast", active: props.Theme == design.ThemeHighContrast, act: themeSetter(props, design.ThemeHighContrast)},
			}),
		choiceRow(props, "Density", "Compact tightens spacing without shrinking type or touch targets.",
			[]choice{
				{label: "Comfortable", active: props.Density != design.DensityCompact, act: densitySetter(props, design.DensityComfortable)},
				{label: "Compact", active: props.Density == design.DensityCompact, act: densitySetter(props, design.DensityCompact)},
			}),
		choiceRow(props, "Motion", motionHint(props),
			[]choice{
				{label: "Follow the system", active: props.MotionFollowsSystem, act: props.OnMotionFollow},
				{label: "Full motion", active: !props.MotionFollowsSystem && !props.ReduceMotion, act: motionSetter(props, false)},
				{label: "Reduce motion", active: !props.MotionFollowsSystem && props.ReduceMotion, act: motionSetter(props, true)},
			}),
		html.P(html.Props{
			Class: hintClass(tokens, props.Axis.Measure),
			Text:  "These choices are remembered on this machine and apply to every page.",
		}),
	)
}

// motionHint says what following the system currently produces, because
// "Follow the system" with no further information is a control whose effect
// nobody can see.
func motionHint(props Props) string {
	if props.SystemReducesMotion {
		return "This system asks for reduced motion."
	}
	return "This system does not ask for reduced motion."
}

func themeSetter(props Props, value design.Theme) func() {
	if props.OnTheme == nil {
		return nil
	}
	return func() { props.OnTheme(value) }
}

func densitySetter(props Props, value design.Density) func() {
	if props.OnDensity == nil {
		return nil
	}
	return func() { props.OnDensity(value) }
}

func motionSetter(props Props, reduce bool) func() {
	if props.OnMotionReduce == nil {
		return nil
	}
	return func() { props.OnMotionReduce(reduce) }
}

type choice struct {
	label  string
	active bool
	act    func()
}

// choiceRow lays a labelled setting beside its options.
//
// Stacked, the label and hint sit above the controls, which is what a narrow
// column wants: a label column there would leave the options wrapping in
// whatever space was left. Given an axis, the options move onto it, so a page
// of settings can be read down one column instead of hunting for each value
// wherever its own label happened to end.
func choiceRow(props Props, label, hint string, choices []choice) ui.Node {
	tokens := props.Mode.Tokens()
	controls := make([]ui.Node, 0, len(choices))
	for _, item := range choices {
		controls = append(controls, primitives.ToggleButton(primitives.ToggleButtonProps{
			Label: item.label, Pressed: item.active, Mode: props.Mode,
			// On an axis these sit in a row of values, so they are sized to the
			// row rather than to a fingertip.
			Compact:  !props.Axis.stacked(),
			Disabled: item.act == nil,
			OnToggle: func(bool) {
				if item.act != nil {
					item.act()
				}
			},
		}))
	}
	name := []ui.Node{
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
				css.FontWeight.Semibold,
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			).String(),
			Text: label,
		}),
	}
	if strings.TrimSpace(hint) != "" {
		name = append(name, html.Span(html.Props{
			Class: hintClass(tokens, props.Axis.Measure), Text: hint,
		}))
	}
	group := html.Div(html.Props{
		Role: "group",
		Aria: map[string]string{"label": label},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.FlexWrap.Wrap,
			css.Gap(css.Px(tokens.Spacing.XS)),
			css.MarginY(css.Px(tokens.Spacing.XS)),
			// Options run right to left off the axis, so the group ends where
			// every other value on the page ends however many options it holds.
			u.JustifyEnd,
			css.MinWidth(css.Zero),
		).String(),
	}, controls...)
	rowData := map[string]string{"setting": strings.ToLower(label)}
	if props.Axis.stacked() {
		return html.Div(html.Props{
			Data:  rowData,
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2))).String(),
		}, append(name, group)...)
	}
	return html.Div(html.Props{
		Data: rowData,
		Class: css.New(
			u.Grid,
			css.GridCols(
				css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
				css.TrackLen(css.Px(props.Axis.Value)),
				css.TrackLen(css.Px(props.Axis.Rail)),
			),
			css.Gap(css.Px(tokens.Spacing.MD)),
			u.ItemsStart,
			css.PaddingY(css.Px(tokens.Spacing.SM)),
			css.BorderBottom(
				css.Px(tokens.Geometry.BorderWidth),
				css.Hex(string(tokens.Colors.BorderSubtle)),
			),
			css.MinWidth(css.Zero),
		).String(),
	},
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.FlexCol, css.Gap(css.Px(2)), css.MinWidth(css.Zero),
			).String(),
		}, name...),
		group,
		// The rail is empty here and still reserved, because the axis is only
		// shared if it stays in the same place on every row of the page.
		html.Span(html.Props{}),
	)
}

func hintClass(tokens design.Tokens, measure int) string {
	rules := []css.Rule{
		css.Margin(css.Zero),
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
	}
	if measure > 0 {
		rules = append(rules, css.MaxWidth(css.Px(measure)))
	}
	return css.New(rules...).String()
}
