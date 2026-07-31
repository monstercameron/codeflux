package shortcuts

import "strings"

// Platform controls primary-modifier matching and user-facing key labels.
type Platform string

const (
	PlatformMacOS   Platform = "macos"
	PlatformWindows Platform = "windows"
	PlatformLinux   Platform = "linux"
	PlatformOther   Platform = "other"
)

// ParsePlatform normalizes browser and operating-system platform names.
func ParsePlatform(raw string) Platform {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case normalized == "darwin", normalized == "ios", strings.Contains(normalized, "mac"), strings.Contains(normalized, "iphone"), strings.Contains(normalized, "ipad"):
		return PlatformMacOS
	case strings.Contains(normalized, "win"):
		return PlatformWindows
	case strings.Contains(normalized, "linux"), strings.Contains(normalized, "android"):
		return PlatformLinux
	default:
		return PlatformOther
	}
}

// CurrentPlatform detects the current platform through the platform-specific
// implementation. Browser builds use GWC interop rather than handwritten JS.
func CurrentPlatform() Platform { return currentPlatform() }

// Label formats a concise visible shortcut label for the given platform.
func (chord Chord) Label(platform Platform) string {
	parts := chord.labelParts(platform, false)
	if platform == PlatformMacOS {
		return strings.Join(parts, "")
	}
	return strings.Join(parts, "+")
}

// AccessibleLabel formats a screen-reader-friendly shortcut label.
func (chord Chord) AccessibleLabel(platform Platform) string {
	return strings.Join(chord.labelParts(platform, true), "+")
}

func (chord Chord) labelParts(platform Platform, accessible bool) []string {
	parts := make([]string, 0, 4)
	if chord.Primary {
		switch {
		case platform == PlatformMacOS && accessible:
			parts = append(parts, "Command")
		case platform == PlatformMacOS:
			parts = append(parts, "⌘")
		default:
			parts = append(parts, "Ctrl")
		}
	}
	if chord.Alt {
		switch {
		case platform == PlatformMacOS && accessible:
			parts = append(parts, "Option")
		case platform == PlatformMacOS:
			parts = append(parts, "⌥")
		default:
			parts = append(parts, "Alt")
		}
	}
	if chord.Shift {
		if platform == PlatformMacOS && !accessible {
			parts = append(parts, "⇧")
		} else {
			parts = append(parts, "Shift")
		}
	}
	parts = append(parts, displayKey(chord.Key))
	return parts
}

func displayKey(key string) string {
	normalized := normalizeKey(key)
	if len(normalized) == 1 {
		return strings.ToUpper(normalized)
	}
	switch normalized {
	case "escape":
		return "Esc"
	case "arrowleft":
		return "Left Arrow"
	case "arrowright":
		return "Right Arrow"
	default:
		return normalized
	}
}
