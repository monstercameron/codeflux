package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/graphlayout"
	"google.golang.org/grpc"
)

var (
	errGraphResourceScopeUnavailable  = errors.New("authoritative graph scope is unavailable")
	errGraphResourceBridgeUnavailable = errors.New("authoritative graph bridge is unavailable")
	errGraphResourceMalformed         = errors.New("authoritative graph resource is malformed")
)

type graphResourceScope struct {
	ProjectID domain.ProjectID
	TaskID    domain.TaskID
}

type graphResource struct {
	GraphID      domain.GraphID
	TaskID       domain.TaskID
	Revision     taskgraph.Revision
	Layout       graphlayout.Layout
	Mode         taskgraph.Mode
	Continuation string
	Truncated    bool
}

type graphNodeResource struct {
	Node            taskgraph.Node
	MessageIDs      []domain.MessageID
	SourceLocations []graphSourceLocationResource
	Layout          *graphlayout.Placement
}

type graphSourceLocationResource struct {
	RepositoryID       domain.RepositoryID
	RepositoryRevision string
	RelativePath       string
	StartLine          uint64
	StartColumn        uint64
	EndLine            uint64
	EndColumn          uint64
}

type graphChangeResource struct {
	Entity     string
	Kind       string
	StableID   string
	BeforeNode *taskgraph.Node
	AfterNode  *taskgraph.Node
	BeforeEdge *taskgraph.Edge
	AfterEdge  *taskgraph.Edge
}

type graphComparisonResource struct {
	FromRevisionID domain.GraphRevisionID
	ToRevisionID   domain.GraphRevisionID
	FromRevision   uint64
	ToRevision     uint64
	Changes        []graphChangeResource
	Continuation   string
	Truncated      bool
}

type graphResourceClient interface {
	GetGraphSlice(context.Context, *codefluxv1.GetGraphSliceRequest, ...grpc.CallOption) (*codefluxv1.GetGraphSliceResponse, error)
	ExpandGraph(context.Context, *codefluxv1.ExpandGraphRequest, ...grpc.CallOption) (*codefluxv1.ExpandGraphResponse, error)
	GetNode(context.Context, *codefluxv1.GetNodeRequest, ...grpc.CallOption) (*codefluxv1.GetNodeResponse, error)
	ExplainNode(context.Context, *codefluxv1.ExplainNodeRequest, ...grpc.CallOption) (*codefluxv1.ExplainNodeResponse, error)
	SearchGraph(context.Context, *codefluxv1.SearchGraphRequest, ...grpc.CallOption) (*codefluxv1.SearchGraphResponse, error)
	CompareGraphRevisions(context.Context, *codefluxv1.CompareGraphRevisionsRequest, ...grpc.CallOption) (*codefluxv1.CompareGraphRevisionsResponse, error)
}

type graphNodeExplanationResource struct {
	Node        graphNodeResource
	Explanation string
}

type graphResourceLease struct {
	client graphResourceClient
	close  func() error
}

type graphResourceClientOpener func(context.Context) (graphResourceLease, error)

func loadGraphResource(ctx context.Context, opener graphResourceClientOpener, scope graphResourceScope, mode taskgraph.Mode, maxNodes, maxEdges int, continuation string) (graphResource, error) {
	return loadGraphResourceAtRevision(ctx, opener, scope, mode, maxNodes, maxEdges, continuation, nil)
}

func loadGraphResourceAtRevision(ctx context.Context, opener graphResourceClientOpener, scope graphResourceScope, mode taskgraph.Mode, maxNodes, maxEdges int, continuation string, expectedRevision *domain.GraphRevisionID) (graphResource, error) {
	if err := validateGraphResourceInputs(opener, scope, mode); err != nil {
		return graphResource{}, err
	}
	lease, err := opener(ctx)
	if err != nil {
		return graphResource{}, err
	}
	if lease.client == nil || lease.close == nil {
		return graphResource{}, errGraphResourceBridgeUnavailable
	}
	defer lease.close()
	request := &codefluxv1.GetGraphSliceRequest{ProjectId: graphProjectIdentity(scope.ProjectID), TaskId: graphTaskIdentity(scope.TaskID), Mode: string(mode), MaxNodes: uint32(maxNodes), MaxEdges: uint32(maxEdges), ContinuationCursor: continuation}
	if expectedRevision != nil {
		if expectedRevision.IsZero() {
			return graphResource{}, errGraphResourceScopeUnavailable
		}
		request.ExpectedGraphRevisionId = graphRevisionIdentity(*expectedRevision)
	}
	response, err := lease.client.GetGraphSlice(ctx, request)
	if err != nil {
		return graphResource{}, err
	}
	return decodeGraphResource(response.GetGraph(), scope)
}

func expandGraphResource(ctx context.Context, opener graphResourceClientOpener, scope graphResourceScope, revisionID domain.GraphRevisionID, roots []domain.NodeID, mode taskgraph.Mode, traversal string, maxHops, maxNodes, maxEdges int, continuation string) (graphResource, error) {
	if err := validateGraphResourceInputs(opener, scope, mode); err != nil {
		return graphResource{}, err
	}
	if revisionID.IsZero() || len(roots) == 0 {
		return graphResource{}, errGraphResourceScopeUnavailable
	}
	lease, err := opener(ctx)
	if err != nil {
		return graphResource{}, err
	}
	if lease.client == nil || lease.close == nil {
		return graphResource{}, errGraphResourceBridgeUnavailable
	}
	defer lease.close()
	rootViews := make([]*codefluxv1.StableIdentity, 0, len(roots))
	for _, root := range roots {
		if root.IsZero() {
			return graphResource{}, errGraphResourceScopeUnavailable
		}
		rootViews = append(rootViews, graphNodeIdentity(root))
	}
	response, err := lease.client.ExpandGraph(ctx, &codefluxv1.ExpandGraphRequest{ProjectId: graphProjectIdentity(scope.ProjectID), TaskId: graphTaskIdentity(scope.TaskID), GraphRevisionId: graphRevisionIdentity(revisionID), RootNodeIds: rootViews, Mode: string(mode), Traversal: traversal, MaxHops: uint32(maxHops), MaxNodes: uint32(maxNodes), MaxEdges: uint32(maxEdges), ContinuationCursor: continuation})
	if err != nil {
		return graphResource{}, err
	}
	return decodeGraphResource(response.GetGraph(), scope)
}

func searchGraphResource(ctx context.Context, opener graphResourceClientOpener, scope graphResourceScope, revisionID domain.GraphRevisionID, mode taskgraph.Mode, query string, maxResults int, continuation string) (graphResource, error) {
	if err := validateGraphResourceInputs(opener, scope, mode); err != nil {
		return graphResource{}, err
	}
	if revisionID.IsZero() || strings.TrimSpace(query) == "" {
		return graphResource{}, errGraphResourceScopeUnavailable
	}
	lease, err := opener(ctx)
	if err != nil {
		return graphResource{}, err
	}
	if lease.client == nil || lease.close == nil {
		return graphResource{}, errGraphResourceBridgeUnavailable
	}
	defer lease.close()
	response, err := lease.client.SearchGraph(ctx, &codefluxv1.SearchGraphRequest{ProjectId: graphProjectIdentity(scope.ProjectID), TaskId: graphTaskIdentity(scope.TaskID), GraphRevisionId: graphRevisionIdentity(revisionID), Mode: string(mode), Query: strings.TrimSpace(query), MaxResults: uint32(maxResults), ContinuationCursor: continuation})
	if err != nil {
		return graphResource{}, err
	}
	return decodeGraphResource(response.GetGraph(), scope)
}

func loadGraphNodeResource(ctx context.Context, opener graphResourceClientOpener, scope graphResourceScope, revisionID domain.GraphRevisionID, nodeID domain.NodeID) (graphNodeResource, error) {
	if opener == nil || scope.ProjectID.IsZero() || scope.TaskID.IsZero() || revisionID.IsZero() || nodeID.IsZero() {
		return graphNodeResource{}, errGraphResourceScopeUnavailable
	}
	lease, err := opener(ctx)
	if err != nil {
		return graphNodeResource{}, err
	}
	if lease.client == nil || lease.close == nil {
		return graphNodeResource{}, errGraphResourceBridgeUnavailable
	}
	defer lease.close()
	response, err := lease.client.GetNode(ctx, &codefluxv1.GetNodeRequest{ProjectId: graphProjectIdentity(scope.ProjectID), TaskId: graphTaskIdentity(scope.TaskID), GraphRevisionId: graphRevisionIdentity(revisionID), NodeId: graphNodeIdentity(nodeID)})
	if err != nil {
		return graphNodeResource{}, err
	}
	node, err := decodeGraphNode(response.GetNode())
	if err != nil {
		return graphNodeResource{}, err
	}
	result := graphNodeResource{Node: node}
	for _, raw := range response.GetNode().GetRelatedMessageIds() {
		id, parseErr := decodeGraphMessageID(raw)
		if parseErr != nil {
			return graphNodeResource{}, parseErr
		}
		result.MessageIDs = append(result.MessageIDs, id)
	}
	for _, raw := range response.GetNode().GetSourceLocations() {
		location, parseErr := decodeGraphSourceLocation(raw)
		if parseErr != nil {
			return graphNodeResource{}, parseErr
		}
		result.SourceLocations = append(result.SourceLocations, location)
	}
	if raw := response.GetNode().GetLayout(); raw != nil {
		placement, parseErr := decodeGraphPlacement(response.GetNode().GetNodeId(), raw)
		if parseErr != nil {
			return graphNodeResource{}, parseErr
		}
		result.Layout = &placement
	}
	return result, nil
}

func explainGraphNodeResource(ctx context.Context, opener graphResourceClientOpener, scope graphResourceScope, revisionID domain.GraphRevisionID, nodeID domain.NodeID) (graphNodeExplanationResource, error) {
	if opener == nil || scope.ProjectID.IsZero() || scope.TaskID.IsZero() || revisionID.IsZero() || nodeID.IsZero() {
		return graphNodeExplanationResource{}, errGraphResourceScopeUnavailable
	}
	lease, err := opener(ctx)
	if err != nil {
		return graphNodeExplanationResource{}, err
	}
	if lease.client == nil || lease.close == nil {
		return graphNodeExplanationResource{}, errGraphResourceBridgeUnavailable
	}
	defer lease.close()
	response, err := lease.client.ExplainNode(ctx, &codefluxv1.ExplainNodeRequest{ProjectId: graphProjectIdentity(scope.ProjectID), TaskId: graphTaskIdentity(scope.TaskID), GraphRevisionId: graphRevisionIdentity(revisionID), NodeId: graphNodeIdentity(nodeID)})
	if err != nil {
		return graphNodeExplanationResource{}, err
	}
	if response.GetExplanation() == nil || strings.TrimSpace(response.GetExplanation().GetValue()) == "" {
		return graphNodeExplanationResource{}, errGraphResourceMalformed
	}
	node, err := decodeGraphNode(response.GetNode())
	if err != nil {
		return graphNodeExplanationResource{}, err
	}
	result := graphNodeResource{Node: node}
	for _, raw := range response.GetNode().GetRelatedMessageIds() {
		id, parseErr := decodeGraphMessageID(raw)
		if parseErr != nil {
			return graphNodeExplanationResource{}, parseErr
		}
		result.MessageIDs = append(result.MessageIDs, id)
	}
	for _, raw := range response.GetNode().GetSourceLocations() {
		location, parseErr := decodeGraphSourceLocation(raw)
		if parseErr != nil {
			return graphNodeExplanationResource{}, parseErr
		}
		result.SourceLocations = append(result.SourceLocations, location)
	}
	return graphNodeExplanationResource{Node: result, Explanation: response.GetExplanation().GetValue()}, nil
}

func compareGraphResources(ctx context.Context, opener graphResourceClientOpener, scope graphResourceScope, from, to domain.GraphRevisionID, maxChanges int, continuation string) (graphComparisonResource, error) {
	if opener == nil || scope.ProjectID.IsZero() || scope.TaskID.IsZero() || from.IsZero() || to.IsZero() {
		return graphComparisonResource{}, errGraphResourceScopeUnavailable
	}
	lease, err := opener(ctx)
	if err != nil {
		return graphComparisonResource{}, err
	}
	if lease.client == nil || lease.close == nil {
		return graphComparisonResource{}, errGraphResourceBridgeUnavailable
	}
	defer lease.close()
	response, err := lease.client.CompareGraphRevisions(ctx, &codefluxv1.CompareGraphRevisionsRequest{ProjectId: graphProjectIdentity(scope.ProjectID), TaskId: graphTaskIdentity(scope.TaskID), FromGraphRevisionId: graphRevisionIdentity(from), ToGraphRevisionId: graphRevisionIdentity(to), MaxChanges: uint32(maxChanges), ContinuationCursor: continuation})
	if err != nil {
		return graphComparisonResource{}, err
	}
	result := graphComparisonResource{FromRevisionID: from, ToRevisionID: to, FromRevision: response.GetFromRevision(), ToRevision: response.GetToRevision(), Continuation: response.GetContinuationCursor(), Truncated: response.GetTruncated()}
	for _, raw := range response.GetChanges() {
		if raw == nil || strings.TrimSpace(raw.GetEntityKind()) == "" || strings.TrimSpace(raw.GetChangeKind()) == "" || strings.TrimSpace(raw.GetStableId()) == "" {
			return graphComparisonResource{}, errGraphResourceMalformed
		}
		change := graphChangeResource{Entity: raw.GetEntityKind(), Kind: raw.GetChangeKind(), StableID: raw.GetStableId()}
		if raw.GetBeforeNode() != nil {
			value, parseErr := decodeGraphNode(raw.GetBeforeNode())
			if parseErr != nil {
				return graphComparisonResource{}, parseErr
			}
			change.BeforeNode = &value
		}
		if raw.GetAfterNode() != nil {
			value, parseErr := decodeGraphNode(raw.GetAfterNode())
			if parseErr != nil {
				return graphComparisonResource{}, parseErr
			}
			change.AfterNode = &value
		}
		if raw.GetBeforeEdge() != nil {
			value, parseErr := decodeGraphComparisonEdge(raw.GetBeforeEdge())
			if parseErr != nil {
				return graphComparisonResource{}, parseErr
			}
			change.BeforeEdge = &value
		}
		if raw.GetAfterEdge() != nil {
			value, parseErr := decodeGraphComparisonEdge(raw.GetAfterEdge())
			if parseErr != nil {
				return graphComparisonResource{}, parseErr
			}
			change.AfterEdge = &value
		}
		result.Changes = append(result.Changes, change)
	}
	return result, nil
}

func validateGraphResourceInputs(opener graphResourceClientOpener, scope graphResourceScope, mode taskgraph.Mode) error {
	if opener == nil {
		return errGraphResourceBridgeUnavailable
	}
	if scope.ProjectID.IsZero() || scope.TaskID.IsZero() || !mode.IsValid() {
		return errGraphResourceScopeUnavailable
	}
	return nil
}

func decodeGraphResource(value *codefluxv1.GraphSliceView, expected graphResourceScope) (graphResource, error) {
	if value == nil || value.GetTaskId() == nil || value.GetGraphId() == nil || value.GetGraphRevisionId() == nil || value.GetCreatedAt() == nil {
		return graphResource{}, errGraphResourceMalformed
	}
	graphID, err := decodeGraphID(value.GetGraphId())
	if err != nil {
		return graphResource{}, err
	}
	taskID, err := decodeGraphTaskID(value.GetTaskId())
	if err != nil || taskID != expected.TaskID {
		return graphResource{}, errGraphResourceMalformed
	}
	revisionID, err := decodeGraphRevisionID(value.GetGraphRevisionId())
	if err != nil {
		return graphResource{}, err
	}
	if err := value.GetCreatedAt().CheckValid(); err != nil {
		return graphResource{}, errGraphResourceMalformed
	}
	var parentID *domain.GraphRevisionID
	if value.GetParentGraphRevisionId() != nil {
		parsed, parseErr := decodeGraphRevisionID(value.GetParentGraphRevisionId())
		if parseErr != nil {
			return graphResource{}, parseErr
		}
		parentID = &parsed
	}
	sources, _ := taskgraph.NewSourceLinks(nil, nil)
	metadata, err := taskgraph.NewRevisionMetadata(revisionID, graphID, value.GetRevision(), parentID, taskgraph.SchemaVersion(value.GetGraphSchemaVersion()), value.GetCreatedAt().AsTime().UTC(), sources)
	if err != nil {
		return graphResource{}, fmt.Errorf("%w: revision metadata", errGraphResourceMalformed)
	}
	nodes := make([]taskgraph.Node, 0, len(value.GetNodes()))
	for _, raw := range value.GetNodes() {
		node, parseErr := decodeGraphNode(raw)
		if parseErr != nil {
			return graphResource{}, parseErr
		}
		nodes = append(nodes, node)
	}
	edges := make([]taskgraph.Edge, 0, len(value.GetEdges()))
	for _, raw := range value.GetEdges() {
		edge, parseErr := decodeGraphEdge(raw)
		if parseErr != nil {
			return graphResource{}, parseErr
		}
		edges = append(edges, edge)
	}
	revision, err := taskgraph.NewRevision(metadata, nodes, edges)
	if err != nil {
		return graphResource{}, fmt.Errorf("%w: graph revision", errGraphResourceMalformed)
	}
	mode := taskgraph.Mode(value.GetMode())
	if !mode.IsValid() {
		return graphResource{}, errGraphResourceMalformed
	}
	layout, err := decodeGraphLayout(value, revision)
	if err != nil {
		return graphResource{}, err
	}
	return graphResource{GraphID: graphID, TaskID: taskID, Revision: revision, Layout: layout, Mode: mode, Continuation: value.GetContinuationCursor(), Truncated: value.GetTruncated()}, nil
}

func decodeGraphNode(value *codefluxv1.GraphNodeView) (taskgraph.Node, error) {
	if value == nil || value.GetTombstoned() && value.GetState() != string(taskgraph.NodeStatusInvalidated) {
		return taskgraph.Node{}, errGraphResourceMalformed
	}
	nodeID, err := decodeGraphNodeID(value.GetNodeId())
	if err != nil {
		return taskgraph.Node{}, err
	}
	contract, err := taskgraph.NewContractSummary(redactedValue(value.GetContractPurpose()), redactedValues(value.GetContractInputs()), redactedValues(value.GetContractOutputs()), redactedValues(value.GetContractEffects()))
	if err != nil {
		return taskgraph.Node{}, errGraphResourceMalformed
	}
	sources, err := decodeGraphSources(value.GetEventIds(), value.GetPlanSteps())
	if err != nil {
		return taskgraph.Node{}, err
	}
	node, err := taskgraph.NewNode(nodeID, taskgraph.NodeClass(value.GetKind()), taskgraph.NodeStatus(value.GetState()), redactedValue(value.GetLabel()), contract, sources)
	if err != nil {
		return taskgraph.Node{}, errGraphResourceMalformed
	}
	return node, nil
}

func decodeGraphEdge(value *codefluxv1.GraphEdgeView) (taskgraph.Edge, error) {
	if value == nil || value.GetTombstoned() {
		return taskgraph.Edge{}, errGraphResourceMalformed
	}
	return decodeGraphEdgeValue(value)
}

func decodeGraphComparisonEdge(value *codefluxv1.GraphEdgeView) (taskgraph.Edge, error) {
	if value == nil {
		return taskgraph.Edge{}, errGraphResourceMalformed
	}
	return decodeGraphEdgeValue(value)
}

func decodeGraphEdgeValue(value *codefluxv1.GraphEdgeView) (taskgraph.Edge, error) {
	edgeID, err := decodeGraphEdgeID(value.GetEdgeId())
	if err != nil {
		return taskgraph.Edge{}, err
	}
	from, err := decodeGraphNodeID(value.GetFromNodeId())
	if err != nil {
		return taskgraph.Edge{}, err
	}
	to, err := decodeGraphNodeID(value.GetToNodeId())
	if err != nil {
		return taskgraph.Edge{}, err
	}
	sources, err := decodeGraphSources(value.GetEventIds(), value.GetPlanSteps())
	if err != nil {
		return taskgraph.Edge{}, err
	}
	edge, err := taskgraph.NewEdge(edgeID, taskgraph.EdgeClass(value.GetKind()), from, to, sources)
	if err != nil {
		return taskgraph.Edge{}, errGraphResourceMalformed
	}
	return edge, nil
}

func decodeGraphSources(eventViews []*codefluxv1.StableIdentity, stepViews []*codefluxv1.GraphPlanStepLinkView) (taskgraph.SourceLinks, error) {
	events := make([]domain.EventID, 0, len(eventViews))
	for _, raw := range eventViews {
		id, err := decodeGraphEventID(raw)
		if err != nil {
			return taskgraph.SourceLinks{}, err
		}
		events = append(events, id)
	}
	steps := make([]taskgraph.PlanStepLink, 0, len(stepViews))
	for _, raw := range stepViews {
		if raw == nil {
			return taskgraph.SourceLinks{}, errGraphResourceMalformed
		}
		steps = append(steps, taskgraph.PlanStepLink{PlanRevision: raw.GetPlanRevision(), StepID: raw.GetStepId()})
	}
	value, err := taskgraph.NewSourceLinks(events, steps)
	if err != nil {
		return taskgraph.SourceLinks{}, errGraphResourceMalformed
	}
	return value, nil
}

func decodeGraphLayout(value *codefluxv1.GraphSliceView, revision taskgraph.Revision) (graphlayout.Layout, error) {
	input := graphlayout.Input{}
	for _, node := range revision.Nodes() {
		input.Nodes = append(input.Nodes, graphlayout.Node{ID: node.ID(), Class: node.Class()})
	}
	for _, edge := range revision.Edges() {
		input.Edges = append(input.Edges, graphlayout.Edge{ID: edge.ID(), FromNode: edge.FromNode(), ToNode: edge.ToNode()})
	}
	computed, err := graphlayout.Compute(input)
	if err != nil {
		return graphlayout.Layout{}, errGraphResourceMalformed
	}
	placements := make(map[domain.NodeID]graphlayout.Placement, len(computed.Nodes))
	for _, placement := range computed.Nodes {
		placements[placement.NodeID] = placement
	}
	for _, raw := range value.GetNodes() {
		if raw.GetLayout() == nil {
			continue
		}
		placement, parseErr := decodeGraphPlacement(raw.GetNodeId(), raw.GetLayout())
		if parseErr != nil {
			return graphlayout.Layout{}, parseErr
		}
		placements[placement.NodeID] = placement
	}
	computed.Nodes = computed.Nodes[:0]
	computed.Components = computed.Components[:0]
	for _, node := range revision.Nodes() {
		placement := placements[node.ID()]
		computed.Nodes = append(computed.Nodes, placement)
		computed.Components = append(computed.Components, graphlayout.Component{Key: placement.ComponentKey, Members: []domain.NodeID{node.ID()}, Rank: placement.Rank})
	}
	sort.Slice(computed.Nodes, func(i, j int) bool { return computed.Nodes[i].NodeID.String() < computed.Nodes[j].NodeID.String() })
	sort.Slice(computed.Components, func(i, j int) bool { return computed.Components[i].Key.String() < computed.Components[j].Key.String() })
	computed.AlgorithmVersion = value.GetLayoutAlgorithm() + "-v" + fmt.Sprint(value.GetLayoutAlgorithmVersion())
	if value.GetLayoutAlgorithm() == "" {
		computed.AlgorithmVersion = graphlayout.AlgorithmVersion
	}
	computed.Bounds = graphLayoutBounds(computed.Nodes)
	return computed, nil
}

func decodeGraphPlacement(identity *codefluxv1.StableIdentity, value *codefluxv1.GraphLayoutHintView) (graphlayout.Placement, error) {
	id, err := decodeGraphNodeID(identity)
	if err != nil || value == nil || value.GetWidthMilli() == 0 || value.GetHeightMilli() == 0 {
		return graphlayout.Placement{}, errGraphResourceMalformed
	}
	return graphlayout.Placement{NodeID: id, ComponentKey: id, Rank: int(value.GetRank()), Order: int(value.GetSiblingOrder()), Bounds: graphlayout.Rect{X: milliToLayout(value.GetXMilli()), Y: milliToLayout(value.GetYMilli()), Width: milliToLayout(int64(value.GetWidthMilli())), Height: milliToLayout(int64(value.GetHeightMilli()))}}, nil
}

func graphLayoutBounds(nodes []graphlayout.Placement) graphlayout.Rect {
	if len(nodes) == 0 {
		return graphlayout.Rect{}
	}
	minX, minY := nodes[0].Bounds.X, nodes[0].Bounds.Y
	maxX, maxY := minX+nodes[0].Bounds.Width, minY+nodes[0].Bounds.Height
	for _, node := range nodes[1:] {
		minX = min(minX, node.Bounds.X)
		minY = min(minY, node.Bounds.Y)
		maxX = max(maxX, node.Bounds.X+node.Bounds.Width)
		maxY = max(maxY, node.Bounds.Y+node.Bounds.Height)
	}
	return graphlayout.Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
}

func milliToLayout(value int64) int64 {
	if value >= 0 {
		return (value + 500) / 1000
	}
	return (value - 500) / 1000
}

func decodeGraphSourceLocation(value *codefluxv1.GraphSourceLocationView) (graphSourceLocationResource, error) {
	if value == nil || value.GetRepositoryRelativePath() == nil {
		return graphSourceLocationResource{}, errGraphResourceMalformed
	}
	repositoryID, err := decodeGraphRepositoryID(value.GetRepositoryId())
	if err != nil {
		return graphSourceLocationResource{}, err
	}
	return graphSourceLocationResource{RepositoryID: repositoryID, RepositoryRevision: value.GetRepositoryRevision(), RelativePath: value.GetRepositoryRelativePath().GetWorkspaceRelativeSlashPath(), StartLine: uint64(value.GetStartLine()), StartColumn: uint64(value.GetStartColumn()), EndLine: uint64(value.GetEndLine()), EndColumn: uint64(value.GetEndColumn())}, nil
}

func redactedValue(value *codefluxv1.RedactedText) string {
	if value == nil {
		return ""
	}
	return value.GetValue()
}
func redactedValues(values []*codefluxv1.RedactedText) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			return nil
		}
		result = append(result, value.GetValue())
	}
	return result
}

func graphProjectIdentity(value domain.ProjectID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROJECT, Value: value.String()}
}
func graphTaskIdentity(value domain.TaskID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: value.String()}
}
func graphRevisionIdentity(value domain.GraphRevisionID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH_REVISION, Value: value.String()}
}
func graphNodeIdentity(value domain.NodeID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_NODE, Value: value.String()}
}

func decodeGraphID(value *codefluxv1.StableIdentity) (domain.GraphID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH {
		return domain.GraphID{}, errGraphResourceMalformed
	}
	return domain.ParseGraphID(value.GetValue())
}
func decodeGraphRevisionID(value *codefluxv1.StableIdentity) (domain.GraphRevisionID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH_REVISION {
		return domain.GraphRevisionID{}, errGraphResourceMalformed
	}
	return domain.ParseGraphRevisionID(value.GetValue())
}
func decodeGraphNodeID(value *codefluxv1.StableIdentity) (domain.NodeID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_NODE {
		return domain.NodeID{}, errGraphResourceMalformed
	}
	return domain.ParseNodeID(value.GetValue())
}
func decodeGraphEdgeID(value *codefluxv1.StableIdentity) (domain.EdgeID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EDGE {
		return domain.EdgeID{}, errGraphResourceMalformed
	}
	return domain.ParseEdgeID(value.GetValue())
}
func decodeGraphTaskID(value *codefluxv1.StableIdentity) (domain.TaskID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK {
		return domain.TaskID{}, errGraphResourceMalformed
	}
	return domain.ParseTaskID(value.GetValue())
}
func decodeGraphRepositoryID(value *codefluxv1.StableIdentity) (domain.RepositoryID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY {
		return domain.RepositoryID{}, errGraphResourceMalformed
	}
	return domain.ParseRepositoryID(value.GetValue())
}
func decodeGraphEventID(value *codefluxv1.StableIdentity) (domain.EventID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVENT {
		return domain.EventID{}, errGraphResourceMalformed
	}
	return domain.ParseEventID(value.GetValue())
}
func decodeGraphMessageID(value *codefluxv1.StableIdentity) (domain.MessageID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE {
		return domain.MessageID{}, errGraphResourceMalformed
	}
	return domain.ParseMessageID(value.GetValue())
}
