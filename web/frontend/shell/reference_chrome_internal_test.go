package shell

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestApplicationBarShortcutHelpButtonInvokesShellHandler(t *testing.T) {
	opened := false
	root := ApplicationBar(ApplicationBarProps{
		Mode:           primitives.Mode{},
		OnShortcutHelp: func() { opened = true },
	})
	handler, found := findButtonHandler(root, "Shortcut help")
	if !found {
		t.Fatal("application bar did not render the shortcut-help control")
	}
	if handler == nil {
		t.Fatal("shortcut-help control did not retain the shell open handler")
	}
	handler()
	if !opened {
		t.Fatal("shortcut-help control did not invoke the shell open handler")
	}
}

func TestShortcutHelpCloseButtonInvokesDismissHandler(t *testing.T) {
	dismissed := false
	root := ShortcutHelpDialog(ShortcutHelpDialogProps{
		Open: true, Mode: primitives.Mode{}, OnDismiss: func() { dismissed = true },
	})
	handler, found := findButtonHandler(root, "Close keyboard shortcut help")
	if !found || handler == nil {
		t.Fatal("shortcut-help dialog did not retain its close handler")
	}
	handler()
	if !dismissed {
		t.Fatal("shortcut-help close control did not invoke the dismiss handler")
	}
}

func findButtonHandler(node ui.Node, accessibleLabel string) (func(), bool) {
	if node == nil {
		return nil, false
	}
	if raw, ok := node.Props["__ui_props"]; ok {
		if overlay, ok := raw.(ui.AccessibleOverlayProps); ok {
			if handler, found := findButtonHandler(overlay.Child, accessibleLabel); found {
				return handler, true
			}
		}
	}
	if label, ok := node.Props["aria-label"].(string); ok && label == accessibleLabel {
		handler, _ := node.Props["onclick"].(func())
		return handler, true
	}
	for _, child := range node.Children {
		childNode, ok := child.(ui.Node)
		if !ok {
			continue
		}
		if handler, found := findButtonHandler(childNode, accessibleLabel); found {
			return handler, true
		}
	}
	return nil, false
}
