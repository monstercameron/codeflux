package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

type approvedRunPlanStore interface {
	GetPlanRevision(
		context.Context,
		domain.TaskID,
		uint64,
	) (storage.PlanRevision, error)
	BindRunPlan(
		context.Context,
		storage.BindRunPlan,
	) (storage.RunPlanBinding, error)
}

type PlanApprovedCheckpointer interface {
	CapturePlanApprovedCheckpoint(
		context.Context,
		domain.TaskID,
		domain.RunID,
		uint64,
		domain.ApprovalID,
		string,
	) (domain.CheckpointID, error)
}

// ApprovedRunPlanInitializer makes the run/plan binding durable before
// checkpointing the exact granted approval. Agent work must start only after
// this method returns successfully.
type ApprovedRunPlanInitializer struct {
	store       approvedRunPlanStore
	checkpoints PlanApprovedCheckpointer
}

func NewApprovedRunPlanInitializer(
	store approvedRunPlanStore,
	checkpoints PlanApprovedCheckpointer,
) (*ApprovedRunPlanInitializer, error) {
	if store == nil || checkpoints == nil {
		return nil, errors.New(
			"approved run plan store and checkpointer are required",
		)
	}
	return &ApprovedRunPlanInitializer{
		store:       store,
		checkpoints: checkpoints,
	}, nil
}

type InitializeApprovedRunPlan struct {
	TaskID         domain.TaskID
	RunID          domain.RunID
	PlanRevision   uint64
	IdempotencyKey string
}

type InitializedApprovedRunPlan struct {
	Binding      storage.RunPlanBinding
	ApprovalID   domain.ApprovalID
	CheckpointID domain.CheckpointID
}

func (service *ApprovedRunPlanInitializer) Initialize(
	ctx context.Context,
	input InitializeApprovedRunPlan,
) (InitializedApprovedRunPlan, error) {
	if input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.PlanRevision == 0 || input.IdempotencyKey == "" {
		return InitializedApprovedRunPlan{}, errors.New(
			"approved run plan input is invalid",
		)
	}
	plan, err := service.store.GetPlanRevision(
		ctx,
		input.TaskID,
		input.PlanRevision,
	)
	if err != nil {
		return InitializedApprovedRunPlan{}, err
	}
	if plan.ApprovalRequired &&
		(plan.ApprovalID == nil || plan.ApprovalID.IsZero()) {
		return InitializedApprovedRunPlan{}, errors.New(
			"current plan lacks granted approval identity",
		)
	}
	if plan.ApprovalID == nil || plan.ApprovalID.IsZero() {
		return InitializedApprovedRunPlan{}, errors.New(
			"plan-approved checkpoint requires approval identity",
		)
	}
	binding, err := service.store.BindRunPlan(
		ctx,
		storage.BindRunPlan{
			TaskID:         input.TaskID,
			RunID:          input.RunID,
			PlanRevision:   input.PlanRevision,
			IdempotencyKey: input.IdempotencyKey + "/binding",
		},
	)
	if err != nil {
		return InitializedApprovedRunPlan{}, err
	}
	checkpointID, err := service.checkpoints.CapturePlanApprovedCheckpoint(
		ctx,
		input.TaskID,
		input.RunID,
		input.PlanRevision,
		*plan.ApprovalID,
		input.IdempotencyKey+"/checkpoint",
	)
	if err != nil {
		return InitializedApprovedRunPlan{
			Binding:    binding,
			ApprovalID: *plan.ApprovalID,
		}, err
	}
	return InitializedApprovedRunPlan{
		Binding:      binding,
		ApprovalID:   *plan.ApprovalID,
		CheckpointID: checkpointID,
	}, nil
}

var _ PlanApprovedCheckpointer = (*LaneACheckpointAdapter)(nil)
