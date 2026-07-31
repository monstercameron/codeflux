package transport

import (
	"context"
	"errors"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TaskControlCommand is the transport-independent command accepted by the
// pause/resume/cancel application boundary.
type TaskControlCommand struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	IdempotencyKey   string
	ReasonRedacted   string
}

// TaskControlView is the minimum honest task projection returned by control
// mutations.
type TaskControlView struct {
	TaskID    domain.TaskID
	State     domain.TaskState
	Revision  uint64
	UpdatedAt time.Time
}

// TaskControlApplication owns the durable control use cases behind the thin
// gRPC handler.
type TaskControlApplication interface {
	PauseTaskControl(
		context.Context,
		TaskControlCommand,
	) (TaskControlView, error)
	ResumeTaskControl(
		context.Context,
		TaskControlCommand,
	) (TaskControlView, error)
	CancelTaskControl(
		context.Context,
		TaskControlCommand,
	) (TaskControlView, error)
}

// TaskService implements the existing TaskService pause/resume/cancel RPCs.
// Other TaskService methods intentionally retain Unimplemented responses until
// their owning milestones provide application services.
type TaskService struct {
	codefluxv1.UnimplementedTaskServiceServer
	controls TaskControlApplication
}

func NewTaskService(controls TaskControlApplication) (*TaskService, error) {
	if controls == nil {
		return nil, errors.New("task control application is required")
	}
	return &TaskService{controls: controls}, nil
}

func (service *TaskService) PauseTask(
	ctx context.Context,
	request *codefluxv1.PauseTaskRequest,
) (*codefluxv1.PauseTaskResponse, error) {
	command, err := taskControlCommand(
		request.GetControl(),
		request.GetTaskId(),
		request.GetReason(),
		true,
	)
	if err != nil {
		return nil, err
	}
	view, err := service.controls.PauseTaskControl(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	task, err := taskControlViewToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.PauseTaskResponse{Task: task}, nil
}

func (service *TaskService) ResumeTask(
	ctx context.Context,
	request *codefluxv1.ResumeTaskRequest,
) (*codefluxv1.ResumeTaskResponse, error) {
	command, err := taskControlCommand(
		request.GetControl(),
		request.GetTaskId(),
		"compatibility-checked task resume",
		true,
	)
	if err != nil {
		return nil, err
	}
	view, err := service.controls.ResumeTaskControl(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	task, err := taskControlViewToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.ResumeTaskResponse{Task: task}, nil
}

func (service *TaskService) CancelTask(
	ctx context.Context,
	request *codefluxv1.CancelTaskRequest,
) (*codefluxv1.CancelTaskResponse, error) {
	command, err := taskControlCommand(
		request.GetControl(),
		request.GetTaskId(),
		request.GetReason(),
		true,
	)
	if err != nil {
		return nil, err
	}
	view, err := service.controls.CancelTaskControl(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	task, err := taskControlViewToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.CancelTaskResponse{Task: task}, nil
}

func taskControlCommand(
	control *codefluxv1.MutationControl,
	taskIdentity *codefluxv1.StableIdentity,
	reason string,
	requireReason bool,
) (TaskControlCommand, error) {
	if control == nil || control.ExpectedRevision == nil {
		return TaskControlCommand{}, &RequestValidationError{
			Field:  "control.expected_revision",
			Reason: "is required",
		}
	}
	taskID, err := TaskIDFromProto(taskIdentity)
	if err != nil {
		return TaskControlCommand{}, err
	}
	reason = strings.TrimSpace(reason)
	if requireReason && reason == "" {
		return TaskControlCommand{}, &RequestValidationError{
			Field:  "reason",
			Reason: "is required",
		}
	}
	if len(reason) > 2048 {
		return TaskControlCommand{}, &RequestValidationError{
			Field:  "reason",
			Reason: "is too long",
		}
	}
	return TaskControlCommand{
		TaskID:           taskID,
		ExpectedRevision: control.GetExpectedRevision(),
		IdempotencyKey:   control.GetIdempotencyKey(),
		ReasonRedacted:   reason,
	}, nil
}

func taskControlViewToProto(
	view TaskControlView,
) (*codefluxv1.TaskView, error) {
	taskID, err := TaskIDToProto(view.TaskID)
	if err != nil {
		return nil, err
	}
	updated := timestamppb.New(view.UpdatedAt.UTC())
	if err := updated.CheckValid(); err != nil {
		return nil, err
	}
	return &codefluxv1.TaskView{
		TaskId:    taskID,
		State:     string(view.State),
		Revision:  view.Revision,
		UpdatedAt: updated,
	}, nil
}

func mapTaskControlError(
	err error,
	taskID domain.TaskID,
) error {
	entity, _ := TaskIDToProto(taskID)
	switch {
	case errors.Is(err, storage.ErrStaleRevision):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION,
			SafeMessage: "The task changed before this control request.",
			EntityID:    entity,
		}
	case errors.Is(err, storage.ErrNotFound):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_NOT_FOUND,
			SafeMessage: "The task could not be found.",
			EntityID:    entity,
		}
	case errors.Is(err, storage.ErrConflict):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_INVALID_TRANSITION,
			SafeMessage: "The task cannot accept that control in its current state.",
			EntityID:    entity,
		}
	default:
		return err
	}
}

var _ codefluxv1.TaskServiceServer = (*TaskService)(nil)
