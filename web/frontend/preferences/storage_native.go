//go:build !js || !wasm

package preferences

func openBrowserBackend() (Backend, error) {
	return nil, ErrStorageUnavailable
}
