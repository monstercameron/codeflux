//go:build !js || !wasm

package shortcuts

import "runtime"

func currentPlatform() Platform { return ParsePlatform(runtime.GOOS) }
