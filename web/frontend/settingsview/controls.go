package settingsview

import (
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The controls on this sheet are one vocabulary, not three.
//
// The first pass stripped their chrome so values would sit on the axis, and
// stripped too much: a switch became the bare word "on" with nothing to say it
// could be clicked, and a number kept the browser's own spinner arrows crammed
// against its last digit. A control a person cannot see is worse than a loud
// one — it reads as a value that cannot be changed.
//
// Every discrete value — a posture, a switch — is now one segmented control,
// and it stands on the axis so the value is not printed twice. A number is a
// stepper: the digits stay editable and gain a decrement and an increment that
// respect the bound the engine declared.

// segmentedWidthBudget is how many characters of options fit on the axis.
//
// Beyond it a segmented control would push the value column out of alignment,
// so the row falls back to reporting the value and offering the options
// underneath it.
const segmentedWidthBudget = 26

// discreteControl renders a posture or a switch as one segmented control.
func discreteControl(props Props, setting FlowSetting, options []string) ui.Node {
	tokens := props.Mode.Tokens()
	segments := make([]ui.Node, 0, len(options))
	for index, option := range options {
		value := option
		selected := discreteSelected(setting, option)
		segmentProps := html.PropsOf(html.OnClick(func() {
			applyDiscrete(props, setting, value)
		}))
		segmentProps.Type = "button"
		segmentProps.ID = segmentID(setting.Key, index)
		segmentProps.Aria = map[string]string{
			"label":   "Set " + setting.Label + " to " + option,
			"pressed": boolLabel(selected),
		}
		segmentProps.Disabled = discreteDisabled(props, setting)
		segmentProps.Data = map[string]string{
			"option": option, "selected": boolLabel(selected),
		}
		segmentProps.Class = segmentClass(props, selected)
		segments = append(segments, html.Button(segmentProps, html.Text(option)))
	}
	return html.Div(
		html.Props{
			Role: "group",
			Aria: map[string]string{"label": setting.Label},
			Data: map[string]string{"component": "settings-segmented"},
			Class: css.New(
				u.InlineFlex, u.ItemsCenter,
				css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
				css.Border(
					css.Px(tokens.Geometry.BorderWidth),
					css.Hex(string(tokens.Colors.BorderSubtle)),
				),
				css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
				css.Padding(css.Px(2)),
				css.Gap(css.Px(2)),
			).String(),
		},
		segments...,
	)
}

// segmentClass styles one option. The chosen one carries the weight so the
// axis still reads as a column of values rather than a column of controls.
func segmentClass(props Props, selected bool) string {
	tokens := props.Mode.Tokens()
	background := css.Transparent
	color := tokens.Colors.TextMuted
	weight := css.FontWeight.Normal
	if selected {
		background = css.Hex(string(tokens.Colors.Selection))
		color = tokens.Colors.OnSelection
		weight = css.FontWeight.Semibold
	}
	return css.New(
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.Code.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Code.LineHeight)),
		weight,
		css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.PaddingY(css.Px(3)),
		css.MinHeight(css.Px(controlHeight-6)),
		css.Bg(background),
		css.TextColor(css.Hex(string(color))),
		css.Border(css.Zero, css.Transparent),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Cursor.Pointer,
		css.WhiteSpace.NoWrap,
	).String()
}

// stepperControl renders a bounded number: editable digits between a decrement
// and an increment.
//
// The browser's own spinner is removed. It draws two arrows a few pixels tall
// inside the field, which is neither readable at this size nor reachable by
// anybody who is not using a mouse precisely.
func stepperControl(props Props, setting FlowSetting) ui.Node {
	tokens := props.Mode.Tokens()
	disabled := props.OnFlowNumber == nil || props.FlowBusy
	inputProps := html.PropsOf(html.OnInput(func(event ui.InputEvent) {
		if props.OnFlowNumber == nil {
			return
		}
		parsed, err := strconv.ParseInt(strings.TrimSpace(event.GetValue()), 10, 32)
		if err != nil {
			// A cleared or half-typed field is not a value. Reporting it as one
			// would send the coordinator a number nobody meant.
			return
		}
		props.OnFlowNumber(setting.Key, int32(parsed))
	}))
	inputProps.ID = "flow-" + setting.Key
	inputProps.Type = "text"
	inputProps.Value = strconv.FormatInt(int64(setting.Number), 10)
	inputProps.Disabled = disabled
	inputProps.Aria = map[string]string{
		"label": setting.Label + ", between " +
			strconv.FormatInt(int64(setting.Minimum), 10) + " and " +
			strconv.FormatInt(int64(setting.Maximum), 10),
		"invalid": boolLabel(numberOutOfBound(setting)),
	}
	inputProps.Data = map[string]string{"component": "settings-number"}
	inputProps.Class = css.New(
		css.Appearance.None,
		css.W(css.Px(52)),
		css.PaddingX(css.Px(tokens.Spacing.XS)),
		css.PaddingY(css.Px(2)),
		css.Bg(css.Transparent),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.Body.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
		css.TextAlign.Center,
		css.Border(css.Zero, css.Transparent),
	).String()
	return html.Div(
		html.Props{
			Role: "group",
			Aria: map[string]string{"label": setting.Label},
			Data: map[string]string{"component": "settings-stepper"},
			Class: css.New(
				u.InlineFlex, u.ItemsCenter,
				css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
				css.Border(
					css.Px(tokens.Geometry.BorderWidth),
					css.Hex(string(stepperBorder(props, setting))),
				),
				css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
				css.Padding(css.Px(2)),
			).String(),
		},
		stepperButton(props, setting, -1, "−", "Decrease "+setting.Label),
		html.Input(inputProps),
		stepperButton(props, setting, 1, "+", "Increase "+setting.Label),
	)
}

// stepperButton moves a number by one, and refuses to leave its bound.
func stepperButton(
	props Props,
	setting FlowSetting,
	step int32,
	glyph string,
	label string,
) ui.Node {
	next := setting.Number + step
	blocked := next < setting.Minimum || next > setting.Maximum ||
		props.OnFlowNumber == nil || props.FlowBusy
	buttonProps := html.PropsOf(html.OnClick(func() {
		if props.OnFlowNumber != nil {
			props.OnFlowNumber(setting.Key, next)
		}
	}))
	buttonProps.Type = "button"
	buttonProps.Aria = map[string]string{"label": label}
	buttonProps.Disabled = blocked
	buttonProps.Data = map[string]string{"step": strconv.FormatInt(int64(step), 10)}
	buttonProps.Class = stepperButtonClass(props, blocked)
	return html.Button(buttonProps, html.Text(glyph))
}

// stepperButtonClass styles one end of a stepper.
func stepperButtonClass(props Props, blocked bool) string {
	tokens := props.Mode.Tokens()
	color := tokens.Colors.TextSecondary
	if blocked {
		// A control at its bound says so by going quiet rather than by
		// disappearing, so the bound is visible before it is reached.
		color = tokens.Colors.TextDisabled
	}
	return css.New(
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
		css.W(css.Px(controlHeight-6)), css.MinHeight(css.Px(controlHeight-6)),
		css.Bg(css.Transparent),
		css.TextColor(css.Hex(string(color))),
		css.Border(css.Zero, css.Transparent),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Cursor.Pointer,
	).String()
}

// stepperBorder marks a field holding a value the engine would refuse.
func stepperBorder(props Props, setting FlowSetting) design.Color {
	tokens := props.Mode.Tokens()
	if numberOutOfBound(setting) {
		return tokens.Colors.Failure
	}
	return tokens.Colors.BorderSubtle
}

// discreteSelected reports whether one option is the value in force.
func discreteSelected(setting FlowSetting, option string) bool {
	if setting.Kind == FlowSwitch {
		return (option == "on") == setting.Enabled
	}
	return setting.Text == option
}

// discreteDisabled reports whether a discrete control can be used.
func discreteDisabled(props Props, setting FlowSetting) bool {
	if setting.Kind == FlowSwitch {
		return props.OnFlowSwitch == nil || props.FlowBusy
	}
	return props.OnFlowChoice == nil || props.FlowBusy
}

// applyDiscrete hands one chosen option to the right handler.
func applyDiscrete(props Props, setting FlowSetting, option string) {
	if setting.Kind == FlowSwitch {
		if props.OnFlowSwitch != nil && !discreteSelected(setting, option) {
			props.OnFlowSwitch(setting.Key)
		}
		return
	}
	if props.OnFlowChoice != nil {
		props.OnFlowChoice(setting.Key, option)
	}
}

// segmentID names one option so a search result can jump to it.
func segmentID(key string, index int) string {
	if index != 0 {
		return ""
	}
	return "flow-" + key
}

// discreteOptions are the options a discrete setting offers.
func discreteOptions(setting FlowSetting) []string {
	if setting.Kind == FlowSwitch {
		return []string{"on", "off"}
	}
	return setting.Choices
}

// segmentedFitsTheAxis reports whether a control's options fit beside the
// value column without pushing it out of alignment.
func segmentedFitsTheAxis(options []string) bool {
	if len(options) == 0 {
		return false
	}
	width := 0
	for _, option := range options {
		width += len(option)
	}
	return len(options) <= 3 && width <= segmentedWidthBudget
}

// listControl offers a long option set without pushing the row apart.
//
// A segmented control is right for two or three short postures and wrong for
// eight model identities: laid out in a row it is wider than the sheet, and
// laid into the control column it paints over the setting's own name. A list
// shows the option in force in the same width whatever the option set holds,
// and opens to the rest.
func listControl(props Props, setting FlowSetting) ui.Node {
	tokens := props.Mode.Tokens()
	options := make([]ui.Node, 0, len(setting.Choices))
	for _, choice := range setting.Choices {
		optionProps := html.Props{Value: choice, Text: choice}
		optionProps.Selected = choice == setting.Text
		options = append(options, html.Option(optionProps))
	}
	selectProps := html.PropsOf(html.OnChange(func(event ui.InputEvent) {
		if props.OnFlowChoice != nil {
			props.OnFlowChoice(setting.Key, event.GetValue())
		}
	}))
	selectProps.ID = "flow-" + setting.Key
	selectProps.Value = setting.Text
	selectProps.Disabled = props.OnFlowChoice == nil || props.FlowBusy
	selectProps.Aria = map[string]string{"label": setting.Label}
	selectProps.Data = map[string]string{"component": "settings-list"}
	selectProps.Class = css.New(
		css.Appearance.None,
		css.W(css.Full), css.MaxWidth(css.Px(valueColumnWidth)),
		css.MinWidth(css.Zero),
		css.MinHeight(css.Px(controlHeight)),
		css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.Code.Size)),
		css.TextAlign.Right,
		css.Border(
			css.Px(tokens.Geometry.BorderWidth),
			css.Hex(string(tokens.Colors.BorderSubtle)),
		),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Cursor.Pointer,
	).String()
	return html.Select(selectProps, options...)
}
