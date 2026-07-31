package coordinator

import (
	"context"
	"errors"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// CaptureGracefulShutdownCheckpoint commits a bounded checkpoint before its
// identity is sent to the worker.
func (adapter *LaneACheckpointAdapter) CaptureGracefulShutdownCheckpoint(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	idempotencyKey string,
	timeout time.Duration,
) (domain.CheckpointID, error) {
	if timeout <= 0 || timeout > time.Minute {
		return domain.CheckpointID{}, errors.New(
			"graceful checkpoint timeout must be within one minute",
		)
	}
	planRevision, err := adapter.Plans.CurrentCheckpointPlanRevision(
		ctx,
		taskID,
		runID,
	)
	if err != nil {
		return domain.CheckpointID{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return adapter.capture(
		bounded,
		taskID,
		runID,
		planRevision,
		checkpoint.TriggerGracefulShutdown,
		checkpoint.TriggerAttribution{},
		idempotencyKey,
	)
}

// CheckpointCapture is the Lane A checkpoint service surface consumed by
// control/agent trigger adapters.
type CheckpointCapture interface {
	Capture(
		context.Context,
		checkpoint.CaptureCommand,
	) (checkpoint.CaptureResult, error)
}

// CheckpointPlanRevisionSource resolves the current approved plan for
// triggers, such as pause, whose command does not already carry the binding.
type CheckpointPlanRevisionSource interface {
	CurrentCheckpointPlanRevision(
		context.Context,
		domain.TaskID,
		domain.RunID,
	) (uint64, error)
}

// LaneACheckpointAdapter binds trigger-specific Lane B call sites to the
// general Lane A capture service without widening the coordinator ports.
type LaneACheckpointAdapter struct {
	CaptureService  CheckpointCapture
	Plans           CheckpointPlanRevisionSource
	NewCheckpointID func() (domain.CheckpointID, error)
}

// NewLaneACheckpointAdapter validates production checkpoint trigger wiring.
func NewLaneACheckpointAdapter(
	capture CheckpointCapture,
	plans CheckpointPlanRevisionSource,
) (*LaneACheckpointAdapter, error) {
	if capture == nil || plans == nil {
		return nil, errors.New(
			"checkpoint capture and plan revision source are required",
		)
	}
	return &LaneACheckpointAdapter{
		CaptureService:  capture,
		Plans:           plans,
		NewCheckpointID: domain.NewCheckpointID,
	}, nil
}

// CapturePauseCheckpoint implements the user-pause checkpoint boundary.
func (adapter *LaneACheckpointAdapter) CapturePauseCheckpoint(
	ctx context.Context,
	control storage.TaskControlSnapshot,
	idempotencyKey string,
) (domain.CheckpointID, error) {
	planRevision, err := adapter.Plans.CurrentCheckpointPlanRevision(
		ctx,
		control.TaskID,
		control.RunID,
	)
	if err != nil {
		return domain.CheckpointID{}, err
	}
	return adapter.capture(
		ctx,
		control.TaskID,
		control.RunID,
		planRevision,
		checkpoint.TriggerUserPaused,
		checkpoint.TriggerAttribution{},
		idempotencyKey,
	)
}

// CreateCheckpoint adapts material-edit and before-risky-action triggers from
// the M14 agent loop.
func (adapter *LaneACheckpointAdapter) CreateCheckpoint(
	ctx context.Context,
	request agentloop.CheckpointRequest,
) error {
	trigger := checkpoint.Trigger("")
	switch request.Trigger {
	case agentloop.CheckpointMaterialEdit:
		trigger = checkpoint.TriggerMaterialEditApplied
	case agentloop.CheckpointBeforeRisky:
		trigger = checkpoint.TriggerBeforeRiskyAction
	default:
		return errors.New("agent checkpoint trigger is unsupported")
	}
	_, err := adapter.capture(
		ctx,
		request.TaskID,
		request.RunID,
		request.PlanRevision,
		trigger,
		checkpoint.TriggerAttribution{
			ToolRequestID:        request.ToolRequestID,
			PermissionDecisionID: request.PermissionID,
			ActionSHA256:         request.ActionSHA256,
		},
		checkpointIdempotencyKey(request),
	)
	return err
}

// CreateSuccessfulValidationCheckpoint implements the post-validation
// boundary used by RepairCompletionService.
func (adapter *LaneACheckpointAdapter) CreateSuccessfulValidationCheckpoint(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	planRevision uint64,
	validationID domain.ValidationID,
	idempotencyKey string,
) (domain.CheckpointID, error) {
	return adapter.capture(
		ctx,
		taskID,
		runID,
		planRevision,
		checkpoint.TriggerValidationSucceeded,
		checkpoint.TriggerAttribution{ValidationID: &validationID},
		idempotencyKey,
	)
}

// CapturePlanApprovedCheckpoint is the explicit plan-approval trigger for
// application services that own the approval transaction.
func (adapter *LaneACheckpointAdapter) CapturePlanApprovedCheckpoint(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	planRevision uint64,
	approvalID domain.ApprovalID,
	idempotencyKey string,
) (domain.CheckpointID, error) {
	return adapter.capture(
		ctx,
		taskID,
		runID,
		planRevision,
		checkpoint.TriggerPlanApproved,
		checkpoint.TriggerAttribution{ApprovalID: &approvalID},
		idempotencyKey,
	)
}

// CreatePlanApprovedCheckpoint is invoked by the execution loop after its
// first durable control check and before the first model action.
func (adapter *LaneACheckpointAdapter) CreatePlanApprovedCheckpoint(
	ctx context.Context,
	request agentloop.PlanApprovedCheckpointRequest,
) error {
	_, err := adapter.CapturePlanApprovedCheckpoint(
		ctx,
		request.TaskID,
		request.RunID,
		request.PlanRevision,
		request.ApprovalID,
		"agent/"+request.RunID.String()+"/"+
			request.ApprovalID.String()+"/plan-approved",
	)
	return err
}

func (adapter *LaneACheckpointAdapter) capture(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	planRevision uint64,
	trigger checkpoint.Trigger,
	attribution checkpoint.TriggerAttribution,
	idempotencyKey string,
) (domain.CheckpointID, error) {
	if adapter == nil || adapter.CaptureService == nil ||
		adapter.NewCheckpointID == nil {
		return domain.CheckpointID{}, errors.New(
			"checkpoint adapter is unavailable",
		)
	}
	checkpointID, err := adapter.NewCheckpointID()
	if err != nil {
		return domain.CheckpointID{}, err
	}
	result, err := adapter.CaptureService.Capture(
		ctx,
		checkpoint.CaptureCommand{
			CheckpointID:         checkpointID,
			TaskID:               taskID,
			RunID:                runID,
			ExpectedPlanRevision: planRevision,
			Trigger:              trigger,
			Attribution:          attribution,
			IdempotencyKey:       idempotencyKey,
		},
	)
	if err != nil {
		return domain.CheckpointID{}, err
	}
	return result.Checkpoint.ID, nil
}

func checkpointIdempotencyKey(
	request agentloop.CheckpointRequest,
) string {
	return "agent/" + request.RunID.String() + "/" +
		request.ToolRequestID + "/" + string(request.Trigger)
}

var (
	_ TaskControlCheckpointer               = (*LaneACheckpointAdapter)(nil)
	_ agentloop.CheckpointStore             = (*LaneACheckpointAdapter)(nil)
	_ agentloop.PlanApprovedCheckpointStore = (*LaneACheckpointAdapter)(nil)
	_ SuccessfulValidationCheckpointCreator = (*LaneACheckpointAdapter)(nil)
)
