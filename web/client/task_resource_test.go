package main

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/coordinator"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const taskResourceFixtureUUID = "01890f3c-4a00-7abc-8def-0123456789ab"

func TestDecodeTaskControlPropsUsesProjectionForCorrectnessAndTaskViewForMetadata(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	view := taskResourceFixtureView(scope)
	view.SelectedProvider = "openai"
	view.SelectedModel = "gpt-5.6-sol"
	view.SelectedEffort = "maximum"
	settling := true
	view.SettlingProviderRequest = &settling
	checkpointID, _ := domain.NewCheckpointID()
	view.LatestCheckpointId = &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT,
		Value: checkpointID.String(),
	}
	view.LatestCheckpointState = string(domain.CheckpointStateReady)
	view.LatestCheckpointPlanStep = "step-2"
	view.Forecast = &codefluxv1.TaskForecastView{
		AlgorithmVersion: "transparent-heuristic-v1", EstimateNotice: "Estimate, not a promise.",
		LatencyKnown: true, LatencyP50Ms: 1200, LatencyP90Ms: 2400,
		TokensKnown: true, TokensP50: 400, TokensP90: 800,
		UncertaintyReasons: []string{"price-unavailable"}, Revision: 2,
	}
	projection := taskResourceFixtureProjection(scope)
	projection.Checkpoint = taskprojection.CheckpointProjection{
		Present: true, ID: checkpointID, TaskRevision: projection.Revision,
		PlanStep: "step-2", CreatedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC), Revision: 1,
	}
	props, err := decodeTaskControlProps(view, scope, frontendstate.SessionView{
		Bootstrap: frontendstate.BootstrapReady, Connection: frontendstate.ConnectionLive,
	}, projection)
	if err != nil {
		t.Fatal(err)
	}
	if props.TaskState != domain.TaskStateRunning || props.Phase != taskcontrols.PhaseEditing {
		t.Fatalf("state/phase = %q/%q", props.TaskState, props.Phase)
	}
	if props.Delivery.State != taskcontrols.DeliveryLive || !props.Delivery.SequenceCertain {
		t.Fatalf("delivery = %+v", props.Delivery)
	}
	if !props.Usage.Tokens.Known || props.Usage.Tokens.ProviderSpecific["authoritative task total"] != 123 {
		t.Fatalf("tokens = %+v", props.Usage.Tokens)
	}
	if !props.Usage.Cost.Known || props.Usage.Cost.Value.MinorUnits != 250 ||
		props.Usage.Cost.Value.Currency != domain.CurrencyCode("USD") {
		t.Fatalf("cost = %+v", props.Usage.Cost)
	}
	if !props.Budget.HardLimitKnown || props.Budget.HardLimit.MinorUnits != 1000 ||
		!props.BudgetRevisionKnown || props.BudgetRevision != 5 ||
		!props.Budget.RemainingKnown || props.Budget.Remaining.MinorUnits != 750 ||
		props.Budget.HardCapReached || props.Budget.SettlingInFlightKnown ||
		props.Budget.SettlingInFlight || !props.Budget.CheckpointKnown ||
		!strings.Contains(props.Budget.CheckpointedState, checkpointID.String()) ||
		!strings.Contains(props.Budget.CheckpointedState, "step-2") {
		t.Fatalf("budget = %+v", props.Budget)
	}
	if props.Selection.Provider != "openai" || props.Selection.Model != "gpt-5.6-sol" ||
		props.Selection.Effort != "maximum" || props.Forecast.Range.TokensP90 != 800 ||
		!strings.Contains(props.Forecast.Assumptions, "Estimate, not a promise.") ||
		props.Usage.Cost.PricingSnapshot != "prices-2026-07-31" {
		t.Fatalf("authoritative top-bar facts = selection=%+v forecast=%+v cost=%+v", props.Selection, props.Forecast, props.Usage.Cost)
	}
	if err := props.Validate(); err != nil {
		t.Fatalf("decoded props are invalid: %v", err)
	}
}

func TestTaskStopConfirmationIsLimitedToStatesWithNonObviousConsequences(t *testing.T) {
	for _, state := range []domain.TaskState{
		domain.TaskStateForecasting, domain.TaskStateRunning,
		domain.TaskStateAwaitingAuthority, domain.TaskStateValidating,
	} {
		if !taskStopRequiresConfirmation(state) {
			t.Errorf("state %s should require stop confirmation", state)
		}
	}
	for _, state := range []domain.TaskState{
		domain.TaskStateDraft, domain.TaskStatePaused, domain.TaskStateAwaitingPlanApproval,
		domain.TaskStateReady, domain.TaskStateAwaitingReview, domain.TaskStateRecoveryRequired,
		domain.TaskStateCompleted, domain.TaskStateFailed, domain.TaskStateCancelled,
		domain.TaskStateRolledBack,
	} {
		if taskStopRequiresConfirmation(state) {
			t.Errorf("state %s should not require stop confirmation", state)
		}
	}
}

func TestSQLiteGRPCTaskControlDecoderPreservesAuthoritativeTopBarFacts(t *testing.T) {
	ctx := t.Context()
	database, err := storage.Open(ctx, storage.OpenOptions{
		Path: filepath.Join(t.TempDir(), "task-controls.sqlite3"), MaximumConnections: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	if _, err = database.Migrate(ctx, storage.MigrationOptions{
		ApplicationVersion: "task-control-decoder-test", BackupDirectory: filepath.Join(t.TempDir(), "backups"),
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := storage.NewRepositories(database, func() time.Time {
		return time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	messageID, _ := domain.NewMessageID()
	taskID, _ := domain.NewTaskID()
	if _, err = repositories.CreateProject(ctx, storage.CreateProject{ID: projectID, Name: "Task controls"}); err != nil {
		t.Fatal(err)
	}
	if _, err = repositories.CreateRepository(ctx, storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID, CanonicalPath: t.TempDir(), GitIdentity: "task-controls-fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Top bar facts",
	}); err != nil {
		t.Fatal(err)
	}
	message, err := repositories.AppendMessage(ctx, storage.AppendMessage{
		ID: messageID, ThreadID: threadID, Role: storage.MessageRoleUser,
		BodyRedacted: "Show exact task facts.", IdempotencyKey: "top-bar-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repositories.CreateTask(ctx, storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID, RequestMessageID: &message.ID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "top-bar-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := policy.Select(policy.SelectionInput{BaselineModelRevision: "fixture-revision"})
	if err != nil {
		t.Fatal(err)
	}
	policyRevision, err := repositories.RecordExecutionPolicy(ctx, storage.RecordExecutionPolicy{
		TaskID: task.ID, Policy: selected, IdempotencyKey: "top-bar-policy",
	})
	if err != nil {
		t.Fatal(err)
	}
	estimate, err := forecast.Generate(forecast.Input{
		RepositoryRevision: "fixture-repository", TaskFingerprint: "fixture-task",
		TaskClass:      forecast.TaskClassSmallChange,
		RepositorySize: forecast.RepositorySize{Files: 12, Bytes: 4096},
		LikelyFiles:    []string{"internal/task.go"}, ValidationCommands: []string{"go test ./..."},
		Policy: selected, ToolConfigurationVersion: "tools-v1", ValidationProfileVersion: "validation-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	eligibility, err := forecast.NewCounterfactualEligibility(true, []string{"fixed-policy-task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repositories.RecordEffortForecast(ctx, storage.RecordEffortForecast{
		TaskID: task.ID, PolicyRevision: policyRevision.Revision, Forecast: estimate,
		Eligibility: eligibility, IdempotencyKey: "top-bar-forecast",
	}); err != nil {
		t.Fatal(err)
	}
	budgetID, _ := domain.NewBudgetID()
	budget, err := selected.BudgetDefaults.Materialize(budgetID)
	if err != nil {
		t.Fatal(err)
	}
	budgetAccount, err := repositories.CreateBudget(ctx, storage.CreateBudget{TaskID: task.ID, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	checkpointID, _ := domain.NewCheckpointID()
	if _, err = repositories.CreateCheckpoint(ctx, storage.CreateCheckpoint{
		ID: checkpointID, TaskID: task.ID, State: domain.CheckpointStateReady,
		RepositoryRevision: "fixture-checkpoint", WorktreeDiffHash: strings.Repeat("7", 64),
		EventSequence: 1, IdempotencyKey: "top-bar-checkpoint",
	}); err != nil {
		t.Fatal(err)
	}

	query, err := coordinator.NewTaskQueryService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	service, err := transport.NewTaskService(taskControlQueryTestStub{}, query)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	codefluxv1.RegisterTaskServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///task-controls", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	scope := taskResourceScope{taskID: task.ID, threadID: threadID}
	projection := taskprojection.TaskProjection{
		TaskID: task.ID, State: task.State, Revision: task.Revision,
		Budget: taskprojection.BudgetProjection{
			Present: true, Revision: budgetAccount.Revision, HardLimit: budget.HardStopCost,
			Reserved: budgetAccount.ReservedCost, Actual: budgetAccount.ActualCost,
		},
		Recovery: taskprojection.RecoveryNone,
	}
	props, err := loadTaskControlProps(ctx, func(context.Context) (taskViewLease, error) {
		return taskViewLease{client: codefluxv1.NewTaskServiceClient(connection), close: func() error { return nil }}, nil
	}, scope, frontendstate.SessionView{Bootstrap: frontendstate.BootstrapReady, Connection: frontendstate.ConnectionLive}, projection)
	if err != nil {
		t.Fatal(err)
	}
	if props.Selection.Provider != policy.FixedBaselineProvider || props.Selection.Model != policy.FixedBaselineModel ||
		props.Selection.Effort != string(domain.ReasoningEffortMaximum) ||
		props.Forecast.Range.LatencyP50Millis != estimate.Latency.P50Millis ||
		props.Forecast.Range.LatencyP90Millis != estimate.Latency.P90Millis ||
		props.Forecast.Range.TokensP50 != estimate.Tokens.P50 || props.Forecast.Range.TokensP90 != estimate.Tokens.P90 ||
		!props.Usage.Tokens.Known || props.Usage.Tokens.ProviderSpecific["authoritative task total"] != 0 ||
		!props.Usage.Cost.Known || props.Usage.Cost.Value != budgetAccount.ActualCost || !props.Budget.RemainingKnown ||
		props.Budget.WarningThresholdKnown || props.Budget.Remaining != budget.HardStopCost ||
		props.Budget.WarningReached || props.Budget.HardCapReached ||
		props.Budget.SettlingInFlightKnown || props.Budget.SettlingInFlight || props.Budget.CheckpointKnown {
		t.Fatalf("decoded SQLite task controls = %+v", props)
	}
	markup, err := ui.RenderToString(taskcontrols.TaskControlPanel(props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{policy.FixedBaselineProvider, policy.FixedBaselineModel,
		forecast.EstimateNotice, forecast.AlgorithmVersion, "Estimated P50", "Remaining hard budget", "Unknown"} {
		if !strings.Contains(markup, want) {
			t.Errorf("mounted task controls omitted %q: %s", want, markup)
		}
	}
}

func TestSQLiteGRPCDecoderDoesNotLetTaskViewSettlementMetadataMutateProjectedBudget(t *testing.T) {
	t.Run("known true", func(t *testing.T) {
		repositories := openTaskResourceRepositories(t)
		model := providers.ModelIdentity{
			Provider: providers.ProviderIdentity{
				Adapter: "openai-responses", AdapterVersion: "adapter-v1",
				Provider: "openai", ProviderVersion: "responses-api-v1",
			},
			Model: "fixture-model", Revision: "fixture-model-v1",
		}
		selected, err := policy.Select(policy.SelectionInput{
			BaselineModelRevision: model.Revision,
			Override: &policy.ManualOverride{Model: model, Reasoning: domain.ReasoningEffortMaximum,
				Actor: "test", AuthorityReference: "hard-cap-settlement", Reason: "test fixture"},
		})
		if err != nil {
			t.Fatal(err)
		}
		estimate, err := forecast.Generate(forecast.Input{
			RepositoryRevision: "git-hard-cap", TaskFingerprint: strings.Repeat("9", 64),
			TaskClass: forecast.TaskClassSmallChange, Policy: selected,
			ToolConfigurationVersion: "tools-v1", ValidationProfileVersion: "validation-v1",
		})
		if err != nil {
			t.Fatal(err)
		}
		eligibility, _ := forecast.NewCounterfactualEligibility(false, []string{"test-fixture"})
		budgetID, _ := domain.NewBudgetID()
		budget, err := selected.BudgetDefaults.Materialize(budgetID)
		if err != nil {
			t.Fatal(err)
		}
		smoke, err := repositories.PrepareLiveProviderSmokeRequest(t.Context(), storage.PrepareLiveProviderSmokeRequest{
			IdempotencyKey: "hard-cap-settling-true", RepositoryPath: filepath.Join(t.TempDir(), "repository"),
			RepositoryGitIdentity: "git-hard-cap", ProviderType: "openai", ProviderDisplayName: "OpenAI",
			AdapterName: "openai-responses", AdapterVersion: "adapter-v1", ProviderVersion: "responses-api-v1",
			EndpointRedacted: "https://api.example.invalid/v1/responses", CapabilitiesJSON: `{"streaming":true}`,
			OpaqueCredentialReference: "os://openai/hard-cap", ModelIdentifier: model.Model,
			ModelVersion: model.Revision, RequestSHA256: strings.Repeat("9", 64),
			Policy: selected, Forecast: estimate, Eligibility: eligibility, Budget: budget,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repositories.TransitionProviderLogicalRequest(t.Context(), storage.TransitionProviderLogicalRequest{
			ID: smoke.Request.ID, ExpectedRevision: smoke.Request.Revision,
			From: storage.ProviderLogicalRequestPlanned, To: storage.ProviderLogicalRequestInFlight,
			AccountingStatus: storage.ProviderAccountingUnknown,
		}); err != nil {
			t.Fatal(err)
		}
		projection := taskprojection.TaskProjection{
			TaskID: smoke.TaskID, State: domain.TaskStateDraft, Revision: 1,
			Budget: taskprojection.BudgetProjection{
				Present: true, Revision: 1, HardLimit: budget.HardStopCost,
				Reserved: domain.Money{Currency: budget.HardStopCost.Currency},
				Actual:   domain.Money{Currency: budget.HardStopCost.Currency},
			},
			Recovery: taskprojection.RecoveryNone,
		}
		props := loadTaskResourcePropsThroughGRPC(t, repositories, smoke.TaskID, smoke.ThreadID, projection)
		if props.Budget.SettlingInFlightKnown || props.Budget.SettlingInFlight {
			t.Fatalf("TaskView settlement metadata mutated projected budget: %+v", props.Budget)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		repositories := openTaskResourceRepositories(t)
		projectID, _ := domain.NewProjectID()
		repositoryID, _ := domain.NewRepositoryID()
		threadID, _ := domain.NewThreadID()
		taskID, _ := domain.NewTaskID()
		if _, err := repositories.CreateProject(t.Context(), storage.CreateProject{ID: projectID, Name: "Unknown hard cap"}); err != nil {
			t.Fatal(err)
		}
		if _, err := repositories.CreateRepository(t.Context(), storage.CreateRepository{
			ID: repositoryID, ProjectID: projectID, CanonicalPath: t.TempDir(), GitIdentity: "unknown-hard-cap",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := repositories.CreateThread(t.Context(), storage.CreateThread{
			ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Unknown",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := repositories.CreateTask(t.Context(), storage.CreateTask{
			ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
			PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
			RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
			IdempotencyKey: "unknown-hard-cap-task",
		}); err != nil {
			t.Fatal(err)
		}
		projection := taskprojection.TaskProjection{
			TaskID: taskID, State: domain.TaskStateDraft, Revision: 1, Recovery: taskprojection.RecoveryNone,
		}
		props := loadTaskResourcePropsThroughGRPC(t, repositories, taskID, threadID, projection)
		if props.Budget.SettlingInFlightKnown || props.Budget.SettlingInFlight || props.Budget.CheckpointKnown {
			t.Fatalf("unknown facts became defaults: %+v", props.Budget)
		}
	})
}

func openTaskResourceRepositories(t *testing.T) *storage.Repositories {
	t.Helper()
	database, err := storage.Open(t.Context(), storage.OpenOptions{
		Path: filepath.Join(t.TempDir(), "task-resource.sqlite3"), MaximumConnections: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	if _, err := database.Migrate(t.Context(), storage.MigrationOptions{
		ApplicationVersion: "task-resource-hard-cap-test", BackupDirectory: filepath.Join(t.TempDir(), "backups"),
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := storage.NewRepositories(database, func() time.Time {
		return time.Date(2026, 7, 31, 18, 30, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return repositories
}

func loadTaskResourcePropsThroughGRPC(
	t *testing.T,
	repositories *storage.Repositories,
	taskID domain.TaskID,
	threadID domain.ThreadID,
	projection taskprojection.TaskProjection,
) taskcontrols.Props {
	t.Helper()
	query, err := coordinator.NewTaskQueryService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	service, err := transport.NewTaskService(taskControlQueryTestStub{}, query)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	codefluxv1.RegisterTaskServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///hard-cap-task",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	props, err := loadTaskControlProps(t.Context(), func(context.Context) (taskViewLease, error) {
		return taskViewLease{client: codefluxv1.NewTaskServiceClient(connection), close: func() error { return nil }}, nil
	}, taskResourceScope{taskID: taskID, threadID: threadID}, frontendstate.SessionView{
		Bootstrap: frontendstate.BootstrapReady, Connection: frontendstate.ConnectionLive,
	}, projection)
	if err != nil {
		t.Fatal(err)
	}
	return props
}

type taskControlQueryTestStub struct{}

func (taskControlQueryTestStub) PauseTaskControl(context.Context, transport.TaskControlCommand) (transport.TaskControlView, error) {
	return transport.TaskControlView{}, errors.New("not used")
}
func (taskControlQueryTestStub) ResumeTaskControl(context.Context, transport.TaskControlCommand) (transport.TaskControlView, error) {
	return transport.TaskControlView{}, errors.New("not used")
}
func (taskControlQueryTestStub) CancelTaskControl(context.Context, transport.TaskControlCommand) (transport.TaskControlView, error) {
	return transport.TaskControlView{}, errors.New("not used")
}

func TestDecodeTaskControlPropsKeepsAbsentAndKnownZeroDistinct(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	unknown := taskResourceFixtureView(scope)
	unknown.ActualCost = nil
	unknown.HardBudget = nil
	unknown.RemainingHardBudget = nil
	unknown.WarningThreshold = nil
	unknown.ActualTokens = nil
	unknownProjection := taskResourceFixtureProjection(scope)
	unknownProjection.Budget = taskprojection.BudgetProjection{}
	props, err := decodeTaskControlProps(unknown, scope, frontendstate.SessionView{Connection: frontendstate.ConnectionLive}, unknownProjection)
	if err != nil {
		t.Fatal(err)
	}
	if props.Usage.Cost.Known || props.Usage.Tokens.Known || props.Budget.HardLimitKnown || props.Budget.RemainingKnown ||
		props.Budget.SettlingInFlightKnown || props.Budget.CheckpointKnown {
		t.Fatalf("absent facts were represented as known zero: %+v", props)
	}
	markup, err := ui.RenderToString(taskcontrols.TaskControlPanel(props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Unknown") || strings.Contains(markup, "USD 0 minor units") {
		t.Fatalf("unknown values were not rendered honestly: %s", markup)
	}

	knownZero := taskResourceFixtureView(scope)
	knownZero.ActualCost.MinorUnits = 0
	knownZero.ActualTokens.Tokens = 0
	knownZero.RemainingHardBudget.MinorUnits = 0
	knownZero.WarningThreshold.MinorUnits = 0
	knownZero.WarningReached = true
	knownZero.HardCapReached = true
	settlingFalse := false
	knownZero.SettlingProviderRequest = &settlingFalse
	knownZeroProjection := taskResourceFixtureProjection(scope)
	knownZeroProjection.Budget.Reserved.MinorUnits = 1000
	knownZeroProjection.Budget.Actual.MinorUnits = 0
	props, err = decodeTaskControlProps(knownZero, scope, frontendstate.SessionView{Connection: frontendstate.ConnectionLive}, knownZeroProjection)
	if err != nil {
		t.Fatal(err)
	}
	if !props.Usage.Cost.Known || props.Usage.Cost.Value.MinorUnits != 0 || !props.Usage.Tokens.Known ||
		!props.Budget.RemainingKnown || props.Budget.Remaining.MinorUnits != 0 ||
		props.Budget.WarningThresholdKnown || props.Budget.WarningReached || !props.Budget.HardCapReached ||
		props.Budget.SettlingInFlightKnown || props.Budget.SettlingInFlight {
		t.Fatalf("present zero values lost presence: cost=%+v tokens=%+v budget=%+v", props.Usage.Cost, props.Usage.Tokens, props.Budget)
	}
}

func TestDecodeTaskControlPropsMountsOnlySchemaBackedRecoveryFacts(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	view := taskResourceFixtureView(scope)
	view.State = string(domain.TaskStateRecoveryRequired)
	view.Revision = 12
	view.PlanRevision = 4
	props, err := decodeTaskControlProps(
		view,
		scope,
		frontendstate.SessionView{Connection: frontendstate.ConnectionLive},
		taskResourceFixtureRecoveryProjection(scope),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !props.Recovery.Required ||
		!strings.Contains(props.Recovery.KnownState, "task revision 12") ||
		!strings.Contains(props.Recovery.KnownState, "plan revision 4") {
		t.Fatalf("schema-backed recovery facts = %+v", props.Recovery)
	}
	if props.Recovery.SafeResumeVerified || props.Recovery.ReconcileRequired ||
		props.Recovery.PatchPreservable || len(props.Recovery.Details) != 0 ||
		props.OnSafeResume != nil || props.OnReconcile != nil || props.OnPreservePatch != nil {
		t.Fatalf("unsupported recovery facts/actions were invented: %+v", props.Recovery)
	}
	if !strings.Contains(props.Recovery.Ambiguity, "authoritative projection has not yet supplied") ||
		props.Review.Stale {
		t.Fatalf("schema gaps were not represented honestly: recovery=%+v review=%+v", props.Recovery, props.Review)
	}
	markup, err := ui.RenderToString(taskcontrols.TaskControlPanel(props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-component="recovery-panel"`, `data-known-state-first="true"`,
		"Needs recovery", "task revision 12 and plan revision 4",
		"Inspect authoritative recovery events before choosing a recovery action.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mounted recovery omitted %q: %s", want, markup)
		}
	}
	for _, unsupported := range []string{"Safe resume", "Reconcile user edits", "Preserve patch", `data-component="review-staleness"`} {
		if strings.Contains(markup, unsupported) {
			t.Errorf("mounted TaskView recovery invented %q: %s", unsupported, markup)
		}
	}
}

func TestDecorateTaskControlsFromProjectionMountsAuthoritativeRecoveryAndReviewFacts(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	view := taskResourceFixtureView(scope)
	view.State = string(domain.TaskStateRecoveryRequired)
	view.Revision = 12
	props, err := decodeTaskControlProps(
		view,
		scope,
		frontendstate.SessionView{Connection: frontendstate.ConnectionLive},
		taskResourceFixtureRecoveryProjection(scope),
	)
	if err != nil {
		t.Fatal(err)
	}
	checkpointID, err := domain.ParseCheckpointID("ckp_" + taskResourceFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := domain.ParseEventID("evt_" + taskResourceFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	checkpointAt := time.Date(2026, 7, 31, 10, 30, 0, 0, time.UTC)
	accepted := taskprojection.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 1}
	projection := taskprojection.TaskProjection{
		TaskID: scope.taskID,
		Checkpoint: taskprojection.CheckpointProjection{
			Present: true, ID: checkpointID, TaskRevision: 11, PlanStep: "validate workspace",
			CreatedAt: checkpointAt, Revision: 1,
		},
		Plan:       taskprojection.PlanProjection{Present: true, Revision: 2},
		Validation: taskprojection.ValidationProjection{Present: true, Revision: 2, DiffRevision: 2},
		Graph:      taskprojection.GraphProjection{Present: true, Revision: 2},
		Acceptance: taskprojection.AcceptanceProjection{
			Present: true, State: domain.ChangeAcceptanceStatePending, Revision: 1, Bindings: accepted,
		},
		Recovery: taskprojection.RecoveryAmbiguousOutcome,
		RecoveryDetail: taskprojection.RecoveryProjection{
			Present: true, Revision: 1, Classification: taskprojection.RecoveryAmbiguousOutcome,
			CheckpointID: &checkpointID, SafeReason: "External settlement is uncertain.",
			DivergenceSummary:        "The worktree differs from the checkpoint.",
			ExternalOutcomeAmbiguous: true, PreservePatchAvailable: true, Bindings: accepted,
			RelatedEventIDs: []domain.EventID{eventID}, RelatedFiles: []string{"web/client/main.go", "../outside"},
		},
	}

	decorateTaskControlsFromProjection(&props, projection)
	if !props.Recovery.Required || props.Recovery.LastCheckpointAt != checkpointAt ||
		props.Recovery.LastCheckpointPlanStep != "validate workspace" ||
		props.Recovery.DivergenceSummary != "The worktree differs from the checkpoint." ||
		!props.Recovery.ExternalOutcomeAmbiguous || !props.Recovery.PatchPreservable ||
		props.Recovery.SafeResumeVerified || props.Recovery.ReconcileRequired {
		t.Fatalf("projected recovery = %+v", props.Recovery)
	}
	if len(props.Recovery.Details) != 3 || props.Recovery.Details[0].Identity != eventID.String() ||
		props.Recovery.Details[1].Identity != "web/client/main.go" ||
		props.Recovery.Details[1].DisabledReason != "" ||
		props.Recovery.Details[2].Identity != "../outside" ||
		props.Recovery.Details[2].DisabledReason == "" {
		t.Fatalf("recovery details = %+v", props.Recovery.Details)
	}
	if !props.Review.Stale || len(props.Review.Reasons) < 3 {
		t.Fatalf("review staleness = %+v", props.Review)
	}
	if err := props.Validate(); err != nil {
		t.Fatalf("decorated props are invalid: %v", err)
	}
	markup, err := ui.RenderToString(taskcontrols.TaskControlPanel(props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"External action outcome is ambiguous", "Preserve patch", "Recovery event " + eventID.String(),
		"web/client/main.go", "Review is stale", "plan revision changed from 1 to 2",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mounted projection omitted %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, ">Retry<") || strings.Contains(markup, ">Safe resume<") {
		t.Fatalf("ambiguous recovery exposed an unsafe repeat: %s", markup)
	}
}

func TestDecorateTaskControlsFromProjectionBindsOnlyVerifiedSafeResume(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	view := taskResourceFixtureView(scope)
	view.State = string(domain.TaskStateRecoveryRequired)
	props, err := decodeTaskControlProps(
		view,
		scope,
		frontendstate.SessionView{Connection: frontendstate.ConnectionLive},
		taskResourceFixtureRecoveryProjection(scope),
	)
	if err != nil {
		t.Fatal(err)
	}
	resumed := false
	props.OnResume = func() { resumed = true }
	props.Controls.Resume = taskcontrols.CommandState{IdempotencyKey: "idem-safe-resume"}
	bindings := taskprojection.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 1}
	decorateTaskControlsFromProjection(&props, taskprojection.TaskProjection{
		TaskID: scope.taskID,
		RecoveryDetail: taskprojection.RecoveryProjection{
			Present: true, Revision: 1, Classification: taskprojection.RecoverySafeResume,
			SafeReason: "The checkpoint is verified.", DivergenceSummary: "No divergence detected.",
			SafeResumeVerified: true, Bindings: bindings,
		},
	})
	if props.OnSafeResume == nil || !props.Recovery.SafeResumeVerified ||
		props.Recovery.SafeResume.IdempotencyKey != "idem-safe-resume" {
		t.Fatalf("verified safe resume was not bound: recovery=%+v callback=%v", props.Recovery, props.OnSafeResume != nil)
	}
	props.OnSafeResume()
	if !resumed {
		t.Fatal("safe resume did not use the typed resume command")
	}
}

func TestDecodeTaskControlPropsRejectsMalformedOrMismatchedFacts(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	tests := map[string]func(*codefluxv1.TaskView){
		"task identity mismatch": func(view *codefluxv1.TaskView) {
			view.TaskId.Value = "tsk_01890f3c-4a00-7abc-8def-0123456789ac"
		},
		"wrong thread identity kind": func(view *codefluxv1.TaskView) {
			view.ThreadId.Kind = codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			view := taskResourceFixtureView(scope)
			mutate(view)
			if _, err := decodeTaskControlProps(view, scope, frontendstate.SessionView{Connection: frontendstate.ConnectionLive}, taskResourceFixtureProjection(scope)); !errors.Is(err, errTaskResourceMalformed) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDecodeTaskControlPropsIgnoresTaskViewCorrectnessFields(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	view := taskResourceFixtureView(scope)
	view.State = "probably-running"
	view.Revision = 999
	view.PlanRevision = 888
	view.BudgetRevision = 777
	view.ActualCost.MinorUnits = -1
	view.HardBudget.CurrencyCode = "EUR"
	view.RemainingHardBudget = nil
	view.WarningReached = true
	view.HardCapReached = true
	view.LatestCheckpointId = &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: scope.taskID.String()}

	projection := taskResourceFixtureProjection(scope)
	props, err := decodeTaskControlProps(
		view, scope, frontendstate.SessionView{Connection: frontendstate.ConnectionLive}, projection,
	)
	if err != nil {
		t.Fatal(err)
	}
	if props.TaskState != projection.State || props.TaskRevision != projection.Revision ||
		props.BudgetRevision != projection.Budget.Revision || props.Budget.HardLimit != projection.Budget.HardLimit ||
		props.Usage.Cost.Value != projection.Budget.Actual || props.Budget.HardCapReached ||
		props.Budget.CheckpointKnown {
		t.Fatalf("TaskView correctness fields leaked into mounted props: %+v", props)
	}
}

func TestLoadTaskControlPropsUsesTypedTaskIdentityAndClosesLease(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	client := &fakeTaskViewClient{response: &codefluxv1.GetTaskResponse{Task: taskResourceFixtureView(scope)}}
	closed := false
	props, err := loadTaskControlProps(t.Context(), func(context.Context) (taskViewLease, error) {
		return taskViewLease{client: client, close: func() error { closed = true; return nil }}, nil
	}, scope, frontendstate.SessionView{Connection: frontendstate.ConnectionLive}, taskResourceFixtureProjection(scope))
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("task client lease was not closed")
	}
	if client.request == nil || client.request.GetTaskId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK ||
		client.request.GetTaskId().GetValue() != scope.taskID.String() {
		t.Fatalf("request identity = %+v", client.request)
	}
	if props.TaskState != domain.TaskStateRunning {
		t.Fatalf("decoded state = %q", props.TaskState)
	}
}

func TestSelectedTaskResourceScopeComesFromAuthoritativeThread(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	repositoryID, _ := domain.ParseRepositoryID("repo_" + taskResourceFixtureUUID)
	workspaceID, _ := domain.ParseWorkspaceID("wsp_" + taskResourceFixtureUUID)
	thread, err := threadrail.NewThread(threadrail.ThreadInput{
		ID: scope.threadID, RepositoryID: repositoryID, WorkspaceID: workspaceID, TaskID: scope.taskID,
		Title: "Authoritative row", TaskState: threadrail.TaskStateRunning, Attention: threadrail.AttentionNone,
		Revision: 7, UpdatedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := selectedTaskResourceScope(thread)
	if err != nil {
		t.Fatal(err)
	}
	if got != scope {
		t.Fatalf("scope = %+v, want %+v", got, scope)
	}
	if _, err := selectedTaskResourceScope(threadrail.Thread{}); !errors.Is(err, errTaskResourceSelectionUnavailable) {
		t.Fatalf("empty selection error = %v", err)
	}
}

func TestTaskDeliveryViewMapsEverySessionConnectionConservatively(t *testing.T) {
	tests := []struct {
		connection frontendstate.ConnectionState
		state      taskcontrols.DeliveryState
		certain    bool
	}{
		{frontendstate.ConnectionLive, taskcontrols.DeliveryLive, true},
		{frontendstate.ConnectionConnecting, taskcontrols.DeliveryDegraded, false},
		{frontendstate.ConnectionReplaying, taskcontrols.DeliveryDegraded, false},
		{frontendstate.ConnectionDegraded, taskcontrols.DeliveryDegraded, false},
		{frontendstate.ConnectionDisconnected, taskcontrols.DeliveryDisconnected, false},
		{frontendstate.ConnectionUnauthorized, taskcontrols.DeliveryDisconnected, false},
		{frontendstate.ConnectionIncompatible, taskcontrols.DeliveryDisconnected, false},
	}
	for _, test := range tests {
		t.Run(string(test.connection), func(t *testing.T) {
			got := taskDeliveryView(frontendstate.SessionView{Connection: test.connection, Message: "status detail"})
			if got.State != test.state || got.SequenceCertain != test.certain || got.Explanation != "status detail" {
				t.Fatalf("delivery = %+v", got)
			}
		})
	}
}

func TestTaskPhaseCoversEveryAuthoritativeTaskState(t *testing.T) {
	tests := map[domain.TaskState]taskcontrols.Phase{
		domain.TaskStateDraft:                taskcontrols.PhasePlanning,
		domain.TaskStateForecasting:          taskcontrols.PhasePlanning,
		domain.TaskStateAwaitingPlanApproval: taskcontrols.PhasePlanning,
		domain.TaskStateReady:                taskcontrols.PhasePlanning,
		domain.TaskStateRunning:              taskcontrols.PhaseEditing,
		domain.TaskStatePaused:               taskcontrols.PhaseEditing,
		domain.TaskStateAwaitingAuthority:    taskcontrols.PhaseEditing,
		domain.TaskStateValidating:           taskcontrols.PhaseValidating,
		domain.TaskStateFailed:               taskcontrols.PhaseRepairing,
		domain.TaskStateRecoveryRequired:     taskcontrols.PhaseRepairing,
		domain.TaskStateAwaitingReview:       taskcontrols.PhaseReviewing,
		domain.TaskStateCompleted:            taskcontrols.PhaseReviewing,
		domain.TaskStateCancelled:            taskcontrols.PhaseReviewing,
		domain.TaskStateRolledBack:           taskcontrols.PhaseReviewing,
	}
	for state, want := range tests {
		if got := taskPhase(state); got != want {
			t.Errorf("taskPhase(%q) = %q, want %q", state, got, want)
		}
	}
}

type fakeTaskViewClient struct {
	request  *codefluxv1.GetTaskRequest
	response *codefluxv1.GetTaskResponse
	err      error
}

func (client *fakeTaskViewClient) GetTask(
	_ context.Context,
	request *codefluxv1.GetTaskRequest,
	_ ...grpc.CallOption,
) (*codefluxv1.GetTaskResponse, error) {
	client.request = request
	return client.response, client.err
}

func taskResourceFixtureScope(t *testing.T) taskResourceScope {
	t.Helper()
	taskID, err := domain.ParseTaskID("tsk_" + taskResourceFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.ParseThreadID("thr_" + taskResourceFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	return taskResourceScope{taskID: taskID, threadID: threadID}
}

func taskResourceFixtureView(scope taskResourceScope) *codefluxv1.TaskView {
	return &codefluxv1.TaskView{
		TaskId: taskIdentity(scope.taskID),
		ThreadId: &codefluxv1.StableIdentity{
			Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, Value: scope.threadID.String(),
		},
		State: "running", Revision: 9, BudgetRevision: 5,
		ActualCost:               &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 250, DecimalPlaces: 2},
		HardBudget:               &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 1000, DecimalPlaces: 2},
		RemainingHardBudget:      &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 750, DecimalPlaces: 2},
		WarningThreshold:         &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 800, DecimalPlaces: 2},
		ActualPricingSnapshotIds: []string{"prices-2026-07-31"},
		ActualTokens:             &codefluxv1.TokenAmount{Tokens: 123},
	}
}

func taskResourceFixtureProjection(scope taskResourceScope) taskprojection.TaskProjection {
	return taskprojection.TaskProjection{
		TaskID:   scope.taskID,
		State:    domain.TaskStateRunning,
		Revision: 9,
		Budget: taskprojection.BudgetProjection{
			Present:   true,
			Revision:  5,
			HardLimit: domain.Money{Currency: domain.CurrencyCode("USD"), MinorUnits: 1000},
			Reserved:  domain.Money{Currency: domain.CurrencyCode("USD")},
			Actual:    domain.Money{Currency: domain.CurrencyCode("USD"), MinorUnits: 250},
		},
		Recovery: taskprojection.RecoveryNone,
	}
}

func taskResourceFixtureRecoveryProjection(scope taskResourceScope) taskprojection.TaskProjection {
	projection := taskResourceFixtureProjection(scope)
	projection.State = domain.TaskStateRecoveryRequired
	projection.Revision = 12
	projection.Plan = taskprojection.PlanProjection{Present: true, Revision: 4}
	projection.Recovery = taskprojection.RecoveryNeedsReconcile
	return projection
}
