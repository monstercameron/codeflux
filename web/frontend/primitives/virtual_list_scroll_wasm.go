//go:build js && wasm

package primitives

import "github.com/monstercameron/GoWebComponents/v5/interop"

func scrollVirtualListToIndex(listID string, targetIndex int, rowHeight float64) {
	document, err := interop.GetDocument()
	if err != nil {
		return
	}
	element, found, err := document.ElementByID(listID)
	if err != nil || !found {
		return
	}
	current, _, viewportHeight, err := element.ScrollMetrics()
	if err != nil {
		return
	}
	target := VirtualListScrollTop(current, viewportHeight, rowHeight, targetIndex)
	if target != current {
		_ = element.SetScrollTop(target)
	}
}
