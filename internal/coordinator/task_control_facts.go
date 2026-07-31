package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

type taskControlFactRepository interface {
	GetBudgetSnapshot(
		context.Context,
		domain.TaskID,
	) (storage.BudgetSnapshot, error)
	GetRunPlanBinding(
		context.Context,
		domain.RunID,
	) (storage.RunPlanBinding, error)
	GetCurrentPlanRevision(
		context.Context,
		domain.TaskID,
	) (storage.PlanRevision, error)
	GetPlanRevision(
		context.Context,
		domain.TaskID,
		uint64,
	) (storage.PlanRevision, error)
	ListPlanStepStates(
		context.Context,
		domain.RunID,
	) ([]storage.PlanStepStatus, error)
}

// StoredTaskControlFacts derives between-action budget, policy, and
// validation gates exclusively from immutable run bindings and current
// durable repository facts.
type StoredTaskControlFacts struct {
	repositories taskControlFactRepository
}

func NewStoredTaskControlFacts(
	repositories taskControlFactRepository,
) (*StoredTaskControlFacts, error) {
	if repositories == nil {
		return nil, errors.New(
			"task control fact repository is required",
		)
	}
	return &StoredTaskControlFacts{repositories: repositories}, nil
}

func (facts *StoredTaskControlFacts) ReadTaskControlFacts(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) (bool, bool, bool, error) {
	if facts == nil || taskID.IsZero() || runID.IsZero() {
		return false, false, false, errors.New(
			"task control fact identities are required",
		)
	}
	binding, err := facts.repositories.GetRunPlanBinding(ctx, runID)
	if err != nil {
		return false, false, false, err
	}
	if binding.TaskID != taskID {
		return false, false, false, errors.New(
			"task control run plan belongs to another task",
		)
	}
	budget, err := facts.repositories.GetBudgetSnapshot(ctx, taskID)
	if err != nil {
		return false, false, false, err
	}
	budgetAvailable := budget.BudgetID == binding.BudgetID &&
		budget.LimitRevision == binding.BudgetLimitRevision &&
		!budget.HardCapReached &&
		!budget.ReconciliationPending &&
		!budget.CostAccountingUnknown &&
		!budget.TokenAccountingUnknown

	currentPlan, err := facts.repositories.GetCurrentPlanRevision(ctx, taskID)
	if err != nil {
		return false, false, false, err
	}
	policyCurrent := currentPlan.Revision == binding.PlanRevision &&
		currentPlan.ContentSHA256 == binding.PlanSHA256 &&
		currentPlan.PolicyRevision == binding.PolicyRevision

	boundPlan, err := facts.repositories.GetPlanRevision(
		ctx,
		taskID,
		binding.PlanRevision,
	)
	if err != nil {
		return false, false, false, err
	}
	states, err := facts.repositories.ListPlanStepStates(ctx, runID)
	if err != nil {
		return false, false, false, err
	}
	stateByID := make(map[string]storage.PlanStepState, len(states))
	for _, state := range states {
		stateByID[state.PlanStepID] = state.State
	}
	validationComplete := true
	for _, step := range boundPlan.Plan.Steps {
		if len(step.ValidationCommands) == 0 {
			continue
		}
		if stateByID[step.ID] != storage.PlanStepValidated {
			validationComplete = false
			break
		}
	}
	return budgetAvailable, policyCurrent, validationComplete, nil
}

var _ TaskControlFacts = (*StoredTaskControlFacts)(nil)
