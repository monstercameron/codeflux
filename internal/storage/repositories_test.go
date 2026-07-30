package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestProjectAndRepositoryOperationsUseTypedIdentities(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1)
	project, err := repositories.CreateProject(ctx, CreateProject{
		ID:   projectID,
		Name: "Fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotProject, err := repositories.GetProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if gotProject != project {
		t.Fatalf("project round trip = %#v, want %#v", gotProject, project)
	}

	repositoryID := testRepositoryID(t, 2)
	repository, err := repositories.CreateRepository(ctx, CreateRepository{
		ID:            repositoryID,
		ProjectID:     projectID,
		CanonicalPath: "/fixture",
		GitIdentity:   "git-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotRepository, err := repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRepository != repository {
		t.Fatalf("repository round trip = %#v, want %#v", gotRepository, repository)
	}

	if _, err := repositories.CreateProject(ctx, CreateProject{
		ID:   projectID,
		Name: "Duplicate",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate project error = %v, want conflict", err)
	}
	if _, err := repositories.GetProject(ctx, testProjectID(t, 99)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project error = %v, want not found", err)
	}
	if _, err := repositories.CreateRepository(ctx, CreateRepository{
		ID:            testRepositoryID(t, 100),
		ProjectID:     testProjectID(t, 101),
		CanonicalPath: "/orphan",
		GitIdentity:   "git-orphan",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("orphan repository error = %v, want constraint", err)
	}
}

func TestThreadCursorPaginationIsStable(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 10)
	repositoryID := testRepositoryID(t, 11)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	ids := []domain.ThreadID{
		testThreadID(t, 12),
		testThreadID(t, 13),
		testThreadID(t, 14),
	}
	for index, id := range ids {
		if _, err := repositories.CreateThread(ctx, CreateThread{
			ID:           id,
			ProjectID:    projectID,
			RepositoryID: repositoryID,
			Title:        fmt.Sprintf("Thread %d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repositories.ListThreads(ctx, ListThreads{
		RepositoryID: repositoryID,
		Limit:        2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Threads) != 2 || first.Next == nil {
		t.Fatalf("first page = %#v", first)
	}
	if first.Threads[0].ID != ids[2] || first.Threads[1].ID != ids[1] {
		t.Fatalf("first page order = %v, %v", first.Threads[0].ID, first.Threads[1].ID)
	}
	second, err := repositories.ListThreads(ctx, ListThreads{
		RepositoryID: repositoryID,
		Before:       first.Next,
		Limit:        2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Threads) != 1 ||
		second.Threads[0].ID != ids[0] ||
		second.Next != nil {
		t.Fatalf("second page = %#v", second)
	}
}

func TestThreadCreationRejectsCrossProjectRepository(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	firstProject := testProjectID(t, 20)
	secondProject := testProjectID(t, 21)
	repositoryID := testRepositoryID(t, 22)
	mustCreateProjectRepository(t, repositories, firstProject, repositoryID)
	if _, err := repositories.CreateProject(ctx, CreateProject{
		ID:   secondProject,
		Name: "Second",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, CreateThread{
		ID:           testThreadID(t, 23),
		ProjectID:    secondProject,
		RepositoryID: repositoryID,
		Title:        "Mismatched",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project thread error = %v, want constraint", err)
	}
}

func TestAppendMessageIsIdempotentAndAllocatesSequence(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 30)
	repositoryID := testRepositoryID(t, 31)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	threadID := testThreadID(t, 32)
	if _, err := repositories.CreateThread(ctx, CreateThread{
		ID:           threadID,
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		Title:        "Messages",
	}); err != nil {
		t.Fatal(err)
	}
	input := AppendMessage{
		ID:             testMessageID(t, 33),
		ThreadID:       threadID,
		Role:           MessageRoleUser,
		BodyRedacted:   "first",
		IdempotencyKey: "message-one",
	}
	first, err := repositories.AppendMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.AppendMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if retried != first || first.Sequence != 1 {
		t.Fatalf("idempotent messages = %#v, %#v", first, retried)
	}
	second, err := repositories.AppendMessage(ctx, AppendMessage{
		ID:             testMessageID(t, 34),
		ThreadID:       threadID,
		Role:           MessageRoleAssistant,
		BodyRedacted:   "second",
		IdempotencyKey: "message-two",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Sequence != 2 {
		t.Fatalf("second message sequence = %d, want 2", second.Sequence)
	}
	input.BodyRedacted = "changed"
	if _, err := repositories.AppendMessage(ctx, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed retry error = %v, want conflict", err)
	}
}

func TestTaskTransitionAndEventAppendAreAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 100)
	transition := TransitionTask{
		EventID:          testEventID(t, 107),
		TaskID:           task.ID,
		ExpectedRevision: 0,
		From:             domain.TaskStateDraft,
		To:               domain.TaskStateForecasting,
		IdempotencyKey:   "transition-one",
	}
	first, err := repositories.TransitionTask(ctx, transition)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.TransitionTask(ctx, transition)
	if err != nil {
		t.Fatal(err)
	}
	if first != retried ||
		first.Task.State != domain.TaskStateForecasting ||
		first.Task.Revision != 1 ||
		first.Event.Sequence != 1 {
		t.Fatalf("transition results = %#v, %#v", first, retried)
	}
	changed := transition
	changed.EventID = testEventID(t, 108)
	if _, err := repositories.TransitionTask(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed transition retry error = %v, want conflict", err)
	}
	if _, err := repositories.TransitionTask(ctx, TransitionTask{
		EventID:          testEventID(t, 109),
		TaskID:           task.ID,
		ExpectedRevision: 0,
		From:             domain.TaskStateForecasting,
		To:               domain.TaskStateAwaitingPlanApproval,
		IdempotencyKey:   "stale-transition",
	}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale transition error = %v, want stale revision", err)
	}
}

func TestConcurrentTaskEventAppendAllocatesMonotonicSequence(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 200)
	const count = 20
	events := make(chan TaskEvent, count)
	failures := make(chan error, count)
	eventIDs := make([]domain.EventID, count)
	for index := range eventIDs {
		eventIDs[index] = testEventID(t, 210+index)
	}
	var wait sync.WaitGroup
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			event, err := repositories.AppendTaskEvent(ctx, AppendTaskEvent{
				ID:             eventIDs[index],
				TaskID:         task.ID,
				EventType:      "fixture.concurrent",
				PayloadJSON:    fmt.Sprintf(`{"index":%d}`, index),
				IdempotencyKey: fmt.Sprintf("concurrent-%d", index),
			})
			if err != nil {
				failures <- err
				return
			}
			events <- event
		}(index)
	}
	wait.Wait()
	close(events)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	sequences := make([]int, 0, count)
	for event := range events {
		sequences = append(sequences, int(event.Sequence))
	}
	sort.Ints(sequences)
	if len(sequences) != count {
		t.Fatalf("event count = %d, want %d", len(sequences), count)
	}
	for index, sequence := range sequences {
		if sequence != index+1 {
			t.Fatalf("event sequences = %v", sequences)
		}
	}
}

func TestApprovalAndBudgetOperationsAreRevisionChecked(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 300)
	expires := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	approvalInput := CreateApproval{
		ID:             testApprovalID(t, 307),
		TaskID:         task.ID,
		Scope:          "run-command",
		RequestReason:  "fixture authority",
		IdempotencyKey: "approval-one",
		ExpiresAt:      &expires,
	}
	approval, err := repositories.CreateApproval(ctx, approvalInput)
	if err != nil {
		t.Fatal(err)
	}
	retriedApproval, err := repositories.CreateApproval(ctx, approvalInput)
	if err != nil {
		t.Fatal(err)
	}
	if retriedApproval.ID != approval.ID || retriedApproval.Revision != 0 {
		t.Fatalf("idempotent approvals = %#v, %#v", approval, retriedApproval)
	}
	resolved, err := repositories.ResolveApproval(ctx, ResolveApproval{
		ID:               approval.ID,
		ExpectedRevision: 0,
		To:               domain.ApprovalRequestStateGranted,
		ResolutionReason: "approved fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != domain.ApprovalRequestStateGranted || resolved.Revision != 1 {
		t.Fatalf("resolved approval = %#v", resolved)
	}
	if _, err := repositories.ResolveApproval(ctx, ResolveApproval{
		ID:               approval.ID,
		ExpectedRevision: 0,
		To:               domain.ApprovalRequestStateDenied,
		ResolutionReason: "stale",
	}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale approval error = %v", err)
	}

	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	budgetID := testBudgetID(t, 308)
	account, err := repositories.CreateBudget(ctx, CreateBudget{
		TaskID: task.ID,
		Budget: domain.TaskBudget{
			ID:                    budgetID,
			WarningCost:           domain.Money{Currency: usd, MinorUnits: 500},
			HardStopCost:          domain.Money{Currency: usd, MinorUnits: 1_000},
			WarningTokens:         5_000,
			HardStopTokens:        10_000,
			WarningWallClock:      10_000,
			HardStopWallClock:     20_000,
			MaximumProviderCalls:  10,
			MaximumRepairRounds:   2,
			MaximumToolExecutions: 20,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Revision != 0 || account.ReservedCost.MinorUnits != 0 {
		t.Fatalf("created budget = %#v", account)
	}
	reserved, err := repositories.ReserveBudget(ctx, ReserveBudget{
		ID:               budgetID,
		ExpectedRevision: 0,
		Amount:           domain.Money{Currency: usd, MinorUnits: 400},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reserved.ReservedCost.MinorUnits != 400 || reserved.Revision != 1 {
		t.Fatalf("reserved budget = %#v", reserved)
	}
	if _, err := repositories.ReserveBudget(ctx, ReserveBudget{
		ID:               budgetID,
		ExpectedRevision: 0,
		Amount:           domain.Money{Currency: usd, MinorUnits: 1},
	}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale budget error = %v", err)
	}
	posted, err := repositories.PostActualCost(ctx, PostActualCost{
		ID:                   budgetID,
		ExpectedRevision:     1,
		Actual:               domain.Money{Currency: usd, MinorUnits: 250},
		ReleaseReservedMinor: 300,
		Tokens:               10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if posted.ReservedCost.MinorUnits != 100 ||
		posted.ActualCost.MinorUnits != 250 ||
		posted.ActualTokens != 10 ||
		posted.Revision != 2 {
		t.Fatalf("posted budget = %#v", posted)
	}
	if _, err := repositories.ReserveBudget(ctx, ReserveBudget{
		ID:               budgetID,
		ExpectedRevision: 2,
		Amount:           domain.Money{Currency: usd, MinorUnits: 700},
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("over-budget error = %v, want constraint", err)
	}
}

func TestCheckpointValidationAndEvidencePersistAgainstTask(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 400)
	checkpointInput := CreateCheckpoint{
		ID:                 testCheckpointID(t, 407),
		TaskID:             task.ID,
		State:              domain.CheckpointStateCreating,
		RepositoryRevision: "0123456789abcdef",
		WorktreeDiffHash:   strings.Repeat("a", 64),
		EventSequence:      0,
		IdempotencyKey:     "checkpoint-one",
	}
	checkpoint, err := repositories.CreateCheckpoint(ctx, checkpointInput)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.CreateCheckpoint(ctx, checkpointInput)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.ID != retried.ID || checkpoint.CreatedAt != retried.CreatedAt {
		t.Fatalf("idempotent checkpoints = %#v, %#v", checkpoint, retried)
	}
	loadedCheckpoint, err := repositories.GetCheckpoint(ctx, checkpoint.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedCheckpoint != checkpoint {
		t.Fatalf("loaded checkpoint = %#v, want %#v", loadedCheckpoint, checkpoint)
	}
	validation, err := repositories.CreateValidation(ctx, CreateValidation{
		ID:          testValidationID(t, 408),
		TaskID:      task.ID,
		State:       domain.ValidationStatePending,
		Severity:    domain.ValidationSeverityBlocking,
		ProfileName: "go-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := repositories.CreateEvidence(ctx, CreateEvidence{
		ID:              testEvidenceID(t, 409),
		ValidationID:    validation.ID,
		TaskID:          task.ID,
		AssuranceLevel:  domain.AssuranceLevelRuntimeOnly,
		EvidenceType:    "test-run",
		ContentHash:     strings.Repeat("b", 64),
		SummaryRedacted: "fixture passed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ValidationID != validation.ID || evidence.TaskID != task.ID {
		t.Fatalf("evidence = %#v", evidence)
	}
}

func openTestRepositories(t *testing.T) *Repositories {
	t.Helper()
	database := openMigratedSchema(t)
	current := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	repositories, err := NewRepositories(database, func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		current = current.Add(time.Microsecond)
		return current
	})
	if err != nil {
		t.Fatal(err)
	}
	return repositories
}

func createTaskFixture(t *testing.T, base int) (*Repositories, Task) {
	t.Helper()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, base)
	repositoryID := testRepositoryID(t, base+1)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	threadID := testThreadID(t, base+2)
	if _, err := repositories.CreateThread(context.Background(), CreateThread{
		ID:           threadID,
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		Title:        "Task fixture",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := repositories.CreateTask(context.Background(), CreateTask{
		ID:                testTaskID(t, base+3),
		ThreadID:          threadID,
		RepositoryID:      repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey:    "task-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.CreateTask(context.Background(), CreateTask{
		ID:                task.ID,
		ThreadID:          threadID,
		RepositoryID:      repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey:    "task-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if retried != task {
		t.Fatalf("task retry = %#v, want %#v", retried, task)
	}
	return repositories, task
}

func mustCreateProjectRepository(
	t *testing.T,
	repositories *Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
) {
	t.Helper()
	if _, err := repositories.CreateProject(context.Background(), CreateProject{
		ID:   projectID,
		Name: "Fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(context.Background(), CreateRepository{
		ID:            repositoryID,
		ProjectID:     projectID,
		CanonicalPath: "/fixture/" + repositoryID.String(),
		GitIdentity:   "git-" + repositoryID.String(),
	}); err != nil {
		t.Fatal(err)
	}
}

func testProjectID(t *testing.T, number int) domain.ProjectID {
	t.Helper()
	id, err := domain.ParseProjectID("prj_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testRepositoryID(t *testing.T, number int) domain.RepositoryID {
	t.Helper()
	id, err := domain.ParseRepositoryID("repo_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testThreadID(t *testing.T, number int) domain.ThreadID {
	t.Helper()
	id, err := domain.ParseThreadID("thr_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testMessageID(t *testing.T, number int) domain.MessageID {
	t.Helper()
	id, err := domain.ParseMessageID("msg_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testTaskID(t *testing.T, number int) domain.TaskID {
	t.Helper()
	id, err := domain.ParseTaskID("tsk_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testEventID(t *testing.T, number int) domain.EventID {
	t.Helper()
	id, err := domain.ParseEventID("evt_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testApprovalID(t *testing.T, number int) domain.ApprovalID {
	t.Helper()
	id, err := domain.ParseApprovalID("apr_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testBudgetID(t *testing.T, number int) domain.BudgetID {
	t.Helper()
	id, err := domain.ParseBudgetID("bdg_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testCheckpointID(t *testing.T, number int) domain.CheckpointID {
	t.Helper()
	id, err := domain.ParseCheckpointID("ckp_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testValidationID(t *testing.T, number int) domain.ValidationID {
	t.Helper()
	id, err := domain.ParseValidationID("val_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testEvidenceID(t *testing.T, number int) domain.EvidenceID {
	t.Helper()
	id, err := domain.ParseEvidenceID("evd_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testUUID(number int) string {
	return fmt.Sprintf("01890f3c-4a00-7abc-8def-%012x", number)
}
