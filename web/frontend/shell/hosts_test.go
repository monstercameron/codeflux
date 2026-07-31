package shell_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/shortcuts"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestGlobalErrorBoundaryPreservesExternalRouteAndDraftState(t *testing.T) {
	route := routes.Route{Name: routes.Settings}
	uiStore := state.NewUIStore(state.DefaultLayoutPreferences(), map[string]string{"thread-1": "draft"})
	panicComponent := func() ui.Node { panic("sensitive raw detail") }
	markup := render(t, ui.CreateElement(shell.GlobalErrorBoundary, shell.GlobalErrorBoundaryProps{
		Child: ui.CreateElement(panicComponent), Route: route, UI: uiStore,
	}))
	if !strings.Contains(markup, "unsent draft were preserved") {
		t.Fatalf("missing safe recovery copy: %s", markup)
	}
	if strings.Contains(markup, "sensitive raw detail") {
		t.Fatalf("raw component error leaked: %s", markup)
	}
	if route.Name != routes.Settings || uiStore.Draft("thread-1") != "draft" {
		t.Fatal("error boundary mutated route or draft ownership")
	}
}

func TestHostsUsePoliteNonFocusStealingRegions(t *testing.T) {
	markup := render(t, ui.Fragment(
		ui.CreateElement(shell.DialogHost, shell.HostProps{}),
		ui.CreateElement(shell.ToastHost, shell.HostProps{}),
		ui.CreateElement(shell.AccessibilityAnnouncer, shell.AnnouncerProps{Message: "Task paused"}),
	))
	if strings.Count(markup, `aria-live="polite"`) != 2 {
		t.Fatalf("expected polite toast and announcer regions: %s", markup)
	}
	if strings.Contains(markup, `aria-live="assertive"`) || strings.Contains(markup, `autofocus`) {
		t.Fatalf("host steals focus or uses assertive live region: %s", markup)
	}
}

func TestDispatchShortcutActionGatesTaskRequests(t *testing.T) {
	called := map[shortcuts.Action]int{}
	handlers := shell.ShortcutActionHandlers{
		OnFocusConversation: func() { called[shortcuts.ActionFocusConversation]++ },
		OnFocusGraph:        func() { called[shortcuts.ActionFocusGraph]++ },
		OnPauseRequested:    func() { called[shortcuts.ActionPause]++ },
		OnStopRequested:     func() { called[shortcuts.ActionStop]++ },
		OnOpenHelp:          func() { called[shortcuts.ActionHelp]++ },
	}
	for _, action := range []shortcuts.Action{
		shortcuts.ActionFocusConversation, shortcuts.ActionFocusGraph, shortcuts.ActionHelp,
	} {
		if !shell.DispatchShortcutAction(action, handlers) || called[action] != 1 {
			t.Fatalf("action %s was not dispatched exactly once: %v", action, called)
		}
	}
	if shell.DispatchShortcutAction(shortcuts.ActionPause, handlers) ||
		shell.DispatchShortcutAction(shortcuts.ActionStop, handlers) {
		t.Fatalf("disabled task action dispatched: %v", called)
	}
	handlers.PauseEnabled = true
	handlers.StopEnabled = true
	if !shell.DispatchShortcutAction(shortcuts.ActionPause, handlers) ||
		!shell.DispatchShortcutAction(shortcuts.ActionStop, handlers) ||
		called[shortcuts.ActionPause] != 1 || called[shortcuts.ActionStop] != 1 {
		t.Fatalf("enabled task requests were not dispatched: %v", called)
	}
	if shell.DispatchShortcutAction(shortcuts.Action("unknown"), handlers) {
		t.Fatal("unknown shortcut action was consumed")
	}
}

func TestGlobalShortcutManagerPreservesChildrenAndDeclaresPlatform(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.GlobalShortcutManager, shell.ShortcutManagerProps{
		Platform: shortcuts.PlatformMacOS,
		Children: []ui.Node{html.Main(html.Props{ID: "managed-child", Text: "Workspace"})},
	}))
	for _, want := range []string{
		`data-component="global-shortcut-manager"`,
		`data-platform="macos"`,
		`id="shortcut-managed-content"`,
		`id="managed-child"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("manager markup missing %q: %s", want, markup)
		}
	}
}

func TestShortcutHelpDialogUsesAccessibleOverlayContract(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.ShortcutHelpDialog, shell.ShortcutHelpDialogProps{
		Open: true, Platform: shortcuts.PlatformMacOS, Mode: primitives.Mode{},
	}))
	for _, want := range []string{
		`data-focus-policy="trap-restore"`,
		`data-dismiss-policy="escape-outside"`,
		`data-state="open"`,
		`id="shortcut-help-title"`,
		`id="shortcut-help-description"`,
		`id="shortcut-help-close"`,
		`aria-label="Command+Option+1"`,
		`data-shortcut-action="stop"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("help dialog missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `autofocus`) {
		t.Fatalf("dialog used autofocus instead of the managed focus contract: %s", markup)
	}
	policy := primitives.ModalOverlayAccessibilityPolicy()
	if policy.Role != "dialog" || !policy.Modal || !policy.TrapFocus || !policy.RestoreFocus ||
		!policy.CloseOnEscape || !policy.BackgroundInert {
		t.Fatalf("help dialog primitive lost modal focus-restoration policy: %+v", policy)
	}
}

func TestAppShellDeclaresStableFocusLandmarkOrder(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: readySnapshot(), Route: routes.Route{Name: routes.ThreadWorkspace}, Tokens: tokens(t),
	}))
	for _, want := range []string{
		`data-focus-order="1" data-focus-region="rail"`,
		`data-focus-order="2" data-focus-region="conversation"`,
		`data-focus-order="3" data-focus-region="composer"`,
		`data-focus-order="4" data-focus-region="graph"`,
		`data-focus-order="5" data-focus-region="inspector"`,
		`id="thread-composer"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("focus contract missing %q: %s", want, markup)
		}
	}
}
