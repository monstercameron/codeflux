package storage

import (
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
)

func TestReadTaskServiceSnapshotReturnsOneAuthoritativeProjection(t *testing.T) {
	fixture := createAgentPlanFixture(t, 18_100)
	sessionID := testSessionID(t, 18_190)
	if _, err := fixture.repositories.CreateSession(t.Context(), CreateSession{
		ID: sessionID, ThreadID: fixture.task.ThreadID,
	}); err != nil {
		t.Fatal(err)
	}
	providerID, configuration, pricing := seedProviderRequestDependencies(t, fixture.repositories, 18_191)
	request := planAndStartProviderRequest(t, fixture.repositories, fixture.task.ID, providerID, configuration, pricing, 18_192)
	attempt, err := fixture.repositories.CreateProviderRequestAttempt(t.Context(), CreateProviderRequestAttempt{
		ID: "task-service-priced-attempt", LogicalRequestID: request.ID, AttemptNumber: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := fixture.repositories.GetBudgetSnapshot(t.Context(), fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	bound := ExactMinorCost{Currency: initial.HardCost.Currency, Numerator: 10, Denominator: 1}
	tokens := domain.TokenCount(7)
	reservation, _, err := fixture.repositories.ReserveProviderBudget(t.Context(), ReserveProviderBudget{
		ID: "task-service-priced-reservation", BudgetID: fixture.budgetID,
		ExpectedRevision: initial.Revision, OperationID: request.ID.String(), AttemptID: &attempt.ID,
		RetryOrdinal: 0, Category: BudgetCostModel, CostBound: bound, TokenBound: &tokens,
		IdempotencyKey: "task-service-priced-reservation", ProvenanceJSON: `{"schema_version":1}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.repositories.SettleProviderBudget(t.Context(), SettleProviderBudget{
		ID: "task-service-priced-posting", ReservationID: reservation.ID,
		ActualCost: &bound, ActualTokens: &tokens, IdempotencyKey: "task-service-priced-posting",
		ProvenanceJSON: `{"schema_version":1}`,
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := fixture.repositories.ReadTaskServiceSnapshot(t.Context(), fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Task.ID != fixture.task.ID || snapshot.Task.ThreadID != fixture.task.ThreadID ||
		snapshot.SessionID != sessionID || snapshot.PlanRevision != fixture.plan.Revision {
		t.Fatalf("task projection = %#v", snapshot)
	}
	if snapshot.SummaryRedacted != fixture.message.BodyRedacted || snapshot.SummaryTruncated ||
		snapshot.SummaryOriginalBytes != uint64(len(fixture.message.BodyRedacted)) {
		t.Fatalf("redacted summary = %q bytes=%d truncated=%t", snapshot.SummaryRedacted,
			snapshot.SummaryOriginalBytes, snapshot.SummaryTruncated)
	}
	if snapshot.Budget == nil || snapshot.Budget.TaskID != fixture.task.ID ||
		snapshot.Budget.BudgetID != fixture.budgetID || snapshot.Budget.HardCost.Denominator != 1 {
		t.Fatalf("budget projection = %#v", snapshot.Budget)
	}
	if snapshot.Policy == nil || snapshot.PolicyRevision != fixture.policyRevision ||
		snapshot.Policy.Model.Provider.Provider == "" || snapshot.Policy.Model.Model == "" ||
		snapshot.Forecast == nil || snapshot.ForecastRevision != fixture.forecastRevision ||
		snapshot.Forecast.EstimateNotice == "" || snapshot.Forecast.Latency.P90Millis < snapshot.Forecast.Latency.P50Millis {
		t.Fatalf("policy/forecast projection = policy %#v forecast %#v", snapshot.Policy, snapshot.Forecast)
	}
	if len(snapshot.ActualPricingSnapshotIDs) != 1 || snapshot.ActualPricingSnapshotIDs[0] != pricing.ID ||
		snapshot.Budget.ActualKnownCost != bound || snapshot.Budget.ActualTokens != tokens {
		t.Fatalf("actual usage pricing projection = ids %#v budget %#v", snapshot.ActualPricingSnapshotIDs, snapshot.Budget)
	}
	if snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.Location().String() != "UTC" {
		t.Fatalf("observed at = %s", snapshot.ObservedAt)
	}
	if snapshot.SettlingProviderRequest == nil || !*snapshot.SettlingProviderRequest {
		t.Fatalf("settling provider request = %#v, want known true", snapshot.SettlingProviderRequest)
	}
}

func TestReadTaskServiceSnapshotSupportsDraftWithoutOptionalAggregates(t *testing.T) {
	repositories, task := createTaskFixture(t, 18_200)
	snapshot, err := repositories.ReadTaskServiceSnapshot(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.SessionID.IsZero() || snapshot.PlanRevision != 0 ||
		snapshot.SummaryRedacted != "" || snapshot.Budget != nil || snapshot.Policy != nil || snapshot.Forecast != nil ||
		len(snapshot.ActualPricingSnapshotIDs) != 0 {
		t.Fatalf("optional draft projection = %#v", snapshot)
	}
	if snapshot.SettlingProviderRequest != nil || snapshot.LatestCheckpoint != nil {
		t.Fatalf("draft unknown hard-cap facts were materialized: %#v", snapshot)
	}
}

func TestReadTaskServiceSnapshotReturnsKnownFalseSettlementAndCheckpointContext(t *testing.T) {
	fixture := createBoundAgentRunFixture(t, 18_400)
	binding, err := fixture.repositories.CreateWorktreeBinding(t.Context(), CreateWorktreeBinding{
		WorkspaceID: testWorkspaceID(t, 18_401), TaskID: fixture.task.ID,
		RepositoryID: fixture.task.RepositoryID,
		BaseRevision: fixture.plan.RepositoryRevision, HeadRevision: fixture.plan.RepositoryRevision,
		BranchName: "codeflux/task/task-service-checkpoint", WorktreePath: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.RecordRunToolSchema(t.Context(), fixture.runID, executor.ToolSchemaVersion); err != nil {
		t.Fatal(err)
	}
	recordCheckpointRunConfiguration(t, fixture)
	runtimeState, err := fixture.repositories.ReadCheckpointRuntimeState(t.Context(), fixture.task.ID, fixture.runID)
	if err != nil {
		t.Fatal(err)
	}
	checkpointID, _ := domain.NewCheckpointID()
	canonical := canonicalCheckpointFixture(t, fixture, binding, runtimeState, strings.Repeat("9", 40))
	if _, _, err := fixture.repositories.CommitCheckpointAndEvent(t.Context(), atomicCheckpointFixture(
		checkpointID, fixture, binding, runtimeState, canonical, "task-service-hard-cap-checkpoint",
	)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := fixture.repositories.ReadTaskServiceSnapshot(t.Context(), fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SettlingProviderRequest == nil || *snapshot.SettlingProviderRequest {
		t.Fatalf("settling provider request = %#v, want known false", snapshot.SettlingProviderRequest)
	}
	wantPlanStep := taskServiceCheckpointPlanStep(canonical.Snapshot)
	if snapshot.LatestCheckpoint == nil || snapshot.LatestCheckpoint.ID != checkpointID ||
		snapshot.LatestCheckpoint.State != domain.CheckpointStateReady ||
		snapshot.LatestCheckpoint.PlanStep != wantPlanStep {
		t.Fatalf("latest checkpoint = %#v, want id=%s state=ready step=%q", snapshot.LatestCheckpoint, checkpointID, wantPlanStep)
	}
}

func TestReadTaskServiceSnapshotMapsMissingTask(t *testing.T) {
	repositories := openTestRepositories(t)
	_, err := repositories.ReadTaskServiceSnapshot(t.Context(), testTaskID(t, 18_300))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing task error = %v", err)
	}
	if _, err := repositories.ReadTaskServiceSnapshot(t.Context(), domain.TaskID{}); err == nil {
		t.Fatal("empty task identity was accepted")
	}
}
