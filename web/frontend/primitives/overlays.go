package primitives

import (
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
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
		BackdropClass:         modalBackdropClass(tokens, kind),
		SurfaceClass:          modalSurfaceClass(tokens, kind),
		Child:                 content,
		OnDismiss:             props.OnDismiss,
	})
}

func modalBackdropClass(tokens design.Tokens, kind ui.OverlayKind) string {
	rules := []css.Rule{
		u.Fixed, css.Inset(css.Zero), u.Flex, u.ItemsCenter, u.JustifyCenter,
		css.Padding(css.Px(tokens.Spacing.LG)),
		css.Bg(css.RGBA(3, 12, 20, 0.76)),
	}
	if tokens.Theme == design.ThemeLight {
		rules[len(rules)-1] = css.Bg(css.RGBA(21, 40, 54, 0.42))
	}
	if kind == ui.OverlayKindSheet {
		rules = append(rules, u.JustifyEnd, css.Padding(css.Zero))
	}
	return css.New(rules...).String()
}

func modalSurfaceClass(tokens design.Tokens, kind ui.OverlayKind) string {
	rules := []css.Rule{
		css.W(css.RawLength("min(680px, calc(100vw - 32px))")),
		css.MaxHeight(css.RawLength("calc(100dvh - 32px)")),
		css.OverflowY.Auto,
	}
	if kind == ui.OverlayKindSheet {
		rules = []css.Rule{
			css.W(css.RawLength("min(480px, 100vw)")),
			css.H(css.Full),
			css.MaxHeight(css.Full),
			css.OverflowY.Auto,
			css.Rounded(css.Zero),
		}
	}
	return css.New(append([]css.Rule{
		u.Flex, u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.MD)),
		css.Padding(css.Px(tokens.Rhythm.PanelInset)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.DialogRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.Shadow(elevationShadow(tokens, tokens.Elevation.Modal)),
	}, rules...)...).String()
}

func Popover(props OverlayProps) ui.Node {
	if strings.TrimSpace(props.LabelledBy) == "" {
		return contractError("popover", "labelled-by id is required")
	}
	tokens := props.Mode.Tokens()
	return accessibleOverlay(ui.AccessibleOverlayProps{
		Open:            props.Open,
		AppRootSelector: props.AppRootSelector,
		SurfaceID:       props.ID,
		Kind:            ui.OverlayKindPopover,
		// GWC promotes every role=dialog surface to modal, which makes the app
		// root inert. An anchored popover is non-modal and remains a labelled
		// region so navigation cannot strand background accessibility state.
		Role:                "region",
		LabelledBy:          props.LabelledBy,
		DescribedBy:         props.DescribedBy,
		AnchorSelector:      props.AnchorSelector,
		Positioning:         "anchor",
		RestoreFocus:        true,
		TrapFocus:           false,
		CloseOnEscape:       true,
		CloseOnOutsideClick: true,
		SurfaceClass:        surfaceClassAt(tokens, tokens.Elevation.Floating),
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
	tokens := props.Mode.Tokens()
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
		SurfaceClass:        surfaceClassAt(tokens, tokens.Elevation.Floating),
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
	if strings.TrimSpace(props.Target.Selector) == "" {
		props.Target = ui.PortalTarget{Selector: "#app"}
	}
	return ui.CreateElement(ui.AccessibleOverlay, props)
}
