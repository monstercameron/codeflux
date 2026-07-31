package graphcanvas

import (
	"fmt"
	"reflect"
	"testing"

	"codeflux.dev/codeflux/web/frontend/state"
)

func TestStableLayoutHandlesOneHundredTwentyNodesDeterministically(t *testing.T) {
	nodes := make([]state.GraphNodeView, 120)
	edges := make([]Edge, 0, 119)
	for index := range nodes {
		nodes[index] = state.GraphNodeView{ID: fmt.Sprintf("node-%03d", index), Title: fmt.Sprintf("Operation %03d", index), Status: "pending"}
		if index > 0 {
			edges = append(edges, Edge{ID: fmt.Sprintf("edge-%03d", index), FromID: nodes[index-1].ID, ToID: nodes[index].ID, Kind: EdgeControl})
		}
	}
	first, err := BuildStableLayout(nodes, edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildStableLayout(nodes, edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first.Nodes) != 120 || len(first.Edges) != 119 {
		t.Fatalf("large layout is unstable or incomplete: nodes=%d edges=%d", len(first.Nodes), len(first.Edges))
	}
	for left := range first.Nodes {
		for right := left + 1; right < len(first.Nodes); right++ {
			if strictOverlap(first.Nodes[left].Bounds, first.Nodes[right].Bounds) {
				t.Fatalf("nodes %s and %s overlap", first.Nodes[left].Node.ID, first.Nodes[right].Node.ID)
			}
		}
	}
}

func TestStableLayoutPreservesPriorNodesAcrossRevision(t *testing.T) {
	nodes := []state.GraphNodeView{
		{ID: "requirement", Title: "Requirement", Status: "passed"},
		{ID: "operation", Title: "Operation", Status: "active"},
		{ID: "artifact", Title: "Artifact", Status: "pending"},
	}
	edges := []Edge{
		{ID: "one", FromID: "requirement", ToID: "operation", Kind: EdgeControl},
		{ID: "two", FromID: "operation", ToID: "artifact", Kind: EdgeData},
	}
	before, err := BuildStableLayout(nodes, edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	revisedNodes := append(append([]state.GraphNodeView(nil), nodes...), state.GraphNodeView{ID: "validation", Title: "Validation evidence", Status: "pending"})
	revisedEdges := append(append([]Edge(nil), edges...), Edge{ID: "three", FromID: "operation", ToID: "validation", Kind: EdgeEvidence})
	after, err := BuildStableLayout(revisedNodes, revisedEdges, &before)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"requirement", "operation", "artifact"} {
		oldBounds, oldOK := placementBounds(before, id)
		newBounds, newOK := placementBounds(after, id)
		if !oldOK || !newOK || oldBounds != newBounds {
			t.Errorf("stable node %s moved from %#v to %#v", id, oldBounds, newBounds)
		}
	}
	operation, _ := placementBounds(after, "operation")
	validation, ok := placementBounds(after, "validation")
	if !ok || validation.Y > operation.Y+nodeHeight+layerGap+1 {
		t.Fatalf("new neighbor placement operation=%#v validation=%#v", operation, validation)
	}
}

func TestStableLayoutCollapsesCycleIntoOneLayer(t *testing.T) {
	nodes := []state.GraphNodeView{
		{ID: "a", Title: "A", Status: "active"},
		{ID: "b", Title: "B", Status: "active"},
		{ID: "root", Title: "Plan root", Status: "passed"},
	}
	edges := []Edge{
		{ID: "root-a", FromID: "root", ToID: "a", Kind: EdgeControl},
		{ID: "a-b", FromID: "a", ToID: "b", Kind: EdgeControl},
		{ID: "b-a", FromID: "b", ToID: "a", Kind: EdgeRetry},
	}
	layout, err := BuildStableLayout(nodes, edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := placement(layout, "a")
	b := placement(layout, "b")
	if a.Layer != b.Layer {
		t.Fatalf("cycle layers differ: a=%d b=%d", a.Layer, b.Layer)
	}
}

func placement(layout Layout, id string) NodePlacement {
	for _, node := range layout.Nodes {
		if node.Node.ID == id {
			return node
		}
	}
	return NodePlacement{}
}

func placementBounds(layout Layout, id string) (Rect, bool) {
	node := placement(layout, id)
	return node.Bounds, node.Node.ID != ""
}

func strictOverlap(left, right Rect) bool {
	return left.X < right.X+right.Width && left.X+left.Width > right.X && left.Y < right.Y+right.Height && left.Y+left.Height > right.Y
}
