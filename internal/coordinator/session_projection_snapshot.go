package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

type sessionProjectionSnapshotStore interface {
	ReadSessionProjectionSnapshot(context.Context, domain.SessionID) (storage.SessionProjectionSnapshot, error)
	ReconcileSessionProjectionInvalidations(
		context.Context,
		domain.SessionID,
		storage.CommittedEventPublisher,
	) error
}

// SessionProjectionSnapshotService maps one atomic SQLite snapshot into the
// transport-neutral correctness base consumed by the frontend reducer.
type SessionProjectionSnapshotService struct {
	store     sessionProjectionSnapshotStore
	publisher storage.CommittedEventPublisher
}

func NewSessionProjectionSnapshotService(
	store sessionProjectionSnapshotStore,
	publisher storage.CommittedEventPublisher,
) (*SessionProjectionSnapshotService, error) {
	if store == nil || publisher == nil {
		return nil, errors.New("session projection snapshot dependencies are required")
	}
	return &SessionProjectionSnapshotService{store: store, publisher: publisher}, nil
}

func (service *SessionProjectionSnapshotService) GetSessionProjectionSnapshot(
	ctx context.Context,
	sessionID domain.SessionID,
) (transport.SessionProjectionSnapshotView, error) {
	if err := service.store.ReconcileSessionProjectionInvalidations(ctx, sessionID, service.publisher); err != nil {
		return transport.SessionProjectionSnapshotView{}, err
	}
	snapshot, err := service.store.ReadSessionProjectionSnapshot(ctx, sessionID)
	if err != nil {
		return transport.SessionProjectionSnapshotView{}, err
	}
	view := transport.SessionProjectionSnapshotView{
		SessionID: snapshot.SessionID, ThreadID: snapshot.ThreadID,
		ThroughSequence: snapshot.ThroughSequence, ObservedAt: snapshot.ObservedAt,
		Plan: snapshot.Plan, PlanApproval: snapshot.PlanApproval,
		Tool: snapshot.Tool, ToolRevision: snapshot.ToolRevision,
		Validation: snapshot.Validation, ValidationRevision: snapshot.ValidationRev,
		Checkpoint: snapshot.Checkpoint, CheckpointRevision: snapshot.CheckpointRev,
		CheckpointAt: snapshot.CheckpointAt, Recovery: snapshot.Recovery,
		RecoveryRevision: snapshot.RecoveryRev, Acceptance: snapshot.Acceptance,
		AcceptanceRevision: snapshot.AcceptanceRev,
		ReviewBindings:     snapshot.ReviewBindings, ReviewRevision: snapshot.ReviewRev,
		GraphRevision: snapshot.GraphRevision,
	}
	if snapshot.Task == nil {
		return view, nil
	}
	taskID := snapshot.Task.ID
	view.TaskID, view.TaskState, view.TaskRevision = &taskID, snapshot.Task.State, snapshot.Task.Revision
	if snapshot.PendingApproval != nil {
		value := snapshot.PendingApproval
		view.PendingApproval = &events.Approval{
			ApprovalID: value.ID, State: value.State, Scope: value.Scope,
			RedactedReason: value.RequestReason,
		}
		view.ApprovalRevision = value.Revision
	}
	if snapshot.Budget != nil {
		hard, hardErr := integralTaskMoney(snapshot.Budget.HardCost)
		reserved, reservedErr := integralTaskMoney(snapshot.Budget.ReservedCost)
		actual, actualErr := integralTaskMoney(snapshot.Budget.ActualKnownCost)
		if err := errors.Join(hardErr, reservedErr, actualErr); err != nil {
			return transport.SessionProjectionSnapshotView{}, err
		}
		if hard != nil && reserved != nil && actual != nil {
			view.Budget = &events.Budget{HardLimit: *hard, Reserved: *reserved, Actual: *actual}
			view.BudgetRevision = snapshot.Budget.Revision
		}
	}
	return view, nil
}

var _ interface {
	GetSessionProjectionSnapshot(context.Context, domain.SessionID) (transport.SessionProjectionSnapshotView, error)
} = (*SessionProjectionSnapshotService)(nil)
