package graphcanvas

import (
	"math"
	"sort"
	"strings"

	"codeflux.dev/codeflux/web/frontend/state"
)

const (
	nodeWidth  = 152.0
	nodeHeight = 58.0
	layerGap   = 54.0
	rowGap     = 28.0
	worldPad   = 36.0
)

func BuildLayout(nodes []state.GraphNodeView, edges []Edge) (Layout, error) {
	nodes, edges, err := normalizeGraph(nodes, edges)
	if err != nil {
		return Layout{}, err
	}
	if len(nodes) == 0 {
		return Layout{Bounds: Rect{Width: worldPad * 2, Height: worldPad * 2}, Key: "empty"}, nil
	}
	layers := assignLayers(nodes, edges)
	groups := make(map[int][]state.GraphNodeView)
	maxLayer := 0
	for _, node := range nodes {
		layer := layers[node.ID]
		groups[layer] = append(groups[layer], node)
		maxLayer = max(maxLayer, layer)
	}
	maxColumns := 1
	for layer := 0; layer <= maxLayer; layer++ {
		sort.SliceStable(groups[layer], func(i, j int) bool { return groups[layer][i].ID < groups[layer][j].ID })
		maxColumns = max(maxColumns, len(groups[layer]))
	}
	worldWidth := worldPad*2 + float64(maxColumns)*nodeWidth + float64(maxColumns-1)*rowGap
	placements := make([]NodePlacement, 0, len(nodes))
	byID := make(map[string]NodePlacement, len(nodes))
	for layer := 0; layer <= maxLayer; layer++ {
		groupWidth := float64(len(groups[layer]))*nodeWidth + float64(max(len(groups[layer])-1, 0))*rowGap
		x := (worldWidth - groupWidth) / 2
		for order, node := range groups[layer] {
			placement := NodePlacement{
				Node: node, Layer: layer, Order: order, Semantic: classifyNode(node),
				Bounds: Rect{
					X: x, Y: worldPad + float64(layer)*(nodeHeight+layerGap),
					Width: nodeWidth, Height: nodeHeight,
				},
			}
			placements = append(placements, placement)
			byID[node.ID] = placement
			x += nodeWidth + rowGap
		}
	}
	placedEdges := make([]EdgePlacement, 0, len(edges))
	for _, edge := range edges {
		from, to := byID[edge.FromID], byID[edge.ToID]
		start := Point{X: from.Bounds.X + from.Bounds.Width/2, Y: from.Bounds.Y + from.Bounds.Height}
		destination := Point{X: to.Bounds.X + to.Bounds.Width/2, Y: to.Bounds.Y}
		bend := math.Max(28, math.Abs(destination.Y-start.Y)*0.46)
		direction := 1.0
		if destination.Y < start.Y {
			direction = -1
		}
		placedEdges = append(placedEdges, EdgePlacement{
			Edge: edge, Start: start,
			Control1:    Point{X: start.X, Y: start.Y + direction*bend},
			Control2:    Point{X: destination.X, Y: destination.Y - direction*bend},
			Destination: destination,
		})
	}
	sort.SliceStable(placements, func(i, j int) bool { return placements[i].Node.ID < placements[j].Node.ID })
	worldHeight := worldPad*2 + float64(maxLayer+1)*nodeHeight + float64(maxLayer)*layerGap
	return Layout{
		Nodes: placements, Edges: placedEdges,
		Bounds: Rect{Width: worldWidth, Height: worldHeight},
		Key:    layoutKey(nodes, edges),
	}, nil
}

func assignLayers(nodes []state.GraphNodeView, edges []Edge) map[string]int {
	if len(edges) == 0 {
		columns := max(1, int(math.Ceil(math.Sqrt(float64(len(nodes))))))
		layers := make(map[string]int, len(nodes))
		for index, node := range nodes {
			layers[node.ID] = index % columns
		}
		return layers
	}
	indegree := make(map[string]int, len(nodes))
	outgoing := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		indegree[node.ID] = 0
	}
	for _, edge := range edges {
		if edge.FromID == edge.ToID {
			continue
		}
		indegree[edge.ToID]++
		outgoing[edge.FromID] = append(outgoing[edge.FromID], edge.ToID)
	}
	for id := range outgoing {
		sort.Strings(outgoing[id])
	}
	queue := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if indegree[node.ID] == 0 {
			queue = append(queue, node.ID)
		}
	}
	sort.Strings(queue)
	layers := make(map[string]int, len(nodes))
	processed := make(map[string]bool, len(nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		processed[id] = true
		for _, destination := range outgoing[id] {
			layers[destination] = max(layers[destination], layers[id]+1)
			indegree[destination]--
			if indegree[destination] == 0 {
				queue = append(queue, destination)
				sort.Strings(queue)
			}
		}
	}
	maxLayer := 0
	for _, layer := range layers {
		maxLayer = max(maxLayer, layer)
	}
	cycleLayer := maxLayer + 1
	for _, node := range nodes {
		if !processed[node.ID] {
			layers[node.ID] = cycleLayer
		}
	}
	return layers
}

func layoutKey(nodes []state.GraphNodeView, edges []Edge) string {
	var value strings.Builder
	for _, node := range nodes {
		value.WriteString(node.ID)
		value.WriteByte(':')
		value.WriteString(node.Title)
		value.WriteByte(':')
		value.WriteString(node.Status)
		value.WriteByte(';')
	}
	for _, edge := range edges {
		value.WriteString(edge.ID)
		value.WriteByte(':')
		value.WriteString(edge.FromID)
		value.WriteByte('>')
		value.WriteString(edge.ToID)
		value.WriteByte(':')
		value.WriteString(string(edge.Kind))
		value.WriteByte(';')
	}
	return value.String()
}
