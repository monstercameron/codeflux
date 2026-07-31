package sessionprojection

import (
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

// ApplySessionSnapshot is the only snapshot entry point for authoritative
// remote state. It preserves drafts and clears any prior repair request only
// after accepting a complete, non-stale snapshot.
func ApplySessionSnapshot(
	projection Projection,
	snapshot SessionSnapshot,
) (Projection, error) {
	next := cloneProjection(projection)
	if err := snapshot.Session.Validate(); err != nil {
		return requestSnapshotRepair(
			next,
			RepairInvalidSnapshot,
			0,
			"validate session snapshot",
			err,
		)
	}
	if !projection.snapshot.Session.SessionID.IsZero() &&
		(snapshot.Session.SessionID != projection.snapshot.Session.SessionID ||
			snapshot.Session.ThreadID != projection.snapshot.Session.ThreadID) {
		return requestSnapshotRepair(
			next,
			RepairIdentityMismatch,
			0,
			"replace session snapshot identity",
			errors.New("snapshot identity differs from active projection"),
		)
	}
	if err := validateTaskProjectionSnapshot(snapshot); err != nil {
		return requestSnapshotRepair(
			next,
			RepairInvalidSnapshot,
			0,
			"validate task projection snapshot",
			err,
		)
	}
	currentTaskID := projection.snapshot.Session.TaskID
	incomingTaskID := snapshot.Session.TaskID
	if currentTaskID != nil {
		if incomingTaskID == nil || *currentTaskID != *incomingTaskID {
			return requestSnapshotRepair(
				next,
				RepairTaskIdentityMismatch,
				0,
				"replace session snapshot task identity",
				errors.New("snapshot task identity differs from active projection"),
			)
		}
	}
	if projection.snapshot.Task != nil && snapshot.Task == nil {
		return requestSnapshotRepair(
			next,
			RepairInvalidSnapshot,
			0,
			"replace task projection snapshot",
			errors.New("snapshot omits the active task projection"),
		)
	}
	if snapshot.Session.ThroughSequence < projection.diagnostics.LastAppliedSequence {
		return requestSnapshotRepair(
			next,
			RepairInvalidSnapshot,
			0,
			"replace session snapshot cursor",
			errors.New("snapshot cursor predates last applied event"),
		)
	}
	if !projection.snapshot.Session.SessionID.IsZero() &&
		(snapshot.Session.TaskRevision < projection.snapshot.Session.TaskRevision ||
			snapshot.Session.TaskRevision == projection.snapshot.Session.TaskRevision &&
				snapshot.Session.TaskState != projection.snapshot.Session.TaskState ||
			snapshot.GraphRevision < projection.snapshot.GraphRevision) {
		return requestSnapshotRepair(
			next,
			RepairInvalidSnapshot,
			0,
			"replace session snapshot revision",
			errors.New("snapshot regresses or contradicts a trusted entity revision"),
		)
	}
	if taskProjectionSnapshotRegresses(projection.snapshot.Task, snapshot.Task) {
		return requestSnapshotRepair(
			next,
			RepairInvalidSnapshot,
			0,
			"replace task projection revisions",
			errors.New("snapshot regresses a trusted task projection revision"),
		)
	}
	next.snapshot = cloneSessionSnapshot(snapshot)
	next.diagnostics.LastAppliedSequence = snapshot.Session.ThroughSequence
	next.diagnostics.LastEventKind = ""
	next.diagnostics.Repair = nil
	next.connection = ConnectionProjection{State: ConnectionReplaying}
	return next, nil
}

// ApplySessionEvent is the only ordered event entry point for authoritative
// remote state. Duplicates are counted and ignored. Gaps and projection
// inconsistencies retain the last trusted state and request snapshot repair.
func ApplySessionEvent(
	projection Projection,
	event events.SessionEvent,
) (Projection, error) {
	next := cloneProjection(projection)
	if projection.snapshot.Session.SessionID.IsZero() {
		next, repairErr := requestSnapshotRepair(
			next,
			RepairMissingSnapshot,
			event.Sequence,
			"apply event without session snapshot",
			errors.New("authoritative snapshot is missing"),
		)
		setRepairEventKind(&next, event.Kind)
		return next, repairErr
	}
	if next.diagnostics.Repair != nil {
		return next, ErrSnapshotRepairRequired
	}
	if err := event.Validate(); err != nil {
		reason := RepairInvalidEvent
		switch event.Kind {
		case events.KindTaskStateChanged:
			reason = RepairTaskTransitionMismatch
		case events.KindGraphSnapshot, events.KindGraphPatch:
			reason = RepairGraphRevisionMismatch
		}
		next, repairErr := requestSnapshotRepair(
			next, reason, event.Sequence, "validate session event", err,
		)
		setRepairEventKind(&next, event.Kind)
		return next, repairErr
	}
	if event.SessionID != next.snapshot.Session.SessionID ||
		event.ThreadID != next.snapshot.Session.ThreadID {
		next, repairErr := requestSnapshotRepair(
			next,
			RepairIdentityMismatch,
			event.Sequence,
			"apply session event identity",
			errors.New("event identity differs from active snapshot"),
		)
		setRepairEventKind(&next, event.Kind)
		return next, repairErr
	}
	last := next.diagnostics.LastAppliedSequence
	if event.Sequence <= last {
		next.diagnostics.DuplicateEvents++
		return next, nil
	}
	if event.Sequence != last+1 {
		next.diagnostics.SequenceGaps++
		next, repairErr := requestSnapshotRepair(
			next,
			RepairSequenceGap,
			event.Sequence,
			"apply session event sequence",
			fmt.Errorf("expected sequence %d", last+1),
		)
		setRepairEventKind(&next, event.Kind)
		return next, repairErr
	}
	var projectedTaskSnapshot *taskprojection.Snapshot
	if next.snapshot.Task != nil {
		currentTask, err := taskprojection.ApplySnapshot(*next.snapshot.Task)
		if err != nil {
			next, repairErr := requestSnapshotRepair(
				next, RepairInvalidSnapshot, event.Sequence,
				"restore task projection snapshot", err,
			)
			setRepairEventKind(&next, event.Kind)
			return next, repairErr
		}
		projectedTask, taskErr := taskprojection.ApplySessionEvent(currentTask, event)
		if taskErr != nil {
			reason := RepairTaskProjectionInconsistent
			if errors.Is(taskErr, taskprojection.ErrUnsupportedSessionEventProjection) {
				reason = RepairTaskProjectionUnsupported
			} else if _, _, typed := taskprojection.RepairSignal(taskErr, currentTask.LastSequence); !typed {
				return next, taskErr
			}
			next, repairErr := requestSnapshotRepair(
				next, reason, event.Sequence,
				"apply task projection event", taskErr,
			)
			setRepairEventKind(&next, event.Kind)
			return next, repairErr
		}
		projectedTaskSnapshot = &taskprojection.Snapshot{Projection: projectedTask}
	}
	if err := applyEventProjection(&next, event); err != nil {
		return next, err
	}
	if projectedTaskSnapshot != nil {
		next.snapshot.Task = projectedTaskSnapshot
	}
	next.snapshot.Session.ThroughSequence = event.Sequence
	next.diagnostics.LastAppliedSequence = event.Sequence
	next.diagnostics.AppliedEvents++
	next.diagnostics.LastEventKind = event.Kind
	return next, nil
}

func validateTaskProjectionSnapshot(snapshot SessionSnapshot) error {
	if snapshot.Task == nil {
		return nil
	}
	projected, err := taskprojection.ApplySnapshot(*snapshot.Task)
	if err != nil {
		return err
	}
	if snapshot.Session.TaskID == nil ||
		projected.TaskID != *snapshot.Session.TaskID {
		return errors.New("task projection identity differs from session snapshot")
	}
	if projected.State != snapshot.Session.TaskState ||
		projected.Revision != snapshot.Session.TaskRevision {
		return errors.New("task projection state differs from session snapshot")
	}
	if projected.LastSequence != snapshot.Session.ThroughSequence {
		return errors.New("task projection cursor differs from session snapshot")
	}
	return nil
}

func taskProjectionSnapshotRegresses(
	current *taskprojection.Snapshot,
	incoming *taskprojection.Snapshot,
) bool {
	if current == nil || incoming == nil {
		return false
	}
	left := current.Projection
	right := incoming.Projection
	return projectionEntityRevisionRegresses(left.Plan.Present, left.Plan.Revision, right.Plan.Present, right.Plan.Revision) ||
		projectionEntityRevisionRegresses(left.Tool.Present, left.Tool.Revision, right.Tool.Present, right.Tool.Revision) ||
		projectionEntityRevisionRegresses(left.Approval.Present, left.Approval.Revision, right.Approval.Present, right.Approval.Revision) ||
		projectionEntityRevisionRegresses(left.Checkpoint.Present, left.Checkpoint.Revision, right.Checkpoint.Present, right.Checkpoint.Revision) ||
		projectionEntityRevisionRegresses(left.Validation.Present, left.Validation.Revision, right.Validation.Present, right.Validation.Revision) ||
		projectionEntityRevisionRegresses(left.Review.Present, left.Review.Revision, right.Review.Present, right.Review.Revision) ||
		projectionEntityRevisionRegresses(left.Acceptance.Present, left.Acceptance.Revision, right.Acceptance.Present, right.Acceptance.Revision) ||
		projectionEntityRevisionRegresses(left.Budget.Present, left.Budget.Revision, right.Budget.Present, right.Budget.Revision) ||
		projectionEntityRevisionRegresses(left.Graph.Present, left.Graph.Revision, right.Graph.Present, right.Graph.Revision)
}

func projectionEntityRevisionRegresses(
	currentPresent bool,
	currentRevision uint64,
	incomingPresent bool,
	incomingRevision uint64,
) bool {
	return currentPresent && (!incomingPresent || incomingRevision < currentRevision)
}

func applyEventProjection(projection *Projection, event events.SessionEvent) error {
	switch event.Kind {
	case events.KindTaskStateChanged:
		return applyTaskTransition(projection, event)
	case events.KindGraphSnapshot:
		return applyGraphSnapshot(projection, event)
	case events.KindGraphPatch:
		return applyGraphPatch(projection, event)
	default:
		return nil
	}
}

func applyTaskTransition(projection *Projection, event events.SessionEvent) error {
	change := event.Payload.TaskStateChanged
	if event.TaskID == nil {
		return projectionFailure(
			projection,
			RepairTaskIdentityMismatch,
			event,
			errors.New("task event identity is missing"),
		)
	}
	snapshot := projection.snapshot.Session
	if snapshot.TaskID == nil {
		if snapshot.TaskRevision != 0 || snapshot.TaskState != "" {
			return projectionFailure(
				projection,
				RepairTaskTransitionMismatch,
				event,
				errors.New("task snapshot fields are incomplete"),
			)
		}
		taskID := *event.TaskID
		snapshot.TaskID = &taskID
		snapshot.TaskState = change.From
	} else if *snapshot.TaskID != *event.TaskID {
		return projectionFailure(
			projection,
			RepairTaskIdentityMismatch,
			event,
			errors.New("task event identity differs from snapshot"),
		)
	}
	if event.Revision != snapshot.TaskRevision+1 {
		return projectionFailure(
			projection,
			RepairTaskRevisionMismatch,
			event,
			fmt.Errorf("task revision %d does not follow %d", event.Revision, snapshot.TaskRevision),
		)
	}
	if change.From != snapshot.TaskState {
		return projectionFailure(
			projection,
			RepairTaskTransitionMismatch,
			event,
			fmt.Errorf("task transition starts at %s, snapshot is %s", change.From, snapshot.TaskState),
		)
	}
	if err := domain.ValidateTaskTransition(domain.TaskTransition{
		From: change.From, To: change.To, Approval: change.Approval,
	}); err != nil {
		return projectionFailure(projection, RepairTaskTransitionMismatch, event, err)
	}
	snapshot.TaskState = change.To
	snapshot.TaskRevision = event.Revision
	projection.snapshot.Session = snapshot
	return nil
}

func applyGraphSnapshot(projection *Projection, event events.SessionEvent) error {
	if event.Revision == 0 || event.Revision < projection.snapshot.GraphRevision {
		return projectionFailure(
			projection,
			RepairGraphRevisionMismatch,
			event,
			fmt.Errorf("graph snapshot revision %d predates %d", event.Revision, projection.snapshot.GraphRevision),
		)
	}
	projection.snapshot.GraphRevision = event.Revision
	return nil
}

func applyGraphPatch(projection *Projection, event events.SessionEvent) error {
	if projection.snapshot.GraphRevision == 0 ||
		event.Revision != projection.snapshot.GraphRevision+1 {
		return projectionFailure(
			projection,
			RepairGraphRevisionMismatch,
			event,
			fmt.Errorf(
				"graph patch revision %d does not follow %d",
				event.Revision,
				projection.snapshot.GraphRevision,
			),
		)
	}
	projection.snapshot.GraphRevision = event.Revision
	return nil
}

func projectionFailure(
	projection *Projection,
	reason RepairReason,
	event events.SessionEvent,
	cause error,
) error {
	next, err := requestSnapshotRepair(
		*projection,
		reason,
		event.Sequence,
		"apply "+string(event.Kind),
		cause,
	)
	setRepairEventKind(&next, event.Kind)
	*projection = next
	return err
}

func setRepairEventKind(projection *Projection, kind events.Kind) {
	if projection.diagnostics.Repair != nil {
		projection.diagnostics.Repair.EventKind = kind
	}
}

func requestSnapshotRepair(
	projection Projection,
	reason RepairReason,
	receivedSequence uint64,
	operation string,
	cause error,
) (Projection, error) {
	last := projection.diagnostics.LastAppliedSequence
	projection.diagnostics.Repair = &SnapshotRepairRequest{
		Reason:           reason,
		AfterSequence:    last,
		ExpectedSequence: last + 1,
		ReceivedSequence: receivedSequence,
	}
	projection.connection = ConnectionProjection{State: ConnectionDegraded}
	return projection, fmt.Errorf("%w: %s: %v", ErrSnapshotRepairRequired, operation, cause)
}
