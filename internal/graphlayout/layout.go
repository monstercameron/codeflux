package graphlayout

import (
	"sort"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
)

type componentState struct {
	key      domain.NodeID
	members  []domain.NodeID
	rank     int
	floor    int
	outgoing []int
}

// Compute returns stable left-to-right placement hints. Strongly connected
// components are collapsed for ranking, then their members are placed in
// stable-identity order within the shared rank.
func Compute(input Input) (Layout, error) {
	if err := validateInput(input); err != nil {
		return Layout{}, err
	}
	if len(input.Nodes) == 0 {
		return Layout{
			AlgorithmVersion: AlgorithmVersion,
			Bounds:           Rect{Width: 2 * DefaultPadding, Height: 2 * DefaultPadding},
		}, nil
	}
	nodes := append([]Node(nil), input.Nodes...)
	edges := append([]Edge(nil), input.Edges...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID.String() < nodes[j].ID.String() })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID.String() < edges[j].ID.String() })

	components, componentOf := collapseComponents(nodes, edges)
	rankComponents(components, componentOf, edges)
	canonical := canonicalPlacements(nodes, components, componentOf)
	placed := reuseAndPlace(canonical, edges, input.Previous)
	assignOrders(placed)

	result := Layout{
		AlgorithmVersion: AlgorithmVersion,
		Nodes:            make([]Placement, 0, len(placed)),
		Components:       make([]Component, 0, len(components)),
	}
	for _, placement := range placed {
		result.Nodes = append(result.Nodes, placement)
	}
	sort.Slice(result.Nodes, func(i, j int) bool {
		return result.Nodes[i].NodeID.String() < result.Nodes[j].NodeID.String()
	})
	for _, component := range components {
		result.Components = append(result.Components, Component{
			Key: component.key, Members: append([]domain.NodeID(nil), component.members...), Rank: component.rank,
		})
	}
	sort.Slice(result.Components, func(i, j int) bool {
		return result.Components[i].Key.String() < result.Components[j].Key.String()
	})
	result.Bounds = layoutBounds(result.Nodes)
	return result, nil
}

func collapseComponents(nodes []Node, edges []Edge) ([]componentState, map[domain.NodeID]int) {
	adjacency := make(map[domain.NodeID][]domain.NodeID, len(nodes))
	classes := make(map[domain.NodeID]graph.NodeClass, len(nodes))
	for _, node := range nodes {
		adjacency[node.ID] = nil
		classes[node.ID] = node.Class
	}
	for _, edge := range edges {
		adjacency[edge.FromNode] = append(adjacency[edge.FromNode], edge.ToNode)
	}
	for id := range adjacency {
		sort.Slice(adjacency[id], func(i, j int) bool {
			return adjacency[id][i].String() < adjacency[id][j].String()
		})
	}

	index := 0
	indices := make(map[domain.NodeID]int, len(nodes))
	lowlink := make(map[domain.NodeID]int, len(nodes))
	onStack := make(map[domain.NodeID]bool, len(nodes))
	stack := make([]domain.NodeID, 0, len(nodes))
	memberships := make([][]domain.NodeID, 0, len(nodes))
	var visit func(domain.NodeID)
	visit = func(id domain.NodeID) {
		index++
		indices[id], lowlink[id] = index, index
		stack = append(stack, id)
		onStack[id] = true
		for _, neighbor := range adjacency[id] {
			if indices[neighbor] == 0 {
				visit(neighbor)
				lowlink[id] = min(lowlink[id], lowlink[neighbor])
			} else if onStack[neighbor] {
				lowlink[id] = min(lowlink[id], indices[neighbor])
			}
		}
		if lowlink[id] != indices[id] {
			return
		}
		component := make([]domain.NodeID, 0, 1)
		for {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[last] = false
			component = append(component, last)
			if last == id {
				break
			}
		}
		sort.Slice(component, func(i, j int) bool { return component[i].String() < component[j].String() })
		memberships = append(memberships, component)
	}
	for _, node := range nodes {
		if indices[node.ID] == 0 {
			visit(node.ID)
		}
	}
	sort.Slice(memberships, func(i, j int) bool {
		return memberships[i][0].String() < memberships[j][0].String()
	})
	components := make([]componentState, len(memberships))
	componentOf := make(map[domain.NodeID]int, len(nodes))
	for componentIndex, members := range memberships {
		floor := 0
		for _, member := range members {
			floor = max(floor, classRankFloor(classes[member]))
			componentOf[member] = componentIndex
		}
		components[componentIndex] = componentState{key: members[0], members: members, floor: floor, rank: floor}
	}
	return components, componentOf
}

func rankComponents(components []componentState, componentOf map[domain.NodeID]int, edges []Edge) {
	outgoingSets := make([]map[int]struct{}, len(components))
	indegree := make([]int, len(components))
	for index := range components {
		outgoingSets[index] = map[int]struct{}{}
	}
	for _, edge := range edges {
		from, to := componentOf[edge.FromNode], componentOf[edge.ToNode]
		if from == to {
			continue
		}
		if _, duplicate := outgoingSets[from][to]; duplicate {
			continue
		}
		outgoingSets[from][to] = struct{}{}
		indegree[to]++
	}
	for index := range components {
		for destination := range outgoingSets[index] {
			components[index].outgoing = append(components[index].outgoing, destination)
		}
		sort.Slice(components[index].outgoing, func(i, j int) bool {
			return components[components[index].outgoing[i]].key.String() < components[components[index].outgoing[j]].key.String()
		})
	}
	queue := make([]int, 0, len(components))
	for index := range components {
		if indegree[index] == 0 {
			queue = append(queue, index)
		}
	}
	sortComponentQueue(queue, components)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, destination := range components[current].outgoing {
			components[destination].rank = max(
				components[destination].rank,
				components[current].rank+1,
				components[destination].floor,
			)
			indegree[destination]--
			if indegree[destination] == 0 {
				queue = append(queue, destination)
				sortComponentQueue(queue, components)
			}
		}
	}
}

func sortComponentQueue(queue []int, components []componentState) {
	sort.Slice(queue, func(i, j int) bool {
		return components[queue[i]].key.String() < components[queue[j]].key.String()
	})
}

func canonicalPlacements(
	nodes []Node,
	components []componentState,
	componentOf map[domain.NodeID]int,
) map[domain.NodeID]Placement {
	ordered := append([]Node(nil), nodes...)
	sort.Slice(ordered, func(i, j int) bool {
		left, right := components[componentOf[ordered[i].ID]], components[componentOf[ordered[j].ID]]
		if left.rank != right.rank {
			return left.rank < right.rank
		}
		if left.key != right.key {
			return left.key.String() < right.key.String()
		}
		return ordered[i].ID.String() < ordered[j].ID.String()
	})
	orders := make(map[int]int)
	result := make(map[domain.NodeID]Placement, len(nodes))
	for _, node := range ordered {
		component := components[componentOf[node.ID]]
		order := orders[component.rank]
		orders[component.rank]++
		result[node.ID] = Placement{
			NodeID: node.ID, ComponentKey: component.key, Rank: component.rank, Order: order,
			Bounds: Rect{
				X:     DefaultPadding + int64(component.rank)*(DefaultNodeWidth+DefaultRankGap),
				Y:     DefaultPadding + int64(order)*(DefaultNodeHeight+DefaultSiblingGap),
				Width: DefaultNodeWidth, Height: DefaultNodeHeight,
			},
		}
	}
	return result
}

func reuseAndPlace(
	canonical map[domain.NodeID]Placement,
	edges []Edge,
	previous *PriorLayout,
) map[domain.NodeID]Placement {
	result := make(map[domain.NodeID]Placement, len(canonical))
	occupied := make(map[int][]Rect)
	reused := make(map[domain.NodeID]bool, len(canonical))
	if previous != nil && previous.AlgorithmVersion == AlgorithmVersion {
		prior := append([]Placement(nil), previous.Nodes...)
		sort.Slice(prior, func(i, j int) bool { return prior[i].NodeID.String() < prior[j].NodeID.String() })
		for _, hint := range prior {
			placement, exists := canonical[hint.NodeID]
			if !exists || hint.Rank != placement.Rank || hint.Bounds.Width != DefaultNodeWidth ||
				hint.Bounds.Height != DefaultNodeHeight || hint.Bounds.X != placement.Bounds.X || hint.Bounds.Y < 0 ||
				overlapsAny(hint.Bounds, occupied[hint.Rank]) {
				continue
			}
			placement.Bounds = hint.Bounds
			result[hint.NodeID] = placement
			occupied[hint.Rank] = append(occupied[hint.Rank], hint.Bounds)
			reused[hint.NodeID] = true
		}
	}
	neighbors := stableNeighbors(edges)
	ids := make([]domain.NodeID, 0, len(canonical))
	for id := range canonical {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for _, id := range ids {
		if _, exists := result[id]; exists {
			continue
		}
		placement := canonical[id]
		candidateY, nearStableNeighbor := stableNeighborY(neighbors[id], result, reused)
		if nearStableNeighbor {
			placement.Bounds.Y = max(DefaultPadding, candidateY)
		}
		placement.Bounds.Y = nearestFreeY(placement.Bounds, occupied[placement.Rank])
		result[id] = placement
		occupied[placement.Rank] = append(occupied[placement.Rank], placement.Bounds)
	}
	return result
}

func stableNeighbors(edges []Edge) map[domain.NodeID][]domain.NodeID {
	neighbors := make(map[domain.NodeID][]domain.NodeID)
	for _, edge := range edges {
		neighbors[edge.FromNode] = append(neighbors[edge.FromNode], edge.ToNode)
		neighbors[edge.ToNode] = append(neighbors[edge.ToNode], edge.FromNode)
	}
	for id := range neighbors {
		sort.Slice(neighbors[id], func(i, j int) bool {
			return neighbors[id][i].String() < neighbors[id][j].String()
		})
	}
	return neighbors
}

func stableNeighborY(
	neighbors []domain.NodeID,
	placed map[domain.NodeID]Placement,
	reused map[domain.NodeID]bool,
) (int64, bool) {
	var total int64
	count := int64(0)
	for _, neighbor := range neighbors {
		placement, exists := placed[neighbor]
		if !exists || !reused[neighbor] {
			continue
		}
		total += placement.Bounds.Y
		count++
	}
	if count == 0 {
		return 0, false
	}
	return total / count, true
}

func nearestFreeY(candidate Rect, occupied []Rect) int64 {
	if !overlapsAny(candidate, occupied) {
		return candidate.Y
	}
	step := DefaultNodeHeight + DefaultSiblingGap
	for distance := int64(1); distance <= int64(len(occupied)+1); distance++ {
		down := candidate
		down.Y = candidate.Y + distance*step
		if !overlapsAny(down, occupied) {
			return down.Y
		}
		upY := candidate.Y - distance*step
		if upY >= DefaultPadding {
			up := candidate
			up.Y = upY
			if !overlapsAny(up, occupied) {
				return up.Y
			}
		}
	}
	return candidate.Y + int64(len(occupied)+1)*step
}

func overlapsAny(candidate Rect, occupied []Rect) bool {
	for _, existing := range occupied {
		if candidate.X < existing.right()+DefaultRankGap && candidate.right()+DefaultRankGap > existing.X &&
			candidate.Y < existing.bottom()+DefaultSiblingGap && candidate.bottom()+DefaultSiblingGap > existing.Y {
			return true
		}
	}
	return false
}

func assignOrders(placements map[domain.NodeID]Placement) {
	byRank := make(map[int][]domain.NodeID)
	for id, placement := range placements {
		byRank[placement.Rank] = append(byRank[placement.Rank], id)
	}
	for _, ids := range byRank {
		sort.Slice(ids, func(i, j int) bool {
			left, right := placements[ids[i]], placements[ids[j]]
			if left.Bounds.Y == right.Bounds.Y {
				return ids[i].String() < ids[j].String()
			}
			return left.Bounds.Y < right.Bounds.Y
		})
		for order, id := range ids {
			placement := placements[id]
			placement.Order = order
			placements[id] = placement
		}
	}
}

func layoutBounds(nodes []Placement) Rect {
	if len(nodes) == 0 {
		return Rect{Width: 2 * DefaultPadding, Height: 2 * DefaultPadding}
	}
	minX, minY := nodes[0].Bounds.X, nodes[0].Bounds.Y
	maxX, maxY := nodes[0].Bounds.right(), nodes[0].Bounds.bottom()
	for _, node := range nodes[1:] {
		minX = min(minX, node.Bounds.X)
		minY = min(minY, node.Bounds.Y)
		maxX = max(maxX, node.Bounds.right())
		maxY = max(maxY, node.Bounds.bottom())
	}
	return Rect{
		X:      max(int64(0), minX-DefaultPadding),
		Y:      max(int64(0), minY-DefaultPadding),
		Width:  maxX - max(int64(0), minX-DefaultPadding) + DefaultPadding,
		Height: maxY - max(int64(0), minY-DefaultPadding) + DefaultPadding,
	}
}
