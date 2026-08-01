package graphcanvas

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestRendererEmitsCanvasInteractionAndAccessibilityContract(t *testing.T) {
	nodes := []state.GraphNodeView{
		{ID: "requirements", Title: "Shell requirements", Status: "complete"},
		{ID: "implementation", Title: "GWC workspace", Status: "active", Selected: true},
	}
	markup, err := ui.RenderToString(ui.CreateElement(Renderer, Props{
		Nodes:     nodes,
		Edges:     []Edge{{ID: "requirements-implementation", FromID: "requirements", ToID: "implementation", Kind: EdgeControl}},
		CurrentID: "implementation", ResponsiveMode: "wide",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`data-component="graph-canvas"`,
		`data-renderer="canvas-2d"`,
		`data-high-dpi="true"`,
		`data-responsive-mode="wide"`,
		`data-selected-node-id="implementation"`,
		`data-component="graph-render-surface"`,
		`data-node-id="requirements"`,
		`value="requirements"`,
		`aria-label="Shell requirements - Complete"`,
		`aria-label="GWC workspace - Active"`,
		`aria-label="Zoom in"`,
		`aria-label="Zoom out"`,
		`aria-label="Fit graph"`,
		`aria-label="Reset graph view"`,
		`Shell requirements`,
		`control to GWC workspace`,
	} {
		if !strings.Contains(markup, fragment) {
			t.Errorf("rendered graph is missing %q\n%s", fragment, markup)
		}
	}
	if got := strings.Count(markup, `aria-pressed="true"`); got != 1 {
		t.Fatalf("selected graph nodes = %d, want 1\n%s", got, markup)
	}
	if strings.Contains(markup, `aria-label="Return to current graph node"`) {
		t.Fatalf("return-to-current control should be hidden while current node is selected\n%s", markup)
	}

	inspected, err := ui.RenderToString(ui.CreateElement(Renderer, Props{
		Nodes: nodes, SelectedID: "requirements",
		Edges:     []Edge{{ID: "requirements-implementation", FromID: "requirements", ToID: "implementation", Kind: EdgeControl}},
		CurrentID: "implementation", ResponsiveMode: "wide",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inspected, `aria-label="Return to current graph node"`) {
		t.Fatalf("return-to-current control is missing during deliberate inspection\n%s", inspected)
	}
}

// TestRendererPreservesFullDisplayNameWhenTheVisualLabelIsTruncated is
// M21-165/M21-166: a node title longer than the canvas label budget must
// still expose its complete text through the native hover tooltip, the
// accessible label, and a full-label data hook, while the stable node
// identity (data-node-id / button value) stays independent of that label
// content entirely.
func TestRendererPreservesFullDisplayNameWhenTheVisualLabelIsTruncated(t *testing.T) {
	longTitle := "ValidateProviderEvidenceAcrossEveryTaskGraphRevision"
	if len([]rune(longTitle)) <= MaximumCanvasNodeLabelRunes {
		t.Fatalf("fixture title must exceed the canvas label budget of %d runes", MaximumCanvasNodeLabelRunes)
	}
	nodes := []state.GraphNodeView{
		{ID: "atom-1", Title: longTitle, Status: "active"},
		{ID: "atom-2", Title: "Short label", Status: "passed"},
	}
	markup, err := ui.RenderToString(ui.CreateElement(Renderer, Props{
		Nodes: nodes, CurrentID: "atom-1", ResponsiveMode: "wide",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`data-node-id="atom-1"`,
		`value="atom-1"`,
		`title="` + longTitle + `"`,
		`data-full-label="` + longTitle + `"`,
		`data-label-truncated="true"`,
		`aria-label="` + longTitle + ` - Active"`,
	} {
		if !strings.Contains(markup, fragment) {
			t.Errorf("truncated-label node is missing %q\n%s", fragment, markup)
		}
	}
	if !strings.Contains(markup, `data-node-id="atom-2"`) || !strings.Contains(markup, `data-label-truncated="false"`) {
		t.Errorf("short-label node should report an untruncated label\n%s", markup)
	}

	// Re-render with the same stable node ID but a different display name:
	// the identity must remain resolvable and unchanged while the label text
	// follows the rename.
	renamed := []state.GraphNodeView{
		{ID: "atom-1", Title: "Renamed after documentation revision", Status: "active"},
		nodes[1],
	}
	renamedMarkup, err := ui.RenderToString(ui.CreateElement(Renderer, Props{
		Nodes: renamed, CurrentID: "atom-1", ResponsiveMode: "wide",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(renamedMarkup, `data-node-id="atom-1"`) || !strings.Contains(renamedMarkup, `value="atom-1"`) {
		t.Fatalf("node identity must survive a display-name change\n%s", renamedMarkup)
	}
	if !strings.Contains(renamedMarkup, `data-full-label="Renamed after documentation revision"`) {
		t.Fatalf("renamed node's full label was not applied\n%s", renamedMarkup)
	}
	if strings.Contains(renamedMarkup, `data-full-label="`+longTitle+`"`) {
		t.Fatalf("stale label from before the rename leaked into the re-rendered markup\n%s", renamedMarkup)
	}
}

func TestRendererKeepsInvalidGraphVisibleAsAlert(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(Renderer, Props{
		Nodes: []state.GraphNodeView{{ID: "node", Title: "One"}, {ID: "node", Title: "Duplicate"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Graph unavailable") || strings.Contains(markup, `data-component="graph-render-surface"`) {
		t.Fatalf("invalid graph fallback did not render safely: %s", markup)
	}
}
