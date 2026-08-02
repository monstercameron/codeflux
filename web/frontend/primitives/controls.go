package primitives

import (
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type ButtonProps struct {
	ID              string
	Label           string
	AccessibleLabel string
	Value           string
	Primary         bool
	// Quiet marks a control that belongs to the surface it sits on rather
	// than standing on top of it: a copy action inside a transcript entry, a
	// rail's own housekeeping. It keeps the full pointer target and the same
	// focus ring, and gives up its filled background and border.
	Quiet bool
	// Compact drops the pointer-target floor, for an action set inside a dense
	// readout rather than standing on its own.
	Compact     bool
	Disabled    bool
	Busy        bool
	Expanded    *bool
	Controls    string
	DescribedBy string
	Mode        Mode
	OnClick     func()
	// StableOnClick allows a hook-owning parent to pass ui.UseEvent without
	// weakening the ordinary callback contract used by stateless callers.
	OnClickHandler ui.Handler
	StableOnClick  bool
	// Icon replaces the visible label with a drawn mark. The accessible name
	// still comes from Label or AccessibleLabel, so an icon-only control is
	// never nameless: it looks like an icon and reads like a sentence.
	Icon IconName
	// LeadingIcon draws a mark before the label instead of replacing it. Every
	// action in this console carries one, so a row of controls reads as a set
	// rather than as a paragraph of words in boxes.
	LeadingIcon IconName
	// DisabledReason says why a control cannot be used. A control that is
	// neither actionable nor explained is the worst state an interface can be
	// in, because it looks like it works; the reason reaches both a pointer
	// user, through the native tooltip, and assistive technology, through the
	// description.
	DisabledReason string
}

func Button(props ButtonProps) ui.Node {
	name := AccessibleName(props.Label, props.AccessibleLabel)
	if name == "" {
		return contractError("button", "accessible label is required")
	}
	state := InteractionState{Disabled: props.Disabled, Busy: props.Busy}
	htmlProps := html.PropsOf(html.OnClick(props.OnClick))
	if props.StableOnClick {
		htmlProps.OnClick = props.OnClickHandler
	}
	htmlProps.ID = props.ID
	htmlProps.Type = "button"
	htmlProps.Value = props.Value
	htmlProps.Disabled = !state.Enabled()
	htmlProps.Class = controlVariantClass(
		props.Mode.Tokens(), props.Primary, props.Quiet, props.Compact,
	)
	htmlProps.Aria = map[string]string{"label": name, "busy": boolARIA(props.Busy)}
	if props.Expanded != nil {
		htmlProps.Aria["expanded"] = boolARIA(*props.Expanded)
	}
	if props.Controls != "" {
		htmlProps.Aria["controls"] = props.Controls
	}
	if props.DescribedBy != "" {
		htmlProps.Aria["describedby"] = props.DescribedBy
	}
	htmlProps.Data = map[string]string{
		"component":       "button",
		"state":           stateName(state),
		"reduced-motion":  boolARIA(props.Mode.ReducedMotion),
		"high-contrast":   boolARIA(props.Mode.HighContrast),
		"pointer-minimum": "44",
	}
	if props.Disabled && strings.TrimSpace(props.DisabledReason) != "" {
		htmlProps.Title = props.DisabledReason
		htmlProps.Aria["description"] = props.DisabledReason
		htmlProps.Data["disabled-reason"] = props.DisabledReason
	}
	label := props.Label
	if props.Busy {
		label = "Working… " + props.Label
	}
	if props.Icon != "" && !props.Busy {
		// The mark is hidden from assistive technology because the button
		// already carries an accessible name. Announcing both would read the
		// control twice.
		htmlProps.Aria["label"] = name
		// A mark alone in a 44-pixel target needs to be large enough to read
		// as a shape rather than as a speck in a box.
		return html.Button(htmlProps, Icon(IconProps{Name: props.Icon, Size: IconSizeLarge}))
	}
	if props.LeadingIcon != "" && !props.Busy {
		return html.Button(htmlProps,
			Icon(IconProps{Name: props.LeadingIcon, Size: IconSizeBase}),
			html.Span(html.Props{Text: label}),
		)
	}
	return html.Button(htmlProps, html.Text(label))
}

type IconButtonProps struct {
	ID              string
	Icon            string
	AccessibleLabel string
	Disabled        bool
	Busy            bool
	Pressed         *bool
	Mode            Mode
	OnClick         func()
}

func IconButton(props IconButtonProps) ui.Node {
	if strings.TrimSpace(props.AccessibleLabel) == "" {
		return contractError("icon-button", "accessible label is required")
	}
	state := InteractionState{Disabled: props.Disabled, Busy: props.Busy}
	htmlProps := html.PropsOf(html.OnClick(props.OnClick))
	htmlProps.ID = props.ID
	htmlProps.Type = "button"
	htmlProps.Title = props.AccessibleLabel
	htmlProps.Disabled = !state.Enabled()
	htmlProps.Class = controlClass(props.Mode.Tokens(), false)
	htmlProps.Aria = map[string]string{
		"label": props.AccessibleLabel,
		"busy":  boolARIA(props.Busy),
	}
	if props.Pressed != nil {
		htmlProps.Aria["pressed"] = boolARIA(*props.Pressed)
	}
	htmlProps.Data = map[string]string{
		"component":      "icon-button",
		"state":          stateName(state),
		"reduced-motion": boolARIA(props.Mode.ReducedMotion),
		"high-contrast":  boolARIA(props.Mode.HighContrast),
	}
	return html.Button(
		htmlProps,
		html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, html.Text(props.Icon)),
	)
}

type ToggleButtonProps struct {
	ID              string
	Label           string
	AccessibleLabel string
	Pressed         bool
	Disabled        bool
	Busy            bool
	Mode            Mode
	// Compact drops the pointer-target floor, for a control set inside a dense
	// readout rather than standing on its own.
	Compact  bool
	OnToggle func(bool)
}

func ToggleButton(props ToggleButtonProps) ui.Node {
	onClick := func() {
		if props.OnToggle != nil {
			props.OnToggle(!props.Pressed)
		}
	}
	pressed := props.Pressed
	return toggleButton(props, pressed, onClick)
}

func toggleButton(props ToggleButtonProps, pressed bool, onClick func()) ui.Node {
	name := AccessibleName(props.Label, props.AccessibleLabel)
	if name == "" {
		return contractError("toggle-button", "accessible label is required")
	}
	state := InteractionState{Disabled: props.Disabled, Busy: props.Busy}
	htmlProps := html.PropsOf(html.OnClick(onClick))
	htmlProps.ID = props.ID
	htmlProps.Type = "button"
	htmlProps.Disabled = !state.Enabled()
	htmlProps.Class = segmentClass(props.Mode.Tokens(), pressed, props.Compact)
	htmlProps.Aria = map[string]string{
		"label":   name,
		"pressed": boolARIA(pressed),
		"busy":    boolARIA(props.Busy),
	}
	htmlProps.Data = map[string]string{"component": "toggle-button", "state": stateName(state)}
	return html.Button(htmlProps, html.Text(props.Label))
}

type TabItem struct {
	ID       string
	Label    string
	Disabled bool
}

type TabsProps struct {
	Label      string
	Items      []TabItem
	SelectedID string
	Mode       Mode
	OnSelect   func(string)
}

// ResolveTabsKey implements the horizontal tabs keyboard model.
func ResolveTabsKey(items []TabItem, selectedID, key string) string {
	enabled := make([]TabItem, 0, len(items))
	for _, item := range items {
		if !item.Disabled && item.ID != "" {
			enabled = append(enabled, item)
		}
	}
	if len(enabled) == 0 {
		return ""
	}
	current := 0
	for index, item := range enabled {
		if item.ID == selectedID {
			current = index
			break
		}
	}
	switch key {
	case "ArrowRight":
		return enabled[(current+1)%len(enabled)].ID
	case "ArrowLeft":
		return enabled[(current-1+len(enabled))%len(enabled)].ID
	case "Home":
		return enabled[0].ID
	case "End":
		return enabled[len(enabled)-1].ID
	default:
		return selectedID
	}
}

func Tabs(props TabsProps) ui.Node {
	if strings.TrimSpace(props.Label) == "" {
		return contractError("tabs", "group label is required")
	}
	for _, item := range props.Items {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Label) == "" {
			return contractError("tabs", "every item requires an id and label")
		}
	}
	tokens := props.Mode.Tokens()
	children := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
		item := item
		selected := item.ID == props.SelectedID
		onClick := func() {
			if !item.Disabled && props.OnSelect != nil {
				props.OnSelect(item.ID)
			}
		}
		onKeyDown := func(event ui.KeyboardEvent) {
			next := ResolveTabsKey(props.Items, props.SelectedID, event.GetKey())
			if next != "" && next != props.SelectedID && props.OnSelect != nil {
				event.PreventDefault()
				props.OnSelect(next)
			}
		}
		tabIndex := -1
		if selected {
			tabIndex = html.TabIndexZero
		}
		buttonProps := html.PropsOf(html.OnClick(onClick), html.OnKeyDown(onKeyDown))
		buttonProps.ID = item.ID
		buttonProps.Type = "button"
		buttonProps.Role = "tab"
		buttonProps.Disabled = item.Disabled
		buttonProps.TabIndex = tabIndex
		buttonProps.Class = controlClass(tokens, selected)
		buttonProps.Aria = map[string]string{
			"selected": boolARIA(selected),
			"controls": item.ID + "-panel",
		}
		buttonProps.Data = map[string]string{"component": "tab", "state": stateName(InteractionState{Disabled: item.Disabled})}
		children = append(children, html.Button(buttonProps, html.Text(item.Label)))
	}
	return html.Div(
		html.Props{
			Role: "tablist",
			Aria: map[string]string{"label": props.Label, "orientation": "horizontal"},
			Data: map[string]string{"component": "tabs", "keyboard": "arrows-home-end"},
			Class: css.New(
				u.Flex, css.Gap(css.Px(tokens.Spacing.XS)),
				css.MaxWidth(css.Full), css.OverflowX.Auto, css.Padding(css.Px(1)),
			).String(),
		},
		children...,
	)
}

type TextFieldProps struct {
	ID              string
	Label           string
	AccessibleLabel string
	Value           string
	Placeholder     string
	Disabled        bool
	Required        bool
	InvalidMessage  string
	Mode            Mode
	OnInput         func(string)
}

func TextField(props TextFieldProps) ui.Node {
	name := AccessibleName(props.Label, props.AccessibleLabel)
	if name == "" {
		return contractError("text-field", "accessible label is required")
	}
	tokens := props.Mode.Tokens()
	onInput := func(event ui.InputEvent) {
		if props.OnInput != nil {
			props.OnInput(event.GetValue())
		}
	}
	inputProps := html.PropsOf(html.OnInput(onInput))
	inputProps.ID = props.ID
	inputProps.Type = "text"
	inputProps.Value = props.Value
	inputProps.Placeholder = props.Placeholder
	inputProps.Disabled = props.Disabled
	inputProps.Required = props.Required
	inputProps.Aria = map[string]string{"label": name, "invalid": boolARIA(props.InvalidMessage != "")}
	if props.InvalidMessage != "" {
		inputProps.Aria["errormessage"] = props.ID + "-error"
	}
	inputProps.Data = map[string]string{"component": "text-field", "state": stateName(InteractionState{Disabled: props.Disabled})}
	inputProps.Class = fieldClass(tokens)
	children := []ui.Node{html.Label(html.Props{
		For: props.ID,
		Class: css.New(
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.ControlLabel.LineHeight)),
			css.FontWeight.Semibold,
		).String(),
	}, html.Text(props.Label)), html.Input(inputProps)}
	if props.InvalidMessage != "" {
		children = append(children, html.Span(
			html.Props{
				ID: props.ID + "-error", Role: "alert",
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.Failure))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
				).String(),
			},
			html.Text(props.InvalidMessage),
		))
	}
	return html.Div(html.Props{Class: stackClass(tokens)}, children...)
}

func fieldClass(tokens design.Tokens) string {
	rules := []css.Rule{
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.Body.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
	}
	if tokens.Motion.Control > 0 {
		rules = append(rules, css.Transition(
			css.TransitionProps(css.PropColors, css.PropShadow),
			css.Ms(int(tokens.Motion.Control.Milliseconds())),
			css.EaseOut,
		))
	}
	rules = append(rules, css.Hover(
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
	)...)
	rules = append(rules, css.Disabled(
		css.OpacityNum(css.Num(0.55)), css.Cursor.NotAllowed,
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
	)...)
	return css.New(rules...).String()
}

func stackClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String()
}
