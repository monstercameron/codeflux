package coordinator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// approvalFixture is a booted application with one repository and one thread.
type approvalFixture struct {
	application *Application
	threadID    domain.ThreadID
}

func newApprovalFixture(t *testing.T, withWorker bool) approvalFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	repositoryPath := filepath.Join(root, "repo")
	initializeCoordinatorGitRepository(t, repositoryPath)

	options := ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		WorktreeRoot: filepath.Join(root, "worktrees"),
		TaskControls: &applicationTaskControlStub{},
	}
	if withWorker {
		options.WorkerExecutable = buildCoordinatorWorkerExecutable(t)
	}
	application, err := StartApplication(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	repositories := application.repos
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	if _, err := repositories.CreateProject(ctx, storage.CreateProject{
		ID: projectID, Name: "Approval",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(ctx, storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: repositoryPath, GitIdentity: strings.Repeat("f", 40),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Approval",
	}); err != nil {
		t.Fatal(err)
	}
	return approvalFixture{application: application, threadID: threadID}
}

func (fixture approvalFixture) create(t *testing.T, key string) transport.CreatedTaskView {
	t.Helper()
	created, err := fixture.application.TaskLifecycleApplication().CreateTaskFromRequirement(
		context.Background(), transport.CreateTaskCommand{
			ThreadID:    fixture.threadID,
			Requirement: "Add a doc comment to the ResolveRevision function.",
			TaskClass:   string(fingerprint.TaskClassDocumentation),
			// The environment fields are deliberately left empty: they are
			// observed, not chosen, and a caller that supplies none must get
			// the coordinator's own reading rather than a refusal.
			IdempotencyKey: key,
		})
	if err != nil {
		t.Fatalf("intake failed: %v", err)
	}
	return created
}

func TestApprovingAPlanMakesATaskStartable(t *testing.T) {
	// This is the step that was missing, and its absence was invisible: every
	// unit test passed, the interface connected and reported honestly, and a
	// submitted request became a draft task that nothing could ever start.
	fixture := newApprovalFixture(t, true)
	lifecycle := fixture.application.TaskLifecycleApplication()
	created := fixture.create(t, "approval-happy")
	if created.State != domain.TaskStateDraft {
		t.Fatalf("created state = %s, want draft", created.State)
	}

	approved, err := lifecycle.ApproveTaskPlan(context.Background(), transport.ApprovePlanCommand{
		TaskID: created.TaskID, ExpectedRevision: created.Revision,
		IdempotencyKey: "approval-happy",
	})
	if err != nil {
		t.Fatalf("approving the plan failed: %v", err)
	}
	if approved.State != domain.TaskStateReady {
		t.Fatalf("approved state = %s, want ready", approved.State)
	}
	if approved.PreflightRevision == 0 {
		// Binding execution to a zero revision would pass every check and mean
		// nothing, so the absence is refused rather than reported as success.
		t.Fatal("approval produced no preflight binding")
	}

	started, err := lifecycle.StartPreparedTask(context.Background(), transport.StartTaskCommand{
		TaskID: created.TaskID, ExpectedRevision: approved.Revision,
		PreflightRevision: approved.PreflightRevision,
		IdempotencyKey:    "approval-happy-start",
	})
	if err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}
	if started.State != domain.TaskStateRunning {
		t.Fatalf("started state = %s, want running", started.State)
	}

	// The last link: a start that only writes a row is the defect this whole
	// chain exists to close.
	binding, err := fixture.application.repos.GetWorktreeBinding(
		context.Background(), created.TaskID)
	if err != nil {
		t.Fatalf("a started task has no worktree: %v", err)
	}
	if binding.WorktreePath == "" {
		t.Fatal("the worktree binding names no path")
	}
	t.Logf("task %s is running in %s", created.TaskID, binding.WorktreePath)
}

func TestIntakeObservesTheEnvironmentTheCallerCannotKnow(t *testing.T) {
	// A browser inventing a toolchain version would put an unmeasured guess
	// inside the fingerprint that gates memory retrieval and routing. The
	// coordinator reads its own, so an empty request is answered rather than
	// refused.
	fixture := newApprovalFixture(t, false)
	created := fixture.create(t, "environment-observed")
	if created.PolicyRevision == 0 || created.ForecastRevision == 0 {
		t.Fatalf("intake produced policy rev %d and forecast rev %d; both must exist "+
			"for there to be anything to approve",
			created.PolicyRevision, created.ForecastRevision)
	}
	if created.BudgetID.IsZero() {
		t.Fatal("intake produced no budget, so nothing caps what the task may spend")
	}
}

func TestApprovingAMovedTaskIsRefused(t *testing.T) {
	// A person approves a task as they saw it. If it has moved since, what
	// would start is not what was reviewed.
	fixture := newApprovalFixture(t, false)
	created := fixture.create(t, "approval-stale")
	_, err := fixture.application.TaskLifecycleApplication().ApproveTaskPlan(
		context.Background(), transport.ApprovePlanCommand{
			TaskID: created.TaskID, ExpectedRevision: created.Revision + 7,
			IdempotencyKey: "approval-stale",
		})
	if err == nil {
		t.Fatal("approving a task at a revision it is not at was accepted")
	}
	if !errors.Is(err, storage.ErrConflict) {
		t.Errorf("a stale approval reported %v, want a conflict", err)
	}
}

func TestApprovingATaskThatIsNotWaitingIsRefused(t *testing.T) {
	// Approving something already running would bind a second preflight to a
	// task that is mid-execution.
	fixture := newApprovalFixture(t, false)
	lifecycle := fixture.application.TaskLifecycleApplication()
	created := fixture.create(t, "approval-twice")
	approved, err := lifecycle.ApproveTaskPlan(context.Background(), transport.ApprovePlanCommand{
		TaskID: created.TaskID, ExpectedRevision: created.Revision,
		IdempotencyKey: "approval-twice",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = lifecycle.ApproveTaskPlan(context.Background(), transport.ApprovePlanCommand{
		TaskID: created.TaskID, ExpectedRevision: approved.Revision,
		IdempotencyKey: "approval-twice-again",
	})
	if err == nil {
		t.Fatal("a task already approved was approved again")
	}
}
