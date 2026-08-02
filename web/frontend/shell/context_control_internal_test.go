package shell

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// renderChrome renders a node to markup. shell_test.go's helper of the same
// purpose lives in the external test package and is not reachable from here.
func renderChrome(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func contextControlFixture() contextControlProps {
	return contextControlProps{
		Kind: "repository", Label: "codeflux", TargetPath: "/",
		Mode: primitives.Mode{}, OnNavigate: func(string) {},
	}
}

// TestAnOpenInstrumentPanelCanCloseItself is the regression check for a panel
// that stayed open.
//
// It was a bare <details>, which offers no way out: the panel remained until
// the summary was clicked a second time. Escape did nothing, a click elsewhere
// did nothing, and focus moving away did nothing. The dismissal contract is
// asserted on the overlay the panel is built from, because that is where the
// behaviour lives — GWC's key handlers cannot read which key was pressed, so an
// Escape rule written in this package could not exist at all.
func TestAnOpenInstrumentPanelCanCloseItself(t *testing.T) {
	dismissed := false
	panel := contextControlPanelWhenOpen(
		contextControlFixture(), true, "context-option-trigger-repository",
		func() { dismissed = true }, chromeTestTokens(t),
	)
	raw, found := panel.Props["__ui_props"]
	if !found {
		t.Fatal("the instrument panel is not an accessible overlay")
	}
	overlay, ok := raw.(ui.AccessibleOverlayProps)
	if !ok {
		t.Fatalf("the instrument panel carries %T rather than overlay props", raw)
	}
	if !overlay.CloseOnEscape {
		t.Error("the instrument panel does not close on Escape")
	}
	if !overlay.CloseOnOutsideClick {
		t.Error("the instrument panel does not close on a click outside it")
	}
	if !overlay.RestoreFocus {
		t.Error("the instrument panel does not return focus to its trigger")
	}
	// Non-modal: an anchored readout must not make the rest of the application
	// inert or trap the keyboard inside itself.
	if overlay.Modal || overlay.TrapFocus || overlay.BackgroundInert {
		t.Errorf("the instrument panel became modal: %#v", overlay)
	}
	if overlay.OnDismiss == nil {
		t.Fatal("the instrument panel has no dismiss handler to call")
	}
	overlay.OnDismiss()
	if !dismissed {
		t.Error("dismissing the instrument panel did not close the control")
	}
	if overlay.AnchorSelector != "#context-option-trigger-repository" {
		t.Errorf("the panel is anchored to %q rather than to its trigger",
			overlay.AnchorSelector)
	}
}

// TestAClosedInstrumentPanelMountsNothing proves a shut panel leaves no overlay
// in the document.
//
// A closed overlay that still mounts is what emptied a whole route frame once
// before, and an overlay present while shut is also how a dismissed panel goes
// on intercepting clicks aimed past it.
func TestAClosedInstrumentPanelMountsNothing(t *testing.T) {
	panel := contextControlPanelWhenOpen(
		contextControlFixture(), false, "context-option-trigger-repository",
		func() {}, chromeTestTokens(t),
	)
	if _, found := panel.Props["__ui_props"]; found {
		t.Error("a closed instrument panel still mounted an overlay")
	}
	markup := renderChrome(t, panel)
	if strings.Contains(markup, "context-option-panel") {
		t.Errorf("a closed instrument panel rendered its contents: %s", markup)
	}
}

// TestActingOnAnInstrumentPanelClosesItFirst proves the panel does not survive
// the navigation it triggers.
//
// Leaving it open behind a route change would put a stale readout over the new
// page, describing where the agent used to be standing.
func TestActingOnAnInstrumentPanelClosesItFirst(t *testing.T) {
	dismissed := false
	navigated := ""
	props := contextControlFixture()
	props.OnNavigate = func(path string) { navigated = path }
	panel := contextControlPanelWhenOpen(
		props, true, "context-option-trigger-repository",
		func() { dismissed = true }, chromeTestTokens(t),
	)
	handler, found := findButtonHandler(panel, "Open repositories")
	if !found || handler == nil {
		t.Fatal("the instrument panel offers no way to act on its subject")
	}
	handler()
	if !dismissed {
		t.Error("acting on the instrument panel left it open")
	}
	if navigated != "/" {
		t.Errorf("the instrument panel navigated to %q, want /", navigated)
	}
}

// TestAnInstrumentTriggerReportsWhetherItsPanelIsOpen proves the control says
// what it is doing to a screen reader.
func TestAnInstrumentTriggerReportsWhetherItsPanelIsOpen(t *testing.T) {
	markup := renderChrome(t, ContextControl(contextControlFixture()))
	for _, want := range []string{
		`aria-expanded="false"`,
		`aria-haspopup="true"`,
		`id="context-option-trigger-repository"`,
		"codeflux",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("a closed instrument does not render %q: %s", want, markup)
		}
	}
}
