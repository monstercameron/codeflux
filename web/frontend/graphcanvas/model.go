// Package graphcanvas renders bounded task-graph slices with a typed Canvas 2D
// surface and an accessible DOM companion.
package graphcanvas

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"codeflux.dev/codeflux/web/frontend/state"
)

var ErrInvalidGraph = errors.New("invalid graph canvas model")

type EdgeKind string

const (
	EdgeData           EdgeKind = "data"
	EdgeControl        EdgeKind = "control"
	EdgeEvidence       EdgeKind = "evidence"
	EdgeRetry          EdgeKind = "retry"
	EdgeReconciliation EdgeKind = "reconciliation"
	EdgeCompensation   EdgeKind = "compensation"
)

func (k EdgeKind) Valid() bool {
	switch k {
	case EdgeData, EdgeControl, EdgeEvidence, EdgeRetry, EdgeReconciliation, EdgeCompensation:
		return true
	default:
		return false
	}
}

type Edge struct {
	ID     string
	FromID string
	ToID   string
	Kind   EdgeKind
}

type Point struct{ X, Y float64 }

type Rect struct{ X, Y, Width, Height float64 }

func (r Rect) Center() Point { return Point{X: r.X + r.Width/2, Y: r.Y + r.Height/2} }

func (r Rect) Contains(point Point) bool {
	return point.X >= r.X && point.X <= r.X+r.Width &&
		point.Y >= r.Y && point.Y <= r.Y+r.Height
}

type NodeSemantic string

const (
	NodeOperation NodeSemantic = "operation"
	NodeExternal  NodeSemantic = "external"
	NodePlan      NodeSemantic = "plan"
	NodeEvidence  NodeSemantic = "evidence"
	NodeMemory    NodeSemantic = "memory"
)

type NodePlacement struct {
	Node     state.GraphNodeView
	Bounds   Rect
	Layer    int
	Order    int
	Semantic NodeSemantic
}

type EdgePlacement struct {
	Edge                  Edge
	Start, Control1       Point
	Control2, Destination Point
}

type Layout struct {
	Nodes  []NodePlacement
	Edges  []EdgePlacement
	Bounds Rect
	Key    string
}

func normalizeGraph(nodes []state.GraphNodeView, edges []Edge) ([]state.GraphNodeView, []Edge, error) {
	normalizedNodes := append([]state.GraphNodeView(nil), nodes...)
	sort.SliceStable(normalizedNodes, func(i, j int) bool {
		if normalizedNodes[i].ID == normalizedNodes[j].ID {
			return normalizedNodes[i].Title < normalizedNodes[j].Title
		}
		return normalizedNodes[i].ID < normalizedNodes[j].ID
	})
	known := make(map[string]struct{}, len(normalizedNodes))
	for index := range normalizedNodes {
		normalizedNodes[index].ID = strings.TrimSpace(normalizedNodes[index].ID)
		normalizedNodes[index].Title = strings.TrimSpace(normalizedNodes[index].Title)
		if normalizedNodes[index].ID == "" || normalizedNodes[index].Title == "" {
			return nil, nil, fmt.Errorf("%w: node identity and title are required", ErrInvalidGraph)
		}
		if _, duplicate := known[normalizedNodes[index].ID]; duplicate {
			return nil, nil, fmt.Errorf("%w: duplicate node %q", ErrInvalidGraph, normalizedNodes[index].ID)
		}
		known[normalizedNodes[index].ID] = struct{}{}
	}
	normalizedEdges := append([]Edge(nil), edges...)
	for index := range normalizedEdges {
		normalizedEdges[index].ID = strings.TrimSpace(normalizedEdges[index].ID)
		normalizedEdges[index].FromID = strings.TrimSpace(normalizedEdges[index].FromID)
		normalizedEdges[index].ToID = strings.TrimSpace(normalizedEdges[index].ToID)
		if normalizedEdges[index].Kind == "" {
			normalizedEdges[index].Kind = EdgeData
		}
		if !normalizedEdges[index].Kind.Valid() {
			return nil, nil, fmt.Errorf("%w: unsupported edge kind %q", ErrInvalidGraph, normalizedEdges[index].Kind)
		}
		if _, ok := known[normalizedEdges[index].FromID]; !ok {
			return nil, nil, fmt.Errorf("%w: edge source %q is unknown", ErrInvalidGraph, normalizedEdges[index].FromID)
		}
		if _, ok := known[normalizedEdges[index].ToID]; !ok {
			return nil, nil, fmt.Errorf("%w: edge destination %q is unknown", ErrInvalidGraph, normalizedEdges[index].ToID)
		}
		if normalizedEdges[index].ID == "" {
			normalizedEdges[index].ID = normalizedEdges[index].FromID + ":" + string(normalizedEdges[index].Kind) + ":" + normalizedEdges[index].ToID
		}
	}
	sort.SliceStable(normalizedEdges, func(i, j int) bool {
		if normalizedEdges[i].ID == normalizedEdges[j].ID {
			if normalizedEdges[i].FromID == normalizedEdges[j].FromID {
				return normalizedEdges[i].ToID < normalizedEdges[j].ToID
			}
			return normalizedEdges[i].FromID < normalizedEdges[j].FromID
		}
		return normalizedEdges[i].ID < normalizedEdges[j].ID
	})
	for index := 1; index < len(normalizedEdges); index++ {
		if normalizedEdges[index-1].ID == normalizedEdges[index].ID {
			return nil, nil, fmt.Errorf("%w: duplicate edge %q", ErrInvalidGraph, normalizedEdges[index].ID)
		}
	}
	return normalizedNodes, normalizedEdges, nil
}

func classifyNode(node state.GraphNodeView) NodeSemantic {
	label := strings.ToLower(node.Title)
	switch {
	case strings.Contains(label, "evidence"), strings.Contains(label, "proof"), strings.Contains(label, "validation"):
		return NodeEvidence
	case strings.Contains(label, "plan"), strings.Contains(label, "strategy"):
		return NodePlan
	case strings.Contains(label, "memory"), strings.Contains(label, "history"):
		return NodeMemory
	case strings.Contains(label, "test"), strings.Contains(label, "tool"), strings.Contains(label, "adapter"):
		return NodeExternal
	default:
		return NodeOperation
	}
}

func edgeLabel(edge Edge, nodes map[string]state.GraphNodeView) string {
	return nodes[edge.FromID].Title + " — " + string(edge.Kind) + " to " + nodes[edge.ToID].Title
}
