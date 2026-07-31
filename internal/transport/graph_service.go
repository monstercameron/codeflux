package transport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrGraphQueryNotFound     = errors.New("graph query target not found")
	ErrGraphQueryStale        = errors.New("graph query revision is stale")
	ErrGraphQueryInvalid      = errors.New("graph query is invalid")
	ErrGraphQueryContinuation = errors.New("graph continuation is invalid")
)

type GraphQueryScope struct {
	ProjectID domain.ProjectID
	TaskID    domain.TaskID
}

type GraphLayoutSelection struct {
	Algorithm string
	Version   uint64
}

type GraphLayoutHintView struct {
	Algorithm    string
	Version      uint64
	XMilli       int64
	YMilli       int64
	WidthMilli   uint64
	HeightMilli  uint64
	Rank         uint64
	SiblingOrder uint64
}

type GraphSourceLocationView struct {
	RepositoryID       domain.RepositoryID
	RepositoryRevision string
	RelativePath       string
	StartLine          uint64
	StartColumn        uint64
	EndLine            uint64
	EndColumn          uint64
}

type GraphNodeQueryView struct {
	Node            taskgraph.Node
	Tombstoned      bool
	MessageIDs      []domain.MessageID
	SourceLocations []GraphSourceLocationView
	Layout          *GraphLayoutHintView
}

type GraphEdgeQueryView struct {
	Edge       taskgraph.Edge
	Tombstoned bool
}

type GraphRevisionHeaderView struct {
	GraphID         domain.GraphID
	RevisionID      domain.GraphRevisionID
	Ordinal         uint64
	ParentID        *domain.GraphRevisionID
	SchemaVersion   taskgraph.SchemaVersion
	CreatedAt       time.Time
	ContentSHA256   string
	LayoutAlgorithm string
	LayoutVersion   uint64
}

type GraphSliceQuery struct {
	Scope              GraphQueryScope
	ExpectedRevisionID *domain.GraphRevisionID
	Mode               taskgraph.Mode
	MaxNodes           int
	MaxEdges           int
	Continuation       string
	Layout             GraphLayoutSelection
}

type GraphNodeQuery struct {
	Scope      GraphQueryScope
	RevisionID domain.GraphRevisionID
	NodeID     domain.NodeID
	Layout     GraphLayoutSelection
}

type GraphExpansionQuery struct {
	Scope        GraphQueryScope
	RevisionID   domain.GraphRevisionID
	RootNodeIDs  []domain.NodeID
	Mode         taskgraph.Mode
	Traversal    string
	MaxHops      int
	MaxNodes     int
	MaxEdges     int
	Continuation string
	Layout       GraphLayoutSelection
}

type GraphSearchQuery struct {
	Scope        GraphQueryScope
	RevisionID   domain.GraphRevisionID
	Mode         taskgraph.Mode
	Text         string
	MaxResults   int
	Continuation string
	Layout       GraphLayoutSelection
}

type GraphSliceQueryView struct {
	TaskID       domain.TaskID
	Header       GraphRevisionHeaderView
	Mode         taskgraph.Mode
	Nodes        []GraphNodeQueryView
	Edges        []GraphEdgeQueryView
	Continuation string
}

type GraphComparisonQuery struct {
	Scope          GraphQueryScope
	FromRevisionID domain.GraphRevisionID
	ToRevisionID   domain.GraphRevisionID
	MaxChanges     int
	Continuation   string
}

type GraphRevisionChangeView struct {
	Entity     string
	Kind       string
	ID         string
	BeforeNode *GraphNodeQueryView
	AfterNode  *GraphNodeQueryView
	BeforeEdge *GraphEdgeQueryView
	AfterEdge  *GraphEdgeQueryView
}

type GraphComparisonQueryView struct {
	From         GraphRevisionHeaderView
	To           GraphRevisionHeaderView
	Changes      []GraphRevisionChangeView
	Continuation string
}

type GraphQueryApplication interface {
	GetGraphSlice(context.Context, GraphSliceQuery) (GraphSliceQueryView, error)
	ExpandGraph(context.Context, GraphExpansionQuery) (GraphSliceQueryView, error)
	GetGraphNode(context.Context, GraphNodeQuery) (GraphNodeQueryView, error)
	SearchGraph(context.Context, GraphSearchQuery) (GraphSliceQueryView, error)
	CompareGraphRevisions(context.Context, GraphComparisonQuery) (GraphComparisonQueryView, error)
}

type GraphService struct {
	codefluxv1.UnimplementedGraphServiceServer
	application GraphQueryApplication
}

func NewGraphService(application GraphQueryApplication) (*GraphService, error) {
	if application == nil {
		return nil, errors.New("graph query application is required")
	}
	return &GraphService{application: application}, nil
}

func (service *GraphService) GetGraphSlice(ctx context.Context, request *codefluxv1.GetGraphSliceRequest) (*codefluxv1.GetGraphSliceResponse, error) {
	query, err := graphSliceQuery(request)
	if err != nil {
		return nil, err
	}
	view, err := service.application.GetGraphSlice(ctx, query)
	if err != nil {
		return nil, mapGraphQueryError(err)
	}
	graph, err := graphSliceViewToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.GetGraphSliceResponse{Graph: graph}, nil
}

func (service *GraphService) ExpandGraph(ctx context.Context, request *codefluxv1.ExpandGraphRequest) (*codefluxv1.ExpandGraphResponse, error) {
	query, err := graphExpansionQuery(request)
	if err != nil {
		return nil, err
	}
	view, err := service.application.ExpandGraph(ctx, query)
	if err != nil {
		return nil, mapGraphQueryError(err)
	}
	graph, err := graphSliceViewToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.ExpandGraphResponse{Graph: graph}, nil
}

func (service *GraphService) GetNode(ctx context.Context, request *codefluxv1.GetNodeRequest) (*codefluxv1.GetNodeResponse, error) {
	query, err := graphNodeQuery(request)
	if err != nil {
		return nil, err
	}
	view, err := service.application.GetGraphNode(ctx, query)
	if err != nil {
		return nil, mapGraphQueryError(err)
	}
	node, err := graphNodeViewToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.GetNodeResponse{Node: node, RelatedMessageIds: node.RelatedMessageIds}, nil
}

func (service *GraphService) SearchGraph(ctx context.Context, request *codefluxv1.SearchGraphRequest) (*codefluxv1.SearchGraphResponse, error) {
	query, err := graphSearchQuery(request)
	if err != nil {
		return nil, err
	}
	view, err := service.application.SearchGraph(ctx, query)
	if err != nil {
		return nil, mapGraphQueryError(err)
	}
	graph, err := graphSliceViewToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.SearchGraphResponse{Graph: graph}, nil
}

func (service *GraphService) ExplainNode(ctx context.Context, request *codefluxv1.ExplainNodeRequest) (*codefluxv1.ExplainNodeResponse, error) {
	if request == nil {
		return nil, invalidGraphField("request", "is required")
	}
	query, err := graphNodeQuery(&codefluxv1.GetNodeRequest{GraphRevisionId: request.GetGraphRevisionId(), NodeId: request.GetNodeId(), ProjectId: request.GetProjectId(), TaskId: request.GetTaskId(), LayoutAlgorithm: request.GetLayoutAlgorithm(), LayoutAlgorithmVersion: request.GetLayoutAlgorithmVersion()})
	if err != nil {
		return nil, err
	}
	view, err := service.application.GetGraphNode(ctx, query)
	if err != nil {
		return nil, mapGraphQueryError(err)
	}
	node, err := graphNodeViewToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.ExplainNodeResponse{Node: node, Explanation: redactedGraphText(graphNodeExplanation(view))}, nil
}

func graphNodeExplanation(view GraphNodeQueryView) string {
	contract := view.Node.Contract()
	text := fmt.Sprintf("%s is a %s node with %s status.", view.Node.DisplayName(), view.Node.Class(), view.Node.Status())
	if purpose := strings.TrimSpace(contract.Purpose()); purpose != "" {
		text += " Purpose: " + purpose
	}
	text += fmt.Sprintf(" The bounded contract records %d inputs, %d outputs, and %d effects.", len(contract.Inputs()), len(contract.Outputs()), len(contract.Effects()))
	if view.Tombstoned {
		text += " This historical node is invalidated in this graph revision."
	}
	return text
}

func (service *GraphService) CompareGraphRevisions(ctx context.Context, request *codefluxv1.CompareGraphRevisionsRequest) (*codefluxv1.CompareGraphRevisionsResponse, error) {
	query, err := graphComparisonQuery(request)
	if err != nil {
		return nil, err
	}
	view, err := service.application.CompareGraphRevisions(ctx, query)
	if err != nil {
		return nil, mapGraphQueryError(err)
	}
	response, err := graphComparisonViewToProto(view)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func graphSliceQuery(request *codefluxv1.GetGraphSliceRequest) (GraphSliceQuery, error) {
	if request == nil {
		return GraphSliceQuery{}, invalidGraphField("request", "is required")
	}
	scope, err := graphScope(request.GetProjectId(), request.GetTaskId())
	if err != nil {
		return GraphSliceQuery{}, err
	}
	query := GraphSliceQuery{Scope: scope, Mode: taskgraph.Mode(request.GetMode()), MaxNodes: int(request.GetMaxNodes()), MaxEdges: int(request.GetMaxEdges()), Continuation: request.GetContinuationCursor(), Layout: GraphLayoutSelection{Algorithm: request.GetLayoutAlgorithm(), Version: request.GetLayoutAlgorithmVersion()}}
	if value := request.GetExpectedGraphRevisionId(); value != nil {
		parsed, parseErr := GraphRevisionIDFromProto(value)
		if parseErr != nil {
			return GraphSliceQuery{}, parseErr
		}
		query.ExpectedRevisionID = &parsed
	}
	if !query.Mode.IsValid() {
		return GraphSliceQuery{}, invalidGraphField("mode", "is not supported")
	}
	if err := validateTransportGraphLayout(query.Layout); err != nil {
		return GraphSliceQuery{}, err
	}
	return query, nil
}

func graphExpansionQuery(request *codefluxv1.ExpandGraphRequest) (GraphExpansionQuery, error) {
	if request == nil {
		return GraphExpansionQuery{}, invalidGraphField("request", "is required")
	}
	scope, err := graphScope(request.GetProjectId(), request.GetTaskId())
	if err != nil {
		return GraphExpansionQuery{}, err
	}
	revisionID, err := GraphRevisionIDFromProto(request.GetGraphRevisionId())
	if err != nil {
		return GraphExpansionQuery{}, err
	}
	roots := make([]domain.NodeID, 0, len(request.GetRootNodeIds()))
	for _, raw := range request.GetRootNodeIds() {
		id, parseErr := NodeIDFromProto(raw)
		if parseErr != nil {
			return GraphExpansionQuery{}, parseErr
		}
		roots = append(roots, id)
	}
	query := GraphExpansionQuery{Scope: scope, RevisionID: revisionID, RootNodeIDs: roots, Mode: taskgraph.Mode(request.GetMode()), Traversal: request.GetTraversal(), MaxHops: int(request.GetMaxHops()), MaxNodes: int(request.GetMaxNodes()), MaxEdges: int(request.GetMaxEdges()), Continuation: request.GetContinuationCursor(), Layout: GraphLayoutSelection{Algorithm: request.GetLayoutAlgorithm(), Version: request.GetLayoutAlgorithmVersion()}}
	if !query.Mode.IsValid() {
		return GraphExpansionQuery{}, invalidGraphField("mode", "is not supported")
	}
	if err := validateTransportGraphLayout(query.Layout); err != nil {
		return GraphExpansionQuery{}, err
	}
	return query, nil
}

func graphNodeQuery(request *codefluxv1.GetNodeRequest) (GraphNodeQuery, error) {
	if request == nil {
		return GraphNodeQuery{}, invalidGraphField("request", "is required")
	}
	scope, err := graphScope(request.GetProjectId(), request.GetTaskId())
	if err != nil {
		return GraphNodeQuery{}, err
	}
	revisionID, err := GraphRevisionIDFromProto(request.GetGraphRevisionId())
	if err != nil {
		return GraphNodeQuery{}, err
	}
	nodeID, err := NodeIDFromProto(request.GetNodeId())
	if err != nil {
		return GraphNodeQuery{}, err
	}
	query := GraphNodeQuery{Scope: scope, RevisionID: revisionID, NodeID: nodeID, Layout: GraphLayoutSelection{Algorithm: request.GetLayoutAlgorithm(), Version: request.GetLayoutAlgorithmVersion()}}
	if err := validateTransportGraphLayout(query.Layout); err != nil {
		return GraphNodeQuery{}, err
	}
	return query, nil
}

func graphSearchQuery(request *codefluxv1.SearchGraphRequest) (GraphSearchQuery, error) {
	if request == nil {
		return GraphSearchQuery{}, invalidGraphField("request", "is required")
	}
	scope, err := graphScope(request.GetProjectId(), request.GetTaskId())
	if err != nil {
		return GraphSearchQuery{}, err
	}
	revisionID, err := GraphRevisionIDFromProto(request.GetGraphRevisionId())
	if err != nil {
		return GraphSearchQuery{}, err
	}
	query := GraphSearchQuery{Scope: scope, RevisionID: revisionID, Mode: taskgraph.Mode(request.GetMode()), Text: strings.TrimSpace(request.GetQuery()), MaxResults: int(request.GetMaxResults()), Continuation: request.GetContinuationCursor(), Layout: GraphLayoutSelection{Algorithm: request.GetLayoutAlgorithm(), Version: request.GetLayoutAlgorithmVersion()}}
	if !query.Mode.IsValid() {
		return GraphSearchQuery{}, invalidGraphField("mode", "is not supported")
	}
	if query.Text == "" {
		return GraphSearchQuery{}, invalidGraphField("query", "must not be empty")
	}
	if err := validateTransportGraphLayout(query.Layout); err != nil {
		return GraphSearchQuery{}, err
	}
	return query, nil
}

func graphComparisonQuery(request *codefluxv1.CompareGraphRevisionsRequest) (GraphComparisonQuery, error) {
	if request == nil {
		return GraphComparisonQuery{}, invalidGraphField("request", "is required")
	}
	scope, err := graphScope(request.GetProjectId(), request.GetTaskId())
	if err != nil {
		return GraphComparisonQuery{}, err
	}
	from, err := GraphRevisionIDFromProto(request.GetFromGraphRevisionId())
	if err != nil {
		return GraphComparisonQuery{}, err
	}
	to, err := GraphRevisionIDFromProto(request.GetToGraphRevisionId())
	if err != nil {
		return GraphComparisonQuery{}, err
	}
	return GraphComparisonQuery{Scope: scope, FromRevisionID: from, ToRevisionID: to, MaxChanges: int(request.GetMaxChanges()), Continuation: request.GetContinuationCursor()}, nil
}

func graphScope(projectRaw, taskRaw *codefluxv1.StableIdentity) (GraphQueryScope, error) {
	projectID, err := ProjectIDFromProto(projectRaw)
	if err != nil {
		return GraphQueryScope{}, err
	}
	taskID, err := TaskIDFromProto(taskRaw)
	if err != nil {
		return GraphQueryScope{}, err
	}
	return GraphQueryScope{ProjectID: projectID, TaskID: taskID}, nil
}

func validateTransportGraphLayout(value GraphLayoutSelection) error {
	if (strings.TrimSpace(value.Algorithm) == "") != (value.Version == 0) {
		return invalidGraphField("layout", "algorithm and version must be selected together")
	}
	return nil
}

func invalidGraphField(field, reason string) error {
	return &RequestValidationError{Field: field, Reason: reason}
}

func mapGraphQueryError(err error) error {
	switch {
	case errors.Is(err, ErrGraphQueryNotFound):
		return &ApplicationError{Code: codefluxv1.ErrorCode_ERROR_CODE_NOT_FOUND, SafeMessage: "The requested task graph item was not found."}
	case errors.Is(err, ErrGraphQueryStale):
		return &ApplicationError{Code: codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION, SafeMessage: "The task graph revision changed; reload the current graph."}
	case errors.Is(err, ErrGraphQueryInvalid), errors.Is(err, ErrGraphQueryContinuation):
		return &ApplicationError{Code: codefluxv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, SafeMessage: "The graph query or continuation is invalid."}
	default:
		return err
	}
}

func graphSliceViewToProto(view GraphSliceQueryView) (*codefluxv1.GraphSliceView, error) {
	graphID, err := GraphIDToProto(view.Header.GraphID)
	if err != nil {
		return nil, err
	}
	revisionID, err := GraphRevisionIDToProto(view.Header.RevisionID)
	if err != nil {
		return nil, err
	}
	taskID, err := TaskIDToProto(view.TaskID)
	if err != nil {
		return nil, err
	}
	nodes := make([]*codefluxv1.GraphNodeView, 0, len(view.Nodes))
	for _, item := range view.Nodes {
		converted, conversionErr := graphNodeViewToProto(item)
		if conversionErr != nil {
			return nil, conversionErr
		}
		nodes = append(nodes, converted)
	}
	edges := make([]*codefluxv1.GraphEdgeView, 0, len(view.Edges))
	for _, item := range view.Edges {
		converted, conversionErr := graphEdgeViewToProto(item)
		if conversionErr != nil {
			return nil, conversionErr
		}
		edges = append(edges, converted)
	}
	createdAt := timestamppb.New(view.Header.CreatedAt)
	if err := createdAt.CheckValid(); err != nil {
		return nil, status.Error(codes.Internal, "graph revision time is invalid")
	}
	result := &codefluxv1.GraphSliceView{GraphId: graphID, GraphRevisionId: revisionID, TaskId: taskID, Revision: view.Header.Ordinal, Nodes: nodes, Edges: edges, ContinuationCursor: view.Continuation, Truncated: view.Continuation != "", Mode: string(view.Mode), GraphSchemaVersion: uint32(view.Header.SchemaVersion), ContentSha256: view.Header.ContentSHA256, LayoutAlgorithm: view.Header.LayoutAlgorithm, LayoutAlgorithmVersion: view.Header.LayoutVersion, CreatedAt: createdAt}
	if view.Header.ParentID != nil {
		result.ParentGraphRevisionId, err = GraphRevisionIDToProto(*view.Header.ParentID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func graphNodeViewToProto(view GraphNodeQueryView) (*codefluxv1.GraphNodeView, error) {
	nodeID, err := NodeIDToProto(view.Node.ID())
	if err != nil {
		return nil, err
	}
	events := make([]*codefluxv1.StableIdentity, 0, len(view.Node.Sources().EventIDs()))
	for _, id := range view.Node.Sources().EventIDs() {
		converted, conversionErr := EventIDToProto(id)
		if conversionErr != nil {
			return nil, conversionErr
		}
		events = append(events, converted)
	}
	messages := make([]*codefluxv1.StableIdentity, 0, len(view.MessageIDs))
	for _, id := range view.MessageIDs {
		converted, conversionErr := MessageIDToProto(id)
		if conversionErr != nil {
			return nil, conversionErr
		}
		messages = append(messages, converted)
	}
	steps := make([]*codefluxv1.GraphPlanStepLinkView, 0, len(view.Node.Sources().PlanSteps()))
	for _, step := range view.Node.Sources().PlanSteps() {
		steps = append(steps, &codefluxv1.GraphPlanStepLinkView{PlanRevision: step.PlanRevision, StepId: step.StepID})
	}
	locations := make([]*codefluxv1.GraphSourceLocationView, 0, len(view.SourceLocations))
	for _, location := range view.SourceLocations {
		if location.StartLine > uint64(^uint32(0)) || location.StartColumn > uint64(^uint32(0)) || location.EndLine > uint64(^uint32(0)) || location.EndColumn > uint64(^uint32(0)) {
			return nil, status.Error(codes.Internal, "graph source coordinates exceed the wire contract")
		}
		repositoryID, conversionErr := RepositoryIDToProto(location.RepositoryID)
		if conversionErr != nil {
			return nil, conversionErr
		}
		locations = append(locations, &codefluxv1.GraphSourceLocationView{RepositoryId: repositoryID, RepositoryRevision: location.RepositoryRevision, RepositoryRelativePath: safeGraphPath(location.RelativePath), StartLine: uint32(location.StartLine), StartColumn: uint32(location.StartColumn), EndLine: uint32(location.EndLine), EndColumn: uint32(location.EndColumn)})
	}
	contract := view.Node.Contract()
	result := &codefluxv1.GraphNodeView{NodeId: nodeID, Kind: string(view.Node.Class()), State: string(view.Node.Status()), Label: redactedGraphText(view.Node.DisplayName()), ContractPurpose: redactedGraphText(contract.Purpose()), ContractInputs: redactedGraphTexts(contract.Inputs()), ContractOutputs: redactedGraphTexts(contract.Outputs()), ContractEffects: redactedGraphTexts(contract.Effects()), Tombstoned: view.Tombstoned, RelatedMessageIds: messages, EventIds: events, PlanSteps: steps, SourceLocations: locations}
	if len(locations) > 0 {
		result.SourcePath = locations[0].RepositoryRelativePath
		result.SourceLine = locations[0].StartLine
	}
	if view.Layout != nil {
		if view.Layout.Rank > uint64(^uint32(0)) {
			return nil, status.Error(codes.Internal, "graph layout rank exceeds the wire contract")
		}
		result.Rank = uint32(view.Layout.Rank)
		result.Layout = &codefluxv1.GraphLayoutHintView{Algorithm: view.Layout.Algorithm, AlgorithmVersion: view.Layout.Version, XMilli: view.Layout.XMilli, YMilli: view.Layout.YMilli, WidthMilli: view.Layout.WidthMilli, HeightMilli: view.Layout.HeightMilli, Rank: view.Layout.Rank, SiblingOrder: view.Layout.SiblingOrder}
	}
	return result, nil
}

func graphEdgeViewToProto(view GraphEdgeQueryView) (*codefluxv1.GraphEdgeView, error) {
	edgeID, err := EdgeIDToProto(view.Edge.ID())
	if err != nil {
		return nil, err
	}
	from, err := NodeIDToProto(view.Edge.FromNode())
	if err != nil {
		return nil, err
	}
	to, err := NodeIDToProto(view.Edge.ToNode())
	if err != nil {
		return nil, err
	}
	events := make([]*codefluxv1.StableIdentity, 0, len(view.Edge.Sources().EventIDs()))
	for _, id := range view.Edge.Sources().EventIDs() {
		converted, conversionErr := EventIDToProto(id)
		if conversionErr != nil {
			return nil, conversionErr
		}
		events = append(events, converted)
	}
	steps := make([]*codefluxv1.GraphPlanStepLinkView, 0, len(view.Edge.Sources().PlanSteps()))
	for _, step := range view.Edge.Sources().PlanSteps() {
		steps = append(steps, &codefluxv1.GraphPlanStepLinkView{PlanRevision: step.PlanRevision, StepId: step.StepID})
	}
	return &codefluxv1.GraphEdgeView{EdgeId: edgeID, FromNodeId: from, ToNodeId: to, Kind: string(view.Edge.Class()), Tombstoned: view.Tombstoned, EventIds: events, PlanSteps: steps}, nil
}

func graphComparisonViewToProto(view GraphComparisonQueryView) (*codefluxv1.CompareGraphRevisionsResponse, error) {
	fromGraph, err := GraphIDToProto(view.From.GraphID)
	if err != nil {
		return nil, err
	}
	toGraph, err := GraphIDToProto(view.To.GraphID)
	if err != nil {
		return nil, err
	}
	response := &codefluxv1.CompareGraphRevisionsResponse{ContinuationCursor: view.Continuation, Truncated: view.Continuation != "", FromGraphId: fromGraph, FromRevision: view.From.Ordinal, ToGraphId: toGraph, ToRevision: view.To.Ordinal}
	for _, change := range view.Changes {
		item := &codefluxv1.GraphRevisionChangeView{EntityKind: change.Entity, ChangeKind: change.Kind, StableId: change.ID}
		if change.BeforeNode != nil {
			item.BeforeNode, err = graphNodeViewToProto(*change.BeforeNode)
			if err != nil {
				return nil, err
			}
		}
		if change.AfterNode != nil {
			item.AfterNode, err = graphNodeViewToProto(*change.AfterNode)
			if err != nil {
				return nil, err
			}
		}
		if change.BeforeEdge != nil {
			item.BeforeEdge, err = graphEdgeViewToProto(*change.BeforeEdge)
			if err != nil {
				return nil, err
			}
		}
		if change.AfterEdge != nil {
			item.AfterEdge, err = graphEdgeViewToProto(*change.AfterEdge)
			if err != nil {
				return nil, err
			}
		}
		response.Changes = append(response.Changes, item)
		if change.Entity == "node" {
			switch change.Kind {
			case "added":
				response.AddedNodes = append(response.AddedNodes, item.AfterNode)
			case "updated", "tombstoned":
				response.ChangedNodes = append(response.ChangedNodes, item.AfterNode)
			case "removed":
				identity, parseErr := domain.ParseNodeID(change.ID)
				if parseErr != nil {
					return nil, fmt.Errorf("parse removed graph node: %w", parseErr)
				}
				converted, conversionErr := NodeIDToProto(identity)
				if conversionErr != nil {
					return nil, conversionErr
				}
				response.RemovedNodeIds = append(response.RemovedNodeIds, converted)
			}
		}
	}
	return response, nil
}

func redactedGraphText(value string) *codefluxv1.RedactedText {
	return &codefluxv1.RedactedText{Value: value, OriginalBytes: uint64(len(value))}
}
func redactedGraphTexts(values []string) []*codefluxv1.RedactedText {
	result := make([]*codefluxv1.RedactedText, 0, len(values))
	for _, value := range values {
		result = append(result, redactedGraphText(value))
	}
	return result
}
func safeGraphPath(value string) *codefluxv1.SafePath {
	return &codefluxv1.SafePath{WorkspaceRelativeSlashPath: value}
}
