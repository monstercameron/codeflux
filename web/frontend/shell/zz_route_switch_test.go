package shell_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/state"
)

// TestThreadWorkspaceRendersAtEveryViewport guards the surface that came up
// blank in the browser: the workspace must produce its transcript and composer
// at every width, not only at the one the reading column was written for.
func TestThreadWorkspaceRendersAtEveryViewport(t *testing.T) {
	for _, viewport := range []state.ViewportClass{
		state.ViewportWide, state.ViewportMedium, state.ViewportNarrow, state.ViewportMinimum,
	} {
		snapshot := readySnapshot()
		layout := snapshot.Layout.Normalize()
		layout.Viewport = viewport
		store := state.NewStore(snapshot)
		store, err := store.ReduceUI(state.LayoutChanged{Preferences: layout})
		if err != nil {
			t.Fatalf("%s layout: %v", viewport, err)
		}
		markup := render(t, shell.RouteShell(shell.RouteShellProps{
			Snapshot: store.Snapshot(), Route: routes.Route{Name: routes.ThreadWorkspace}, Tokens: tokens(t),
		}))
		for _, want := range []string{
			`data-component="task-workspace-shell"`,
			`data-component="conversation-pane"`,
			`data-component="composer"`,
		} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s workspace missing %q", viewport, want)
			}
		}
	}
}
