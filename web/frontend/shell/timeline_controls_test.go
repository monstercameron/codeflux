package shell_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/timeline"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTimelineControlsMountEveryCorrectnessBearingTransition(t *testing.T) {
	t.Parallel()
	markup, err := ui.RenderToString(ui.CreateElement(shell.TimelineControls, shell.TimelineControlProps{
		Enabled: true, HasOlder: true, OlderError: "Older messages could not be loaded.",
		Gaps: []timeline.SequenceGap{{After: 2, Before: 4}}, NewEvents: 3,
		ReviewOpen: true, ReturnToCurrentAvailable: true,
		Latency:     timelinecard.LatencyPresentation{Phase: "Validating", ShowStop: true},
		OnLoadOlder: func() {}, OnRetryOlder: func() {}, OnReturnLive: func() {},
		OnOpenReview: func() {}, OnCloseReview: func() {}, OnReturnToCurrent: func() {}, OnStop: func() {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{
		`data-component="timeline-controls"`,
		"Load older messages",
		`data-component="older-page-error"`,
		`data-component="sequence-gap-recovery"`,
		"3 new events",
		`data-component="review-drawer"`,
		`data-component="drawer"`,
		`data-focus-policy="trap-restore"`,
		`data-dismiss-policy="escape-outside"`,
		`id="task-review-trigger"`,
		`aria-expanded="true"`,
		`aria-controls="task-review-drawer"`,
		`id="task-review-close"`,
		`position="preserved"`,
		"Return to current graph node",
		`data-component="first-message-latency"`,
		"Stop delayed task",
	} {
		if !strings.Contains(markup, contract) {
			t.Fatalf("missing mounted contract %q in %s", contract, markup)
		}
	}
}

func TestTimelineControlsRemainAbsentUnlessAuthoritativeOwnerEnablesThem(t *testing.T) {
	t.Parallel()
	markup, err := ui.RenderToString(ui.CreateElement(shell.TimelineControls, shell.TimelineControlProps{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, `data-component="timeline-controls"`) {
		t.Fatalf("disabled controls leaked into mounted shell: %s", markup)
	}
}
