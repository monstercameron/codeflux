//go:build js && wasm

package main

import (
	"context"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func openBrowserGraphResourceClient(ctx context.Context) (graphResourceLease, error) {
	connection, err := grpctunnel.DialContext(ctx, sessionclient.BridgePath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return graphResourceLease{}, err
	}
	return graphResourceLease{client: codefluxv1.NewGraphServiceClient(connection), close: connection.Close}, nil
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

func decodeGraphComparisonEdge(value *codefluxv1.GraphEdgeView) (taskgraph.Edge, error) {
	if value == nil {
		return taskgraph.Edge{}, errGraphResourceMalformed
	}
	return decodeGraphEdgeValue(value)
}
