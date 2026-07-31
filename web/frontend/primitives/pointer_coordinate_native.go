//go:build !js || !wasm

package primitives

import "github.com/monstercameron/GoWebComponents/v5/ui"

func splitPointerCoordinate(_ ui.Event, _ SplitOrientation) float64 { return 0 }
