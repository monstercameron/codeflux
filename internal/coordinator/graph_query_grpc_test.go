package coordinator

import (
	"context"
	"database/sql"
	"net"
	"path/filepath"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"codeflux.dev/codeflux/migrations"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	_ "modernc.org/sqlite"
)

func TestGraphServiceRealSQLiteParityAndAuthentication(t *testing.T) {
	fixture := createGraphGRPCFixture(t)
	boundary, err := transport.NewBoundary(transport.BoundaryOptions{SessionToken: fixture.token})
	if err != nil {
		t.Fatal(err)
	}
	application, err := NewGraphQueryService(fixture.repositories)
	if err != nil {
		t.Fatal(err)
	}
	service, err := transport.NewGraphService(application)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(boundary.UnaryInterceptor()))
	codefluxv1.RegisterGraphServiceServer(server, service)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-serveDone })
	connection, err := grpc.NewClient("passthrough:///graph", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := codefluxv1.NewGraphServiceClient(connection)

	request := &codefluxv1.GetGraphSliceRequest{ProjectId: projectIdentity(fixture.projectID), TaskId: taskIdentity(fixture.taskID), Mode: "program", MaxNodes: 1, MaxEdges: 1}
	if _, err := client.GetGraphSlice(t.Context(), request); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated graph code = %v (%v)", status.Code(err), err)
	}
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(transport.SessionMetadataKey, fixture.token))
	first, err := client.GetGraphSlice(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetGraph().GetGraphRevisionId().GetValue() != fixture.secondRevision.String() || len(first.GetGraph().GetNodes()) != 1 || !first.GetGraph().GetTruncated() || first.GetGraph().GetContinuationCursor() == "" {
		t.Fatalf("initial graph page = %#v", first.GetGraph())
	}
	if first.GetGraph().GetLayoutAlgorithm() != "layered-ltr" || first.GetGraph().GetLayoutAlgorithmVersion() != 1 {
		t.Fatalf("layout identity = %q/%d", first.GetGraph().GetLayoutAlgorithm(), first.GetGraph().GetLayoutAlgorithmVersion())
	}
	secondPage, err := client.GetGraphSlice(ctx, &codefluxv1.GetGraphSliceRequest{ProjectId: projectIdentity(fixture.projectID), TaskId: taskIdentity(fixture.taskID), Mode: "program", MaxNodes: 1, MaxEdges: 1, ContinuationCursor: first.GetGraph().GetContinuationCursor()})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.GetGraph().GetNodes()) != 1 {
		t.Fatalf("continued nodes = %d", len(secondPage.GetGraph().GetNodes()))
	}

	node, err := client.GetNode(ctx, &codefluxv1.GetNodeRequest{ProjectId: projectIdentity(fixture.projectID), TaskId: taskIdentity(fixture.taskID), GraphRevisionId: graphRevisionIdentity(fixture.secondRevision), NodeId: nodeIdentity(fixture.secondNode)})
	if err != nil {
		t.Fatal(err)
	}
	if node.GetNode().GetLabel().GetValue() != "Second result updated" || node.GetNode().GetLayout().GetAlgorithmVersion() != 1 {
		t.Fatalf("node parity = %#v", node.GetNode())
	}
	explanation, err := client.ExplainNode(ctx, &codefluxv1.ExplainNodeRequest{ProjectId: projectIdentity(fixture.projectID), TaskId: taskIdentity(fixture.taskID), GraphRevisionId: graphRevisionIdentity(fixture.secondRevision), NodeId: nodeIdentity(fixture.secondNode)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(explanation.GetExplanation().GetValue(), "artifact-result node with passed status") || explanation.GetNode().GetNodeId().GetValue() != fixture.secondNode.String() {
		t.Fatalf("node explanation parity = %#v", explanation)
	}

	expanded, err := client.ExpandGraph(ctx, &codefluxv1.ExpandGraphRequest{ProjectId: projectIdentity(fixture.projectID), TaskId: taskIdentity(fixture.taskID), GraphRevisionId: graphRevisionIdentity(fixture.secondRevision), RootNodeIds: []*codefluxv1.StableIdentity{nodeIdentity(fixture.firstNode)}, Mode: "program", Traversal: "neighbors", MaxHops: 1, MaxNodes: 10, MaxEdges: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(expanded.GetGraph().GetNodes()) != 2 || len(expanded.GetGraph().GetEdges()) != 1 {
		t.Fatalf("expanded parity = %#v", expanded.GetGraph())
	}

	search, err := client.SearchGraph(ctx, &codefluxv1.SearchGraphRequest{ProjectId: projectIdentity(fixture.projectID), TaskId: taskIdentity(fixture.taskID), GraphRevisionId: graphRevisionIdentity(fixture.secondRevision), Mode: "program", Query: "updated", MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.GetGraph().GetNodes()) != 1 || search.GetGraph().GetNodes()[0].GetNodeId().GetValue() != fixture.secondNode.String() {
		t.Fatalf("search parity = %#v", search.GetGraph())
	}

	comparison, err := client.CompareGraphRevisions(ctx, &codefluxv1.CompareGraphRevisionsRequest{ProjectId: projectIdentity(fixture.projectID), TaskId: taskIdentity(fixture.taskID), FromGraphRevisionId: graphRevisionIdentity(fixture.firstRevision), ToGraphRevisionId: graphRevisionIdentity(fixture.secondRevision), MaxChanges: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(comparison.GetChanges()) != 1 || comparison.GetChanges()[0].GetChangeKind() != "updated" || comparison.GetChangedNodes()[0].GetNodeId().GetValue() != fixture.secondNode.String() {
		t.Fatalf("comparison parity = %#v", comparison)
	}

	wrongProject, _ := domain.NewProjectID()
	_, err = client.GetGraphSlice(ctx, &codefluxv1.GetGraphSliceRequest{ProjectId: projectIdentity(wrongProject), TaskId: taskIdentity(fixture.taskID), Mode: "program", MaxNodes: 10, MaxEdges: 10})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("cross-project graph code = %v (%v)", status.Code(err), err)
	}
}

type graphGRPCFixture struct {
	repositories                  *storage.Repositories
	database                      *storage.Database
	token                         string
	projectID                     domain.ProjectID
	taskID                        domain.TaskID
	firstRevision, secondRevision domain.GraphRevisionID
	firstNode, secondNode         domain.NodeID
}

func createGraphGRPCFixture(t *testing.T) graphGRPCFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph-grpc.sqlite3")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	sources, err := migrations.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if _, err := raw.ExecContext(t.Context(), source.SQL); err != nil {
			t.Fatalf("apply %s: %v", source.Descriptor.Name, err)
		}
	}
	if _, err := raw.ExecContext(t.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}

	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	taskID, _ := domain.NewTaskID()
	graphID, _ := domain.NewGraphID()
	firstRevision, _ := domain.NewGraphRevisionID()
	secondRevision, _ := domain.NewGraphRevisionID()
	firstNode, _ := domain.NewNodeID()
	secondNode, _ := domain.NewNodeID()
	edgeID, _ := domain.NewEdgeID()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id, name, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, 'Graph project', 1, 1, 1)`, []any{projectID}},
		{`INSERT INTO repositories (id, project_id, canonical_path, git_identity, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, ?, 'C:/graph', 'graph-git', 1, 1, 1)`, []any{repositoryID, projectID}},
		{`INSERT INTO threads (id, project_id, repository_id, title, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, ?, ?, 'Graph thread', 1, 1, 1)`, []any{threadID, projectID, repositoryID}},
		{`INSERT INTO tasks (id, thread_id, repository_id, state, policy_preset, reasoning_effort, risk_level, required_assurance, idempotency_key, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, ?, ?, 'running', 'balanced', 'standard', 'routine', 'runtime-only', 'graph-task', 1, 1, 1)`, []any{taskID, threadID, repositoryID}},
		{`INSERT INTO graphs (id, project_id, repository_id, created_at_unix_micros) VALUES (?, ?, ?, 1)`, []any{graphID, projectID, repositoryID}},
		{`INSERT INTO graph_task_bindings (graph_id, project_id, repository_id, thread_id, task_id, created_at_unix_micros) VALUES (?, ?, ?, ?, ?, 1)`, []any{graphID, projectID, repositoryID, threadID, taskID}},
		{`INSERT INTO graph_revisions (id, graph_id, revision, graph_schema_version, metadata_policy, created_at_unix_micros) VALUES (?, ?, 1, 1, 'typed-fields-only', 1)`, []any{firstRevision, graphID}},
		{`INSERT INTO graph_nodes (id, graph_id, created_at_unix_micros) VALUES (?, ?, 1), (?, ?, 1)`, []any{firstNode, graphID, secondNode, graphID}},
		{`INSERT INTO graph_node_revisions (graph_id, graph_revision_id, node_id, node_class, status, display_name_redacted, contract_purpose_redacted, tombstoned, created_at_unix_micros) VALUES (?, ?, ?, 'requirement', 'active', 'Accepted intent', 'Bind the accepted intent.', 0, 1), (?, ?, ?, 'artifact-result', 'pending', 'Second result', 'Return bounded evidence.', 0, 1)`, []any{graphID, firstRevision, firstNode, graphID, firstRevision, secondNode}},
		{`INSERT INTO graph_edges (id, graph_id, created_at_unix_micros) VALUES (?, ?, 1)`, []any{edgeID, graphID}},
		{`INSERT INTO graph_edge_revisions (graph_id, graph_revision_id, edge_id, edge_class, source_node_id, target_node_id, tombstoned, created_at_unix_micros) VALUES (?, ?, ?, 'evidence-dependency', ?, ?, 0, 1)`, []any{graphID, firstRevision, edgeID, firstNode, secondNode}},
		{`INSERT INTO graph_revision_seals (graph_id, graph_revision_id, node_count, edge_count, content_sha256, sealed_at_unix_micros) VALUES (?, ?, 2, 1, ?, 1)`, []any{graphID, firstRevision, strings.Repeat("1", 64)}},
		{`INSERT INTO graph_revisions (id, graph_id, revision, parent_revision_id, graph_schema_version, metadata_policy, created_at_unix_micros) VALUES (?, ?, 2, ?, 1, 'typed-fields-only', 2)`, []any{secondRevision, graphID, firstRevision}},
		{`INSERT INTO graph_node_revisions (graph_id, graph_revision_id, node_id, node_class, status, display_name_redacted, contract_purpose_redacted, tombstoned, created_at_unix_micros) VALUES (?, ?, ?, 'requirement', 'active', 'Accepted intent', 'Bind the accepted intent.', 0, 2), (?, ?, ?, 'artifact-result', 'passed', 'Second result updated', 'Return bounded evidence.', 0, 2)`, []any{graphID, secondRevision, firstNode, graphID, secondRevision, secondNode}},
		{`INSERT INTO graph_edge_revisions (graph_id, graph_revision_id, edge_id, edge_class, source_node_id, target_node_id, tombstoned, created_at_unix_micros) VALUES (?, ?, ?, 'evidence-dependency', ?, ?, 0, 2)`, []any{graphID, secondRevision, edgeID, firstNode, secondNode}},
		{`INSERT INTO graph_layout_hints (graph_id, graph_revision_id, node_id, algorithm, algorithm_version, x_milli, y_milli, width_milli, height_milli, rank, sibling_order, created_at_unix_micros) VALUES (?, ?, ?, 'layered-ltr', 1, 1000, 2000, 240000, 88000, 0, 0, 2), (?, ?, ?, 'layered-ltr', 1, 353000, 2000, 240000, 88000, 1, 0, 2)`, []any{graphID, secondRevision, firstNode, graphID, secondRevision, secondNode}},
		{`INSERT INTO graph_revision_seals (graph_id, graph_revision_id, node_count, edge_count, content_sha256, sealed_at_unix_micros) VALUES (?, ?, 2, 1, ?, 2)`, []any{graphID, secondRevision, strings.Repeat("2", 64)}},
	}
	for _, statement := range statements {
		if _, err := raw.ExecContext(t.Context(), statement.query, statement.args...); err != nil {
			t.Fatalf("seed graph fixture: %v\n%s", err, statement.query)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := storage.Open(t.Context(), storage.OpenOptions{Path: path, MaximumConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	return graphGRPCFixture{repositories: repositories, database: database, token: strings.Repeat("g", 48), projectID: projectID, taskID: taskID, firstRevision: firstRevision, secondRevision: secondRevision, firstNode: firstNode, secondNode: secondNode}
}

func projectIdentity(id domain.ProjectID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROJECT, Value: id.String()}
}
func taskIdentity(id domain.TaskID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: id.String()}
}
func graphRevisionIdentity(id domain.GraphRevisionID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH_REVISION, Value: id.String()}
}
func nodeIdentity(id domain.NodeID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_NODE, Value: id.String()}
}
