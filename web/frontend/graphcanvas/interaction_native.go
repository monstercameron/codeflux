//go:build !js || !wasm

package graphcanvas

import "github.com/monstercameron/GoWebComponents/v5/ui"

func pointerButton(ui.Event) int                        { return 0 }
func pointerClientPosition(ui.Event) (float64, float64) { return 0, 0 }
func pointerOffsetPosition(ui.Event) (float64, float64) { return 0, 0 }
func wheelDeltaY(ui.Event) float64                      { return 0 }
func browserDevicePixelRatio() float64                  { return 1 }
func capturePointer(ui.Event)                           {}
func releasePointer(ui.Event)                           {}
