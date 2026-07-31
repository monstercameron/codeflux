package graphlayout_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/graphlayout"
)

func TestComputeSnapshotsDeterministicLeftToRightCoordinates(t *testing.T) {
	fixture := linearFixture(t)
	first, err := graphlayout.Compute(fixture)
	if err != nil {
		t.Fatal(err)
	}
	shuffled := graphlayout.Input{
		Nodes: []graphlayout.Node{
			fixture.Nodes[4], fixture.Nodes[2], fixture.Nodes[0], fixture.Nodes[5], fixture.Nodes[1], fixture.Nodes[3],
		},
		Edges: []graphlayout.Edge{fixture.Edges[3], fixture.Edges[1], fixture.Edges[4], fixture.Edges[0], fixture.Edges[2]},
	}
	second, err := graphlayout.Compute(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("input order changed deterministic layout:\nfirst=%+v\nsecond=%+v", first, second)
	}

	want := []struct {
		id    domain.NodeID
		rank  int
		order int
		x     int64
		y     int64
	}{
		{fixture.Nodes[0].ID, 0, 0, 64, 64},
		{fixture.Nodes[1].ID, 1, 0, 416, 64},
		{fixture.Nodes[2].ID, 2, 0, 768, 64},
		{fixture.Nodes[3].ID, 3, 0, 1120, 64},
		{fixture.Nodes[4].ID, 5, 0, 1824, 64},
		{fixture.Nodes[5].ID, 5, 1, 1824, 192},
	}
	for _, expected := range want {
		placement := placement(t, first, expected.id)
		if placement.Rank != expected.rank || placement.Order != expected.order ||
			placement.Bounds.X != expected.x || placement.Bounds.Y != expected.y {
			t.Errorf("node %s placement=%+v want rank=%d order=%d x=%d y=%d", expected.id, placement, expected.rank, expected.order, expected.x, expected.y)
		}
	}
	if first.AlgorithmVersion != graphlayout.AlgorithmVersion {
		t.Fatalf("algorithm version=%q", first.AlgorithmVersion)
	}
	if first.Bounds != (graphlayout.Rect{Width: 2128, Height: 344}) {
		t.Fatalf("bounds=%+v", first.Bounds)
	}
}

func TestComputeRanksSemanticClassesBeforeEffectsAndArtifacts(t *testing.T) {
	nodes := []graphlayout.Node{
		{ID: nodeID(t, 5), Class: graph.NodeClassArtifactResult},
		{ID: nodeID(t, 3), Class: graph.NodeClassAtomOperation},
		{ID: nodeID(t, 1), Class: graph.NodeClassRequirement},
		{ID: nodeID(t, 4), Class: graph.NodeClassEffect},
		{ID: nodeID(t, 2), Class: graph.NodeClassPlanRegion},
	}
	layout, err := graphlayout.Compute(graphlayout.Input{Nodes: nodes})
	if err != nil {
		t.Fatal(err)
	}
	for index, wantRank := range []int{5, 2, 0, 3, 1} {
		if got := placement(t, layout, nodes[index].ID).Rank; got != wantRank {
			t.Errorf("class %s rank=%d want=%d", nodes[index].Class, got, wantRank)
		}
	}
}

func TestComputeCollapsesStronglyConnectedComponentsBeforeRanking(t *testing.T) {
	plan, left, right, artifact := nodeID(t, 1), nodeID(t, 2), nodeID(t, 3), nodeID(t, 4)
	layout, err := graphlayout.Compute(graphlayout.Input{
		Nodes: []graphlayout.Node{
			{ID: right, Class: graph.NodeClassAtomOperation},
			{ID: artifact, Class: graph.NodeClassArtifactResult},
			{ID: plan, Class: graph.NodeClassPlanRegion},
			{ID: left, Class: graph.NodeClassAtomOperation},
		},
		Edges: []graphlayout.Edge{
			{ID: edgeID(t, 1), FromNode: plan, ToNode: left},
			{ID: edgeID(t, 2), FromNode: left, ToNode: right},
			{ID: edgeID(t, 3), FromNode: right, ToNode: left},
			{ID: edgeID(t, 4), FromNode: right, ToNode: artifact},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	leftPlacement, rightPlacement := placement(t, layout, left), placement(t, layout, right)
	if leftPlacement.Rank != rightPlacement.Rank || leftPlacement.ComponentKey != rightPlacement.ComponentKey {
		t.Fatalf("cycle was not ranked as one component: left=%+v right=%+v", leftPlacement, rightPlacement)
	}
	if leftPlacement.Rank != 2 || placement(t, layout, artifact).Rank != 5 {
		t.Fatalf("collapsed ranks left=%d artifact=%d", leftPlacement.Rank, placement(t, layout, artifact).Rank)
	}
	component := componentFor(t, layout, leftPlacement.ComponentKey)
	if !reflect.DeepEqual(component.Members, []domain.NodeID{left, right}) {
		t.Fatalf("component members=%v", component.Members)
	}
}

func TestComputeUsesStableIdentitySiblingOrder(t *testing.T) {
	ids := []domain.NodeID{nodeID(t, 3), nodeID(t, 1), nodeID(t, 2)}
	layout, err := graphlayout.Compute(graphlayout.Input{Nodes: []graphlayout.Node{
		{ID: ids[0], Class: graph.NodeClassAtomOperation},
		{ID: ids[1], Class: graph.NodeClassAtomOperation},
		{ID: ids[2], Class: graph.NodeClassAtomOperation},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for index, id := range []domain.NodeID{ids[1], ids[2], ids[0]} {
		got := placement(t, layout, id)
		if got.Order != index || got.Bounds.Y != graphlayout.DefaultPadding+int64(index)*(graphlayout.DefaultNodeHeight+graphlayout.DefaultSiblingGap) {
			t.Errorf("node %s order=%d y=%d", id, got.Order, got.Bounds.Y)
		}
	}
}

func TestComputeReusesCoordinatesAndKeepsUnrelatedAdditionsStable(t *testing.T) {
	fixture := linearFixture(t)
	fixture.Nodes = fixture.Nodes[:5]
	fixture.Edges = fixture.Edges[:4]
	before, err := graphlayout.Compute(fixture)
	if err != nil {
		t.Fatal(err)
	}
	previous := prior(before)
	added := nodeID(t, 20)
	fixture.Nodes = append(fixture.Nodes, graphlayout.Node{ID: added, Class: graph.NodeClassArtifactResult})
	after, err := graphlayout.Compute(graphlayout.Input{Nodes: fixture.Nodes, Edges: fixture.Edges, Previous: &previous})
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range before.Nodes {
		if got := placement(t, after, old.NodeID).Bounds; got != old.Bounds {
			t.Errorf("unrelated addition moved %s from %+v to %+v", old.NodeID, old.Bounds, got)
		}
	}
	if addedPlacement := placement(t, after, added); addedPlacement.Bounds == (graphlayout.Rect{}) {
		t.Fatal("new unrelated node was not placed")
	}
}

func TestComputePlacesNewNodesNearReusedStableNeighbors(t *testing.T) {
	requirement, plan, existing := nodeID(t, 1), nodeID(t, 2), nodeID(t, 3)
	base := graphlayout.Input{
		Nodes: []graphlayout.Node{
			{ID: requirement, Class: graph.NodeClassRequirement},
			{ID: plan, Class: graph.NodeClassPlanRegion},
			{ID: existing, Class: graph.NodeClassAtomOperation},
		},
		Edges: []graphlayout.Edge{
			{ID: edgeID(t, 1), FromNode: requirement, ToNode: plan},
			{ID: edgeID(t, 2), FromNode: plan, ToNode: existing},
		},
	}
	before, err := graphlayout.Compute(base)
	if err != nil {
		t.Fatal(err)
	}
	newNeighbor := nodeID(t, 4)
	base.Nodes = append(base.Nodes, graphlayout.Node{ID: newNeighbor, Class: graph.NodeClassAtomOperation})
	base.Edges = append(base.Edges, graphlayout.Edge{ID: edgeID(t, 3), FromNode: plan, ToNode: newNeighbor})
	previous := prior(before)
	base.Previous = &previous
	after, err := graphlayout.Compute(base)
	if err != nil {
		t.Fatal(err)
	}
	if placement(t, after, plan).Bounds != placement(t, before, plan).Bounds ||
		placement(t, after, existing).Bounds != placement(t, before, existing).Bounds {
		t.Fatal("stable neighbors moved while placing a new adjacent node")
	}
	newBounds := placement(t, after, newNeighbor).Bounds
	planBounds := placement(t, after, plan).Bounds
	if delta := abs(newBounds.Y - planBounds.Y); delta > graphlayout.DefaultNodeHeight+graphlayout.DefaultSiblingGap {
		t.Fatalf("new node was not placed near stable neighbor: plan=%+v new=%+v", planBounds, newBounds)
	}
	if overlaps(newBounds, placement(t, after, existing).Bounds) {
		t.Fatalf("new node overlaps existing sibling: %+v", newBounds)
	}
}

func TestComputeIgnoresCoordinatesFromAnotherAlgorithmVersion(t *testing.T) {
	fixture := linearFixture(t)
	canonical, err := graphlayout.Compute(fixture)
	if err != nil {
		t.Fatal(err)
	}
	previous := prior(canonical)
	previous.AlgorithmVersion = "obsolete-layout"
	previous.Nodes[0].Bounds.X = 99_999
	got, err := graphlayout.Compute(graphlayout.Input{Nodes: fixture.Nodes, Edges: fixture.Edges, Previous: &previous})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, canonical) {
		t.Fatalf("obsolete coordinates were reused:\ngot=%+v\nwant=%+v", got, canonical)
	}
}

func TestComputeRejectsInvalidAndUnboundedInputs(t *testing.T) {
	known := nodeID(t, 1)
	unknown := nodeID(t, 2)
	_, err := graphlayout.Compute(graphlayout.Input{
		Nodes: []graphlayout.Node{{ID: known, Class: graph.NodeClassPlanRegion}},
		Edges: []graphlayout.Edge{{ID: edgeID(t, 1), FromNode: known, ToNode: unknown}},
	})
	if !errors.Is(err, graphlayout.ErrInvalidLayoutInput) {
		t.Fatalf("unknown endpoint error=%v", err)
	}
	overflow := make([]graphlayout.Node, graphlayout.MaxLayoutNodes+1)
	for index := range overflow {
		overflow[index] = graphlayout.Node{ID: nodeID(t, index+100), Class: graph.NodeClassAtomOperation}
	}
	if _, err = graphlayout.Compute(graphlayout.Input{Nodes: overflow}); !errors.Is(err, graphlayout.ErrInvalidLayoutInput) {
		t.Fatalf("unbounded input error=%v", err)
	}
}

func TestCacheBoundaryScopesHintsByGraphRevisionAndAlgorithm(t *testing.T) {
	graphID, err := domain.ParseGraphID("grf_01890f3c-4a00-7abc-8def-012345678901")
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := domain.ParseGraphRevisionID("grv_01890f3c-4a00-7abc-8def-012345678902")
	if err != nil {
		t.Fatal(err)
	}
	cache := &memoryCache{entries: map[graphlayout.CacheKey]graphlayout.CachedLayout{}}
	var port graphlayout.Cache = cache
	key := graphlayout.CacheKey{GraphID: graphID, GraphRevisionID: revisionID, AlgorithmVersion: graphlayout.AlgorithmVersion}
	want := graphlayout.CachedLayout{Key: key, Layout: graphlayout.Layout{AlgorithmVersion: graphlayout.AlgorithmVersion}}
	if err := want.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := port.StoreLayout(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := port.LoadLayout(t.Context(), key)
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("cache load=%+v ok=%t err=%v", got, ok, err)
	}
	wrongVersion := key
	wrongVersion.AlgorithmVersion = "other"
	if _, ok, err = port.LoadLayout(t.Context(), wrongVersion); err != nil || ok {
		t.Fatalf("cache crossed algorithm versions: ok=%t err=%v", ok, err)
	}
}

type memoryCache struct {
	entries map[graphlayout.CacheKey]graphlayout.CachedLayout
}

func (cache *memoryCache) LoadLayout(_ context.Context, key graphlayout.CacheKey) (graphlayout.CachedLayout, bool, error) {
	value, ok := cache.entries[key]
	return value, ok, nil
}

func (cache *memoryCache) StoreLayout(_ context.Context, value graphlayout.CachedLayout) error {
	cache.entries[value.Key] = value
	return nil
}

func linearFixture(t *testing.T) graphlayout.Input {
	t.Helper()
	nodes := []graphlayout.Node{
		{ID: nodeID(t, 1), Class: graph.NodeClassRequirement},
		{ID: nodeID(t, 2), Class: graph.NodeClassPlanRegion},
		{ID: nodeID(t, 3), Class: graph.NodeClassAtomOperation},
		{ID: nodeID(t, 4), Class: graph.NodeClassEffect},
		{ID: nodeID(t, 5), Class: graph.NodeClassArtifactResult},
		{ID: nodeID(t, 6), Class: graph.NodeClassArtifactResult},
	}
	return graphlayout.Input{
		Nodes: nodes,
		Edges: []graphlayout.Edge{
			{ID: edgeID(t, 1), FromNode: nodes[0].ID, ToNode: nodes[1].ID},
			{ID: edgeID(t, 2), FromNode: nodes[1].ID, ToNode: nodes[2].ID},
			{ID: edgeID(t, 3), FromNode: nodes[2].ID, ToNode: nodes[3].ID},
			{ID: edgeID(t, 4), FromNode: nodes[3].ID, ToNode: nodes[4].ID},
			{ID: edgeID(t, 5), FromNode: nodes[2].ID, ToNode: nodes[5].ID},
		},
	}
}

func prior(layout graphlayout.Layout) graphlayout.PriorLayout {
	return graphlayout.PriorLayout{AlgorithmVersion: layout.AlgorithmVersion, Nodes: append([]graphlayout.Placement(nil), layout.Nodes...)}
}

func placement(t *testing.T, layout graphlayout.Layout, id domain.NodeID) graphlayout.Placement {
	t.Helper()
	for _, placement := range layout.Nodes {
		if placement.NodeID == id {
			return placement
		}
	}
	t.Fatalf("layout has no node %s", id)
	return graphlayout.Placement{}
}

func componentFor(t *testing.T, layout graphlayout.Layout, key domain.NodeID) graphlayout.Component {
	t.Helper()
	for _, component := range layout.Components {
		if component.Key == key {
			return component
		}
	}
	t.Fatalf("layout has no component %s", key)
	return graphlayout.Component{}
}

func nodeID(t *testing.T, suffix int) domain.NodeID {
	t.Helper()
	id, err := domain.ParseNodeID(fixtureIdentity("nod", suffix))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func edgeID(t *testing.T, suffix int) domain.EdgeID {
	t.Helper()
	id, err := domain.ParseEdgeID(fixtureIdentity("edg", suffix))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fixtureIdentity(prefix string, suffix int) string {
	const hex = "0123456789abcdef"
	last := make([]byte, 12)
	for index := len(last) - 1; index >= 0; index-- {
		last[index] = hex[suffix&15]
		suffix >>= 4
	}
	return prefix + "_01890f3c-4a00-7abc-8def-" + string(last)
}

func overlaps(left, right graphlayout.Rect) bool {
	return left.X < right.X+right.Width && left.X+left.Width > right.X &&
		left.Y < right.Y+right.Height && left.Y+left.Height > right.Y
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
