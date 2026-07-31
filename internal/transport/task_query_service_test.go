package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
)

func TestTaskServiceGetTaskMapsAuthoritativeView(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	threadID, _ := domain.NewThreadID()
	sessionID, _ := domain.NewSessionID()
	usd, _ := domain.ParseCurrencyCode("USD")
	actual, _ := domain.NewMoney(usd, 321)
	hard, _ := domain.NewMoney(usd, 10_000)
	tokens := domain.TokenCount(42)
	now := time.Date(2026, 7, 31, 15, 0, 0, 0, time.UTC)
	settling := false
	checkpointID, _ := domain.NewCheckpointID()
	queries := &taskQueryApplicationStub{view: TaskQueryView{
		TaskID: taskID, ThreadID: threadID, SessionID: sessionID,
		State: domain.TaskStateRunning, Revision: 7, PlanRevision: 3,
		SummaryRedacted: "safe task summary", SummaryOriginalBytes: 17,
		ActualCost: &actual, HardBudget: &hard, BudgetRevision: 5, ActualTokens: &tokens,
		SelectedProvider: "openai", SelectedModel: "gpt-5.6-sol", SelectedEffort: "maximum",
		Forecast: &TaskForecastQueryView{Range: domain.ForecastRange{
			LatencyKnown: true, LatencyP50Millis: 100, LatencyP90Millis: 200,
			TokensKnown: true, TokensP50: 40, TokensP90: 80,
		}, AlgorithmVersion: "transparent-heuristic-v1", EstimateNotice: "Estimate, not a promise.", Revision: 2},
		ActualPricingSnapshotIDs: []string{"prices-1"},
		RemainingHardBudget:      &hard, WarningThreshold: &actual, WarningReached: true,
		Elapsed: 4 * time.Minute, UpdatedAt: now,
		SettlingProviderRequest: &settling,
		LatestCheckpointID:      &checkpointID, LatestCheckpointState: domain.CheckpointStateReady,
		LatestCheckpointPlanStep: "step-2",
	}}
	service, err := NewTaskService(&taskControlApplicationStub{}, queries)
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := TaskIDToProto(taskID)
	response, err := service.GetTask(t.Context(), &codefluxv1.GetTaskRequest{TaskId: identity})
	if err != nil {
		t.Fatal(err)
	}
	task := response.GetTask()
	if queries.taskID != taskID || task.GetTaskId().GetValue() != taskID.String() ||
		task.GetThreadId().GetValue() != threadID.String() ||
		task.GetSessionId().GetValue() != sessionID.String() ||
		task.GetState() != string(domain.TaskStateRunning) || task.GetRevision() != 7 ||
		task.GetPlanRevision() != 3 || task.GetSummary().GetValue() != "safe task summary" ||
		task.GetActualCost().GetMinorUnits() != 321 || task.GetHardBudget().GetMinorUnits() != 10_000 ||
		task.GetBudgetRevision() != 5 ||
		task.GetActualTokens().GetTokens() != 42 || task.GetElapsed().AsDuration() != 4*time.Minute ||
		task.GetSelectedProvider() != "openai" || task.GetSelectedModel() != "gpt-5.6-sol" ||
		task.GetSelectedEffort() != "maximum" || task.GetForecast().GetTokensP90() != 80 ||
		len(task.GetActualPricingSnapshotIds()) != 1 || task.GetRemainingHardBudget() == nil ||
		task.GetWarningThreshold() == nil || !task.GetWarningReached() ||
		!task.GetUpdatedAt().AsTime().Equal(now) || task.SettlingProviderRequest == nil ||
		task.GetSettlingProviderRequest() || task.GetLatestCheckpointId().GetValue() != checkpointID.String() ||
		task.GetLatestCheckpointState() != "ready" || task.GetLatestCheckpointPlanStep() != "step-2" {
		t.Fatalf("task response = %#v", task)
	}
}

func TestTaskServiceGetTaskPreservesUnknownValuesAsAbsent(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	threadID, _ := domain.NewThreadID()
	now := time.Date(2026, 7, 31, 15, 0, 0, 0, time.UTC)
	service, err := NewTaskService(&taskControlApplicationStub{}, &taskQueryApplicationStub{view: TaskQueryView{
		TaskID: taskID, ThreadID: threadID, State: domain.TaskStateDraft,
		Revision: 1, UpdatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := TaskIDToProto(taskID)
	response, err := service.GetTask(t.Context(), &codefluxv1.GetTaskRequest{TaskId: identity})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetTask().ActualCost != nil || response.GetTask().HardBudget != nil ||
		response.GetTask().ActualTokens != nil || response.GetTask().Summary != nil ||
		response.GetTask().SessionId != nil || response.GetTask().Forecast != nil ||
		response.GetTask().RemainingHardBudget != nil || response.GetTask().WarningThreshold != nil ||
		len(response.GetTask().ActualPricingSnapshotIds) != 0 || response.GetTask().WarningReached ||
		response.GetTask().HardCapReached {
		t.Fatalf("unknown values were materialized: %#v", response.GetTask())
	}
	if response.GetTask().SettlingProviderRequest != nil || response.GetTask().LatestCheckpointId != nil ||
		response.GetTask().GetLatestCheckpointState() != "" || response.GetTask().GetLatestCheckpointPlanStep() != "" {
		t.Fatalf("unknown hard-cap facts were materialized: %#v", response.GetTask())
	}
}

func TestTaskServiceGetTaskMapsNotFoundSafely(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	identity, _ := TaskIDToProto(taskID)
	service, err := NewTaskService(&taskControlApplicationStub{}, &taskQueryApplicationStub{err: ErrTaskQueryNotFound})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.GetTask(t.Context(), &codefluxv1.GetTaskRequest{TaskId: identity})
	var applicationErr *ApplicationError
	if !errors.As(err, &applicationErr) || applicationErr.Code != codefluxv1.ErrorCode_ERROR_CODE_NOT_FOUND ||
		applicationErr.EntityID.GetValue() != taskID.String() {
		t.Fatalf("get task error = %#v", err)
	}
}

func TestTaskServiceConstructorRemainsControlOnlyCompatible(t *testing.T) {
	service, err := NewTaskService(&taskControlApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.GetTask(t.Context(), &codefluxv1.GetTaskRequest{})
	if err == nil {
		t.Fatal("control-only service unexpectedly served task queries")
	}
}

type taskQueryApplicationStub struct {
	taskID domain.TaskID
	view   TaskQueryView
	err    error
}

func (stub *taskQueryApplicationStub) GetTaskQuery(
	_ context.Context,
	taskID domain.TaskID,
) (TaskQueryView, error) {
	stub.taskID = taskID
	return stub.view, stub.err
}
