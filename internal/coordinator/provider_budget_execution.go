package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

var (
	// ErrProviderBudgetPriceUnknown prevents unknown pricing from becoming a
	// zero-cost authorization.
	ErrProviderBudgetPriceUnknown = errors.New("provider budget price is unknown")
	// ErrProviderBudgetBoundExceeded means billed usage escaped the complete
	// retry bound. Settlement is still recorded, and the ledger blocks later
	// billable work.
	ErrProviderBudgetBoundExceeded = errors.New("provider settlement exceeded approved bound")
)

const maximumBudgetReleaseRevisionRetries = 8

type providerBudgetExecutor interface {
	execute(context.Context, ExecuteProviderRequest) (ProviderExecutionResult, error)
	maximumPhysicalAttempts() int
	getProviderLogicalRequest(
		context.Context,
		domain.ModelRequestID,
	) (storage.ProviderLogicalRequest, error)
	activateProviderLogicalRequest(
		context.Context,
		storage.ProviderLogicalRequest,
	) (storage.ProviderLogicalRequest, error)
}

type providerBudgetLedger interface {
	ReserveProviderBudget(
		context.Context,
		storage.ReserveProviderBudget,
	) (storage.BudgetReservation, storage.BudgetSnapshot, error)
	SettleProviderBudget(
		context.Context,
		storage.SettleProviderBudget,
	) (storage.BudgetSnapshot, error)
	ReleaseProviderBudget(
		context.Context,
		storage.ReleaseProviderBudget,
	) (storage.BudgetSnapshot, error)
	RecordBudgetReconciliationIntent(
		context.Context,
		storage.RecordBudgetReconciliationIntent,
	) (storage.BudgetSnapshot, error)
	RaiseProviderBudget(
		context.Context,
		storage.RaiseProviderBudget,
	) (storage.BudgetSnapshot, error)
	GetBudgetSnapshot(
		context.Context,
		domain.TaskID,
	) (storage.BudgetSnapshot, error)
	GetTaskExecutionPreflight(
		context.Context,
		domain.TaskID,
		uint64,
	) (storage.ExecutionPreflight, error)
}

type budgetedProviderStore interface {
	providerExecutionStore
	providerBudgetLedger
}

// ProviderBudgetSignal keeps warning, exhaustion, and unknown accounting
// distinct from provider cancellation and failure errors.
type ProviderBudgetSignal string

const (
	ProviderBudgetWithinLimit           ProviderBudgetSignal = "within-limit"
	ProviderBudgetWarning               ProviderBudgetSignal = "warning"
	ProviderBudgetExhausted             ProviderBudgetSignal = "exhausted"
	ProviderBudgetAccountingUnknown     ProviderBudgetSignal = "accounting-unknown"
	ProviderBudgetReconciliationPending ProviderBudgetSignal = "reconciliation-pending"
)

// ExecuteBudgetedProviderRequest reserves the complete approved retry bound
// before Execute can reach provider I/O. ApprovedUsagePerAttempt is a hard
// usage bound, not the forecast in Provider.EstimatedUsage.
type ExecuteBudgetedProviderRequest struct {
	BudgetID                domain.BudgetID
	TaskID                  domain.TaskID
	RunID                   domain.RunID
	PreflightRevision       uint64
	ExpectedBudgetRevision  uint64
	ApprovedUsagePerAttempt providers.Usage
	Provider                ExecuteProviderRequest
}

// BudgetedProviderExecutionResult includes the provider result even when the
// just-settled request exhausts the budget and all subsequent work is blocked.
type BudgetedProviderExecutionResult struct {
	Provider    ProviderExecutionResult
	Reservation storage.BudgetReservation
	Budget      storage.BudgetSnapshot
	Signal      ProviderBudgetSignal
}

// BudgetedProviderExecutionService binds provider I/O to the durable budget
// ledger. New reservations surface revision conflicts to callers; settlement
// and release retry idempotently because billed work must never be dropped.
type BudgetedProviderExecutionService struct {
	executor providerBudgetExecutor
	ledger   providerBudgetLedger
}

func newBudgetedProviderExecutionService(
	executor providerBudgetExecutor,
	ledger providerBudgetLedger,
) (*BudgetedProviderExecutionService, error) {
	if executor == nil || ledger == nil {
		return nil, errors.New("budgeted provider execution dependencies are required")
	}
	if executor.maximumPhysicalAttempts() < 1 {
		return nil, errors.New("provider physical-attempt bound is unavailable")
	}
	return &BudgetedProviderExecutionService{executor: executor, ledger: ledger}, nil
}

// NewBudgetedProviderExecutionService is the only production constructor for
// provider execution. The raw executor remains package-private so a caller
// cannot obtain provider I/O authority without the durable budget ledger.
func NewBudgetedProviderExecutionService(
	provider providers.ModelProvider,
	store budgetedProviderStore,
	policy providers.RetryPolicy,
) (*BudgetedProviderExecutionService, error) {
	executor, err := newProviderExecutionService(provider, store, policy)
	if err != nil {
		return nil, err
	}
	return newBudgetedProviderExecutionService(executor, store)
}

func (service *BudgetedProviderExecutionService) Execute(
	ctx context.Context,
	input ExecuteBudgetedProviderRequest,
) (BudgetedProviderExecutionResult, error) {
	var output BudgetedProviderExecutionResult
	if service == nil || service.executor == nil || service.ledger == nil {
		return output, errors.New("budgeted provider execution service is unavailable")
	}
	if input.BudgetID.IsZero() {
		return output, errors.New("provider budget ID is required")
	}
	if input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.PreflightRevision == 0 {
		return output, errors.New(
			"provider task, run, and execution preflight are required",
		)
	}
	if input.Provider.PriceSnapshot == nil {
		return output, ErrProviderBudgetPriceUnknown
	}
	if err := providers.ValidateRequestIdentity(
		input.Provider.Request.Identity,
	); err != nil {
		return output, err
	}
	if err := validateExecutionInput(input.Provider); err != nil {
		return output, err
	}
	logical, err := service.executor.getProviderLogicalRequest(
		ctx,
		input.Provider.Request.Identity.ModelRequestID,
	)
	if err != nil {
		return output, err
	}
	if logical.TaskID != input.TaskID || logical.RunID == nil ||
		*logical.RunID != input.RunID {
		return output, errors.New(
			"provider request does not belong to the authorized task and run",
		)
	}
	if logical.State != storage.ProviderLogicalRequestPlanned {
		return output, errors.New(
			"provider request must be planned before budget reservation",
		)
	}
	budgetSnapshot, err := service.ledger.GetBudgetSnapshot(ctx, input.TaskID)
	if err != nil {
		return output, err
	}
	if budgetSnapshot.BudgetID != input.BudgetID ||
		budgetSnapshot.TaskID != input.TaskID ||
		budgetSnapshot.Revision != input.ExpectedBudgetRevision {
		return output, errors.New(
			"provider budget does not match the authorized task revision",
		)
	}
	preflight, err := service.ledger.GetTaskExecutionPreflight(
		ctx,
		input.TaskID,
		input.PreflightRevision,
	)
	if err != nil {
		return output, err
	}
	if preflight.BudgetID != input.BudgetID ||
		preflight.TaskID != input.TaskID ||
		preflight.BudgetLimitRevision != budgetSnapshot.LimitRevision {
		return output, errors.New(
			"provider budget is not bound to the execution preflight",
		)
	}
	if err := validateProviderUsage(input.ApprovedUsagePerAttempt); err != nil {
		return output, fmt.Errorf("validate approved provider usage bound: %w", err)
	}
	if !input.ApprovedUsagePerAttempt.Known {
		return output, errors.New("approved provider usage bound must be known")
	}
	if input.Provider.Request.MaximumTokens > 0 &&
		input.ApprovedUsagePerAttempt.OutputTokens <
			input.Provider.Request.MaximumTokens {
		return output, errors.New(
			"approved provider usage does not cover the request output limit",
		)
	}
	attemptLimit := service.executor.maximumPhysicalAttempts()
	cost, err := exactMinorCostFromAmount(
		input.Provider.PriceSnapshot.Cost(input.ApprovedUsagePerAttempt),
	)
	if err != nil {
		return output, errors.Join(ErrProviderBudgetPriceUnknown, err)
	}
	if err := validateEstimatedUsageWithinApprovedBound(
		*input.Provider.PriceSnapshot,
		input.Provider.EstimatedUsage,
		input.ApprovedUsagePerAttempt,
	); err != nil {
		return output, err
	}
	costBound, err := multiplyExactMinorCost(cost, attemptLimit)
	if err != nil {
		return output, err
	}
	// The prototype policy grants no unreserved overage. A future non-zero
	// envelope must be added to the immutable policy and preflight rather than
	// accepted as a caller-controlled execution parameter.
	overage := storage.ExactMinorCost{
		Numerator: 0, Denominator: 1, Currency: costBound.Currency,
	}
	maximumSettlement, err := addExactMinorCosts(costBound, overage)
	if err != nil {
		return output, err
	}
	tokenBound, err := providerRetryTokenBound(
		input.ApprovedUsagePerAttempt,
		attemptLimit,
	)
	if err != nil {
		return output, err
	}
	maximumSettlementTokens := tokenBound
	requestID := input.Provider.Request.Identity.ModelRequestID.String()
	if requestID == "" {
		return output, errors.New("provider model request ID is required")
	}
	provenance, err := json.Marshal(map[string]any{
		"schema_version":            1,
		"source":                    "coordinator-provider-budget",
		"price_snapshot_id":         input.Provider.PriceSnapshot.ID,
		"maximum_physical_attempts": attemptLimit,
		"maximum_in_flight_cost_overage": map[string]any{
			"minor_numerator":   overage.Numerator,
			"minor_denominator": overage.Denominator,
			"currency":          overage.Currency,
		},
		"maximum_token_overage": 0,
	})
	if err != nil {
		return output, err
	}
	reservationID := requestID + "-model-budget"
	reservation, snapshot, err := service.ledger.ReserveProviderBudget(
		ctx,
		storage.ReserveProviderBudget{
			ID: reservationID, BudgetID: input.BudgetID,
			ExpectedRevision: input.ExpectedBudgetRevision,
			OperationID:      requestID, RetryOrdinal: 0,
			Category:          storage.BudgetCostModel,
			ProviderCallSlots: uint32(attemptLimit),
			CostBound:         costBound,
			TokenBound:        &tokenBound,
			IdempotencyKey:    reservationID + "-reserve",
			ProvenanceJSON:    string(provenance),
		},
	)
	output.Reservation = reservation
	output.Budget = snapshot
	output.Signal = providerBudgetSignal(snapshot)
	if err != nil {
		return output, err
	}
	if reservation.State != storage.BudgetReservationActive {
		return output, errors.New("provider budget reservation is not active")
	}
	_, activationErr := service.executor.activateProviderLogicalRequest(
		context.WithoutCancel(ctx),
		logical,
	)
	if activationErr != nil {
		released, releaseErr := service.releaseWithRevisionRetry(
			context.WithoutCancel(ctx),
			storage.ReleaseProviderBudget{
				ReservationID:    reservation.ID,
				ExpectedRevision: snapshot.Revision,
				ReasonRedacted:   "provider request activation failed before I/O",
				ProvenanceJSON:   string(provenance),
			},
			reservation.TaskID,
		)
		output.Budget = released
		output.Signal = providerBudgetSignal(released)
		return output, errors.Join(activationErr, releaseErr)
	}

	providerResult, executionErr := service.executor.execute(ctx, input.Provider)
	output.Provider = providerResult
	if providerResult.Accounting.AttemptCount == 0 {
		released, releaseErr := service.releaseWithRevisionRetry(
			context.WithoutCancel(ctx),
			storage.ReleaseProviderBudget{
				ReservationID:    reservation.ID,
				ExpectedRevision: snapshot.Revision,
				ReasonRedacted:   "provider I/O did not begin",
				ProvenanceJSON:   string(provenance),
			},
			reservation.TaskID,
		)
		output.Budget = released
		output.Signal = providerBudgetSignal(released)
		return output, errors.Join(executionErr, releaseErr)
	}

	actualCost, actualCostErr := normalizedProviderActualCost(
		providerResult.Accounting.Cost,
	)
	actualTokens, actualTokensErr := providerActualTokens(
		providerResult.Accounting.Usage,
	)
	actualProviderCallSlots := uint32(providerResult.Accounting.AttemptCount)
	if providerResult.Accounting.AttemptCount > math.MaxUint32 {
		actualProviderCallSlots = math.MaxUint32
	}
	boundErr := providerSettlementBoundError(
		actualCost,
		maximumSettlement,
		actualTokens,
		maximumSettlementTokens,
	)
	if providerResult.Accounting.AttemptCount > uint64(attemptLimit) {
		boundErr = errors.Join(
			boundErr,
			ErrProviderBudgetBoundExceeded,
			errors.New("provider physical attempt count exceeded approved bound"),
		)
	}
	if boundErr != nil {
		pending, intentErr := service.ledger.RecordBudgetReconciliationIntent(
			context.WithoutCancel(ctx),
			storage.RecordBudgetReconciliationIntent{
				ID:                      reservationID + "-reconciliation",
				ReservationID:           reservation.ID,
				ActualProviderCallSlots: actualProviderCallSlots,
				ActualCost:              actualCost,
				ActualTokens:            actualTokens,
				ReasonRedacted:          "provider usage exceeded the finite in-flight settlement envelope",
				IdempotencyKey:          reservationID + "-reconciliation",
				ProvenanceJSON:          string(provenance),
			},
		)
		output.Budget = pending
		output.Signal = providerBudgetSignal(pending)
		joined := errors.Join(
			executionErr,
			actualCostErr,
			actualTokensErr,
			boundErr,
			intentErr,
		)
		if intentErr == nil && pending.ReconciliationPending {
			joined = errors.Join(joined, storage.ErrBudgetReconciliationPending)
		}
		return output, joined
	}
	settled, settleErr := service.ledger.SettleProviderBudget(
		context.WithoutCancel(ctx),
		storage.SettleProviderBudget{
			ID:                      reservationID + "-usage",
			ReservationID:           reservation.ID,
			ActualProviderCallSlots: actualProviderCallSlots,
			ActualCost:              actualCost,
			ActualTokens:            actualTokens,
			IdempotencyKey:          reservationID + "-settle",
			ProvenanceJSON:          string(provenance),
		},
	)
	if settleErr != nil {
		pending, intentErr := service.ledger.RecordBudgetReconciliationIntent(
			context.WithoutCancel(ctx),
			storage.RecordBudgetReconciliationIntent{
				ID:                      reservationID + "-reconciliation",
				ReservationID:           reservation.ID,
				ActualProviderCallSlots: actualProviderCallSlots,
				ActualCost:              actualCost,
				ActualTokens:            actualTokens,
				ReasonRedacted:          "provider usage settlement requires reconciliation",
				IdempotencyKey:          reservationID + "-reconciliation",
				ProvenanceJSON:          string(provenance),
			},
		)
		if intentErr == nil {
			settled = pending
			if pending.ReconciliationPending {
				settleErr = errors.Join(
					settleErr,
					storage.ErrBudgetReconciliationPending,
				)
			} else {
				settleErr = nil
			}
		} else {
			settleErr = errors.Join(settleErr, intentErr)
		}
	}
	output.Budget = settled
	output.Signal = providerBudgetSignal(settled)
	return output, errors.Join(
		executionErr, actualCostErr, actualTokensErr, settleErr, boundErr,
	)
}

func (service *BudgetedProviderExecutionService) releaseWithRevisionRetry(
	ctx context.Context,
	input storage.ReleaseProviderBudget,
	taskID domain.TaskID,
) (storage.BudgetSnapshot, error) {
	var snapshot storage.BudgetSnapshot
	for attempt := 0; attempt < maximumBudgetReleaseRevisionRetries; attempt++ {
		current, err := service.ledger.ReleaseProviderBudget(ctx, input)
		snapshot = current
		if !errors.Is(err, storage.ErrStaleRevision) {
			return snapshot, err
		}
		current, getErr := service.ledger.GetBudgetSnapshot(ctx, taskID)
		if getErr != nil {
			return snapshot, errors.Join(err, getErr)
		}
		snapshot = current
		input.ExpectedRevision = current.Revision
	}
	return snapshot, fmt.Errorf(
		"release provider budget after concurrent postings: %w",
		storage.ErrStaleRevision,
	)
}

// RaiseLimit delegates only an explicitly approval-bound increase. The ledger
// verifies the approval is granted, task-scoped, and attributable.
func (service *BudgetedProviderExecutionService) RaiseLimit(
	ctx context.Context,
	input storage.RaiseProviderBudget,
) (storage.BudgetSnapshot, error) {
	if service == nil || service.ledger == nil {
		return storage.BudgetSnapshot{},
			errors.New("budgeted provider execution service is unavailable")
	}
	if input.ApprovalID.IsZero() {
		return storage.BudgetSnapshot{}, storage.ErrBudgetApprovalRequired
	}
	return service.ledger.RaiseProviderBudget(ctx, input)
}

func providerRetryTokenBound(
	usage providers.Usage,
	attemptLimit int,
) (domain.TokenCount, error) {
	specific, err := providers.NormalizeProviderSpecificUsage(
		usage.ProviderSpecific,
	)
	if err != nil {
		return 0, err
	}
	counts := []int64{
		usage.InputTokens, usage.CachedInputTokens, usage.CacheWriteTokens,
		usage.OutputTokens, usage.ReasoningTokens,
	}
	for _, count := range specific {
		counts = append(counts, count)
	}
	var total uint64
	for _, count := range counts {
		if count < 0 || uint64(count) > math.MaxUint64-total {
			return 0, errors.New("approved provider token bound overflows")
		}
		total += uint64(count)
	}
	if attemptLimit < 1 ||
		total != 0 && uint64(attemptLimit) > math.MaxUint64/total {
		return 0, errors.New("approved provider retry token bound overflows")
	}
	total *= uint64(attemptLimit)
	return domain.TokenCount(total), nil
}

func validateEstimatedUsageWithinApprovedBound(
	price providers.PriceSnapshot,
	estimated providers.Usage,
	approved providers.Usage,
) error {
	if !estimated.Known {
		return nil
	}
	estimatedCost, err := exactMinorCostFromAmount(price.Cost(estimated))
	if err != nil {
		return errors.Join(ErrProviderBudgetPriceUnknown, err)
	}
	approvedCost, err := exactMinorCostFromAmount(price.Cost(approved))
	if err != nil {
		return errors.Join(ErrProviderBudgetPriceUnknown, err)
	}
	comparison, err := compareExactMinorCosts(estimatedCost, approvedCost)
	if err != nil {
		return err
	}
	if comparison > 0 {
		return errors.New("estimated provider cost exceeds approved usage bound")
	}
	estimatedTokens, err := providerRetryTokenBound(estimated, 1)
	if err != nil {
		return err
	}
	approvedTokens, err := providerRetryTokenBound(approved, 1)
	if err != nil {
		return err
	}
	if estimatedTokens > approvedTokens {
		return errors.New("estimated provider tokens exceed approved usage bound")
	}
	return nil
}

func normalizedProviderActualCost(
	cost *storage.ExactMinorCost,
) (*storage.ExactMinorCost, error) {
	if cost == nil {
		return nil, nil
	}
	normalized, err := normalizeExactMinorCost(*cost)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func providerActualTokens(
	usage domain.TokenUsage,
) (*domain.TokenCount, error) {
	total, known, err := usage.Total()
	if err != nil || !known {
		return nil, err
	}
	return &total, nil
}

func providerSettlementBoundError(
	actual *storage.ExactMinorCost,
	bound storage.ExactMinorCost,
	actualTokens *domain.TokenCount,
	tokenBound domain.TokenCount,
) error {
	if actual != nil {
		comparison, err := compareExactMinorCosts(*actual, bound)
		if err != nil {
			return err
		}
		if comparison > 0 {
			return ErrProviderBudgetBoundExceeded
		}
	}
	if actualTokens != nil && *actualTokens > tokenBound {
		return ErrProviderBudgetBoundExceeded
	}
	return nil
}

func providerBudgetSignal(snapshot storage.BudgetSnapshot) ProviderBudgetSignal {
	switch {
	case snapshot.ReconciliationPending:
		return ProviderBudgetReconciliationPending
	case snapshot.CostAccountingUnknown || snapshot.TokenAccountingUnknown:
		return ProviderBudgetAccountingUnknown
	case snapshot.HardCapReached:
		return ProviderBudgetExhausted
	case snapshot.WarningReached:
		return ProviderBudgetWarning
	default:
		return ProviderBudgetWithinLimit
	}
}
