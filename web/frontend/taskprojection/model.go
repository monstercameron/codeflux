// Package taskprojection owns the pure frontend projection of one live task.
// It has no transport, persistence, browser, or rendering authority.
package taskprojection

import (
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

type PlanProjection struct {
	Present         bool
	Revision        uint64
	RedactedSummary string
	Approval        domain.ApprovalRequestState
	PriorRevisions  []uint64
}

type ToolProjection struct {
	Present     bool
	ExecutionID string
	CommandName string
	State       domain.CommandExecutionState
	Revision    uint64
	SafeSummary string
}

func (projection ToolProjection) Active() bool {
	return projection.Present && !projection.State.IsTerminal()
}

type ApprovalProjection struct {
	Present    bool
	ID         domain.ApprovalID
	State      domain.ApprovalRequestState
	Scope      string
	SafeReason string
	Revision   uint64
}

func (projection ApprovalProjection) Pending() bool {
	return projection.Present && projection.State == domain.ApprovalRequestStatePending
}

type CheckpointProjection struct {
	Present      bool
	ID           domain.CheckpointID
	TaskRevision uint64
	PlanStep     string
	CreatedAt    time.Time
	Revision     uint64
}

type ValidationProjection struct {
	Present      bool
	ID           domain.ValidationID
	State        domain.ValidationState
	Required     bool
	Acknowledged bool
	SafeSummary  string
	Revision     uint64
	DiffRevision uint64
}

type RevisionBindings struct {
	Diff       uint64
	Plan       uint64
	Validation uint64
	Evidence   uint64
	Graph      uint64
}

type ReviewProjection struct {
	Present  bool
	Revision uint64
	Bindings RevisionBindings
}

type AcceptanceProjection struct {
	Present  bool
	State    domain.ChangeAcceptanceState
	Revision uint64
	Bindings RevisionBindings
}

type BudgetProjection struct {
	Present   bool
	Revision  uint64
	HardLimit domain.Money
	Reserved  domain.Money
	Actual    domain.Money
}

type GraphProjection struct {
	Present  bool
	Revision uint64
}

type RecoveryClassification string

const (
	RecoveryNone             RecoveryClassification = "none"
	RecoverySafeResume       RecoveryClassification = "safe-resume"
	RecoveryNeedsReconcile   RecoveryClassification = "needs-reconcile"
	RecoveryPreserveOnly     RecoveryClassification = "preserve-only"
	RecoveryAmbiguousOutcome RecoveryClassification = "ambiguous-outcome"
)

func (classification RecoveryClassification) IsValid() bool {
	switch classification {
	case RecoveryNone, RecoverySafeResume, RecoveryNeedsReconcile,
		RecoveryPreserveOnly, RecoveryAmbiguousOutcome:
		return true
	default:
		return false
	}
}

type RecoveryProjection struct {
	Present                  bool
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

type ActionPolicy struct {
	Denied     []ActionKind
	SafeReason string
}

func (policy ActionPolicy) Denies(action ActionKind) bool {
	for _, denied := range policy.Denied {
		if denied == action {
			return true
		}
	}
	return false
}

type TaskProjection struct {
	TaskID         domain.TaskID
	State          domain.TaskState
	Revision       uint64
	LastSequence   uint64
	Plan           PlanProjection
	Tool           ToolProjection
	Approval       ApprovalProjection
	Checkpoint     CheckpointProjection
	Validation     ValidationProjection
	Review         ReviewProjection
	Acceptance     AcceptanceProjection
	Budget         BudgetProjection
	Graph          GraphProjection
	Recovery       RecoveryClassification
	RecoveryDetail RecoveryProjection
	Policy         ActionPolicy
	PendingCommand CommandState
}

type Snapshot struct {
	Projection TaskProjection
}

func ApplySnapshot(snapshot Snapshot) (TaskProjection, error) {
	projection := cloneProjection(snapshot.Projection)
	if projection.Recovery == "" {
		projection.Recovery = RecoveryNone
	}
	if projection.PendingCommand.Status == "" {
		projection.PendingCommand.Status = CommandIdle
	}
	if err := validateProjection(projection); err != nil {
		return TaskProjection{}, err
	}
	return projection, nil
}

func cloneProjection(projection TaskProjection) TaskProjection {
	projection.Plan.PriorRevisions = append([]uint64(nil), projection.Plan.PriorRevisions...)
	projection.Policy.Denied = append([]ActionKind(nil), projection.Policy.Denied...)
	if projection.RecoveryDetail.CheckpointID != nil {
		value := *projection.RecoveryDetail.CheckpointID
		projection.RecoveryDetail.CheckpointID = &value
	}
	projection.RecoveryDetail.RelatedEventIDs = append([]domain.EventID(nil), projection.RecoveryDetail.RelatedEventIDs...)
	projection.RecoveryDetail.RelatedFiles = append([]string(nil), projection.RecoveryDetail.RelatedFiles...)
	return projection
}
