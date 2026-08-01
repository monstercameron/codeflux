package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TaskLifecycleAdapter implements transport.TaskLifecycleApplication over
// TaskPreflightService, closing the last link between a submitted
// requirement and the agent loop.
//
// It is deliberately thin. Everything it does is translate a transport
// command into a TaskIntakeRequest and translate the result back; no
// lifecycle judgement lives here.
type TaskLifecycleAdapter struct {
	preflight *TaskPreflightService
	store     taskLifecycleStore
}

// taskLifecycleStore is the narrow read/write surface starting a task needs
// beyond the preflight service itself.
type taskLifecycleStore interface {
	GetTask(context.Context, domain.TaskID) (storage.Task, error)
	GetTaskExecutionPreflight(context.Context, domain.TaskID, uint64) (storage.ExecutionPreflight, error)
}

// NewTaskLifecycleAdapter builds the adapter.
func NewTaskLifecycleAdapter(
	preflight *TaskPreflightService,
	store taskLifecycleStore,
) (*TaskLifecycleAdapter, error) {
	if preflight == nil {
		return nil, errors.New("task preflight service must not be nil")
	}
	if store == nil {
		return nil, errors.New("task lifecycle store must not be nil")
	}
	return &TaskLifecycleAdapter{preflight: preflight, store: store}, nil
}

// CreateTaskFromRequirement turns a submitted requirement into a forecasted
// task and returns the immutable policy, forecast, and budget to present for
// approval.
func (adapter *TaskLifecycleAdapter) CreateTaskFromRequirement(
	ctx context.Context,
	command transport.CreateTaskCommand,
) (transport.CreatedTaskView, error) {
	result, err := adapter.preflight.IntakeTask(ctx, TaskIntakeRequest{
		ThreadID:                 command.ThreadID,
		RequestMessageID:         command.RequestMessageID,
		Requirement:              command.Requirement,
		TaskClass:                fingerprint.TaskClass(command.TaskClass),
		RepositoryRevision:       command.RepositoryRevision,
		BaselineModelRevision:    command.BaselineModelRevision,
		ToolConfigurationVersion: command.ToolConfigurationVersion,
		ValidationProfileVersion: command.ValidationProfileVersion,
		AffectedPaths:            command.AffectedPaths,
		AffectedPackages:         command.AffectedPackages,
		AffectedSymbols:          command.AffectedSymbols,
		IdempotencyKey:           command.IdempotencyKey,
	})
	if err != nil {
		return transport.CreatedTaskView{}, err
	}
	return transport.CreatedTaskView{
		TaskControlView: transport.TaskControlView{
			TaskID:    result.Task.ID,
			State:     result.Task.State,
			Revision:  result.Task.Revision,
			UpdatedAt: result.Task.UpdatedAt,
		},
		PolicyRevision:   result.Forecasted.Policy.Revision,
		ForecastRevision: result.Forecasted.Forecast.Revision,
		BudgetID:         result.Budget.BudgetID,
	}, nil
}

// StartPreparedTask begins the exact preflight that was prepared and
// approved for this task.
//
// The caller names the exact preflight revision it approved, and
// StartPreparedTaskRun validates that binding against the task's current
// state. A stale or superseded binding is rejected rather than silently
// upgraded, so what starts is exactly what was reviewed.
func (adapter *TaskLifecycleAdapter) StartPreparedTask(
	ctx context.Context,
	command transport.StartTaskCommand,
) (transport.TaskControlView, error) {
	preflight, err := adapter.store.GetTaskExecutionPreflight(ctx, command.TaskID, command.PreflightRevision)
	if err != nil {
		return transport.TaskControlView{}, err
	}
	runID, err := domain.NewRunID()
	if err != nil {
		return transport.TaskControlView{}, err
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		return transport.TaskControlView{}, err
	}
	if _, err := adapter.preflight.Start(ctx, storage.StartPreparedTaskRun{
		RunID:                runID,
		EventID:              eventID,
		TaskID:               command.TaskID,
		PreflightRevision:    preflight.Revision,
		ExpectedTaskRevision: command.ExpectedRevision,
		Attempt:              1,
		IdempotencyKey:       "start:" + command.IdempotencyKey,
		EventIdempotencyKey:  "start-event:" + command.IdempotencyKey,
	}); err != nil {
		return transport.TaskControlView{}, err
	}
	started, err := adapter.store.GetTask(ctx, command.TaskID)
	if err != nil {
		return transport.TaskControlView{}, err
	}
	return transport.TaskControlView{
		TaskID:    started.ID,
		State:     started.State,
		Revision:  started.Revision,
		UpdatedAt: started.UpdatedAt,
	}, nil
}
