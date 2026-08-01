// Package transport: the CreateTask and StartTask RPCs.
//
// These two were declared in api/proto/codeflux/v1/product_api.proto from the
// beginning and returned Unimplemented, with a note that they awaited "their
// owning milestone". No milestone ever claimed them, so nothing could turn a
// submitted requirement into running work: the composer could send a message,
// but a message is not a task.
//
// The handlers stay thin, as AGENTS.md requires — authentication, validation,
// conversion, application delegation, safe error mapping. All lifecycle
// judgement lives in coordinator.TaskPreflightService.
package transport

import (
	"context"
	"errors"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
)

// CreateTaskCommand carries one submitted requirement.
//
// Everything that shapes the task — its class, the base revision, the
// toolchain, the model being measured against — is DECLARED here rather than
// inferred, because no classifier exists and a guess would land inside the
// fingerprint that gates memory retrieval and routing. See
// coordinator.TaskIntakeRequest for the full reasoning.
type CreateTaskCommand struct {
	ThreadID                 domain.ThreadID
	RequestMessageID         *domain.MessageID
	Requirement              string
	TaskClass                string
	RepositoryRevision       string
	BaselineModelRevision    string
	ToolConfigurationVersion string
	ValidationProfileVersion string
	AffectedPaths            []string
	AffectedPackages         []string
	AffectedSymbols          []string
	IdempotencyKey           string
}

// CreatedTaskView is the created task plus the immutable policy, forecast,
// and budget revisions to present for approval. It is not an execution
// binding: a task reaches Ready only through a granted approval.
type CreatedTaskView struct {
	TaskControlView
	PolicyRevision   uint64
	ForecastRevision uint64
	BudgetID         domain.BudgetID
}

// StartTaskCommand starts one already-approved, already-prepared task.
type StartTaskCommand struct {
	TaskID               domain.TaskID
	ExpectedRevision     uint64
	ApprovedPlanRevision uint64
	// PreflightRevision names the exact reviewed policy/forecast/budget
	// binding to start. It is required: without it "start what was approved"
	// would be advisory rather than enforced.
	PreflightRevision uint64
	IdempotencyKey    string
}

// TaskLifecycleApplication is the application surface the two lifecycle RPCs
// delegate to.
type TaskLifecycleApplication interface {
	CreateTaskFromRequirement(context.Context, CreateTaskCommand) (CreatedTaskView, error)
	ApproveTaskPlan(context.Context, ApprovePlanCommand) (ApprovedPlanView, error)
	StartPreparedTask(context.Context, StartTaskCommand) (TaskControlView, error)
}

// ApprovePlanCommand records one person's decision to run a created task.
type ApprovePlanCommand struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	IdempotencyKey   string
}

// ApprovedPlanView is the approved task and the binding the approval produced.
//
// The preflight revision is returned rather than looked up again because it
// names the exact policy, forecast, and budget the person saw. Re-reading it at
// start time would let a settings change between the two silently substitute a
// different binding for the one that was approved.
type ApprovedPlanView struct {
	TaskControlView
	PreflightRevision uint64
}

// ConfigureTaskLifecycle installs the lifecycle application. Until it is
// installed both RPCs report Unavailable rather than pretending to work.
func (service *TaskService) ConfigureTaskLifecycle(lifecycle TaskLifecycleApplication) {
	service.lifecycle = lifecycle
}

// CreateTask turns a submitted requirement into a forecasted task.
func (service *TaskService) CreateTask(
	ctx context.Context,
	request *codefluxv1.CreateTaskRequest,
) (*codefluxv1.CreateTaskResponse, error) {
	if service.lifecycle == nil {
		return nil, unavailableLifecycleError()
	}
	control := request.GetControl()
	if control == nil {
		return nil, &RequestValidationError{Field: "control", Reason: "is required"}
	}
	idempotencyKey := control.GetIdempotencyKey()
	threadID, err := ThreadIDFromProto(request.GetThreadId())
	if err != nil {
		return nil, err
	}
	command := CreateTaskCommand{
		ThreadID:                 threadID,
		Requirement:              request.GetRequirement(),
		TaskClass:                request.GetTaskClass(),
		RepositoryRevision:       request.GetRepositoryRevision(),
		BaselineModelRevision:    request.GetBaselineModelRevision(),
		ToolConfigurationVersion: request.GetToolConfigurationVersion(),
		ValidationProfileVersion: request.GetValidationProfileVersion(),
		AffectedPaths:            request.GetAffectedPaths(),
		AffectedPackages:         request.GetAffectedPackages(),
		AffectedSymbols:          request.GetAffectedSymbols(),
		IdempotencyKey:           idempotencyKey,
	}
	if identity := request.GetSourceMessageId(); identity != nil && identity.GetValue() != "" {
		parsed, err := MessageIDFromProto(identity)
		if err != nil {
			return nil, err
		}
		command.RequestMessageID = &parsed
	}
	view, err := service.lifecycle.CreateTaskFromRequirement(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, view.TaskID)
	}
	task, err := taskControlViewToProto(view.TaskControlView)
	if err != nil {
		return nil, err
	}
	if err := service.notifyProjectionInvalidated(
		ctx, view.TaskID, "task", view.Revision, idempotencyKey,
	); err != nil {
		return nil, err
	}
	return &codefluxv1.CreateTaskResponse{Task: task}, nil
}

// ApprovePlan records the decision to run a created task and binds what was
// reviewed.
//
// A created task is Draft: it carries a policy, an effort forecast, and a hard
// budget to look at, and nothing binds it to execution. Reaching Ready needs a
// granted approval and binding the preflight needs Ready, and neither step was
// reachable from any client — so a request became a draft task and stopped
// there, with the interface correctly reporting that no task was running.
func (service *TaskService) ApprovePlan(
	ctx context.Context,
	request *codefluxv1.ApprovePlanRequest,
) (*codefluxv1.ApprovePlanResponse, error) {
	if service.lifecycle == nil {
		return nil, unavailableLifecycleError()
	}
	command, err := taskControlCommand(
		request.GetControl(),
		request.GetTaskId(),
		"task plan approval",
		true,
	)
	if err != nil {
		return nil, err
	}
	view, err := service.lifecycle.ApproveTaskPlan(ctx, ApprovePlanCommand{
		TaskID:           command.TaskID,
		ExpectedRevision: command.ExpectedRevision,
		IdempotencyKey:   command.IdempotencyKey,
	})
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	task, err := taskControlViewToProto(view.TaskControlView)
	if err != nil {
		return nil, err
	}
	if err := service.notifyProjectionInvalidated(
		ctx, command.TaskID, "task", view.Revision, command.IdempotencyKey,
	); err != nil {
		return nil, err
	}
	return &codefluxv1.ApprovePlanResponse{
		Task: task, PreflightRevision: view.PreflightRevision,
	}, nil
}

// StartTask begins an approved, prepared task.
func (service *TaskService) StartTask(
	ctx context.Context,
	request *codefluxv1.StartTaskRequest,
) (*codefluxv1.StartTaskResponse, error) {
	if service.lifecycle == nil {
		return nil, unavailableLifecycleError()
	}
	command, err := taskControlCommand(
		request.GetControl(),
		request.GetTaskId(),
		"approved task start",
		true,
	)
	if err != nil {
		return nil, err
	}
	if request.GetPreflightRevision() == 0 {
		return nil, &RequestValidationError{Field: "preflight_revision", Reason: "is required"}
	}
	view, err := service.lifecycle.StartPreparedTask(ctx, StartTaskCommand{
		TaskID:               command.TaskID,
		ExpectedRevision:     command.ExpectedRevision,
		ApprovedPlanRevision: request.GetApprovedPlanRevision(),
		PreflightRevision:    request.GetPreflightRevision(),
		IdempotencyKey:       command.IdempotencyKey,
	})
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	task, err := taskControlViewToProto(view)
	if err != nil {
		return nil, err
	}
	if err := service.notifyProjectionInvalidated(
		ctx, command.TaskID, "task", view.Revision, command.IdempotencyKey,
	); err != nil {
		return nil, err
	}
	return &codefluxv1.StartTaskResponse{Task: task}, nil
}

// unavailableLifecycleError reports an uninstalled lifecycle application
// honestly rather than returning a misleading success or a bare panic.
func unavailableLifecycleError() error {
	return mapTaskControlError(
		errors.New("task lifecycle application is not configured"),
		domain.TaskID{},
	)
}
