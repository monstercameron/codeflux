package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	taskgraph "codeflux.dev/codeflux/internal/graph"
)

const MaximumEncodedGraphChangeBytes = 1 << 20

// CommitTaskGraphRevision is the transactional write boundary between the
// pure graph projector and durable query/session state. The supplied revision
// must already have been committed by graph.CommitGraphRevision.
type CommitTaskGraphRevision struct {
	Scope         GraphQueryScope
	Graph         taskgraph.Graph
	Revision      taskgraph.Revision
	EncodedChange []byte
}

// CommittedTaskGraphRevision returns the immutable query header and the
// correctness-bearing session event that may be published after commit.
type CommittedTaskGraphRevision struct {
	Header   GraphRevisionHeader
	Event    events.SessionEvent
	Replayed bool
}

// GraphProjectionOperations is the write-side graph repository port.
type GraphProjectionOperations interface {
	CommitTaskGraphRevision(context.Context, CommitTaskGraphRevision) (CommittedTaskGraphRevision, error)
	LoadTaskGraphProjection(context.Context, GraphQueryScope) (taskgraph.GraphProjection, error)
}

var _ GraphProjectionOperations = (*Repositories)(nil)

// LoadTaskGraphProjection reconstructs the latest sealed material state for a
// task. Non-material token deltas intentionally do not participate in the
// restart sequence; the latest revision ordinal is the next material cursor.
func (repositories *Repositories) LoadTaskGraphProjection(
	ctx context.Context,
	scope GraphQueryScope,
) (taskgraph.GraphProjection, error) {
	if err := scope.validate(); err != nil {
		return taskgraph.GraphProjection{}, err
	}
	loaded, err := repositories.loadTaskGraphRevision(ctx, scope, nil, GraphLayoutSelection{})
	if err != nil {
		return taskgraph.GraphProjection{}, err
	}
	var createdMicros int64
	err = repositories.database.sql.QueryRowContext(ctx, `SELECT graphs.created_at_unix_micros
		FROM graphs JOIN graph_task_bindings ON graph_task_bindings.graph_id = graphs.id
		WHERE graphs.id = ? AND graphs.project_id = ? AND graph_task_bindings.task_id = ?`,
		loaded.header.GraphID, scope.ProjectID, scope.TaskID).Scan(&createdMicros)
	if errors.Is(err, sql.ErrNoRows) {
		return taskgraph.GraphProjection{}, typedError(ErrNotFound, "load task graph identity", err)
	}
	if err != nil {
		return taskgraph.GraphProjection{}, classify("load task graph identity", err)
	}
	identity, err := taskgraph.NewGraph(loaded.header.GraphID, scope.TaskID, repositoryTime(createdMicros))
	if err != nil {
		return taskgraph.GraphProjection{}, typedError(ErrCorrupt, "reconstruct task graph identity", err)
	}
	projection, err := taskgraph.NewGraphProjection(identity, &loaded.revision, loaded.header.Ordinal)
	if err != nil {
		return taskgraph.GraphProjection{}, typedError(ErrCorrupt, "reconstruct task graph projection", err)
	}
	return projection, nil
}

// CommitTaskGraphRevision writes the graph identity, complete revision,
// provenance links, seal, and graph session event in one SQLite transaction.
// The seal is deliberately last so readers can never observe a partial graph.
func (repositories *Repositories) CommitTaskGraphRevision(
	ctx context.Context,
	input CommitTaskGraphRevision,
) (CommittedTaskGraphRevision, error) {
	if err := validateGraphRevisionCommit(input); err != nil {
		return CommittedTaskGraphRevision{}, err
	}
	hash, err := graphRevisionContentHash(input.Revision)
	if err != nil {
		return CommittedTaskGraphRevision{}, err
	}
	metadata := input.Revision.Metadata()
	header := GraphRevisionHeader{
		GraphID: metadata.GraphID(), RevisionID: metadata.ID(), Ordinal: metadata.Ordinal(),
		SchemaVersion: metadata.SchemaVersion(), CreatedAt: metadata.CreatedAt(), ContentSHA256: hash,
	}
	if parent, ok := metadata.ParentID(); ok {
		header.ParentID = &parent
	}

	var committed CommittedTaskGraphRevision
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		authority, err := loadGraphCommitAuthority(ctx, transaction, input.Scope)
		if err != nil {
			return err
		}
		if existing, found, err := loadExistingGraphCommit(ctx, transaction, authority.sessionID, metadata.ID()); err != nil {
			return err
		} else if found {
			if existing.Header.GraphID != input.Graph.ID() || existing.Header.ContentSHA256 != hash ||
				existing.Event.Payload.Graph == nil || !bytes.Equal(existing.Event.Payload.Graph.EncodedChange, input.EncodedChange) {
				return typedError(ErrConflict, "commit idempotent graph revision", errors.New("revision identity belongs to different graph content"))
			}
			committed = existing
			return nil
		}

		micros := metadata.CreatedAt().UnixMicro()
		if metadata.Ordinal() == 1 {
			if err := insertGraphIdentity(ctx, transaction, input, authority, micros); err != nil {
				return err
			}
		} else if err := verifyGraphBinding(ctx, transaction, input, authority); err != nil {
			return err
		}
		if err := insertGraphRevisionRows(ctx, transaction, input, hash, micros); err != nil {
			return err
		}

		kind := events.KindGraphPatch
		if metadata.Ordinal() == 1 {
			kind = events.KindGraphSnapshot
		}
		taskID := input.Graph.TaskID()
		causation := firstGraphEventID(input.Revision)
		event, err := repositories.AppendSessionEvent(ctx, transaction, events.NewSessionEvent{
			SessionID: authority.sessionID, ThreadID: authority.threadID, TaskID: &taskID,
			Kind: kind, Revision: metadata.Ordinal(), CausationID: causation,
			PayloadVersion: 1, Payload: events.Payload{Graph: &events.Graph{
				RevisionID: metadata.ID(), EncodedChange: append([]byte(nil), input.EncodedChange...),
			}},
		})
		if err != nil {
			return err
		}
		committed = CommittedTaskGraphRevision{Header: header, Event: event}
		return nil
	})
	if err != nil {
		return CommittedTaskGraphRevision{}, err
	}
	return committed, nil
}

type graphCommitAuthority struct {
	projectID    domain.ProjectID
	repositoryID domain.RepositoryID
	threadID     domain.ThreadID
	sessionID    domain.SessionID
}

func validateGraphRevisionCommit(input CommitTaskGraphRevision) error {
	if err := input.Scope.validate(); err != nil {
		return err
	}
	if err := input.Graph.Validate(); err != nil {
		return err
	}
	if err := input.Revision.Validate(); err != nil {
		return err
	}
	if input.Graph.TaskID() != input.Scope.TaskID || input.Revision.Metadata().GraphID() != input.Graph.ID() {
		return errors.New("graph revision does not match the task-scoped graph identity")
	}
	if len(input.Revision.Nodes()) > MaximumStoredGraphNodes || len(input.Revision.Edges()) > MaximumStoredGraphEdges {
		return ErrGraphQueryLimitExceeded
	}
	if len(input.EncodedChange) == 0 || len(input.EncodedChange) > MaximumEncodedGraphChangeBytes {
		return errors.New("encoded graph change must be non-empty and bounded")
	}
	return nil
}

func loadGraphCommitAuthority(ctx context.Context, transaction *Transaction, scope GraphQueryScope) (graphCommitAuthority, error) {
	var authority graphCommitAuthority
	err := transaction.sql.QueryRowContext(ctx, `SELECT threads.project_id, tasks.repository_id,
		       tasks.thread_id, sessions.id
		FROM tasks
		JOIN threads ON threads.id = tasks.thread_id
		JOIN sessions ON sessions.thread_id = threads.id
		WHERE tasks.id = ? AND threads.project_id = ?
		  AND threads.repository_id = tasks.repository_id
		  AND threads.deleted_at_unix_micros IS NULL`, scope.TaskID, scope.ProjectID).Scan(
		&authority.projectID, &authority.repositoryID, &authority.threadID, &authority.sessionID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return graphCommitAuthority{}, typedError(ErrNotFound, "authorize graph revision commit", err)
	}
	if err != nil {
		return graphCommitAuthority{}, classify("authorize graph revision commit", err)
	}
	return authority, nil
}

func insertGraphIdentity(ctx context.Context, transaction *Transaction, input CommitTaskGraphRevision, authority graphCommitAuthority, micros int64) error {
	if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graphs
		(id, project_id, repository_id, created_at_unix_micros) VALUES (?, ?, ?, ?)`,
		input.Graph.ID(), authority.projectID, authority.repositoryID, input.Graph.CreatedAt().UnixMicro()); err != nil {
		return repositoryWriteError("insert graph identity", err)
	}
	if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_task_bindings
		(graph_id, project_id, repository_id, thread_id, task_id, created_at_unix_micros)
		VALUES (?, ?, ?, ?, ?, ?)`, input.Graph.ID(), authority.projectID, authority.repositoryID,
		authority.threadID, input.Graph.TaskID(), micros); err != nil {
		return repositoryWriteError("bind graph to task", err)
	}
	return nil
}

func verifyGraphBinding(ctx context.Context, transaction *Transaction, input CommitTaskGraphRevision, authority graphCommitAuthority) error {
	var found int
	err := transaction.sql.QueryRowContext(ctx, `SELECT 1 FROM graph_task_bindings
		WHERE graph_id = ? AND project_id = ? AND repository_id = ? AND thread_id = ? AND task_id = ?`,
		input.Graph.ID(), authority.projectID, authority.repositoryID, authority.threadID, input.Graph.TaskID()).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return typedError(ErrNotFound, "verify graph task binding", err)
	}
	return classify("verify graph task binding", err)
}

func insertGraphRevisionRows(ctx context.Context, transaction *Transaction, input CommitTaskGraphRevision, hash string, micros int64) error {
	metadata := input.Revision.Metadata()
	var parent any
	if value, ok := metadata.ParentID(); ok {
		parent = value
	}
	if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_revisions
		(id, graph_id, revision, parent_revision_id, graph_schema_version, metadata_policy, created_at_unix_micros)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, metadata.ID(), metadata.GraphID(), metadata.Ordinal(), parent,
		metadata.SchemaVersion(), metadata.MetadataPolicy(), micros); err != nil {
		return repositoryWriteError("insert graph revision", err)
	}

	planRevision, _, err := collectGraphPlanSteps(input.Revision)
	if err != nil {
		return err
	}
	if planRevision != 0 {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_plan_bindings
			(graph_id, graph_revision_id, task_id, plan_revision, created_at_unix_micros)
			VALUES (?, ?, ?, ?, ?)`, input.Graph.ID(), metadata.ID(), input.Graph.TaskID(), planRevision, micros); err != nil {
			return repositoryWriteError("insert graph plan binding", err)
		}
	}
	if err := insertRevisionProvenance(ctx, transaction, input, planRevision, metadata.Sources().PlanSteps()); err != nil {
		return err
	}
	for _, node := range input.Revision.Nodes() {
		if err := insertGraphNode(ctx, transaction, input, node, micros, planRevision); err != nil {
			return err
		}
	}
	for _, edge := range input.Revision.Edges() {
		if err := insertGraphEdge(ctx, transaction, input, edge, micros, planRevision); err != nil {
			return err
		}
	}
	if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_revision_seals
		(graph_id, graph_revision_id, node_count, edge_count, content_sha256, sealed_at_unix_micros)
		VALUES (?, ?, ?, ?, ?, ?)`, input.Graph.ID(), metadata.ID(), len(input.Revision.Nodes()),
		len(input.Revision.Edges()), hash, micros); err != nil {
		return repositoryWriteError("seal graph revision", err)
	}
	return nil
}

func insertGraphNode(ctx context.Context, transaction *Transaction, input CommitTaskGraphRevision, node taskgraph.Node, micros int64, planRevision uint64) error {
	if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_nodes (id, graph_id, created_at_unix_micros)
		VALUES (?, ?, ?) ON CONFLICT(id) DO NOTHING`, node.ID(), input.Graph.ID(), micros); err != nil {
		return repositoryWriteError("insert stable graph node", err)
	}
	contract := node.Contract()
	if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_node_revisions
		(graph_id, graph_revision_id, node_id, node_class, status, display_name_redacted,
		 contract_purpose_redacted, tombstoned, created_at_unix_micros)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`, input.Graph.ID(), input.Revision.Metadata().ID(), node.ID(),
		node.Class(), node.Status(), node.DisplayName(), contract.Purpose(), micros); err != nil {
		return repositoryWriteError("insert graph node revision", err)
	}
	for kind, values := range map[string][]string{"input": contract.Inputs(), "output": contract.Outputs(), "effect": contract.Effects()} {
		for ordinal, value := range values {
			if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_node_contract_items
				(graph_revision_id, node_id, item_kind, ordinal, value_redacted) VALUES (?, ?, ?, ?, ?)`,
				input.Revision.Metadata().ID(), node.ID(), kind, ordinal, value); err != nil {
				return repositoryWriteError("insert graph node contract item", err)
			}
		}
	}
	return insertNodeProvenance(ctx, transaction, input, node, planRevision)
}

func insertGraphEdge(ctx context.Context, transaction *Transaction, input CommitTaskGraphRevision, edge taskgraph.Edge, micros int64, planRevision uint64) error {
	if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_edges (id, graph_id, created_at_unix_micros)
		VALUES (?, ?, ?) ON CONFLICT(id) DO NOTHING`, edge.ID(), input.Graph.ID(), micros); err != nil {
		return repositoryWriteError("insert stable graph edge", err)
	}
	if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_edge_revisions
		(graph_id, graph_revision_id, edge_id, edge_class, source_node_id, target_node_id, tombstoned, created_at_unix_micros)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?)`, input.Graph.ID(), input.Revision.Metadata().ID(), edge.ID(),
		edge.Class(), edge.FromNode(), edge.ToNode(), micros); err != nil {
		return repositoryWriteError("insert graph edge revision", err)
	}
	return insertEdgeProvenance(ctx, transaction, input, edge, planRevision)
}

func insertRevisionProvenance(ctx context.Context, transaction *Transaction, input CommitTaskGraphRevision, planRevision uint64, planSteps []taskgraph.PlanStepLink) error {
	metadata := input.Revision.Metadata()
	for ordinal, eventID := range metadata.Sources().EventIDs() {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_revision_event_links
			(graph_id, graph_revision_id, task_id, event_id, ordinal) VALUES (?, ?, ?, ?, ?)`,
			input.Graph.ID(), metadata.ID(), input.Graph.TaskID(), eventID, ordinal); err != nil {
			return repositoryWriteError("insert graph revision event link", err)
		}
	}
	for ordinal, link := range planSteps {
		if link.PlanRevision != planRevision {
			return errors.New("graph revision references more than one plan revision")
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_revision_plan_step_links
			(graph_id, graph_revision_id, task_id, plan_revision, step_id, ordinal) VALUES (?, ?, ?, ?, ?, ?)`,
			input.Graph.ID(), metadata.ID(), input.Graph.TaskID(), link.PlanRevision, link.StepID, ordinal); err != nil {
			return repositoryWriteError("insert graph revision plan-step link", err)
		}
	}
	return nil
}

func insertNodeProvenance(ctx context.Context, transaction *Transaction, input CommitTaskGraphRevision, node taskgraph.Node, planRevision uint64) error {
	for ordinal, eventID := range node.Sources().EventIDs() {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_node_event_links
			(graph_id, graph_revision_id, node_id, task_id, event_id, ordinal) VALUES (?, ?, ?, ?, ?, ?)`,
			input.Graph.ID(), input.Revision.Metadata().ID(), node.ID(), input.Graph.TaskID(), eventID, ordinal); err != nil {
			return repositoryWriteError("insert graph node event link", err)
		}
	}
	for ordinal, link := range node.Sources().PlanSteps() {
		if link.PlanRevision != planRevision {
			return errors.New("graph node references a different plan revision")
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_node_plan_step_links
			(graph_id, graph_revision_id, node_id, task_id, plan_revision, step_id, ordinal) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			input.Graph.ID(), input.Revision.Metadata().ID(), node.ID(), input.Graph.TaskID(), link.PlanRevision, link.StepID, ordinal); err != nil {
			return repositoryWriteError("insert graph node plan-step link", err)
		}
	}
	return nil
}

func insertEdgeProvenance(ctx context.Context, transaction *Transaction, input CommitTaskGraphRevision, edge taskgraph.Edge, planRevision uint64) error {
	for ordinal, eventID := range edge.Sources().EventIDs() {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_edge_event_links
			(graph_id, graph_revision_id, edge_id, task_id, event_id, ordinal) VALUES (?, ?, ?, ?, ?, ?)`,
			input.Graph.ID(), input.Revision.Metadata().ID(), edge.ID(), input.Graph.TaskID(), eventID, ordinal); err != nil {
			return repositoryWriteError("insert graph edge event link", err)
		}
	}
	for ordinal, link := range edge.Sources().PlanSteps() {
		if link.PlanRevision != planRevision {
			return errors.New("graph edge references a different plan revision")
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO graph_edge_plan_step_links
			(graph_id, graph_revision_id, edge_id, task_id, plan_revision, step_id, ordinal) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			input.Graph.ID(), input.Revision.Metadata().ID(), edge.ID(), input.Graph.TaskID(), link.PlanRevision, link.StepID, ordinal); err != nil {
			return repositoryWriteError("insert graph edge plan-step link", err)
		}
	}
	return nil
}

func collectGraphPlanSteps(revision taskgraph.Revision) (uint64, []taskgraph.PlanStepLink, error) {
	seen := make(map[taskgraph.PlanStepLink]struct{})
	var planRevision uint64
	add := func(links []taskgraph.PlanStepLink) error {
		for _, link := range links {
			if planRevision != 0 && planRevision != link.PlanRevision {
				return errors.New("one graph revision cannot bind multiple plan revisions")
			}
			planRevision = link.PlanRevision
			seen[link] = struct{}{}
		}
		return nil
	}
	if err := add(revision.Metadata().Sources().PlanSteps()); err != nil {
		return 0, nil, err
	}
	for _, node := range revision.Nodes() {
		if err := add(node.Sources().PlanSteps()); err != nil {
			return 0, nil, err
		}
	}
	for _, edge := range revision.Edges() {
		if err := add(edge.Sources().PlanSteps()); err != nil {
			return 0, nil, err
		}
	}
	steps := make([]taskgraph.PlanStepLink, 0, len(seen))
	for step := range seen {
		steps = append(steps, step)
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].StepID < steps[j].StepID })
	return planRevision, steps, nil
}

func firstGraphEventID(revision taskgraph.Revision) *domain.EventID {
	links := revision.Metadata().Sources().EventIDs()
	if len(links) == 0 {
		return nil
	}
	value := links[0]
	return &value
}

type graphHashNode struct {
	ID, Class, Status, Name, Purpose string
	Inputs, Outputs, Effects         []string
	Events                           []string
	Steps                            []taskgraph.PlanStepLink
}

type graphHashEdge struct {
	ID, Class, From, To string
	Events              []string
	Steps               []taskgraph.PlanStepLink
}

func graphRevisionContentHash(revision taskgraph.Revision) (string, error) {
	metadata := revision.Metadata()
	payload := struct {
		Policy string
		Schema uint64
		Events []string
		Steps  []taskgraph.PlanStepLink
		Nodes  []graphHashNode
		Edges  []graphHashEdge
	}{Policy: string(metadata.MetadataPolicy()), Schema: uint64(metadata.SchemaVersion())}
	payload.Events = graphEventStrings(metadata.Sources().EventIDs())
	payload.Steps = metadata.Sources().PlanSteps()
	for _, node := range revision.Nodes() {
		contract := node.Contract()
		payload.Nodes = append(payload.Nodes, graphHashNode{ID: node.ID().String(), Class: string(node.Class()), Status: string(node.Status()), Name: node.DisplayName(), Purpose: contract.Purpose(), Inputs: contract.Inputs(), Outputs: contract.Outputs(), Effects: contract.Effects(), Events: graphEventStrings(node.Sources().EventIDs()), Steps: node.Sources().PlanSteps()})
	}
	for _, edge := range revision.Edges() {
		payload.Edges = append(payload.Edges, graphHashEdge{ID: edge.ID().String(), Class: string(edge.Class()), From: edge.FromNode().String(), To: edge.ToNode().String(), Events: graphEventStrings(edge.Sources().EventIDs()), Steps: edge.Sources().PlanSteps()})
	}
	sort.Slice(payload.Nodes, func(i, j int) bool { return payload.Nodes[i].ID < payload.Nodes[j].ID })
	sort.Slice(payload.Edges, func(i, j int) bool { return payload.Edges[i].ID < payload.Edges[j].ID })
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode graph revision content: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func graphEventStrings(ids []domain.EventID) []string {
	result := make([]string, len(ids))
	for index, id := range ids {
		result[index] = id.String()
	}
	return result
}

func loadExistingGraphCommit(ctx context.Context, transaction *Transaction, sessionID domain.SessionID, revisionID domain.GraphRevisionID) (CommittedTaskGraphRevision, bool, error) {
	var result CommittedTaskGraphRevision
	var parent sql.NullString
	var createdMicros int64
	err := transaction.sql.QueryRowContext(ctx, `SELECT revisions.graph_id, revisions.id, revisions.revision,
		revisions.parent_revision_id, revisions.graph_schema_version, revisions.created_at_unix_micros,
		seals.content_sha256
		FROM graph_revisions AS revisions
		JOIN graph_revision_seals AS seals ON seals.graph_revision_id = revisions.id
		WHERE revisions.id = ?`, revisionID).Scan(&result.Header.GraphID, &result.Header.RevisionID,
		&result.Header.Ordinal, &parent, &result.Header.SchemaVersion, &createdMicros, &result.Header.ContentSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return CommittedTaskGraphRevision{}, false, nil
	}
	if err != nil {
		return CommittedTaskGraphRevision{}, false, classify("load idempotent graph revision", err)
	}
	result.Header.CreatedAt = repositoryTime(createdMicros)
	if parent.Valid {
		parsed, parseErr := domain.ParseGraphRevisionID(parent.String)
		if parseErr != nil {
			return CommittedTaskGraphRevision{}, false, parseErr
		}
		result.Header.ParentID = &parsed
	}
	row := transaction.sql.QueryRowContext(ctx, `SELECT sequence, thread_id, task_id, timestamp_unix_micros,
		kind, entity_revision, causation_id, correlation_id, payload_version, payload_json
		FROM session_events
		WHERE session_id = ? AND kind IN (?, ?) AND json_extract(payload_json, '$.graph.revision_id') = ?
		ORDER BY sequence DESC LIMIT 1`, sessionID, events.KindGraphSnapshot, events.KindGraphPatch, revisionID.String())
	result.Event, err = scanSessionEvent(row, sessionID)
	if err != nil {
		return CommittedTaskGraphRevision{}, false, err
	}
	result.Replayed = true
	return result, true, nil
}
