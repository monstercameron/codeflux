//go:build !js || !wasm

package main

import (
	"context"

	"codeflux.dev/codeflux/internal/domain"
)

//lint:ignore U1000 Native contract sentinel for the js/wasm bridge entry point.
func openBrowserThreadRailClient(
	context.Context,
	domain.RepositoryID,
	domain.WorkspaceID,
) (threadRailClientLease, error) {
	return threadRailClientLease{}, errThreadRailBridgeUnavailable
}
