//go:build js && wasm

// Command graphmountfixture mounts the same typed graph/inspector seam used by
// the production task workspace for focused browser interaction evidence.
package main

import (
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/graphlayout"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/graphcanvas"
	"codeflux.dev/codeflux/web/frontend/graphinspector"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/GoWebComponents/v5/utils"
)

var fixtureRevision, fixtureLayout = mustFixtureGraph()

func main() {
	ui.Render(ui.CreateElement(graphMountFixture), "#app")
	utils.WaitForever()
}

func graphMountFixture() ui.Node {
	mode := primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable}
	nodes := fixtureRevision.Nodes()
	selected := ui.UseState(nodes[0].ID())
	detailLoads := ui.UseState(0)
	actionCount := ui.UseState(0)
	selectedNode := nodes[0]
	for _, node := range nodes {
		if node.ID() == selected.Get() {
			selectedNode = node
			break
		}
	}
	activate := func() { actionCount.Update(func(current int) int { return current + 1 }) }
	available := graphinspector.ActionAvailability{Enabled: true}
	inspectorProps := graphinspector.Props{
		Mode: mode, RevisionID: fixtureRevision.Metadata().ID(), Node: selectedNode,
		Evidence: graphinspector.EvidenceView{UnknownReason: "fixture evidence is intentionally bounded"},
		Duration: graphinspector.DurationAttribution{UnknownReason: "fixture duration is unavailable"},
		Tokens:   graphinspector.TokenAttribution{UnknownReason: "fixture tokens are unavailable"},
		Cost:     graphinspector.CostAttribution{UnknownReason: "fixture cost is unavailable"},
		Actions: graphinspector.Actions{
			ExplainInChat: available, ExpandNeighbors: available, DependencyCone: available,
			EvidenceCone: available, CompareRevision: available,
			OpenInEditor: graphinspector.ActionAvailability{DisabledReason: "fixture has no validated source location"},
		},
		OnExplainInChat:         func(domain.NodeID) { activate() },
		OnExpandNeighbors:       func(domain.NodeID) { activate() },
		OnIsolateDependencyCone: func(domain.NodeID) { activate() },
		OnIsolateEvidenceCone:   func(domain.NodeID) { activate() },
		OnCompareRevision:       func(domain.GraphRevisionID) { activate() },
	}
	authoritative := graphcanvas.AuthoritativeProps{
		Revision: fixtureRevision, Layout: fixtureLayout, TaskState: domain.TaskStateRunning,
		SelectedNodeID: selected.Get(), CurrentNodeID: nodes[0].ID(), VisualMode: mode,
		OnSelectNode: func(nodeID domain.NodeID) {
			selected.Set(nodeID)
			detailLoads.Update(func(current int) int { return current + 1 })
		},
	}
	return html.Main(html.Props{
		Data: map[string]string{
			"testid": "graph-mount-fixture", "detail-loads": fmt.Sprint(detailLoads.Get()),
			"action-count": fmt.Sprint(actionCount.Get()),
		},
	}, ui.CreateElement(shell.GraphPane, shell.GraphPaneProps{
		State: state.DataReady, Revision: 2, Viewport: state.ViewportWide, Mode: mode,
		Authoritative: &authoritative,
		Inspector:     ui.CreateElement(graphinspector.ContextInspector, inspectorProps),
	}))
}

func mustFixtureGraph() (graph.Revision, graphlayout.Layout) {
	first := mustID(domain.ParseNodeID, "nod", 1)
	second := mustID(domain.ParseNodeID, "nod", 2)
	nodeOne, err := graph.NewNode(first, graph.NodeClassRequirement, graph.NodeStatusActive, "Requirement", graph.ContractSummary{}, graph.SourceLinks{})
	if err != nil {
		panic(err)
	}
	nodeTwo, err := graph.NewNode(second, graph.NodeClassArtifactResult, graph.NodeStatusPending, "Implementation", graph.ContractSummary{}, graph.SourceLinks{})
	if err != nil {
		panic(err)
	}
	edge, err := graph.NewEdge(mustID(domain.ParseEdgeID, "edg", 1), graph.EdgeClassControl, first, second, graph.SourceLinks{})
	if err != nil {
		panic(err)
	}
	graphID := mustID(domain.ParseGraphID, "grf", 1)
	revisionID := mustID(domain.ParseGraphRevisionID, "grv", 2)
	parentID := mustID(domain.ParseGraphRevisionID, "grv", 1)
	metadata, err := graph.NewRevisionMetadata(revisionID, graphID, 2, &parentID, graph.CurrentSchemaVersion, time.Date(2026, 7, 31, 20, 0, 0, 0, time.UTC), graph.SourceLinks{})
	if err != nil {
		panic(err)
	}
	revision, err := graph.NewRevision(metadata, []graph.Node{nodeOne, nodeTwo}, []graph.Edge{edge})
	if err != nil {
		panic(err)
	}
	layout, err := graphlayout.Compute(graphlayout.Input{
		Nodes: []graphlayout.Node{{ID: first, Class: nodeOne.Class()}, {ID: second, Class: nodeTwo.Class()}},
		Edges: []graphlayout.Edge{{ID: edge.ID(), FromNode: first, ToNode: second}},
	})
	if err != nil {
		panic(err)
	}
	return revision, layout
}

func mustID[T any](parse func(string) (T, error), prefix string, ordinal int) T {
	value, err := parse(fmt.Sprintf("%s_01890f3c-4a00-7abc-8def-%012d", prefix, ordinal))
	if err != nil {
		panic(err)
	}
	return value
}
