package state_test

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/state"
)

func TestNamedStoreBoundariesCloneCollectionViews(t *testing.T) {
	threads := []state.ThreadView{{ID: "t1", Title: "Original"}}
	store := state.NewThreadStore(threads)
	threads[0].Title = "caller mutation"
	got := store.Threads()
	got[0].Title = "reader mutation"
	if store.Threads()[0].Title != "Original" {
		t.Fatal("ThreadStore leaked mutable slice ownership")
	}

	firstRun := state.NewFirstRunStore(state.FirstRunView{Completed: []string{"local-promise"}})
	view := firstRun.View()
	view.Completed[0] = "changed"
	if firstRun.View().Completed[0] != "local-promise" {
		t.Fatal("FirstRunStore leaked mutable slice ownership")
	}
}

func TestUIStorePreservesDraftAcrossLayoutRecovery(t *testing.T) {
	before := state.NewUIStore(
		state.DefaultLayoutPreferences(),
		map[string]string{"thread-1": "unsent answer"},
	)
	after := before.WithLayout(state.LayoutPreferences{
		Viewport: state.ViewportNarrow, ActivePane: state.PaneGraph, SplitPercent: 70,
	})
	if got := after.Draft("thread-1"); got != "unsent answer" {
		t.Fatalf("draft after recovery = %q", got)
	}
	if before.Layout().Viewport != state.ViewportWide {
		t.Fatal("immutable UIStore mutated the prior value")
	}
}
