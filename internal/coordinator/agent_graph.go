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

// declarePlan records the steps the diagram will hang work from.
//
// It does NOT yet project a plan region. A graph plan region must bind to a
// durable agent plan revision, and that binding requires the whole requirement,
// policy, forecast, and budget chain to have been recorded first. This run does
// not record one, so asking for the region produced a constraint failure and an
// empty diagram. Until the durable plan is written, every step points at the
// requirement, which is true: the work was done because that was asked for.
func (recorder *agentGraphRecorder) declarePlan(
	_ context.Context,
	plan durablePlan,
	steps []graphPlanStep,
) {
	if !recorder.available {
		return
	}
	recorder.planRevision = plan.Revision
	recorder.planSteps = plan.Steps
	for _, step := range steps {
		recorder.steps[step.ID] = recorder.requirement
	}
	recorder.planRegion = recorder.requirement
}

// graphPlanStep is one step the diagram shows.
type graphPlanStep struct {
	ID      string
	Summary string
}

// recordFileEdit records one file the agent wrote.
func (recorder *agentGraphRecorder) recordFileEdit(
	ctx context.Context,
	planStepID, path string,
	revision uint64,
	succeeded bool,
) {
	recorder.recordOperation(ctx, graph.OperationFileEdit,
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

// recordOperation records one operation against its plan step.
func (recorder *agentGraphRecorder) recordOperation(
	ctx context.Context,
	kind graph.OperationKind,
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
