package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestBudgetLedgerReservesRetriesSettlesAndReleasesExactly(t *testing.T) {
	ctx := context.Background()
	repositories, task, budgetID, usd := createBudgetLedgerFixture(t, 10, 10_000)
	initial, err := repositories.GetBudgetSnapshot(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Revision != 0 ||
		initial.HardCost.Numerator != 10 ||
		initial.HardCost.Denominator != 1 ||
		initial.RemainingCost == nil ||
		initial.RemainingCost.Numerator != 10 {
		t.Fatalf("initial snapshot = %#v", initial)
	}
	firstInput := ReserveProviderBudget{
		ID: "reservation-first", BudgetID: budgetID, ExpectedRevision: 0,
		OperationID: "logical-request", AttemptID: stringPointer("attempt-1"),
		RetryOrdinal: 1, Category: BudgetCostModel,
		CostBound: ExactMinorCost{
			Numerator: 1, Denominator: 3, Currency: usd,
		},
		TokenBound:     tokenCountPointer(300),
		IdempotencyKey: "reserve-attempt-1",
		ProvenanceJSON: `{"schema_version":1,"pricing_known":true}`,
	}
	first, snapshot, err := repositories.ReserveProviderBudget(ctx, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != BudgetReservationActive ||
		snapshot.ReservedCost.Numerator != 1 ||
		snapshot.ReservedCost.Denominator != 3 ||
		snapshot.ReservedTokens != 300 ||
		snapshot.Revision != 1 {
		t.Fatalf("first reservation = %#v, snapshot = %#v", first, snapshot)
	}
	retried, retrySnapshot, err := repositories.ReserveProviderBudget(
		ctx, firstInput,
	)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != first.ID || retrySnapshot.Revision != 1 {
		t.Fatalf("idempotent reservation = %#v, %#v", retried, retrySnapshot)
	}
	secondInput := firstInput
	secondInput.ID = "reservation-second"
	secondInput.ExpectedRevision = 1
	secondInput.AttemptID = stringPointer("attempt-2")
	secondInput.RetryOrdinal = 2
	secondInput.IdempotencyKey = "reserve-attempt-2"
	_, snapshot, err = repositories.ReserveProviderBudget(ctx, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ReservedCost.Numerator != 2 ||
		snapshot.ReservedCost.Denominator != 3 ||
		snapshot.Categories[0].ReservationCount != 2 {
		t.Fatalf("retry snapshot = %#v", snapshot)
	}
	actualCost := ExactMinorCost{
		Numerator: 1, Denominator: 2, Currency: usd,
	}
	actualTokens := domain.TokenCount(150)
	snapshot, err = repositories.SettleProviderBudget(
		ctx,
		SettleProviderBudget{
			ID: "posting-first", ReservationID: first.ID,
			ActualCost: &actualCost, ActualTokens: &actualTokens,
			IdempotencyKey: "post-attempt-1",
			ProvenanceJSON: `{"schema_version":1,"source":"provider"}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ReservedCost.Numerator != 1 ||
		snapshot.ReservedCost.Denominator != 3 ||
		snapshot.ChargedCost.Numerator != 1 ||
		snapshot.ChargedCost.Denominator != 2 ||
		snapshot.ActualKnownCost != actualCost ||
		snapshot.ActualTokens != actualTokens ||
		snapshot.ChargedTokens != actualTokens {
		t.Fatalf("settled snapshot = %#v", snapshot)
	}
	snapshot, err = repositories.ReleaseProviderBudget(
		ctx,
		ReleaseProviderBudget{
			ReservationID: secondInput.ID, ExpectedRevision: 3,
			ReasonRedacted: "retry was cancelled before provider I/O",
			ProvenanceJSON: `{"schema_version":1,"pre_io":true}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ReservedCost.Numerator != 0 ||
		snapshot.ChargedCost != actualCost ||
		snapshot.Revision != 4 {
		t.Fatalf("released snapshot = %#v", snapshot)
	}
}

func TestBudgetLedgerReconcilesReservedProviderCallsToPhysicalAttempts(
	t *testing.T,
) {
	ctx := context.Background()
	repositories, task, budgetID, usd := createBudgetLedgerFixture(
		t, 10, 1_000,
	)
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE budgets SET maximum_provider_calls = 3 WHERE id = ?`,
		budgetID,
	); err != nil {
		t.Fatal(err)
	}
	tokenBound := domain.TokenCount(100)
	_, reserved, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "retry-capacity-first", BudgetID: budgetID,
			ExpectedRevision: 0, OperationID: "retry-operation-first",
			Category: BudgetCostModel, ProviderCallSlots: 3,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 1, Currency: usd,
			},
			TokenBound:     &tokenBound,
			IdempotencyKey: "retry-capacity-first",
			ProvenanceJSON: `{"schema_version":1}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	actualCost := ExactMinorCost{
		Numerator: 1, Denominator: 2, Currency: usd,
	}
	actualTokens := domain.TokenCount(25)
	settled, err := repositories.SettleProviderBudget(
		ctx,
		SettleProviderBudget{
			ID:            "retry-capacity-first-usage",
			ReservationID: "retry-capacity-first",
			ActualCost:    &actualCost, ActualTokens: &actualTokens,
			ActualProviderCallSlots: 1,
			IdempotencyKey:          "retry-capacity-first-settle",
			ProvenanceJSON:          `{"schema_version":1}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if reserved.ProviderCallSlots != 3 || settled.ProviderCallSlots != 1 {
		t.Fatalf(
			"provider slots before/after settlement = %d/%d, want 3/1",
			reserved.ProviderCallSlots, settled.ProviderCallSlots,
		)
	}

	secondTokenBound := domain.TokenCount(50)
	_, second, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "retry-capacity-second", BudgetID: budgetID,
			ExpectedRevision: settled.Revision,
			OperationID:      "retry-operation-second",
			Category:         BudgetCostModel, ProviderCallSlots: 2,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 1, Currency: usd,
			},
			TokenBound:     &secondTokenBound,
			IdempotencyKey: "retry-capacity-second",
			ProvenanceJSON: `{"schema_version":1}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if second.ProviderCallSlots != 3 {
		t.Fatalf("provider slots after second reserve = %d, want 3", second.ProviderCallSlots)
	}
	_, _, err = repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "retry-capacity-over", BudgetID: budgetID,
			ExpectedRevision: second.Revision,
			OperationID:      "retry-operation-over",
			Category:         BudgetCostModel, ProviderCallSlots: 1,
			CostBound: ExactMinorCost{
				Numerator: 0, Denominator: 1, Currency: usd,
			},
			TokenBound:     &secondTokenBound,
			IdempotencyKey: "retry-capacity-over",
			ProvenanceJSON: `{"schema_version":1}`,
		},
	)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("provider-call cap error = %v", err)
	}
	snapshot, err := repositories.GetBudgetSnapshot(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ProviderCallSlots != 3 {
		t.Fatalf("final provider slots = %d, want 3", snapshot.ProviderCallSlots)
	}
}

func TestBudgetLedgerAllowsInFlightSettlementBeyondCapThenBlocks(t *testing.T) {
	ctx := context.Background()
	repositories, task, budgetID, usd := createBudgetLedgerFixture(t, 1, 100)
	reservation, _, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "reservation-in-flight", BudgetID: budgetID,
			ExpectedRevision: 0, OperationID: "request-overrun",
			AttemptID: stringPointer("attempt-overrun"), RetryOrdinal: 1,
			Category: BudgetCostModel,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 1, Currency: usd,
			},
			TokenBound:     tokenCountPointer(100),
			IdempotencyKey: "reserve-overrun",
			ProvenanceJSON: `{"schema_version":1,"pricing_known":true}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	actual := ExactMinorCost{Numerator: 3, Denominator: 2, Currency: usd}
	actualTokens := domain.TokenCount(120)
	snapshot, err := repositories.SettleProviderBudget(
		ctx,
		SettleProviderBudget{
			ID: "posting-overrun", ReservationID: reservation.ID,
			ActualCost: &actual, ActualTokens: &actualTokens,
			IdempotencyKey: "post-overrun",
			ProvenanceJSON: `{"schema_version":1,"overrun":"in-flight"}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.HardCapReached ||
		snapshot.ChargedCost.Numerator != 3 ||
		snapshot.ChargedCost.Denominator != 2 ||
		snapshot.RemainingCost == nil ||
		snapshot.RemainingCost.Numerator != 0 {
		t.Fatalf("overrun snapshot = %#v", snapshot)
	}
	_, _, err = repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "reservation-after-overrun", BudgetID: budgetID,
			ExpectedRevision: snapshot.Revision, OperationID: "request-after",
			AttemptID: stringPointer("attempt-after"), RetryOrdinal: 1,
			Category: BudgetCostModel,
			CostBound: ExactMinorCost{
				Numerator: 0, Denominator: 1, Currency: usd,
			},
			TokenBound:     tokenCountPointer(0),
			IdempotencyKey: "reserve-after-overrun",
			ProvenanceJSON: `{"schema_version":1,"pricing_known":true}`,
		},
	)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("post-cap reservation error = %v", err)
	}
	events := countBudgetTaskEvents(t, repositories, task.ID)
	if events < 4 {
		t.Fatalf("budget boundary task events = %d, want at least 4", events)
	}
}

func TestBudgetLedgerUnknownActualBlocksWithoutBecomingZero(t *testing.T) {
	ctx := context.Background()
	repositories, _, budgetID, usd := createBudgetLedgerFixture(t, 5, 500)
	reservation, _, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "reservation-unknown", BudgetID: budgetID,
			ExpectedRevision: 0, OperationID: "request-unknown",
			AttemptID: stringPointer("attempt-unknown"), RetryOrdinal: 1,
			Category: BudgetCostModel,
			CostBound: ExactMinorCost{
				Numerator: 2, Denominator: 1, Currency: usd,
			},
			TokenBound:     tokenCountPointer(200),
			IdempotencyKey: "reserve-unknown",
			ProvenanceJSON: `{"schema_version":1,"pricing_known":true}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repositories.SettleProviderBudget(
		ctx,
		SettleProviderBudget{
			ID: "posting-unknown", ReservationID: reservation.ID,
			IdempotencyKey: "post-unknown",
			ProvenanceJSON: `{"schema_version":1,"pricing":"unknown"}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.CostAccountingUnknown ||
		!snapshot.TokenAccountingUnknown ||
		snapshot.RemainingCost != nil ||
		snapshot.RemainingTokens != nil ||
		snapshot.ChargedCost.Numerator != 2 ||
		snapshot.ChargedTokens != 200 {
		t.Fatalf("unknown snapshot = %#v", snapshot)
	}
	_, _, err = repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "reservation-after-unknown", BudgetID: budgetID,
			ExpectedRevision: 2, OperationID: "request-after-unknown",
			AttemptID: stringPointer("attempt-after-unknown"), RetryOrdinal: 1,
			Category: BudgetCostModel,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 1, Currency: usd,
			},
			TokenBound:     tokenCountPointer(1),
			IdempotencyKey: "reserve-after-unknown",
			ProvenanceJSON: `{"schema_version":1,"pricing_known":true}`,
		},
	)
	if !errors.Is(err, ErrBudgetAccountingUnknown) {
		t.Fatalf("unknown-accounting reservation error = %v", err)
	}
}

func TestBudgetLedgerSettlementIgnoresUnrelatedRevisionChurn(t *testing.T) {
	ctx := context.Background()
	repositories, _, budgetID, usd := createBudgetLedgerFixture(t, 20, 20_000)
	owned, _, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "settlement-owned-reservation", BudgetID: budgetID,
			ExpectedRevision: 0, OperationID: "settlement-owned-operation",
			Category: BudgetCostModel, ProviderCallSlots: 1,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 1, Currency: usd,
			},
			TokenBound:     tokenCountPointer(100),
			IdempotencyKey: "settlement-owned-reservation",
			ProvenanceJSON: `{"schema_version":1,"owner":"request"}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	revision := uint64(1)
	for index := 0; index < 12; index++ {
		_, snapshot, err := repositories.ReserveProviderBudget(
			ctx,
			ReserveProviderBudget{
				ID:       fmt.Sprintf("revision-churn-%02d", index),
				BudgetID: budgetID, ExpectedRevision: revision,
				OperationID: fmt.Sprintf("revision-churn-%02d", index),
				Category:    BudgetCostTool,
				CostBound: ExactMinorCost{
					Numerator: 0, Denominator: 1, Currency: usd,
				},
				IdempotencyKey: fmt.Sprintf("revision-churn-%02d", index),
				ProvenanceJSON: `{"schema_version":1,"churn":true}`,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		revision = snapshot.Revision
	}
	actualCost := ExactMinorCost{
		Numerator: 3, Denominator: 4, Currency: usd,
	}
	actualTokens := domain.TokenCount(75)
	snapshot, err := repositories.SettleProviderBudget(
		ctx,
		SettleProviderBudget{
			ID: "settlement-after-churn", ReservationID: owned.ID,
			ActualCost: &actualCost, ActualTokens: &actualTokens,
			IdempotencyKey: "settlement-after-churn",
			ProvenanceJSON: `{"schema_version":1,"revision_churn":12}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != revision+1 ||
		snapshot.ActualKnownCost != actualCost ||
		snapshot.ActualTokens != actualTokens {
		t.Fatalf("revision-independent settlement = %#v", snapshot)
	}
}

func TestBudgetLedgerReconciliationIntentBlocksUntilExactSettlement(t *testing.T) {
	ctx := context.Background()
	repositories, _, budgetID, usd := createBudgetLedgerFixture(t, 5, 500)
	reservation, _, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "reconciliation-reservation", BudgetID: budgetID,
			ExpectedRevision: 0, OperationID: "reconciliation-operation",
			Category: BudgetCostModel, ProviderCallSlots: 2,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 1, Currency: usd,
			},
			TokenBound:     tokenCountPointer(100),
			IdempotencyKey: "reconciliation-reservation",
			ProvenanceJSON: `{"schema_version":1,"pricing_known":true}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	actualCost := ExactMinorCost{
		Numerator: 3, Denominator: 4, Currency: usd,
	}
	actualTokens := domain.TokenCount(80)
	intent := RecordBudgetReconciliationIntent{
		ID: "reconciliation-intent", ReservationID: reservation.ID,
		ActualCost: &actualCost, ActualTokens: &actualTokens,
		ActualProviderCallSlots: 1,
		ReasonRedacted:          "exact settlement could not be confirmed",
		IdempotencyKey:          "reconciliation-intent",
		ProvenanceJSON:          `{"schema_version":1,"source":"settlement-fallback"}`,
	}
	snapshot, err := repositories.RecordBudgetReconciliationIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.ReconciliationPending || !snapshot.HardCapReached ||
		snapshot.ReservedCost.Numerator != 0 ||
		snapshot.ChargedCost != actualCost ||
		snapshot.ActualKnownCost != actualCost ||
		snapshot.ActualTokens != actualTokens ||
		snapshot.ProviderCallSlots != 1 {
		t.Fatalf("pending reconciliation snapshot = %#v", snapshot)
	}
	idempotent, err := repositories.RecordBudgetReconciliationIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if idempotent.Revision != snapshot.Revision {
		t.Fatalf("idempotent reconciliation snapshot = %#v", idempotent)
	}
	_, _, err = repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "blocked-by-reconciliation", BudgetID: budgetID,
			ExpectedRevision: snapshot.Revision,
			OperationID:      "blocked-by-reconciliation",
			Category:         BudgetCostTool,
			CostBound: ExactMinorCost{
				Numerator: 0, Denominator: 1, Currency: usd,
			},
			IdempotencyKey: "blocked-by-reconciliation",
			ProvenanceJSON: `{"schema_version":1,"blocked":true}`,
		},
	)
	if !errors.Is(err, ErrBudgetReconciliationPending) {
		t.Fatalf("pending reconciliation reserve error = %v", err)
	}
	settled, err := repositories.SettleProviderBudget(
		ctx,
		SettleProviderBudget{
			ID: "reconciled-posting", ReservationID: reservation.ID,
			ActualCost: &actualCost, ActualTokens: &actualTokens,
			ActualProviderCallSlots: 1,
			IdempotencyKey:          "reconciled-posting",
			ProvenanceJSON:          `{"schema_version":1,"source":"reconciler"}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if settled.ReconciliationPending ||
		settled.ActualKnownCost != actualCost ||
		settled.ActualTokens != actualTokens ||
		settled.ProviderCallSlots != 1 {
		t.Fatalf("reconciled snapshot = %#v", settled)
	}
}

func TestBudgetLedgerConcurrentReservationsCannotOverspend(t *testing.T) {
	ctx := context.Background()
	repositories, task, budgetID, usd := createBudgetLedgerFixture(t, 1, 100)
	var successes atomic.Int64
	unexpected := make(chan error, 18)
	var wait sync.WaitGroup
	for index := 0; index < 18; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			for retry := 0; retry < 30; retry++ {
				snapshot, err := repositories.GetBudgetSnapshot(ctx, task.ID)
				if err != nil {
					unexpected <- err
					return
				}
				_, _, err = repositories.ReserveProviderBudget(
					ctx,
					ReserveProviderBudget{
						ID:               fmt.Sprintf("concurrent-reservation-%02d", index),
						BudgetID:         budgetID,
						ExpectedRevision: snapshot.Revision,
						OperationID:      fmt.Sprintf("concurrent-operation-%02d", index),
						AttemptID: stringPointer(
							fmt.Sprintf("concurrent-attempt-%02d", index),
						),
						RetryOrdinal: 1, Category: BudgetCostModel,
						CostBound: ExactMinorCost{
							Numerator: 1, Denominator: 3, Currency: usd,
						},
						TokenBound:     tokenCountPointer(1),
						IdempotencyKey: fmt.Sprintf("concurrent-key-%02d", index),
						ProvenanceJSON: `{"schema_version":1,"concurrent":true}`,
					},
				)
				switch {
				case err == nil:
					successes.Add(1)
					return
				case errors.Is(err, ErrStaleRevision), errors.Is(err, ErrBusy):
					continue
				case errors.Is(err, ErrBudgetExhausted):
					return
				default:
					unexpected <- err
					return
				}
			}
			unexpected <- errors.New("concurrent reservation retry limit reached")
		}(index)
	}
	wait.Wait()
	close(unexpected)
	for err := range unexpected {
		t.Fatal(err)
	}
	if successes.Load() != 3 {
		t.Fatalf("successful reservations = %d, want 3", successes.Load())
	}
	snapshot, err := repositories.GetBudgetSnapshot(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ReservedCost.Numerator != 1 ||
		snapshot.ReservedCost.Denominator != 1 ||
		!snapshot.HardCapReached {
		t.Fatalf("concurrent snapshot = %#v", snapshot)
	}
}

func TestBudgetLedgerMixedLegacyAndExactOperationsShareOneCap(t *testing.T) {
	ctx := context.Background()
	repositories, task, budgetID, usd := createBudgetLedgerFixture(t, 2, 1_000)
	_, exactSnapshot, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "mixed-exact-first", BudgetID: budgetID, ExpectedRevision: 0,
			OperationID: "mixed-exact-operation", Category: BudgetCostTool,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 3, Currency: usd,
			},
			IdempotencyKey: "mixed-exact-first",
			ProvenanceJSON: `{"schema_version":1,"api":"exact"}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := repositories.ReserveBudget(
		ctx,
		ReserveBudget{
			ID: budgetID, ExpectedRevision: exactSnapshot.Revision,
			Amount: domain.Money{Currency: usd, MinorUnits: 1},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Revision != 2 {
		t.Fatalf("legacy revision = %d, want 2", legacy.Revision)
	}
	mixed, err := repositories.GetBudgetSnapshot(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mixed.ReservedCost.Numerator != 4 ||
		mixed.ReservedCost.Denominator != 3 ||
		mixed.Revision != 2 {
		t.Fatalf("mixed exact-first snapshot = %#v", mixed)
	}
	legacyPosted, err := repositories.PostActualCost(
		ctx,
		PostActualCost{
			ID: budgetID, ExpectedRevision: 2,
			Actual:               domain.Money{Currency: usd, MinorUnits: 1},
			ReleaseReservedMinor: 1, Tokens: 10,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if legacyPosted.Revision != 3 {
		t.Fatalf("legacy posting revision = %d, want 3", legacyPosted.Revision)
	}
	mixed, err = repositories.GetBudgetSnapshot(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mixed.ReservedCost.Numerator != 1 ||
		mixed.ReservedCost.Denominator != 3 ||
		mixed.ChargedCost.Numerator != 1 ||
		mixed.ChargedCost.Denominator != 1 {
		t.Fatalf("mixed posted snapshot = %#v", mixed)
	}
	_, _, err = repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "mixed-exact-to-cap", BudgetID: budgetID,
			ExpectedRevision: mixed.Revision,
			OperationID:      "mixed-to-cap", Category: BudgetCostTool,
			CostBound: ExactMinorCost{
				Numerator: 2, Denominator: 3, Currency: usd,
			},
			IdempotencyKey: "mixed-exact-to-cap",
			ProvenanceJSON: `{"schema_version":1,"api":"exact"}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repositories.ReserveBudget(
		ctx,
		ReserveBudget{
			ID: budgetID, ExpectedRevision: 4,
			Amount: domain.Money{Currency: usd, MinorUnits: 1},
		},
	)
	if !errors.Is(err, ErrBudgetExhausted) &&
		!errors.Is(err, ErrConstraint) {
		t.Fatalf("legacy mixed over-cap error = %v", err)
	}
}

func TestBudgetLedgerMixedConcurrentAPIsCannotOverspend(t *testing.T) {
	ctx := context.Background()
	repositories, task, budgetID, usd := createBudgetLedgerFixture(t, 2, 1_000)
	_, err := repositories.ReserveBudget(
		ctx,
		ReserveBudget{
			ID: budgetID, ExpectedRevision: 0,
			Amount: domain.Money{Currency: usd, MinorUnits: 1},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "mixed-concurrent-base", BudgetID: budgetID,
			ExpectedRevision: 1, OperationID: "mixed-concurrent-base",
			Category: BudgetCostTool,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 2, Currency: usd,
			},
			IdempotencyKey: "mixed-concurrent-base",
			ProvenanceJSON: `{"schema_version":1,"api":"exact"}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, _, err := repositories.ReserveProviderBudget(
			ctx,
			ReserveProviderBudget{
				ID: "mixed-concurrent-exact", BudgetID: budgetID,
				ExpectedRevision: 2, OperationID: "mixed-concurrent-exact",
				Category: BudgetCostTool,
				CostBound: ExactMinorCost{
					Numerator: 1, Denominator: 2, Currency: usd,
				},
				IdempotencyKey: "mixed-concurrent-exact",
				ProvenanceJSON: `{"schema_version":1,"api":"exact"}`,
			},
		)
		results <- err
	}()
	go func() {
		<-start
		_, err := repositories.ReserveBudget(
			ctx,
			ReserveBudget{
				ID: budgetID, ExpectedRevision: 2,
				Amount: domain.Money{Currency: usd, MinorUnits: 1},
			},
		)
		results <- err
	}()
	close(start)
	firstErr, secondErr := <-results, <-results
	successes := 0
	for _, err := range []error{firstErr, secondErr} {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrStaleRevision) &&
			!errors.Is(err, ErrConstraint) &&
			!errors.Is(err, ErrBudgetExhausted) {
			t.Fatalf("mixed concurrent error = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf(
			"mixed concurrent successes = %d, want 1; errors = %v / %v",
			successes, firstErr, secondErr,
		)
	}
	snapshot, err := repositories.GetBudgetSnapshot(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	exposure, err := addExactCosts(snapshot.ReservedCost, snapshot.ChargedCost)
	if err != nil {
		t.Fatal(err)
	}
	if compareExactCosts(exposure, snapshot.HardCost) > 0 {
		t.Fatalf("mixed concurrent snapshot overspent = %#v", snapshot)
	}
}

func TestBudgetLedgerRaiseRequiresGrantedSameTaskApproval(t *testing.T) {
	ctx := context.Background()
	repositories, task, budgetID, usd := createBudgetLedgerFixture(t, 1, 100)
	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	warningCost := ExactMinorCost{
		Numerator: 1, Denominator: 1, Currency: usd,
	}
	hardCost := ExactMinorCost{
		Numerator: 3, Denominator: 2, Currency: usd,
	}
	approval, err := repositories.CreateApproval(
		ctx,
		CreateApproval{
			ID: approvalID, TaskID: task.ID,
			Scope: BudgetRaiseApprovalScope(
				budgetID, warningCost, hardCost, 100, 150,
			),
			RequestReason:  "test explicit budget increase",
			IdempotencyKey: "budget-raise-approval",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repositories.ResolveApproval(
		ctx,
		ResolveApproval{
			ID: approval.ID, ExpectedRevision: 0,
			To:               domain.ApprovalRequestStateGranted,
			ResolutionReason: "user explicitly approved exact new cap",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	raiseInput := RaiseProviderBudget{
		BudgetID: budgetID, ExpectedRevision: 0,
		WarningCost: warningCost, HardCost: hardCost,
		WarningTokens: 100, HardTokens: 150, ApprovalID: approval.ID,
		ActorKind: "user", ActorReference: "local-user",
		ReasonRedacted: "approve one bounded final request",
		IdempotencyKey: "raise-budget-once",
		ProvenanceJSON: `{"schema_version":1,"authority":"explicit"}`,
	}
	snapshot, err := repositories.RaiseProviderBudget(ctx, raiseInput)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 1 || snapshot.LimitRevision != 1 ||
		snapshot.HardCost.Numerator != 3 ||
		snapshot.HardCost.Denominator != 2 ||
		snapshot.HardTokens != 150 {
		t.Fatalf("raised snapshot = %#v", snapshot)
	}
	idempotent, err := repositories.RaiseProviderBudget(ctx, raiseInput)
	if err != nil {
		t.Fatal(err)
	}
	if idempotent.Revision != snapshot.Revision {
		t.Fatalf("idempotent raise snapshot = %#v", idempotent)
	}
	changedSameKey := raiseInput
	changedSameKey.HardCost = ExactMinorCost{
		Numerator: 2, Denominator: 1, Currency: usd,
	}
	if _, err := repositories.RaiseProviderBudget(
		ctx, changedSameKey,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed idempotent raise error = %v", err)
	}
	reusedApproval := raiseInput
	reusedApproval.ExpectedRevision = 1
	reusedApproval.HardCost = changedSameKey.HardCost
	reusedApproval.HardTokens = 200
	reusedApproval.IdempotencyKey = "reuse-budget-approval"
	if _, err := repositories.RaiseProviderBudget(
		ctx, reusedApproval,
	); !errors.Is(err, ErrBudgetApprovalRequired) {
		t.Fatalf("reused approval for different limits error = %v", err)
	}
	missingApprovalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	_, err = repositories.RaiseProviderBudget(
		ctx,
		RaiseProviderBudget{
			BudgetID: budgetID, ExpectedRevision: 1,
			WarningCost: snapshot.WarningCost,
			HardCost: ExactMinorCost{
				Numerator: 2, Denominator: 1, Currency: usd,
			},
			WarningTokens: 100, HardTokens: 200,
			ApprovalID: missingApprovalID, ActorKind: "user",
			ActorReference: "local-user",
			ReasonRedacted: "unapproved increase",
			IdempotencyKey: "raise-without-approval",
			ProvenanceJSON: `{"schema_version":1,"authority":"missing"}`,
		},
	)
	if !errors.Is(err, ErrBudgetApprovalRequired) {
		t.Fatalf("missing approval error = %v", err)
	}
	pendingID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	pendingHard := ExactMinorCost{
		Numerator: 2, Denominator: 1, Currency: usd,
	}
	pending, err := repositories.CreateApproval(
		ctx,
		CreateApproval{
			ID: pendingID, TaskID: task.ID,
			Scope: BudgetRaiseApprovalScope(
				budgetID, warningCost, pendingHard, 100, 200,
			),
			RequestReason:  "pending exact budget increase",
			IdempotencyKey: "pending-budget-raise",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repositories.RaiseProviderBudget(
		ctx,
		RaiseProviderBudget{
			BudgetID: budgetID, ExpectedRevision: 1,
			WarningCost: warningCost, HardCost: pendingHard,
			WarningTokens: 100, HardTokens: 200, ApprovalID: pending.ID,
			ActorKind: "user", ActorReference: "local-user",
			ReasonRedacted: "pending authority must not apply",
			IdempotencyKey: "raise-with-pending-approval",
			ProvenanceJSON: `{"schema_version":1,"authority":"pending"}`,
		},
	)
	if !errors.Is(err, ErrBudgetApprovalRequired) {
		t.Fatalf("pending approval error = %v", err)
	}
	otherTaskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	otherTask, err := repositories.CreateTask(
		ctx,
		CreateTask{
			ID: otherTaskID, ThreadID: task.ThreadID,
			RepositoryID:      task.RepositoryID,
			PolicyPreset:      task.PolicyPreset,
			ReasoningEffort:   task.ReasoningEffort,
			RiskLevel:         task.RiskLevel,
			RequiredAssurance: task.RequiredAssurance,
			IdempotencyKey:    "other-budget-task",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	otherBudgetID, err := domain.NewBudgetID()
	if err != nil {
		t.Fatal(err)
	}
	_, err = repositories.CreateBudget(
		ctx,
		CreateBudget{
			TaskID: otherTask.ID,
			Budget: domain.TaskBudget{
				ID:            otherBudgetID,
				WarningCost:   domain.Money{Currency: usd, MinorUnits: 0},
				HardStopCost:  domain.Money{Currency: usd, MinorUnits: 1},
				WarningTokens: 0, HardStopTokens: 100,
				WarningWallClock: 1, HardStopWallClock: 2,
				MaximumProviderCalls: 2, MaximumRepairRounds: 1,
				MaximumToolExecutions: 2,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	wrongBudget := raiseInput
	wrongBudget.BudgetID = otherBudgetID
	wrongBudget.ExpectedRevision = 0
	wrongBudget.IdempotencyKey = "wrong-task-budget-raise"
	if _, err := repositories.RaiseProviderBudget(
		ctx, wrongBudget,
	); !errors.Is(err, ErrBudgetApprovalRequired) {
		t.Fatalf("wrong task/budget approval error = %v", err)
	}
}

func TestBudgetLedgerRejectsMissingPricingAndSeparatesCategories(t *testing.T) {
	ctx := context.Background()
	repositories, _, budgetID, usd := createBudgetLedgerFixture(t, 10, 1_000)
	_, _, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "missing-pricing", BudgetID: budgetID, ExpectedRevision: 0,
			OperationID:    "missing-pricing-operation",
			Category:       BudgetCostInfrastructure,
			CostBound:      ExactMinorCost{},
			IdempotencyKey: "missing-pricing",
			ProvenanceJSON: `{"schema_version":1,"pricing_known":false}`,
		},
	)
	if err == nil {
		t.Fatal("missing pricing was treated as a zero-cost reservation")
	}
	_, callSnapshot, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "provider-call-envelope", BudgetID: budgetID,
			ExpectedRevision: 0, OperationID: "provider-call-envelope",
			Category: BudgetCostModel, ProviderCallSlots: 100,
			CostBound: ExactMinorCost{
				Numerator: 0, Denominator: 1, Currency: usd,
			},
			TokenBound:     tokenCountPointer(0),
			IdempotencyKey: "provider-call-envelope",
			ProvenanceJSON: `{"schema_version":1,"attempt_bound":100}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if callSnapshot.ProviderCallSlots != 100 {
		t.Fatalf("provider call slots = %d, want 100", callSnapshot.ProviderCallSlots)
	}
	_, _, err = repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "provider-call-overflow", BudgetID: budgetID,
			ExpectedRevision: callSnapshot.Revision,
			OperationID:      "provider-call-overflow", Category: BudgetCostModel,
			ProviderCallSlots: 1,
			CostBound: ExactMinorCost{
				Numerator: 0, Denominator: 1, Currency: usd,
			},
			TokenBound:     tokenCountPointer(0),
			IdempotencyKey: "provider-call-overflow",
			ProvenanceJSON: `{"schema_version":1,"attempt_bound":1}`,
		},
	)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("provider call hard-cap error = %v", err)
	}
	_, snapshot, err := repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "tool-reservation", BudgetID: budgetID,
			ExpectedRevision: callSnapshot.Revision,
			OperationID:      "tool-operation", Category: BudgetCostTool,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 4, Currency: usd,
			},
			IdempotencyKey: "tool-reservation",
			ProvenanceJSON: `{"schema_version":1,"pricing_known":true}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, snapshot, err = repositories.ReserveProviderBudget(
		ctx,
		ReserveProviderBudget{
			ID: "infrastructure-reservation", BudgetID: budgetID,
			ExpectedRevision: snapshot.Revision,
			OperationID:      "infrastructure-operation",
			Category:         BudgetCostInfrastructure,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 2, Currency: usd,
			},
			IdempotencyKey: "infrastructure-reservation",
			ProvenanceJSON: `{"schema_version":1,"pricing_known":true}`,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Categories[1].ReservedCost.Numerator != 1 ||
		snapshot.Categories[1].ReservedCost.Denominator != 4 ||
		snapshot.Categories[2].ReservedCost.Numerator != 1 ||
		snapshot.Categories[2].ReservedCost.Denominator != 2 {
		t.Fatalf("category snapshot = %#v", snapshot.Categories)
	}
}

func createBudgetLedgerFixture(
	t *testing.T,
	hardCost int64,
	hardTokens domain.TokenCount,
) (*Repositories, Task, domain.BudgetID, domain.CurrencyCode) {
	t.Helper()
	repositories, task := createTaskFixture(t, 12_000+int(hardTokens%1_000))
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	budgetID, err := domain.NewBudgetID()
	if err != nil {
		t.Fatal(err)
	}
	warningCost := hardCost / 2
	warningTokens := hardTokens / 2
	_, err = repositories.CreateBudget(
		context.Background(),
		CreateBudget{
			TaskID: task.ID,
			Budget: domain.TaskBudget{
				ID: budgetID,
				WarningCost: domain.Money{
					Currency: usd, MinorUnits: warningCost,
				},
				HardStopCost: domain.Money{
					Currency: usd, MinorUnits: hardCost,
				},
				WarningTokens: warningTokens, HardStopTokens: hardTokens,
				WarningWallClock: 10_000, HardStopWallClock: 20_000,
				MaximumProviderCalls: 100, MaximumRepairRounds: 5,
				MaximumToolExecutions: 100,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return repositories, task, budgetID, usd
}

func tokenCountPointer(value domain.TokenCount) *domain.TokenCount {
	return &value
}

func countBudgetTaskEvents(
	t *testing.T,
	repositories *Repositories,
	taskID domain.TaskID,
) int {
	t.Helper()
	var count int
	if err := repositories.database.sql.QueryRow(
		`SELECT count(*) FROM task_events
		 WHERE task_id = ? AND event_type LIKE 'budget.%'`,
		taskID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
