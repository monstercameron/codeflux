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
