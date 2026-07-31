package transport

import (
	"context"
	"errors"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
)

func TestTaskServiceSetBudgetDelegatesExactMinorUnits(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	budgetID, _ := domain.NewBudgetID()
	identity, _ := TaskIDToProto(taskID)
	usd, _ := domain.ParseCurrencyCode("USD")
	revision := uint64(4)
	budgets := &taskBudgetApplicationStub{view: TaskBudgetView{
		BudgetID: budgetID, TaskID: taskID,
		HardLimit: domain.Money{Currency: usd, MinorUnits: 7_500},
		Revision:  5,
	}}
	service, err := NewTaskServiceWithBudget(
		&taskControlApplicationStub{}, &taskQueryApplicationStub{}, budgets,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.SetBudget(t.Context(), &codefluxv1.SetBudgetRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "set-budget-exact-1", ExpectedRevision: &revision,
		},
		TaskId: identity,
		HardLimit: &codefluxv1.Money{
			CurrencyCode: "USD", MinorUnits: 7_500, DecimalPlaces: 0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if budgets.command.TaskID != taskID || budgets.command.ExpectedRevision != 4 ||
		budgets.command.IdempotencyKey != "set-budget-exact-1" ||
		budgets.command.HardLimit.MinorUnits != 7_500 ||
		response.GetBudget().GetBudgetId().GetValue() != budgetID.String() ||
		response.GetBudget().GetHardLimit().GetMinorUnits() != 7_500 ||
		response.GetBudget().GetRevision() != 5 {
		t.Fatalf("command/response = %#v / %#v", budgets.command, response)
	}
}

func TestTaskServiceSetBudgetRejectsUnrepresentableDecimalScale(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	identity, _ := TaskIDToProto(taskID)
	revision := uint64(0)
	service, err := NewTaskServiceWithBudget(
		&taskControlApplicationStub{}, &taskQueryApplicationStub{}, &taskBudgetApplicationStub{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SetBudget(t.Context(), &codefluxv1.SetBudgetRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "set-budget-scaled-1", ExpectedRevision: &revision,
		},
		TaskId: identity,
		HardLimit: &codefluxv1.Money{
			CurrencyCode: "USD", MinorUnits: 125, DecimalPlaces: 2,
		},
	})
	var validation *RequestValidationError
	if !errors.As(err, &validation) || validation.Field != "hard_limit.decimal_places" {
		t.Fatalf("scaled money error = %#v", err)
	}
}

func TestTaskServiceSetBudgetMapsApprovalRequired(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	identity, _ := TaskIDToProto(taskID)
	revision := uint64(2)
	service, err := NewTaskServiceWithBudget(
		&taskControlApplicationStub{}, &taskQueryApplicationStub{},
		&taskBudgetApplicationStub{err: ErrTaskBudgetApprovalRequired},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SetBudget(t.Context(), &codefluxv1.SetBudgetRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "set-budget-needs-approval", ExpectedRevision: &revision,
		},
		TaskId:    identity,
		HardLimit: &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 9_000},
	})
	var applicationErr *ApplicationError
	if !errors.As(err, &applicationErr) ||
		applicationErr.Code != codefluxv1.ErrorCode_ERROR_CODE_DENIED {
		t.Fatalf("approval error = %#v", err)
	}
}

type taskBudgetApplicationStub struct {
	command TaskBudgetCommand
	view    TaskBudgetView
	err     error
}

func (stub *taskBudgetApplicationStub) SetTaskBudget(
	_ context.Context,
	command TaskBudgetCommand,
) (TaskBudgetView, error) {
	stub.command = command
	return stub.view, stub.err
}
