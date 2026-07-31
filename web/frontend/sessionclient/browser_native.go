//go:build !js || !wasm

package sessionclient

import "context"

// BrowserConnector is present in native builds so lifecycle code can compile
// and tests can assert that browser-only authentication is never emulated.
type BrowserConnector struct {
	Reconnect TunnelReconnectPolicy
}

// Connect rejects native use because only a browser can supply the protected
// same-origin launch cookie used by the production bridge.
func (BrowserConnector) Connect(context.Context) (Connection, error) {
	return nil, ErrBrowserTransportUnavailable
}
