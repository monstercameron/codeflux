package atomname

import "strings"

// truncationEllipsis marks a visually truncated graph-node label.
const truncationEllipsis = "…"

// TruncatedLabel is a derived, display-only rendering of a DisplayName. It
// never replaces or mutates the stored DisplayName it was computed from
// (M21-175): a caller that truncates a label and later reads the owning
// AtomNameRecord.DisplayName still gets the full, untruncated name.
type TruncatedLabel struct {
	Text      string
	Truncated bool
}

// TruncateGraphNodeLabel derives the visual graph-node label for name,
// truncating only the returned Text when it would exceed maxRunes runes
// (M21-165, M21-166: truncate only the visual label; the tooltip,
// inspector, search result, and accessibility label continue to use the
// full DisplayName / CanonicalName the frontend graph lane reads directly
// from the stored AtomNameRecord, not from this function's output). A
// non-positive maxRunes is treated as "no truncation" rather than as an
// error, since a caller with no rendering budget has nothing useful to
// truncate to.
func TruncateGraphNodeLabel(name DisplayName, maxRunes int) TruncatedLabel {
	text := name.String()
	runes := []rune(text)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return TruncatedLabel{Text: text, Truncated: false}
	}

	ellipsisLength := len([]rune(truncationEllipsis))
	if maxRunes <= ellipsisLength {
		return TruncatedLabel{Text: string(runes[:maxRunes]), Truncated: true}
	}

	kept := strings.TrimRight(string(runes[:maxRunes-ellipsisLength]), " ")
	return TruncatedLabel{Text: kept + truncationEllipsis, Truncated: true}
}
