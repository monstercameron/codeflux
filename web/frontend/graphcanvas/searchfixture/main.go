//go:build js && wasm

// Command searchfixture mounts graphcanvas.Authoritative with its search
// props wired to a small, fixed, in-memory node set, so REPO-036's graph
// search control can be driven with real clicks and keystrokes in a browser
// without a coordinator, a database, or a network round trip.
//
// It is a verification fixture, not a product surface: the production graph
// search asks the server's SearchGraph RPC (searchGraphResource in
// web/client), which this fixture intentionally does not exercise. What it
// does exercise is everything graphcanvas.Authoritative itself owns — the
// search field, the loading/empty/populated states, keyboard operation, and
// selecting a match to focus and center a node — which is exactly the part
// AGENTS.md requires a real browser to confirm.
package main

import (
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/graphlayout"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/graphcanvas"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/GoWebComponents/v5/utils"
)

var fixtureRevision, fixtureLayout = mustFixtureGraph()

func main() {
	ui.Render(ui.CreateElement(searchFixture), "#app")
	utils.WaitForever()
}

func searchFixture() ui.Node {
	mode := primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable}
	nodes := fixtureRevision.Nodes()

	query := ui.UseState("")
	results := ui.UseState([]graph.Node{})
	loading := ui.UseState(false)
	status := ui.UseState("")
	focusNodeID := ui.UseState(domain.NodeID{})
	selected := ui.UseState(domain.NodeID{})
	selectCount := ui.UseState(0)

	runSearch := func(text string) {
		query.Set(text)
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			results.Set(nil)
			status.Set("")
			loading.Set(false)
			return
		}
		loading.Set(true)
		status.Set("")
		ui.SafeGo("search fixture graph", func() {
			// A short, real asynchronous gap — long enough that a headless
			// browser reliably observes the loading state before the result,
			// the same ordering a network round trip would produce.
			time.Sleep(120 * time.Millisecond)
			needle := strings.ToLower(trimmed)
			var matches []graph.Node
			for _, node := range nodes {
				if strings.Contains(strings.ToLower(node.DisplayName()), needle) {
					matches = append(matches, node)
				}
			}
			ui.PostAsync(func() {
				if query.Get() != text {
					return
				}
				loading.Set(false)
				results.Set(matches)
			})
		})
	}

	authoritative := graphcanvas.AuthoritativeProps{
		Revision: fixtureRevision, Layout: fixtureLayout, TaskState: domain.TaskStateRunning,
		SelectedNodeID: selected.Get(), CurrentNodeID: nodes[0].ID(), VisualMode: mode,
		FocusNodeID: focusNodeID.Get(),
		OnSelectNode: func(nodeID domain.NodeID) {
			selected.Set(nodeID)
			selectCount.Update(func(current int) int { return current + 1 })
		},
		SearchQuery: query.Get(), SearchResults: results.Get(),
		SearchLoading: loading.Get(), SearchStatus: status.Get(),
		OnSearchQueryChange: runSearch,
		OnSearchResultSelect: func(nodeID domain.NodeID) {
			focusNodeID.Set(nodeID)
		},
	}
	return html.Main(html.Props{
		Data: map[string]string{
			"testid": "graph-search-fixture", "select-count": itoa(selectCount.Get()),
		},
	}, ui.CreateElement(graphcanvas.Renderer, graphcanvas.Props{Authoritative: &authoritative}))
}

func mustFixtureGraph() (graph.Revision, graphlayout.Layout) {
	first := mustID(domain.ParseNodeID, "nod", 1)
	second := mustID(domain.ParseNodeID, "nod", 2)
	third := mustID(domain.ParseNodeID, "nod", 3)
	nodeOne, err := graph.NewNode(first, graph.NodeClassRequirement, graph.NodeStatusActive, "Authenticate user session", graph.ContractSummary{}, graph.SourceLinks{})
	if err != nil {
		panic(err)
	}
	nodeTwo, err := graph.NewNode(second, graph.NodeClassArtifactResult, graph.NodeStatusPending, "internal/session/token.go", graph.ContractSummary{}, graph.SourceLinks{})
	if err != nil {
		panic(err)
	}
	nodeThree, err := graph.NewNode(third, graph.NodeClassEffect, graph.NodeStatusPassed, "Persist session token", graph.ContractSummary{}, graph.SourceLinks{})
	if err != nil {
		panic(err)
	}
	edgeOne, err := graph.NewEdge(mustID(domain.ParseEdgeID, "edg", 1), graph.EdgeClassControl, first, second, graph.SourceLinks{})
	if err != nil {
		panic(err)
	}
	edgeTwo, err := graph.NewEdge(mustID(domain.ParseEdgeID, "edg", 2), graph.EdgeClassControl, second, third, graph.SourceLinks{})
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
	revision, err := graph.NewRevision(metadata, []graph.Node{nodeOne, nodeTwo, nodeThree}, []graph.Edge{edgeOne, edgeTwo})
	if err != nil {
		panic(err)
	}
	layout, err := graphlayout.Compute(graphlayout.Input{
		Nodes: []graphlayout.Node{
			{ID: first, Class: nodeOne.Class()}, {ID: second, Class: nodeTwo.Class()}, {ID: third, Class: nodeThree.Class()},
		},
		Edges: []graphlayout.Edge{
			{ID: edgeOne.ID(), FromNode: first, ToNode: second},
			{ID: edgeTwo.ID(), FromNode: second, ToNode: third},
		},
	})
	if err != nil {
		panic(err)
	}
	return revision, layout
}

func mustID[T any](parse func(string) (T, error), prefix string, ordinal int) T {
	value, err := parse(sprintfID(prefix, ordinal))
	if err != nil {
		panic(err)
	}
	return value
}

func sprintfID(prefix string, ordinal int) string {
	digits := itoa(ordinal)
	for len(digits) < 12 {
		digits = "0" + digits
	}
	return prefix + "_01890f3c-4a00-7abc-8def-" + digits
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
