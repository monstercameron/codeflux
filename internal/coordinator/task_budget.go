package coordinator

import (
	"context"
	"errors"
	"math/big"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

const (
	taskBudgetActorReference = "authenticated-local-user"
	taskBudgetReason         = "user requested exact task hard-budget change"
	taskBudgetProvenance     = `{"schema_version":1,"source":"task-service-set-budget"}`
)

// TaskBudgetStore owns the authoritative budget read and the two existing
// authority-preserving mutation paths used by TaskService.SetBudget.
type TaskBudgetStore interface {
	ReadTaskBudgetAdjustmentState(
		context.Context,
		domain.TaskID,
	) (storage.TaskBudgetAdjustmentState, error)
	AdjustBudgetBeforeApproval(
		context.Context,
		storage.AdjustPreApprovalBudget,
	) (storage.PreApprovalBudgetAdjustment, storage.BudgetSnapshot, error)
	ResolveBudgetRaiseApproval(
		context.Context,
		domain.BudgetID,
		domain.TaskID,
		string,
		string,
	) (domain.ApprovalID, error)
	RaiseProviderBudget(
		context.Context,
		storage.RaiseProviderBudget,
	) (storage.BudgetSnapshot, error)
}

// TaskBudgetService changes only the hard cost cap carried by the v1 RPC. It
// preserves every omitted budget dimension from authoritative storage.
type TaskBudgetService struct {
	store TaskBudgetStore
}

func NewTaskBudgetService(store TaskBudgetStore) (*TaskBudgetService, error) {
	if store == nil {
		return nil, errors.New("task budget store is required")
	}
	return &TaskBudgetService{store: store}, nil
}

func (service *TaskBudgetService) SetTaskBudget(
	ctx context.Context,
	command transport.TaskBudgetCommand,
) (transport.TaskBudgetView, error) {
	current, err := service.store.ReadTaskBudgetAdjustmentState(ctx, command.TaskID)
	if err != nil {
		return transport.TaskBudgetView{}, taskBudgetPortError(err)
	}
	if command.HardLimit.Currency != current.Snapshot.HardCost.Currency {
		return transport.TaskBudgetView{}, transport.ErrTaskBudgetConflict
	}
	requestedHard := storage.ExactMinorCost{
		Numerator: command.HardLimit.MinorUnits, Denominator: 1,
		Currency: command.HardLimit.Currency,
	}
	var snapshot storage.BudgetSnapshot
	switch current.TaskState {
	case domain.TaskStateDraft,
		domain.TaskStateForecasting,
		domain.TaskStateAwaitingPlanApproval:
		// The pre-approval replacement API stores integral minor units. Refuse
		// an inconsistent rational revision rather than rounding it or silently
		// copying stale legacy columns.
		if !sameIntegralBudgetLimits(current) {
			return transport.TaskBudgetView{}, transport.ErrTaskBudgetUnsupported
		}
		requested := current.Account.Budget
		requested.HardStopCost = command.HardLimit
		if requested.WarningCost.MinorUnits > requested.HardStopCost.MinorUnits {
			return transport.TaskBudgetView{}, transport.ErrTaskBudgetUnsupported
		}
		_, snapshot, err = service.store.AdjustBudgetBeforeApproval(
			ctx,
			storage.AdjustPreApprovalBudget{
				BudgetID:               current.Snapshot.BudgetID,
				ExpectedBudgetRevision: command.ExpectedRevision,
				ExpectedLimitRevision:  current.Snapshot.LimitRevision,
				Requested:              requested,
				Actor:                  taskBudgetActorReference,
				AuthorityReference:     "TaskService.SetBudget",
				Reason:                 taskBudgetReason,
				IdempotencyKey:         command.IdempotencyKey,
			},
		)
	default:
		if compareTaskBudgetCosts(requestedHard, current.Snapshot.HardCost) < 0 {
			return transport.TaskBudgetView{}, transport.ErrTaskBudgetConflict
		}
		scope := storage.BudgetRaiseApprovalScope(
			current.Snapshot.BudgetID,
			current.Snapshot.WarningCost,
			requestedHard,
			current.Snapshot.WarningTokens,
			current.Snapshot.HardTokens,
		)
		approvalID, approvalErr := service.store.ResolveBudgetRaiseApproval(
			ctx, current.Snapshot.BudgetID, command.TaskID,
			command.IdempotencyKey, scope,
		)
		if approvalErr != nil {
			return transport.TaskBudgetView{}, taskBudgetPortError(approvalErr)
		}
		snapshot, err = service.store.RaiseProviderBudget(
			ctx,
			storage.RaiseProviderBudget{
				BudgetID:         current.Snapshot.BudgetID,
				ExpectedRevision: command.ExpectedRevision,
				WarningCost:      current.Snapshot.WarningCost,
				HardCost:         requestedHard,
				WarningTokens:    current.Snapshot.WarningTokens,
				HardTokens:       current.Snapshot.HardTokens,
				ApprovalID:       approvalID,
				ActorKind:        "user",
				ActorReference:   taskBudgetActorReference,
				ReasonRedacted:   taskBudgetReason,
				IdempotencyKey:   command.IdempotencyKey,
				ProvenanceJSON:   taskBudgetProvenance,
			},
		)
	}
	if err != nil {
		return transport.TaskBudgetView{}, taskBudgetPortError(err)
	}
	return taskBudgetTransportView(snapshot)
}

func sameIntegralBudgetLimits(state storage.TaskBudgetAdjustmentState) bool {
	return state.Snapshot.WarningCost.Denominator == 1 &&
		state.Snapshot.HardCost.Denominator == 1 &&
		state.Snapshot.WarningCost.Currency == state.Account.Budget.WarningCost.Currency &&
		state.Snapshot.HardCost.Currency == state.Account.Budget.HardStopCost.Currency &&
		state.Snapshot.WarningCost.Numerator == state.Account.Budget.WarningCost.MinorUnits &&
		state.Snapshot.HardCost.Numerator == state.Account.Budget.HardStopCost.MinorUnits &&
		state.Snapshot.WarningTokens == state.Account.Budget.WarningTokens &&
		state.Snapshot.HardTokens == state.Account.Budget.HardStopTokens
}

func compareTaskBudgetCosts(left, right storage.ExactMinorCost) int {
	leftValue := new(big.Int).Mul(
		big.NewInt(left.Numerator), big.NewInt(right.Denominator),
	)
	rightValue := new(big.Int).Mul(
		big.NewInt(right.Numerator), big.NewInt(left.Denominator),
	)
	return leftValue.Cmp(rightValue)
}

func taskBudgetTransportView(
	snapshot storage.BudgetSnapshot,
) (transport.TaskBudgetView, error) {
	hard, err := requiredIntegralBudgetMoney(snapshot.HardCost)
	if err != nil {
		return transport.TaskBudgetView{}, err
	}
	reserved, err := optionalIntegralBudgetMoney(snapshot.ReservedCost)
	if err != nil {
		return transport.TaskBudgetView{}, err
	}
	var actual *domain.Money
	if !snapshot.CostAccountingUnknown {
		actual, err = optionalIntegralBudgetMoney(snapshot.ActualKnownCost)
		if err != nil {
			return transport.TaskBudgetView{}, err
		}
	}
	return transport.TaskBudgetView{
		BudgetID: snapshot.BudgetID, TaskID: snapshot.TaskID,
		HardLimit: hard, Reserved: reserved, Actual: actual,
		Revision: snapshot.Revision,
	}, nil
}

func requiredIntegralBudgetMoney(value storage.ExactMinorCost) (domain.Money, error) {
	money, err := optionalIntegralBudgetMoney(value)
	if err != nil {
		return domain.Money{}, err
	}
	if money == nil {
		return domain.Money{}, transport.ErrTaskBudgetUnsupported
	}
	return *money, nil
}

func optionalIntegralBudgetMoney(value storage.ExactMinorCost) (*domain.Money, error) {
	if value.Denominator <= 0 || value.Numerator < 0 {
		return nil, errors.New("budget exact cost is invalid")
	}
	if value.Denominator != 1 {
		return nil, nil
	}
	money, err := domain.NewMoney(value.Currency, value.Numerator)
	if err != nil {
		return nil, err
	}
	return &money, nil
}

func taskBudgetPortError(err error) error {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return transport.ErrTaskBudgetNotFound
	case errors.Is(err, storage.ErrStaleRevision):
		return transport.ErrTaskBudgetStaleRevision
	case errors.Is(err, storage.ErrBudgetApprovalRequired):
		return transport.ErrTaskBudgetApprovalRequired
	case errors.Is(err, storage.ErrConflict), errors.Is(err, storage.ErrConstraint):
		return transport.ErrTaskBudgetConflict
	default:
		return err
	}
}

var _ transport.TaskBudgetApplication = (*TaskBudgetService)(nil)
