package coordinator

import (
	"context"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// planApprovalSteps are the states a created task passes through on its way to
// being startable.
//
// They are walked here rather than collapsed into one write because each is a
// durable, observable state with its own event: a task that jumped from Draft
// to Ready would leave no record that a forecast was ever made or that anything
// was ever awaiting a decision, and the session timeline reads those events.
var planApprovalSteps = []struct {
	from domain.TaskState
	to   domain.TaskState
}{
	{domain.TaskStateDraft, domain.TaskStateForecasting},
	{domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval},
	{domain.TaskStateAwaitingPlanApproval, domain.TaskStateReady},
}

// ApproveTaskPlan records the decision to run a created task and binds exactly
// what was reviewed.
//
// This is the step that was missing. A created task is Draft: it carries a
// policy, an effort forecast, and a hard budget to look at, and nothing binds
// it to execution. Reaching Ready requires a granted approval and binding the
// preflight requires Ready, and neither was reachable from outside this
// process — so a submitted request became a draft task and stopped, while the
// interface correctly reported that no task was running.
func (adapter *TaskLifecycleAdapter) ApproveTaskPlan(
	ctx context.Context,
	command transport.ApprovePlanCommand,
) (transport.ApprovedPlanView, error) {
	store := adapter.store
	task, err := store.GetTask(ctx, command.TaskID)
	if err != nil {
		return transport.ApprovedPlanView{}, err
	}
	if command.ExpectedRevision != 0 && task.Revision != command.ExpectedRevision {
		// The caller approved a task as it was; if it has moved since, what
		// they saw is not what would start.
		return transport.ApprovedPlanView{}, fmt.Errorf(
			"%w: the task moved to revision %d while it was being reviewed",
			storage.ErrConflict, task.Revision)
	}
	forecast, err := store.GetCurrentTaskForecast(ctx, command.TaskID)
	if err != nil {
		return transport.ApprovedPlanView{}, err
	}

	revision, err := advanceToReady(ctx, store, task, command.IdempotencyKey)
	if err != nil {
		return transport.ApprovedPlanView{}, err
	}
	// The approval is granted the instant advanceToReady's last transition
	// commits, so the pump is signalled here rather than after binding the
	// preflight below: binding can still fail, but the grant itself is
	// already durable and worth a look regardless (AUDIT-018a).
	if adapter.notifyDispatch != nil {
		adapter.notifyDispatch()
	}

	preflight, err := adapter.preflight.BindExecution(
		ctx, command.TaskID, revision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: forecast.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: forecast.ForecastRevision},
		},
		"approve-bind:"+command.IdempotencyKey,
	)
	if err != nil {
		return transport.ApprovedPlanView{}, err
	}
	approved, err := store.GetTask(ctx, command.TaskID)
	if err != nil {
		return transport.ApprovedPlanView{}, err
	}
	return transport.ApprovedPlanView{
		TaskControlView: transport.TaskControlView{
			TaskID:    approved.ID,
			State:     approved.State,
			Revision:  approved.Revision,
			UpdatedAt: approved.UpdatedAt,
		},
		PreflightRevision: preflight.Revision,
	}, nil
}

// advanceToReady walks a task from wherever it is to Ready.
//
// Steps already taken are skipped rather than retried, so approving a task that
// a previous partial attempt left mid-walk completes it instead of failing on a
// transition that has already happened.
func advanceToReady(
	ctx context.Context,
	store taskLifecycleStore,
	task storage.Task,
	idempotencyKey string,
) (uint64, error) {
	revision := task.Revision
	reached := false
	for _, step := range planApprovalSteps {
		if !reached {
			if task.State != step.from {
				continue
			}
			reached = true
		}
		eventID, err := domain.NewEventID()
		if err != nil {
			return 0, err
		}
		approval := domain.ApprovalRequestState("")
		if step.to == domain.TaskStateReady {
			// The grant is recorded on the transition into Ready, because Ready
			// is exactly the state that means "a person said run this".
			approval = domain.ApprovalRequestStateGranted
		}
		transitioned, err := store.TransitionTask(ctx, storage.TransitionTask{
			EventID: eventID, TaskID: task.ID, ExpectedRevision: revision,
			From: step.from, To: step.to, Approval: approval,
			IdempotencyKey: fmt.Sprintf("approve:%s:%s", idempotencyKey, step.to),
		})
		if err != nil {
			return 0, fmt.Errorf("advance %s to %s: %w", step.from, step.to, err)
		}
		revision = transitioned.Task.Revision
	}
	if !reached {
		return 0, fmt.Errorf(
			"%w: a task in %s cannot be approved for execution",
			storage.ErrConflict, task.State)
	}
	return revision, nil
}
