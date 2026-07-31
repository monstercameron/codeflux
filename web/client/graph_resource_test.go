package main

import (
	"context"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDecodeGraphResourceProducesNativeRevisionAndServerLayout(t *testing.T) {
	fixture := graphResourceProtoFixture(t)
	resource, err := decodeGraphResource(fixture.view, fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	if resource.GraphID != fixture.graphID || resource.TaskID != fixture.scope.TaskID || resource.Mode != taskgraph.ModeEvidence || resource.Continuation != "next" || !resource.Truncated {
		t.Fatalf("resource envelope = %#v", resource)
	}
	if resource.Revision.Metadata().ID() != fixture.revisionID || resource.Revision.Metadata().Ordinal() != 2 || len(resource.Revision.Nodes()) != 2 || len(resource.Revision.Edges()) != 1 {
		t.Fatalf("native revision = %#v", resource.Revision)
	}
	if resource.Layout.AlgorithmVersion != "layered-ltr-v1" || len(resource.Layout.Nodes) != 2 {
		t.Fatalf("native layout = %#v", resource.Layout)
	}
	var found bool
	for _, placement := range resource.Layout.Nodes {
		if placement.NodeID == fixture.secondNode {
			found = true
			if placement.Bounds.X != 353 || placement.Bounds.Width != 240 || placement.Bounds.Height != 88 || placement.Rank != 1 {
				t.Fatalf("server placement = %#v", placement)
			}
		}
	}
	if !found {
		t.Fatal("server-positioned node missing from native layout")
	}
}

func TestGraphInspectorPropsExposeTypedQueryActions(t *testing.T) {
	fixture := graphResourceProtoFixture(t)
	resource, err := decodeGraphResource(fixture.view, fixture.scope)
	if err != nil {
		t.Fatal(err)
	}
	node := resource.Revision.Nodes()[1]
	selected := graphNodeResource{Node: node}
	var expanded, evidence domain.NodeID
	props, err := graphInspectorProps(selected, fixture.revisionID, graphInspectorResourceActions{
		Mode: primitives.Mode{}, ComparisonAvailable: true,
		OnExpandNeighbors: func(id domain.NodeID) { expanded = id },
		OnEvidenceCone:    func(id domain.NodeID) { evidence = id },
		OnCompareRevision: func(domain.GraphRevisionID) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !props.Actions.ExpandNeighbors.Enabled || !props.Actions.EvidenceCone.Enabled || !props.Actions.CompareRevision.Enabled || props.Actions.DependencyCone.Enabled {
		t.Fatalf("inspector actions = %#v", props.Actions)
	}
	props.OnExpandNeighbors(node.ID())
	props.OnIsolateEvidenceCone(node.ID())
	if expanded != node.ID() || evidence != node.ID() {
		t.Fatalf("typed callbacks = %v/%v", expanded, evidence)
	}
}

func TestLoadGraphResourceCarriesProjectTaskModeAndContinuation(t *testing.T) {
	fixture := graphResourceProtoFixture(t)
	client := &graphResourceClientStub{slice: &codefluxv1.GetGraphSliceResponse{Graph: fixture.view}}
	closed := false
	resource, err := loadGraphResource(t.Context(), func(context.Context) (graphResourceLease, error) {
		return graphResourceLease{client: client, close: func() error { closed = true; return nil }}, nil
	}, fixture.scope, taskgraph.ModeEvidence, 23, 47, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if !closed || resource.Revision.Metadata().ID() != fixture.revisionID {
		t.Fatalf("resource/lease = %#v closed=%t", resource, closed)
	}
	if client.sliceRequest == nil || client.sliceRequest.GetProjectId().GetValue() != fixture.scope.ProjectID.String() || client.sliceRequest.GetTaskId().GetValue() != fixture.scope.TaskID.String() || client.sliceRequest.GetMode() != "evidence" || client.sliceRequest.GetMaxNodes() != 23 || client.sliceRequest.GetMaxEdges() != 47 || client.sliceRequest.GetContinuationCursor() != "resume" {
		t.Fatalf("graph request = %#v", client.sliceRequest)
	}
}

func TestExplainGraphNodeResourceUsesScopedTypedRPC(t *testing.T) {
	fixture := graphResourceProtoFixture(t)
	client := &graphResourceClientStub{explanation: &codefluxv1.ExplainNodeResponse{Node: fixture.view.GetNodes()[1], Explanation: &codefluxv1.RedactedText{Value: "Bounded explanation.", OriginalBytes: 20}}}
	resource, err := explainGraphNodeResource(t.Context(), func(context.Context) (graphResourceLease, error) {
		return graphResourceLease{client: client, close: func() error { return nil }}, nil
	}, fixture.scope, fixture.revisionID, fixture.secondNode)
	if err != nil {
		t.Fatal(err)
	}
	if resource.Explanation != "Bounded explanation." || resource.Node.Node.ID() != fixture.secondNode {
		t.Fatalf("explanation resource = %#v", resource)
	}
	if client.explainRequest == nil || client.explainRequest.GetProjectId().GetValue() != fixture.scope.ProjectID.String() || client.explainRequest.GetTaskId().GetValue() != fixture.scope.TaskID.String() || client.explainRequest.GetGraphRevisionId().GetValue() != fixture.revisionID.String() || client.explainRequest.GetNodeId().GetValue() != fixture.secondNode.String() {
		t.Fatalf("explain request = %#v", client.explainRequest)
	}
}

type graphResourceProtoFixtureValue struct {
	view       *codefluxv1.GraphSliceView
	scope      graphResourceScope
	graphID    domain.GraphID
	revisionID domain.GraphRevisionID
	secondNode domain.NodeID
}

func graphResourceProtoFixture(t *testing.T) graphResourceProtoFixtureValue {
	t.Helper()
	projectID, _ := domain.NewProjectID()
	taskID, _ := domain.NewTaskID()
	graphID, _ := domain.NewGraphID()
	revisionID, _ := domain.NewGraphRevisionID()
	parentID, _ := domain.NewGraphRevisionID()
	firstNode, _ := domain.NewNodeID()
	secondNode, _ := domain.NewNodeID()
	edgeID, _ := domain.NewEdgeID()
	node := func(id domain.NodeID, class taskgraph.NodeClass, state taskgraph.NodeStatus, label string, rank uint64, x int64) *codefluxv1.GraphNodeView {
		return &codefluxv1.GraphNodeView{NodeId: graphNodeIdentity(id), Kind: string(class), State: string(state), Label: &codefluxv1.RedactedText{Value: label, OriginalBytes: uint64(len(label))}, ContractPurpose: &codefluxv1.RedactedText{Value: "Bounded purpose", OriginalBytes: 15}, Layout: &codefluxv1.GraphLayoutHintView{Algorithm: "layered-ltr", AlgorithmVersion: 1, XMilli: x, YMilli: 2000, WidthMilli: 240000, HeightMilli: 88000, Rank: rank}}
	}
	view := &codefluxv1.GraphSliceView{GraphId: &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH, Value: graphID.String()}, GraphRevisionId: graphRevisionIdentity(revisionID), ParentGraphRevisionId: graphRevisionIdentity(parentID), TaskId: graphTaskIdentity(taskID), Revision: 2, Mode: "evidence", GraphSchemaVersion: 1, ContentSha256: "fixture", LayoutAlgorithm: "layered-ltr", LayoutAlgorithmVersion: 1, CreatedAt: timestamppb.New(time.Date(2026, 7, 31, 19, 0, 0, 0, time.UTC)), ContinuationCursor: "next", Truncated: true, Nodes: []*codefluxv1.GraphNodeView{node(firstNode, taskgraph.NodeClassRequirement, taskgraph.NodeStatusActive, "Accepted intent", 0, 1000), node(secondNode, taskgraph.NodeClassArtifactResult, taskgraph.NodeStatusPassed, "Evidence", 1, 353000)}, Edges: []*codefluxv1.GraphEdgeView{{EdgeId: &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EDGE, Value: edgeID.String()}, FromNodeId: graphNodeIdentity(firstNode), ToNodeId: graphNodeIdentity(secondNode), Kind: string(taskgraph.EdgeClassEvidenceDependency)}}}
	return graphResourceProtoFixtureValue{view: view, scope: graphResourceScope{ProjectID: projectID, TaskID: taskID}, graphID: graphID, revisionID: revisionID, secondNode: secondNode}
}

type graphResourceClientStub struct {
	slice          *codefluxv1.GetGraphSliceResponse
	sliceRequest   *codefluxv1.GetGraphSliceRequest
	explanation    *codefluxv1.ExplainNodeResponse
	explainRequest *codefluxv1.ExplainNodeRequest
}

func (client *graphResourceClientStub) GetGraphSlice(_ context.Context, request *codefluxv1.GetGraphSliceRequest, _ ...grpc.CallOption) (*codefluxv1.GetGraphSliceResponse, error) {
	client.sliceRequest = request
	return client.slice, nil
}
func (*graphResourceClientStub) ExpandGraph(context.Context, *codefluxv1.ExpandGraphRequest, ...grpc.CallOption) (*codefluxv1.ExpandGraphResponse, error) {
	return nil, nil
}
func (*graphResourceClientStub) GetNode(context.Context, *codefluxv1.GetNodeRequest, ...grpc.CallOption) (*codefluxv1.GetNodeResponse, error) {
	return nil, nil
}
func (client *graphResourceClientStub) ExplainNode(_ context.Context, request *codefluxv1.ExplainNodeRequest, _ ...grpc.CallOption) (*codefluxv1.ExplainNodeResponse, error) {
	client.explainRequest = request
	return client.explanation, nil
}
func (*graphResourceClientStub) SearchGraph(context.Context, *codefluxv1.SearchGraphRequest, ...grpc.CallOption) (*codefluxv1.SearchGraphResponse, error) {
	return nil, nil
}
func (*graphResourceClientStub) CompareGraphRevisions(context.Context, *codefluxv1.CompareGraphRevisionsRequest, ...grpc.CallOption) (*codefluxv1.CompareGraphRevisionsResponse, error) {
	return nil, nil
}
