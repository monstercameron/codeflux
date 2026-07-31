package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
)

const (
	MaximumGraphQueryNodes       = 500
	MaximumGraphQueryEdges       = 1000
	MaximumGraphSearchResults    = 100
	MaximumGraphCompareChanges   = 500
	MaximumGraphExpansionHops    = 8
	MaximumGraphExpansionRoots   = 32
	MaximumStoredGraphNodes      = 4096
	MaximumStoredGraphEdges      = 8192
	MaximumGraphSearchTextBytes  = 256
	graphContinuationTokenSchema = 1
)

var (
	ErrGraphQueryUnbounded     = errors.New("graph query must be explicitly bounded")
	ErrGraphQueryLimitExceeded = errors.New("graph query limit exceeded")
	ErrGraphContinuation       = errors.New("invalid graph continuation token")
)

type GraphQueryScope struct {
	ProjectID domain.ProjectID
	TaskID    domain.TaskID
}

func (scope GraphQueryScope) validate() error {
	if scope.ProjectID.IsZero() || scope.TaskID.IsZero() {
		return fmt.Errorf("graph project and task scope must not be empty")
	}
	return nil
}

type GraphLayoutSelection struct {
	Algorithm string
	Version   uint64
}

func (selection GraphLayoutSelection) validate() error {
	if (selection.Algorithm == "") != (selection.Version == 0) {
		return errors.New("graph layout algorithm and version must be selected together")
	}
	if len(selection.Algorithm) > 128 {
		return errors.New("graph layout algorithm is too long")
	}
	return nil
}

type GraphLayoutHint struct {
	Algorithm    string
	Version      uint64
	XMilli       int64
	YMilli       int64
	WidthMilli   int64
	HeightMilli  int64
	Rank         uint64
	SiblingOrder uint64
}

type GraphSourceLocation struct {
	RepositoryID       domain.RepositoryID
	RepositoryRevision string
	RelativePath       string
	StartLine          uint64
	StartColumn        uint64
	EndLine            uint64
	EndColumn          uint64
}

type GraphNodeRecord struct {
	Node            taskgraph.Node
	Tombstoned      bool
	MessageIDs      []domain.MessageID
	SourceLocations []GraphSourceLocation
	Layout          *GraphLayoutHint
}

type GraphEdgeRecord struct {
	Edge       taskgraph.Edge
	Tombstoned bool
}

type GraphRevisionHeader struct {
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

type TaskGraphSlice struct {
	Header       GraphRevisionHeader
	Mode         taskgraph.Mode
	Nodes        []GraphNodeRecord
	Edges        []GraphEdgeRecord
	Continuation string
}

type TaskGraphSliceQuery struct {
	Scope              GraphQueryScope
	ExpectedRevisionID *domain.GraphRevisionID
	Mode               taskgraph.Mode
	MaxNodes           int
	MaxEdges           int
	Continuation       string
	Layout             GraphLayoutSelection
}

type TaskGraphNodeQuery struct {
	Scope      GraphQueryScope
	RevisionID domain.GraphRevisionID
	NodeID     domain.NodeID
	Layout     GraphLayoutSelection
}

type GraphTraversal string

const (
	GraphTraversalNeighbors    GraphTraversal = "neighbors"
	GraphTraversalDependencies GraphTraversal = "dependencies"
	GraphTraversalEvidence     GraphTraversal = "evidence"
)

func (value GraphTraversal) valid() bool {
	return value == GraphTraversalNeighbors || value == GraphTraversalDependencies || value == GraphTraversalEvidence
}

type TaskGraphExpansionQuery struct {
	Scope        GraphQueryScope
	RevisionID   domain.GraphRevisionID
	RootNodeIDs  []domain.NodeID
	Mode         taskgraph.Mode
	Traversal    GraphTraversal
	MaxHops      int
	MaxNodes     int
	MaxEdges     int
	Continuation string
	Layout       GraphLayoutSelection
}

type TaskGraphSearchQuery struct {
	Scope        GraphQueryScope
	RevisionID   domain.GraphRevisionID
	Mode         taskgraph.Mode
	Text         string
	MaxResults   int
	Continuation string
	Layout       GraphLayoutSelection
}

type TaskGraphSearchResult struct {
	Header       GraphRevisionHeader
	Mode         taskgraph.Mode
	Nodes        []GraphNodeRecord
	Continuation string
}

type GraphChangeKind string

const (
	GraphChangeAdded      GraphChangeKind = "added"
	GraphChangeUpdated    GraphChangeKind = "updated"
	GraphChangeRemoved    GraphChangeKind = "removed"
	GraphChangeTombstoned GraphChangeKind = "tombstoned"
)

type GraphEntityKind string

const (
	GraphEntityNode GraphEntityKind = "node"
	GraphEntityEdge GraphEntityKind = "edge"
)

type GraphRevisionChange struct {
	Entity     GraphEntityKind
	Kind       GraphChangeKind
	ID         string
	Before     *GraphNodeRecord
	After      *GraphNodeRecord
	BeforeEdge *GraphEdgeRecord
	AfterEdge  *GraphEdgeRecord
}

type TaskGraphComparisonQuery struct {
	Scope          GraphQueryScope
	FromRevisionID domain.GraphRevisionID
	ToRevisionID   domain.GraphRevisionID
	MaxChanges     int
	Continuation   string
}

type TaskGraphComparison struct {
	From         GraphRevisionHeader
	To           GraphRevisionHeader
	Changes      []GraphRevisionChange
	Continuation string
}

// GraphQueryOperations is the read-only, task-scoped graph repository
// boundary. Every collection operation requires an explicit caller limit.
type GraphQueryOperations interface {
	GetTaskGraphSlice(context.Context, TaskGraphSliceQuery) (TaskGraphSlice, error)
	GetTaskGraphNode(context.Context, TaskGraphNodeQuery) (GraphNodeRecord, error)
	ExpandTaskGraph(context.Context, TaskGraphExpansionQuery) (TaskGraphSlice, error)
	SearchTaskGraph(context.Context, TaskGraphSearchQuery) (TaskGraphSearchResult, error)
	CompareTaskGraphRevisions(context.Context, TaskGraphComparisonQuery) (TaskGraphComparison, error)
}

var _ GraphQueryOperations = (*Repositories)(nil)

type graphContinuation struct {
	Schema     int    `json:"v"`
	Kind       string `json:"k"`
	RevisionA  string `json:"a"`
	RevisionB  string `json:"b,omitempty"`
	Digest     string `json:"d"`
	NodeOffset int    `json:"n"`
	EdgeOffset int    `json:"e,omitempty"`
}

type loadedGraphRevision struct {
	header   GraphRevisionHeader
	revision taskgraph.Revision
	nodes    []GraphNodeRecord
	edges    []GraphEdgeRecord
	nodeByID map[domain.NodeID]GraphNodeRecord
	edgeByID map[domain.EdgeID]GraphEdgeRecord
}

func (repositories *Repositories) GetTaskGraphSlice(ctx context.Context, query TaskGraphSliceQuery) (TaskGraphSlice, error) {
	if err := validateSliceBounds(query.Scope, query.Mode, query.MaxNodes, query.MaxEdges, query.Layout); err != nil {
		return TaskGraphSlice{}, err
	}
	loaded, err := repositories.loadTaskGraphRevision(ctx, query.Scope, nil, query.Layout)
	if err != nil {
		return TaskGraphSlice{}, err
	}
	if query.ExpectedRevisionID != nil && *query.ExpectedRevisionID != loaded.header.RevisionID {
		return TaskGraphSlice{}, typedError(ErrStaleRevision, "get task graph slice", errors.New("graph revision changed"))
	}
	nodes, edges, err := filterGraphMode(loaded, query.Mode)
	if err != nil {
		return TaskGraphSlice{}, fmt.Errorf("filter task graph mode: %w", err)
	}
	digest := graphQueryDigest("slice", string(query.Mode), query.Layout.Algorithm, fmt.Sprint(query.Layout.Version))
	pageNodes, pageEdges, continuation, err := pageGraphRecords(
		"slice", loaded.header.RevisionID, domain.GraphRevisionID{}, digest,
		nodes, edges, query.MaxNodes, query.MaxEdges, query.Continuation,
	)
	if err != nil {
		return TaskGraphSlice{}, err
	}
	return TaskGraphSlice{Header: loaded.header, Mode: query.Mode, Nodes: pageNodes, Edges: pageEdges, Continuation: continuation}, nil
}

func (repositories *Repositories) GetTaskGraphNode(ctx context.Context, query TaskGraphNodeQuery) (GraphNodeRecord, error) {
	if err := query.Scope.validate(); err != nil {
		return GraphNodeRecord{}, err
	}
	if query.RevisionID.IsZero() || query.NodeID.IsZero() {
		return GraphNodeRecord{}, errors.New("graph revision and node identity must not be empty")
	}
	if err := query.Layout.validate(); err != nil {
		return GraphNodeRecord{}, err
	}
	loaded, err := repositories.loadTaskGraphRevision(ctx, query.Scope, &query.RevisionID, query.Layout)
	if err != nil {
		return GraphNodeRecord{}, err
	}
	record, ok := loaded.nodeByID[query.NodeID]
	if !ok {
		return GraphNodeRecord{}, typedError(ErrNotFound, "get task graph node", sql.ErrNoRows)
	}
	return cloneGraphNodeRecord(record), nil
}

func (repositories *Repositories) ExpandTaskGraph(ctx context.Context, query TaskGraphExpansionQuery) (TaskGraphSlice, error) {
	if err := validateSliceBounds(query.Scope, query.Mode, query.MaxNodes, query.MaxEdges, query.Layout); err != nil {
		return TaskGraphSlice{}, err
	}
	if query.RevisionID.IsZero() || !query.Traversal.valid() {
		return TaskGraphSlice{}, errors.New("graph revision and traversal must be valid")
	}
	if query.MaxHops < 1 {
		return TaskGraphSlice{}, ErrGraphQueryUnbounded
	}
	if query.MaxHops > MaximumGraphExpansionHops || len(query.RootNodeIDs) > MaximumGraphExpansionRoots {
		return TaskGraphSlice{}, ErrGraphQueryLimitExceeded
	}
	if len(query.RootNodeIDs) == 0 {
		return TaskGraphSlice{}, errors.New("at least one graph expansion root is required")
	}
	loaded, err := repositories.loadTaskGraphRevision(ctx, query.Scope, &query.RevisionID, query.Layout)
	if err != nil {
		return TaskGraphSlice{}, err
	}
	modeNodes, modeEdges, err := filterGraphMode(loaded, query.Mode)
	if err != nil {
		return TaskGraphSlice{}, err
	}
	nodes, edges, err := expandGraphRecords(modeNodes, modeEdges, query.RootNodeIDs, query.Traversal, query.MaxHops)
	if err != nil {
		return TaskGraphSlice{}, err
	}
	roots := slices.Clone(query.RootNodeIDs)
	sort.Slice(roots, func(i, j int) bool { return roots[i].String() < roots[j].String() })
	rootText := make([]string, len(roots))
	for index, root := range roots {
		rootText[index] = root.String()
	}
	digest := graphQueryDigest("expand", string(query.Mode), string(query.Traversal), fmt.Sprint(query.MaxHops), strings.Join(rootText, ","), query.Layout.Algorithm, fmt.Sprint(query.Layout.Version))
	pageNodes, pageEdges, continuation, err := pageGraphRecords(
		"expand", loaded.header.RevisionID, domain.GraphRevisionID{}, digest,
		nodes, edges, query.MaxNodes, query.MaxEdges, query.Continuation,
	)
	if err != nil {
		return TaskGraphSlice{}, err
	}
	return TaskGraphSlice{Header: loaded.header, Mode: query.Mode, Nodes: pageNodes, Edges: pageEdges, Continuation: continuation}, nil
}

func (repositories *Repositories) SearchTaskGraph(ctx context.Context, query TaskGraphSearchQuery) (TaskGraphSearchResult, error) {
	if err := query.Scope.validate(); err != nil {
		return TaskGraphSearchResult{}, err
	}
	if query.RevisionID.IsZero() || !query.Mode.IsValid() {
		return TaskGraphSearchResult{}, errors.New("graph revision and mode must be valid")
	}
	if query.MaxResults < 1 {
		return TaskGraphSearchResult{}, ErrGraphQueryUnbounded
	}
	if query.MaxResults > MaximumGraphSearchResults {
		return TaskGraphSearchResult{}, ErrGraphQueryLimitExceeded
	}
	if err := query.Layout.validate(); err != nil {
		return TaskGraphSearchResult{}, err
	}
	text := strings.TrimSpace(query.Text)
	if text == "" || len(text) > MaximumGraphSearchTextBytes {
		return TaskGraphSearchResult{}, errors.New("graph search text must be non-empty and bounded")
	}
	loaded, err := repositories.loadTaskGraphRevision(ctx, query.Scope, &query.RevisionID, query.Layout)
	if err != nil {
		return TaskGraphSearchResult{}, err
	}
	nodes, _, err := filterGraphMode(loaded, query.Mode)
	if err != nil {
		return TaskGraphSearchResult{}, err
	}
	needle := strings.ToLower(text)
	matches := make([]GraphNodeRecord, 0)
	for _, record := range nodes {
		if graphNodeMatches(record, needle) {
			matches = append(matches, record)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		leftExact := strings.EqualFold(matches[i].Node.ID().String(), text)
		rightExact := strings.EqualFold(matches[j].Node.ID().String(), text)
		if leftExact != rightExact {
			return leftExact
		}
		return matches[i].Node.ID().String() < matches[j].Node.ID().String()
	})
	digest := graphQueryDigest("search", string(query.Mode), needle, query.Layout.Algorithm, fmt.Sprint(query.Layout.Version))
	token, err := parseGraphContinuation(query.Continuation, "search", loaded.header.RevisionID, domain.GraphRevisionID{}, digest)
	if err != nil {
		return TaskGraphSearchResult{}, err
	}
	if token.NodeOffset > len(matches) {
		return TaskGraphSearchResult{}, ErrGraphContinuation
	}
	end := min(token.NodeOffset+query.MaxResults, len(matches))
	page := cloneGraphNodes(matches[token.NodeOffset:end])
	continuation := ""
	if end < len(matches) {
		continuation, err = encodeGraphContinuation(graphContinuation{Schema: graphContinuationTokenSchema, Kind: "search", RevisionA: loaded.header.RevisionID.String(), Digest: digest, NodeOffset: end})
		if err != nil {
			return TaskGraphSearchResult{}, err
		}
	}
	return TaskGraphSearchResult{Header: loaded.header, Mode: query.Mode, Nodes: page, Continuation: continuation}, nil
}

func (repositories *Repositories) CompareTaskGraphRevisions(ctx context.Context, query TaskGraphComparisonQuery) (TaskGraphComparison, error) {
	if err := query.Scope.validate(); err != nil {
		return TaskGraphComparison{}, err
	}
	if query.FromRevisionID.IsZero() || query.ToRevisionID.IsZero() {
		return TaskGraphComparison{}, errors.New("both graph revision identities are required")
	}
	if query.MaxChanges < 1 {
		return TaskGraphComparison{}, ErrGraphQueryUnbounded
	}
	if query.MaxChanges > MaximumGraphCompareChanges {
		return TaskGraphComparison{}, ErrGraphQueryLimitExceeded
	}
	from, err := repositories.loadTaskGraphRevision(ctx, query.Scope, &query.FromRevisionID, GraphLayoutSelection{})
	if err != nil {
		return TaskGraphComparison{}, err
	}
	to, err := repositories.loadTaskGraphRevision(ctx, query.Scope, &query.ToRevisionID, GraphLayoutSelection{})
	if err != nil {
		return TaskGraphComparison{}, err
	}
	if from.header.GraphID != to.header.GraphID {
		return TaskGraphComparison{}, typedError(ErrNotFound, "compare task graph revisions", sql.ErrNoRows)
	}
	changes := compareLoadedGraphs(from, to)
	digest := graphQueryDigest("compare")
	token, err := parseGraphContinuation(query.Continuation, "compare", from.header.RevisionID, to.header.RevisionID, digest)
	if err != nil {
		return TaskGraphComparison{}, err
	}
	if token.NodeOffset > len(changes) {
		return TaskGraphComparison{}, ErrGraphContinuation
	}
	end := min(token.NodeOffset+query.MaxChanges, len(changes))
	page := slices.Clone(changes[token.NodeOffset:end])
	continuation := ""
	if end < len(changes) {
		continuation, err = encodeGraphContinuation(graphContinuation{Schema: graphContinuationTokenSchema, Kind: "compare", RevisionA: from.header.RevisionID.String(), RevisionB: to.header.RevisionID.String(), Digest: digest, NodeOffset: end})
		if err != nil {
			return TaskGraphComparison{}, err
		}
	}
	return TaskGraphComparison{From: from.header, To: to.header, Changes: page, Continuation: continuation}, nil
}

func validateSliceBounds(scope GraphQueryScope, mode taskgraph.Mode, maxNodes, maxEdges int, layout GraphLayoutSelection) error {
	if err := scope.validate(); err != nil {
		return err
	}
	if !mode.IsValid() {
		return errors.New("graph mode must be valid")
	}
	if maxNodes < 1 || maxEdges < 1 {
		return ErrGraphQueryUnbounded
	}
	if maxNodes > MaximumGraphQueryNodes || maxEdges > MaximumGraphQueryEdges {
		return ErrGraphQueryLimitExceeded
	}
	return layout.validate()
}

func (repositories *Repositories) loadTaskGraphRevision(ctx context.Context, scope GraphQueryScope, revisionID *domain.GraphRevisionID, layout GraphLayoutSelection) (loadedGraphRevision, error) {
	if err := scope.validate(); err != nil {
		return loadedGraphRevision{}, err
	}
	if err := layout.validate(); err != nil {
		return loadedGraphRevision{}, err
	}
	statement := `
		SELECT g.id, gr.id, gr.revision, gr.parent_revision_id,
		       gr.graph_schema_version, gr.created_at_unix_micros,
		       seal.content_sha256, seal.node_count, seal.edge_count
		FROM graph_task_bindings AS binding
		JOIN graphs AS g ON g.id = binding.graph_id
		JOIN graph_revisions AS gr ON gr.graph_id = g.id
		LEFT JOIN graph_revision_seals AS seal ON seal.graph_revision_id = gr.id
		WHERE binding.project_id = ? AND binding.task_id = ?`
	arguments := []any{scope.ProjectID, scope.TaskID}
	if revisionID == nil {
		statement += " AND seal.graph_revision_id IS NOT NULL ORDER BY gr.revision DESC LIMIT 1"
	} else {
		statement += " AND gr.id = ? LIMIT 1"
		arguments = append(arguments, *revisionID)
	}
	var graphRaw, revisionRaw string
	var ordinal, schemaVersion, createdMicros int64
	var parentRaw, contentSHA sql.NullString
	var nodeCount, edgeCount sql.NullInt64
	err := repositories.database.sql.QueryRowContext(ctx, statement, arguments...).Scan(
		&graphRaw, &revisionRaw, &ordinal, &parentRaw, &schemaVersion, &createdMicros,
		&contentSHA, &nodeCount, &edgeCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return loadedGraphRevision{}, typedError(ErrNotFound, "load task graph revision", err)
	}
	if err != nil {
		return loadedGraphRevision{}, classify("load task graph revision", err)
	}
	if !contentSHA.Valid || !nodeCount.Valid || !edgeCount.Valid {
		return loadedGraphRevision{}, typedError(ErrStaleRevision, "load task graph revision", errors.New("graph revision is not sealed"))
	}
	if nodeCount.Int64 > MaximumStoredGraphNodes || edgeCount.Int64 > MaximumStoredGraphEdges {
		return loadedGraphRevision{}, ErrGraphQueryLimitExceeded
	}
	graphID, err := domain.ParseGraphID(graphRaw)
	if err != nil {
		return loadedGraphRevision{}, err
	}
	parsedRevisionID, err := domain.ParseGraphRevisionID(revisionRaw)
	if err != nil {
		return loadedGraphRevision{}, err
	}
	var parentID *domain.GraphRevisionID
	if parentRaw.Valid {
		parsed, parseErr := domain.ParseGraphRevisionID(parentRaw.String)
		if parseErr != nil {
			return loadedGraphRevision{}, parseErr
		}
		parentID = &parsed
	}
	header := GraphRevisionHeader{GraphID: graphID, RevisionID: parsedRevisionID, Ordinal: uint64(ordinal), ParentID: parentID, SchemaVersion: taskgraph.SchemaVersion(schemaVersion), CreatedAt: time.UnixMicro(createdMicros).UTC(), ContentSHA256: contentSHA.String}
	revisionSources, err := repositories.loadGraphSourceLinks(ctx, revisionRaw, "graph_revision_event_links", "", "", scope.TaskID)
	if err != nil {
		return loadedGraphRevision{}, err
	}
	metadata, err := taskgraph.NewRevisionMetadata(parsedRevisionID, graphID, uint64(ordinal), parentID, taskgraph.SchemaVersion(schemaVersion), header.CreatedAt, revisionSources)
	if err != nil {
		return loadedGraphRevision{}, fmt.Errorf("reconstruct graph revision metadata: %w", err)
	}
	nodes, err := repositories.loadGraphNodes(ctx, graphID, parsedRevisionID, scope.TaskID)
	if err != nil {
		return loadedGraphRevision{}, err
	}
	edges, err := repositories.loadGraphEdges(ctx, parsedRevisionID, scope.TaskID)
	if err != nil {
		return loadedGraphRevision{}, err
	}
	activeEdges := make([]taskgraph.Edge, 0, len(edges))
	for _, edge := range edges {
		if !edge.Tombstoned {
			activeEdges = append(activeEdges, edge.Edge)
		}
	}
	domainNodes := make([]taskgraph.Node, len(nodes))
	for i := range nodes {
		domainNodes[i] = nodes[i].Node
	}
	revision, err := taskgraph.NewRevision(metadata, domainNodes, activeEdges)
	if err != nil {
		return loadedGraphRevision{}, fmt.Errorf("reconstruct graph revision: %w", err)
	}
	algorithm, version, hints, err := repositories.loadGraphLayout(ctx, revisionRaw, layout)
	if err != nil {
		return loadedGraphRevision{}, err
	}
	header.LayoutAlgorithm, header.LayoutVersion = algorithm, version
	nodeByID := make(map[domain.NodeID]GraphNodeRecord, len(nodes))
	for index := range nodes {
		if hint, ok := hints[nodes[index].Node.ID()]; ok {
			copy := hint
			nodes[index].Layout = &copy
		}
		nodeByID[nodes[index].Node.ID()] = nodes[index]
	}
	edgeByID := make(map[domain.EdgeID]GraphEdgeRecord, len(edges))
	for _, edge := range edges {
		edgeByID[edge.Edge.ID()] = edge
	}
	return loadedGraphRevision{header: header, revision: revision, nodes: nodes, edges: edges, nodeByID: nodeByID, edgeByID: edgeByID}, nil
}

func (repositories *Repositories) loadGraphNodes(ctx context.Context, graphID domain.GraphID, revisionID domain.GraphRevisionID, taskID domain.TaskID) ([]GraphNodeRecord, error) {
	rows, err := repositories.database.sql.QueryContext(ctx, `
		SELECT node_id, node_class, status, display_name_redacted,
		       contract_purpose_redacted, tombstoned
		FROM graph_node_revisions WHERE graph_revision_id = ?
		ORDER BY node_id`, revisionID)
	if err != nil {
		return nil, classify("load graph nodes", err)
	}
	defer rows.Close()
	type rawNode struct {
		id, class, status, display, purpose string
		tomb                                bool
	}
	raw := make([]rawNode, 0)
	for rows.Next() {
		var value rawNode
		if err := rows.Scan(&value.id, &value.class, &value.status, &value.display, &value.purpose, &value.tomb); err != nil {
			return nil, classify("scan graph node", err)
		}
		raw = append(raw, value)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate graph nodes", err)
	}
	items, err := repositories.loadGraphContractItems(ctx, revisionID)
	if err != nil {
		return nil, err
	}
	result := make([]GraphNodeRecord, 0, len(raw))
	for _, value := range raw {
		nodeID, err := domain.ParseNodeID(value.id)
		if err != nil {
			return nil, err
		}
		contractItems := items[nodeID]
		contract, err := taskgraph.NewContractSummary(value.purpose, contractItems["input"], contractItems["output"], contractItems["effect"])
		if err != nil {
			return nil, err
		}
		sources, err := repositories.loadGraphSourceLinks(ctx, revisionID.String(), "graph_node_event_links", "node_id", value.id, taskID)
		if err != nil {
			return nil, err
		}
		node, err := taskgraph.NewNode(nodeID, taskgraph.NodeClass(value.class), taskgraph.NodeStatus(value.status), value.display, contract, sources)
		if err != nil {
			return nil, err
		}
		messages, err := repositories.loadGraphMessageIDs(ctx, revisionID, nodeID)
		if err != nil {
			return nil, err
		}
		locations, err := repositories.loadGraphLocations(ctx, revisionID, nodeID)
		if err != nil {
			return nil, err
		}
		result = append(result, GraphNodeRecord{Node: node, Tombstoned: value.tomb, MessageIDs: messages, SourceLocations: locations})
	}
	_ = graphID
	return result, nil
}

func (repositories *Repositories) loadGraphEdges(ctx context.Context, revisionID domain.GraphRevisionID, taskID domain.TaskID) ([]GraphEdgeRecord, error) {
	rows, err := repositories.database.sql.QueryContext(ctx, `
		SELECT edge_id, edge_class, source_node_id, target_node_id, tombstoned
		FROM graph_edge_revisions WHERE graph_revision_id = ? ORDER BY edge_id`, revisionID)
	if err != nil {
		return nil, classify("load graph edges", err)
	}
	defer rows.Close()
	type rawEdge struct {
		id, class, from, to string
		tomb                bool
	}
	raw := make([]rawEdge, 0)
	for rows.Next() {
		var value rawEdge
		if err := rows.Scan(&value.id, &value.class, &value.from, &value.to, &value.tomb); err != nil {
			return nil, classify("scan graph edge", err)
		}
		raw = append(raw, value)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate graph edges", err)
	}
	result := make([]GraphEdgeRecord, 0, len(raw))
	for _, value := range raw {
		edgeID, err := domain.ParseEdgeID(value.id)
		if err != nil {
			return nil, err
		}
		fromID, err := domain.ParseNodeID(value.from)
		if err != nil {
			return nil, err
		}
		toID, err := domain.ParseNodeID(value.to)
		if err != nil {
			return nil, err
		}
		sources, err := repositories.loadGraphSourceLinks(ctx, revisionID.String(), "graph_edge_event_links", "edge_id", value.id, taskID)
		if err != nil {
			return nil, err
		}
		edge, err := taskgraph.NewEdge(edgeID, taskgraph.EdgeClass(value.class), fromID, toID, sources)
		if err != nil {
			return nil, err
		}
		result = append(result, GraphEdgeRecord{Edge: edge, Tombstoned: value.tomb})
	}
	return result, nil
}

func (repositories *Repositories) loadGraphContractItems(ctx context.Context, revisionID domain.GraphRevisionID) (map[domain.NodeID]map[string][]string, error) {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT node_id, item_kind, value_redacted FROM graph_node_contract_items WHERE graph_revision_id = ? ORDER BY node_id, item_kind, ordinal`, revisionID)
	if err != nil {
		return nil, classify("load graph contract items", err)
	}
	defer rows.Close()
	result := make(map[domain.NodeID]map[string][]string)
	for rows.Next() {
		var rawID, kind, value string
		if err := rows.Scan(&rawID, &kind, &value); err != nil {
			return nil, classify("scan graph contract item", err)
		}
		id, err := domain.ParseNodeID(rawID)
		if err != nil {
			return nil, err
		}
		if result[id] == nil {
			result[id] = make(map[string][]string)
		}
		result[id][kind] = append(result[id][kind], value)
	}
	return result, rows.Err()
}

func (repositories *Repositories) loadGraphSourceLinks(ctx context.Context, revisionRaw, eventTable, identityColumn, identityRaw string, taskID domain.TaskID) (taskgraph.SourceLinks, error) {
	if eventTable != "graph_revision_event_links" && eventTable != "graph_node_event_links" && eventTable != "graph_edge_event_links" {
		return taskgraph.SourceLinks{}, errors.New("unsupported graph event link table")
	}
	where := "graph_revision_id = ?"
	arguments := []any{revisionRaw}
	if identityColumn != "" {
		where += " AND " + identityColumn + " = ?"
		arguments = append(arguments, identityRaw)
	}
	eventRows, err := repositories.database.sql.QueryContext(ctx, "SELECT event_id FROM "+eventTable+" WHERE "+where+" ORDER BY ordinal", arguments...)
	if err != nil {
		return taskgraph.SourceLinks{}, classify("load graph event links", err)
	}
	eventIDs := make([]domain.EventID, 0)
	for eventRows.Next() {
		var raw string
		if err := eventRows.Scan(&raw); err != nil {
			eventRows.Close()
			return taskgraph.SourceLinks{}, classify("scan graph event link", err)
		}
		id, err := domain.ParseEventID(raw)
		if err != nil {
			eventRows.Close()
			return taskgraph.SourceLinks{}, err
		}
		eventIDs = append(eventIDs, id)
	}
	if err := eventRows.Close(); err != nil {
		return taskgraph.SourceLinks{}, classify("close graph event links", err)
	}
	planTable := strings.Replace(eventTable, "event", "plan_step", 1)
	planRows, err := repositories.database.sql.QueryContext(ctx, "SELECT plan_revision, step_id FROM "+planTable+" WHERE "+where+" ORDER BY ordinal", arguments...)
	if err != nil {
		return taskgraph.SourceLinks{}, classify("load graph plan links", err)
	}
	defer planRows.Close()
	planSteps := make([]taskgraph.PlanStepLink, 0)
	for planRows.Next() {
		var revision uint64
		var step string
		if err := planRows.Scan(&revision, &step); err != nil {
			return taskgraph.SourceLinks{}, classify("scan graph plan link", err)
		}
		planSteps = append(planSteps, taskgraph.PlanStepLink{PlanRevision: revision, StepID: step})
	}
	if err := planRows.Err(); err != nil {
		return taskgraph.SourceLinks{}, classify("iterate graph plan links", err)
	}
	_ = taskID
	return taskgraph.NewSourceLinks(eventIDs, planSteps)
}

func (repositories *Repositories) loadGraphMessageIDs(ctx context.Context, revisionID domain.GraphRevisionID, nodeID domain.NodeID) ([]domain.MessageID, error) {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT message_id FROM graph_message_links WHERE graph_revision_id = ? AND node_id = ? ORDER BY ordinal`, revisionID, nodeID)
	if err != nil {
		return nil, classify("load graph message links", err)
	}
	defer rows.Close()
	result := make([]domain.MessageID, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, classify("scan graph message link", err)
		}
		id, err := domain.ParseMessageID(raw)
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func (repositories *Repositories) loadGraphLocations(ctx context.Context, revisionID domain.GraphRevisionID, nodeID domain.NodeID) ([]GraphSourceLocation, error) {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT repository_id, repository_revision, repository_relative_path, start_line, start_column, end_line, end_column FROM graph_source_links WHERE graph_revision_id = ? AND node_id = ? ORDER BY ordinal`, revisionID, nodeID)
	if err != nil {
		return nil, classify("load graph source locations", err)
	}
	defer rows.Close()
	result := make([]GraphSourceLocation, 0)
	for rows.Next() {
		var repoRaw, revision, path string
		var startLine, startColumn, endLine, endColumn uint64
		if err := rows.Scan(&repoRaw, &revision, &path, &startLine, &startColumn, &endLine, &endColumn); err != nil {
			return nil, classify("scan graph source location", err)
		}
		repoID, err := domain.ParseRepositoryID(repoRaw)
		if err != nil {
			return nil, err
		}
		result = append(result, GraphSourceLocation{RepositoryID: repoID, RepositoryRevision: revision, RelativePath: path, StartLine: startLine, StartColumn: startColumn, EndLine: endLine, EndColumn: endColumn})
	}
	return result, rows.Err()
}

func (repositories *Repositories) loadGraphLayout(ctx context.Context, revisionRaw string, selection GraphLayoutSelection) (string, uint64, map[domain.NodeID]GraphLayoutHint, error) {
	algorithm, version := selection.Algorithm, selection.Version
	if algorithm == "" {
		err := repositories.database.sql.QueryRowContext(ctx, `SELECT algorithm, algorithm_version FROM graph_layout_hints WHERE graph_revision_id = ? GROUP BY algorithm, algorithm_version ORDER BY CASE WHEN algorithm = 'layered-ltr' THEN 0 ELSE 1 END, algorithm, algorithm_version DESC LIMIT 1`, revisionRaw).Scan(&algorithm, &version)
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, map[domain.NodeID]GraphLayoutHint{}, nil
		}
		if err != nil {
			return "", 0, nil, classify("select graph layout", err)
		}
	}
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT node_id, x_milli, y_milli, width_milli, height_milli, rank, sibling_order FROM graph_layout_hints WHERE graph_revision_id = ? AND algorithm = ? AND algorithm_version = ? ORDER BY node_id`, revisionRaw, algorithm, version)
	if err != nil {
		return "", 0, nil, classify("load graph layout", err)
	}
	defer rows.Close()
	result := make(map[domain.NodeID]GraphLayoutHint)
	for rows.Next() {
		var raw string
		var hint GraphLayoutHint
		hint.Algorithm, hint.Version = algorithm, version
		if err := rows.Scan(&raw, &hint.XMilli, &hint.YMilli, &hint.WidthMilli, &hint.HeightMilli, &hint.Rank, &hint.SiblingOrder); err != nil {
			return "", 0, nil, classify("scan graph layout", err)
		}
		id, err := domain.ParseNodeID(raw)
		if err != nil {
			return "", 0, nil, err
		}
		result[id] = hint
	}
	if err := rows.Err(); err != nil {
		return "", 0, nil, classify("iterate graph layout", err)
	}
	if len(result) == 0 && selection.Algorithm != "" {
		return "", 0, nil, typedError(ErrNotFound, "load graph layout", sql.ErrNoRows)
	}
	return algorithm, version, result, nil
}

func filterGraphMode(loaded loadedGraphRevision, mode taskgraph.Mode) ([]GraphNodeRecord, []GraphEdgeRecord, error) {
	visible, err := taskgraph.DeriveModeVisibility(loaded.revision, mode)
	if err != nil {
		return nil, nil, err
	}
	nodes := make([]GraphNodeRecord, 0, len(visible.Nodes))
	for _, node := range visible.Nodes {
		nodes = append(nodes, cloneGraphNodeRecord(loaded.nodeByID[node.ID()]))
	}
	edges := make([]GraphEdgeRecord, 0, len(visible.Edges))
	for _, edge := range visible.Edges {
		edges = append(edges, loaded.edgeByID[edge.ID()])
	}
	return nodes, edges, nil
}

func expandGraphRecords(nodes []GraphNodeRecord, edges []GraphEdgeRecord, roots []domain.NodeID, traversal GraphTraversal, maxHops int) ([]GraphNodeRecord, []GraphEdgeRecord, error) {
	nodeMap := make(map[domain.NodeID]GraphNodeRecord, len(nodes))
	for _, node := range nodes {
		nodeMap[node.Node.ID()] = node
	}
	rootSet := make(map[domain.NodeID]struct{}, len(roots))
	frontier := make([]domain.NodeID, 0, len(roots))
	for _, root := range roots {
		if _, duplicate := rootSet[root]; duplicate {
			continue
		}
		if _, ok := nodeMap[root]; !ok {
			return nil, nil, typedError(ErrNotFound, "expand task graph", sql.ErrNoRows)
		}
		rootSet[root] = struct{}{}
		frontier = append(frontier, root)
	}
	sort.Slice(frontier, func(i, j int) bool { return frontier[i].String() < frontier[j].String() })
	visited := make(map[domain.NodeID]struct{}, len(frontier))
	for _, root := range frontier {
		visited[root] = struct{}{}
	}
	eligible := func(edge GraphEdgeRecord) bool {
		return traversal != GraphTraversalEvidence || edge.Edge.Class() == taskgraph.EdgeClassDataProvenance || edge.Edge.Class() == taskgraph.EdgeClassEvidenceDependency || edge.Edge.Class() == taskgraph.EdgeClassReconciliation || edge.Edge.Class() == taskgraph.EdgeClassCompensation
	}
	for hop := 0; hop < maxHops && len(frontier) > 0; hop++ {
		nextSet := make(map[domain.NodeID]struct{})
		for _, current := range frontier {
			for _, edge := range edges {
				if !eligible(edge) {
					continue
				}
				var next domain.NodeID
				matched := false
				if traversal == GraphTraversalNeighbors {
					if edge.Edge.FromNode() == current {
						next, matched = edge.Edge.ToNode(), true
					} else if edge.Edge.ToNode() == current {
						next, matched = edge.Edge.FromNode(), true
					}
				} else if edge.Edge.ToNode() == current {
					next, matched = edge.Edge.FromNode(), true
				}
				if matched {
					if _, seen := visited[next]; !seen {
						nextSet[next] = struct{}{}
					}
				}
			}
		}
		frontier = frontier[:0]
		for id := range nextSet {
			frontier = append(frontier, id)
			visited[id] = struct{}{}
		}
		sort.Slice(frontier, func(i, j int) bool { return frontier[i].String() < frontier[j].String() })
	}
	resultNodes := make([]GraphNodeRecord, 0, len(visited))
	for id := range visited {
		resultNodes = append(resultNodes, cloneGraphNodeRecord(nodeMap[id]))
	}
	sort.Slice(resultNodes, func(i, j int) bool { return resultNodes[i].Node.ID().String() < resultNodes[j].Node.ID().String() })
	resultEdges := make([]GraphEdgeRecord, 0)
	for _, edge := range edges {
		_, from := visited[edge.Edge.FromNode()]
		_, to := visited[edge.Edge.ToNode()]
		if from && to && eligible(edge) {
			resultEdges = append(resultEdges, edge)
		}
	}
	sort.Slice(resultEdges, func(i, j int) bool { return resultEdges[i].Edge.ID().String() < resultEdges[j].Edge.ID().String() })
	return resultNodes, resultEdges, nil
}

func pageGraphRecords(kind string, revisionA, revisionB domain.GraphRevisionID, digest string, nodes []GraphNodeRecord, edges []GraphEdgeRecord, maxNodes, maxEdges int, rawToken string) ([]GraphNodeRecord, []GraphEdgeRecord, string, error) {
	token, err := parseGraphContinuation(rawToken, kind, revisionA, revisionB, digest)
	if err != nil {
		return nil, nil, "", err
	}
	if token.NodeOffset > len(nodes) || token.EdgeOffset > len(edges) {
		return nil, nil, "", ErrGraphContinuation
	}
	nodeEnd := min(token.NodeOffset+maxNodes, len(nodes))
	pageNodes := cloneGraphNodes(nodes[token.NodeOffset:nodeEnd])
	selected := make(map[domain.NodeID]struct{}, len(pageNodes))
	for _, node := range pageNodes {
		selected[node.Node.ID()] = struct{}{}
	}
	eligibleEdges := make([]GraphEdgeRecord, 0)
	for _, edge := range edges {
		_, from := selected[edge.Edge.FromNode()]
		_, to := selected[edge.Edge.ToNode()]
		if from && to {
			eligibleEdges = append(eligibleEdges, edge)
		}
	}
	edgeOffset := token.EdgeOffset
	if edgeOffset > len(eligibleEdges) {
		return nil, nil, "", ErrGraphContinuation
	}
	edgeEnd := min(edgeOffset+maxEdges, len(eligibleEdges))
	pageEdges := slices.Clone(eligibleEdges[edgeOffset:edgeEnd])
	continuation := ""
	next := graphContinuation{Schema: graphContinuationTokenSchema, Kind: kind, RevisionA: revisionA.String(), Digest: digest}
	if !revisionB.IsZero() {
		next.RevisionB = revisionB.String()
	}
	if edgeEnd < len(eligibleEdges) {
		next.NodeOffset, next.EdgeOffset = token.NodeOffset, edgeEnd
	} else if nodeEnd < len(nodes) {
		next.NodeOffset = nodeEnd
	}
	if next.NodeOffset != 0 || next.EdgeOffset != 0 {
		continuation, err = encodeGraphContinuation(next)
		if err != nil {
			return nil, nil, "", err
		}
	}
	return pageNodes, pageEdges, continuation, nil
}

func graphNodeMatches(record GraphNodeRecord, needle string) bool {
	values := []string{record.Node.ID().String(), record.Node.DisplayName(), record.Node.Contract().Purpose()}
	values = append(values, record.Node.Contract().Inputs()...)
	values = append(values, record.Node.Contract().Outputs()...)
	values = append(values, record.Node.Contract().Effects()...)
	for _, id := range record.MessageIDs {
		values = append(values, id.String())
	}
	for _, location := range record.SourceLocations {
		values = append(values, location.RelativePath, location.RepositoryRevision)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func compareLoadedGraphs(from, to loadedGraphRevision) []GraphRevisionChange {
	changes := make([]GraphRevisionChange, 0)
	nodeIDs := make(map[domain.NodeID]struct{}, len(from.nodeByID)+len(to.nodeByID))
	for id := range from.nodeByID {
		nodeIDs[id] = struct{}{}
	}
	for id := range to.nodeByID {
		nodeIDs[id] = struct{}{}
	}
	for id := range nodeIDs {
		before, beforeOK := from.nodeByID[id]
		after, afterOK := to.nodeByID[id]
		kind := GraphChangeUpdated
		switch {
		case !beforeOK:
			kind = GraphChangeAdded
		case !afterOK:
			kind = GraphChangeRemoved
		case !before.Tombstoned && after.Tombstoned:
			kind = GraphChangeTombstoned
		case equalGraphNodeRecords(before, after):
			continue
		}
		change := GraphRevisionChange{Entity: GraphEntityNode, Kind: kind, ID: id.String()}
		if beforeOK {
			copy := cloneGraphNodeRecord(before)
			change.Before = &copy
		}
		if afterOK {
			copy := cloneGraphNodeRecord(after)
			change.After = &copy
		}
		changes = append(changes, change)
	}
	edgeIDs := make(map[domain.EdgeID]struct{}, len(from.edgeByID)+len(to.edgeByID))
	for id := range from.edgeByID {
		edgeIDs[id] = struct{}{}
	}
	for id := range to.edgeByID {
		edgeIDs[id] = struct{}{}
	}
	for id := range edgeIDs {
		before, beforeOK := from.edgeByID[id]
		after, afterOK := to.edgeByID[id]
		kind := GraphChangeUpdated
		switch {
		case !beforeOK:
			kind = GraphChangeAdded
		case !afterOK:
			kind = GraphChangeRemoved
		case !before.Tombstoned && after.Tombstoned:
			kind = GraphChangeTombstoned
		case equalGraphEdgeRecords(before, after):
			continue
		}
		change := GraphRevisionChange{Entity: GraphEntityEdge, Kind: kind, ID: id.String()}
		if beforeOK {
			copy := before
			change.BeforeEdge = &copy
		}
		if afterOK {
			copy := after
			change.AfterEdge = &copy
		}
		changes = append(changes, change)
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Entity != changes[j].Entity {
			return changes[i].Entity < changes[j].Entity
		}
		return changes[i].ID < changes[j].ID
	})
	return changes
}

func equalGraphNodeRecords(left, right GraphNodeRecord) bool {
	return left.Tombstoned == right.Tombstoned && left.Node.Class() == right.Node.Class() && left.Node.Status() == right.Node.Status() && left.Node.DisplayName() == right.Node.DisplayName() && reflect.DeepEqual(left.Node.Contract().Inputs(), right.Node.Contract().Inputs()) && reflect.DeepEqual(left.Node.Contract().Outputs(), right.Node.Contract().Outputs()) && reflect.DeepEqual(left.Node.Contract().Effects(), right.Node.Contract().Effects()) && left.Node.Contract().Purpose() == right.Node.Contract().Purpose() && reflect.DeepEqual(left.Node.Sources(), right.Node.Sources()) && reflect.DeepEqual(left.MessageIDs, right.MessageIDs) && reflect.DeepEqual(left.SourceLocations, right.SourceLocations)
}

func equalGraphEdgeRecords(left, right GraphEdgeRecord) bool {
	return left.Tombstoned == right.Tombstoned && left.Edge.Class() == right.Edge.Class() && left.Edge.FromNode() == right.Edge.FromNode() && left.Edge.ToNode() == right.Edge.ToNode() && reflect.DeepEqual(left.Edge.Sources(), right.Edge.Sources())
}

func cloneGraphNodeRecord(value GraphNodeRecord) GraphNodeRecord {
	clone := value
	clone.MessageIDs = slices.Clone(value.MessageIDs)
	clone.SourceLocations = slices.Clone(value.SourceLocations)
	if value.Layout != nil {
		hint := *value.Layout
		clone.Layout = &hint
	}
	return clone
}
func cloneGraphNodes(values []GraphNodeRecord) []GraphNodeRecord {
	result := make([]GraphNodeRecord, len(values))
	for index := range values {
		result[index] = cloneGraphNodeRecord(values[index])
	}
	return result
}

func graphQueryDigest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func encodeGraphContinuation(value graphContinuation) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + hex.EncodeToString(sum[:]), nil
}

func parseGraphContinuation(raw, kind string, revisionA, revisionB domain.GraphRevisionID, digest string) (graphContinuation, error) {
	if raw == "" {
		return graphContinuation{Schema: graphContinuationTokenSchema, Kind: kind, RevisionA: revisionA.String(), RevisionB: revisionB.String(), Digest: digest}, nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return graphContinuation{}, ErrGraphContinuation
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payload) > 1024 {
		return graphContinuation{}, ErrGraphContinuation
	}
	sum := sha256.Sum256(payload)
	if !strings.EqualFold(parts[1], hex.EncodeToString(sum[:])) {
		return graphContinuation{}, ErrGraphContinuation
	}
	var value graphContinuation
	if err := json.Unmarshal(payload, &value); err != nil {
		return graphContinuation{}, ErrGraphContinuation
	}
	if value.Schema != graphContinuationTokenSchema || value.Kind != kind || value.RevisionA != revisionA.String() || value.RevisionB != revisionB.String() || value.Digest != digest || value.NodeOffset < 0 || value.EdgeOffset < 0 {
		return graphContinuation{}, ErrGraphContinuation
	}
	return value, nil
}
