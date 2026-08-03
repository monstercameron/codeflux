package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/storage"
)

// agentGraphRecorder builds the task graph as the work happens.
//
// The graph pane was empty because the only thing that ever projected a node
// was the draft-task path, which the composer no longer uses: a task now comes
// from intake instead. Nothing else in the product produced a graph fact, so
// the panel had nothing to draw for any run.
//
// What it records is the flow of the work: the requirement, the plan, each file
// the agent wrote, the command it ran, and the verdict. That is what this graph
// vocabulary describes — requirement, plan region, operation, effect, artifact —
// and it is derived from what actually happened rather than described.
type agentGraphRecorder struct {
	service      *GraphProjectionService
	repositories *storage.Repositories
	scope        storage.GraphQueryScope
	projection   graph.GraphProjection
	requirement  domain.NodeID
	planRegion   domain.NodeID
	steps        map[string]domain.NodeID
	last         domain.NodeID
	available    bool
	// failure is why recording stopped, kept so the run can say it. Swallowing
	// it left an empty diagram with no explanation anywhere, which is the same
	// silence that made a failed worktree read as "exit status 128".
	failure  error
	reported bool
	// planRevision and planSteps are the durable plan every drawn operation is
	// attributed to. Without them an operation names a plan that does not exist
	// and the store refuses it.
	planRevision uint64
	planSteps    map[string]string
}

// Failure reports why the diagram stopped being built, if it did.
func (recorder *agentGraphRecorder) Failure() error {
	if recorder == nil {
		return nil
	}
	return recorder.failure
}

// newAgentGraphRecorder starts a graph for one task.
//
// A failure here disables recording rather than failing the run: a missing
// diagram is a worse interface, and a refused task is a worse product.
func newAgentGraphRecorder(
	ctx context.Context,
	service *GraphProjectionService,
	repositories *storage.Repositories,
	projectID domain.ProjectID,
	taskID domain.TaskID,
	requirement string,
) *agentGraphRecorder {
	recorder := &agentGraphRecorder{
		service: service, repositories: repositories,
		scope: storage.GraphQueryScope{ProjectID: projectID, TaskID: taskID},
		steps: map[string]domain.NodeID{},
	}
	if service == nil || repositories == nil {
		return recorder
	}
	graphID, err := domain.NewGraphID()
	if err != nil {
		return recorder
	}
	identity, err := graph.NewGraph(graphID, taskID, time.Now().UTC())
	if err != nil {
		return recorder
	}
	projection, err := graph.NewGraphProjection(identity, nil, 0)
	if err != nil {
		return recorder
	}
	recorder.projection = projection
	recorder.available = true

	node, err := domain.NewNodeID()
	if err != nil {
		recorder.available = false
		return recorder
	}
	recorder.requirement = node
	recorder.record(ctx, "task.requirement-accepted", graph.ProjectionEvent{
		Kind: graph.ProjectionRequirementAccepted,
		Requirement: &graph.RequirementFact{
			NodeID: node, DisplayName: shortGraphLabel(requirement, "The request"),
		},
	})
	recorder.last = node
	return recorder
}

// declarePlan projects the plan region and its steps.
//
// By the time this runs, execution.recordDurablePlan has already committed the
// plan revision — agent_execution.go calls it first and only reaches this
// method with the result — so the plan region projected here binds to plan
// history that genuinely exists rather than one drawn ahead of the plan it
// names. When recordDurablePlan failed, plan.Revision is zero: no durable plan
// exists to attribute a region to, so none is projected, and every step falls
// back to pointing at the requirement, which is still true — the work was
// done because that was asked for.
func (recorder *agentGraphRecorder) declarePlan(
	ctx context.Context,
	plan durablePlan,
	steps []graphPlanStep,
) {
	if !recorder.available {
		return
	}
	recorder.planRevision = plan.Revision
	recorder.planSteps = plan.Steps
	if plan.Revision == 0 || len(steps) == 0 {
		recorder.planRegion = recorder.requirement
		return
	}

	regionNode, err := domain.NewNodeID()
	if err != nil {
		return
	}
	requirementEdge, err := domain.NewEdgeID()
	if err != nil {
		return
	}
	fact := &graph.PlanFact{
		Revision: plan.Revision, RegionNodeID: regionNode,
		RegionDisplayName: "The plan", RequirementNodeID: recorder.requirement,
		RequirementEdgeID: requirementEdge,
	}
	stepNodes := make(map[string]domain.NodeID, len(steps))
	for _, step := range steps {
		stepNode, err := domain.NewNodeID()
		if err != nil {
			return
		}
		controlEdge, err := domain.NewEdgeID()
		if err != nil {
			return
		}
		fact.Steps = append(fact.Steps, graph.PlanStepFact{
			StepID: step.ID, NodeID: stepNode, ControlEdge: controlEdge,
			DisplayName: shortGraphLabel(step.Summary, step.ID),
		})
		stepNodes[step.ID] = stepNode
	}
	recorder.record(ctx, "task.plan-declared", graph.ProjectionEvent{
		Kind: graph.ProjectionPlanDeclared, Plan: fact,
	})
	if !recorder.available {
		return
	}
	recorder.planRegion = regionNode
	for id, node := range stepNodes {
		recorder.steps[id] = node
	}
	recorder.last = regionNode
}

// graphPlanStep is one step the diagram shows.
type graphPlanStep struct {
	ID      string
	Summary string
}

// recordFileEdit records one file the agent wrote. It returns the node the
// edit was drawn as, or the zero NodeID when nothing was drawn, so a caller
// that later needs the artifact this edit produced can name the operation
// that produced it instead of guessing at the recorder's last-drawn node.
func (recorder *agentGraphRecorder) recordFileEdit(
	ctx context.Context,
	planStepID, path string,
	revision uint64,
	succeeded bool,
) domain.NodeID {
	return recorder.recordOperation(ctx, graph.OperationFileEdit,
		planStepID, path, revision, succeeded)
}

// recordCommand records one command the agent ran.
//
// A command is an effect rather than an operation: it reaches outside the
// worktree's contents and its result is evidence, which is exactly the
// distinction this vocabulary draws.
func (recorder *agentGraphRecorder) recordCommand(
	ctx context.Context,
	planStepID, command string,
	revision uint64,
	succeeded bool,
) {
	if !recorder.available {
		return
	}
	node, err := domain.NewNodeID()
	if err != nil {
		return
	}
	edge, err := domain.NewEdgeID()
	if err != nil {
		return
	}
	operation := recorder.steps[planStepID]
	if operation.IsZero() {
		operation = recorder.last
	}
	planStepID, revision = recorder.planAttribution(planStepID, revision)
	recorder.record(ctx, "task.effect-observed", graph.ProjectionEvent{
		Kind: graph.ProjectionEffectObserved,
		Effect: &graph.EffectFact{
			Kind: graph.EffectCommandCall, NodeID: node, ControlEdge: edge,
			OperationNode: operation, PlanRevision: revision,
			PlanStepID: planStepID, DisplayName: shortGraphLabel(command, "command"),
			State: graphState(succeeded),
		},
	})
	recorder.last = node
}

// recordOperation records one operation against its plan step and returns the
// node it was drawn as, or the zero NodeID when the recorder could not draw
// it.
func (recorder *agentGraphRecorder) recordOperation(
	ctx context.Context,
	kind graph.OperationKind,
	planStepID, label string,
	revision uint64,
	succeeded bool,
) domain.NodeID {
	if !recorder.available {
		return domain.NodeID{}
	}
	node, err := domain.NewNodeID()
	if err != nil {
		return domain.NodeID{}
	}
	edge, err := domain.NewEdgeID()
	if err != nil {
		return domain.NodeID{}
	}
	planNode := recorder.steps[planStepID]
	if planNode.IsZero() {
		planNode = recorder.planRegion
	}
	planStepID, revision = recorder.planAttribution(planStepID, revision)
	recorder.record(ctx, "task.operation-observed", graph.ProjectionEvent{
		Kind: graph.ProjectionOperationObserved,
		Operation: &graph.OperationFact{
			Kind: kind, NodeID: node, ControlEdge: edge,
			PlanNodeID: planNode, PlanRevision: revision, PlanStepID: planStepID,
			DisplayName: shortGraphLabel(label, string(kind)),
			State:       graphState(succeeded),
		},
	})
	recorder.last = node
	if !recorder.available {
		return domain.NodeID{}
	}
	return node
}

// recordValidation records one check of a validation obligation — for this
// run, the moment the test tool a "verify" plan step demands finishes and its
// verdict is known. That is deliberately when this is called: recording
// earlier would assert a pass or fail before either was true, and never
// recording it would leave the diagram showing files written and commands run
// but nothing that ever says whether the work was checked.
func (recorder *agentGraphRecorder) recordValidation(
	ctx context.Context,
	planStepID, label string,
	revision uint64,
	succeeded bool,
) {
	if !recorder.available {
		return
	}
	node, err := domain.NewNodeID()
	if err != nil {
		return
	}
	edge, err := domain.NewEdgeID()
	if err != nil {
		return
	}
	predecessor := recorder.steps[planStepID]
	if predecessor.IsZero() {
		predecessor = recorder.planRegion
	}
	planStepID, revision = recorder.planAttribution(planStepID, revision)
	recorder.record(ctx, "task.validation-observed", graph.ProjectionEvent{
		Kind: graph.ProjectionValidationObserved,
		Validation: &graph.ValidationFact{
			ObligationNodeID: node, ControlEdge: edge, Predecessor: predecessor,
			DisplayName:  shortGraphLabel(label, "validation"),
			State:        graphState(succeeded),
			PlanRevision: revision, PlanStepID: planStepID,
		},
	})
	// last is deliberately left untouched: an effect drawn for the same tool
	// call still chains from the operation that preceded it, not from the
	// obligation node just drawn beside it.
}

// recordArtifact records one file the agent produced that has actually been
// stored durably. The fact is projected only after the caller's own durable
// write has committed — RecordProducedArtifact succeeding is what makes
// "this artifact exists" true, and drawing the node before that write
// succeeded would show a result nothing in storage backs.
func (recorder *agentGraphRecorder) recordArtifact(
	ctx context.Context,
	kind graph.ArtifactKind,
	producer domain.NodeID,
	planStepID, label string,
	revision uint64,
	succeeded bool,
) {
	if !recorder.available || producer.IsZero() {
		return
	}
	node, err := domain.NewNodeID()
	if err != nil {
		return
	}
	edge, err := domain.NewEdgeID()
	if err != nil {
		return
	}
	planStepID, revision = recorder.planAttribution(planStepID, revision)
	recorder.record(ctx, "task.artifact-observed", graph.ProjectionEvent{
		Kind: graph.ProjectionArtifactObserved,
		Artifact: &graph.ArtifactFact{
			Kind: kind, NodeID: node, ProvenanceEdge: edge, ProducerNode: producer,
			DisplayName:  shortGraphLabel(label, string(kind)),
			State:        graphState(succeeded),
			PlanRevision: revision, PlanStepID: planStepID,
		},
	})
}

// planAttribution settles which plan a drawn operation is attributed to.
//
// The step arrives already named in the stored plan's vocabulary, because the
// run adopts those identities before it starts and the tool journal reports
// them back. This only supplies the plan revision when a caller did not carry
// one, so that an operation is never drawn against revision zero — the store
// reads that as an operation with no plan at all and refuses it.
func (recorder *agentGraphRecorder) planAttribution(
	planStepID string,
	revision uint64,
) (string, uint64) {
	if revision == 0 {
		revision = recorder.planRevision
	}
	return planStepID, revision
}

// record appends the durable event and projects the fact from it.
//
// The durable event comes first because the graph is a projection of what the
// task journal already recorded. A node with no event behind it would be a
// diagram of something that never happened.
func (recorder *agentGraphRecorder) record(
	ctx context.Context,
	eventType string,
	fact graph.ProjectionEvent,
) {
	eventID, err := domain.NewEventID()
	if err != nil {
		recorder.available = false
		return
	}
	revisionID, err := domain.NewGraphRevisionID()
	if err != nil {
		recorder.available = false
		return
	}
	payload, err := json.Marshal(map[string]string{"node": string(fact.Kind)})
	if err != nil {
		recorder.available = false
		return
	}
	durable, err := recorder.repositories.AppendTaskEvent(ctx, storage.AppendTaskEvent{
		ID: eventID, TaskID: recorder.scope.TaskID, EventType: eventType,
		PayloadJSON: string(payload), IdempotencyKey: eventID.String(),
	})
	if err != nil {
		recorder.failure = fmt.Errorf("record %s: %w", eventType, err)
		recorder.available = false
		return
	}
	projected, err := recorder.service.ProjectCommittedTaskEvent(
		ctx, recorder.scope, recorder.projection, durable, fact, revisionID)
	if err != nil {
		recorder.failure = fmt.Errorf("project %s: %w", fact.Kind, err)
		// One rejected fact does not invalidate the ones already drawn, but it
		// does mean the projection and this recorder have diverged, so nothing
		// further is recorded rather than building on a state that is wrong.
		recorder.available = false
		return
	}
	recorder.projection = projected.Projection
}

// graphState maps an outcome onto the projection vocabulary.
func graphState(succeeded bool) graph.ProjectionState {
	if succeeded {
		return graph.ProjectionPassedState
	}
	return graph.ProjectionFailedState
}

// shortGraphLabel bounds a node label to something a diagram can hold.
func shortGraphLabel(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	if index := strings.IndexByte(trimmed, '\n'); index >= 0 {
		trimmed = trimmed[:index]
	}
	const bound = 72
	if len(trimmed) > bound {
		trimmed = trimmed[:bound] + "…"
	}
	return trimmed
}
