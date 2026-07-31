package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
)

func TestTaskServicePauseMapsRevisionedCommandAndTaskView(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 123_000_000).UTC()
	application := &taskControlApplicationStub{
		pauseView: TaskControlView{
			TaskID:    taskID,
			State:     domain.TaskStatePaused,
			Revision:  9,
			UpdatedAt: now,
		},
	}
	service, err := NewTaskService(application)
	if err != nil {
		t.Fatal(err)
	}
	taskIdentity, err := TaskIDToProto(taskID)
	if err != nil {
		t.Fatal(err)
	}
	revision := uint64(8)
	response, err := service.PauseTask(
		t.Context(),
		&codefluxv1.PauseTaskRequest{
			Control: &codefluxv1.MutationControl{
				IdempotencyKey:   "pause-rpc-1",
				ExpectedRevision: &revision,
			},
			TaskId: taskIdentity,
			Reason: "user requested a pause",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if application.pause.TaskID != taskID ||
		application.pause.ExpectedRevision != revision ||
		application.pause.IdempotencyKey != "pause-rpc-1" ||
		application.pause.ReasonRedacted != "user requested a pause" {
		t.Fatalf("pause command = %#v", application.pause)
	}
	if response.GetTask().GetState() != string(domain.TaskStatePaused) ||
		response.GetTask().GetRevision() != 9 ||
		!response.GetTask().GetUpdatedAt().AsTime().Equal(now) {
		t.Fatalf("pause response = %#v", response.GetTask())
	}
}

func TestTaskServiceRequiresOptimisticRevision(t *testing.T) {
	service, err := NewTaskService(&taskControlApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	taskIdentity, err := TaskIDToProto(taskID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ResumeTask(
		t.Context(),
		&codefluxv1.ResumeTaskRequest{
			Control: &codefluxv1.MutationControl{
				IdempotencyKey: "resume-missing-revision",
			},
			TaskId: taskIdentity,
		},
	)
	var validation *RequestValidationError
	if !errors.As(err, &validation) ||
		validation.Field != "control.expected_revision" {
		t.Fatalf("resume error = %v", err)
	}
}

func TestTaskServiceMapsStaleControlWithoutLeakingInternals(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	taskIdentity, err := TaskIDToProto(taskID)
	if err != nil {
		t.Fatal(err)
	}
	application := &taskControlApplicationStub{
		cancelErr: ErrTaskControlStaleRevision,
	}
	service, err := NewTaskService(application)
	if err != nil {
		t.Fatal(err)
	}
	revision := uint64(4)
	_, err = service.CancelTask(
		t.Context(),
		&codefluxv1.CancelTaskRequest{
			Control: &codefluxv1.MutationControl{
				IdempotencyKey:   "cancel-stale-1",
				ExpectedRevision: &revision,
			},
			TaskId: taskIdentity,
			Reason: "user cancelled",
		},
	)
	var applicationErr *ApplicationError
	if !errors.As(err, &applicationErr) ||
		applicationErr.Code !=
			codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION ||
		applicationErr.EntityID.GetValue() != taskID.String() {
		t.Fatalf("cancel error = %#v", err)
	}
}

type taskControlApplicationStub struct {
	pause      TaskControlCommand
	resume     TaskControlCommand
	cancel     TaskControlCommand
	pauseView  TaskControlView
	resumeView TaskControlView
	cancelView TaskControlView
	pauseErr   error
	resumeErr  error
	cancelErr  error
}

func (stub *taskControlApplicationStub) PauseTaskControl(
	_ context.Context,
	command TaskControlCommand,
) (TaskControlView, error) {
	stub.pause = command
	return stub.pauseView, stub.pauseErr
}

func (stub *taskControlApplicationStub) ResumeTaskControl(
	_ context.Context,
	command TaskControlCommand,
) (TaskControlView, error) {
	stub.resume = command
	return stub.resumeView, stub.resumeErr
}

func (stub *taskControlApplicationStub) CancelTaskControl(
	_ context.Context,
	command TaskControlCommand,
) (TaskControlView, error) {
	stub.cancel = command
	return stub.cancelView, stub.cancelErr
}
