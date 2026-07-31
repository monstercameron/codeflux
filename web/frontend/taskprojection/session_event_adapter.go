package taskprojection

import (
	"errors"
	"fmt"
	"slices"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

var ErrUnsupportedSessionEventProjection = errors.New("session event lacks required task projection facts")

// UnsupportedSessionEventProjectionError names only the durable event kind and
// absent structural fields. It never copies task content into diagnostics.
type UnsupportedSessionEventProjectionError struct {
	Kind    events.Kind
	Missing []string
}

func (failure *UnsupportedSessionEventProjectionError) Error() string {
	return fmt.Sprintf(
		"%s: kind=%s missing=%v",
		ErrUnsupportedSessionEventProjection,
		failure.Kind,
		failure.Missing,
	)
}

func (failure *UnsupportedSessionEventProjectionError) Unwrap() error {
	return ErrUnsupportedSessionEventProjection
}

func (failure *UnsupportedSessionEventProjectionError) MissingFacts() []string {
	return slices.Clone(failure.Missing)
}

// ApplySessionEvent is the honest adapter boundary from the durable event
// schema into the task reducer. Valid events irrelevant to TaskProjection use
// EventNoop so the shared session cursor still advances. Events whose required
// projection facts are absent from the schema request snapshot repair.
func ApplySessionEvent(current TaskProjection, event events.SessionEvent) (TaskProjection, error) {
	adapted, err := ProjectionEventFromSessionEvent(current, event)
	if err != nil {
		return cloneProjection(current), err
	}
	return ApplyEvent(current, adapted)
}

// ProjectionEventFromSessionEvent converts only facts present in the durable
// envelope or its typed payload. It does not infer policy, review bindings,
// validation authority, acknowledgement, or recovery safety.
func ProjectionEventFromSessionEvent(
	current TaskProjection,
	event events.SessionEvent,
) (ProjectionEvent, error) {
	if err := event.Validate(); err != nil {
		return ProjectionEvent{}, inconsistency(
			"invalid-session-event", "session", current.LastSequence+1, event.Sequence,
			"", string(event.Kind), err,
		)
	}
	if event.TaskID != nil && *event.TaskID != current.TaskID {
		return ProjectionEvent{}, inconsistency(
			"task-identity", "task", current.Revision, event.Revision,
			current.TaskID.String(), event.TaskID.String(), errors.New("session event task identity differs from projection"),
		)
	}
	adapted := ProjectionEvent{Sequence: event.Sequence, Kind: EventNoop}
	switch event.Kind {
	case events.KindPlanCreated, events.KindPlanChanged:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		if event.Revision != event.Payload.Plan.Revision {
			return ProjectionEvent{}, inconsistency(
				"plan-envelope-revision", "plan", event.Payload.Plan.Revision, event.Revision,
				"", "", errors.New("plan envelope and payload revisions differ"),
			)
		}
		adapted.Kind = EventPlanRevision
		adapted.Plan = PlanRevisionEvent{
			Revision: event.Payload.Plan.Revision, RedactedSummary: event.Payload.Plan.RedactedSummary,
		}
	case events.KindToolStarted, events.KindToolProgress, events.KindToolCompleted:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		adapted.Kind = EventToolUpdate
		adapted.Tool = ToolEvent{
			ExecutionID: event.Payload.Tool.ExecutionID,
			CommandName: event.Payload.Tool.CommandName,
			State:       domain.CommandExecutionState(event.Payload.Tool.State),
			Revision:    event.Revision,
			SafeSummary: event.Payload.Tool.RedactedSummary,
		}
	case events.KindApprovalRequested, events.KindApprovalResolved:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		adapted.Kind = EventApprovalUpdate
		adapted.Approval = ApprovalEvent{
			ID: event.Payload.Approval.ApprovalID, State: event.Payload.Approval.State,
			Scope: event.Payload.Approval.Scope, SafeReason: event.Payload.Approval.RedactedReason,
			Revision: event.Revision,
		}
	case events.KindTaskStateChanged:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		adapted.Kind = EventTaskTransition
		adapted.TaskTransition = TaskTransitionEvent{
			From: event.Payload.TaskStateChanged.From, To: event.Payload.TaskStateChanged.To,
			Approval: event.Payload.TaskStateChanged.Approval, Revision: event.Revision,
		}
	case events.KindBudgetUpdated:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		adapted.Kind = EventBudgetUpdate
		adapted.Budget = BudgetEvent{
			Revision: event.Revision, HardLimit: event.Payload.Budget.HardLimit,
			Reserved: event.Payload.Budget.Reserved, Actual: event.Payload.Budget.Actual,
		}
	case events.KindCheckpointCreated:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		adapted.Kind = EventCheckpoint
		adapted.Checkpoint = CheckpointEvent{
			ID: event.Payload.Checkpoint.CheckpointID, TaskRevision: event.Payload.Checkpoint.TaskRevision,
			PlanStep: event.Payload.Checkpoint.PlanStep, CreatedAt: event.Timestamp, Revision: event.Revision,
		}
	case events.KindGraphSnapshot:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		adapted.Kind = EventGraphSnapshot
		adapted.GraphSnapshot = GraphSnapshotEvent{Revision: event.Revision}
	case events.KindGraphPatch:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		adapted.Kind = EventGraphPatch
		if event.Revision > 0 {
			adapted.Graph = GraphPatchEvent{BaseRevision: event.Revision - 1, Revision: event.Revision}
		}
	case events.KindValidationUpdated:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		adapted.Kind = EventValidationUpdate
		adapted.Validation = ValidationEvent{
			ID: event.Payload.Validation.ValidationID, State: event.Payload.Validation.State,
			Required:     event.Payload.Validation.Required,
			Acknowledged: event.Payload.Validation.Acknowledged,
			SafeSummary:  event.Payload.Validation.RedactedSummary,
			Revision:     event.Revision, DiffRevision: event.Payload.Validation.DiffRevision,
		}
	case events.KindChangeAcceptanceUpdated:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		value := event.Payload.ChangeAcceptance
		adapted.Kind = EventAcceptanceUpdate
		adapted.Acceptance = AcceptanceEvent{
			State: value.State, Revision: event.Revision,
			Bindings: RevisionBindings{
				Diff: value.Bindings.Diff, Plan: value.Bindings.Plan,
				Validation: value.Bindings.Validation, Evidence: value.Bindings.Evidence,
				Graph: value.Bindings.Graph,
			},
		}
	case events.KindRecoveryRequired:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		value := event.Payload.RecoveryRequired
		classification := RecoveryClassification(value.Classification)
		adapted.Kind = EventRecoveryUpdate
		adapted.Recovery = classification
		adapted.RecoveryDetail = RecoveryEvent{
			Revision: event.Revision, Classification: classification,
			CheckpointID: value.CheckpointID, SafeReason: value.RedactedReason,
			DivergenceSummary:        value.DivergenceSummary,
			ExternalOutcomeAmbiguous: value.ExternalOutcomeAmbiguous,
			SafeResumeVerified:       value.SafeResumeVerified, ReconcileAvailable: value.ReconcileAvailable,
			PreservePatchAvailable: value.PreservePatchAvailable,
			Bindings: RevisionBindings{
				Diff: value.Bindings.Diff, Plan: value.Bindings.Plan,
				Validation: value.Bindings.Validation, Evidence: value.Bindings.Evidence,
				Graph: value.Bindings.Graph,
			},
			RelatedEventIDs: append([]domain.EventID(nil), value.RelatedEventIDs...),
			RelatedFiles:    append([]string(nil), value.RelatedFiles...),
		}
	case events.KindTaskProjectionInvalidated:
		if err := requireTaskEvent(current, event); err != nil {
			return ProjectionEvent{}, err
		}
		return ProjectionEvent{}, &UnsupportedSessionEventProjectionError{
			Kind: event.Kind, Missing: []string{"authoritative-session-snapshot"},
		}
	case events.KindMessageDelta, events.KindMessageFinal,
		events.KindThreadCreated, events.KindThreadRenamed, events.KindThreadArchived,
		events.KindForecastUpdated, events.KindUsageUpdated, events.KindCostUpdated,
		events.KindError:
		// These events are valid session-cursor facts, but TaskProjection does
		// not own their state. EventNoop advances the durable sequence only.
	default:
		return ProjectionEvent{}, inconsistency(
			"unsupported-session-kind", "session", 0, 0, "", string(event.Kind),
			errors.New("validated session event kind has no adapter case"),
		)
	}
	return adapted, nil
}

func requireTaskEvent(current TaskProjection, event events.SessionEvent) error {
	if event.TaskID != nil {
		return nil
	}
	return inconsistency(
		"task-identity", "task", current.Revision, event.Revision,
		current.TaskID.String(), "", errors.New("task-projecting session event has no task identity"),
	)
}
