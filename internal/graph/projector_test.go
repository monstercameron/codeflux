package graph

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestProjectionCoversProgramExecutionEvidenceAndRecoveryFacts(t *testing.T) {
	fixture := newProjectionFixture(t)
	projection := fixture.projection
	var revisions []Revision

	for index, event := range fixture.events {
		previous, hadPrevious := projection.Revision()
		projected, change, err := ProjectSessionEvent(projection, event)
		if err != nil {
			t.Fatalf("event %d (%s): %v", index+1, event.Kind, err)
		}
		if event.Kind == ProjectionTokenDelta {
			if change.Material() {
				t.Fatal("token-only delta became material")
			}
			if current, ok := projected.Revision(); ok != hadPrevious || ok && current.Metadata().ID() != previous.Metadata().ID() {
				t.Fatal("token-only delta changed the committed revision")
			}
			projection = projected
			continue
		}
		if !change.Material() {
			t.Fatalf("event %s did not produce a material change", event.Kind)
		}
		committed, revision, err := CommitGraphRevision(projected, change, fixture.revisionIDs[index])
		if err != nil {
			t.Fatalf("commit %d: %v", index+1, err)
		}
		if revision.Metadata().Ordinal() != uint64(len(revisions)+1) {
			t.Fatalf("revision ordinal = %d", revision.Metadata().Ordinal())
		}
		if hadPrevious {
			parent, ok := revision.Metadata().ParentID()
			if !ok || parent != previous.Metadata().ID() {
				t.Fatalf("revision parent = %s, %t", parent, ok)
			}
		}
		patch, err := BuildGraphPatch(optionalRevision(hadPrevious, previous), revision, DefaultPatchLimits())
		if err != nil {
			t.Fatalf("patch %d: %v", index+1, err)
		}
		if patch.RevisionID != revision.Metadata().ID() || len(patch.AddedNodes)+len(patch.UpdatedNodes)+len(patch.AddedEdges)+len(patch.UpdatedEdges) == 0 {
			t.Fatalf("empty or misbound material patch: %+v", patch)
		}
		projection = committed
		revisions = append(revisions, revision)
	}

	latest, ok := projection.Revision()
	if !ok {
		t.Fatal("projection has no committed revision")
	}
	assertProjectedVocabulary(t, latest)
	assertModeSlices(t, latest)
	if projection.LastSequence() != uint64(len(fixture.events)) {
		t.Fatalf("last sequence = %d", projection.LastSequence())
	}
}

func TestReplayOfSameOrderedFactsProducesIdenticalRevisions(t *testing.T) {
	fixture := newProjectionFixture(t)
	first := replayProjection(t, fixture.projection, fixture.events, fixture.revisionIDs)
	second := replayProjection(t, fixture.projection, fixture.events, fixture.revisionIDs)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same ordered task facts did not produce byte-for-byte equivalent revision values")
	}
	if first.Metadata().SchemaVersion() != CurrentSchemaVersion {
		t.Fatal("replay lost the independent graph projection schema version")
	}
}

func TestProjectionRejectsSequenceScopeIdentityAndUntypedSemanticDrift(t *testing.T) {
	fixture := newProjectionFixture(t)
	valid := fixture.events[0]

	tests := []struct {
		name   string
		change func(*ProjectionEvent)
	}{
		{"sequence gap", func(event *ProjectionEvent) { event.Sequence = 2 }},
		{"wrong task", func(event *ProjectionEvent) { event.TaskID = mustTaskID(t) }},
		{"missing source identity", func(event *ProjectionEvent) { event.SourceEventID = domain.EventID{} }},
		{"local timestamp", func(event *ProjectionEvent) { event.OccurredAt = event.OccurredAt.In(time.FixedZone("local", -18000)) }},
		{"kind payload mismatch", func(event *ProjectionEvent) { event.Kind = ProjectionPlanDeclared }},
		{"multiple payloads", func(event *ProjectionEvent) { event.TokenDelta = &TokenDeltaFact{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			test.change(&event)
			if _, _, err := ProjectSessionEvent(fixture.projection, event); !errors.Is(err, ErrInvalidProjectionEvent) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestMaterialChangeMustCommitBeforeNextEventAndPatchFailsClosedAtLimit(t *testing.T) {
	fixture := newProjectionFixture(t)
	projected, change, err := ProjectSessionEvent(fixture.projection, fixture.events[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ProjectSessionEvent(projected, fixture.events[1]); !errors.Is(err, ErrProjectionPending) {
		t.Fatalf("pending error = %v", err)
	}
	otherEvent := fixture.events[0]
	otherEvent.Requirement = &RequirementFact{
		NodeID: mustNodeID(t), DisplayName: "Different accepted intent",
	}
	_, otherChange, err := ProjectSessionEvent(fixture.projection, otherEvent)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := CommitGraphRevision(projected, otherChange, fixture.revisionIDs[0]); !errors.Is(err, ErrInvalidProjectionEvent) {
		t.Fatalf("cross-branch commit error = %v", err)
	}
	committed, revision, err := CommitGraphRevision(projected, change, fixture.revisionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := CommitGraphRevision(committed, change, fixture.revisionIDs[0]); !errors.Is(err, ErrInvalidProjectionEvent) {
		t.Fatalf("duplicate commit error = %v", err)
	}
	if _, err := BuildGraphPatch(nil, revision, PatchLimits{MaximumNodes: 1, MaximumEdges: 1}); err != nil {
		t.Fatalf("one-node root patch should fit: %v", err)
	}

	projected, change, err = ProjectSessionEvent(committed, fixture.events[1])
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := CommitGraphRevision(projected, change, fixture.revisionIDs[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildGraphPatch(&revision, second, PatchLimits{MaximumNodes: 1, MaximumEdges: 1}); !errors.Is(err, ErrPatchLimitExceeded) {
		t.Fatalf("bounded patch error = %v", err)
	}
	if len(revision.Nodes()) != 1 || len(revision.Edges()) != 0 {
		t.Fatal("later projection mutated the already committed root revision")
	}
}

func TestStableIdentityCannotBeReusedForDifferentGraphMeaning(t *testing.T) {
	fixture := newProjectionFixture(t)
	projection := fixture.projection
	projected, change, err := ProjectSessionEvent(projection, fixture.events[0])
	if err != nil {
		t.Fatal(err)
	}
	projection, _, err = CommitGraphRevision(projected, change, fixture.revisionIDs[0])
	if err != nil {
		t.Fatal(err)
	}

	event := fixture.events[1]
	event.Plan.RegionNodeID = fixture.events[0].Requirement.NodeID
	if _, _, err := ProjectSessionEvent(projection, event); !errors.Is(err, ErrInvalidProjectionEvent) {
		t.Fatalf("identity reuse error = %v", err)
	}
}

func assertProjectedVocabulary(t *testing.T, revision Revision) {
	t.Helper()
	classes := make(map[NodeClass]int)
	statuses := make(map[NodeStatus]int)
	for _, node := range revision.Nodes() {
		classes[node.Class()]++
		statuses[node.Status()]++
	}
	for _, class := range []NodeClass{
		NodeClassRequirement, NodeClassPlanRegion, NodeClassAtomOperation,
		NodeClassEffect, NodeClassBranchMatchMerge, NodeClassObligation,
		NodeClassArtifactResult,
	} {
		if classes[class] == 0 {
			t.Fatalf("projected graph lacks %s nodes", class)
		}
	}
	edges := make(map[EdgeClass]int)
	for _, edge := range revision.Edges() {
		edges[edge.Class()]++
	}
	for _, class := range []EdgeClass{
		EdgeClassControl, EdgeClassDataProvenance, EdgeClassEvidenceDependency,
		EdgeClassRetry, EdgeClassReconciliation,
	} {
		if edges[class] == 0 {
			t.Fatalf("projected graph lacks %s edges", class)
		}
	}
	if statuses[NodeStatusBlocked] == 0 || statuses[NodeStatusPassed] == 0 {
		t.Fatalf("execution status projection is incomplete: %+v", statuses)
	}
}

func assertModeSlices(t *testing.T, revision Revision) {
	t.Helper()
	program, err := DeriveModeVisibility(revision, ModeProgram)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := DeriveModeVisibility(revision, ModeExecution)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := DeriveModeVisibility(revision, ModeEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Nodes) != len(revision.Nodes()) || len(program.Edges) != len(revision.Edges()) {
		t.Fatal("program mode did not preserve the complete declared program")
	}
	for _, node := range execution.Nodes {
		if node.Class() == NodeClassRequirement {
			t.Fatal("execution mode included accepted-intent context")
		}
	}
	hasObligation, hasArtifact, hasProducer := false, false, false
	for _, node := range evidence.Nodes {
		switch node.Class() {
		case NodeClassObligation:
			hasObligation = true
		case NodeClassArtifactResult:
			hasArtifact = true
		case NodeClassAtomOperation, NodeClassEffect:
			hasProducer = true
		}
	}
	if !hasObligation || !hasArtifact || !hasProducer {
		t.Fatalf("evidence mode lacks obligation/artifact/provenance context: nodes=%d", len(evidence.Nodes))
	}
}

func replayProjection(t *testing.T, projection GraphProjection, events []ProjectionEvent, revisions []domain.GraphRevisionID) Revision {
	t.Helper()
	for index, event := range events {
		projected, change, err := ProjectSessionEvent(projection, event)
		if err != nil {
			t.Fatalf("replay event %d: %v", index+1, err)
		}
		if !change.Material() {
			projection = projected
			continue
		}
		projection, _, err = CommitGraphRevision(projected, change, revisions[index])
		if err != nil {
			t.Fatalf("replay commit %d: %v", index+1, err)
		}
	}
	revision, ok := projection.Revision()
	if !ok {
		t.Fatal("replay produced no revision")
	}
	return revision
}

func optionalRevision(present bool, revision Revision) *Revision {
	if !present {
		return nil
	}
	return &revision
}

type projectionFixture struct {
	projection  GraphProjection
	events      []ProjectionEvent
	revisionIDs []domain.GraphRevisionID
}

func newProjectionFixture(t *testing.T) projectionFixture {
	t.Helper()
	taskID := mustTaskID(t)
	graphID, err := domain.NewGraphID()
	if err != nil {
		t.Fatal(err)
	}
	baseTime := time.Date(2026, 7, 31, 21, 0, 0, 0, time.UTC)
	identity, err := NewGraph(graphID, taskID, baseTime)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := NewGraphProjection(identity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	nodes := make([]domain.NodeID, 15)
	for index := range nodes {
		nodes[index], err = domain.NewNodeID()
		if err != nil {
			t.Fatal(err)
		}
	}
	edges := make([]domain.EdgeID, 18)
	for index := range edges {
		edges[index], err = domain.NewEdgeID()
		if err != nil {
			t.Fatal(err)
		}
	}
	eventIDs := make([]domain.EventID, 14)
	for index := range eventIDs {
		eventIDs[index], err = domain.NewEventID()
		if err != nil {
			t.Fatal(err)
		}
	}
	revisions := make([]domain.GraphRevisionID, len(eventIDs))
	for index := range revisions {
		revisions[index], err = domain.NewGraphRevisionID()
		if err != nil {
			t.Fatal(err)
		}
	}

	stepInspect, stepEdit := "inspect-repository", "edit-files"
	events := []ProjectionEvent{
		fixtureEvent(1, eventIDs[0], taskID, baseTime, ProjectionRequirementAccepted, func(event *ProjectionEvent) {
			event.Requirement = &RequirementFact{NodeID: nodes[0], DisplayName: "Accepted user intent"}
		}),
		fixtureEvent(2, eventIDs[1], taskID, baseTime, ProjectionPlanDeclared, func(event *ProjectionEvent) {
			event.Plan = &PlanFact{Revision: 1, RegionNodeID: nodes[1], RegionDisplayName: "Approved plan", RequirementNodeID: nodes[0], RequirementEdgeID: edges[0], Steps: []PlanStepFact{
				{StepID: stepInspect, NodeID: nodes[2], ControlEdge: edges[1], DisplayName: "Inspect repository"},
				{StepID: stepEdit, NodeID: nodes[3], ControlEdge: edges[2], DisplayName: "Edit task files"},
			}}
		}),
		fixtureEvent(3, eventIDs[2], taskID, baseTime, ProjectionOperationObserved, func(event *ProjectionEvent) {
			event.Operation = &OperationFact{Kind: OperationRepositoryInspection, NodeID: nodes[4], ControlEdge: edges[3], PlanNodeID: nodes[2], PlanRevision: 1, PlanStepID: stepInspect, DisplayName: "Read repository state", State: ProjectionPassedState}
		}),
		fixtureEvent(4, eventIDs[3], taskID, baseTime, ProjectionOperationObserved, func(event *ProjectionEvent) {
			event.Operation = &OperationFact{Kind: OperationFileEdit, NodeID: nodes[5], ControlEdge: edges[4], PlanNodeID: nodes[3], PlanRevision: 1, PlanStepID: stepEdit, DisplayName: "Apply bounded edit", State: ProjectionPassedState}
		}),
		fixtureEvent(5, eventIDs[4], taskID, baseTime, ProjectionEffectObserved, func(event *ProjectionEvent) {
			event.Effect = &EffectFact{Kind: EffectCommandCall, NodeID: nodes[6], ControlEdge: edges[5], OperationNode: nodes[4], PlanRevision: 1, PlanStepID: stepInspect, DisplayName: "Run repository command", State: ProjectionPassedState}
		}),
		fixtureEvent(6, eventIDs[5], taskID, baseTime, ProjectionEffectObserved, func(event *ProjectionEvent) {
			event.Effect = &EffectFact{Kind: EffectProviderCall, NodeID: nodes[7], ControlEdge: edges[6], OperationNode: nodes[5], PlanRevision: 1, PlanStepID: stepEdit, DisplayName: "Request provider decision", State: ProjectionWarningState}
		}),
		fixtureEvent(7, eventIDs[6], taskID, baseTime, ProjectionApprovalBoundary, func(event *ProjectionEvent) {
			event.Approval = &ApprovalFact{NodeID: nodes[8], ControlEdge: edges[7], Predecessor: nodes[5], DisplayName: "Approval boundary", State: ApprovalRequested, PlanRevision: 1, PlanStepID: stepEdit}
		}),
		fixtureEvent(8, eventIDs[7], taskID, baseTime, ProjectionValidationObserved, func(event *ProjectionEvent) {
			event.Validation = &ValidationFact{ObligationNodeID: nodes[9], ControlEdge: edges[8], Predecessor: nodes[6], DisplayName: "Required validation", State: ProjectionPassedState, PlanRevision: 1, PlanStepID: stepInspect, ResultNodeID: nodes[10], EvidenceEdge: edges[9], ResultDisplayName: "Validation result"}
		}),
		fixtureEvent(9, eventIDs[8], taskID, baseTime, ProjectionArtifactObserved, func(event *ProjectionEvent) {
			event.Artifact = &ArtifactFact{Kind: ArtifactChangedFile, NodeID: nodes[11], ProvenanceEdge: edges[10], ProducerNode: nodes[5], DisplayName: "Changed file", State: ProjectionPassedState, PlanRevision: 1, PlanStepID: stepEdit}
		}),
		fixtureEvent(10, eventIDs[9], taskID, baseTime, ProjectionArtifactObserved, func(event *ProjectionEvent) {
			event.Artifact = &ArtifactFact{Kind: ArtifactDiff, NodeID: nodes[12], ProvenanceEdge: edges[11], ProducerNode: nodes[5], DisplayName: "Task diff", State: ProjectionPassedState, PlanRevision: 1, PlanStepID: stepEdit}
		}),
		fixtureEvent(11, eventIDs[10], taskID, baseTime, ProjectionArtifactObserved, func(event *ProjectionEvent) {
			event.Artifact = &ArtifactFact{Kind: ArtifactCheckpoint, NodeID: nodes[13], ProvenanceEdge: edges[12], ProducerNode: nodes[5], DisplayName: "Recovery checkpoint", State: ProjectionPassedState, PlanRevision: 1, PlanStepID: stepEdit}
		}),
		fixtureEvent(12, eventIDs[11], taskID, baseTime, ProjectionRecoveryObserved, func(event *ProjectionEvent) {
			event.Recovery = &RecoveryFact{Kind: RecoveryRequired, NodeID: nodes[14], RelationEdge: edges[13], CheckpointNodeID: nodes[13], DisplayName: "Recovery required"}
		}),
		fixtureEvent(13, eventIDs[12], taskID, baseTime, ProjectionRetryObserved, func(event *ProjectionEvent) {
			event.Retry = &RetryFact{EdgeID: edges[14], PreviousNodeID: nodes[6], RetryNodeID: nodes[7]}
		}),
		fixtureEvent(14, eventIDs[13], taskID, baseTime, ProjectionTokenDelta, func(event *ProjectionEvent) {
			event.TokenDelta = &TokenDeltaFact{}
		}),
	}
	return projectionFixture{projection: projection, events: events, revisionIDs: revisions}
}

func fixtureEvent(sequence uint64, eventID domain.EventID, taskID domain.TaskID, base time.Time,
	kind ProjectionEventKind, assign func(*ProjectionEvent)) ProjectionEvent {
	event := ProjectionEvent{Sequence: sequence, SourceEventID: eventID, TaskID: taskID,
		OccurredAt: base.Add(time.Duration(sequence) * time.Second), Kind: kind}
	assign(&event)
	return event
}

func mustTaskID(t *testing.T) domain.TaskID {
	t.Helper()
	value, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustNodeID(t *testing.T) domain.NodeID {
	t.Helper()
	value, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
