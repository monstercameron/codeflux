//go:build js && wasm

package shell

import (
	"strconv"

	"codeflux.dev/codeflux/web/frontend/shortcuts"
	"github.com/monstercameron/GoWebComponents/v5/interop"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func normalizeShortcutEvent(event ui.KeyboardEvent) shortcuts.Event {
	target, scope := activeShortcutTarget()
	return shortcuts.Event{
		Key: event.GetKey(), Ctrl: event.GetCtrlKey(), Meta: event.GetMetaKey(),
		Alt: event.GetAltKey(), Shift: event.GetShiftKey(), Target: target, Scope: scope,
	}
}

func activeShortcutTarget() (shortcuts.TargetKind, shortcuts.Scope) {
	document, err := interop.GetDocument()
	if err != nil {
		return shortcuts.TargetOther, shortcuts.ScopeApplication
	}
	active, ok, err := document.QuerySelector(":focus")
	if err != nil || !ok {
		return shortcuts.TargetOther, shortcuts.ScopeApplication
	}
	_, editable, _ := document.QuerySelector(":focus:read-write")
	target := shortcuts.ClassifyTarget(active.TagName(), editable)
	scope := shortcuts.ScopeApplication
	if _, composerFocused, _ := document.QuerySelector("#thread-composer:focus"); composerFocused {
		scope = shortcuts.ScopeComposer
	} else if _, graphFocused, _ := document.QuerySelector("#graph-region:focus-within"); graphFocused {
		scope = shortcuts.ScopeGraph
	} else if _, conversationFocused, _ := document.QuerySelector("#main-content:focus-within"); conversationFocused {
		scope = shortcuts.ScopeConversation
	}
	return target, scope
}

func shortcutEventIdentity(event shortcuts.Event) string {
	return event.Key + ":" + strconv.FormatBool(event.Ctrl) + ":" +
		strconv.FormatBool(event.Meta) + ":" + strconv.FormatBool(event.Alt) + ":" +
		strconv.FormatBool(event.Shift)
}
