package coordinator

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

func TestBudgetedProviderExecutionBlocksUnknownPriceBeforeIO(t *testing.T) {
	executor := &budgetExecutorStub{maximumAttempts: 3}
	ledger := &budgetLedgerStub{}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)
	input := budgetedProviderRequest(t)
	model := input.Provider.PriceSnapshot.Model
	input.Provider.PriceSnapshot = &providers.PriceSnapshot{
		ID: "unknown-price", Model: model,
		Price: providers.TokenPrice{
			Input: providers.UnknownAmount("USD"),
		},
	}

	_, err := service.Execute(context.Background(), input)
	if !errors.Is(err, ErrProviderBudgetPriceUnknown) {
		t.Fatalf("error = %v, want unknown price", err)
	}
	if executor.calls != 0 || ledger.reserveCalls != 0 {
		t.Fatalf(
			"provider calls = %d, reserves = %d; wanted no I/O",
			executor.calls, ledger.reserveCalls,
		)
	}
}

func TestBudgetedProviderExecutionBlocksHardCapBeforeIO(t *testing.T) {
	executor := &budgetExecutorStub{maximumAttempts: 2}
	ledger := &budgetLedgerStub{reserveErr: storage.ErrBudgetExhausted}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)

	_, err := service.Execute(context.Background(), budgetedProviderRequest(t))
	if !errors.Is(err, storage.ErrBudgetExhausted) {
		t.Fatalf("error = %v, want hard-cap exhaustion", err)
	}
	if executor.calls != 0 {
		t.Fatalf("provider calls = %d, want zero", executor.calls)
	}
}

func TestBudgetedProviderExecutionReservesCompleteRetryBoundThenSettles(t *testing.T) {
	usd := domain.CurrencyCode("USD")
	executor := &budgetExecutorStub{
		maximumAttempts: 3,
		result: ProviderExecutionResult{
			Accounting: storage.ProviderRequestAccountingSummary{
				AttemptCount: 2,
				Cost: &storage.ExactMinorCost{
					Numerator: 2, Denominator: 1, Currency: usd,
				},
				Usage: domain.TokenUsage{
					Known: true, Input: 2_000_000,
				},
			},
		},
	}
	ledger := &budgetLedgerStub{
		reserveSnapshot: storage.BudgetSnapshot{Revision: 8},
		settleSnapshot: storage.BudgetSnapshot{
			Revision: 9, WarningReached: true,
		},
	}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)

	result, err := service.Execute(
		context.Background(),
		budgetedProviderRequest(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.order) != 3 ||
		ledger.order[0] != "reserve" ||
		ledger.order[1] != "activate" ||
		ledger.order[2] != "settle" {
		t.Fatalf("ledger order = %#v", ledger.order)
	}
	if executor.reserveCallsObserved != 1 {
		t.Fatalf(
			"reserves visible before provider = %d, want one",
			executor.reserveCallsObserved,
		)
	}
	if ledger.reserve.CostBound.Numerator != 3 ||
		ledger.reserve.CostBound.Denominator != 1 {
		t.Fatalf("retry cost bound = %#v", ledger.reserve.CostBound)
	}
	if ledger.reserve.TokenBound == nil ||
		*ledger.reserve.TokenBound != 3_000_000 {
		t.Fatalf("retry token bound = %#v", ledger.reserve.TokenBound)
	}
	if ledger.reserve.ProviderCallSlots != 3 {
		t.Fatalf(
			"provider-call slots = %d, want 3",
			ledger.reserve.ProviderCallSlots,
		)
	}
	if ledger.settle.ActualCost == nil ||
		ledger.settle.ActualCost.Numerator != 2 {
		t.Fatalf("settlement = %#v", ledger.settle)
	}
	if ledger.settle.ActualProviderCallSlots != 2 {
		t.Fatalf(
			"settled provider-call slots = %d, want 2",
			ledger.settle.ActualProviderCallSlots,
		)
	}
	if result.Signal != ProviderBudgetWarning {
		t.Fatalf("budget signal = %q, want warning", result.Signal)
	}
}

func TestBudgetedProviderExecutionRejectsCrossTaskBudgetBeforeReservation(
	t *testing.T,
) {
	executor := &budgetExecutorStub{maximumAttempts: 1}
	ledger := &budgetLedgerStub{}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)
	input := budgetedProviderRequest(t)
	wrongTask, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	executor.logicalTaskID = wrongTask

	_, err = service.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("cross-task provider request unexpectedly authorized")
	}
	if ledger.reserveCalls != 0 || executor.activationCalls != 0 ||
		executor.calls != 0 {
		t.Fatalf(
			"reserve=%d activate=%d execute=%d; want all zero",
			ledger.reserveCalls,
			executor.activationCalls,
			executor.calls,
		)
	}
}

func TestBudgetedProviderExecutionQuarantinesEscapedCostBound(t *testing.T) {
	usd := domain.CurrencyCode("USD")
	executor := &budgetExecutorStub{
		maximumAttempts: 1,
		result: ProviderExecutionResult{
			Accounting: storage.ProviderRequestAccountingSummary{
				AttemptCount: 1,
				Cost: &storage.ExactMinorCost{
					Numerator: 2, Denominator: 1, Currency: usd,
				},
				Usage: domain.TokenUsage{Known: true, Input: 1_000_000},
			},
		},
	}
	ledger := &budgetLedgerStub{
		reserveSnapshot: storage.BudgetSnapshot{Revision: 2},
		recordSnapshot: storage.BudgetSnapshot{
			Revision: 3, ReconciliationPending: true,
		},
	}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)

	result, err := service.Execute(
		context.Background(),
		budgetedProviderRequest(t),
	)
	if !errors.Is(err, ErrProviderBudgetBoundExceeded) {
		t.Fatalf("error = %v, want escaped-bound error", err)
	}
	if ledger.settleCalls != 0 || ledger.recordCalls != 1 {
		t.Fatalf(
			"settle calls = %d, reconciliation calls = %d; want 0, 1",
			ledger.settleCalls,
			ledger.recordCalls,
		)
	}
	if ledger.record.ActualProviderCallSlots != 1 {
		t.Fatalf(
			"reconciled provider-call slots = %d, want 1",
			ledger.record.ActualProviderCallSlots,
		)
	}
	if !errors.Is(err, storage.ErrBudgetReconciliationPending) ||
		result.Signal != ProviderBudgetReconciliationPending {
		t.Fatalf(
			"error = %v, budget signal = %q; want durable reconciliation",
			err,
			result.Signal,
		)
	}
}

func TestBudgetedProviderExecutionReleasesWhenProviderIODidNotBegin(t *testing.T) {
	executor := &budgetExecutorStub{
		maximumAttempts: 1,
		err:             context.Canceled,
	}
	ledger := &budgetLedgerStub{
		reserveSnapshot: storage.BudgetSnapshot{Revision: 4},
		releaseSnapshot: storage.BudgetSnapshot{Revision: 5},
	}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)

	_, err := service.Execute(context.Background(), budgetedProviderRequest(t))
	if !errors.Is(err, context.Canceled) ||
		errors.Is(err, storage.ErrBudgetExhausted) {
		t.Fatalf("error = %v, want cancellation distinct from exhaustion", err)
	}
	if ledger.releaseCalls != 1 || ledger.settleCalls != 0 {
		t.Fatalf(
			"release calls = %d, settle calls = %d",
			ledger.releaseCalls, ledger.settleCalls,
		)
	}
}

func TestBudgetedProviderExecutionDurablyRecordsFailedSettlement(t *testing.T) {
	usd := domain.CurrencyCode("USD")
	executor := &budgetExecutorStub{
		maximumAttempts: 1,
		result: ProviderExecutionResult{
			Accounting: storage.ProviderRequestAccountingSummary{
				AttemptCount: 1,
				Cost: &storage.ExactMinorCost{
					Numerator: 1, Denominator: 1, Currency: usd,
				},
				Usage: domain.TokenUsage{Known: true, Input: 1_000_000},
			},
		},
	}
	ledger := &budgetLedgerStub{
		reserveSnapshot: storage.BudgetSnapshot{Revision: 4},
		settleErr:       storage.ErrStaleRevision,
		recordSnapshot: storage.BudgetSnapshot{
			Revision: 5, ReconciliationPending: true,
		},
	}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)

	result, err := service.Execute(
		context.Background(),
		budgetedProviderRequest(t),
	)
	if !errors.Is(err, storage.ErrStaleRevision) ||
		!errors.Is(err, storage.ErrBudgetReconciliationPending) {
		t.Fatalf("error = %v, want stale plus durable reconciliation", err)
	}
	if ledger.settleCalls != 1 || ledger.recordCalls != 1 {
		t.Fatalf(
			"settle calls = %d, reconciliation records = %d",
			ledger.settleCalls, ledger.recordCalls,
		)
	}
	if ledger.record.ActualCost == nil ||
		ledger.record.ActualTokens == nil ||
		result.Signal != ProviderBudgetReconciliationPending {
		t.Fatalf("reconciliation = %#v, result = %#v", ledger.record, result)
	}
}

func TestBudgetedProviderExecutionConfirmsAmbiguousSettlementByPostingLookup(
	t *testing.T,
) {
	usd := domain.CurrencyCode("USD")
	executor := &budgetExecutorStub{
		maximumAttempts: 1,
		result: ProviderExecutionResult{
			Accounting: storage.ProviderRequestAccountingSummary{
				AttemptCount: 1,
				Cost: &storage.ExactMinorCost{
					Numerator: 1, Denominator: 1, Currency: usd,
				},
				Usage: domain.TokenUsage{Known: true, Input: 1_000_000},
			},
		},
	}
	ledger := &budgetLedgerStub{
		reserveSnapshot: storage.BudgetSnapshot{Revision: 4},
		settleErr:       storage.ErrBusy,
		recordSnapshot:  storage.BudgetSnapshot{Revision: 5},
	}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)

	result, err := service.Execute(
		context.Background(),
		budgetedProviderRequest(t),
	)
	if err != nil {
		t.Fatalf("confirmed posting returned error: %v", err)
	}
	if ledger.recordCalls != 1 ||
		result.Signal != ProviderBudgetWithinLimit {
		t.Fatalf(
			"record calls = %d, result = %#v",
			ledger.recordCalls, result,
		)
	}
}

func TestBudgetedProviderExecutionSurfacesTokenOnlyBoundEscape(t *testing.T) {
	usd := domain.CurrencyCode("USD")
	executor := &budgetExecutorStub{
		maximumAttempts: 1,
		result: ProviderExecutionResult{
			Accounting: storage.ProviderRequestAccountingSummary{
				AttemptCount: 1,
				Cost: &storage.ExactMinorCost{
					Numerator: 1, Denominator: 1, Currency: usd,
				},
				Usage: domain.TokenUsage{Known: true, Input: 1_000_001},
			},
		},
	}
	ledger := &budgetLedgerStub{
		reserveSnapshot: storage.BudgetSnapshot{Revision: 2},
		recordSnapshot: storage.BudgetSnapshot{
			Revision: 3, ReconciliationPending: true,
		},
	}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)

	_, err := service.Execute(context.Background(), budgetedProviderRequest(t))
	if !errors.Is(err, ErrProviderBudgetBoundExceeded) {
		t.Fatalf("error = %v, want token bound escape", err)
	}
	if ledger.settleCalls != 0 || ledger.recordCalls != 1 {
		t.Fatalf(
			"settle calls = %d, reconciliation calls = %d; want 0, 1",
			ledger.settleCalls,
			ledger.recordCalls,
		)
	}
}

func TestBudgetedProviderExecutionRequiresApprovalToRaiseLimit(t *testing.T) {
	executor := &budgetExecutorStub{maximumAttempts: 1}
	ledger := &budgetLedgerStub{}
	service := newBudgetedProviderExecutionFixture(t, executor, ledger)

	_, err := service.RaiseLimit(
		context.Background(),
		storage.RaiseProviderBudget{},
	)
	if !errors.Is(err, storage.ErrBudgetApprovalRequired) {
		t.Fatalf("error = %v, want approval required", err)
	}
	if ledger.raiseCalls != 0 {
		t.Fatalf("raise calls = %d, want zero", ledger.raiseCalls)
	}
}

func newBudgetedProviderExecutionFixture(
	t *testing.T,
	executor *budgetExecutorStub,
	ledger *budgetLedgerStub,
) *BudgetedProviderExecutionService {
	t.Helper()
	executor.ledger = ledger
	service, err := newBudgetedProviderExecutionService(executor, ledger)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func budgetedProviderRequest(t *testing.T) ExecuteBudgetedProviderRequest {
	t.Helper()
	budgetID, err := domain.ParseBudgetID(
		"bdg_00000000-0000-7000-8000-000000000301",
	)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.ParseTaskID(
		"tsk_00000000-0000-7000-8000-000000000302",
	)
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.ParseRunID(
		"run_00000000-0000-7000-8000-000000000303",
	)
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	inputPrice, err := providers.NewExactAmount("USD", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	provider := providers.ProviderIdentity{
		Adapter: "test", AdapterVersion: "1",
		Provider: "test", ProviderVersion: "1",
	}
	model := providers.ModelIdentity{
		Provider: provider, Model: "test-model", Revision: "1",
	}
	identity := providers.RequestIdentity{
		ModelRequestID: requestID, Provider: provider, Model: model,
		IdempotencyKey: "logical-request-1",
		RequestHash:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	return ExecuteBudgetedProviderRequest{
		BudgetID:               budgetID,
		TaskID:                 taskID,
		RunID:                  runID,
		PreflightRevision:      1,
		ExpectedBudgetRevision: 7,
		ApprovedUsagePerAttempt: providers.Usage{
			Known: true, Source: providers.UsageSourceEstimated,
			InputTokens: 1_000_000,
		},
		Provider: ExecuteProviderRequest{
			Request: providers.ModelRequest{
				Identity: identity,
			},
			EstimatedUsage: providers.Usage{
				Known: true, Source: providers.UsageSourceEstimated,
				InputTokens: 500_000,
			},
			PriceSnapshot: &providers.PriceSnapshot{
				ID: "price-1", Model: model,
				Price: providers.TokenPrice{
					Input: inputPrice,
				},
			},
		},
	}
}

type budgetExecutorStub struct {
	maximumAttempts      int
	result               ProviderExecutionResult
	err                  error
	ledger               *budgetLedgerStub
	calls                int
	reserveCallsObserved int
	activationCalls      int
	logicalTaskID        domain.TaskID
}

func (executor *budgetExecutorStub) getProviderLogicalRequest(
	_ context.Context,
	requestID domain.ModelRequestID,
) (storage.ProviderLogicalRequest, error) {
	taskID, err := domain.ParseTaskID(
		"tsk_00000000-0000-7000-8000-000000000302",
	)
	if err != nil {
		return storage.ProviderLogicalRequest{}, err
	}
	if !executor.logicalTaskID.IsZero() {
		taskID = executor.logicalTaskID
	}
	runID, err := domain.ParseRunID(
		"run_00000000-0000-7000-8000-000000000303",
	)
	if err != nil {
		return storage.ProviderLogicalRequest{}, err
	}
	return storage.ProviderLogicalRequest{
		ID: requestID, TaskID: taskID, RunID: &runID,
		State: storage.ProviderLogicalRequestPlanned,
	}, nil
}

func (executor *budgetExecutorStub) activateProviderLogicalRequest(
	_ context.Context,
	logical storage.ProviderLogicalRequest,
) (storage.ProviderLogicalRequest, error) {
	executor.activationCalls++
	if executor.ledger != nil {
		executor.ledger.order = append(executor.ledger.order, "activate")
	}
	logical.State = storage.ProviderLogicalRequestInFlight
	return logical, nil
}

func (executor *budgetExecutorStub) maximumPhysicalAttempts() int {
	return executor.maximumAttempts
}

func (executor *budgetExecutorStub) execute(
	context.Context,
	ExecuteProviderRequest,
) (ProviderExecutionResult, error) {
	executor.calls++
	if executor.ledger != nil {
		executor.reserveCallsObserved = executor.ledger.reserveCalls
	}
	return executor.result, executor.err
}

type budgetLedgerStub struct {
	reserve         storage.ReserveProviderBudget
	settle          storage.SettleProviderBudget
	release         storage.ReleaseProviderBudget
	record          storage.RecordBudgetReconciliationIntent
	raise           storage.RaiseProviderBudget
	reservation     storage.BudgetReservation
	reserveSnapshot storage.BudgetSnapshot
	settleSnapshot  storage.BudgetSnapshot
	releaseSnapshot storage.BudgetSnapshot
	recordSnapshot  storage.BudgetSnapshot
	raiseSnapshot   storage.BudgetSnapshot
	getSnapshot     storage.BudgetSnapshot
	reserveErr      error
	settleErr       error
	releaseErr      error
	recordErr       error
	raiseErr        error
	getErr          error
	reserveCalls    int
	settleCalls     int
	releaseCalls    int
	recordCalls     int
	raiseCalls      int
	getCalls        int
	order           []string
}

func (ledger *budgetLedgerStub) ReserveProviderBudget(
	_ context.Context,
	input storage.ReserveProviderBudget,
) (storage.BudgetReservation, storage.BudgetSnapshot, error) {
	ledger.reserveCalls++
	ledger.order = append(ledger.order, "reserve")
	ledger.reserve = input
	reservation := ledger.reservation
	if reservation.ID == "" {
		reservation.ID = input.ID
		reservation.CostBound = input.CostBound
		reservation.TokenBound = input.TokenBound
		reservation.State = storage.BudgetReservationActive
	}
	return reservation, ledger.reserveSnapshot, ledger.reserveErr
}

func (ledger *budgetLedgerStub) SettleProviderBudget(
	_ context.Context,
	input storage.SettleProviderBudget,
) (storage.BudgetSnapshot, error) {
	ledger.settleCalls++
	ledger.order = append(ledger.order, "settle")
	ledger.settle = input
	return ledger.settleSnapshot, ledger.settleErr
}

func (ledger *budgetLedgerStub) ReleaseProviderBudget(
	_ context.Context,
	input storage.ReleaseProviderBudget,
) (storage.BudgetSnapshot, error) {
	ledger.releaseCalls++
	ledger.order = append(ledger.order, "release")
	ledger.release = input
	return ledger.releaseSnapshot, ledger.releaseErr
}

func (ledger *budgetLedgerStub) RecordBudgetReconciliationIntent(
	_ context.Context,
	input storage.RecordBudgetReconciliationIntent,
) (storage.BudgetSnapshot, error) {
	ledger.recordCalls++
	ledger.order = append(ledger.order, "record-reconciliation")
	ledger.record = input
	return ledger.recordSnapshot, ledger.recordErr
}

func (ledger *budgetLedgerStub) RaiseProviderBudget(
	_ context.Context,
	input storage.RaiseProviderBudget,
) (storage.BudgetSnapshot, error) {
	ledger.raiseCalls++
	ledger.raise = input
	return ledger.raiseSnapshot, ledger.raiseErr
}

func (ledger *budgetLedgerStub) GetBudgetSnapshot(
	_ context.Context,
	taskID domain.TaskID,
) (storage.BudgetSnapshot, error) {
	ledger.getCalls++
	snapshot := ledger.getSnapshot
	if snapshot.BudgetID.IsZero() {
		budgetID, err := domain.ParseBudgetID(
			"bdg_00000000-0000-7000-8000-000000000301",
		)
		if err != nil {
			return storage.BudgetSnapshot{}, err
		}
		snapshot.BudgetID = budgetID
		snapshot.TaskID = taskID
		snapshot.Revision = 7
		snapshot.LimitRevision = 1
	}
	return snapshot, ledger.getErr
}

func (ledger *budgetLedgerStub) GetTaskExecutionPreflight(
	_ context.Context,
	taskID domain.TaskID,
	revision uint64,
) (storage.ExecutionPreflight, error) {
	budgetID, err := domain.ParseBudgetID(
		"bdg_00000000-0000-7000-8000-000000000301",
	)
	if err != nil {
		return storage.ExecutionPreflight{}, err
	}
	return storage.ExecutionPreflight{
		TaskID: taskID, Revision: revision, BudgetID: budgetID,
		BudgetLimitRevision: 1,
	}, nil
}
