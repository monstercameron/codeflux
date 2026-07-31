//go:build !js || !wasm

package preferences_test

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/web/frontend/preferences"
)

func TestOpenBrowserStoreReportsUnavailableOnNativeBuild(t *testing.T) {
	_, err := preferences.OpenBrowserStore()
	if !errors.Is(err, preferences.ErrStorageUnavailable) {
		t.Fatalf("OpenBrowserStore error = %v", err)
	}
}
