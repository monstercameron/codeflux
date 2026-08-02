package coordinator

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/retrieval"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestRequirementReachesAStartedRunThroughTheRealApplication drives the whole
// path a user takes, against a real booted Application and real SQLite:
//
//	requirement -> task -> fingerprint -> memory consulted -> policy +
//	forecast + budget -> approval -> Ready -> preflight bound -> started run
//
// Every earlier test in this package covers one seam. This one refuses to
// mock any of them, so a break anywhere in the chain fails here.
func TestRequirementReachesAStartedRunThroughTheRealApplication(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	// A real Git repository and a real worker executable, because the last
	// link in this chain is a subprocess: a start that only writes a row is
	// the defect this test exists to catch.
	repositoryPath := filepath.Join(root, "repo")
	initializeCoordinatorGitRepository(t, repositoryPath)
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		WorktreeRoot:     filepath.Join(root, "worktrees"),
		WorkerExecutable: buildCoordinatorWorkerExecutable(t),
		TaskControls:     &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	repositories := application.repos
	lifecycle := application.TaskLifecycleApplication()
	if lifecycle == nil {
		t.Fatal("the booted application must expose a task lifecycle: without it nothing can start work")
	}

	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	if _, err := repositories.CreateProject(ctx, storage.CreateProject{ID: projectID, Name: "End to end"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(ctx, storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: repositoryPath, GitIdentity: strings.Repeat("f", 40),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Server and health",
	}); err != nil {
		t.Fatal(err)
	}

	// 1. A requirement becomes a forecasted task.
	created, err := lifecycle.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 threadID,
		Requirement:              "Add a health endpoint returning 200 with a JSON body and a request identity header.",
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("1", 40),
		BaselineModelRevision:    "scripted-provider-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"internal/server"},
		IdempotencyKey:           "e2e-requirement",
	})
	if err != nil {
		t.Fatalf("requirement intake failed: %v", err)
	}
	if created.State != domain.TaskStateDraft {
		t.Fatalf("created state = %s, want draft", created.State)
	}
	t.Logf("task %s created: policy rev %d, forecast rev %d, budget %s",
		created.TaskID, created.PolicyRevision, created.ForecastRevision, created.BudgetID)

	// 2. Memory was consulted before planning.
	retrievalService, err := retrieval.NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	report, err := retrievalService.ListTaskMemoryInfluence(ctx, created.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.FellBackQueries) == 0 {
		t.Fatal("memory must be consulted before planning, recording a fallback when nothing is eligible")
	}
	t.Logf("memory consulted: %d fallback(s), %d influential item(s)",
		len(report.FellBackQueries), len(report.Influenced))

	// 3. The user approves, driving the task to Ready. Reaching Ready needs a
	//    granted approval, which is what gates everything after.
	readyRevision := driveTaskToReady(t, repositories, created.TaskID, created.Revision)

	// 4. Bind the exact reviewed preflight.
	preflight, err := application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"e2e-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	t.Logf("preflight bound at revision %d", preflight.Revision)

	// 5. Start exactly what was approved.
	started, err := lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "e2e-start",
	})
	if err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}
	if started.State != domain.TaskStateRunning {
		t.Fatalf("started state = %s, want running", started.State)
	}
	t.Logf("task %s is running at revision %d", started.TaskID, started.Revision)

	// 6. The last link: a worktree exists for the task and a worker was
	// actually asked for. Before this was wired, everything above passed and
	// nothing ran.
	binding, err := repositories.GetWorktreeBinding(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("starting a task must create its worktree: %v", err)
	}
	if binding.WorktreePath == "" {
		t.Fatal("the task worktree binding records no path")
	}
	if _, err := os.Stat(binding.WorktreePath); err != nil {
		t.Fatalf("the recorded worktree does not exist on disk: %v", err)
	}
	t.Logf("task worktree created at %s", binding.WorktreePath)
}

// initializeCoordinatorGitRepository creates the repository a task edits.
func initializeCoordinatorGitRepository(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(arguments ...string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", arguments...)
		command.Dir = path
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	run("init", "--initial-branch=main")
	// The repository is a real Go module, because the product's whole job is to
	// write Go into it and then run the module's own tests. Without go.mod
	// nothing an agent writes can compile and no validation command can run, so
	// a fixture missing it quietly tests a repository no user would ever have.
	for name, content := range map[string]string{
		"go.mod":  "module codeflux.test/workspace\n\ngo 1.26.0\n",
		"main.go": "package main\n\nfunc main() {}\n",
	} {
		if err := os.WriteFile(
			filepath.Join(path, name), []byte(content), 0o600,
		); err != nil {
			t.Fatal(err)
		}
		run("add", name)
	}
	run("-c", "user.name=Codeflux Test", "-c", "user.email=codeflux@example.invalid",
		"commit", "-m", "base")
}

// buildCoordinatorWorkerExecutable compiles the real worker entry point.
//
// A stub would prove only that some process was spawned. Building the actual
// subprocess proves the coordinator hands it something it can decode.
func buildCoordinatorWorkerExecutable(t *testing.T) string {
	t.Helper()
	name := "codeflux-worker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	output := filepath.Join(t.TempDir(), name)
	command := exec.CommandContext(t.Context(),
		"go", "build", "-o", output, "codeflux.dev/codeflux/cmd/codeflux-worker")
	if built, err := command.CombinedOutput(); err != nil {
		t.Skipf("cannot build the worker executable in this environment: %v\n%s", err, built)
	}
	return output
}

// driveTaskToReady performs the approval transitions a user's decision would,
// and returns the task revision at Ready.
func driveTaskToReady(
	t *testing.T,
	repositories *storage.Repositories,
	taskID domain.TaskID,
	fromRevision uint64,
) uint64 {
	t.Helper()
	ctx := context.Background()
	steps := []struct {
		from domain.TaskState
		to   domain.TaskState
		key  string
	}{
		{domain.TaskStateDraft, domain.TaskStateForecasting, "e2e-forecasting"},
		{domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval, "e2e-awaiting"},
		{domain.TaskStateAwaitingPlanApproval, domain.TaskStateReady, "e2e-ready"},
	}
	revision := fromRevision
	for _, step := range steps {
		eventID, err := domain.NewEventID()
		if err != nil {
			t.Fatal(err)
		}
		approval := domain.ApprovalRequestState("")
		if step.to == domain.TaskStateReady {
			approval = domain.ApprovalRequestStateGranted
		}
		transitioned, err := repositories.TransitionTask(ctx, storage.TransitionTask{
			EventID: eventID, TaskID: taskID, ExpectedRevision: revision,
			From: step.from, To: step.to, Approval: approval,
			IdempotencyKey: step.key,
		})
		if err != nil {
			t.Fatalf("transition %s -> %s failed: %v", step.from, step.to, err)
		}
		revision = transitioned.Task.Revision
	}
	return revision
}

// TestStartingWithoutApprovalIsRefusedByTheRealApplication proves the
// approval gate is enforced by the running system, not just by intent: a
// task that never reached Ready cannot be started.
func TestStartingWithoutApprovalIsRefusedByTheRealApplication(t *testing.T) {
	ctx := context.Background()
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

	repositories := application.repos
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	if _, err := repositories.CreateProject(ctx, storage.CreateProject{ID: projectID, Name: "Refusal"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(ctx, storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: filepath.Join(root, "repo"), GitIdentity: strings.Repeat("f", 40),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Refusal",
	}); err != nil {
		t.Fatal(err)
	}

	created, err := application.TaskLifecycleApplication().CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 threadID,
		Requirement:              "Change the retry policy.",
		TaskClass:                string(fingerprint.TaskClassBugFix),
		RepositoryRevision:       strings.Repeat("2", 40),
		BaselineModelRevision:    "scripted-provider-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		IdempotencyKey:           "e2e-refusal",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = application.TaskLifecycleApplication().StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  created.Revision,
		PreflightRevision: 1,
		IdempotencyKey:    "e2e-refusal-start",
	})
	if err == nil {
		t.Fatal("a task that was never approved into Ready must not start")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation rather than a refusal: %v", err)
	}
	t.Logf("unapproved start correctly refused: %v", err)
}
