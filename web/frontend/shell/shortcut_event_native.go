//go:build !js || !wasm

package shell

import (
	"codeflux.dev/codeflux/web/frontend/shortcuts"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func normalizeShortcutEvent(event ui.KeyboardEvent) shortcuts.Event {
	return shortcuts.Event{Key: event.GetKey(), Target: shortcuts.TargetOther, Scope: shortcuts.ScopeApplication}
}

func shortcutEventIdentity(event shortcuts.Event) string { return event.Key }
