//go:build js && wasm

package composer

import "github.com/monstercameron/GoWebComponents/v5/ui"

func keyInputFromEvent(event ui.KeyboardEvent, composing bool) KeyInput {
	return KeyInput{
		Key: event.GetKey(), Shift: event.GetShiftKey(), Control: event.GetCtrlKey(),
		Alt: event.GetAltKey(), Meta: event.GetMetaKey(), Composing: composing,
	}
}
