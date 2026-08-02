package shell

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/css"
)

// capturedCSS collects the CSS the css package emits for each class.
type capturedCSS map[string]string

func (captured capturedCSS) Emit(class, text string) { captured[class] = text }

// captureChromeCSS builds a class and returns the CSS the framework emitted for
// it.
//
// The registry memoises emission process-wide, so it is reset first: without
// that, a class another test already built would emit nothing here and the
// assertion would pass against an empty string.
func captureChromeCSS(t *testing.T, build func() string) string {
	t.Helper()
	css.Reset()
	captured := capturedCSS{}
	previous := css.SetSink(captured)
	class := build()
	css.SetSink(previous)
	text, found := captured[class]
	if !found {
		t.Fatalf("no CSS was emitted for class %q", class)
	}
	return text
}

var stackingLayerPattern = regexp.MustCompile(`z-index\s*:\s*(-?\d+)`)

// declaredLayer reports the z-index a rule set declares.
func declaredLayer(t *testing.T, label, text string) int {
	t.Helper()
	match := stackingLayerPattern.FindStringSubmatch(text)
	if match == nil {
		t.Fatalf("%s declares no z-index: %s", label, text)
	}
	layer, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("%s has an unreadable z-index %q", label, match[1])
	}
	return layer
}

func chromeTestTokens(t *testing.T) design.Tokens {
	t.Helper()
	value, err := design.TokensFor(design.Options{
		Theme: design.ThemeDark, Density: design.DensityComfortable,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// TestTheApplicationBarOutranksEverythingInTheFrameBelowIt is the regression
// check for the bar's controls being unclickable.
//
// The bar declared no position and no z-index, and the application frame under
// it is position: relative. CSS paints positioned elements above non-positioned
// content regardless of document order, so the frame and everything positioned
// inside it painted over a bar that came first in the markup, and swallowed the
// clicks aimed at the bar's buttons. A z-index alone would not have fixed it
// either: z-index is inert on a static element, so the position is asserted
// here too.
func TestTheApplicationBarOutranksEverythingInTheFrameBelowIt(t *testing.T) {
	tokens := chromeTestTokens(t)
	barCSS := captureChromeCSS(t, func() string {
		return applicationBarClass([]css.Track{css.TrackAuto}, tokens)
	})
	if !strings.Contains(strings.ReplaceAll(barCSS, " ", ""), "position:relative") {
		t.Fatalf("the application bar is not positioned, so its z-index is inert: %s", barCSS)
	}
	bar := declaredLayer(t, "the application bar", barCSS)

	// The compact sidebar is the highest-stacked thing inside the frame, and it
	// is drawn over the bar's left-hand controls when it wins.
	sidebarCSS := captureChromeCSS(t, func() string {
		return productSidebarClass(
			state.LayoutPreferences{Viewport: state.ViewportMedium, RailWidth: 280},
			tokens,
		)
	})
	sidebar := declaredLayer(t, "the product sidebar", sidebarCSS)
	if bar <= sidebar {
		t.Errorf("application bar layer %d does not outrank the sidebar's %d",
			bar, sidebar)
	}

	// The skip link has to stay reachable on the very first Tab, so the bar
	// must not cover it.
	skipCSS := captureChromeCSS(t, func() string { return skipLinkClass(tokens) })
	skip := declaredLayer(t, "the skip link", skipCSS)
	if bar >= skip {
		t.Errorf("application bar layer %d covers the skip link at %d", bar, skip)
	}
}

// TestTheInstrumentStripClipsNothing proves the bar's disclosures can escape it.
//
// Each instrument opens a panel positioned below the bar. A clipping ancestor
// trims an absolutely positioned descendant away whatever z-index it declares,
// so an overflow rule here makes the panel's button unreachable while looking
// like a stacking problem.
func TestTheInstrumentStripClipsNothing(t *testing.T) {
	stripCSS := captureChromeCSS(t, instrumentStripClass)
	if strings.Contains(stripCSS, "overflow") {
		t.Errorf("the instrument strip clips its disclosure panels: %s", stripCSS)
	}
}

// TestAnInstrumentValueCanShrinkSoItsEllipsisWorks proves the row stays inside
// its track without an ancestor clipping it.
//
// The value already declared text-overflow: ellipsis, but a flex item defaults
// to min-width auto and this one also refused to shrink, so the ellipsis could
// never engage and the row had to be clipped from above instead. That is what
// made the clipping ancestor look necessary.
func TestAnInstrumentValueCanShrinkSoItsEllipsisWorks(t *testing.T) {
	tokens := chromeTestTokens(t)
	valueCSS := captureChromeCSS(t, func() string {
		return contextValueClass("repository", tokens)
	})
	compact := strings.ReplaceAll(valueCSS, " ", "")
	if strings.Contains(compact, "flex-shrink:0") {
		t.Errorf("an instrument value cannot shrink, so it can never ellipsize: %s",
			valueCSS)
	}
	if !strings.Contains(compact, "min-width:0") {
		t.Errorf("an instrument value keeps min-width auto, so it cannot shrink "+
			"below its text: %s", valueCSS)
	}
	if !strings.Contains(compact, "text-overflow:ellipsis") {
		t.Errorf("an instrument value does not ellipsize: %s", valueCSS)
	}
}
