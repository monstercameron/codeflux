package coordinator

import (
	"context"
	"path/filepath"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestApplicationSetBudgetPreservesPolicyAndRequiresExactRaiseApproval(t *testing.T) {
	application, taskID, budgetID, usd := startTaskBudgetApplicationFixture(t)
	connection, err := grpc.NewClient(
		application.TaskControlAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := codefluxv1.NewTaskServiceClient(connection)
	ctx := metadata.AppendToOutgoingContext(
		t.Context(), transport.SessionMetadataKey, application.BrowserSessionSecret(),
	)
	identity, _ := transport.TaskIDToProto(taskID)
	revisionZero := uint64(0)
	unsupported := &codefluxv1.SetBudgetRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "unsupported-set-budget", ExpectedRevision: &revisionZero,
		},
		TaskId: identity,
		// The v1 request cannot also lower the authoritative warning threshold,
		// so this hard cap must fail instead of inventing one.
		HardLimit: &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 2_000},
	}
	if _, err := client.SetBudget(ctx, unsupported); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unsupported warning/hard relationship error = %v", err)
	}
	preapproval := &codefluxv1.SetBudgetRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "preapproval-set-budget", ExpectedRevision: &revisionZero,
		},
		TaskId:    identity,
		HardLimit: &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 6_000},
	}
	response, err := client.SetBudget(ctx, preapproval)
	if err != nil {
		t.Fatal(err)
	}
	if response.GetBudget().GetHardLimit().GetMinorUnits() != 6_000 ||
		response.GetBudget().GetRevision() != 1 {
		t.Fatalf("preapproval response = %#v", response.GetBudget())
	}
	retried, err := client.SetBudget(ctx, preapproval)
	if err != nil || retried.GetBudget().GetRevision() != 1 {
		t.Fatalf("idempotent preapproval = %#v, %v", retried, err)
	}
	state, err := application.repos.ReadTaskBudgetAdjustmentState(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Account.Budget.WarningCost.MinorUnits != 2_500 ||
		state.Account.Budget.WarningTokens != 1_000 ||
		state.Account.Budget.HardStopTokens != 2_000 ||
		state.Account.Budget.MaximumProviderCalls != 2 ||
		state.Account.Budget.MaximumRepairRounds != 1 ||
		state.Account.Budget.MaximumToolExecutions != 4 {
		t.Fatalf("omitted policy changed = %#v", state.Account.Budget)
	}

	transitionTaskToReady(t, application.repos, taskID)
	revisionOne := uint64(1)
	raiseRequest := &codefluxv1.SetBudgetRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "approved-set-budget", ExpectedRevision: &revisionOne,
		},
		TaskId:    identity,
		HardLimit: &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 8_000},
	}
	if _, err := client.SetBudget(ctx, raiseRequest); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("unapproved raise error = %v", err)
	}
	grantExactBudgetRaise(t, application.repos, taskID, budgetID, usd, 8_000, "raise-eight")
	raised, err := client.SetBudget(ctx, raiseRequest)
	if err != nil {
		t.Fatal(err)
	}
	if raised.GetBudget().GetHardLimit().GetMinorUnits() != 8_000 ||
		raised.GetBudget().GetRevision() != 2 {
		t.Fatalf("approved raise response = %#v", raised.GetBudget())
	}
	retried, err = client.SetBudget(ctx, raiseRequest)
	if err != nil || retried.GetBudget().GetRevision() != 2 {
		t.Fatalf("idempotent approved raise = %#v, %v", retried, err)
	}

	grantExactBudgetRaise(t, application.repos, taskID, budgetID, usd, 9_000, "raise-nine")
	reusedKey := &codefluxv1.SetBudgetRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "approved-set-budget", ExpectedRevision: &revisionOne,
		},
		TaskId:    identity,
		HardLimit: &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 9_000},
	}
	if _, err := client.SetBudget(ctx, reusedKey); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("changed idempotency key reuse error = %v", err)
	}
	staleRequest := &codefluxv1.SetBudgetRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "stale-set-budget", ExpectedRevision: &revisionOne,
		},
		TaskId:    identity,
		HardLimit: &codefluxv1.Money{CurrencyCode: "USD", MinorUnits: 9_000},
	}
	if _, err := client.SetBudget(ctx, staleRequest); status.Code(err) != codes.Aborted {
		t.Fatalf("stale raise error = %v", err)
	}
}

func startTaskBudgetApplicationFixture(
	t *testing.T,
) (*Application, domain.TaskID, domain.BudgetID, domain.CurrencyCode) {
	t.Helper()
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls: &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	taskID, _ := domain.NewTaskID()
	budgetID, _ := domain.NewBudgetID()
	usd, _ := domain.ParseCurrencyCode("USD")
	if _, err := application.repos.CreateProject(t.Context(), storage.CreateProject{ID: projectID, Name: "Task budget"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateRepository(t.Context(), storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: filepath.Join(root, "repository"), GitIdentity: "task-budget-repository",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateThread(t.Context(), storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Task budget",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateTask(t.Context(), storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "task-budget-task",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateBudget(t.Context(), storage.CreateBudget{
		TaskID: taskID,
		Budget: domain.TaskBudget{
			ID:            budgetID,
			WarningCost:   domain.Money{Currency: usd, MinorUnits: 2_500},
			HardStopCost:  domain.Money{Currency: usd, MinorUnits: 5_000},
			WarningTokens: 1_000, HardStopTokens: 2_000,
			WarningWallClock: 60_000, HardStopWallClock: 120_000,
			MaximumProviderCalls: 2, MaximumRepairRounds: 1, MaximumToolExecutions: 4,
		},
	}); err != nil {
		t.Fatal(err)
	}
	return application, taskID, budgetID, usd
}

func transitionTaskToReady(t *testing.T, repositories *storage.Repositories, taskID domain.TaskID) {
	t.Helper()
	steps := []struct {
		from domain.TaskState
		to   domain.TaskState
		key  string
	}{
		{domain.TaskStateDraft, domain.TaskStateForecasting, "task-budget-transition-1"},
		{domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval, "task-budget-transition-2"},
		{domain.TaskStateAwaitingPlanApproval, domain.TaskStateReady, "task-budget-transition-3"},
	}
	for index, step := range steps {
		eventID, _ := domain.NewEventID()
		approval := domain.ApprovalRequestState("")
		if step.to == domain.TaskStateReady {
			approval = domain.ApprovalRequestStateGranted
		}
		if _, err := repositories.TransitionTask(t.Context(), storage.TransitionTask{
			EventID: eventID, TaskID: taskID, ExpectedRevision: uint64(index),
			From: step.from, To: step.to, Approval: approval,
			IdempotencyKey: step.key,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func grantExactBudgetRaise(
	t *testing.T,
	repositories *storage.Repositories,
	taskID domain.TaskID,
	budgetID domain.BudgetID,
	currency domain.CurrencyCode,
	hard int64,
	key string,
) {
	t.Helper()
	approvalID, _ := domain.NewApprovalID()
	approval, err := repositories.CreateApproval(t.Context(), storage.CreateApproval{
		ID: approvalID, TaskID: taskID,
		Scope: storage.BudgetRaiseApprovalScope(
			budgetID,
			storage.ExactMinorCost{Numerator: 2_500, Denominator: 1, Currency: currency},
			storage.ExactMinorCost{Numerator: hard, Denominator: 1, Currency: currency},
			1_000, 2_000,
		),
		RequestReason: "approve exact test budget raise", IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.ResolveApproval(t.Context(), storage.ResolveApproval{
		ID: approval.ID, ExpectedRevision: 0, To: domain.ApprovalRequestStateGranted,
		ResolutionReason: "user approved exact test budget raise",
	}); err != nil {
		t.Fatal(err)
	}
}
