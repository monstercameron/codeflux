package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/policy"
)

func TestAdjustBudgetBeforeApprovalPersistsExactAttributedRevision(t *testing.T) {
	repositories, task := createTaskFixture(t, 3100)
	selected, err := policy.Select(policy.SelectionInput{
		BaselineModelRevision: "budget-adjustment-revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	budgetID := testBudgetID(t, 3110)
	initial, err := selected.BudgetDefaults.Materialize(budgetID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateBudget(t.Context(), CreateBudget{
		TaskID: task.ID,
		Budget: initial,
	}); err != nil {
		t.Fatal(err)
	}
	requested := initial
	requested.WarningCost.MinorUnits = 4_500
	requested.HardStopCost.MinorUnits = 6_000
	requested.WarningTokens = 900_000
	requested.HardStopTokens = 1_200_000
	requested.WarningWallClock = 6_000_000
	requested.HardStopWallClock = 8_000_000
	requested.MaximumProviderCalls = 20
	requested.MaximumRepairRounds = 4
	requested.MaximumToolExecutions = 240
	input := AdjustPreApprovalBudget{
		BudgetID: budgetID, ExpectedBudgetRevision: 0,
		ExpectedLimitRevision: 0, Requested: requested,
		Actor: "user:fixture", AuthorityReference: "choice:budget-3110",
		Reason:         "explicitly broadened task scope",
		IdempotencyKey: "preapproval-budget-3110",
	}
	adjustment, snapshot, err := repositories.AdjustBudgetBeforeApproval(
		t.Context(),
		input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if adjustment.Previous != initial ||
		adjustment.Adjusted != requested ||
		adjustment.Actor != input.Actor ||
		adjustment.AuthorityReference != input.AuthorityReference ||
		adjustment.Reason != input.Reason ||
		adjustment.PreviousLimitRevision != 0 ||
		adjustment.AdjustedLimitRevision != 1 {
		t.Fatalf("adjustment = %#v", adjustment)
	}
	if snapshot.Revision != 1 ||
		snapshot.LimitRevision != 1 ||
		snapshot.HardCost.Numerator != 6_000 ||
		snapshot.HardCost.Denominator != 1 ||
		snapshot.HardTokens != 1_200_000 {
		t.Fatalf("adjusted snapshot = %#v", snapshot)
	}
	var (
		authorityKind, actorKind, actorReference string
		approvalCount                            int
	)
	if err := repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT authority_kind, actor_kind, actor_reference,
		        CASE WHEN approval_id IS NULL THEN 0 ELSE 1 END
		 FROM budget_limit_revisions
		 WHERE budget_id = ? AND revision = ?`,
		budgetID,
		snapshot.LimitRevision,
	).Scan(
		&authorityKind,
		&actorKind,
		&actorReference,
		&approvalCount,
	); err != nil {
		t.Fatal(err)
	}
	if authorityKind != "preapproval-user-adjustment" ||
		actorKind != "user" ||
		actorReference != input.Actor ||
		approvalCount != 0 {
		t.Fatalf(
			"adjustment authority = %q, %q, %q, approval=%d",
			authorityKind,
			actorKind,
			actorReference,
			approvalCount,
		)
	}
	retried, retriedSnapshot, err := repositories.AdjustBudgetBeforeApproval(
		t.Context(),
		input,
	)
	if err != nil || retried != adjustment ||
		retriedSnapshot.Revision != snapshot.Revision {
		t.Fatalf("idempotent adjustment = %#v, %#v, %v", retried, retriedSnapshot, err)
	}

	task = transitionTaskFixtureToReady(t, repositories, task, 3120)
	tooLate := input
	tooLate.ExpectedBudgetRevision = snapshot.Revision
	tooLate.ExpectedLimitRevision = snapshot.LimitRevision
	tooLate.IdempotencyKey = "preapproval-budget-too-late"
	tooLate.Requested.HardStopTokens++
	if _, _, err := repositories.AdjustBudgetBeforeApproval(
		t.Context(),
		tooLate,
	); !errors.Is(err, policy.ErrBudgetAdjustmentTooLate) {
		t.Fatalf("post-approval adjustment error = %v", err)
	}
}

func TestAdjustBudgetBeforeApprovalRejectsExecutionExposure(t *testing.T) {
	repositories, task := createTaskFixture(t, 3200)
	selected, err := policy.Select(policy.SelectionInput{
		BaselineModelRevision: "budget-exposure-revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	budgetID := testBudgetID(t, 3210)
	initial, err := selected.BudgetDefaults.Materialize(budgetID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateBudget(t.Context(), CreateBudget{
		TaskID: task.ID,
		Budget: initial,
	}); err != nil {
		t.Fatal(err)
	}
	currency := initial.HardStopCost.Currency
	tokenBound := domain.TokenCount(1)
	if _, _, err := repositories.ReserveProviderBudget(
		t.Context(),
		ReserveProviderBudget{
			ID: "preapproval-exposure", BudgetID: budgetID,
			ExpectedRevision: 0, OperationID: "operation-exposure",
			RetryOrdinal: 1, Category: BudgetCostModel,
			ProviderCallSlots: 1,
			CostBound: ExactMinorCost{
				Numerator: 1, Denominator: 1, Currency: currency,
			},
			TokenBound:     &tokenBound,
			IdempotencyKey: "preapproval-exposure",
			ProvenanceJSON: `{"schema_version":1}`,
		},
	); err != nil {
		t.Fatal(err)
	}
	requested := initial
	requested.HardStopTokens++
	_, _, err = repositories.AdjustBudgetBeforeApproval(
		t.Context(),
		AdjustPreApprovalBudget{
			BudgetID: budgetID, ExpectedBudgetRevision: 1,
			ExpectedLimitRevision: 0, Requested: requested,
			Actor: "user:fixture", AuthorityReference: "choice:budget-3210",
			Reason:         "must not rewrite exposed budget",
			IdempotencyKey: "preapproval-exposed-adjustment",
		},
	)
	if !errors.Is(err, ErrConstraint) {
		t.Fatalf("execution exposure adjustment error = %v", err)
	}
}
