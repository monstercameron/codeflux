package graphcanvas

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/graphlayout"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type authoritativeGraphFixture struct {
	revision graph.Revision
	layout   graphlayout.Layout
	nodes    []domain.NodeID
}

func TestAuthoritativeRendererEmitsCompleteAccessibleVisualContract(t *testing.T) {
	fixture := newAuthoritativeGraphFixture(t)
	markup, err := ui.RenderToString(ui.CreateElement(Renderer, Props{Authoritative: &AuthoritativeProps{
		Revision: fixture.revision, Layout: fixture.layout,
		TaskState: domain.TaskStateRunning, SelectedNodeID: fixture.nodes[1],
		CurrentNodeID: fixture.nodes[2], ComparisonAvailable: true,
		ResponsiveMode: "wide",
	}}))
	if err != nil {
		t.Fatal(err)
	}

	for _, fragment := range []string{
		`data-component="authoritative-task-graph"`,
		`data-renderer="accessible-svg"`,
		`data-responsive-mode="wide"`,
		`data-graph-mode="execution"`,
		`data-component="graph-svg-root"`,
		`id="program-panel"`, `id="execution-panel"`, `id="evidence-panel"`,
		`role="tabpanel"`,
		`role="application"`,
		`data-keyboard="arrows-home-end"`,
		`data-pointer-pan="true"`,
		`data-wheel-zoom="focused-only"`,
		`aria-label="Graph mode"`,
		`Program`, `Execution`, `Evidence`,
		`aria-label="Fit graph"`,
		`aria-label="Zoom out"`,
		`aria-label="Zoom in"`,
		`aria-label="Reset graph view"`,
		`aria-label="Compare new graph revision"`,
		`New revision`,
		`aria-label="Return to current graph node"`,
		`data-component="graph-legend"`,
		`aria-label="Graph legend"`,
		`data-component="graph-selection-announcement"`,
		`aria-live="polite"`,
		`aria-atomic="true"`,
		`data-selected="true"`,
		`data-component="graph-selection-ring"`,
		`tabIndex="0"`,
	} {
		if !strings.Contains(markup, fragment) {
			t.Errorf("authoritative graph is missing %q\n%s", fragment, markup)
		}
	}

	for _, class := range graph.AllNodeClasses() {
		if fragment := `data-class="` + string(class) + `"`; !strings.Contains(markup, fragment) {
			t.Errorf("node class %q is not rendered", class)
		}
		if fragment := `data-node-class="` + string(class) + `"`; !strings.Contains(markup, fragment) {
			t.Errorf("node class %q is missing from the visible legend", class)
		}
	}
	for _, status := range graph.AllNodeStatuses() {
		if fragment := `data-status="` + string(status) + `"`; !strings.Contains(markup, fragment) {
			t.Errorf("node status %q is not rendered", status)
		}
		if fragment := `data-node-status="` + string(status) + `"`; !strings.Contains(markup, fragment) {
			t.Errorf("node status %q is missing from the visible legend", status)
		}
	}
	for _, class := range graph.AllEdgeClasses() {
		if fragment := `data-relationship="` + string(class) + `"`; !strings.Contains(markup, fragment) {
			t.Errorf("edge relationship %q is not rendered", class)
		}
		if fragment := `data-edge-class="` + string(class) + `"`; !strings.Contains(markup, fragment) {
			t.Errorf("edge relationship %q is missing from the visible legend", class)
		}
	}
	if !strings.Contains(markup, `aria-selected="true"`) || strings.Count(markup, `aria-pressed="true"`) != 1 {
		t.Fatalf("expected one selected mode and one selected node\n%s", markup)
	}
}

func TestAuthoritativeRendererRejectsIncompatibleLayoutSafely(t *testing.T) {
	fixture := newAuthoritativeGraphFixture(t)
	fixture.layout.AlgorithmVersion = "future-layout"
	markup, err := ui.RenderToString(ui.CreateElement(Renderer, Props{Authoritative: &AuthoritativeProps{
		Revision: fixture.revision, Layout: fixture.layout,
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-live="polite"`) || !strings.Contains(markup, "Graph unavailable") ||
		strings.Contains(markup, `data-component="graph-svg-root"`) {
		t.Fatalf("incompatible authoritative graph did not fail closed: %s", markup)
	}
}

func TestAuthoritativeStatusesRemainIndependentOfColorInHighContrast(t *testing.T) {
	fixture := newAuthoritativeGraphFixture(t)
	markup, err := ui.RenderToString(ui.CreateElement(Renderer, Props{Authoritative: &AuthoritativeProps{
		Revision: fixture.revision, Layout: fixture.layout,
		TaskState: domain.TaskStateRunning, VisualMode: primitives.Mode{HighContrast: true},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-high-contrast="true"`) {
		t.Fatalf("high-contrast graph contract is missing\n%s", markup)
	}
	seenIcons := map[string]bool{}
	for _, status := range graph.AllNodeStatuses() {
		icon := graphStatusIcon(status)
		if icon == "" || seenIcons[icon] {
			t.Fatalf("status %q lacks a unique non-color icon %q", status, icon)
		}
		seenIcons[icon] = true
		for _, fragment := range []string{
			`data-status="` + string(status) + `"`,
			`data-status-icon="` + icon + `"`,
			icon + " " + graphStatusLabel(status),
		} {
			if !strings.Contains(markup, fragment) {
				t.Errorf("high-contrast status %q is missing redundant encoding %q", status, fragment)
			}
		}
	}
	for _, dash := range []string{`stroke-dasharray="5 4"`, `stroke-dasharray="2 3"`, `stroke-dasharray="8 4 2 4"`} {
		if !strings.Contains(markup, dash) {
			t.Errorf("high-contrast status border pattern is missing %q", dash)
		}
	}
	t.Logf("M19-108 high-contrast graph preserves %d unique status icons, visible labels, and patterned borders", len(seenIcons))
}

func newAuthoritativeGraphFixture(t *testing.T) authoritativeGraphFixture {
	t.Helper()
	nodeClasses := graph.AllNodeClasses()
	statuses := graph.AllNodeStatuses()
	nodeIDs := make([]domain.NodeID, len(nodeClasses))
	nodes := make([]graph.Node, len(nodeClasses))
	for index, class := range nodeClasses {
		nodeIDs[index] = mustAuthoritativeID(t, domain.ParseNodeID,
			fmt.Sprintf("nod_01890f3c-4a00-7abc-8def-%012d", index+1))
		node, err := graph.NewNode(nodeIDs[index], class, statuses[index], graphClassLabel(class), graph.ContractSummary{}, graph.SourceLinks{})
		if err != nil {
			t.Fatal(err)
		}
		nodes[index] = node
	}
	edgeClasses := graph.AllEdgeClasses()
	edges := make([]graph.Edge, len(edgeClasses))
	for index, class := range edgeClasses {
		edgeID := mustAuthoritativeID(t, domain.ParseEdgeID,
			fmt.Sprintf("edg_01890f3c-4a00-7abc-8def-%012d", index+1))
		edge, err := graph.NewEdge(edgeID, class, nodeIDs[index], nodeIDs[index+1], graph.SourceLinks{})
		if err != nil {
			t.Fatal(err)
		}
		edges[index] = edge
	}
	graphID := mustAuthoritativeID(t, domain.ParseGraphID, "grf_01890f3c-4a00-7abc-8def-0123456789ab")
	revisionID := mustAuthoritativeID(t, domain.ParseGraphRevisionID, "grv_01890f3c-4a00-7abc-8def-0123456789ab")
	metadata, err := graph.NewRevisionMetadata(
		revisionID, graphID, 1, nil, graph.CurrentSchemaVersion,
		time.Date(2026, 7, 31, 20, 0, 0, 0, time.UTC), graph.SourceLinks{},
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := graph.NewRevision(metadata, nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	input := graphlayout.Input{Nodes: make([]graphlayout.Node, len(nodes)), Edges: make([]graphlayout.Edge, len(edges))}
	for index, node := range nodes {
		input.Nodes[index] = graphlayout.Node{ID: node.ID(), Class: node.Class()}
	}
	for index, edge := range edges {
		input.Edges[index] = graphlayout.Edge{ID: edge.ID(), FromNode: edge.FromNode(), ToNode: edge.ToNode()}
	}
	layout, err := graphlayout.Compute(input)
	if err != nil {
		t.Fatal(err)
	}
	return authoritativeGraphFixture{revision: revision, layout: layout, nodes: nodeIDs}
}

func mustAuthoritativeID[T any](t *testing.T, parse func(string) (T, error), raw string) T {
	t.Helper()
	value, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
