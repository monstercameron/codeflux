//go:build !js || !wasm

package graphcanvas

import "github.com/monstercameron/GoWebComponents/v5/ui"

func drawCanvas(ui.DOMRef, drawFrame) {}
