package graphlayout

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
)

const (
	m19VisibleNodeCount = 300
	m19PatchCount       = 100
	m19PatchP95Limit    = 100 * time.Millisecond
)

func TestM19Initial300NodeLayoutAndSequentialPatchBudget(t *testing.T) {
	input := m19PerformanceInput(t, m19VisibleNodeCount)
	initialSamples := make([]time.Duration, 30)
	var layout Layout
	for index := range initialSamples {
		started := time.Now()
		for range 10 {
			computed, err := Compute(input)
			if err != nil {
				t.Fatal(err)
			}
			layout = computed
		}
		initialSamples[index] = time.Since(started) / 10
	}
	initialP50, initialP95, initialMax := m19DurationSummary(initialSamples)
	if initialP95 >= m19PatchP95Limit {
		t.Fatalf("300-node initial layout p95=%s exceeds %s", initialP95, m19PatchP95Limit)
	}

	patchSamples := make([]time.Duration, m19PatchCount)
	for patch := range m19PatchCount {
		input.Previous = &PriorLayout{AlgorithmVersion: layout.AlgorithmVersion, Nodes: append([]Placement(nil), layout.Nodes...)}
		edgeIndex := patch % len(input.Edges)
		edge := input.Edges[edgeIndex]
		edge.ToNode = input.Nodes[(patch+2)%len(input.Nodes)].ID
		if edge.ToNode == edge.FromNode {
			edge.ToNode = input.Nodes[(patch+3)%len(input.Nodes)].ID
		}
		input.Edges[edgeIndex] = edge
		started := time.Now()
		computed, err := Compute(input)
		patchSamples[patch] = time.Since(started)
		if err != nil {
			t.Fatalf("patch %d: %v", patch+1, err)
		}
		layout = computed
	}
	patchP50, patchP95, patchMax := m19DurationSummary(patchSamples)
	if patchP95 >= m19PatchP95Limit {
		t.Fatalf("100 sequential graph patches p95=%s exceeds %s", patchP95, m19PatchP95Limit)
	}
	t.Logf("M19-103 300-node layout: p50=%s p95=%s max=%s threshold=%s", initialP50, initialP95, initialMax, m19PatchP95Limit)
	t.Logf("M19-104 100 sequential patches: p50=%s p95=%s max=%s threshold=%s", patchP50, patchP95, patchMax, m19PatchP95Limit)
}

func BenchmarkM19Initial300NodeLayout(b *testing.B) {
	input := m19PerformanceInput(b, m19VisibleNodeCount)
	b.ReportAllocs()
	for range b.N {
		if _, err := Compute(input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkM19Sequential100GraphPatches(b *testing.B) {
	base := m19PerformanceInput(b, m19VisibleNodeCount)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		input := base
		input.Nodes = append([]Node(nil), base.Nodes...)
		input.Edges = append([]Edge(nil), base.Edges...)
		layout, err := Compute(input)
		if err != nil {
			b.Fatal(err)
		}
		for patch := range m19PatchCount {
			input.Previous = &PriorLayout{AlgorithmVersion: layout.AlgorithmVersion, Nodes: append([]Placement(nil), layout.Nodes...)}
			edgeIndex := patch % len(input.Edges)
			edge := input.Edges[edgeIndex]
			edge.ToNode = input.Nodes[(patch+2)%len(input.Nodes)].ID
			if edge.ToNode == edge.FromNode {
				edge.ToNode = input.Nodes[(patch+3)%len(input.Nodes)].ID
			}
			input.Edges[edgeIndex] = edge
			layout, err = Compute(input)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(max(b.N, 1))/float64(m19PatchCount), "ns/patch")
}

func m19PerformanceInput(tb testing.TB, nodeCount int) Input {
	tb.Helper()
	nodes := make([]Node, nodeCount)
	for index := range nodes {
		nodes[index] = Node{
			ID:    m19PerformanceID(tb, domain.ParseNodeID, "nod", index+1),
			Class: graph.AllNodeClasses()[index%len(graph.AllNodeClasses())],
		}
	}
	edges := make([]Edge, 0, nodeCount*2)
	for index := 0; index < nodeCount-1; index++ {
		edges = append(edges, Edge{
			ID:       m19PerformanceID(tb, domain.ParseEdgeID, "edg", len(edges)+1),
			FromNode: nodes[index].ID, ToNode: nodes[index+1].ID,
		})
		if index+5 < nodeCount {
			edges = append(edges, Edge{
				ID:       m19PerformanceID(tb, domain.ParseEdgeID, "edg", len(edges)+1),
				FromNode: nodes[index].ID, ToNode: nodes[index+5].ID,
			})
		}
	}
	return Input{Nodes: nodes, Edges: edges}
}

func m19PerformanceID[T any](tb testing.TB, parse func(string) (T, error), prefix string, ordinal int) T {
	tb.Helper()
	raw := fmt.Sprintf("%s_01890f3c-4a00-7abc-8def-%012d", prefix, ordinal)
	value, err := parse(raw)
	if err != nil {
		tb.Fatal(err)
	}
	return value
}

func m19DurationSummary(samples []time.Duration) (p50, p95, maximum time.Duration) {
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[(len(ordered)-1)*50/100], ordered[(len(ordered)-1)*95/100], ordered[len(ordered)-1]
}
