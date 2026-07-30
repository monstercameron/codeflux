package policy

import (
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// BudgetAdjustment is the attributable before-approval replacement record.
type BudgetAdjustment struct {
	Previous           domain.TaskBudget `json:"previous"`
	Adjusted           domain.TaskBudget `json:"adjusted"`
	Actor              string            `json:"actor"`
	AuthorityReference string            `json:"authority_reference"`
	Reason             string            `json:"reason"`
}

// AdjustBudgetBeforeApproval validates and records a user budget adjustment.
// Once a task is ready or running, the separate approved limit-raise workflow
// must be used.
func AdjustBudgetBeforeApproval(
	state domain.TaskState,
	current domain.TaskBudget,
	requested domain.TaskBudget,
	actor string,
	authorityReference string,
	reason string,
) (BudgetAdjustment, error) {
	switch state {
	case domain.TaskStateDraft,
		domain.TaskStateForecasting,
		domain.TaskStateAwaitingPlanApproval:
	default:
		return BudgetAdjustment{}, ErrBudgetAdjustmentTooLate
	}
	if err := current.Validate(); err != nil {
		return BudgetAdjustment{}, fmt.Errorf("%w: current budget: %v", ErrInvalidPolicy, err)
	}
	if err := requested.Validate(); err != nil {
		return BudgetAdjustment{}, fmt.Errorf("%w: requested budget: %v", ErrInvalidPolicy, err)
	}
	if current.ID != requested.ID {
		return BudgetAdjustment{}, fmt.Errorf("%w: budget identity cannot change", ErrInvalidPolicy)
	}
	fields := []struct {
		name  string
		value string
	}{
		{name: "actor", value: actor},
		{name: "authority reference", value: authorityReference},
		{name: "reason", value: reason},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) != field.value ||
			field.value == "" ||
			len(field.value) > 512 {
			return BudgetAdjustment{}, fmt.Errorf(
				"%w: adjustment %s must be non-empty, trimmed, and bounded",
				ErrInvalidPolicy,
				field.name,
			)
		}
	}
	return BudgetAdjustment{
		Previous: current, Adjusted: requested, Actor: actor,
		AuthorityReference: authorityReference, Reason: reason,
	}, nil
}
