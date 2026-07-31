//go:build js && wasm

package primitives

import "github.com/monstercameron/GoWebComponents/v5/ui"

func splitPointerCoordinate(event ui.Event, orientation SplitOrientation) float64 {
	if orientation == SplitVertical {
		return event.GetClientY()
	}
	return event.GetClientX()
}
