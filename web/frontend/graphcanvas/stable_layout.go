package graphcanvas

import (
	"math"
	"sort"

	"codeflux.dev/codeflux/web/frontend/state"
)

const LayoutAlgorithmVersion = "layered-top-down-stable-v2"

// BuildStableLayout reuses compatible prior coordinates and places new nodes
// near already placed stable neighbors. The authoritative graph remains the
// node and edge input; the previous layout is only an ephemeral hint.
func BuildStableLayout(nodes []state.GraphNodeView, edges []Edge, previous *Layout) (Layout, error) {
	next, err := BuildLayout(nodes, edges)
	if err != nil || previous == nil || len(previous.Nodes) == 0 {
		return next, err
	}
	prior := make(map[string]Rect, len(previous.Nodes))
	for _, placement := range previous.Nodes {
		prior[placement.Node.ID] = placement.Bounds
	}
	byID := make(map[string]NodePlacement, len(next.Nodes))
	occupied := make([]Rect, 0, len(next.Nodes))
	// Stable nodes claim their old positions before new nodes are considered.
	for _, placement := range next.Nodes {
		if bounds, ok := prior[placement.Node.ID]; ok {
			placement.Bounds = bounds
			byID[placement.Node.ID] = placement
			occupied = append(occupied, bounds)
		}
	}
	neighbors := stableNeighbors(edges)
	for _, placement := range next.Nodes {
		if _, exists := byID[placement.Node.ID]; exists {
			continue
		}
		candidate := placement.Bounds
		if x, ok := stableNeighborX(neighbors[placement.Node.ID], byID); ok {
			candidate.X = x
		}
		for overlapsStable(candidate, occupied) {
			candidate.X += nodeWidth + rowGap
		}
		placement.Bounds = candidate
		byID[placement.Node.ID] = placement
		occupied = append(occupied, candidate)
	}
	next.Nodes = next.Nodes[:0]
	for _, placement := range byID {
		next.Nodes = append(next.Nodes, placement)
	}
	sort.Slice(next.Nodes, func(i, j int) bool { return next.Nodes[i].Node.ID < next.Nodes[j].Node.ID })
	next.Edges = stableEdgeGeometry(edges, byID)
	next.Bounds = stableBounds(next.Nodes, next.Edges)
	return next, nil
}

func stableNeighbors(edges []Edge) map[string][]string {
	result := map[string][]string{}
	for _, edge := range edges {
		result[edge.FromID] = append(result[edge.FromID], edge.ToID)
		result[edge.ToID] = append(result[edge.ToID], edge.FromID)
	}
	for id := range result {
		sort.Strings(result[id])
	}
	return result
}

func stableNeighborX(neighbors []string, placed map[string]NodePlacement) (float64, bool) {
	total := 0.0
	count := 0
	for _, id := range neighbors {
		if placement, ok := placed[id]; ok {
			total += placement.Bounds.X
			count++
		}
	}
	if count == 0 {
		return 0, false
	}
	return total / float64(count), true
}

func overlapsStable(candidate Rect, existing []Rect) bool {
	for _, bounds := range existing {
		if candidate.X < bounds.X+bounds.Width+rowGap && candidate.X+candidate.Width+rowGap > bounds.X &&
			candidate.Y < bounds.Y+bounds.Height+rowGap && candidate.Y+candidate.Height+rowGap > bounds.Y {
			return true
		}
	}
	return false
}

func stableEdgeGeometry(edges []Edge, nodes map[string]NodePlacement) []EdgePlacement {
	ordered := append([]Edge(nil), edges...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	result := make([]EdgePlacement, 0, len(ordered))
	for _, edge := range ordered {
		from, fromOK := nodes[edge.FromID]
		to, toOK := nodes[edge.ToID]
		if !fromOK || !toOK {
			continue
		}
		start := Point{X: from.Bounds.X + from.Bounds.Width/2, Y: from.Bounds.Y + from.Bounds.Height}
		destination := Point{X: to.Bounds.X + to.Bounds.Width/2, Y: to.Bounds.Y}
		bend := math.Max(28, math.Abs(destination.Y-start.Y)*0.46)
		direction := 1.0
		if destination.Y < start.Y {
			direction = -1
		}
		result = append(result, EdgePlacement{
			Edge: edge, Start: start,
			Control1:    Point{X: start.X, Y: start.Y + direction*bend},
			Control2:    Point{X: destination.X, Y: destination.Y - direction*bend},
			Destination: destination,
		})
	}
	return result
}

func stableBounds(nodes []NodePlacement, edges []EdgePlacement) Rect {
	if len(nodes) == 0 {
		return Rect{Width: worldPad * 2, Height: worldPad * 2}
	}
	minX, minY := nodes[0].Bounds.X, nodes[0].Bounds.Y
	maxX := nodes[0].Bounds.X + nodes[0].Bounds.Width
	maxY := nodes[0].Bounds.Y + nodes[0].Bounds.Height
	include := func(point Point) {
		minX, minY = math.Min(minX, point.X), math.Min(minY, point.Y)
		maxX, maxY = math.Max(maxX, point.X), math.Max(maxY, point.Y)
	}
	for _, node := range nodes[1:] {
		include(Point{X: node.Bounds.X, Y: node.Bounds.Y})
		include(Point{X: node.Bounds.X + node.Bounds.Width, Y: node.Bounds.Y + node.Bounds.Height})
	}
	for _, edge := range edges {
		include(edge.Start)
		include(edge.Control1)
		include(edge.Control2)
		include(edge.Destination)
	}
	return Rect{X: minX - worldPad, Y: minY - worldPad, Width: maxX - minX + 2*worldPad, Height: maxY - minY + 2*worldPad}
}
