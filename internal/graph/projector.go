package graph

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	MaximumProjectedPlanSteps = 64
	DefaultPatchNodeLimit     = 256
	DefaultPatchEdgeLimit     = 512
)

var (
	ErrInvalidProjectionEvent = errors.New("invalid graph projection event")
	ErrProjectionPending      = errors.New("graph projection change is awaiting commit")
	ErrPatchLimitExceeded     = errors.New("graph patch limit exceeded")
)

// ProjectionEventKind is a closed normalization of durable session and task
// facts. Producers classify facts from their typed durable records; the graph
// projector never infers semantics from redacted text or command names.
type ProjectionEventKind string

const (
	ProjectionRequirementAccepted ProjectionEventKind = "requirement-accepted"
	ProjectionPlanDeclared        ProjectionEventKind = "plan-declared"
	ProjectionOperationObserved   ProjectionEventKind = "operation-observed"
	ProjectionEffectObserved      ProjectionEventKind = "effect-observed"
	ProjectionApprovalBoundary    ProjectionEventKind = "approval-boundary"
	ProjectionValidationObserved  ProjectionEventKind = "validation-observed"
	ProjectionArtifactObserved    ProjectionEventKind = "artifact-observed"
	ProjectionRetryObserved       ProjectionEventKind = "retry-observed"
	ProjectionRecoveryObserved    ProjectionEventKind = "recovery-observed"
	ProjectionTokenDelta          ProjectionEventKind = "token-delta"
)

func (kind ProjectionEventKind) IsValid() bool {
	switch kind {
	case ProjectionRequirementAccepted, ProjectionPlanDeclared,
		ProjectionOperationObserved, ProjectionEffectObserved,
		ProjectionApprovalBoundary, ProjectionValidationObserved,
		ProjectionArtifactObserved, ProjectionRetryObserved,
		ProjectionRecoveryObserved, ProjectionTokenDelta:
		return true
	default:
		return false
	}
}

// ProjectionEvent is one ordered normalized fact. SourceEventID is the
// immutable task-event identity; node, edge, and revision identities are
// allocated outside this pure projector and remain stable across replay.
type ProjectionEvent struct {
	Sequence      uint64
	SourceEventID domain.EventID
	TaskID        domain.TaskID
	OccurredAt    time.Time
	Kind          ProjectionEventKind

	Requirement *RequirementFact
	Plan        *PlanFact
	Operation   *OperationFact
	Effect      *EffectFact
	Approval    *ApprovalFact
	Validation  *ValidationFact
	Artifact    *ArtifactFact
	Retry       *RetryFact
	Recovery    *RecoveryFact
	TokenDelta  *TokenDeltaFact
}

// SessionEvent names the normalized boundary used by ProjectSessionEvent.
// It is an alias so durable adapters can use the plan-level API name without
// confusing this typed fact with the UI delivery envelope in internal/events.
type SessionEvent = ProjectionEvent

type RequirementFact struct {
	NodeID      domain.NodeID
	DisplayName string
	Contract    ContractSummary
}

type PlanFact struct {
	Revision          uint64
	RegionNodeID      domain.NodeID
	RegionDisplayName string
	RequirementNodeID domain.NodeID
	RequirementEdgeID domain.EdgeID
	Steps             []PlanStepFact
}

type PlanStepFact struct {
	StepID      string
	NodeID      domain.NodeID
	ControlEdge domain.EdgeID
	DisplayName string
}

type OperationKind string

const (
	OperationRepositoryInspection OperationKind = "repository-inspection"
	OperationFileEdit             OperationKind = "file-edit"
)

func (kind OperationKind) IsValid() bool {
	return kind == OperationRepositoryInspection || kind == OperationFileEdit
}

type ProjectionState string

const (
	ProjectionPendingState     ProjectionState = "pending"
	ProjectionActiveState      ProjectionState = "active"
	ProjectionPassedState      ProjectionState = "passed"
	ProjectionWarningState     ProjectionState = "warning"
	ProjectionFailedState      ProjectionState = "failed"
	ProjectionBlockedState     ProjectionState = "blocked"
	ProjectionInvalidatedState ProjectionState = "invalidated"
)

func (state ProjectionState) nodeStatus() (NodeStatus, bool) {
	switch state {
	case ProjectionPendingState:
		return NodeStatusPending, true
	case ProjectionActiveState:
		return NodeStatusActive, true
	case ProjectionPassedState:
		return NodeStatusPassed, true
	case ProjectionWarningState:
		return NodeStatusWarning, true
	case ProjectionFailedState:
		return NodeStatusFailed, true
	case ProjectionBlockedState:
		return NodeStatusBlocked, true
	case ProjectionInvalidatedState:
		return NodeStatusInvalidated, true
	default:
		return "", false
	}
}

type OperationFact struct {
	Kind         OperationKind
	NodeID       domain.NodeID
	ControlEdge  domain.EdgeID
	PlanNodeID   domain.NodeID
	PlanRevision uint64
	PlanStepID   string
	DisplayName  string
	State        ProjectionState
}

type EffectKind string

const (
	EffectCommandCall  EffectKind = "command-call"
	EffectProviderCall EffectKind = "provider-call"
)

func (kind EffectKind) IsValid() bool {
	return kind == EffectCommandCall || kind == EffectProviderCall
}

type EffectFact struct {
	Kind          EffectKind
	NodeID        domain.NodeID
	ControlEdge   domain.EdgeID
	OperationNode domain.NodeID
	PlanRevision  uint64
	PlanStepID    string
	DisplayName   string
	State         ProjectionState
}

type ApprovalState string

const (
	ApprovalRequested ApprovalState = "requested"
	ApprovalGranted   ApprovalState = "granted"
	ApprovalDenied    ApprovalState = "denied"
	ApprovalCancelled ApprovalState = "cancelled"
)

func (state ApprovalState) nodeStatus() (NodeStatus, bool) {
	switch state {
	case ApprovalRequested:
		return NodeStatusBlocked, true
	case ApprovalGranted:
		return NodeStatusPassed, true
	case ApprovalDenied:
		return NodeStatusFailed, true
	case ApprovalCancelled:
		return NodeStatusWarning, true
	default:
		return "", false
	}
}

type ApprovalFact struct {
	NodeID       domain.NodeID
	ControlEdge  domain.EdgeID
	Predecessor  domain.NodeID
	DisplayName  string
	State        ApprovalState
	PlanRevision uint64
	PlanStepID   string
}

type ValidationFact struct {
	ObligationNodeID domain.NodeID
	ControlEdge      domain.EdgeID
	Predecessor      domain.NodeID
	DisplayName      string
	State            ProjectionState
	PlanRevision     uint64
	PlanStepID       string

	ResultNodeID      domain.NodeID
	EvidenceEdge      domain.EdgeID
	ResultDisplayName string
}

type ArtifactKind string

const (
	ArtifactChangedFile ArtifactKind = "changed-file"
	ArtifactDiff        ArtifactKind = "diff"
	ArtifactCheckpoint  ArtifactKind = "checkpoint"
)

func (kind ArtifactKind) IsValid() bool {
	return kind == ArtifactChangedFile || kind == ArtifactDiff || kind == ArtifactCheckpoint
}

type ArtifactFact struct {
	Kind           ArtifactKind
	NodeID         domain.NodeID
	ProvenanceEdge domain.EdgeID
	ProducerNode   domain.NodeID
	DisplayName    string
	State          ProjectionState
	PlanRevision   uint64
	PlanStepID     string
}

type RetryFact struct {
	EdgeID         domain.EdgeID
	PreviousNodeID domain.NodeID
	RetryNodeID    domain.NodeID
}

type RecoveryKind string

const (
	RecoveryRequired    RecoveryKind = "required"
	RecoveryReconciled  RecoveryKind = "reconciled"
	RecoveryCompensated RecoveryKind = "compensated"
)

func (kind RecoveryKind) relation() (EdgeClass, NodeStatus, bool) {
	switch kind {
	case RecoveryRequired:
		return EdgeClassReconciliation, NodeStatusBlocked, true
	case RecoveryReconciled:
		return EdgeClassReconciliation, NodeStatusPassed, true
	case RecoveryCompensated:
		return EdgeClassCompensation, NodeStatusWarning, true
	default:
		return "", "", false
	}
}

type RecoveryFact struct {
	Kind             RecoveryKind
	NodeID           domain.NodeID
	RelationEdge     domain.EdgeID
	CheckpointNodeID domain.NodeID
	DisplayName      string
}

// TokenDeltaFact is intentionally empty. Its presence makes the one-of
// explicit while guaranteeing that token text cannot enter graph identity or
// revision materiality.
type TokenDeltaFact struct{}

// GraphProjection is an immutable-in-use, task-scoped projection state. Its
// maps are cloned for every application so callers can safely retain prior
// values for replay comparisons.
type GraphProjection struct {
	graph         Graph
	revision      *Revision
	lastSequence  uint64
	nodes         map[domain.NodeID]Node
	edges         map[domain.EdgeID]Edge
	pendingChange uint64
}

func NewGraphProjection(identity Graph, revision *Revision, lastSequence uint64) (GraphProjection, error) {
	if err := identity.Validate(); err != nil {
		return GraphProjection{}, err
	}
	projection := GraphProjection{
		graph: identity, lastSequence: lastSequence,
		nodes: make(map[domain.NodeID]Node), edges: make(map[domain.EdgeID]Edge),
	}
	if revision == nil {
		return projection, nil
	}
	if err := revision.Validate(); err != nil {
		return GraphProjection{}, err
	}
	if revision.Metadata().GraphID() != identity.ID() {
		return GraphProjection{}, projectionError("revision.graph_id", "does not match the projection graph")
	}
	copyRevision := *revision
	projection.revision = &copyRevision
	for _, node := range revision.Nodes() {
		projection.nodes[node.ID()] = node
	}
	for _, edge := range revision.Edges() {
		projection.edges[edge.ID()] = edge
	}
	return projection, nil
}

func (projection GraphProjection) Graph() Graph         { return projection.graph }
func (projection GraphProjection) LastSequence() uint64 { return projection.lastSequence }
func (projection GraphProjection) Revision() (Revision, bool) {
	if projection.revision == nil {
		return Revision{}, false
	}
	return *projection.revision, true
}

// GraphChange is the candidate complete topology resulting from exactly one
// material event. It is not a committed revision and cannot be patched or
// published until CommitGraphRevision succeeds.
type GraphChange struct {
	material   bool
	graphID    domain.GraphID
	sequence   uint64
	occurredAt time.Time
	sources    SourceLinks
	nodes      []Node
	edges      []Edge
}

func (change GraphChange) Material() bool          { return change.material }
func (change GraphChange) Sequence() uint64        { return change.sequence }
func (change GraphChange) GraphID() domain.GraphID { return change.graphID }
func (change GraphChange) OccurredAt() time.Time   { return change.occurredAt }
func (change GraphChange) Sources() SourceLinks    { return change.sources }
func (change GraphChange) Nodes() []Node           { return slices.Clone(change.nodes) }
func (change GraphChange) Edges() []Edge           { return slices.Clone(change.edges) }

// ProjectSessionEvent deterministically applies one already-classified,
// ordered task/session fact. It performs no I/O and allocates no identities.
func ProjectSessionEvent(
	projection GraphProjection,
	event ProjectionEvent,
) (GraphProjection, GraphChange, error) {
	if projection.pendingChange != 0 {
		return GraphProjection{}, GraphChange{}, ErrProjectionPending
	}
	if err := validateProjectionEvent(projection, event); err != nil {
		return GraphProjection{}, GraphChange{}, err
	}

	next := cloneProjection(projection)
	next.lastSequence = event.Sequence
	if event.Kind == ProjectionTokenDelta {
		return next, GraphChange{sequence: event.Sequence}, nil
	}

	sources, err := NewSourceLinks([]domain.EventID{event.SourceEventID}, nil)
	if err != nil {
		return GraphProjection{}, GraphChange{}, err
	}
	if err := applyProjectionFact(&next, event, sources); err != nil {
		return GraphProjection{}, GraphChange{}, err
	}

	next.pendingChange = event.Sequence
	change := GraphChange{
		material: true, graphID: projection.graph.ID(), sequence: event.Sequence, occurredAt: event.OccurredAt,
		sources: sources, nodes: sortedNodes(next.nodes), edges: sortedEdges(next.edges),
	}
	return next, change, nil
}

func applyProjectionFact(projection *GraphProjection, event ProjectionEvent, eventSources SourceLinks) error {
	switch event.Kind {
	case ProjectionRequirementAccepted:
		fact := event.Requirement
		return putNode(projection, fact.NodeID, NodeClassRequirement, NodeStatusPassed,
			fact.DisplayName, fact.Contract, eventSources)
	case ProjectionPlanDeclared:
		return applyPlan(projection, event.Plan, eventSources)
	case ProjectionOperationObserved:
		fact := event.Operation
		status, _ := fact.State.nodeStatus()
		sources, err := eventAndPlanSources(event.SourceEventID, fact.PlanRevision, fact.PlanStepID)
		if err != nil {
			return err
		}
		if err := putNode(projection, fact.NodeID, NodeClassAtomOperation, status, fact.DisplayName, ContractSummary{}, sources); err != nil {
			return err
		}
		return putEdge(projection, fact.ControlEdge, EdgeClassControl, fact.PlanNodeID, fact.NodeID, sources)
	case ProjectionEffectObserved:
		fact := event.Effect
		status, _ := fact.State.nodeStatus()
		sources, err := eventAndPlanSources(event.SourceEventID, fact.PlanRevision, fact.PlanStepID)
		if err != nil {
			return err
		}
		if err := putNode(projection, fact.NodeID, NodeClassEffect, status, fact.DisplayName, ContractSummary{}, sources); err != nil {
			return err
		}
		return putEdge(projection, fact.ControlEdge, EdgeClassControl, fact.OperationNode, fact.NodeID, sources)
	case ProjectionApprovalBoundary:
		fact := event.Approval
		status, _ := fact.State.nodeStatus()
		sources, err := eventAndPlanSources(event.SourceEventID, fact.PlanRevision, fact.PlanStepID)
		if err != nil {
			return err
		}
		if err := putNode(projection, fact.NodeID, NodeClassBranchMatchMerge, status, fact.DisplayName, ContractSummary{}, sources); err != nil {
			return err
		}
		return putEdge(projection, fact.ControlEdge, EdgeClassControl, fact.Predecessor, fact.NodeID, sources)
	case ProjectionValidationObserved:
		return applyValidation(projection, event.Validation, event.SourceEventID)
	case ProjectionArtifactObserved:
		fact := event.Artifact
		status, _ := fact.State.nodeStatus()
		sources, err := eventAndOptionalPlanSources(event.SourceEventID, fact.PlanRevision, fact.PlanStepID)
		if err != nil {
			return err
		}
		if err := putNode(projection, fact.NodeID, NodeClassArtifactResult, status, fact.DisplayName, ContractSummary{}, sources); err != nil {
			return err
		}
		return putEdge(projection, fact.ProvenanceEdge, EdgeClassDataProvenance, fact.ProducerNode, fact.NodeID, sources)
	case ProjectionRetryObserved:
		fact := event.Retry
		return putEdge(projection, fact.EdgeID, EdgeClassRetry, fact.PreviousNodeID, fact.RetryNodeID, eventSources)
	case ProjectionRecoveryObserved:
		fact := event.Recovery
		relation, status, _ := fact.Kind.relation()
		if err := putNode(projection, fact.NodeID, NodeClassBranchMatchMerge, status, fact.DisplayName, ContractSummary{}, eventSources); err != nil {
			return err
		}
		return putEdge(projection, fact.RelationEdge, relation, fact.CheckpointNodeID, fact.NodeID, eventSources)
	default:
		return projectionError("event.kind", "is not projectable")
	}
}

func applyPlan(projection *GraphProjection, fact *PlanFact, sources SourceLinks) error {
	if err := putNode(projection, fact.RegionNodeID, NodeClassPlanRegion, NodeStatusActive,
		fact.RegionDisplayName, ContractSummary{}, sources); err != nil {
		return err
	}
	if err := putEdge(projection, fact.RequirementEdgeID, EdgeClassControl,
		fact.RequirementNodeID, fact.RegionNodeID, sources); err != nil {
		return err
	}
	for _, step := range fact.Steps {
		stepSources, err := NewSourceLinks(sources.EventIDs(), []PlanStepLink{{PlanRevision: fact.Revision, StepID: step.StepID}})
		if err != nil {
			return err
		}
		if err := putNode(projection, step.NodeID, NodeClassPlanRegion, NodeStatusPending,
			step.DisplayName, ContractSummary{}, stepSources); err != nil {
			return err
		}
		if err := putEdge(projection, step.ControlEdge, EdgeClassControl,
			fact.RegionNodeID, step.NodeID, stepSources); err != nil {
			return err
		}
	}
	return nil
}

func applyValidation(projection *GraphProjection, fact *ValidationFact, eventID domain.EventID) error {
	status, _ := fact.State.nodeStatus()
	sources, err := eventAndPlanSources(eventID, fact.PlanRevision, fact.PlanStepID)
	if err != nil {
		return err
	}
	if err := putNode(projection, fact.ObligationNodeID, NodeClassObligation, status,
		fact.DisplayName, ContractSummary{}, sources); err != nil {
		return err
	}
	if err := putEdge(projection, fact.ControlEdge, EdgeClassControl,
		fact.Predecessor, fact.ObligationNodeID, sources); err != nil {
		return err
	}
	if fact.ResultNodeID.IsZero() {
		return nil
	}
	if err := putNode(projection, fact.ResultNodeID, NodeClassArtifactResult, status,
		fact.ResultDisplayName, ContractSummary{}, sources); err != nil {
		return err
	}
	return putEdge(projection, fact.EvidenceEdge, EdgeClassEvidenceDependency,
		fact.ObligationNodeID, fact.ResultNodeID, sources)
}

func putNode(projection *GraphProjection, id domain.NodeID, class NodeClass, status NodeStatus,
	displayName string, contract ContractSummary, sources SourceLinks) error {
	if existing, ok := projection.nodes[id]; ok {
		if existing.Class() != class || existing.DisplayName() != displayName ||
			!equalContract(existing.Contract(), contract) {
			return projectionError("node.identity", "was reused for different immutable semantics")
		}
		merged, err := mergeSources(existing.Sources(), sources)
		if err != nil {
			return err
		}
		node, err := NewNode(id, class, status, displayName, contract, merged)
		if err != nil {
			return err
		}
		projection.nodes[id] = node
		return nil
	}
	node, err := NewNode(id, class, status, displayName, contract, sources)
	if err != nil {
		return err
	}
	projection.nodes[id] = node
	return nil
}

func putEdge(projection *GraphProjection, id domain.EdgeID, class EdgeClass,
	from, to domain.NodeID, sources SourceLinks) error {
	if _, ok := projection.nodes[from]; !ok {
		return projectionError("edge.from", "references an unprojected node")
	}
	if _, ok := projection.nodes[to]; !ok {
		return projectionError("edge.to", "references an unprojected node")
	}
	if existing, ok := projection.edges[id]; ok {
		if existing.Class() != class || existing.FromNode() != from || existing.ToNode() != to {
			return projectionError("edge.identity", "was reused for different immutable semantics")
		}
		merged, err := mergeSources(existing.Sources(), sources)
		if err != nil {
			return err
		}
		edge, err := NewEdge(id, class, from, to, merged)
		if err != nil {
			return err
		}
		projection.edges[id] = edge
		return nil
	}
	edge, err := NewEdge(id, class, from, to, sources)
	if err != nil {
		return err
	}
	projection.edges[id] = edge
	return nil
}

func mergeSources(left, right SourceLinks) (SourceLinks, error) {
	events := left.EventIDs()
	seenEvents := make(map[domain.EventID]struct{}, len(events))
	for _, value := range events {
		seenEvents[value] = struct{}{}
	}
	for _, value := range right.EventIDs() {
		if _, ok := seenEvents[value]; !ok {
			events = append(events, value)
			seenEvents[value] = struct{}{}
		}
	}
	steps := left.PlanSteps()
	seenSteps := make(map[PlanStepLink]struct{}, len(steps))
	for _, value := range steps {
		seenSteps[value] = struct{}{}
	}
	for _, value := range right.PlanSteps() {
		if _, ok := seenSteps[value]; !ok {
			steps = append(steps, value)
			seenSteps[value] = struct{}{}
		}
	}
	return NewSourceLinks(events, steps)
}

func eventAndPlanSources(eventID domain.EventID, revision uint64, stepID string) (SourceLinks, error) {
	return NewSourceLinks([]domain.EventID{eventID}, []PlanStepLink{{PlanRevision: revision, StepID: stepID}})
}

func eventAndOptionalPlanSources(eventID domain.EventID, revision uint64, stepID string) (SourceLinks, error) {
	if revision == 0 && stepID == "" {
		return NewSourceLinks([]domain.EventID{eventID}, nil)
	}
	return eventAndPlanSources(eventID, revision, stepID)
}

func validateProjectionEvent(projection GraphProjection, event ProjectionEvent) error {
	switch {
	case projection.graph.ID().IsZero():
		return projectionError("projection.graph", "is missing")
	case event.Sequence != projection.lastSequence+1:
		return projectionError("event.sequence", "must be the next contiguous sequence")
	case event.SourceEventID.IsZero():
		return projectionError("event.source_event_id", "must not be empty")
	case event.TaskID.IsZero() || event.TaskID != projection.graph.TaskID():
		return projectionError("event.task_id", "does not match the graph task")
	case event.OccurredAt.IsZero() || event.OccurredAt.Location() != time.UTC:
		return projectionError("event.occurred_at", "must be a non-zero UTC timestamp")
	case !event.Kind.IsValid():
		return projectionError("event.kind", "is not supported")
	case projectionPayloadCount(event) != 1:
		return projectionError("event.payload", "must contain exactly one typed fact")
	}
	if !projectionPayloadMatchesKind(event) {
		return projectionError("event.payload", "does not match the event kind")
	}
	return validateProjectionPayload(projection, event)
}

func validateProjectionPayload(projection GraphProjection, event ProjectionEvent) error {
	switch event.Kind {
	case ProjectionRequirementAccepted:
		if event.Requirement.NodeID.IsZero() {
			return projectionError("requirement.node_id", "must not be empty")
		}
	case ProjectionPlanDeclared:
		fact := event.Plan
		if fact.Revision == 0 || fact.RegionNodeID.IsZero() || fact.RequirementNodeID.IsZero() || fact.RequirementEdgeID.IsZero() || len(fact.Steps) == 0 || len(fact.Steps) > MaximumProjectedPlanSteps {
			return projectionError("plan", "is incomplete or exceeds its step limit")
		}
		if _, ok := projection.nodes[fact.RequirementNodeID]; !ok {
			return projectionError("plan.requirement", "is not projected")
		}
		seenSteps := make(map[string]struct{}, len(fact.Steps))
		seenNodes := make(map[domain.NodeID]struct{}, len(fact.Steps))
		seenEdges := make(map[domain.EdgeID]struct{}, len(fact.Steps))
		for _, step := range fact.Steps {
			if step.StepID == "" || step.NodeID.IsZero() || step.ControlEdge.IsZero() {
				return projectionError("plan.steps", "contains an incomplete step")
			}
			if _, ok := seenSteps[step.StepID]; ok {
				return projectionError("plan.steps", "contains a duplicate step identity")
			}
			seenSteps[step.StepID] = struct{}{}
			if _, ok := seenNodes[step.NodeID]; ok {
				return projectionError("plan.steps", "contains a duplicate node identity")
			}
			seenNodes[step.NodeID] = struct{}{}
			if _, ok := seenEdges[step.ControlEdge]; ok {
				return projectionError("plan.steps", "contains a duplicate edge identity")
			}
			seenEdges[step.ControlEdge] = struct{}{}
		}
	case ProjectionOperationObserved:
		fact := event.Operation
		_, statusOK := fact.State.nodeStatus()
		if !fact.Kind.IsValid() || fact.NodeID.IsZero() || fact.ControlEdge.IsZero() || fact.PlanNodeID.IsZero() || fact.PlanRevision == 0 || fact.PlanStepID == "" || !statusOK {
			return projectionError("operation", "is incomplete")
		}
	case ProjectionEffectObserved:
		fact := event.Effect
		_, statusOK := fact.State.nodeStatus()
		if !fact.Kind.IsValid() || fact.NodeID.IsZero() || fact.ControlEdge.IsZero() || fact.OperationNode.IsZero() || fact.PlanRevision == 0 || fact.PlanStepID == "" || !statusOK {
			return projectionError("effect", "is incomplete")
		}
	case ProjectionApprovalBoundary:
		fact := event.Approval
		_, stateOK := fact.State.nodeStatus()
		if fact.NodeID.IsZero() || fact.ControlEdge.IsZero() || fact.Predecessor.IsZero() || !stateOK || fact.PlanRevision == 0 || fact.PlanStepID == "" {
			return projectionError("approval", "is incomplete")
		}
	case ProjectionValidationObserved:
		fact := event.Validation
		_, statusOK := fact.State.nodeStatus()
		if fact.ObligationNodeID.IsZero() || fact.ControlEdge.IsZero() || fact.Predecessor.IsZero() || !statusOK || fact.PlanRevision == 0 || fact.PlanStepID == "" {
			return projectionError("validation", "is incomplete")
		}
		if fact.ResultNodeID.IsZero() != fact.EvidenceEdge.IsZero() || fact.ResultNodeID.IsZero() != (fact.ResultDisplayName == "") {
			return projectionError("validation.result", "must be entirely present or absent")
		}
	case ProjectionArtifactObserved:
		fact := event.Artifact
		_, statusOK := fact.State.nodeStatus()
		if !fact.Kind.IsValid() || fact.NodeID.IsZero() || fact.ProvenanceEdge.IsZero() || fact.ProducerNode.IsZero() || !statusOK || (fact.PlanRevision == 0) != (fact.PlanStepID == "") {
			return projectionError("artifact", "is incomplete")
		}
	case ProjectionRetryObserved:
		fact := event.Retry
		if fact.EdgeID.IsZero() || fact.PreviousNodeID.IsZero() || fact.RetryNodeID.IsZero() || fact.PreviousNodeID == fact.RetryNodeID {
			return projectionError("retry", "is incomplete")
		}
	case ProjectionRecoveryObserved:
		fact := event.Recovery
		_, _, kindOK := fact.Kind.relation()
		if !kindOK || fact.NodeID.IsZero() || fact.RelationEdge.IsZero() || fact.CheckpointNodeID.IsZero() {
			return projectionError("recovery", "is incomplete")
		}
	case ProjectionTokenDelta:
		// The empty typed payload is the complete contract.
	}
	return nil
}

func projectionPayloadCount(event ProjectionEvent) int {
	count := 0
	for _, present := range []bool{event.Requirement != nil, event.Plan != nil, event.Operation != nil,
		event.Effect != nil, event.Approval != nil, event.Validation != nil, event.Artifact != nil,
		event.Retry != nil, event.Recovery != nil, event.TokenDelta != nil} {
		if present {
			count++
		}
	}
	return count
}

func projectionPayloadMatchesKind(event ProjectionEvent) bool {
	switch event.Kind {
	case ProjectionRequirementAccepted:
		return event.Requirement != nil
	case ProjectionPlanDeclared:
		return event.Plan != nil
	case ProjectionOperationObserved:
		return event.Operation != nil
	case ProjectionEffectObserved:
		return event.Effect != nil
	case ProjectionApprovalBoundary:
		return event.Approval != nil
	case ProjectionValidationObserved:
		return event.Validation != nil
	case ProjectionArtifactObserved:
		return event.Artifact != nil
	case ProjectionRetryObserved:
		return event.Retry != nil
	case ProjectionRecoveryObserved:
		return event.Recovery != nil
	case ProjectionTokenDelta:
		return event.TokenDelta != nil
	default:
		return false
	}
}

// CommitGraphRevision converts a pending material change into a complete
// immutable revision. Revision identity is supplied by the transactional
// caller; this pure boundary never invents replay-unstable IDs.
func CommitGraphRevision(
	projection GraphProjection,
	change GraphChange,
	revisionID domain.GraphRevisionID,
) (GraphProjection, Revision, error) {
	if !change.material || change.graphID != projection.graph.ID() || projection.pendingChange == 0 || projection.pendingChange != change.sequence || revisionID.IsZero() {
		return GraphProjection{}, Revision{}, projectionError("commit", "does not match a pending material change")
	}
	if !equalNodeSlices(change.nodes, sortedNodes(projection.nodes)) ||
		!equalEdgeSlices(change.edges, sortedEdges(projection.edges)) {
		return GraphProjection{}, Revision{}, projectionError("commit.change", "belongs to a different projection branch")
	}
	ordinal := uint64(1)
	var parent *domain.GraphRevisionID
	if projection.revision != nil {
		ordinal = projection.revision.Metadata().Ordinal() + 1
		value := projection.revision.Metadata().ID()
		parent = &value
	}
	metadata, err := NewRevisionMetadata(revisionID, projection.graph.ID(), ordinal, parent,
		CurrentSchemaVersion, change.occurredAt, change.sources)
	if err != nil {
		return GraphProjection{}, Revision{}, err
	}
	revision, err := NewRevision(metadata, change.nodes, change.edges)
	if err != nil {
		return GraphProjection{}, Revision{}, err
	}
	committed := cloneProjection(projection)
	committed.pendingChange = 0
	committed.revision = &revision
	return committed, revision, nil
}

type PatchLimits struct{ MaximumNodes, MaximumEdges int }

func DefaultPatchLimits() PatchLimits {
	return PatchLimits{MaximumNodes: DefaultPatchNodeLimit, MaximumEdges: DefaultPatchEdgeLimit}
}

type GraphPatch struct {
	PreviousRevisionID *domain.GraphRevisionID
	RevisionID         domain.GraphRevisionID
	AddedNodes         []Node
	UpdatedNodes       []Node
	RemovedNodeIDs     []domain.NodeID
	AddedEdges         []Edge
	UpdatedEdges       []Edge
	RemovedEdgeIDs     []domain.EdgeID
}

// BuildGraphPatch compares committed immutable revisions. It fails closed
// instead of emitting a partial patch when the configured bound is exceeded.
func BuildGraphPatch(previous *Revision, current Revision, limits PatchLimits) (GraphPatch, error) {
	if err := current.Validate(); err != nil {
		return GraphPatch{}, err
	}
	if limits.MaximumNodes <= 0 || limits.MaximumEdges <= 0 {
		return GraphPatch{}, projectionError("patch.limits", "must be positive")
	}
	patch := GraphPatch{RevisionID: current.Metadata().ID()}
	previousNodes := map[domain.NodeID]Node{}
	previousEdges := map[domain.EdgeID]Edge{}
	if previous != nil {
		if err := previous.Validate(); err != nil {
			return GraphPatch{}, err
		}
		if previous.Metadata().GraphID() != current.Metadata().GraphID() {
			return GraphPatch{}, projectionError("patch.graph", "revisions belong to different graphs")
		}
		parentID, hasParent := current.Metadata().ParentID()
		if !hasParent || parentID != previous.Metadata().ID() ||
			current.Metadata().Ordinal() != previous.Metadata().Ordinal()+1 {
			return GraphPatch{}, projectionError("patch.parent", "revisions are not adjacent")
		}
		id := previous.Metadata().ID()
		patch.PreviousRevisionID = &id
		for _, node := range previous.Nodes() {
			previousNodes[node.ID()] = node
		}
		for _, edge := range previous.Edges() {
			previousEdges[edge.ID()] = edge
		}
	}
	currentNodes := map[domain.NodeID]Node{}
	currentEdges := map[domain.EdgeID]Edge{}
	for _, node := range current.Nodes() {
		currentNodes[node.ID()] = node
		prior, ok := previousNodes[node.ID()]
		if !ok {
			patch.AddedNodes = append(patch.AddedNodes, node)
		} else if !equalNode(prior, node) {
			patch.UpdatedNodes = append(patch.UpdatedNodes, node)
		}
	}
	for id := range previousNodes {
		if _, ok := currentNodes[id]; !ok {
			patch.RemovedNodeIDs = append(patch.RemovedNodeIDs, id)
		}
	}
	for _, edge := range current.Edges() {
		currentEdges[edge.ID()] = edge
		prior, ok := previousEdges[edge.ID()]
		if !ok {
			patch.AddedEdges = append(patch.AddedEdges, edge)
		} else if !equalEdge(prior, edge) {
			patch.UpdatedEdges = append(patch.UpdatedEdges, edge)
		}
	}
	for id := range previousEdges {
		if _, ok := currentEdges[id]; !ok {
			patch.RemovedEdgeIDs = append(patch.RemovedEdgeIDs, id)
		}
	}
	sortPatch(&patch)
	if len(patch.AddedNodes)+len(patch.UpdatedNodes)+len(patch.RemovedNodeIDs) > limits.MaximumNodes || len(patch.AddedEdges)+len(patch.UpdatedEdges)+len(patch.RemovedEdgeIDs) > limits.MaximumEdges {
		return GraphPatch{}, ErrPatchLimitExceeded
	}
	return patch, nil
}

type ModeSlice struct {
	Mode  Mode
	Nodes []Node
	Edges []Edge
}

// DeriveModeVisibility creates a deterministic class/relationship projection
// without mutating or re-identifying the committed graph.
func DeriveModeVisibility(revision Revision, mode Mode) (ModeSlice, error) {
	if err := revision.Validate(); err != nil {
		return ModeSlice{}, err
	}
	if !mode.IsValid() {
		return ModeSlice{}, projectionError("mode", "is not supported")
	}
	allNodes := revision.Nodes()
	allEdges := revision.Edges()
	visible := make(map[domain.NodeID]bool, len(allNodes))
	switch mode {
	case ModeProgram:
		for _, node := range allNodes {
			visible[node.ID()] = true
		}
	case ModeExecution:
		for _, node := range allNodes {
			if node.Class() != NodeClassRequirement {
				visible[node.ID()] = true
			}
		}
	case ModeEvidence:
		for _, node := range allNodes {
			if node.Class() == NodeClassRequirement || node.Class() == NodeClassObligation || node.Class() == NodeClassArtifactResult {
				visible[node.ID()] = true
			}
		}
		changed := true
		for changed {
			changed = false
			for _, edge := range allEdges {
				if edge.Class() != EdgeClassDataProvenance && edge.Class() != EdgeClassEvidenceDependency && edge.Class() != EdgeClassReconciliation && edge.Class() != EdgeClassCompensation {
					continue
				}
				if visible[edge.FromNode()] || visible[edge.ToNode()] {
					if !visible[edge.FromNode()] {
						visible[edge.FromNode()] = true
						changed = true
					}
					if !visible[edge.ToNode()] {
						visible[edge.ToNode()] = true
						changed = true
					}
				}
			}
		}
	}
	result := ModeSlice{Mode: mode}
	for _, node := range allNodes {
		if visible[node.ID()] {
			result.Nodes = append(result.Nodes, node)
		}
	}
	for _, edge := range allEdges {
		if visible[edge.FromNode()] && visible[edge.ToNode()] {
			result.Edges = append(result.Edges, edge)
		}
	}
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].ID().String() < result.Nodes[j].ID().String() })
	sort.Slice(result.Edges, func(i, j int) bool { return result.Edges[i].ID().String() < result.Edges[j].ID().String() })
	return result, nil
}

func cloneProjection(value GraphProjection) GraphProjection {
	clone := value
	clone.nodes = make(map[domain.NodeID]Node, len(value.nodes))
	for id, node := range value.nodes {
		clone.nodes[id] = node
	}
	clone.edges = make(map[domain.EdgeID]Edge, len(value.edges))
	for id, edge := range value.edges {
		clone.edges[id] = edge
	}
	if value.revision != nil {
		revision := *value.revision
		clone.revision = &revision
	}
	return clone
}

func sortedNodes(values map[domain.NodeID]Node) []Node {
	result := make([]Node, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID().String() < result[j].ID().String() })
	return result
}

func sortedEdges(values map[domain.EdgeID]Edge) []Edge {
	result := make([]Edge, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID().String() < result[j].ID().String() })
	return result
}

func equalContract(left, right ContractSummary) bool {
	return left.Purpose() == right.Purpose() && slices.Equal(left.Inputs(), right.Inputs()) && slices.Equal(left.Outputs(), right.Outputs()) && slices.Equal(left.Effects(), right.Effects())
}

func equalSources(left, right SourceLinks) bool {
	return slices.Equal(left.EventIDs(), right.EventIDs()) && slices.Equal(left.PlanSteps(), right.PlanSteps())
}

func equalNode(left, right Node) bool {
	return left.ID() == right.ID() && left.Class() == right.Class() && left.Status() == right.Status() && left.DisplayName() == right.DisplayName() && equalContract(left.Contract(), right.Contract()) && equalSources(left.Sources(), right.Sources())
}

func equalEdge(left, right Edge) bool {
	return left.ID() == right.ID() && left.Class() == right.Class() && left.FromNode() == right.FromNode() && left.ToNode() == right.ToNode() && equalSources(left.Sources(), right.Sources())
}

func equalNodeSlices(left, right []Node) bool {
	return slices.EqualFunc(left, right, equalNode)
}

func equalEdgeSlices(left, right []Edge) bool {
	return slices.EqualFunc(left, right, equalEdge)
}

func sortPatch(patch *GraphPatch) {
	sort.Slice(patch.AddedNodes, func(i, j int) bool { return patch.AddedNodes[i].ID().String() < patch.AddedNodes[j].ID().String() })
	sort.Slice(patch.UpdatedNodes, func(i, j int) bool { return patch.UpdatedNodes[i].ID().String() < patch.UpdatedNodes[j].ID().String() })
	sort.Slice(patch.RemovedNodeIDs, func(i, j int) bool { return patch.RemovedNodeIDs[i].String() < patch.RemovedNodeIDs[j].String() })
	sort.Slice(patch.AddedEdges, func(i, j int) bool { return patch.AddedEdges[i].ID().String() < patch.AddedEdges[j].ID().String() })
	sort.Slice(patch.UpdatedEdges, func(i, j int) bool { return patch.UpdatedEdges[i].ID().String() < patch.UpdatedEdges[j].ID().String() })
	sort.Slice(patch.RemovedEdgeIDs, func(i, j int) bool { return patch.RemovedEdgeIDs[i].String() < patch.RemovedEdgeIDs[j].String() })
}

func projectionError(field, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidProjectionEvent, field, reason)
}
