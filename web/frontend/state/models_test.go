package state_test

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/state"
)

func TestSnapshotClonesCallerAndReaderSlices(t *testing.T) {
	input := []state.MessageView{{ID: "one", Body: "original"}}
	store := state.NewStore(state.NewSnapshot(nil, input, nil))
	input[0].Body = "caller mutation"

	first := store.Snapshot()
	got := first.Messages()
	if got[0].Body != "original" {
		t.Fatalf("caller mutated immutable snapshot: %q", got[0].Body)
	}
	got[0].Body = "reader mutation"
	if store.Snapshot().Messages()[0].Body != "original" {
		t.Fatal("reader mutated store through accessor")
	}
}

func TestRenderIsolationRevisions(t *testing.T) {
	store := state.NewStore(state.NewSnapshot(nil, nil, nil))
	base := store.Snapshot()

	cost := store.ReduceRemote(state.CostChanged{Label: "$0.12"}).Snapshot()
	if cost.TopBarRevision() == base.TopBarRevision() {
		t.Fatal("cost update did not invalidate top bar")
	}
	if cost.ConversationRevision() != base.ConversationRevision() || cost.GraphRevision() != base.GraphRevision() {
		t.Fatal("cost update invalidated conversation or graph")
	}

	chat := store.ReduceRemote(state.MessagesAppended{
		State: state.DataReady, Messages: []state.MessageView{{ID: "m1", Body: "hello"}},
	}).Snapshot()
	if chat.ConversationRevision() == base.ConversationRevision() {
		t.Fatal("message append did not invalidate conversation")
	}
	if chat.GraphRevision() != base.GraphRevision() {
		t.Fatal("message append invalidated graph")
	}

	selected, err := store.ReduceUI(state.GraphNodeSelected{NodeID: "n1"})
	if err != nil {
		t.Fatal(err)
	}
	graph := selected.Snapshot()
	if graph.GraphRevision() == base.GraphRevision() {
		t.Fatal("graph selection did not invalidate graph")
	}
	if graph.ConversationRevision() != base.ConversationRevision() {
		t.Fatal("graph selection invalidated messages")
	}
}

func TestLayoutPreferencesNormalizeRestoredValues(t *testing.T) {
	got := (state.LayoutPreferences{
		RailWidth: 9999, GraphWidth: 1, SplitPercent: 2,
		Viewport: "television", ActivePane: "unknown",
	}).Normalize()
	if got.RailWidth != 480 || got.GraphWidth != 320 || got.SplitPercent != 35 {
		t.Fatalf("unexpected clamped preferences: %+v", got)
	}
	if got.Viewport != state.ViewportWide || got.ActivePane != state.PaneConversation {
		t.Fatalf("unknown enums were not repaired: %+v", got)
	}
}

func TestUIActionsCannotExpressDurableTaskTransitions(t *testing.T) {
	store := state.NewStore(state.NewSnapshot(nil, nil, nil))
	if _, err := store.ReduceUI(nil); err != state.ErrInvalidUIAction {
		t.Fatalf("ReduceUI(nil) error = %v", err)
	}
	// This compile-time-closed interface is the contract: only this package can
	// implement UIAction, and it exports no task-state transition action.
	var _ state.UIAction = state.LayoutChanged{}
	var _ state.UIAction = state.RailCollapsed{}
	var _ state.UIAction = state.RailWidthChanged{}
	var _ state.UIAction = state.GraphCollapsed{}
	var _ state.UIAction = state.SplitChanged{}
	var _ state.UIAction = state.ThreadSelected{}
	var _ state.UIAction = state.GraphNodeSelected{}
}

func TestCollapseAndRestorePreserveSizedPreferences(t *testing.T) {
	store := state.NewStore(state.NewSnapshot(nil, nil, nil))
	resized, err := store.ReduceUI(state.SplitChanged{Percent: 70})
	if err != nil {
		t.Fatal(err)
	}
	collapsed, err := resized.ReduceUI(state.GraphCollapsed{Collapsed: true})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := collapsed.ReduceUI(state.GraphCollapsed{Collapsed: false})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Snapshot().Layout.SplitPercent != 70 || restored.Snapshot().Layout.GraphCollapsed {
		t.Fatalf("graph restore lost prior split: %+v", restored.Snapshot().Layout)
	}

	railSized, err := restored.ReduceUI(state.RailWidthChanged{Width: 360})
	if err != nil {
		t.Fatal(err)
	}
	railHidden, _ := railSized.ReduceUI(state.RailCollapsed{Collapsed: true})
	railShown, _ := railHidden.ReduceUI(state.RailCollapsed{Collapsed: false})
	if railShown.Snapshot().Layout.RailWidth != 360 || railShown.Snapshot().Layout.RailCollapsed {
		t.Fatalf("rail restore lost prior width: %+v", railShown.Snapshot().Layout)
	}
}
