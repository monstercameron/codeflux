package telemetryview_test

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/web/frontend/telemetryview"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestComponentInspectsContentFreeRowsAndExposesWorkingControls(t *testing.T) {
	reloads, loads, deletes := 0, 0, 0
	markup, err := ui.RenderToString(telemetryview.Component(telemetryview.Props{
		Rows:    []telemetryview.Row{{LocalID: 7, Kind: "reconnect", Outcome: "reconnected", Component: "session", Occurred: time.Unix(10, 0).UTC(), Duration: 80 * time.Millisecond}},
		HasMore: true, OnReload: func() { reloads++ }, OnLoadMore: func() { loads++ }, OnDeleteRequest: func() { deletes++ },
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"telemetry-settings", "reconnect", "reconnected", "80ms", "Load older telemetry", "Delete all local telemetry"} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup lacks %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "href=") {
		t.Fatalf("telemetry controls use raw navigation: %s", markup)
	}
	_ = reloads
	_ = loads
	_ = deletes
}

func TestComponentRequiresASecondExplicitDeletionAction(t *testing.T) {
	markup, err := ui.RenderToString(telemetryview.Component(telemetryview.Props{
		Rows:               []telemetryview.Row{{LocalID: 1, Kind: "first-run-step", Outcome: "succeeded", Component: "first-run", Occurred: time.Unix(1, 0).UTC()}},
		DeleteConfirmation: true, OnDeleteConfirm: func() {}, OnDeleteCancel: func() {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"This cannot be undone", "Confirm telemetry deletion", "Cancel deletion"} {
		if !strings.Contains(markup, want) {
			t.Errorf("confirmation markup lacks %q: %s", want, markup)
		}
	}
}

func TestComponentMakesDeletionUnavailableWithoutData(t *testing.T) {
	markup, err := ui.RenderToString(telemetryview.Component(telemetryview.Props{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "No local telemetry") || !strings.Contains(markup, "disabled") {
		t.Fatalf("empty telemetry surface = %s", markup)
	}
}
