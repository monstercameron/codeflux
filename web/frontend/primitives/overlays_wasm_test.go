//go:build js && wasm

package primitives

import (
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	testrender "github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func overlayLifecycleHarness() ui.Node {
	open := ui.UseState(false)
	openDialog := ui.UseEvent(func() { open.Set(true) })
	closeDialog := func() { open.Set(false) }

	dialog := Dialog(OverlayProps{
		ID: "regression-dialog", Open: open.Get(),
		LabelledBy: "regression-dialog-title", InitialFocusSelector: "#regression-dialog-close",
		AppRootSelector: "#regression-app-root", OnDismiss: closeDialog,
		Content: html.Div(html.Props{},
			html.H2(html.Props{ID: "regression-dialog-title", Text: "Keyboard shortcuts"}),
			Button(ButtonProps{ID: "regression-dialog-close", Label: "Close", OnClick: closeDialog}),
		),
	})

	// This hook intentionally follows Dialog. Before the regression fix,
	// AccessibleOverlay's conditional open-state hooks were attached to this
	// component and displaced this stable state slot when the dialog opened.
	sentinel := ui.UseState("stable")
	return html.Div(html.Props{ID: "regression-app-root"},
		html.Button(html.Props{ID: "regression-dialog-open", Type: "button", OnClick: openDialog}, html.Text("Open shortcuts")),
		html.Output(html.Props{ID: "regression-sentinel", Text: sentinel.Get()}),
		dialog,
	)
}

func TestDialogClosedOpenClosedKeepsHookAndFocusContractsStable(t *testing.T) {
	fixture := testrender.New(t)
	fixture.Render(ui.CreateElement(overlayLifecycleHarness))
	if fixture.ByID("regression-dialog") != nil {
		t.Fatal("dialog rendered before it was opened")
	}

	fixture.ClickByID("regression-dialog-open")
	if dialog := fixture.ByRole("dialog", "Keyboard shortcuts"); dialog == nil {
		t.Fatal("dialog did not render after opening")
	}
	if got := fixture.BuildOverlayFocusSurfaceID(); got != "regression-dialog" {
		t.Fatalf("focus trap owner = %q, want regression-dialog", got)
	}
	if got := fixture.ByID("regression-sentinel").Text(); got != "stable" {
		t.Fatalf("hook following dialog shifted after open: %q", got)
	}

	fixture.ClickByID("regression-dialog-close")
	if fixture.ByID("regression-dialog") != nil {
		t.Fatal("dialog remained rendered after dismissal")
	}
	if got := fixture.BuildOverlayFocusSurfaceID(); got != "" {
		t.Fatalf("closed dialog retained focus trap ownership: %q", got)
	}

	fixture.ClickByID("regression-dialog-open")
	if fixture.ByID("regression-dialog") == nil {
		t.Fatal("dialog did not reopen after focus-restoring dismissal")
	}
}
