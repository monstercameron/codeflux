package taskprojection

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

type ProjectionError struct {
	Code     string
	Entity   string
	Expected uint64
	Observed uint64
	From     string
	To       string
	Cause    error
}

func (failure *ProjectionError) Error() string {
	return fmt.Sprintf(
		"task projection inconsistency code=%s entity=%s expected=%d observed=%d from=%s to=%s",
		failure.Code, failure.Entity, failure.Expected, failure.Observed, failure.From, failure.To,
	)
}

func (failure *ProjectionError) Unwrap() error { return failure.Cause }

type SnapshotRepairRequest struct {
	Required      bool
	ReasonCode    string
	Entity        string
	AfterSequence uint64
}

type SafeDiagnostic struct {
	Code     string
	Entity   string
	Expected uint64
	Observed uint64
	From     string
	To       string
}

// RepairSignal converts only typed projection inconsistencies into a fresh-
// snapshot request and a bounded diagnostic containing no task content.
func RepairSignal(err error, afterSequence uint64) (SnapshotRepairRequest, SafeDiagnostic, bool) {
	var failure *ProjectionError
	if !errors.As(err, &failure) {
		return SnapshotRepairRequest{}, SafeDiagnostic{}, false
	}
	return SnapshotRepairRequest{
			Required: true, ReasonCode: failure.Code, Entity: failure.Entity,
			AfterSequence: afterSequence,
		}, SafeDiagnostic{
			Code: failure.Code, Entity: failure.Entity,
			Expected: failure.Expected, Observed: failure.Observed,
			From: failure.From, To: failure.To,
		}, true
}

type EventKind string

const (
	EventNoop             EventKind = "noop"
	EventTaskTransition   EventKind = "task-transition"
	EventPlanRevision     EventKind = "plan-revision"
	EventToolUpdate       EventKind = "tool-update"
	EventApprovalUpdate   EventKind = "approval-update"
	EventCheckpoint       EventKind = "checkpoint"
	EventValidationUpdate EventKind = "validation-update"
	EventAcceptanceUpdate EventKind = "acceptance-update"
	EventBudgetUpdate     EventKind = "budget-update"
	EventReviewRevision   EventKind = "review-revision"
	EventGraphSnapshot    EventKind = "graph-snapshot"
	EventGraphPatch       EventKind = "graph-patch"
	EventRecoveryUpdate   EventKind = "recovery-update"
)

type TaskTransitionEvent struct {
	From     domain.TaskState
	To       domain.TaskState
	Revision uint64
	Approval domain.ApprovalRequestState
}

type PlanRevisionEvent struct {
	Revision        uint64
	RedactedSummary string
}

type ToolEvent struct {
	ExecutionID string
	CommandName string
	State       domain.CommandExecutionState
	Revision    uint64
	SafeSummary string
}

type ApprovalEvent struct {
	ID         domain.ApprovalID
	State      domain.ApprovalRequestState
	Scope      string
	SafeReason string
	Revision   uint64
}

type CheckpointEvent struct {
	ID           domain.CheckpointID
	TaskRevision uint64
	PlanStep     string
	CreatedAt    time.Time
	Revision     uint64
}

type ValidationEvent struct {
	ID           domain.ValidationID
	State        domain.ValidationState
	Required     bool
	Acknowledged bool
	SafeSummary  string
	Revision     uint64
	DiffRevision uint64
}

type AcceptanceEvent struct {
	State    domain.ChangeAcceptanceState
	Revision uint64
	Bindings RevisionBindings
}

type BudgetEvent struct {
	Revision  uint64
	HardLimit domain.Money
	Reserved  domain.Money
	Actual    domain.Money
}

type ReviewRevisionEvent struct {
	Revision uint64
	Bindings RevisionBindings
}

type GraphPatchEvent struct {
	BaseRevision uint64
	Revision     uint64
}

type GraphSnapshotEvent struct {
	Revision uint64
}

type RecoveryEvent struct {
	Revision                 uint64
	Classification           RecoveryClassification
	CheckpointID             *domain.CheckpointID
	SafeReason               string
	DivergenceSummary        string
	ExternalOutcomeAmbiguous bool
	SafeResumeVerified       bool
	ReconcileAvailable       bool
	PreservePatchAvailable   bool
	Bindings                 RevisionBindings
	RelatedEventIDs          []domain.EventID
	RelatedFiles             []string
}

type ProjectionEvent struct {
	Sequence       uint64
	Kind           EventKind
	TaskTransition TaskTransitionEvent
	Plan           PlanRevisionEvent
	Tool           ToolEvent
	Approval       ApprovalEvent
	Checkpoint     CheckpointEvent
	Validation     ValidationEvent
	Acceptance     AcceptanceEvent
	Budget         BudgetEvent
	Review         ReviewRevisionEvent
	GraphSnapshot  GraphSnapshotEvent
	Graph          GraphPatchEvent
	Recovery       RecoveryClassification
	RecoveryDetail RecoveryEvent
}

func Project(snapshot Snapshot, ordered []ProjectionEvent) (TaskProjection, error) {
	projection, err := ApplySnapshot(snapshot)
	if err != nil {
		return TaskProjection{}, err
	}
	for _, event := range ordered {
		projection, err = ApplyEvent(projection, event)
		if err != nil {
			return projection, err
		}
	}
	return projection, nil
}

func ApplyEvent(current TaskProjection, event ProjectionEvent) (TaskProjection, error) {
	if event.Sequence != current.LastSequence+1 {
		return cloneProjection(current), inconsistency(
			"event-sequence", "session", current.LastSequence+1, event.Sequence, "", "",
			errors.New("ordered event sequence mismatch"),
		)
	}
	var (
		next TaskProjection
		err  error
	)
	switch event.Kind {
	case EventNoop:
		next = cloneProjection(current)
	case EventTaskTransition:
		next, err = ApplyTaskTransition(current, event.TaskTransition)
	case EventPlanRevision:
		next, err = ApplyPlanRevision(current, event.Plan)
	case EventToolUpdate:
		next, err = ApplyToolUpdate(current, event.Tool)
	case EventApprovalUpdate:
		next, err = ApplyApprovalUpdate(current, event.Approval)
	case EventCheckpoint:
		next, err = ApplyCheckpointUpdate(current, event.Checkpoint)
	case EventValidationUpdate:
		next, err = ApplyValidationUpdate(current, event.Validation)
	case EventAcceptanceUpdate:
		next, err = ApplyAcceptanceUpdate(current, event.Acceptance)
	case EventBudgetUpdate:
		next, err = ApplyBudgetUpdate(current, event.Budget)
	case EventReviewRevision:
		next, err = ApplyReviewRevision(current, event.Review)
	case EventGraphSnapshot:
		next, err = ApplyGraphSnapshot(current, event.GraphSnapshot)
	case EventGraphPatch:
		next, err = ApplyGraphPatch(current, event.Graph)
	case EventRecoveryUpdate:
		next, err = ApplyRecoveryUpdate(current, event.Recovery)
		if err == nil && event.RecoveryDetail.Revision > 0 {
			next, err = ApplyRecoveryDetailUpdate(next, event.RecoveryDetail)
		}
	default:
		err = inconsistency("event-kind", "session", 0, 0, "", string(event.Kind), errors.New("unknown projection event"))
	}
	if err != nil {
		return cloneProjection(current), ensureInconsistency(err, "event", current.LastSequence+1, event.Sequence)
	}
	next.LastSequence = event.Sequence
	return next, nil
}

func ApplyTaskTransition(current TaskProjection, event TaskTransitionEvent) (TaskProjection, error) {
	if event.Revision != current.Revision+1 {
		return current, inconsistency("task-revision", "task", current.Revision+1, event.Revision,
			string(current.State), string(event.To), errors.New("task revision mismatch"))
	}
	if event.From != current.State {
		return current, inconsistency("task-state-base", "task", current.Revision, event.Revision,
			string(current.State), string(event.From), errors.New("task transition base mismatch"))
	}
	if err := domain.ValidateTaskTransition(domain.TaskTransition{
		From: event.From, To: event.To, Approval: event.Approval,
	}); err != nil {
		return current, inconsistency("task-transition", "task", current.Revision+1, event.Revision,
			string(event.From), string(event.To), err)
	}
	next := cloneProjection(current)
	next.State = event.To
	next.Revision = event.Revision
	if event.From == domain.TaskStateAwaitingPlanApproval && event.To == domain.TaskStateReady {
		if !next.Plan.Present || event.Approval != domain.ApprovalRequestStateGranted {
			return current, inconsistency("plan-approval", "plan", current.Revision+1, event.Revision,
				string(next.Plan.Approval), string(event.Approval), errors.New("ready transition requires the current approved plan"))
		}
		next.Plan.Approval = domain.ApprovalRequestStateGranted
	}
	if event.From == domain.TaskStateAwaitingPlanApproval && event.To == domain.TaskStateForecasting && next.Plan.Present {
		next.Plan.Approval = domain.ApprovalRequestStateCancelled
	}
	return next, nil
}

func ApplyPlanRevision(current TaskProjection, event PlanRevisionEvent) (TaskProjection, error) {
	expected := uint64(1)
	if current.Plan.Present {
		expected = current.Plan.Revision + 1
	}
	if event.Revision != expected || strings.TrimSpace(event.RedactedSummary) == "" {
		return current, inconsistency("plan-revision", "plan", expected, event.Revision, "", "", errors.New("invalid plan revision"))
	}
	next := cloneProjection(current)
	if next.Plan.Present {
		next.Plan.PriorRevisions = append(next.Plan.PriorRevisions, next.Plan.Revision)
	}
	next.Plan.Present = true
	next.Plan.Revision = event.Revision
	next.Plan.RedactedSummary = strings.TrimSpace(event.RedactedSummary)
	next.Plan.Approval = domain.ApprovalRequestStatePending
	return next, nil
}

func ApplyToolUpdate(current TaskProjection, event ToolEvent) (TaskProjection, error) {
	expected := uint64(1)
	if current.Tool.Present {
		expected = current.Tool.Revision + 1
	}
	if event.Revision != expected || strings.TrimSpace(event.ExecutionID) == "" ||
		strings.TrimSpace(event.CommandName) == "" || !event.State.IsValid() {
		return current, inconsistency("tool-revision", "tool", expected, event.Revision, "", string(event.State), errors.New("invalid tool update"))
	}
	if current.Tool.Active() {
		if event.ExecutionID != current.Tool.ExecutionID {
			return current, inconsistency("tool-identity", "tool", expected, event.Revision,
				current.Tool.ExecutionID, event.ExecutionID, errors.New("active tool identity changed"))
		}
		if event.State != current.Tool.State {
			if err := domain.ValidateCommandExecutionTransition(domain.CommandExecutionTransition{
				From: current.Tool.State, To: event.State,
			}); err != nil {
				return current, inconsistency("tool-transition", "tool", expected, event.Revision,
					string(current.Tool.State), string(event.State), err)
			}
		}
	} else {
		if current.Tool.Present && current.Tool.ExecutionID == event.ExecutionID {
			return current, inconsistency("tool-identity-reuse", "tool", expected, event.Revision,
				current.Tool.ExecutionID, event.ExecutionID, errors.New("terminal tool identity was reused"))
		}
		if event.State != domain.CommandExecutionStatePending && event.State != domain.CommandExecutionStateRunning {
			return current, inconsistency("tool-start-state", "tool", expected, event.Revision,
				"", string(event.State), errors.New("new tool must start pending or running"))
		}
	}
	next := cloneProjection(current)
	next.Tool = ToolProjection{
		Present: true, ExecutionID: event.ExecutionID, CommandName: event.CommandName,
		State: event.State, Revision: event.Revision, SafeSummary: event.SafeSummary,
	}
	return next, nil
}

func ApplyApprovalUpdate(current TaskProjection, event ApprovalEvent) (TaskProjection, error) {
	expected := uint64(1)
	if current.Approval.Present {
		expected = current.Approval.Revision + 1
	}
	if event.Revision != expected || event.ID.IsZero() || !event.State.IsValid() {
		return current, inconsistency("approval-revision", "approval", expected, event.Revision, "", string(event.State), errors.New("invalid approval update"))
	}
	if current.Approval.Pending() {
		if current.Approval.ID != event.ID {
			return current, inconsistency("approval-identity", "approval", expected, event.Revision,
				current.Approval.ID.String(), event.ID.String(), errors.New("pending approval identity changed"))
		}
		if err := domain.ValidateApprovalRequestTransition(current.Approval.State, event.State); err != nil {
			return current, inconsistency("approval-transition", "approval", expected, event.Revision,
				string(current.Approval.State), string(event.State), err)
		}
	} else {
		if current.Approval.Present && current.Approval.ID == event.ID {
			return current, inconsistency("approval-identity-reuse", "approval", expected, event.Revision,
				current.Approval.ID.String(), event.ID.String(), errors.New("terminal approval identity was reused"))
		}
		if event.State != domain.ApprovalRequestStatePending {
			return current, inconsistency("approval-start-state", "approval", expected, event.Revision,
				"", string(event.State), errors.New("new approval must be pending"))
		}
	}
	next := cloneProjection(current)
	next.Approval = ApprovalProjection{
		Present: true, ID: event.ID, State: event.State, Scope: event.Scope,
		SafeReason: event.SafeReason, Revision: event.Revision,
	}
	return next, nil
}

func ApplyCheckpointUpdate(current TaskProjection, event CheckpointEvent) (TaskProjection, error) {
	expected := uint64(1)
	if current.Checkpoint.Present {
		expected = current.Checkpoint.Revision + 1
	}
	if event.Revision != expected || event.ID.IsZero() || event.TaskRevision == 0 ||
		event.TaskRevision > current.Revision || event.CreatedAt.IsZero() ||
		event.CreatedAt.Location() != time.UTC || strings.TrimSpace(event.PlanStep) == "" {
		return current, inconsistency("checkpoint-revision", "checkpoint", expected, event.Revision, "", "", errors.New("invalid checkpoint update"))
	}
	if current.Checkpoint.Present && event.TaskRevision < current.Checkpoint.TaskRevision {
		return current, inconsistency("checkpoint-task-revision", "checkpoint",
			current.Checkpoint.TaskRevision, event.TaskRevision, "", "", errors.New("checkpoint task revision regressed"))
	}
	next := cloneProjection(current)
	next.Checkpoint = CheckpointProjection{
		Present: true, ID: event.ID, TaskRevision: event.TaskRevision,
		PlanStep: event.PlanStep, CreatedAt: event.CreatedAt, Revision: event.Revision,
	}
	return next, nil
}

func ApplyValidationUpdate(current TaskProjection, event ValidationEvent) (TaskProjection, error) {
	expected := uint64(1)
	if current.Validation.Present {
		expected = current.Validation.Revision + 1
	}
	if event.Revision != expected || event.ID.IsZero() || !event.State.IsValid() || event.DiffRevision == 0 {
		return current, inconsistency("validation-revision", "validation", expected, event.Revision, "", string(event.State), errors.New("invalid validation update"))
	}
	if current.Validation.Present {
		if !current.Validation.State.IsTerminal() && current.Validation.ID != event.ID {
			return current, inconsistency("validation-identity", "validation", expected, event.Revision,
				current.Validation.ID.String(), event.ID.String(), errors.New("running validation identity changed"))
		}
		if current.Validation.ID == event.ID {
			if err := domain.ValidateValidationTransition(current.Validation.State, event.State); err != nil {
				return current, inconsistency("validation-transition", "validation", expected, event.Revision,
					string(current.Validation.State), string(event.State), err)
			}
		} else if event.State != domain.ValidationStatePending && event.State != domain.ValidationStateRunning {
			return current, inconsistency("validation-start-state", "validation", expected, event.Revision,
				"", string(event.State), errors.New("new validation must start pending or running"))
		}
	} else if event.State != domain.ValidationStatePending &&
		event.State != domain.ValidationStateRunning {
		return current, inconsistency("validation-start-state", "validation", expected, event.Revision,
			"", string(event.State), errors.New("new validation must start pending or running"))
	}
	next := cloneProjection(current)
	next.Validation = ValidationProjection{
		Present: true, ID: event.ID, State: event.State, Required: event.Required,
		Acknowledged: event.Acknowledged, SafeSummary: event.SafeSummary,
		Revision: event.Revision, DiffRevision: event.DiffRevision,
	}
	return next, nil
}

func ApplyAcceptanceUpdate(current TaskProjection, event AcceptanceEvent) (TaskProjection, error) {
	expected := uint64(1)
	if current.Acceptance.Present {
		expected = current.Acceptance.Revision + 1
	}
	if event.Revision != expected || !event.State.IsValid() {
		return current, inconsistency("acceptance-revision", "acceptance", expected, event.Revision, "", string(event.State), errors.New("invalid acceptance update"))
	}
	if current.Acceptance.Present {
		bindingsChangedForRepair := current.Acceptance.State == domain.ChangeAcceptanceStateRepairRequested &&
			event.State == domain.ChangeAcceptanceStatePending
		if event.Bindings != current.Acceptance.Bindings && !bindingsChangedForRepair {
			return current, inconsistency("acceptance-bindings", "acceptance", expected, event.Revision, "bound", "changed", errors.New("acceptance bindings changed within a review"))
		}
		if err := domain.ValidateChangeAcceptanceTransition(current.Acceptance.State, event.State); err != nil {
			return current, inconsistency("acceptance-transition", "acceptance", expected, event.Revision,
				string(current.Acceptance.State), string(event.State), err)
		}
	} else if event.State != domain.ChangeAcceptanceStatePending {
		return current, inconsistency("acceptance-start-state", "acceptance", expected, event.Revision,
			"", string(event.State), errors.New("new acceptance must start pending"))
	}
	next := cloneProjection(current)
	next.Acceptance = AcceptanceProjection{Present: true, State: event.State, Revision: event.Revision, Bindings: event.Bindings}
	return next, nil
}

func ApplyBudgetUpdate(current TaskProjection, event BudgetEvent) (TaskProjection, error) {
	expected := uint64(1)
	if current.Budget.Present {
		expected = current.Budget.Revision + 1
	}
	valid := event.HardLimit.Validate() == nil && event.Reserved.Validate() == nil && event.Actual.Validate() == nil &&
		event.HardLimit.Currency == event.Reserved.Currency && event.HardLimit.Currency == event.Actual.Currency &&
		event.HardLimit.MinorUnits >= 0 && event.Reserved.MinorUnits >= 0 && event.Actual.MinorUnits >= 0
	if event.Revision != expected || !valid {
		return current, inconsistency("budget-revision", "budget", expected, event.Revision, "", "", errors.New("invalid budget update"))
	}
	next := cloneProjection(current)
	next.Budget = BudgetProjection{
		Present: true, Revision: event.Revision, HardLimit: event.HardLimit,
		Reserved: event.Reserved, Actual: event.Actual,
	}
	return next, nil
}

func ApplyReviewRevision(current TaskProjection, event ReviewRevisionEvent) (TaskProjection, error) {
	expected := uint64(1)
	if current.Review.Present {
		expected = current.Review.Revision + 1
	}
	if event.Revision != expected || !event.Bindings.complete() {
		return current, inconsistency("review-revision", "review", expected, event.Revision, "", "", errors.New("invalid review revision"))
	}
	next := cloneProjection(current)
	next.Review = ReviewProjection{Present: true, Revision: event.Revision, Bindings: event.Bindings}
	return next, nil
}

func ApplyGraphPatch(current TaskProjection, event GraphPatchEvent) (TaskProjection, error) {
	expectedBase := current.Graph.Revision
	if event.BaseRevision != expectedBase || event.Revision != expectedBase+1 {
		return current, inconsistency("graph-revision", "graph", expectedBase, event.BaseRevision, "", "", errors.New("graph patch base mismatch"))
	}
	next := cloneProjection(current)
	next.Graph = GraphProjection{Present: true, Revision: event.Revision}
	return next, nil
}

func ApplyGraphSnapshot(current TaskProjection, event GraphSnapshotEvent) (TaskProjection, error) {
	if event.Revision == 0 || current.Graph.Present && event.Revision < current.Graph.Revision {
		return current, inconsistency(
			"graph-snapshot-revision", "graph", current.Graph.Revision, event.Revision,
			"", "", errors.New("graph snapshot revision regressed"),
		)
	}
	next := cloneProjection(current)
	next.Graph = GraphProjection{Present: true, Revision: event.Revision}
	return next, nil
}

func ApplyRecoveryUpdate(current TaskProjection, classification RecoveryClassification) (TaskProjection, error) {
	if !classification.IsValid() {
		return current, inconsistency("recovery-classification", "recovery", 0, 0, "", string(classification), errors.New("invalid recovery classification"))
	}
	next := cloneProjection(current)
	next.Recovery = classification
	return next, nil
}

func ApplyRecoveryDetailUpdate(current TaskProjection, event RecoveryEvent) (TaskProjection, error) {
	expected := uint64(1)
	if current.RecoveryDetail.Present {
		expected = current.RecoveryDetail.Revision + 1
	}
	if event.Revision != expected || !event.Classification.IsValid() ||
		strings.TrimSpace(event.SafeReason) == "" || strings.TrimSpace(event.DivergenceSummary) == "" ||
		!event.Bindings.complete() ||
		event.ExternalOutcomeAmbiguous != (event.Classification == RecoveryAmbiguousOutcome) {
		return current, inconsistency("recovery-revision", "recovery", expected, event.Revision,
			string(current.Recovery), string(event.Classification), errors.New("invalid recovery detail"))
	}
	if event.Classification == RecoverySafeResume && !event.SafeResumeVerified ||
		event.Classification == RecoveryNeedsReconcile && !event.ReconcileAvailable ||
		(event.Classification == RecoveryPreserveOnly || event.Classification == RecoveryAmbiguousOutcome) &&
			!event.PreservePatchAvailable {
		return current, inconsistency("recovery-action", "recovery", expected, event.Revision,
			"", string(event.Classification), errors.New("recovery lacks its required safe action"))
	}
	next := cloneProjection(current)
	next.Recovery = event.Classification
	detail := RecoveryProjection{
		Present: true, Revision: event.Revision, Classification: event.Classification,
		CheckpointID: event.CheckpointID, SafeReason: event.SafeReason,
		DivergenceSummary:        event.DivergenceSummary,
		ExternalOutcomeAmbiguous: event.ExternalOutcomeAmbiguous,
		SafeResumeVerified:       event.SafeResumeVerified, ReconcileAvailable: event.ReconcileAvailable,
		PreservePatchAvailable: event.PreservePatchAvailable, Bindings: event.Bindings,
		RelatedEventIDs: append([]domain.EventID(nil), event.RelatedEventIDs...),
		RelatedFiles:    append([]string(nil), event.RelatedFiles...),
	}
	if event.CheckpointID != nil {
		value := *event.CheckpointID
		detail.CheckpointID = &value
	}
	next.RecoveryDetail = detail
	return next, nil
}

func validateProjection(projection TaskProjection) error {
	if projection.TaskID.IsZero() || !projection.State.IsValid() ||
		projection.Revision == 0 && projection.State != domain.TaskStateDraft {
		return errors.New("task snapshot identity, state, or revision is invalid")
	}
	if projection.Recovery == "" {
		projection.Recovery = RecoveryNone
	}
	if !projection.Recovery.IsValid() {
		return errors.New("task snapshot recovery classification is invalid")
	}
	if projection.Plan.Present && (projection.Plan.Revision == 0 ||
		strings.TrimSpace(projection.Plan.RedactedSummary) == "" || !projection.Plan.Approval.IsValid()) {
		return errors.New("task snapshot plan is invalid")
	}
	if projection.Tool.Present && (projection.Tool.Revision == 0 ||
		strings.TrimSpace(projection.Tool.ExecutionID) == "" ||
		strings.TrimSpace(projection.Tool.CommandName) == "" || !projection.Tool.State.IsValid()) {
		return errors.New("task snapshot tool is invalid")
	}
	if projection.Approval.Present && (projection.Approval.Revision == 0 ||
		projection.Approval.ID.IsZero() || !projection.Approval.State.IsValid()) {
		return errors.New("task snapshot approval is invalid")
	}
	if projection.Validation.Present && (projection.Validation.Revision == 0 ||
		projection.Validation.ID.IsZero() || !projection.Validation.State.IsValid() ||
		projection.Validation.DiffRevision == 0) {
		return errors.New("task snapshot validation is invalid")
	}
	if projection.Checkpoint.Present && (projection.Checkpoint.Revision == 0 ||
		projection.Checkpoint.ID.IsZero() || projection.Checkpoint.TaskRevision == 0 ||
		projection.Checkpoint.TaskRevision > projection.Revision || projection.Checkpoint.CreatedAt.IsZero() ||
		projection.Checkpoint.CreatedAt.Location() != time.UTC || strings.TrimSpace(projection.Checkpoint.PlanStep) == "") {
		return errors.New("task snapshot checkpoint is invalid")
	}
	if projection.Budget.Present {
		validMoney := projection.Budget.HardLimit.Validate() == nil && projection.Budget.Reserved.Validate() == nil &&
			projection.Budget.Actual.Validate() == nil &&
			projection.Budget.HardLimit.Currency == projection.Budget.Reserved.Currency &&
			projection.Budget.HardLimit.Currency == projection.Budget.Actual.Currency &&
			projection.Budget.HardLimit.MinorUnits >= 0 && projection.Budget.Reserved.MinorUnits >= 0 &&
			projection.Budget.Actual.MinorUnits >= 0
		if !validMoney {
			return errors.New("task snapshot budget is invalid")
		}
	}
	if projection.Review.Present && (projection.Review.Revision == 0 || !projection.Review.Bindings.complete()) {
		return errors.New("task snapshot review is invalid")
	}
	if projection.Acceptance.Present && (projection.Acceptance.Revision == 0 ||
		!projection.Acceptance.State.IsValid() || !projection.Acceptance.Bindings.complete()) {
		return errors.New("task snapshot acceptance is invalid")
	}
	if projection.Graph.Present && projection.Graph.Revision == 0 {
		return errors.New("task snapshot graph is invalid")
	}
	if projection.RecoveryDetail.Present {
		detail := projection.RecoveryDetail
		if detail.Revision == 0 || !detail.Classification.IsValid() || detail.Classification != projection.Recovery ||
			strings.TrimSpace(detail.SafeReason) == "" || strings.TrimSpace(detail.DivergenceSummary) == "" ||
			!detail.Bindings.complete() ||
			detail.ExternalOutcomeAmbiguous != (detail.Classification == RecoveryAmbiguousOutcome) ||
			detail.Classification == RecoverySafeResume && !detail.SafeResumeVerified ||
			detail.Classification == RecoveryNeedsReconcile && !detail.ReconcileAvailable ||
			(detail.Classification == RecoveryPreserveOnly || detail.Classification == RecoveryAmbiguousOutcome) &&
				!detail.PreservePatchAvailable {
			return errors.New("task snapshot recovery detail is invalid")
		}
		for _, eventID := range detail.RelatedEventIDs {
			if eventID.IsZero() {
				return errors.New("task snapshot recovery related event is invalid")
			}
		}
		for _, path := range detail.RelatedFiles {
			if strings.TrimSpace(path) == "" {
				return errors.New("task snapshot recovery related file is invalid")
			}
		}
	}
	if err := validateCommandProjection(projection.PendingCommand); err != nil {
		return err
	}
	return nil
}

func validateCommandProjection(command CommandState) error {
	switch command.Status {
	case CommandIdle:
		if command.Key != "" {
			return errors.New("idle task command retains an idempotency key")
		}
		return nil
	case CommandBusy, CommandCommitted, CommandStale, CommandDenied, CommandDisconnected, CommandFailed:
		if _, err := ParseCommandKey(string(command.Key)); err != nil || command.Action == "" || command.ExpectedRevision == 0 {
			return errors.New("task snapshot command is invalid")
		}
		return nil
	default:
		return errors.New("task snapshot command status is invalid")
	}
}

func (bindings RevisionBindings) complete() bool {
	return bindings.Diff > 0 && bindings.Plan > 0 && bindings.Validation > 0 &&
		bindings.Evidence > 0 && bindings.Graph > 0
}

func inconsistency(code, entity string, expected, observed uint64, from, to string, cause error) error {
	return &ProjectionError{
		Code: code, Entity: entity, Expected: expected, Observed: observed,
		From: from, To: to, Cause: cause,
	}
}

func ensureInconsistency(err error, entity string, expected, observed uint64) error {
	var typed *ProjectionError
	if errors.As(err, &typed) {
		return err
	}
	return inconsistency("event-reducer", entity, expected, observed, "", "", err)
}
