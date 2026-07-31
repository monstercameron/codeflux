package primitives

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type SplitOrientation string

const (
	SplitHorizontal SplitOrientation = "horizontal"
	SplitVertical   SplitOrientation = "vertical"
)

type ResizableSplitProps struct {
	ID                string
	AccessibleLabel   string
	Orientation       SplitOrientation
	Value             float64
	Min               float64
	Max               float64
	Step              float64
	DragPixelsPerUnit float64
	Collapsed         bool
	Mode              Mode
	OnChange          func(float64)
	OnCollapse        func()
	OnRestore         func()
	First             ui.Node
	Second            ui.Node
}

// AdjustSplitDrag converts pointer movement into a bounded split value.
func AdjustSplitDrag(value, deltaPixels, pixelsPerUnit, minimum, maximum float64) float64 {
	if maximum < minimum {
		minimum, maximum = maximum, minimum
	}
	if pixelsPerUnit <= 0 {
		pixelsPerUnit = 8
	}
	return clamp(value+deltaPixels/pixelsPerUnit, minimum, maximum)
}

// AdjustSplitValue implements separator arrow/Home/End keyboard behavior.
func AdjustSplitValue(value, minimum, maximum, step float64, orientation SplitOrientation, key string) float64 {
	if maximum < minimum {
		minimum, maximum = maximum, minimum
	}
	if step <= 0 {
		step = 1
	}
	delta := 0.0
	switch key {
	case "Home":
		return minimum
	case "End":
		return maximum
	case "ArrowLeft":
		if orientation == SplitHorizontal {
			delta = -step
		}
	case "ArrowRight":
		if orientation == SplitHorizontal {
			delta = step
		}
	case "ArrowUp":
		if orientation == SplitVertical {
			delta = -step
		}
	case "ArrowDown":
		if orientation == SplitVertical {
			delta = step
		}
	}
	return clamp(value+delta, minimum, maximum)
}

func ResizableSplit(props ResizableSplitProps) ui.Node {
	if strings.TrimSpace(props.AccessibleLabel) == "" {
		return contractError("resizable-split", "accessible label is required")
	}
	orientation := props.Orientation
	if orientation == "" {
		orientation = SplitHorizontal
	}
	minimum, maximum := props.Min, props.Max
	if maximum <= minimum {
		minimum, maximum = 10, 90
	}
	value := clamp(props.Value, minimum, maximum)
	tokens := props.Mode.Tokens()
	dragging := false
	dragOrigin := 0.0
	dragValue := value
	onKeyDown := func(event ui.KeyboardEvent) {
		next := AdjustSplitValue(value, minimum, maximum, props.Step, orientation, event.GetKey())
		if next != value && props.OnChange != nil {
			event.PreventDefault()
			props.OnChange(next)
		}
	}
	onPointerDown := func(event ui.Event) {
		dragging = true
		dragOrigin = splitPointerCoordinate(event, orientation)
		dragValue = value
		event.PreventDefault()
	}
	onPointerMove := func(event ui.Event) {
		if !dragging || props.OnChange == nil {
			return
		}
		current := splitPointerCoordinate(event, orientation)
		next := AdjustSplitDrag(
			dragValue,
			current-dragOrigin,
			props.DragPixelsPerUnit,
			minimum,
			maximum,
		)
		props.OnChange(next)
	}
	onPointerUp := func() {
		dragging = false
	}
	onDoubleClick := func() {
		if props.Collapsed {
			if props.OnRestore != nil {
				props.OnRestore()
			}
			return
		}
		if props.OnCollapse != nil {
			props.OnCollapse()
		}
	}
	separatorProps := html.PropsOf(
		html.OnKeyDown(onKeyDown),
		html.OnDoubleClick(onDoubleClick),
		html.OnPointerDown(onPointerDown),
	)
	separatorProps.ID = props.ID
	separatorProps.Role = "separator"
	separatorProps.TabIndex = html.TabIndexZero
	separatorProps.Title = "Use arrow keys to resize; double-click to collapse or restore"
	separatorProps.Aria = map[string]string{
		"label":       props.AccessibleLabel,
		"orientation": string(orientation),
		"valuemin":    fmt.Sprintf("%g", minimum),
		"valuemax":    fmt.Sprintf("%g", maximum),
		"valuenow":    fmt.Sprintf("%g", value),
	}
	separatorProps.Data = map[string]string{
		"component":      "split-separator",
		"keyboard":       "arrows-home-end",
		"pointer":        "drag",
		"collapsed":      boolARIA(props.Collapsed),
		"reduced-motion": boolARIA(props.Mode.ReducedMotion),
	}
	rules := []css.Rule{
		u.Flex,
		u.ItemsCenter,
		u.JustifyCenter,
		css.Flex(css.Num(0), css.Num(0), css.Auto),
		css.MinWidth(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.Cursor.Pointer,
		css.UserSelect.None,
		css.Bg(css.Transparent),
		css.TextColor(css.Hex(string(tokens.Colors.BorderStrong))),
		css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)),
	}
	if tokens.Motion.Control > 0 {
		rules = append(rules, css.Transition(
			css.TransitionProps(css.PropColors), css.Ms(int(tokens.Motion.Control.Milliseconds())), css.EaseOut,
		))
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(tokens.Colors.Surface3))),
		css.TextColor(css.Hex(string(tokens.Colors.Accent))),
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	separatorProps.Class = css.New(rules...).String()
	rootProps := html.PropsOf(html.OnPointerMove(onPointerMove), html.OnPointerUp(onPointerUp))
	rootProps.Data = map[string]string{
		"component":   "resizable-split",
		"orientation": string(orientation),
		"state":       map[bool]string{true: "collapsed", false: "expanded"}[props.Collapsed],
	}
	rootRules := []css.Rule{
		u.Grid,
		css.W(css.Percent(100)),
		css.H(css.Full),
		css.MinWidth(css.Zero),
		css.MinHeight(css.Zero),
		css.Overflow.Hidden,
	}
	firstTrack, secondTrack := css.Fr(value), css.Fr(100-value)
	if props.Collapsed {
		firstTrack, secondTrack = css.Fr(1), css.Fr(0)
	}
	if orientation == SplitVertical {
		rootRules = append(rootRules, css.GridRows(
			css.MinMax(css.TrackLen(css.Zero), firstTrack),
			css.TrackLen(css.Px(tokens.Interaction.MinimumPointerTarget)),
			css.MinMax(css.TrackLen(css.Zero), secondTrack),
		))
	} else {
		rootRules = append(rootRules, css.GridCols(
			css.MinMax(css.TrackLen(css.Zero), firstTrack),
			css.TrackLen(css.Px(tokens.Interaction.MinimumPointerTarget)),
			css.MinMax(css.TrackLen(css.Zero), secondTrack),
		))
	}
	rootProps.Class = css.New(rootRules...).String()
	firstPaneRules := []css.Rule{
		css.MinWidth(css.Zero),
		css.MinHeight(css.Zero),
		css.Overflow.Auto,
	}
	secondPaneRules := []css.Rule{
		css.MinWidth(css.Zero),
		css.MinHeight(css.Zero),
		css.Overflow.Auto,
	}
	if props.Collapsed {
		firstPaneRules = []css.Rule{
			css.MinWidth(css.Zero),
			css.MinHeight(css.Zero),
			css.Overflow.Auto,
		}
		secondPaneRules = []css.Rule{
			css.W(css.Zero),
			css.MinWidth(css.Zero),
			css.Overflow.Hidden,
		}
	}
	return html.Div(
		rootProps,
		html.Div(html.Props{
			Data:  map[string]string{"pane": "first"},
			Class: css.New(firstPaneRules...).String(),
		}, props.First),
		html.Div(separatorProps, html.Text("⋮")),
		html.Div(html.Props{
			Hidden: props.Collapsed,
			Data:   map[string]string{"pane": "second"},
			Class:  css.New(secondPaneRules...).String(),
		}, props.Second),
	)
}

type DisclosureProps struct {
	ID       string
	Label    string
	Expanded bool
	Disabled bool
	Mode     Mode
	OnToggle func(bool)
	Content  ui.Node
}

func Disclosure(props DisclosureProps) ui.Node {
	if strings.TrimSpace(props.Label) == "" {
		return contractError("disclosure", "label is required")
	}
	contentID := props.ID + "-content"
	expanded := props.Expanded
	button := Button(ButtonProps{
		ID:       props.ID,
		Label:    props.Label,
		Disabled: props.Disabled,
		Expanded: &expanded,
		Controls: contentID,
		Mode:     props.Mode,
		OnClick: func() {
			if props.OnToggle != nil {
				props.OnToggle(!props.Expanded)
			}
		},
	})
	return html.Section(
		html.Props{
			Data: map[string]string{"component": "disclosure", "state": map[bool]string{true: "expanded", false: "collapsed"}[props.Expanded]},
		},
		button,
		html.Div(html.Props{ID: contentID, Hidden: !props.Expanded}, props.Content),
	)
}
