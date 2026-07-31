//go:build !js || !wasm

package primitives

import "testing"

func TestVirtualListNativeScrollRequestIsSafe(t *testing.T) {
	// Native SSR has no document. The same keyboard path must remain a safe
	// no-op while pure key resolution and markup remain testable.
	scrollVirtualListToIndex("atoms", 12, 48)
}
