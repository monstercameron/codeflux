package dataview_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/dataview"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func mode() primitives.Mode {
	return primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable}
}

// TestLocalDataOffersNothingToForgetWhenNothingIsStored keeps the section from
// offering a destructive action against data that does not exist.
func TestLocalDataOffersNothingToForgetWhenNothingIsStored(t *testing.T) {
	markup := render(t, ui.CreateElement(dataview.Component, dataview.Props{Mode: mode()}))
	if !strings.Contains(markup, "No interface state is stored in this browser yet.") {
		t.Errorf("empty markup = %s", markup)
	}
	if strings.Contains(markup, "Forget interface state") {
		t.Errorf("markup offers to forget nothing: %s", markup)
	}
}

// TestLocalDataConfirmsBeforeForgetting checks the destructive action is
// confirmed and says what it does not touch, because a person reading "forget"
// on a settings page has no way to know how far it reaches.
func TestLocalDataConfirmsBeforeForgetting(t *testing.T) {
	markup := render(t, ui.CreateElement(dataview.Component, dataview.Props{
		Mode: mode(), Stored: true, Confirming: true,
		OnConfirm: func() {}, OnCancel: func() {},
	}))
	for _, want := range []string{
		"Forget the stored route, layout, and theme?",
		"Nothing in the coordinator changes.",
		"Keep it",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("confirmation markup missing %q", want)
		}
	}
}
