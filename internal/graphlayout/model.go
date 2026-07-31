// Package graphlayout computes deterministic server-side placement hints for
// bounded, immutable task-graph slices.
package graphlayout

import (
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
)

const (
	// AlgorithmVersion changes whenever persisted coordinates are no longer
	// compatible with the current ranking or placement rules.
	AlgorithmVersion = "ltr-layered-scc-v1"

	DefaultNodeWidth  int64 = 240
	DefaultNodeHeight int64 = 88
	DefaultRankGap    int64 = 112
	DefaultSiblingGap int64 = 40
	DefaultPadding    int64 = 64

	MaxLayoutNodes = 1_200
	MaxLayoutEdges = 4_800
)

var ErrInvalidLayoutInput = errors.New("invalid graph layout input")

func classRankFloor(class graph.NodeClass) int {
	switch class {
	case graph.NodeClassRequirement:
		return 0
	case graph.NodeClassPlanRegion:
		return 1
	case graph.NodeClassAtomOperation, graph.NodeClassBranchMatchMerge:
		return 2
	case graph.NodeClassEffect:
		return 3
	case graph.NodeClassObligation:
		return 4
	case graph.NodeClassArtifactResult:
		return 5
	default:
		return 0
	}
}

type Node struct {
	ID    domain.NodeID
	Class graph.NodeClass
}

type Edge struct {
	ID       domain.EdgeID
	FromNode domain.NodeID
	ToNode   domain.NodeID
}

type Point struct {
	X int64
	Y int64
}

type Rect struct {
	X      int64
	Y      int64
	Width  int64
	Height int64
}

func (rectangle Rect) right() int64  { return rectangle.X + rectangle.Width }
func (rectangle Rect) bottom() int64 { return rectangle.Y + rectangle.Height }

type Placement struct {
	NodeID       domain.NodeID
	ComponentKey domain.NodeID
	Rank         int
	Order        int
	Bounds       Rect
}

type Component struct {
	Key     domain.NodeID
	Members []domain.NodeID
	Rank    int
}

type Layout struct {
	AlgorithmVersion string
	Nodes            []Placement
	Components       []Component
	Bounds           Rect
}

type PriorLayout struct {
	AlgorithmVersion string
	Nodes            []Placement
}

type Input struct {
	Nodes    []Node
	Edges    []Edge
	Previous *PriorLayout
}

func validateInput(input Input) error {
	if len(input.Nodes) > MaxLayoutNodes || len(input.Edges) > MaxLayoutEdges {
		return fmt.Errorf("%w: bounded layout supports at most %d nodes and %d edges", ErrInvalidLayoutInput, MaxLayoutNodes, MaxLayoutEdges)
	}
	knownNodes := make(map[domain.NodeID]struct{}, len(input.Nodes))
	for _, node := range input.Nodes {
		if node.ID.IsZero() || !node.Class.IsValid() {
			return fmt.Errorf("%w: node identity and class are required", ErrInvalidLayoutInput)
		}
		if _, duplicate := knownNodes[node.ID]; duplicate {
			return fmt.Errorf("%w: duplicate node %s", ErrInvalidLayoutInput, node.ID)
		}
		knownNodes[node.ID] = struct{}{}
	}
	knownEdges := make(map[domain.EdgeID]struct{}, len(input.Edges))
	for _, edge := range input.Edges {
		if edge.ID.IsZero() || edge.FromNode.IsZero() || edge.ToNode.IsZero() {
			return fmt.Errorf("%w: edge identity and endpoints are required", ErrInvalidLayoutInput)
		}
		if _, duplicate := knownEdges[edge.ID]; duplicate {
			return fmt.Errorf("%w: duplicate edge %s", ErrInvalidLayoutInput, edge.ID)
		}
		if _, exists := knownNodes[edge.FromNode]; !exists {
			return fmt.Errorf("%w: edge %s has an unknown source", ErrInvalidLayoutInput, edge.ID)
		}
		if _, exists := knownNodes[edge.ToNode]; !exists {
			return fmt.Errorf("%w: edge %s has an unknown destination", ErrInvalidLayoutInput, edge.ID)
		}
		knownEdges[edge.ID] = struct{}{}
	}
	return nil
}
