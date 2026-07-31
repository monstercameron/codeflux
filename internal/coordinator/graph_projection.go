package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/storage"
)

// GraphProjectionService is the runtime bridge from already-committed durable
// task facts to the pure graph projector and the transactional graph store.
type GraphProjectionService struct {
	repositories storage.GraphProjectionOperations
	publisher    storage.CommittedEventPublisher
}

func NewGraphProjectionService(
	repositories storage.GraphProjectionOperations,
	publisher storage.CommittedEventPublisher,
) (*GraphProjectionService, error) {
	if repositories == nil {
		return nil, errors.New("graph projection repositories are required")
	}
	return &GraphProjectionService{repositories: repositories, publisher: publisher}, nil
}

// ProjectedTaskGraph distinguishes non-material facts from committed graph
// revisions while returning the advanced pure projection in both cases.
type ProjectedTaskGraph struct {
	Projection graph.GraphProjection
	Revision   graph.Revision
	Committed  *storage.CommittedTaskGraphRevision
}

// ProjectNextCommittedTaskEvent reconstructs the latest sealed graph before
// applying a successive material fact. This is the restart-safe production
// entry point for task-event consumers after the root requirement exists.
func (service *GraphProjectionService) ProjectNextCommittedTaskEvent(
	ctx context.Context,
	scope storage.GraphQueryScope,
	durable storage.TaskEvent,
	fact graph.ProjectionEvent,
	revisionID domain.GraphRevisionID,
) (ProjectedTaskGraph, error) {
	projection, err := service.repositories.LoadTaskGraphProjection(ctx, scope)
	if err != nil {
		return ProjectedTaskGraph{}, err
	}
	return service.ProjectCommittedTaskEvent(ctx, scope, projection, durable, fact, revisionID)
}

// ProjectCommittedTaskEvent normalizes authority fields from an immutable
// durable task event, applies one typed fact, transactionally commits material
// graph state, and only then publishes the committed session event.
func (service *GraphProjectionService) ProjectCommittedTaskEvent(
	ctx context.Context,
	scope storage.GraphQueryScope,
	projection graph.GraphProjection,
	durable storage.TaskEvent,
	fact graph.ProjectionEvent,
	revisionID domain.GraphRevisionID,
) (ProjectedTaskGraph, error) {
	if durable.ID.IsZero() || durable.TaskID.IsZero() || durable.CreatedAt.IsZero() {
		return ProjectedTaskGraph{}, errors.New("committed durable task event is incomplete")
	}
	if scope.TaskID != durable.TaskID || projection.Graph().TaskID() != durable.TaskID {
		return ProjectedTaskGraph{}, errors.New("durable task event does not match graph projection scope")
	}
	// Sequence is the normalized graph-fact order, independent of unrelated
	// task journal facts. All other authority fields come from durable storage.
	fact.Sequence = projection.LastSequence() + 1
	fact.SourceEventID = durable.ID
	fact.TaskID = durable.TaskID
	fact.OccurredAt = durable.CreatedAt

	pending, change, err := graph.ProjectSessionEvent(projection, fact)
	if err != nil {
		return ProjectedTaskGraph{}, err
	}
	if !change.Material() {
		return ProjectedTaskGraph{Projection: pending}, nil
	}
	if revisionID.IsZero() {
		return ProjectedTaskGraph{}, errors.New("material graph fact requires a revision identity")
	}
	var previous *graph.Revision
	if value, ok := projection.Revision(); ok {
		previous = &value
	}
	committedProjection, revision, err := graph.CommitGraphRevision(pending, change, revisionID)
	if err != nil {
		return ProjectedTaskGraph{}, err
	}
	patch, err := graph.BuildGraphPatch(previous, revision, graph.DefaultPatchLimits())
	if err != nil {
		return ProjectedTaskGraph{}, err
	}
	encoded, err := encodeCommittedGraphPatch(patch, revision)
	if err != nil {
		return ProjectedTaskGraph{}, err
	}
	commit, err := service.repositories.CommitTaskGraphRevision(ctx, storage.CommitTaskGraphRevision{
		Scope: scope, Graph: committedProjection.Graph(), Revision: revision, EncodedChange: encoded,
	})
	if err != nil {
		return ProjectedTaskGraph{}, err
	}
	result := ProjectedTaskGraph{Projection: committedProjection, Revision: revision, Committed: &commit}
	if service.publisher != nil && !commit.Replayed {
		if err := service.publisher.PublishCommitted(commit.Event); err != nil {
			return result, fmt.Errorf("publish committed graph event: %w", err)
		}
	}
	return result, nil
}

type encodedGraphPatch struct {
	PreviousRevisionID string        `json:"previous_revision_id,omitempty"`
	RevisionID         string        `json:"revision_id"`
	Nodes              []encodedNode `json:"nodes,omitempty"`
	Edges              []encodedEdge `json:"edges,omitempty"`
	RemovedNodeIDs     []string      `json:"removed_node_ids,omitempty"`
	RemovedEdgeIDs     []string      `json:"removed_edge_ids,omitempty"`
}

type encodedNode struct {
	Operation   string   `json:"operation"`
	ID          string   `json:"id"`
	Class       string   `json:"class"`
	Status      string   `json:"status"`
	DisplayName string   `json:"display_name_redacted"`
	Purpose     string   `json:"contract_purpose_redacted,omitempty"`
	Inputs      []string `json:"inputs,omitempty"`
	Outputs     []string `json:"outputs,omitempty"`
	Effects     []string `json:"effects,omitempty"`
}

type encodedEdge struct {
	Operation string `json:"operation"`
	ID        string `json:"id"`
	Class     string `json:"class"`
	From      string `json:"from"`
	To        string `json:"to"`
}

func encodeCommittedGraphPatch(patch graph.GraphPatch, revision graph.Revision) ([]byte, error) {
	payload := encodedGraphPatch{RevisionID: patch.RevisionID.String()}
	if patch.PreviousRevisionID != nil {
		payload.PreviousRevisionID = patch.PreviousRevisionID.String()
	}
	appendNodes := func(operation string, nodes []graph.Node) {
		for _, node := range nodes {
			contract := node.Contract()
			payload.Nodes = append(payload.Nodes, encodedNode{Operation: operation, ID: node.ID().String(),
				Class: string(node.Class()), Status: string(node.Status()), DisplayName: node.DisplayName(),
				Purpose: contract.Purpose(), Inputs: contract.Inputs(), Outputs: contract.Outputs(), Effects: contract.Effects()})
		}
	}
	appendEdges := func(operation string, edges []graph.Edge) {
		for _, edge := range edges {
			payload.Edges = append(payload.Edges, encodedEdge{Operation: operation, ID: edge.ID().String(),
				Class: string(edge.Class()), From: edge.FromNode().String(), To: edge.ToNode().String()})
		}
	}
	appendNodes("add", patch.AddedNodes)
	appendNodes("update", patch.UpdatedNodes)
	appendEdges("add", patch.AddedEdges)
	appendEdges("update", patch.UpdatedEdges)
	for _, id := range patch.RemovedNodeIDs {
		payload.RemovedNodeIDs = append(payload.RemovedNodeIDs, id.String())
	}
	for _, id := range patch.RemovedEdgeIDs {
		payload.RemovedEdgeIDs = append(payload.RemovedEdgeIDs, id.String())
	}
	// A root snapshot is intentionally self-contained even though it uses the
	// same bounded wire DTO as adjacent patches.
	if patch.PreviousRevisionID == nil && len(payload.Nodes) != len(revision.Nodes()) {
		return nil, errors.New("root graph snapshot is incomplete")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode committed graph patch: %w", err)
	}
	if len(encoded) > storage.MaximumEncodedGraphChangeBytes {
		return nil, errors.New("encoded graph patch exceeds the durable event bound")
	}
	return encoded, nil
}
