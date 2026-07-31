package primitives

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type OverlayProps struct {
	ID                   string
	Open                 bool
	LabelledBy           string
	DescribedBy          string
	AnchorSelector       string
	InitialFocusSelector string
	AppRootSelector      string
	Mode                 Mode
	Content              ui.Node
	OnDismiss            func()
}

// OverlayAccessibilityPolicy is the testable focus and dismissal contract used
// by modal overlays.
type OverlayAccessibilityPolicy struct {
	Role                string
	Modal               bool
	RestoreFocus        bool
	TrapFocus           bool
	CloseOnEscape       bool
	CloseOnOutsideClick bool
	LockScroll          bool
	BackgroundInert     bool
}

func ModalOverlayAccessibilityPolicy() OverlayAccessibilityPolicy {
	return OverlayAccessibilityPolicy{
		Role:                "dialog",
		Modal:               true,
		RestoreFocus:        true,
		TrapFocus:           true,
		CloseOnEscape:       true,
		CloseOnOutsideClick: true,
		LockScroll:          true,
		BackgroundInert:     true,
	}
}

func Dialog(props OverlayProps) ui.Node {
	return modalOverlay("dialog", ui.OverlayKindDialog, props)
}

func Drawer(props OverlayProps) ui.Node {
	return modalOverlay("drawer", ui.OverlayKindSheet, props)
}

func modalOverlay(component string, kind ui.OverlayKind, props OverlayProps) ui.Node {
	if strings.TrimSpace(props.LabelledBy) == "" {
		return contractError(component, "labelled-by id is required")
	}
	tokens := props.Mode.Tokens()
	policy := ModalOverlayAccessibilityPolicy()
	content := html.Div(
		html.Props{
			Data: map[string]string{
				"component":      component,
				"state":          map[bool]string{true: "open", false: "closed"}[props.Open],
				"focus-policy":   "trap-restore",
				"dismiss-policy": "escape-outside",
				"reduced-motion": boolARIA(props.Mode.ReducedMotion),
			},
		},
		props.Content,
	)
	return accessibleOverlay(ui.AccessibleOverlayProps{
		Open:                  props.Open,
		AppRootSelector:       props.AppRootSelector,
		SurfaceID:             props.ID,
		Kind:                  kind,
		Role:                  policy.Role,
		LabelledBy:            props.LabelledBy,
		DescribedBy:           props.DescribedBy,
		Modal:                 policy.Modal,
		InitialFocusSelector:  props.InitialFocusSelector,
		FallbackFocusSelector: "#" + props.ID,
		RestoreFocus:          policy.RestoreFocus,
		TrapFocus:             policy.TrapFocus,
		CloseOnEscape:         policy.CloseOnEscape,
		CloseOnOutsideClick:   policy.CloseOnOutsideClick,
		LockScroll:            policy.LockScroll,
		BackgroundInert:       policy.BackgroundInert,
		Backdrop:              true,
		SurfaceClass:          surfaceClass(tokens),
		Child:                 content,
		OnDismiss:             props.OnDismiss,
	})
}

func Popover(props OverlayProps) ui.Node {
	if strings.TrimSpace(props.LabelledBy) == "" {
		return contractError("popover", "labelled-by id is required")
	}
	return accessibleOverlay(ui.AccessibleOverlayProps{
		Open:                props.Open,
		AppRootSelector:     props.AppRootSelector,
		SurfaceID:           props.ID,
		Kind:                ui.OverlayKindPopover,
		Role:                "dialog",
		LabelledBy:          props.LabelledBy,
		DescribedBy:         props.DescribedBy,
		AnchorSelector:      props.AnchorSelector,
		Positioning:         "anchor",
		RestoreFocus:        true,
		TrapFocus:           false,
		CloseOnEscape:       true,
		CloseOnOutsideClick: true,
		SurfaceClass:        surfaceClass(props.Mode.Tokens()),
		Child: html.Div(
			html.Props{Data: map[string]string{
				"component":    "popover",
				"state":        map[bool]string{true: "open", false: "closed"}[props.Open],
				"focus-policy": "restore",
			}},
			props.Content,
		),
		OnDismiss: props.OnDismiss,
	})
}

type TooltipProps struct {
	ID             string
	Open           bool
	Label          string
	AnchorSelector string
	Mode           Mode
}

func Tooltip(props TooltipProps) ui.Node {
	if strings.TrimSpace(props.Label) == "" {
		return contractError("tooltip", "label is required")
	}
	return accessibleOverlay(ui.AccessibleOverlayProps{
		Open:                props.Open,
		SurfaceID:           props.ID,
		Kind:                ui.OverlayKindTooltip,
		Role:                "tooltip",
		AnchorSelector:      props.AnchorSelector,
		Positioning:         "anchor",
		TrapFocus:           false,
		RestoreFocus:        false,
		CloseOnEscape:       true,
		CloseOnOutsideClick: false,
		SurfaceClass:        surfaceClass(props.Mode.Tokens()),
		Child: html.Span(
			html.Props{Data: map[string]string{
				"component":      "tooltip",
				"state":          map[bool]string{true: "open", false: "closed"}[props.Open],
				"reduced-motion": boolARIA(props.Mode.ReducedMotion),
			}},
			html.Text(props.Label),
		),
	})
}

// accessibleOverlay gives GWC's hook-owning overlay implementation a stable
// component boundary. Calling it as a plain Go function attaches its positional
// hooks to the caller, and the closed/open path can then shift any caller hooks
// that follow the overlay.
func accessibleOverlay(props ui.AccessibleOverlayProps) ui.Node {
	return ui.CreateElement(ui.AccessibleOverlay, props)
}
