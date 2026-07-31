package timeline

import "fmt"

// ScrollMetrics is a browser measurement converted to integral CSS pixels.
type ScrollMetrics struct {
	ScrollTop    int
	ClientHeight int
	ScrollHeight int
}

// ShouldLoadOlder requests another bounded page near the top only when a load
// is useful and no prior request is active.
func ShouldLoadOlder(metrics ScrollMetrics, threshold int, hasOlder, loading bool) bool {
	if threshold < 0 || metrics.ScrollTop < 0 || metrics.ClientHeight < 0 || metrics.ScrollHeight < 0 {
		return false
	}
	return hasOlder && !loading && metrics.ScrollTop <= threshold
}

// ShouldAutoFollow returns true only when the user was already near the live
// edge before new content arrived.
func ShouldAutoFollow(metrics ScrollMetrics, threshold int) bool {
	if threshold < 0 || metrics.ScrollTop < 0 || metrics.ClientHeight < 0 ||
		metrics.ScrollHeight < metrics.ClientHeight {
		return false
	}
	distance := metrics.ScrollHeight - metrics.ClientHeight - metrics.ScrollTop
	return distance <= threshold
}

// FollowState drives the inline new-events action without moving focus.
type FollowState struct {
	AtLiveEdge bool
	NewEvents  int
}

// ObserveNewEvents updates follow state from the pre-update viewport.
func ObserveNewEvents(current FollowState, metrics ScrollMetrics, threshold, count int) FollowState {
	if count <= 0 {
		return current
	}
	if ShouldAutoFollow(metrics, threshold) {
		return FollowState{AtLiveEdge: true}
	}
	current.AtLiveEdge = false
	current.NewEvents += count
	return current
}

// ReturnToLive clears the badge and requests one deliberate live-edge scroll.
func ReturnToLive(FollowState) FollowState { return FollowState{AtLiveEdge: true} }

// ShowNewEvents reports whether the inline button is required.
func (state FollowState) ShowNewEvents() bool { return !state.AtLiveEdge && state.NewEvents > 0 }

// Measurement captures a stable item's document-space top and variable height.
type Measurement struct {
	Key    ItemKey
	Top    int
	Height int
}

// PreserveAnchor returns the scroll position that keeps the same item at the
// same viewport offset after older variable-height cards are prepended.
func PreserveAnchor(
	previousScrollTop int,
	key ItemKey,
	before, after []Measurement,
) (int, error) {
	if previousScrollTop < 0 || key == "" {
		return 0, fmt.Errorf("timeline anchor is invalid")
	}
	beforeTop, beforeFound := measuredTop(before, key)
	afterTop, afterFound := measuredTop(after, key)
	if !beforeFound || !afterFound {
		return 0, fmt.Errorf("timeline anchor %q is not present in both layouts", key)
	}
	next := previousScrollTop + afterTop - beforeTop
	if next < 0 {
		next = 0
	}
	return next, nil
}

func measuredTop(measurements []Measurement, key ItemKey) (int, bool) {
	for _, measurement := range measurements {
		if measurement.Key == key && measurement.Top >= 0 && measurement.Height >= 0 {
			return measurement.Top, true
		}
	}
	return 0, false
}
