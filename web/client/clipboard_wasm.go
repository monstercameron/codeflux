//go:build js && wasm

package main

import "syscall/js"

// copyToClipboard writes one value to the browser's clipboard.
//
// The write is issued inside the click that asked for it and its promise is
// deliberately not awaited. Both halves matter: a browser honours a clipboard
// write only while it is still handling the gesture, so moving it to a
// goroutine makes the call succeed in Go and do nothing in the page; and
// awaiting the promise from inside the handler cannot work either, because the
// promise resolves on the same loop the handler is holding.
//
// The older execCommand path is kept for a browser that offers no asynchronous
// clipboard, which is what one outside a secure context reports.
func copyToClipboard(value string) {
	if value == "" {
		return
	}
	document := js.Global().Get("document")
	if navigator := js.Global().Get("navigator"); navigator.Truthy() {
		if clipboard := navigator.Get("clipboard"); clipboard.Truthy() {
			result := clipboard.Call("writeText", value)
			if result.Truthy() {
				// A rejection is swallowed on purpose: a permission a person
				// declined is their answer, not an error to report back at
				// them, and the text is still on screen to select by hand.
				result.Call("catch", js.FuncOf(func(js.Value, []js.Value) any { return nil }))
			}
			return
		}
	}
	if !document.Truthy() {
		return
	}
	field := document.Call("createElement", "textarea")
	field.Set("value", value)
	field.Get("style").Set("position", "fixed")
	field.Get("style").Set("opacity", "0")
	document.Get("body").Call("appendChild", field)
	field.Call("select")
	document.Call("execCommand", "copy")
	document.Get("body").Call("removeChild", field)
}
