package coordinator

import (
	"context"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// GraphQueryService composes the task-scoped storage query boundary into the
// transport-independent application port used by GraphService.
type GraphQueryService struct {
	repositories storage.GraphQueryOperations
}

var _ transport.GraphQueryApplication = (*GraphQueryService)(nil)

func NewGraphQueryService(repositories storage.GraphQueryOperations) (*GraphQueryService, error) {
	if repositories == nil {
		return nil, errors.New("graph query repositories are required")
	}
	return &GraphQueryService{repositories: repositories}, nil
}

func (service *GraphQueryService) GetGraphSlice(ctx context.Context, query transport.GraphSliceQuery) (transport.GraphSliceQueryView, error) {
	result, err := service.repositories.GetTaskGraphSlice(ctx, storage.TaskGraphSliceQuery{
		Scope:              storage.GraphQueryScope{ProjectID: query.Scope.ProjectID, TaskID: query.Scope.TaskID},
		ExpectedRevisionID: query.ExpectedRevisionID, Mode: query.Mode,
		MaxNodes: query.MaxNodes, MaxEdges: query.MaxEdges,
		Continuation: query.Continuation,
		Layout:       storage.GraphLayoutSelection{Algorithm: query.Layout.Algorithm, Version: query.Layout.Version},
	})
	if err != nil {
		return transport.GraphSliceQueryView{}, mapGraphStorageError(err)
	}
	return graphSliceApplicationView(query.Scope.TaskID, result), nil
}

func (service *GraphQueryService) ExpandGraph(ctx context.Context, query transport.GraphExpansionQuery) (transport.GraphSliceQueryView, error) {
	result, err := service.repositories.ExpandTaskGraph(ctx, storage.TaskGraphExpansionQuery{
		Scope:      storage.GraphQueryScope{ProjectID: query.Scope.ProjectID, TaskID: query.Scope.TaskID},
		RevisionID: query.RevisionID, RootNodeIDs: query.RootNodeIDs,
		Mode: query.Mode, Traversal: storage.GraphTraversal(query.Traversal),
		MaxHops: query.MaxHops, MaxNodes: query.MaxNodes, MaxEdges: query.MaxEdges,
		Continuation: query.Continuation,
		Layout:       storage.GraphLayoutSelection{Algorithm: query.Layout.Algorithm, Version: query.Layout.Version},
	})
	if err != nil {
		return transport.GraphSliceQueryView{}, mapGraphStorageError(err)
	}
	return graphSliceApplicationView(query.Scope.TaskID, result), nil
}

func (service *GraphQueryService) GetGraphNode(ctx context.Context, query transport.GraphNodeQuery) (transport.GraphNodeQueryView, error) {
	result, err := service.repositories.GetTaskGraphNode(ctx, storage.TaskGraphNodeQuery{
		Scope:      storage.GraphQueryScope{ProjectID: query.Scope.ProjectID, TaskID: query.Scope.TaskID},
		RevisionID: query.RevisionID, NodeID: query.NodeID,
		Layout: storage.GraphLayoutSelection{Algorithm: query.Layout.Algorithm, Version: query.Layout.Version},
	})
	if err != nil {
		return transport.GraphNodeQueryView{}, mapGraphStorageError(err)
	}
	return graphNodeApplicationView(result), nil
}

func (service *GraphQueryService) SearchGraph(ctx context.Context, query transport.GraphSearchQuery) (transport.GraphSliceQueryView, error) {
	result, err := service.repositories.SearchTaskGraph(ctx, storage.TaskGraphSearchQuery{
		Scope:      storage.GraphQueryScope{ProjectID: query.Scope.ProjectID, TaskID: query.Scope.TaskID},
		RevisionID: query.RevisionID, Mode: query.Mode, Text: query.Text,
		MaxResults: query.MaxResults, Continuation: query.Continuation,
		Layout: storage.GraphLayoutSelection{Algorithm: query.Layout.Algorithm, Version: query.Layout.Version},
	})
	if err != nil {
		return transport.GraphSliceQueryView{}, mapGraphStorageError(err)
	}
	view := transport.GraphSliceQueryView{TaskID: query.Scope.TaskID, Header: graphHeaderApplicationView(result.Header), Mode: result.Mode, Continuation: result.Continuation}
	for _, node := range result.Nodes {
		view.Nodes = append(view.Nodes, graphNodeApplicationView(node))
	}
	return view, nil
}

func (service *GraphQueryService) CompareGraphRevisions(ctx context.Context, query transport.GraphComparisonQuery) (transport.GraphComparisonQueryView, error) {
	result, err := service.repositories.CompareTaskGraphRevisions(ctx, storage.TaskGraphComparisonQuery{
		Scope:          storage.GraphQueryScope{ProjectID: query.Scope.ProjectID, TaskID: query.Scope.TaskID},
		FromRevisionID: query.FromRevisionID, ToRevisionID: query.ToRevisionID,
		MaxChanges: query.MaxChanges, Continuation: query.Continuation,
	})
	if err != nil {
		return transport.GraphComparisonQueryView{}, mapGraphStorageError(err)
	}
	view := transport.GraphComparisonQueryView{From: graphHeaderApplicationView(result.From), To: graphHeaderApplicationView(result.To), Continuation: result.Continuation}
	for _, change := range result.Changes {
		item := transport.GraphRevisionChangeView{Entity: string(change.Entity), Kind: string(change.Kind), ID: change.ID}
		if change.Before != nil {
			converted := graphNodeApplicationView(*change.Before)
			item.BeforeNode = &converted
		}
		if change.After != nil {
			converted := graphNodeApplicationView(*change.After)
			item.AfterNode = &converted
		}
		if change.BeforeEdge != nil {
			converted := graphEdgeApplicationView(*change.BeforeEdge)
			item.BeforeEdge = &converted
		}
		if change.AfterEdge != nil {
			converted := graphEdgeApplicationView(*change.AfterEdge)
			item.AfterEdge = &converted
		}
		view.Changes = append(view.Changes, item)
	}
	return view, nil
}

func graphSliceApplicationView(taskID domain.TaskID, result storage.TaskGraphSlice) transport.GraphSliceQueryView {
	view := transport.GraphSliceQueryView{TaskID: taskID, Header: graphHeaderApplicationView(result.Header), Mode: result.Mode, Continuation: result.Continuation}
	for _, node := range result.Nodes {
		view.Nodes = append(view.Nodes, graphNodeApplicationView(node))
	}
	for _, edge := range result.Edges {
		view.Edges = append(view.Edges, graphEdgeApplicationView(edge))
	}
	return view
}

func graphHeaderApplicationView(value storage.GraphRevisionHeader) transport.GraphRevisionHeaderView {
	return transport.GraphRevisionHeaderView{GraphID: value.GraphID, RevisionID: value.RevisionID, Ordinal: value.Ordinal, ParentID: value.ParentID, SchemaVersion: value.SchemaVersion, CreatedAt: value.CreatedAt, ContentSHA256: value.ContentSHA256, LayoutAlgorithm: value.LayoutAlgorithm, LayoutVersion: value.LayoutVersion}
}

func graphNodeApplicationView(value storage.GraphNodeRecord) transport.GraphNodeQueryView {
	result := transport.GraphNodeQueryView{Node: value.Node, Tombstoned: value.Tombstoned, MessageIDs: append([]domain.MessageID(nil), value.MessageIDs...)}
	for _, location := range value.SourceLocations {
		result.SourceLocations = append(result.SourceLocations, transport.GraphSourceLocationView{RepositoryID: location.RepositoryID, RepositoryRevision: location.RepositoryRevision, RelativePath: location.RelativePath, StartLine: location.StartLine, StartColumn: location.StartColumn, EndLine: location.EndLine, EndColumn: location.EndColumn})
	}
	if value.Layout != nil {
		result.Layout = &transport.GraphLayoutHintView{Algorithm: value.Layout.Algorithm, Version: value.Layout.Version, XMilli: value.Layout.XMilli, YMilli: value.Layout.YMilli, WidthMilli: uint64(value.Layout.WidthMilli), HeightMilli: uint64(value.Layout.HeightMilli), Rank: value.Layout.Rank, SiblingOrder: value.Layout.SiblingOrder}
	}
	return result
}

func graphEdgeApplicationView(value storage.GraphEdgeRecord) transport.GraphEdgeQueryView {
	return transport.GraphEdgeQueryView{Edge: value.Edge, Tombstoned: value.Tombstoned}
}

func mapGraphStorageError(err error) error {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return fmt.Errorf("%w: %v", transport.ErrGraphQueryNotFound, err)
	case errors.Is(err, storage.ErrStaleRevision):
		return fmt.Errorf("%w: %v", transport.ErrGraphQueryStale, err)
	case errors.Is(err, storage.ErrGraphContinuation):
		return fmt.Errorf("%w: %v", transport.ErrGraphQueryContinuation, err)
	case errors.Is(err, storage.ErrGraphQueryUnbounded), errors.Is(err, storage.ErrGraphQueryLimitExceeded):
		return fmt.Errorf("%w: %v", transport.ErrGraphQueryInvalid, err)
	default:
		return err
	}
}
