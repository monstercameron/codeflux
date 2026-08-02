package primitives

import (
	"strconv"
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

// ModalProps configures the console's one modal shell.
type ModalProps struct {
	ID          string
	Title       string
	Description string
	Icon        IconName
	Open        bool
	Mode        Mode
	// Width is the surface's maximum width in CSS pixels. It defaults to a
	// measure that keeps a form readable rather than filling the display.
	Width int
	Body  ui.Node
	// PrimaryLabel names the action that commits and closes. Left empty, the
	// modal is a reading surface and shows only its dismissal.
	PrimaryLabel          string
	PrimaryDisabled       bool
	PrimaryDisabledReason string
	OnPrimary             func()
	// DismissLabel names the way out. It defaults to Close.
	DismissLabel         string
	InitialFocusSelector string
	AppRootSelector      string
	OnDismiss            func()
}

// Modal is the single designed overlay every dialog in the console uses.
//
// Before it, each caller assembled its own: a heading here, a bare "×" there, a
// row of buttons somewhere else, and no two the same width or rhythm. One shell
// means a person learns the shape once — title on the left, the way out on the
// right, the body between them, the committing action at the bottom.
func Modal(props ModalProps) ui.Node {
	tokens := props.Mode.Tokens()
	width := props.Width
	if width <= 0 {
		width = 560
	}
	titleID := props.ID + "-title"
	descriptionID := props.ID + "-description"
	heading := []ui.Node{}
	if props.Icon != "" {
		heading = append(heading, Icon(IconProps{Name: props.Icon, Size: IconSizeBase}))
	}
	heading = append(heading, html.H2(html.Props{
		ID: titleID,
		Class: css.New(
			css.Margin(css.Zero),
			css.Font(css.FontStack(tokens.Fonts.Display)),
			css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
			css.FontWeight.Normal,
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		).String(),
		Text: props.Title,
	}))
	children := []ui.Node{
		html.Header(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.Padding(css.RawLength("0 0 12px")),
				css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
			}, heading...),
			Button(ButtonProps{
				ID: props.ID + "-close", Icon: IconClose, AccessibleLabel: "Close " + props.Title,
				Quiet: true, Mode: props.Mode, OnClick: props.OnDismiss,
			}),
		),
	}
	if strings.TrimSpace(props.Description) != "" {
		children = append(children, html.P(html.Props{
			ID: descriptionID,
			Class: css.New(
				css.Margin(css.Zero),
				css.MaxWidth(css.Ch(72)),
				css.Font(css.FontStack(tokens.Fonts.Reading)),
				css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			).String(),
			Text: props.Description,
		}))
	}
	children = append(children, html.Div(html.Props{
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
			css.MinHeight(css.Zero), css.OverflowY.Auto,
			css.Padding(css.RawLength("4px 0")),
		).String(),
	}, props.Body))
	footer := []ui.Node{}
	if strings.TrimSpace(props.PrimaryLabel) != "" {
		footer = append(footer, Button(ButtonProps{
			ID: props.ID + "-primary", Label: props.PrimaryLabel, Primary: true,
			Disabled: props.PrimaryDisabled, DisabledReason: props.PrimaryDisabledReason,
			Mode: props.Mode, OnClick: props.OnPrimary,
		}))
	}
	dismissLabel := props.DismissLabel
	if strings.TrimSpace(dismissLabel) == "" {
		dismissLabel = "Close"
	}
	footer = append([]ui.Node{Button(ButtonProps{
		ID: props.ID + "-dismiss", Label: dismissLabel, Mode: props.Mode, OnClick: props.OnDismiss,
	})}, footer...)
	children = append(children, html.Footer(html.Props{
		Class: css.New(
			u.Flex, u.ItemsCenter, u.JustifyEnd,
			css.Gap(css.Px(tokens.Spacing.SM)),
			css.Padding(css.RawLength("12px 0 0")),
			css.BorderTop(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		).String(),
	}, footer...))
	initialFocus := props.InitialFocusSelector
	if strings.TrimSpace(initialFocus) == "" {
		initialFocus = "#" + props.ID + "-close"
	}
	described := ""
	if strings.TrimSpace(props.Description) != "" {
		described = descriptionID
	}
	return Dialog(OverlayProps{
		ID: props.ID, Open: props.Open,
		LabelledBy: titleID, DescribedBy: described,
		InitialFocusSelector: initialFocus,
		AppRootSelector:      props.AppRootSelector,
		Mode:                 props.Mode,
		OnDismiss:            props.OnDismiss,
		Content: html.Section(html.Props{
			Data: map[string]string{"component": "modal", "modal": props.ID},
			Class: css.New(
				u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
				css.W(css.RawLength("min("+strconv.Itoa(width)+"px, calc(100vw - 48px))")),
				css.MinHeight(css.Zero),
			).String(),
		}, children...),
	})
}
