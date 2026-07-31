//go:build !js || !wasm

package main

import (
	"context"
	"errors"
)

//lint:ignore U1000 Native contract sentinel for the js/wasm approval bridge.
func resolveApprovalCommand(context.Context, approvalCommand) (uint64, error) {
	return 0, errors.New("browser approval bridge is unavailable")
}
