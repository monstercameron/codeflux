package shell

import (
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shortcuts"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type SessionBootstrapProps struct {
	Root    RootProps
	Dispose func()
}

// SessionBootstrap is the GWC component boundary between startup state and the
// authenticated application shell. It releases every root-owned subscription
// when GWC unmounts the component.
func SessionBootstrap(props SessionBootstrapProps) ui.Node {
	ui.UseMount(func() func() {
		return props.Dispose
	})
	return ui.CreateElement(AppRoot, props.Root)
}

// AppRouter is a pure route-to-component boundary and performs no data fetches.
func AppRouter(props RouteShellProps) ui.Node {
	return ui.CreateElement(RouteShell, props)
}

type GlobalErrorBoundaryProps struct {
	Child ui.Node
	Route routes.Route
	UI    state.UIStore
}

// GlobalErrorBoundary catches component panics without replacing route or draft
// ownership. Its fallback intentionally omits raw error details.
func GlobalErrorBoundary(props GlobalErrorBoundaryProps) ui.Node {
	return ui.NewErrorBoundary(ui.ErrorBoundaryProps{
		Child: props.Child,
		ErrorFallback: func(error, func()) ui.Node {
			return ui.CreateElement(TopLevelState, TopLevelStateProps{
				Kind: "recoverable-error", Title: "This view could not render",
				Body: "Your route and unsent draft were preserved. Retry the view.",
			})
		},
	})
}

type HostProps struct{ Children []ui.Node }

// ShortcutManagerProps binds the pure shortcut policy to shell-local effects.
// Pause and stop callbacks are requests only; coordinator authority remains
// outside the shell.
type ShortcutManagerProps struct {
	Children            []ui.Node
	Mode                primitives.Mode
	Platform            shortcuts.Platform
	HelpOpen            bool
	PauseEnabled        bool
	StopEnabled         bool
	OnFocusConversation func()
	OnFocusGraph        func()
	OnPauseRequested    func()
	OnStopRequested     func()
	OnOpenHelp          func()
	OnCloseHelp         func()
}

// ShortcutActionHandlers are the safe local effects available to keyboard
// actions. Availability gates keep disabled task controls inert.
type ShortcutActionHandlers struct {
	PauseEnabled        bool
	StopEnabled         bool
	OnFocusConversation func()
	OnFocusGraph        func()
	OnPauseRequested    func()
	OnStopRequested     func()
	OnOpenHelp          func()
}

// DispatchShortcutAction invokes at most one authorized local handler and
// reports whether the browser event was consumed.
func DispatchShortcutAction(action shortcuts.Action, handlers ShortcutActionHandlers) bool {
	switch action {
	case shortcuts.ActionFocusConversation:
		return invokeShortcutHandler(handlers.OnFocusConversation)
	case shortcuts.ActionFocusGraph:
		return invokeShortcutHandler(handlers.OnFocusGraph)
	case shortcuts.ActionPause:
		return handlers.PauseEnabled && invokeShortcutHandler(handlers.OnPauseRequested)
	case shortcuts.ActionStop:
		return handlers.StopEnabled && invokeShortcutHandler(handlers.OnStopRequested)
	case shortcuts.ActionHelp:
		return invokeShortcutHandler(handlers.OnOpenHelp)
	default:
		return false
	}
}

func invokeShortcutHandler(handler func()) bool {
	if handler == nil {
		return false
	}
	handler()
	return true
}

func GlobalShortcutManager(props ShortcutManagerProps) ui.Node {
	platform := props.Platform
	if platform == "" {
		platform = shortcuts.CurrentPlatform()
	}
	policy := shortcuts.DefaultPolicy()
	composing := ui.UseRef(false)
	pressed := ui.UseRef("")
	ui.UseDocumentEvent("compositionstart", func(ui.Event) { composing.Set(true) })
	ui.UseDocumentEvent("compositionend", func(ui.Event) { composing.Set(false) })
	ui.UseDocumentEvent("keyup", func(ui.Event) { pressed.Set("") })
	ui.UseWindowEvent("blur", func(ui.Event) { pressed.Set("") })
	handlers := ShortcutActionHandlers{
		PauseEnabled: props.PauseEnabled, StopEnabled: props.StopEnabled,
		OnFocusConversation: props.OnFocusConversation, OnFocusGraph: props.OnFocusGraph,
		OnPauseRequested: props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
		OnOpenHelp: props.OnOpenHelp,
	}
	ui.UseGlobalKey(func(event ui.KeyboardEvent) {
		normalized := normalizeShortcutEvent(event)
		normalized.Composing = composing.Get()
		identity := shortcutEventIdentity(normalized)
		if identity != "" && identity == pressed.Get() {
			normalized.Repeat = true
		}
		decision := policy.Resolve(normalized, platform)
		if !decision.Handled || !DispatchShortcutAction(decision.Action, handlers) {
			return
		}
		pressed.Set(identity)
		event.PreventDefault()
	})

	return html.Div(html.Props{
		Data: map[string]string{
			"component": "global-shortcut-manager",
			"scope":     "application",
			"platform":  string(platform),
		},
	},
		html.Div(html.Props{ID: "shortcut-managed-content"}, props.Children...),
		ui.CreateElement(ShortcutHelpDialog, ShortcutHelpDialogProps{
			Open: props.HelpOpen, Mode: props.Mode, Platform: platform, OnDismiss: props.OnCloseHelp,
		}),
	)
}

type ShortcutHelpDialogProps struct {
	Open      bool
	Mode      primitives.Mode
	Platform  shortcuts.Platform
	OnDismiss func()
}

// ShortcutHelpDialog renders the accessible help model through GWC's modal
// overlay primitive.
func ShortcutHelpDialog(props ShortcutHelpDialogProps) ui.Node {
	tokens := props.Mode.Tokens()
	model := shortcuts.DefaultPolicy().HelpDialog(props.Platform)
	groups := make([]ui.Node, 0, len(model.Groups))
	for _, group := range model.Groups {
		entries := make([]ui.Node, 0, len(group.Entries))
		for _, entry := range group.Entries {
			entries = append(entries, html.Li(html.Props{
				DataAttr: html.DataAttribute{Name: "shortcut-action", Value: string(entry.Action)},
			},
				html.Span(html.Props{Text: entry.Description}),
				html.Kbd(html.Props{Aria: map[string]string{"label": entry.AccessibleKeyLabel}, Text: entry.KeyLabel}),
			))
		}
		groups = append(groups, html.Section(html.Props{Aria: map[string]string{"labelledby": group.ID}},
			html.H3(html.Props{Class: design.HeadingClass(tokens, design.HeadingPanel), ID: group.ID, Text: group.Heading}),
			html.Ul(html.Props{}, entries...),
		))
	}
	content := html.Div(html.Props{},
		html.H2(html.Props{Class: design.HeadingClass(tokens, design.HeadingSection), ID: model.TitleID, Text: model.Title}),
		html.P(html.Props{ID: model.DescriptionID, Text: model.Description}),
		html.Div(html.Props{}, groups...),
		primitives.Button(primitives.ButtonProps{
			ID: model.CloseControlID, Label: "Close", AccessibleLabel: model.CloseLabel,
			Mode: props.Mode, OnClick: props.OnDismiss,
		}),
	)
	return primitives.Dialog(primitives.OverlayProps{
		ID: model.ID, Open: props.Open,
		LabelledBy: model.LabelledBy, DescribedBy: model.DescribedBy,
		InitialFocusSelector: "#" + model.InitialFocusID,
		AppRootSelector:      "#shortcut-managed-content",
		Mode:                 props.Mode, Content: content, OnDismiss: props.OnDismiss,
	})
}

func DialogHost(props HostProps) ui.Node {
	return html.Div(html.Props{
		DataAttr: html.DataAttribute{Name: "component", Value: "dialog-host"},
	}, props.Children...)
}

func ToastHost(props HostProps) ui.Node {
	return html.Div(html.Props{
		Role:     "status",
		DataAttr: html.DataAttribute{Name: "component", Value: "toast-host"},
		Aria:     map[string]string{"live": "polite", "atomic": "true"},
	}, props.Children...)
}

func AccessibilityAnnouncer(props AnnouncerProps) ui.Node {
	return ui.CreateElement(Announcer, props)
}
