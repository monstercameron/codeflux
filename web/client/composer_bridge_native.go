//go:build !js || !wasm

package main

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

//lint:ignore U1000 Native contract sentinel for the js/wasm composer bridge.
func sendComposerCommand(context.Context, composerSendCommand) (domain.MessageID, error) {
	return domain.MessageID{}, errors.New("browser composer bridge is unavailable")
}
