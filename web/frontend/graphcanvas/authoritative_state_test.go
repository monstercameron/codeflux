package graphcanvas

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/graphlayout"
)

func TestGraphModeDefaultsFollowTaskLifecycle(t *testing.T) {
	for _, test := range []struct {
		name  string
		state domain.TaskState
		want  graph.Mode
	}{
		{"draft program", domain.TaskStateDraft, graph.ModeProgram},
		{"running execution", domain.TaskStateRunning, graph.ModeExecution},
		{"validation execution", domain.TaskStateValidating, graph.ModeExecution},
		{"review evidence", domain.TaskStateAwaitingReview, graph.ModeEvidence},
		{"completed evidence", domain.TaskStateCompleted, graph.ModeEvidence},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := DefaultGraphMode(test.state); got != test.want {
				t.Fatalf("DefaultGraphMode(%s) = %s, want %s", test.state, got, test.want)
			}
		})
	}
	if got := InitialGraphMode(graph.ModeEvidence, true, domain.TaskStateRunning); got != graph.ModeEvidence {
		t.Fatalf("explicit mode = %s, want evidence", got)
	}
	if got := InitialGraphMode(graph.Mode("unknown"), true, domain.TaskStateRunning); got != graph.ModeExecution {
		t.Fatalf("invalid explicit mode = %s, want execution fallback", got)
	}
}

func TestGraphPatchPreservesViewportSelectionAndExplicitMode(t *testing.T) {
	fixture := newAuthoritativeGraphFixture(t)
	before := AuthoritativeInteraction{
		Viewport:       Viewport{PanX: 91, PanY: -34, Zoom: 1.4},
		SelectedNodeID: fixture.nodes[3], Mode: graph.ModeProgram,
		UserSelectedMode: true,
	}
	after := ReconcileAuthoritativeInteraction(before, fixture.revision.Metadata().ID(), fixture.layout, domain.TaskStateCompleted)
	if after.Viewport != before.Viewport || after.SelectedNodeID != before.SelectedNodeID || after.Mode != graph.ModeProgram {
		t.Fatalf("compatible patch reset interaction state: before=%+v after=%+v", before, after)
	}

	removedLayout := fixture.layout
	removedLayout.Nodes = append([]graphlayout.Placement(nil), fixture.layout.Nodes[:1]...)
	after = ReconcileAuthoritativeInteraction(before, fixture.revision.Metadata().ID(), removedLayout, domain.TaskStateCompleted)
	if !after.SelectedNodeID.IsZero() || after.Viewport != before.Viewport {
		t.Fatalf("removed selection was not cleared independently of viewport: %+v", after)
	}

	before.UserSelectedMode = false
	after = ReconcileAuthoritativeInteraction(before, fixture.revision.Metadata().ID(), fixture.layout, domain.TaskStateCompleted)
	if after.Mode != graph.ModeEvidence {
		t.Fatalf("automatic completed-state mode = %s, want evidence", after.Mode)
	}
}

func TestKeyboardTraversalAndLinkedNodeActivationUseStableLayoutOrder(t *testing.T) {
	fixture := newAuthoritativeGraphFixture(t)
	first := TraverseAuthoritativeNodes(fixture.layout, domain.NodeID{}, "Home")
	last := TraverseAuthoritativeNodes(fixture.layout, first, "End")
	if first.IsZero() || last.IsZero() || first == last {
		t.Fatalf("unexpected traversal endpoints first=%s last=%s", first, last)
	}
	next := TraverseAuthoritativeNodes(fixture.layout, first, "ArrowRight")
	if next.IsZero() || next == first {
		t.Fatalf("right arrow did not advance from %s", first)
	}
	if got := TraverseAuthoritativeNodes(fixture.layout, next, "unhandled"); got != next {
		t.Fatalf("unhandled key changed selection from %s to %s", next, got)
	}

	current := AuthoritativeInteraction{Viewport: Viewport{Zoom: 1}}
	activated, ok := ActivateLinkedNode(current, fixture.layout, last, 900, 500)
	if !ok || activated.SelectedNodeID != last || activated.Viewport == current.Viewport {
		t.Fatalf("linked node was not selected and centered: ok=%v state=%+v", ok, activated)
	}
	missing := mustAuthoritativeID(t, domain.ParseNodeID, "nod_01890f3c-4a00-7abc-8def-999999999999")
	if unchanged, ok := ActivateLinkedNode(activated, fixture.layout, missing, 900, 500); ok || unchanged != activated {
		t.Fatalf("missing linked node mutated interaction: ok=%v state=%+v", ok, unchanged)
	}
}

func TestModeChangePreservesStableSelectionAndViewport(t *testing.T) {
	fixture := newAuthoritativeGraphFixture(t)
	current := AuthoritativeInteraction{
		Viewport:       Viewport{PanX: 28, PanY: -11, Zoom: 1.2},
		SelectedNodeID: fixture.nodes[4], Mode: graph.ModeExecution,
		RevisionID: fixture.revision.Metadata().ID(),
	}
	next, ok := SelectAuthoritativeMode(current, graph.ModeEvidence)
	if !ok || next.Mode != graph.ModeEvidence || !next.UserSelectedMode ||
		next.SelectedNodeID != current.SelectedNodeID || next.Viewport != current.Viewport {
		t.Fatalf("compatible mode change lost graph interaction state: ok=%v state=%+v", ok, next)
	}
	if unchanged, ok := SelectAuthoritativeMode(next, graph.Mode("unknown")); ok || unchanged != next {
		t.Fatalf("invalid graph mode mutated interaction state: ok=%v state=%+v", ok, unchanged)
	}
}

func TestExternalModeHydrationAndDragSuppression(t *testing.T) {
	fixture := newAuthoritativeGraphFixture(t)
	current := AuthoritativeInteraction{
		Viewport:       Viewport{PanX: 12, PanY: 7, Zoom: 1.1},
		SelectedNodeID: fixture.nodes[2], Mode: graph.ModeExecution,
	}
	next := SynchronizeAuthoritativeMode(current, graph.ModeEvidence, true, domain.TaskStateRunning)
	if next.Mode != graph.ModeEvidence || !next.UserSelectedMode ||
		next.Viewport != current.Viewport || next.SelectedNodeID != current.SelectedNodeID {
		t.Fatalf("hydrated mode preference lost compatible state: %+v", next)
	}
	next = SynchronizeAuthoritativeMode(next, graph.Mode("unknown"), true, domain.TaskStateCompleted)
	if next.Mode != graph.ModeEvidence || next.UserSelectedMode {
		t.Fatalf("invalid preference did not fall back to completed-state evidence: %+v", next)
	}
	if !shouldActivateAuthoritativeNodeAfterGesture(dragState{}) {
		t.Fatal("stationary node click was suppressed")
	}
	if shouldActivateAuthoritativeNodeAfterGesture(dragState{Moved: true}) {
		t.Fatal("post-pan node click was not suppressed")
	}
}

func TestLayoutIdentityChangesWhenAVisibleSliceChanges(t *testing.T) {
	fixture := newAuthoritativeGraphFixture(t)
	full := authoritativeLayoutIdentity(fixture.layout)
	reduced := fixture.layout
	reduced.Nodes = append([]graphlayout.Placement(nil), fixture.layout.Nodes[:len(fixture.layout.Nodes)-1]...)
	if full == authoritativeLayoutIdentity(reduced) {
		t.Fatal("layout identity did not change after the visible slice changed")
	}
}
