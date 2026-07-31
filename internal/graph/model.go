package graph

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	MaximumNodeDisplayNameBytes = 256
	MaximumContractPurposeBytes = 1024
	MaximumContractItems        = 32
	MaximumContractItemBytes    = 512
	MaximumSourceEventLinks     = 64
	MaximumPlanStepLinks        = 64
	MaximumPlanStepIDBytes      = 255
)

var ErrInvalidGraphModel = errors.New("invalid graph model")

// ValidationError reports a structural graph field and a safe reason without
// copying untrusted graph content into diagnostics.
type ValidationError struct {
	Field  string
	Reason string
}

func (failure *ValidationError) Error() string {
	return fmt.Sprintf("%s is invalid: %s", failure.Field, failure.Reason)
}

func (failure *ValidationError) Unwrap() error { return ErrInvalidGraphModel }

// SchemaVersion versions the graph domain independently from SQLite schema
// and application release versions.
type SchemaVersion uint32

const (
	SchemaVersionV1      SchemaVersion = 1
	CurrentSchemaVersion               = SchemaVersionV1
)

func (version SchemaVersion) IsValid() bool { return version == SchemaVersionV1 }

// MetadataPolicy prohibits arbitrary metadata maps in graph schema v1. New
// typed fields require a graph schema version change instead.
type MetadataPolicy string

const MetadataPolicyTypedFieldsOnly MetadataPolicy = "typed-fields-only"

func (policy MetadataPolicy) IsValid() bool { return policy == MetadataPolicyTypedFieldsOnly }

// NodeClass is the explanatory role of one stable task-graph node.
type NodeClass string

const (
	NodeClassRequirement      NodeClass = "requirement"
	NodeClassPlanRegion       NodeClass = "plan-region"
	NodeClassAtomOperation    NodeClass = "atom-operation"
	NodeClassEffect           NodeClass = "effect"
	NodeClassBranchMatchMerge NodeClass = "branch-match-merge"
	NodeClassObligation       NodeClass = "obligation"
	NodeClassArtifactResult   NodeClass = "artifact-result"
)

func (class NodeClass) IsValid() bool {
	switch class {
	case NodeClassRequirement, NodeClassPlanRegion, NodeClassAtomOperation,
		NodeClassEffect, NodeClassBranchMatchMerge, NodeClassObligation,
		NodeClassArtifactResult:
		return true
	default:
		return false
	}
}

func AllNodeClasses() []NodeClass {
	return []NodeClass{
		NodeClassRequirement, NodeClassPlanRegion, NodeClassAtomOperation,
		NodeClassEffect, NodeClassBranchMatchMerge, NodeClassObligation,
		NodeClassArtifactResult,
	}
}

// EdgeClass is the semantic relationship carried by one stable graph edge.
type EdgeClass string

const (
	EdgeClassControl            EdgeClass = "control"
	EdgeClassDataProvenance     EdgeClass = "data-provenance"
	EdgeClassEvidenceDependency EdgeClass = "evidence-dependency"
	EdgeClassRetry              EdgeClass = "retry"
	EdgeClassReconciliation     EdgeClass = "reconciliation"
	EdgeClassCompensation       EdgeClass = "compensation"
)

func (class EdgeClass) IsValid() bool {
	switch class {
	case EdgeClassControl, EdgeClassDataProvenance, EdgeClassEvidenceDependency,
		EdgeClassRetry, EdgeClassReconciliation, EdgeClassCompensation:
		return true
	default:
		return false
	}
}

func AllEdgeClasses() []EdgeClass {
	return []EdgeClass{
		EdgeClassControl, EdgeClassDataProvenance, EdgeClassEvidenceDependency,
		EdgeClassRetry, EdgeClassReconciliation, EdgeClassCompensation,
	}
}

// NodeStatus is the explicit execution or evidence state of a graph node.
type NodeStatus string

const (
	NodeStatusPending     NodeStatus = "pending"
	NodeStatusActive      NodeStatus = "active"
	NodeStatusPassed      NodeStatus = "passed"
	NodeStatusWarning     NodeStatus = "warning"
	NodeStatusFailed      NodeStatus = "failed"
	NodeStatusBlocked     NodeStatus = "blocked"
	NodeStatusInvalidated NodeStatus = "invalidated"
)

func (status NodeStatus) IsValid() bool {
	switch status {
	case NodeStatusPending, NodeStatusActive, NodeStatusPassed, NodeStatusWarning,
		NodeStatusFailed, NodeStatusBlocked, NodeStatusInvalidated:
		return true
	default:
		return false
	}
}

func AllNodeStatuses() []NodeStatus {
	return []NodeStatus{
		NodeStatusPending, NodeStatusActive, NodeStatusPassed, NodeStatusWarning,
		NodeStatusFailed, NodeStatusBlocked, NodeStatusInvalidated,
	}
}

// Mode selects one bounded projection over the same task-scoped graph.
type Mode string

const (
	ModeProgram   Mode = "program"
	ModeExecution Mode = "execution"
	ModeEvidence  Mode = "evidence"
)

func (mode Mode) IsValid() bool {
	return mode == ModeProgram || mode == ModeExecution || mode == ModeEvidence
}

func AllModes() []Mode { return []Mode{ModeProgram, ModeExecution, ModeEvidence} }

// Graph is the stable task-scoped identity record. Revisions refer to ID and
// never derive identity from names, content, source positions, or order.
type Graph struct {
	id        domain.GraphID
	taskID    domain.TaskID
	createdAt time.Time
}

func NewGraph(id domain.GraphID, taskID domain.TaskID, createdAt time.Time) (Graph, error) {
	if id.IsZero() {
		return Graph{}, invalid("graph.id", "must not be empty")
	}
	if taskID.IsZero() {
		return Graph{}, invalid("graph.task_id", "must not be empty")
	}
	if !validUTCTime(createdAt) {
		return Graph{}, invalid("graph.created_at", "must be a non-zero UTC timestamp")
	}
	return Graph{id: id, taskID: taskID, createdAt: createdAt}, nil
}

func (graph Graph) ID() domain.GraphID    { return graph.id }
func (graph Graph) TaskID() domain.TaskID { return graph.taskID }
func (graph Graph) CreatedAt() time.Time  { return graph.createdAt }

func (graph Graph) Validate() error {
	_, err := NewGraph(graph.id, graph.taskID, graph.createdAt)
	return err
}

// PlanStepLink binds graph material to one immutable plan revision and stable
// plan-step identifier.
type PlanStepLink struct {
	PlanRevision uint64
	StepID       string
}

func (link PlanStepLink) Validate() error {
	if link.PlanRevision == 0 {
		return invalid("source.plan_step.plan_revision", "must be greater than zero")
	}
	return validateBoundedText("source.plan_step.step_id", link.StepID, MaximumPlanStepIDBytes)
}

// SourceLinks are bounded typed provenance links. Accessors return clones so
// accepted revision values cannot be changed through caller-owned slices.
type SourceLinks struct {
	eventIDs  []domain.EventID
	planSteps []PlanStepLink
}

func NewSourceLinks(eventIDs []domain.EventID, planSteps []PlanStepLink) (SourceLinks, error) {
	if len(eventIDs) > MaximumSourceEventLinks {
		return SourceLinks{}, invalid("source.event_ids", "exceeds the link limit")
	}
	if len(planSteps) > MaximumPlanStepLinks {
		return SourceLinks{}, invalid("source.plan_steps", "exceeds the link limit")
	}
	seenEvents := make(map[domain.EventID]struct{}, len(eventIDs))
	for _, eventID := range eventIDs {
		if eventID.IsZero() {
			return SourceLinks{}, invalid("source.event_ids", "contains an empty identity")
		}
		if _, duplicate := seenEvents[eventID]; duplicate {
			return SourceLinks{}, invalid("source.event_ids", "contains a duplicate identity")
		}
		seenEvents[eventID] = struct{}{}
	}
	seenSteps := make(map[PlanStepLink]struct{}, len(planSteps))
	for _, link := range planSteps {
		if err := link.Validate(); err != nil {
			return SourceLinks{}, err
		}
		if _, duplicate := seenSteps[link]; duplicate {
			return SourceLinks{}, invalid("source.plan_steps", "contains a duplicate link")
		}
		seenSteps[link] = struct{}{}
	}
	return SourceLinks{
		eventIDs: slices.Clone(eventIDs), planSteps: slices.Clone(planSteps),
	}, nil
}

func (links SourceLinks) EventIDs() []domain.EventID { return slices.Clone(links.eventIDs) }
func (links SourceLinks) PlanSteps() []PlanStepLink  { return slices.Clone(links.planSteps) }
func (links SourceLinks) Validate() error {
	_, err := NewSourceLinks(links.eventIDs, links.planSteps)
	return err
}

// ContractSummary describes only the shallow behavior needed by the
// explanatory graph and inspector. It does not claim a deep atom contract or
// establish assurance by itself.
type ContractSummary struct {
	purpose string
	inputs  []string
	outputs []string
	effects []string
}

func NewContractSummary(purpose string, inputs, outputs, effects []string) (ContractSummary, error) {
	if purpose == "" && len(inputs) == 0 && len(outputs) == 0 && len(effects) == 0 {
		return ContractSummary{}, nil
	}
	if err := validateBoundedText("contract.purpose", purpose, MaximumContractPurposeBytes); err != nil {
		return ContractSummary{}, err
	}
	normalizedInputs, err := validateContractItems("contract.inputs", inputs)
	if err != nil {
		return ContractSummary{}, err
	}
	normalizedOutputs, err := validateContractItems("contract.outputs", outputs)
	if err != nil {
		return ContractSummary{}, err
	}
	normalizedEffects, err := validateContractItems("contract.effects", effects)
	if err != nil {
		return ContractSummary{}, err
	}
	return ContractSummary{
		purpose: purpose, inputs: normalizedInputs, outputs: normalizedOutputs, effects: normalizedEffects,
	}, nil
}

func (summary ContractSummary) Purpose() string   { return summary.purpose }
func (summary ContractSummary) Inputs() []string  { return slices.Clone(summary.inputs) }
func (summary ContractSummary) Outputs() []string { return slices.Clone(summary.outputs) }
func (summary ContractSummary) Effects() []string { return slices.Clone(summary.effects) }
func (summary ContractSummary) IsZero() bool {
	return summary.purpose == "" && len(summary.inputs) == 0 && len(summary.outputs) == 0 && len(summary.effects) == 0
}
func (summary ContractSummary) Validate() error {
	_, err := NewContractSummary(summary.purpose, summary.inputs, summary.outputs, summary.effects)
	return err
}

// Node is one immutable node description within a graph revision. ID remains
// stable across revisions while the remaining fields may change by creating a
// new graph revision.
type Node struct {
	id          domain.NodeID
	class       NodeClass
	status      NodeStatus
	displayName string
	contract    ContractSummary
	sources     SourceLinks
}

func NewNode(
	id domain.NodeID,
	class NodeClass,
	status NodeStatus,
	displayName string,
	contract ContractSummary,
	sources SourceLinks,
) (Node, error) {
	if id.IsZero() {
		return Node{}, invalid("node.id", "must not be empty")
	}
	if !class.IsValid() {
		return Node{}, invalid("node.class", "is not supported")
	}
	if !status.IsValid() {
		return Node{}, invalid("node.status", "is not supported")
	}
	if err := validateBoundedText("node.display_name", displayName, MaximumNodeDisplayNameBytes); err != nil {
		return Node{}, err
	}
	if err := contract.Validate(); err != nil {
		return Node{}, err
	}
	if err := sources.Validate(); err != nil {
		return Node{}, err
	}
	return Node{id: id, class: class, status: status, displayName: displayName, contract: contract, sources: sources}, nil
}

func (node Node) ID() domain.NodeID         { return node.id }
func (node Node) Class() NodeClass          { return node.class }
func (node Node) Status() NodeStatus        { return node.status }
func (node Node) DisplayName() string       { return node.displayName }
func (node Node) Contract() ContractSummary { return node.contract }
func (node Node) Sources() SourceLinks      { return node.sources }
func (node Node) Validate() error {
	_, err := NewNode(node.id, node.class, node.status, node.displayName, node.contract, node.sources)
	return err
}

// Edge is one immutable relationship within a graph revision. Its identity is
// independent from endpoint order and descriptive content.
type Edge struct {
	id       domain.EdgeID
	class    EdgeClass
	fromNode domain.NodeID
	toNode   domain.NodeID
	sources  SourceLinks
}

func NewEdge(id domain.EdgeID, class EdgeClass, fromNode, toNode domain.NodeID, sources SourceLinks) (Edge, error) {
	if id.IsZero() {
		return Edge{}, invalid("edge.id", "must not be empty")
	}
	if !class.IsValid() {
		return Edge{}, invalid("edge.class", "is not supported")
	}
	if fromNode.IsZero() || toNode.IsZero() {
		return Edge{}, invalid("edge.endpoints", "must not contain an empty node identity")
	}
	if err := sources.Validate(); err != nil {
		return Edge{}, err
	}
	return Edge{id: id, class: class, fromNode: fromNode, toNode: toNode, sources: sources}, nil
}

func (edge Edge) ID() domain.EdgeID       { return edge.id }
func (edge Edge) Class() EdgeClass        { return edge.class }
func (edge Edge) FromNode() domain.NodeID { return edge.fromNode }
func (edge Edge) ToNode() domain.NodeID   { return edge.toNode }
func (edge Edge) Sources() SourceLinks    { return edge.sources }
func (edge Edge) Validate() error {
	_, err := NewEdge(edge.id, edge.class, edge.fromNode, edge.toNode, edge.sources)
	return err
}

// RevisionMetadata binds one immutable graph revision to its stable graph,
// parent revision, independent schema version, typed provenance, and UTC time.
type RevisionMetadata struct {
	id             domain.GraphRevisionID
	graphID        domain.GraphID
	ordinal        uint64
	parentID       *domain.GraphRevisionID
	schemaVersion  SchemaVersion
	metadataPolicy MetadataPolicy
	createdAt      time.Time
	sources        SourceLinks
}

func NewRevisionMetadata(
	id domain.GraphRevisionID,
	graphID domain.GraphID,
	ordinal uint64,
	parentID *domain.GraphRevisionID,
	schemaVersion SchemaVersion,
	createdAt time.Time,
	sources SourceLinks,
) (RevisionMetadata, error) {
	if id.IsZero() {
		return RevisionMetadata{}, invalid("revision.id", "must not be empty")
	}
	if graphID.IsZero() {
		return RevisionMetadata{}, invalid("revision.graph_id", "must not be empty")
	}
	if ordinal == 0 {
		return RevisionMetadata{}, invalid("revision.ordinal", "must be greater than zero")
	}
	if (ordinal == 1 && parentID != nil) || (ordinal > 1 && parentID == nil) {
		return RevisionMetadata{}, invalid("revision.parent_id", "must be absent only for the first revision")
	}
	if parentID != nil && (parentID.IsZero() || *parentID == id) {
		return RevisionMetadata{}, invalid("revision.parent_id", "must identify a distinct prior revision")
	}
	if !schemaVersion.IsValid() {
		return RevisionMetadata{}, invalid("revision.schema_version", "is not supported")
	}
	if !validUTCTime(createdAt) {
		return RevisionMetadata{}, invalid("revision.created_at", "must be a non-zero UTC timestamp")
	}
	if err := sources.Validate(); err != nil {
		return RevisionMetadata{}, err
	}
	var parentCopy *domain.GraphRevisionID
	if parentID != nil {
		value := *parentID
		parentCopy = &value
	}
	return RevisionMetadata{
		id: id, graphID: graphID, ordinal: ordinal, parentID: parentCopy,
		schemaVersion: schemaVersion, metadataPolicy: MetadataPolicyTypedFieldsOnly,
		createdAt: createdAt, sources: sources,
	}, nil
}

func (metadata RevisionMetadata) ID() domain.GraphRevisionID     { return metadata.id }
func (metadata RevisionMetadata) GraphID() domain.GraphID        { return metadata.graphID }
func (metadata RevisionMetadata) Ordinal() uint64                { return metadata.ordinal }
func (metadata RevisionMetadata) SchemaVersion() SchemaVersion   { return metadata.schemaVersion }
func (metadata RevisionMetadata) MetadataPolicy() MetadataPolicy { return metadata.metadataPolicy }
func (metadata RevisionMetadata) CreatedAt() time.Time           { return metadata.createdAt }
func (metadata RevisionMetadata) Sources() SourceLinks           { return metadata.sources }
func (metadata RevisionMetadata) ParentID() (domain.GraphRevisionID, bool) {
	if metadata.parentID == nil {
		return domain.GraphRevisionID{}, false
	}
	return *metadata.parentID, true
}
func (metadata RevisionMetadata) Validate() error {
	_, err := NewRevisionMetadata(
		metadata.id, metadata.graphID, metadata.ordinal, metadata.parentID,
		metadata.schemaVersion, metadata.createdAt, metadata.sources,
	)
	if err == nil && !metadata.metadataPolicy.IsValid() {
		return invalid("revision.metadata_policy", "is not supported")
	}
	return err
}

// Revision is a complete immutable task-graph snapshot. It rejects duplicate
// stable identities and edges whose endpoints are absent from the revision.
type Revision struct {
	metadata RevisionMetadata
	nodes    []Node
	edges    []Edge
}

func NewRevision(metadata RevisionMetadata, nodes []Node, edges []Edge) (Revision, error) {
	if err := metadata.Validate(); err != nil {
		return Revision{}, err
	}
	seenNodes := make(map[domain.NodeID]struct{}, len(nodes))
	for _, node := range nodes {
		if err := node.Validate(); err != nil {
			return Revision{}, err
		}
		if _, duplicate := seenNodes[node.ID()]; duplicate {
			return Revision{}, invalid("revision.nodes", "contains a duplicate stable identity")
		}
		seenNodes[node.ID()] = struct{}{}
	}
	seenEdges := make(map[domain.EdgeID]struct{}, len(edges))
	for _, edge := range edges {
		if err := edge.Validate(); err != nil {
			return Revision{}, err
		}
		if _, duplicate := seenEdges[edge.ID()]; duplicate {
			return Revision{}, invalid("revision.edges", "contains a duplicate stable identity")
		}
		seenEdges[edge.ID()] = struct{}{}
		if _, ok := seenNodes[edge.FromNode()]; !ok {
			return Revision{}, invalid("revision.edges", "references an absent source node")
		}
		if _, ok := seenNodes[edge.ToNode()]; !ok {
			return Revision{}, invalid("revision.edges", "references an absent destination node")
		}
	}
	return Revision{metadata: metadata, nodes: slices.Clone(nodes), edges: slices.Clone(edges)}, nil
}

func (revision Revision) Metadata() RevisionMetadata { return revision.metadata }
func (revision Revision) Nodes() []Node              { return slices.Clone(revision.nodes) }
func (revision Revision) Edges() []Edge              { return slices.Clone(revision.edges) }
func (revision Revision) Validate() error {
	_, err := NewRevision(revision.metadata, revision.nodes, revision.edges)
	return err
}

func validateContractItems(field string, values []string) ([]string, error) {
	if len(values) > MaximumContractItems {
		return nil, invalid(field, "exceeds the item limit")
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateBoundedText(field, value, MaximumContractItemBytes); err != nil {
			return nil, err
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, invalid(field, "contains a duplicate item")
		}
		seen[value] = struct{}{}
	}
	return slices.Clone(values), nil
}

func validateBoundedText(field, value string, maximumBytes int) error {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) || len(value) > maximumBytes {
		return invalid(field, "must be non-empty, trimmed, valid UTF-8, and bounded")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return invalid(field, "must not contain control characters")
		}
	}
	return nil
}

func validUTCTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}

func invalid(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}
