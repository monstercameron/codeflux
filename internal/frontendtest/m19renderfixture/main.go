//go:build js && wasm

// Command m19renderfixture mounts the authoritative 300-node task graph with
// independent graph-patch and conversation-token streams for browser evidence.
package main

import (
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/graphlayout"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/graphcanvas"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/GoWebComponents/v5/utils"
)

const (
	m19NodeCount  = 300
	m19PatchCount = 100
	m19TokenCount = 1000
)

type m19Fixture struct {
	graphID domain.GraphID
	nodes   []graph.Node
	edges   []graph.Edge
	layout  graphlayout.Layout
}

var m19Data = mustM19Fixture()

func main() {
	ui.Render(ui.CreateElement(m19PerformanceFixture), "#app")
	utils.WaitForever()
}

func m19PerformanceFixture() ui.Node {
	mode := primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable}
	return html.Main(html.Props{DataAttr: html.DataAttribute{Name: "testid", Value: "m19-performance-fixture"}},
		html.H1(html.Props{Text: "M19 graph performance fixture"}),
		ui.CreateElement(m19TokenStreamBoundary, mode),
		ui.CreateElement(m19GraphPatchBoundary, mode),
	)
}

func m19TokenStreamBoundary(mode primitives.Mode) ui.Node {
	tokenCount := ui.UseState(0)
	running := ui.UseState(false)
	ui.UseInterval(func() {
		if !running.Get() {
			return
		}
		if tokenCount.Get() >= m19TokenCount {
			running.Set(false)
			return
		}
		tokenCount.Update(func(current int) int { return current + 1 })
	}, 30*time.Millisecond, running.Get())
	start := ui.UseEvent(func() {
		tokenCount.Set(0)
		running.Set(true)
	})
	message := state.MessageView{
		ID: "m19-stream-message", Role: "execution", Sequence: 1,
		Body: "Streaming response " + strings.Repeat("token ", tokenCount.Get()),
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "M19 conversation stream"},
		Data: map[string]string{
			"component": "m19-token-stream", "token-count": fmt.Sprint(tokenCount.Get()),
			"streaming": fmt.Sprint(running.Get()),
		},
	},
		html.Button(html.Props{
			Type: "button", Text: "Start token stream", OnClick: start,
			DataAttr: html.DataAttribute{Name: "testid", Value: "m19-start-tokens"},
		}),
		ui.CreateElement(shell.ConversationPane, shell.ConversationPaneProps{
			State: state.DataReady, Messages: []state.MessageView{message},
			Revision: uint64(tokenCount.Get() + 1), Mode: mode,
		}),
	)
}

func m19GraphPatchBoundary(mode primitives.Mode) ui.Node {
	patchCount := ui.UseState(0)
	running := ui.UseState(false)
	selected := ui.UseState(m19Data.nodes[0].ID())
	ui.UseInterval(func() {
		if !running.Get() {
			return
		}
		if patchCount.Get() >= m19PatchCount {
			running.Set(false)
			return
		}
		patchCount.Update(func(current int) int { return current + 1 })
	}, 75*time.Millisecond, running.Get())
	start := ui.UseEvent(func() {
		patchCount.Set(0)
		running.Set(true)
	})
	applyPatch := ui.UseEvent(func() {
		if patchCount.Get() < m19PatchCount {
			patchCount.Update(func(current int) int { return current + 1 })
		}
	})
	revision := mustM19Revision(patchCount.Get())
	return html.Section(html.Props{
		Aria: map[string]string{"label": "M19 graph patch stream"},
		Data: map[string]string{
			"component": "m19-graph-stream", "patch-count": fmt.Sprint(patchCount.Get()),
			"streaming": fmt.Sprint(running.Get()),
		},
	},
		html.Button(html.Props{
			Type: "button", Text: "Start graph patches", OnClick: start,
			DataAttr: html.DataAttribute{Name: "testid", Value: "m19-start-patches"},
		}),
		html.Button(html.Props{
			Type: "button", Text: "Apply graph patch", OnClick: applyPatch,
			DataAttr: html.DataAttribute{Name: "testid", Value: "m19-apply-patch"},
		}),
		ui.CreateElement(graphcanvas.Renderer, graphcanvas.Props{Authoritative: &graphcanvas.AuthoritativeProps{
			Revision: revision, Layout: m19Data.layout, TaskState: domain.TaskStateRunning,
			SelectedNodeID: selected.Get(), CurrentNodeID: m19Data.nodes[patchCount.Get()%len(m19Data.nodes)].ID(),
			ResponsiveMode: "wide", VisualMode: mode, Height: 680,
			OnSelectNode: selected.Set,
		}}),
	)
}

func mustM19Fixture() m19Fixture {
	nodes := make([]graph.Node, m19NodeCount)
	nodeIDs := make([]domain.NodeID, m19NodeCount)
	classes := graph.AllNodeClasses()
	statuses := graph.AllNodeStatuses()
	for index := range nodes {
		nodeIDs[index] = mustM19ID(domain.ParseNodeID, "nod", index+1)
		node, err := graph.NewNode(
			nodeIDs[index], classes[index%len(classes)], statuses[index%len(statuses)],
			fmt.Sprintf("Graph node %03d", index+1), graph.ContractSummary{}, graph.SourceLinks{},
		)
		if err != nil {
			panic(err)
		}
		nodes[index] = node
	}
	edges := make([]graph.Edge, m19NodeCount-1)
	for index := range edges {
		edge, err := graph.NewEdge(
			mustM19ID(domain.ParseEdgeID, "edg", index+1),
			graph.AllEdgeClasses()[index%len(graph.AllEdgeClasses())],
			nodeIDs[index], nodeIDs[index+1], graph.SourceLinks{},
		)
		if err != nil {
			panic(err)
		}
		edges[index] = edge
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
		panic(err)
	}
	return m19Fixture{
		graphID: mustM19ID(domain.ParseGraphID, "grf", 1), nodes: nodes, edges: edges, layout: layout,
	}
}

func mustM19Revision(patch int) graph.Revision {
	nodes := append([]graph.Node(nil), m19Data.nodes...)
	changed := patch % len(nodes)
	base := nodes[changed]
	status := graph.AllNodeStatuses()[(patch+changed)%len(graph.AllNodeStatuses())]
	replacement, err := graph.NewNode(
		base.ID(), base.Class(), status, base.DisplayName(), base.Contract(), base.Sources(),
	)
	if err != nil {
		panic(err)
	}
	nodes[changed] = replacement
	ordinal := uint64(patch + 1)
	revisionID := mustM19ID(domain.ParseGraphRevisionID, "grv", patch+1)
	var parent *domain.GraphRevisionID
	if ordinal > 1 {
		value := mustM19ID(domain.ParseGraphRevisionID, "grv", patch)
		parent = &value
	}
	metadata, err := graph.NewRevisionMetadata(
		revisionID, m19Data.graphID, ordinal, parent, graph.CurrentSchemaVersion,
		time.Date(2026, 7, 31, 20, 0, 0, 0, time.UTC).Add(time.Duration(patch)*time.Second), graph.SourceLinks{},
	)
	if err != nil {
		panic(err)
	}
	revision, err := graph.NewRevision(metadata, nodes, m19Data.edges)
	if err != nil {
		panic(err)
	}
	return revision
}

func mustM19ID[T any](parse func(string) (T, error), prefix string, ordinal int) T {
	raw := fmt.Sprintf("%s_01890f3c-4a00-7abc-8def-%012d", prefix, ordinal)
	value, err := parse(raw)
	if err != nil {
		panic(err)
	}
	return value
}
