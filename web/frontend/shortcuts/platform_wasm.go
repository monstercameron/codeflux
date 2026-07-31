//go:build js && wasm

package shortcuts

import "github.com/monstercameron/GoWebComponents/v5/interop"

func currentPlatform() Platform {
	global, err := interop.GetGlobalThis()
	if err != nil {
		return PlatformOther
	}
	navigator := global.Get("navigator")
	if !navigator.Present() {
		return PlatformOther
	}
	if userAgentData := navigator.Get("userAgentData"); userAgentData.Present() {
		if platform := userAgentData.Get("platform").String(); platform != "" {
			return ParsePlatform(platform)
		}
	}
	return ParsePlatform(navigator.Get("platform").String())
}
