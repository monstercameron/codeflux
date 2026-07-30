package coordinator

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/storage"
)

func TestTaskPreflightServiceExposesPrepareStartAndOutcomeLifecycle(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	budgetID, err := domain.NewBudgetID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	eligibility, err := forecast.NewCounterfactualEligibility(
		true,
		[]string{"fixed-policy-task"},
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &taskPreflightStoreFixture{
		budget: storage.BudgetSnapshot{
			BudgetID: budgetID, TaskID: taskID,
			Revision: 0, LimitRevision: 0,
		},
		preflight: storage.ExecutionPreflight{
			TaskID: taskID, Revision: 1,
			PresentationJSON: `{"notice":"Estimate, not a promise."}`,
		},
		presentation: storage.TaskExecutionPresentation{
			TaskID: taskID, PreflightRevision: 1,
			BudgetSnapshotRevision: 2,
			PresentationJSON:       `{"budget":{"snapshot_revision":2}}`,
		},
		started: storage.StartedTaskRun{RunID: runID, TaskID: taskID},
		outcome: storage.ForecastOutcome{RunID: runID, TaskID: taskID},
	}
	service, err := NewTaskPreflightService(store)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(t.Context(), PrepareTaskPreflight{
		Forecast: TaskForecastInput{
			TaskID: taskID, BudgetID: budgetID,
			BaselineModelRevision: "model-revision-fixture",
			RepositoryRevision:    "repository-revision-fixture",
			TaskFingerprint:       "task-fingerprint-fixture",
			TaskClass:             forecast.TaskClassFeature,
			RepositorySize: forecast.RepositorySize{
				Files: 10, Bytes: 1_024,
			},
			LikelyFiles:              []string{"internal/feature.go"},
			ValidationCommands:       []string{"go test ./..."},
			ToolConfigurationVersion: "tools-v1",
			ValidationProfileVersion: "validation-v1",
			Eligibility:              eligibility,
			PolicyIdempotencyKey:     "policy-fixture",
			ForecastIdempotencyKey:   "forecast-fixture",
		},
		ExpectedTaskRevision:    3,
		PreflightIdempotencyKey: "preflight-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Preflight.PresentationJSON == "" ||
		store.policyInput.Policy.Model.Revision != "model-revision-fixture" ||
		store.forecastInput.Forecast.Bindings.PolicyDigest == "" ||
		store.budgetInput.Budget.ID != budgetID ||
		store.preflightInput.ExpectedTaskRevision != 3 {
		t.Fatalf("prepared lifecycle = %#v, store = %#v", prepared, store)
	}
	presentation, err := service.Presentation(t.Context(), taskID, 1)
	if err != nil || presentation.BudgetSnapshotRevision != 2 {
		t.Fatalf("presentation = %#v, %v", presentation, err)
	}
	started, err := service.Start(t.Context(), storage.StartPreparedTaskRun{
		RunID: runID, EventID: eventID, TaskID: taskID,
		PreflightRevision: 1, ExpectedTaskRevision: 3, Attempt: 1,
		IdempotencyKey:      "start-fixture",
		EventIdempotencyKey: "start-event-fixture",
	})
	if err != nil || started.RunID != runID {
		t.Fatalf("started = %#v, %v", started, err)
	}
	outcome, err := service.RecordOutcome(
		t.Context(),
		storage.RecordForecastOutcome{
			RunID: runID, TaskID: taskID,
			Actual:         forecast.ActualResult{Accepted: true},
			IdempotencyKey: "outcome-fixture",
		},
	)
	if err != nil || outcome.RunID != runID {
		t.Fatalf("outcome = %#v, %v", outcome, err)
	}
	if len(store.calls) != 9 {
		t.Fatalf("lifecycle calls = %#v", store.calls)
	}
}

type taskPreflightStoreFixture struct {
	calls          []string
	policyInput    storage.RecordExecutionPolicy
	forecastInput  storage.RecordEffortForecast
	budgetInput    storage.CreateBudget
	preflightInput storage.PrepareTaskExecution
	budget         storage.BudgetSnapshot
	preflight      storage.ExecutionPreflight
	presentation   storage.TaskExecutionPresentation
	started        storage.StartedTaskRun
	outcome        storage.ForecastOutcome
}

func (store *taskPreflightStoreFixture) RecordExecutionPolicy(
	_ context.Context,
	input storage.RecordExecutionPolicy,
) (storage.ExecutionPolicyRevision, error) {
	store.calls = append(store.calls, "policy")
	store.policyInput = input
	return storage.ExecutionPolicyRevision{
		TaskID: input.TaskID, Revision: 1,
	}, nil
}

func (store *taskPreflightStoreFixture) RecordEffortForecast(
	_ context.Context,
	input storage.RecordEffortForecast,
) (storage.EffortForecastRevision, error) {
	store.calls = append(store.calls, "forecast")
	store.forecastInput = input
	return storage.EffortForecastRevision{
		TaskID: input.TaskID, Revision: 1, PolicyRevision: input.PolicyRevision,
	}, nil
}

func (store *taskPreflightStoreFixture) CreateBudget(
	_ context.Context,
	input storage.CreateBudget,
) (storage.BudgetAccount, error) {
	store.calls = append(store.calls, "create-budget")
	store.budgetInput = input
	return storage.BudgetAccount{
		TaskID: input.TaskID, Budget: input.Budget,
	}, nil
}

func (store *taskPreflightStoreFixture) GetBudgetSnapshot(
	_ context.Context,
	_ domain.TaskID,
) (storage.BudgetSnapshot, error) {
	store.calls = append(store.calls, "get-budget")
	return store.budget, nil
}

func (store *taskPreflightStoreFixture) AdjustBudgetBeforeApproval(
	_ context.Context,
	_ storage.AdjustPreApprovalBudget,
) (storage.PreApprovalBudgetAdjustment, storage.BudgetSnapshot, error) {
	store.calls = append(store.calls, "adjust-budget")
	return storage.PreApprovalBudgetAdjustment{}, store.budget, nil
}

func (store *taskPreflightStoreFixture) PrepareTaskExecution(
	_ context.Context,
	input storage.PrepareTaskExecution,
) (storage.ExecutionPreflight, error) {
	store.calls = append(store.calls, "preflight")
	store.preflightInput = input
	return store.preflight, nil
}

func (store *taskPreflightStoreFixture) GetTaskExecutionPresentation(
	_ context.Context,
	_ domain.TaskID,
	_ uint64,
) (storage.TaskExecutionPresentation, error) {
	store.calls = append(store.calls, "presentation")
	return store.presentation, nil
}

func (store *taskPreflightStoreFixture) StartPreparedTaskRun(
	_ context.Context,
	_ storage.StartPreparedTaskRun,
) (storage.StartedTaskRun, error) {
	store.calls = append(store.calls, "start")
	return store.started, nil
}

func (store *taskPreflightStoreFixture) RecordForecastOutcome(
	_ context.Context,
	_ storage.RecordForecastOutcome,
) (storage.ForecastOutcome, error) {
	store.calls = append(store.calls, "outcome")
	return store.outcome, nil
}
