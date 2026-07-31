//go:build js && wasm

package primitives

import "testing"

func TestVirtualListWASMScrollMathAndMissingDocumentAreSafe(t *testing.T) {
	if got := VirtualListScrollTop(0, 96, 48, 5); got != 192 {
		t.Fatalf("scroll target = %v, want 192", got)
	}
	// Headless WASM test runners need not provide a browser document. The
	// adapter must fail closed rather than panic in that environment.
	scrollVirtualListToIndex("missing-list", 5, 48)
}
