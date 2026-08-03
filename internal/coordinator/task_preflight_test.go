package coordinator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/retrieval"
	"codeflux.dev/codeflux/internal/retrievalgate"
	"codeflux.dev/codeflux/internal/storage"
)

// mustOpenRetrievalRepositories opens a real, temporary, file-backed SQLite
// database and applies every migration, then builds the real
// retrieval.Service the pre-work retrieval gate requires (internal/retrieval
// .Service cannot be backed by a fake store: NewService only accepts a
// concrete *storage.Repositories). Returning the repositories too lets a
// test create fixture memory artifacts and independently verify what
// RunPreWorkGate durably recorded.
func mustOpenRetrievalRepositories(t *testing.T) (*storage.Repositories, *retrieval.Service) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "task-preflight-retrieval.sqlite")
	database, err := storage.Open(ctx, storage.OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := database.Close(closeCtx); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if _, err := database.Migrate(ctx, storage.MigrationOptions{
		ApplicationVersion: "task-preflight-retrieval-test",
		BackupDirectory:    filepath.Join(t.TempDir(), "backups"),
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	retrievalService, err := retrieval.NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	return repositories, retrievalService
}

func mustCreateForecastFixtureProjectAndRepository(t *testing.T, repositories *storage.Repositories) (domain.ProjectID, domain.RepositoryID) {
	t.Helper()
	ctx := context.Background()
	projectID, err := domain.NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateProject(ctx, storage.CreateProject{ID: projectID, Name: "Forecast fixture project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(ctx, storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: "/fixture/" + repositoryID.String(),
		GitIdentity:   "git-" + repositoryID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	return projectID, repositoryID
}

// mustCreateForecastFixtureTask persists a real thread and task row for
// taskID (RunPreWorkGate's memory_retrieval_queries.task_id column
// references tasks(id), so a retrieval call naming a task that was never
// durably created would otherwise fail its own foreign-key constraint --
// exactly as it would in production).
func mustCreateForecastFixtureTask(
	t *testing.T,
	repositories *storage.Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	taskID domain.TaskID,
) {
	t.Helper()
	ctx := context.Background()
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Forecast fixture thread",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateTask(ctx, storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "forecast-fixture-task-" + taskID.String(),
	}); err != nil {
		t.Fatal(err)
	}
}

func mustBuildForecastFingerprintInput(projectID domain.ProjectID, repositoryID domain.RepositoryID) fingerprint.ExactFingerprintInput {
	return fingerprint.ExactFingerprintInput{
		Project:           projectID,
		Repository:        repositoryID,
		BaseRevision:      domain.RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
		TaskClass:         fingerprint.TaskClassBugFix,
		Risk:              domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
	}
}

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
	retrievalRepositories, retrievalService := mustOpenRetrievalRepositories(t)
	projectID, repositoryID := mustCreateForecastFixtureProjectAndRepository(t, retrievalRepositories)
	mustCreateForecastFixtureTask(t, retrievalRepositories, projectID, repositoryID, taskID)
	service, err := NewTaskPreflightService(store, retrievalService)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(t.Context(), PrepareTaskPreflight{
		Forecast: TaskForecastInput{
			TaskID: taskID, BudgetID: budgetID,
			BaselineModelRevision: "model-revision-fixture",
			RepositoryRevision:    "repository-revision-fixture",
			Fingerprint:           mustBuildForecastFingerprintInput(projectID, repositoryID),
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
	// The pre-work retrieval gate ran (a fresh fixture project has nothing
	// to discover) and fell back cleanly; ordinary planning above still
	// completed regardless.
	if !prepared.Forecasted.Retrieval.FellBack || len(prepared.Forecasted.Retrieval.Eligible) != 0 {
		t.Fatalf("retrieval = %#v, want a clean fallback for a fixture project with no memory artifacts", prepared.Forecasted.Retrieval)
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
	// MEM-001 added one call ahead of the rest: ForecastTask now records the
	// task's exact fingerprint binding before selecting a policy.
	if len(store.calls) != 10 {
		t.Fatalf("lifecycle calls = %#v", store.calls)
	}
}

// TestForecastTaskConsultsMemoryBeforePlanningWhenFingerprintMatchesPriorWork
// proves the central coordinator-wiring outcome this lane exists for: a
// task whose exact fingerprint targets a repository with prior accepted
// project memory discovers and evaluates that memory eligible BEFORE
// ForecastTask selects a policy or generates an effort forecast; the
// discovery is durably logged, not merely returned in memory; and only a
// separate, later RecordMemoryInfluence call -- never ForecastTask itself --
// turns "eligible" into "influential."
func TestForecastTaskConsultsMemoryBeforePlanningWhenFingerprintMatchesPriorWork(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	budgetID, err := domain.NewBudgetID()
	if err != nil {
		t.Fatal(err)
	}
	eligibility, err := forecast.NewCounterfactualEligibility(true, []string{"fixed-policy-task"})
	if err != nil {
		t.Fatal(err)
	}

	retrievalRepositories, retrievalService := mustOpenRetrievalRepositories(t)
	projectID, repositoryID := mustCreateForecastFixtureProjectAndRepository(t, retrievalRepositories)
	mustCreateForecastFixtureTask(t, retrievalRepositories, projectID, repositoryID, taskID)

	// Prior accepted project memory bound to the same repository this
	// task's fingerprint targets.
	artifactID, err := domain.NewMemoryArtifactID()
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := domain.NewMemoryArtifactRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	content, err := domain.NewRepositoryFactMemoryContent(domain.RepositoryFactContent{
		Repository: repositoryID, Category: domain.RepositoryFactCategoryTestCommand,
		Statement: "go test ./... (prior accepted work)",
		Binding:   domain.RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retrievalRepositories.CreateMemoryArtifact(t.Context(), storage.CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: revisionID, ProjectID: projectID,
		Content: content, IdempotencyKey: revisionID.String(),
	}); err != nil {
		t.Fatal(err)
	}

	store := &taskPreflightStoreFixture{budget: storage.BudgetSnapshot{BudgetID: budgetID, TaskID: taskID}}
	service, err := NewTaskPreflightService(store, retrievalService)
	if err != nil {
		t.Fatal(err)
	}

	forecasted, err := service.ForecastTask(t.Context(), TaskForecastInput{
		TaskID: taskID, BudgetID: budgetID,
		BaselineModelRevision:    "model-revision-fixture",
		RepositoryRevision:       "repository-revision-fixture",
		Fingerprint:              mustBuildForecastFingerprintInput(projectID, repositoryID),
		TaskClass:                forecast.TaskClassFeature,
		RepositorySize:           forecast.RepositorySize{Files: 10, Bytes: 1_024},
		LikelyFiles:              []string{"internal/feature.go"},
		ValidationCommands:       []string{"go test ./..."},
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
		Eligibility:              eligibility,
		PolicyIdempotencyKey:     "policy-consult-fixture",
		ForecastIdempotencyKey:   "forecast-consult-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}

	if forecasted.Retrieval.FellBack || len(forecasted.Retrieval.Eligible) != 1 || forecasted.Retrieval.Eligible[0].RevisionID != revisionID {
		t.Fatalf("retrieval = %#v, want the prior accepted repository fact eligible and not a fallback", forecasted.Retrieval)
	}
	// Ordinary planning still ran -- memory informs, it never replaces
	// planning. The exact fingerprint binding (MEM-001) is recorded first,
	// ahead of policy selection, since it has nothing to do with retrieval
	// or planning order and everything to do with being available before
	// any run needs it.
	if len(store.calls) < 2 || store.calls[0] != "fingerprint-binding" || store.calls[1] != "policy" {
		t.Fatalf("store.calls = %#v, want the fingerprint binding recorded first and ordinary planning to have proceeded", store.calls)
	}

	// The discovery is durable, not merely an in-memory return value.
	channels, err := retrievalRepositories.ListMemoryRetrievalCandidateChannels(t.Context(), forecasted.Retrieval.Eligible[0].CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		t.Fatal("expected the matched candidate's discovery channel(s) to be durably recorded")
	}

	// The agent now uses the eligible item; only this call may turn
	// "eligible" into "influential."
	if err := service.RecordMemoryInfluence(
		t.Context(), forecasted.Retrieval.Eligible[0], retrievalgate.AgentInfluenceActionUsed, "reused the matching test command verbatim",
	); err != nil {
		t.Fatal(err)
	}
	decision, found, err := retrievalRepositories.GetMemoryRetrievalDecision(t.Context(), forecasted.Retrieval.Eligible[0].CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || decision.Decision != "accepted" {
		t.Fatalf("decision = %#v (found=%v), want accepted", decision, found)
	}
}

// TestForecastTaskFallsBackCleanlyAndStillPlansWhenNoEligibleMemoryExists
// proves the other required outcome: a task fingerprint with nothing
// eligible in memory falls back cleanly (never an error), durably records
// the fallback, and ordinary planning still proceeds to completion.
func TestForecastTaskFallsBackCleanlyAndStillPlansWhenNoEligibleMemoryExists(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	budgetID, err := domain.NewBudgetID()
	if err != nil {
		t.Fatal(err)
	}
	eligibility, err := forecast.NewCounterfactualEligibility(true, []string{"fixed-policy-task"})
	if err != nil {
		t.Fatal(err)
	}

	retrievalRepositories, retrievalService := mustOpenRetrievalRepositories(t)
	projectID, repositoryID := mustCreateForecastFixtureProjectAndRepository(t, retrievalRepositories)
	mustCreateForecastFixtureTask(t, retrievalRepositories, projectID, repositoryID, taskID)
	// Deliberately no memory artifacts created for this project.

	store := &taskPreflightStoreFixture{budget: storage.BudgetSnapshot{BudgetID: budgetID, TaskID: taskID}}
	service, err := NewTaskPreflightService(store, retrievalService)
	if err != nil {
		t.Fatal(err)
	}

	forecasted, err := service.ForecastTask(t.Context(), TaskForecastInput{
		TaskID: taskID, BudgetID: budgetID,
		BaselineModelRevision:    "model-revision-fixture",
		RepositoryRevision:       "repository-revision-fixture",
		Fingerprint:              mustBuildForecastFingerprintInput(projectID, repositoryID),
		TaskClass:                forecast.TaskClassFeature,
		RepositorySize:           forecast.RepositorySize{Files: 10, Bytes: 1_024},
		LikelyFiles:              []string{"internal/feature.go"},
		ValidationCommands:       []string{"go test ./..."},
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
		Eligibility:              eligibility,
		PolicyIdempotencyKey:     "policy-fallback-fixture",
		ForecastIdempotencyKey:   "forecast-fallback-fixture",
	})
	if err != nil {
		t.Fatalf("ForecastTask with nothing eligible must fall back cleanly, not error: %v", err)
	}
	if !forecasted.Retrieval.FellBack || len(forecasted.Retrieval.Eligible) != 0 {
		t.Fatalf("retrieval = %#v, want a clean fallback", forecasted.Retrieval)
	}

	// MEM-001: the fingerprint binding is recorded first, ahead of the rest.
	wantCalls := []string{"fingerprint-binding", "policy", "forecast", "create-budget", "get-budget"}
	if len(store.calls) != len(wantCalls) {
		t.Fatalf("store.calls = %#v, want ordinary planning to have run to completion: %v", store.calls, wantCalls)
	}
	for i, call := range wantCalls {
		if store.calls[i] != call {
			t.Fatalf("store.calls = %#v, want %v", store.calls, wantCalls)
		}
	}

	fallback, found, err := retrievalRepositories.GetMemoryRetrievalFallback(t.Context(), forecasted.Retrieval.QueryID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected the fallback to be durably recorded")
	}
	if fallback.CandidatesConsidered != forecasted.Retrieval.Considered {
		t.Fatalf("fallback = %#v, want CandidatesConsidered = %d", fallback, forecasted.Retrieval.Considered)
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

	fingerprintBindingInput storage.RecordTaskExactFingerprintBinding
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

func (store *taskPreflightStoreFixture) RecordTaskExactFingerprintBinding(
	_ context.Context,
	input storage.RecordTaskExactFingerprintBinding,
) (storage.TaskExactFingerprintBinding, error) {
	store.calls = append(store.calls, "fingerprint-binding")
	store.fingerprintBindingInput = input
	return storage.TaskExactFingerprintBinding{
		TaskID: input.TaskID, FingerprintSchemaVersion: input.FingerprintSchemaVersion,
		FingerprintHash: input.FingerprintHash,
	}, nil
}

// GetThread and CreateTask back IntakeTask (task_intake.go). The fixture
// records the call and returns deterministic values so preflight tests stay
// focused on policy/forecast/binding rather than task creation.
func (fixture *taskPreflightStoreFixture) GetThread(
	_ context.Context,
	threadID domain.ThreadID,
) (storage.Thread, error) {
	fixture.calls = append(fixture.calls, "GetThread")
	return storage.Thread{ID: threadID}, nil
}

func (fixture *taskPreflightStoreFixture) CreateTask(
	_ context.Context,
	input storage.CreateTask,
) (storage.Task, error) {
	fixture.calls = append(fixture.calls, "CreateTask")
	return storage.Task{ID: input.ID, ThreadID: input.ThreadID, RepositoryID: input.RepositoryID}, nil
}

func (fixture *taskPreflightStoreFixture) GetTask(
	_ context.Context,
	taskID domain.TaskID,
) (storage.Task, error) {
	fixture.calls = append(fixture.calls, "GetTask")
	return storage.Task{ID: taskID, Revision: 3}, nil
}
