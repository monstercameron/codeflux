package storage

import (
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
)

type graphQueryFixture struct {
	agentPlanFixture
	projectID   domain.ProjectID
	graphID     domain.GraphID
	revisionID  domain.GraphRevisionID
	requirement domain.NodeID
	obligation  domain.NodeID
	effect      domain.NodeID
	artifact    domain.NodeID
	cycle       domain.NodeID
	edges       []domain.EdgeID
}

func TestTaskGraphQueriesAreBoundedScopedAndModeAware(t *testing.T) {
	fixture := createGraphQueryFixture(t, 29_100)
	ctx := t.Context()
	scope := GraphQueryScope{ProjectID: fixture.projectID, TaskID: fixture.task.ID}

	program, err := fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{
		Scope: scope, Mode: taskgraph.ModeProgram, MaxNodes: 10, MaxEdges: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Nodes) != 5 || len(program.Edges) != 6 {
		t.Fatalf("program slice = %d nodes, %d edges", len(program.Nodes), len(program.Edges))
	}
	if program.Header.LayoutAlgorithm != "layered-ltr" || program.Header.LayoutVersion != 1 {
		t.Fatalf("layout header = %#v", program.Header)
	}
	hasLayout := false
	for _, node := range program.Nodes {
		hasLayout = hasLayout || node.Layout != nil
	}
	if !hasLayout {
		t.Fatal("selected layout hints were not returned")
	}

	execution, err := fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{
		Scope: scope, Mode: taskgraph.ModeExecution, MaxNodes: 10, MaxEdges: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(execution.Nodes) != 4 {
		t.Fatalf("execution nodes = %d", len(execution.Nodes))
	}
	for _, node := range execution.Nodes {
		if node.Node.Class() == taskgraph.NodeClassRequirement {
			t.Fatal("execution mode leaked a requirement")
		}
	}

	evidence, err := fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{
		Scope: scope, Mode: taskgraph.ModeEvidence, MaxNodes: 10, MaxEdges: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Nodes) != 4 {
		t.Fatalf("evidence nodes = %d, want 4", len(evidence.Nodes))
	}
	for _, node := range evidence.Nodes {
		if node.Node.ID() == fixture.cycle {
			t.Fatal("evidence mode leaked the unrelated cycle node")
		}
	}

	node, err := fixture.repositories.GetTaskGraphNode(ctx, TaskGraphNodeQuery{
		Scope: scope, RevisionID: fixture.revisionID, NodeID: fixture.requirement,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(node.MessageIDs) != 1 || len(node.SourceLocations) != 1 || node.Node.DisplayName() != "Accepted requirement" {
		t.Fatalf("stable node lookup = %#v", node)
	}
	missing, _ := domain.NewNodeID()
	if _, err := fixture.repositories.GetTaskGraphNode(ctx, TaskGraphNodeQuery{Scope: scope, RevisionID: fixture.revisionID, NodeID: missing}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing node error = %v", err)
	}
	wrongProject, _ := domain.NewProjectID()
	if _, err := fixture.repositories.GetTaskGraphNode(ctx, TaskGraphNodeQuery{Scope: GraphQueryScope{ProjectID: wrongProject, TaskID: fixture.task.ID}, RevisionID: fixture.revisionID, NodeID: fixture.requirement}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project node error = %v", err)
	}
	wrongTask, _ := domain.NewTaskID()
	if _, err := fixture.repositories.GetTaskGraphNode(ctx, TaskGraphNodeQuery{Scope: GraphQueryScope{ProjectID: fixture.projectID, TaskID: wrongTask}, RevisionID: fixture.revisionID, NodeID: fixture.requirement}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-task node error = %v", err)
	}

	if _, err := fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{Scope: scope, Mode: taskgraph.ModeProgram}); !errors.Is(err, ErrGraphQueryUnbounded) {
		t.Fatalf("unbounded slice error = %v", err)
	}
	if _, err := fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{Scope: scope, Mode: taskgraph.ModeProgram, MaxNodes: MaximumGraphQueryNodes + 1, MaxEdges: 1}); !errors.Is(err, ErrGraphQueryLimitExceeded) {
		t.Fatalf("oversized slice error = %v", err)
	}
	if _, err := fixture.repositories.GetTaskGraphNode(ctx, TaskGraphNodeQuery{Scope: scope, RevisionID: fixture.revisionID, NodeID: fixture.requirement, Layout: GraphLayoutSelection{Algorithm: "missing", Version: 1}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing layout error = %v", err)
	}
}

func TestTaskGraphExpansionSearchAndContinuation(t *testing.T) {
	fixture := createGraphQueryFixture(t, 29_200)
	ctx := t.Context()
	scope := GraphQueryScope{ProjectID: fixture.projectID, TaskID: fixture.task.ID}

	oneHop, err := fixture.repositories.ExpandTaskGraph(ctx, TaskGraphExpansionQuery{
		Scope: scope, RevisionID: fixture.revisionID, RootNodeIDs: []domain.NodeID{fixture.effect},
		Mode: taskgraph.ModeProgram, Traversal: GraphTraversalNeighbors, MaxHops: 1,
		MaxNodes: 10, MaxEdges: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(oneHop.Nodes) != 4 {
		t.Fatalf("one-hop nodes = %d, want 4", len(oneHop.Nodes))
	}

	cycled, err := fixture.repositories.ExpandTaskGraph(ctx, TaskGraphExpansionQuery{
		Scope: scope, RevisionID: fixture.revisionID, RootNodeIDs: []domain.NodeID{fixture.effect},
		Mode: taskgraph.ModeProgram, Traversal: GraphTraversalNeighbors, MaxHops: MaximumGraphExpansionHops,
		MaxNodes: 10, MaxEdges: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cycled.Nodes) != 5 || len(cycled.Edges) != 6 {
		t.Fatalf("cycle expansion = %d nodes, %d edges", len(cycled.Nodes), len(cycled.Edges))
	}

	evidence, err := fixture.repositories.ExpandTaskGraph(ctx, TaskGraphExpansionQuery{
		Scope: scope, RevisionID: fixture.revisionID, RootNodeIDs: []domain.NodeID{fixture.artifact},
		Mode: taskgraph.ModeProgram, Traversal: GraphTraversalEvidence, MaxHops: 2,
		MaxNodes: 10, MaxEdges: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Nodes) != 2 || evidence.Nodes[0].Node.ID() == fixture.cycle || evidence.Nodes[1].Node.ID() == fixture.cycle {
		t.Fatalf("evidence cone = %#v", graphNodeIDs(evidence.Nodes))
	}

	dependencies, err := fixture.repositories.ExpandTaskGraph(ctx, TaskGraphExpansionQuery{
		Scope: scope, RevisionID: fixture.revisionID, RootNodeIDs: []domain.NodeID{fixture.artifact},
		Mode: taskgraph.ModeProgram, Traversal: GraphTraversalDependencies, MaxHops: 2,
		MaxNodes: 10, MaxEdges: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies.Nodes) != 4 {
		t.Fatalf("dependency cone = %#v", graphNodeIDs(dependencies.Nodes))
	}
	if _, err := fixture.repositories.ExpandTaskGraph(ctx, TaskGraphExpansionQuery{Scope: scope, RevisionID: fixture.revisionID, RootNodeIDs: []domain.NodeID{fixture.effect}, Mode: taskgraph.ModeProgram, Traversal: GraphTraversalNeighbors, MaxHops: MaximumGraphExpansionHops + 1, MaxNodes: 1, MaxEdges: 1}); !errors.Is(err, ErrGraphQueryLimitExceeded) {
		t.Fatalf("hop limit error = %v", err)
	}
	missing, _ := domain.NewNodeID()
	if _, err := fixture.repositories.ExpandTaskGraph(ctx, TaskGraphExpansionQuery{Scope: scope, RevisionID: fixture.revisionID, RootNodeIDs: []domain.NodeID{missing}, Mode: taskgraph.ModeProgram, Traversal: GraphTraversalNeighbors, MaxHops: 1, MaxNodes: 1, MaxEdges: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing expansion root error = %v", err)
	}

	search, err := fixture.repositories.SearchTaskGraph(ctx, TaskGraphSearchQuery{Scope: scope, RevisionID: fixture.revisionID, Mode: taskgraph.ModeProgram, Text: "internal/widget.go", MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Nodes) != 1 || search.Nodes[0].Node.ID() != fixture.requirement {
		t.Fatalf("path search = %#v", graphNodeIDs(search.Nodes))
	}
	identity, err := fixture.repositories.SearchTaskGraph(ctx, TaskGraphSearchQuery{Scope: scope, RevisionID: fixture.revisionID, Mode: taskgraph.ModeProgram, Text: fixture.artifact.String(), MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(identity.Nodes) != 1 || identity.Nodes[0].Node.ID() != fixture.artifact {
		t.Fatalf("identity search = %#v", graphNodeIDs(identity.Nodes))
	}

	first, err := fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{Scope: scope, Mode: taskgraph.ModeProgram, MaxNodes: 2, MaxEdges: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Continuation == "" {
		t.Fatal("bounded slice omitted continuation")
	}
	seen := map[domain.NodeID]bool{}
	page := first
	for page.Continuation != "" {
		for _, node := range page.Nodes {
			seen[node.Node.ID()] = true
		}
		page, err = fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{Scope: scope, Mode: taskgraph.ModeProgram, MaxNodes: 2, MaxEdges: 1, Continuation: page.Continuation})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, node := range page.Nodes {
		seen[node.Node.ID()] = true
	}
	if len(seen) != 5 {
		t.Fatalf("continued nodes = %d", len(seen))
	}
	replacement := byte('0')
	if first.Continuation[len(first.Continuation)-1] == replacement {
		replacement = '1'
	}
	tampered := first.Continuation[:len(first.Continuation)-1] + string(replacement)
	if _, err := fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{Scope: scope, Mode: taskgraph.ModeProgram, MaxNodes: 2, MaxEdges: 1, Continuation: tampered}); !errors.Is(err, ErrGraphContinuation) {
		t.Fatalf("tampered continuation error = %v", err)
	}
	if _, err := fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{Scope: scope, Mode: taskgraph.ModeEvidence, MaxNodes: 2, MaxEdges: 1, Continuation: first.Continuation}); !errors.Is(err, ErrGraphContinuation) {
		t.Fatalf("cross-query continuation error = %v", err)
	}
}

func TestTaskGraphStaleRevisionAndSemanticComparison(t *testing.T) {
	fixture := createGraphQueryFixture(t, 29_300)
	ctx := t.Context()
	scope := GraphQueryScope{ProjectID: fixture.projectID, TaskID: fixture.task.ID}
	second, _ := domain.NewGraphRevisionID()
	mustGraphSQL(t, fixture.repositories.database, `INSERT INTO graph_revisions (id, graph_id, revision, parent_revision_id, graph_schema_version, metadata_policy, created_at_unix_micros) VALUES (?, ?, 2, ?, 1, 'typed-fields-only', 2)`, second, fixture.graphID, fixture.revisionID)
	if _, err := fixture.repositories.GetTaskGraphNode(ctx, TaskGraphNodeQuery{Scope: scope, RevisionID: second, NodeID: fixture.effect}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("unsealed revision error = %v", err)
	}

	for _, node := range []struct {
		id                           domain.NodeID
		class, status, name, purpose string
		tomb                         int
	}{
		{fixture.requirement, "requirement", "active", "Accepted requirement", "Explain the accepted user requirement.", 0},
		{fixture.obligation, "obligation", "passed", "Run targeted tests", "Record whether the targeted checks pass.", 0},
		{fixture.effect, "effect", "invalidated", "Write parser", "Implement the bounded parser.", 1},
		{fixture.artifact, "artifact-result", "passed", "Coverage report", "Recorded test evidence.", 0},
		{fixture.cycle, "atom-operation", "active", "Cycle worker", "Exercise cycle safety.", 0},
	} {
		mustGraphSQL(t, fixture.repositories.database, `INSERT INTO graph_node_revisions (graph_id, graph_revision_id, node_id, node_class, status, display_name_redacted, contract_purpose_redacted, tombstoned, created_at_unix_micros) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 2)`, fixture.graphID, second, node.id, node.class, node.status, node.name, node.purpose, node.tomb)
	}
	for index, edge := range fixture.edges {
		classes := []string{"evidence-dependency", "control", "data-provenance", "reconciliation", "control", "retry"}
		endpoints := [][2]domain.NodeID{{fixture.requirement, fixture.obligation}, {fixture.obligation, fixture.effect}, {fixture.effect, fixture.artifact}, {fixture.artifact, fixture.obligation}, {fixture.effect, fixture.cycle}, {fixture.cycle, fixture.effect}}
		tomb := 0
		if index == 4 {
			tomb = 1
		}
		mustGraphSQL(t, fixture.repositories.database, `INSERT INTO graph_edge_revisions (graph_id, graph_revision_id, edge_id, edge_class, source_node_id, target_node_id, tombstoned, created_at_unix_micros) VALUES (?, ?, ?, ?, ?, ?, ?, 2)`, fixture.graphID, second, edge, classes[index], endpoints[index][0], endpoints[index][1], tomb)
	}
	mustGraphSQL(t, fixture.repositories.database, `INSERT INTO graph_revision_seals (graph_id, graph_revision_id, node_count, edge_count, content_sha256, sealed_at_unix_micros) VALUES (?, ?, 5, 6, ?, 2)`, fixture.graphID, second, strings.Repeat("a", 64))

	comparison, err := fixture.repositories.CompareTaskGraphRevisions(ctx, TaskGraphComparisonQuery{Scope: scope, FromRevisionID: fixture.revisionID, ToRevisionID: second, MaxChanges: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(comparison.Changes) != 2 || comparison.Continuation == "" {
		t.Fatalf("comparison page = %#v", comparison)
	}
	all := slicesOfChanges(comparison.Changes)
	for comparison.Continuation != "" {
		comparison, err = fixture.repositories.CompareTaskGraphRevisions(ctx, TaskGraphComparisonQuery{Scope: scope, FromRevisionID: fixture.revisionID, ToRevisionID: second, MaxChanges: 2, Continuation: comparison.Continuation})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, comparison.Changes...)
	}
	var updated, tombstoned bool
	for _, change := range all {
		updated = updated || (change.Entity == GraphEntityNode && change.ID == fixture.obligation.String() && change.Kind == GraphChangeUpdated)
		tombstoned = tombstoned || (change.Entity == GraphEntityNode && change.ID == fixture.effect.String() && change.Kind == GraphChangeTombstoned)
	}
	if !updated || !tombstoned {
		t.Fatalf("semantic changes = %#v", all)
	}
	expectedOld := fixture.revisionID
	if _, err := fixture.repositories.GetTaskGraphSlice(ctx, TaskGraphSliceQuery{Scope: scope, ExpectedRevisionID: &expectedOld, Mode: taskgraph.ModeProgram, MaxNodes: 10, MaxEdges: 10}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale latest expectation error = %v", err)
	}
}

func createGraphQueryFixture(t *testing.T, base int) graphQueryFixture {
	t.Helper()
	agent := createAgentPlanFixture(t, base)
	var projectID domain.ProjectID
	if err := agent.repositories.database.sql.QueryRowContext(t.Context(), "SELECT project_id FROM threads WHERE id = ?", agent.task.ThreadID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	eventID, _ := domain.NewEventID()
	event, err := agent.repositories.AppendTaskEvent(t.Context(), AppendTaskEvent{ID: eventID, TaskID: agent.task.ID, EventType: "graph-query-source", PayloadJSON: `{}`, IdempotencyKey: "graph-query-source"})
	if err != nil {
		t.Fatal(err)
	}
	graphID, _ := domain.NewGraphID()
	revisionID, _ := domain.NewGraphRevisionID()
	requirement, _ := domain.NewNodeID()
	obligation, _ := domain.NewNodeID()
	effect, _ := domain.NewNodeID()
	artifact, _ := domain.NewNodeID()
	cycle, _ := domain.NewNodeID()
	baseEdge, _ := domain.NewEdgeID()
	insertTaskGraphFixture(t, agent, projectID, graphID, revisionID, requirement, obligation, baseEdge, event.ID)
	for _, node := range []struct {
		id                           domain.NodeID
		class, status, name, purpose string
	}{
		{effect, "effect", "active", "Write parser", "Implement the bounded parser."},
		{artifact, "artifact-result", "passed", "Coverage report", "Recorded test evidence."},
		{cycle, "atom-operation", "active", "Cycle worker", "Exercise cycle safety."},
	} {
		mustGraphSQL(t, agent.repositories.database, `INSERT INTO graph_nodes (id, graph_id, created_at_unix_micros) VALUES (?, ?, 1)`, node.id, graphID)
		mustGraphSQL(t, agent.repositories.database, `INSERT INTO graph_node_revisions (graph_id, graph_revision_id, node_id, node_class, status, display_name_redacted, contract_purpose_redacted, tombstoned, created_at_unix_micros) VALUES (?, ?, ?, ?, ?, ?, ?, 0, 1)`, graphID, revisionID, node.id, node.class, node.status, node.name, node.purpose)
	}
	edges := []domain.EdgeID{baseEdge}
	for _, edge := range []struct {
		class    string
		from, to domain.NodeID
	}{
		{"control", obligation, effect}, {"data-provenance", effect, artifact}, {"reconciliation", artifact, obligation}, {"control", effect, cycle}, {"retry", cycle, effect},
	} {
		id, _ := domain.NewEdgeID()
		edges = append(edges, id)
		mustGraphSQL(t, agent.repositories.database, `INSERT INTO graph_edges (id, graph_id, created_at_unix_micros) VALUES (?, ?, 1)`, id, graphID)
		mustGraphSQL(t, agent.repositories.database, `INSERT INTO graph_edge_revisions (graph_id, graph_revision_id, edge_id, edge_class, source_node_id, target_node_id, tombstoned, created_at_unix_micros) VALUES (?, ?, ?, ?, ?, ?, 0, 1)`, graphID, revisionID, id, edge.class, edge.from, edge.to)
	}
	mustGraphSQL(t, agent.repositories.database, `INSERT INTO graph_layout_hints (graph_id, graph_revision_id, node_id, algorithm, algorithm_version, x_milli, y_milli, width_milli, height_milli, rank, sibling_order, created_at_unix_micros) VALUES (?, ?, ?, 'layered-ltr', 1, 2000, 3000, 240000, 80000, 1, 0, 1)`, graphID, revisionID, effect)
	mustGraphSQL(t, agent.repositories.database, `INSERT INTO graph_revision_seals (graph_id, graph_revision_id, node_count, edge_count, content_sha256, sealed_at_unix_micros) VALUES (?, ?, 5, 6, ?, 1)`, graphID, revisionID, strings.Repeat("b", 64))
	return graphQueryFixture{agentPlanFixture: agent, projectID: projectID, graphID: graphID, revisionID: revisionID, requirement: requirement, obligation: obligation, effect: effect, artifact: artifact, cycle: cycle, edges: edges}
}

func graphNodeIDs(nodes []GraphNodeRecord) []string {
	result := make([]string, len(nodes))
	for index, node := range nodes {
		result[index] = node.Node.ID().String()
	}
	return result
}
func slicesOfChanges(changes []GraphRevisionChange) []GraphRevisionChange {
	return append([]GraphRevisionChange(nil), changes...)
}
