//go:build js && wasm

package shell

import "github.com/monstercameron/GoWebComponents/v5/ui"

func handleFirstRunWheel(event ui.Event) {
	raw := event.JSValue()
	currentTarget := raw.Get("currentTarget")
	if !currentTarget.Truthy() {
		return
	}
	currentTarget.Set(
		"scrollTop",
		currentTarget.Get("scrollTop").Float()+raw.Get("deltaY").Float(),
	)
	event.PreventDefault()
}
