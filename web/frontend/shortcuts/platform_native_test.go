//go:build !js || !wasm

package shortcuts_test

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/shortcuts"
)

func TestCurrentPlatformReturnsKnownNativePlatform(t *testing.T) {
	switch got := shortcuts.CurrentPlatform(); got {
	case shortcuts.PlatformMacOS, shortcuts.PlatformWindows, shortcuts.PlatformLinux, shortcuts.PlatformOther:
	default:
		t.Fatalf("CurrentPlatform = %q", got)
	}
}
