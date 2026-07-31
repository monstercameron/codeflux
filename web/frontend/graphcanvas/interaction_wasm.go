//go:build js && wasm

package graphcanvas

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func pointerButton(event ui.Event) int { return event.GetButton() }

func pointerClientPosition(event ui.Event) (float64, float64) {
	return event.GetClientX(), event.GetClientY()
}

func pointerOffsetPosition(event ui.Event) (float64, float64) {
	return event.GetOffsetX(), event.GetOffsetY()
}

func wheelDeltaY(event ui.Event) float64 {
	value := event.JSValue()
	if !value.Truthy() {
		return 0
	}
	delta := value.Get("deltaY")
	if delta.Type() != js.TypeNumber {
		return 0
	}
	return delta.Float()
}

func browserDevicePixelRatio() float64 {
	value := js.Global().Get("devicePixelRatio")
	if value.Type() != js.TypeNumber {
		return 1
	}
	return value.Float()
}

func capturePointer(event ui.Event) {
	value := event.JSValue()
	target := value.Get("currentTarget")
	pointerID := value.Get("pointerId")
	if target.Truthy() && target.Get("setPointerCapture").Type() == js.TypeFunction && pointerID.Type() == js.TypeNumber {
		target.Call("setPointerCapture", pointerID)
	}
}

func releasePointer(event ui.Event) {
	value := event.JSValue()
	target := value.Get("currentTarget")
	pointerID := value.Get("pointerId")
	if target.Truthy() && target.Get("releasePointerCapture").Type() == js.TypeFunction && pointerID.Type() == js.TypeNumber {
		target.Call("releasePointerCapture", pointerID)
	}
}

func focusGraphNode(nodeID string) {
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	element := document.Call("getElementById", graphNodeElementIDFromString(nodeID))
	if element.Truthy() && element.Get("focus").Type() == js.TypeFunction {
		element.Call("focus")
	}
}

func graphNodeElementIDFromString(nodeID string) string { return "graph-svg-node-" + nodeID }
