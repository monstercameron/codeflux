package primitives

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type MenuItem struct {
	ID       string
	Label    string
	Disabled bool
}

type MenuProps struct {
	ID             string
	Label          string
	Open           bool
	ActiveID       string
	Items          []MenuItem
	Mode           Mode
	OnActivate     func(string)
	OnActiveChange func(string)
	OnDismiss      func()
}

// ResolveMenuKey implements looping menu navigation and returns "dismiss" for
// Escape and "activate" for Enter/Space.
func ResolveMenuKey(items []MenuItem, activeID, key string) (nextID, action string) {
	enabled := make([]MenuItem, 0, len(items))
	for _, item := range items {
		if item.ID != "" && !item.Disabled {
			enabled = append(enabled, item)
		}
	}
	if key == "Escape" {
		return activeID, "dismiss"
	}
	if key == "Enter" || key == " " {
		return activeID, "activate"
	}
	if len(enabled) == 0 {
		return "", ""
	}
	index := 0
	for candidate, item := range enabled {
		if item.ID == activeID {
			index = candidate
			break
		}
	}
	switch key {
	case "ArrowDown":
		return enabled[(index+1)%len(enabled)].ID, "move"
	case "ArrowUp":
		return enabled[(index-1+len(enabled))%len(enabled)].ID, "move"
	case "Home":
		return enabled[0].ID, "move"
	case "End":
		return enabled[len(enabled)-1].ID, "move"
	default:
		return activeID, ""
	}
}

func Menu(props MenuProps) ui.Node {
	if strings.TrimSpace(props.Label) == "" {
		return contractError("menu", "label is required")
	}
	for _, item := range props.Items {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Label) == "" {
			return contractError("menu", "every item requires an id and label")
		}
	}
	tokens := props.Mode.Tokens()
	items := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
		item := item
		active := item.ID == props.ActiveID
		onClick := func() {
			if !item.Disabled && props.OnActivate != nil {
				props.OnActivate(item.ID)
			}
		}
		onKeyDown := func(event ui.KeyboardEvent) {
			next, action := ResolveMenuKey(props.Items, props.ActiveID, event.GetKey())
			switch action {
			case "dismiss":
				event.PreventDefault()
				if props.OnDismiss != nil {
					props.OnDismiss()
				}
			case "activate":
				event.PreventDefault()
				if props.OnActivate != nil {
					props.OnActivate(next)
				}
			case "move":
				event.PreventDefault()
				if props.OnActiveChange != nil {
					props.OnActiveChange(next)
				}
			}
		}
		itemProps := html.PropsOf(html.OnClick(onClick), html.OnKeyDown(onKeyDown))
		itemProps.ID = item.ID
		itemProps.Type = "button"
		itemProps.Role = "menuitem"
		itemProps.Disabled = item.Disabled
		itemProps.TabIndex = -1
		if active {
			itemProps.TabIndex = html.TabIndexZero
		}
		itemProps.Class = controlClass(tokens, active)
		itemProps.Data = map[string]string{"component": "menu-item", "state": stateName(InteractionState{Disabled: item.Disabled})}
		items = append(items, html.Li(html.Props{}, html.Button(itemProps, html.Text(item.Label))))
	}
	menu := html.Ul(
		html.Props{
			ID:   props.ID,
			Role: "menu",
			Aria: map[string]string{"label": props.Label},
			Data: map[string]string{
				"component": "menu",
				"state":     map[bool]string{true: "open", false: "closed"}[props.Open],
				"keyboard":  "arrows-home-end-enter-space-escape",
			},
			Class: surfaceClassAt(tokens, tokens.Elevation.Floating),
		},
		items...,
	)
	return ui.AccessibleOverlay(ui.AccessibleOverlayProps{
		Open:                props.Open,
		SurfaceID:           props.ID + "-surface",
		Kind:                ui.OverlayKindMenu,
		Role:                "menu",
		RestoreFocus:        true,
		TrapFocus:           true,
		CloseOnEscape:       true,
		CloseOnOutsideClick: true,
		Child:               menu,
		OnDismiss:           props.OnDismiss,
	})
}
