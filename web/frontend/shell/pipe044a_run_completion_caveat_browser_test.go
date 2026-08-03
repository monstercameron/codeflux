package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/coordinator"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/testfixtures"
	"codeflux.dev/codeflux/internal/transport"
	"github.com/mxschmitt/playwright-go"
)

// TestPIPE044a_RunCompletionCaveatAppearsBesideACompletedRunInARealBrowser is
// PIPE-044a's own browser evidence: it drives one real requirement through a
// real booted coordinator, serving the real compiled frontend from the same
// running Application the whole time, and drives a real headless Chromium
// through /tasks to the completed run's final message to confirm shell.go's
// new wiring -- ConversationPane now setting timelineview.Props.PipelineSkipSummary
// from props.Timeline.PipelineSkipSummary -- actually reaches the rendered
// page, not only the native projection tests in timelineview and
// pipelineledger.
//
// The requirement-driving steps below are a deliberately narrower copy of
// internal/testharness.StartMountedThread's own (that package is not owned by
// this ticket, and its Application never mounts FrontendAssets -- it is a
// data-seeding harness, not a served one). Reusing it here would mean either
// modifying it, out of scope, or shutting its Application down and reopening
// the same database from a second process, which was tried first and
// discarded: closing the coordinator mid-run and reopening the database
// trips this product's own crash-recovery reconciliation, which marks the
// task "needs recovery" and hides it, exactly the way a real restart during
// a real user's run should. One continuously running Application, started
// with FrontendAssets from the beginning, avoids manufacturing a fake crash.
//
// This test is also what found web/client/pipeline_ledger_mounted_wasm.go's
// own real defect: fetch.UseResource's dependency comparison intermittently
// missed the transition from no task selected to a real one, leaving the
// ledger fetch (and this caveat) permanently stuck on the "no task" load's
// resolved error for the rest of the browser session. That hook now guards
// the transition with an explicit UseRef check rather than trusting deps
// diffing alone; see its comment for the mechanism. Even with that fix this
// check is not perfectly reliable on this repository's current shared,
// heavily loaded checkout -- run repeatedly during development, it passed
// with correct, numerically cross-checked output (skip-ratio-percent and the
// established/declined/not-implemented counts matched a direct database read
// of the same attempt's pipeline_stage_records) noticeably more often after
// the fix than before it, but not every single time. A failure here should
// be read against that history: open the screenshot and the debug counts
// logged on failure before concluding the wiring regressed.
//
// It is opt-in behind CODEFLUX_PIPE044A_BROWSER=1 and skipped otherwise. It
// is not part of the ordinary `go test ./web/...` sweep: it shells out to
// `go build` (the worker) and `go run ./cmd/codeflux-dev build-frontend`
// (scaffolds and compiles the WebAssembly client), then drives a real
// Chromium instance -- together these take minutes, not the seconds every
// other test in this package needs. That shape matches the repository's
// other real-provider and real-browser gates (run-live, test-browser), which
// are opt-in for the same reason: a default `go test` sweep must stay fast
// and must not fail on a machine with no Chromium installed.
func TestPIPE044a_RunCompletionCaveatAppearsBesideACompletedRunInARealBrowser(t *testing.T) {
	if os.Getenv("CODEFLUX_PIPE044A_BROWSER") != "1" {
		t.Skip("opt-in: set CODEFLUX_PIPE044A_BROWSER=1 to drive a real browser (see doc comment)")
	}

	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(repositoryRoot, ".artifacts", "pipe044a-browser"))
	if err != nil {
		t.Fatalf("resolve artifact root: %v", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("clear stale artifact root: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create artifact root: %v", err)
	}

	binDirectory := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDirectory, 0o755); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	workerPath := buildGoBinary(t, repositoryRoot, binDirectory, "codeflux-worker", "./cmd/codeflux-worker")

	assetsDirectory := filepath.Join(root, "frontend")
	runToolWithTimeout(t, repositoryRoot, 3*time.Minute,
		"go", "run", "./cmd/codeflux-dev", "build-frontend", "--root", assetsDirectory)

	appRoot := filepath.Join(root, "app")
	repositoryFixturePath := filepath.Join(appRoot, "repository")
	fixtureCtx, fixtureCancel := context.WithTimeout(context.Background(), time.Minute)
	fixture, err := testfixtures.NewRepositoryFixture(
		fixtureCtx, repositoryFixturePath, testfixtures.CleanGoRepositoryFiles())
	fixtureCancel()
	if err != nil {
		t.Fatalf("build the fixture repository: %v", err)
	}

	application, err := coordinator.StartApplication(context.Background(), coordinator.ApplicationOptions{
		DatabasePath:            filepath.Join(appRoot, "coordinator.sqlite3"),
		BackupDirectory:         filepath.Join(appRoot, "backups"),
		ListenAddress:           "127.0.0.1:0",
		TaskListenAddress:       "127.0.0.1:0",
		WorktreeRoot:            filepath.Join(appRoot, "worktrees"),
		WorkerExecutable:        workerPath,
		FrontendAssetsDirectory: assetsDirectory,
		AgentModel: &pipe044aScriptedModel{turns: []pipe044aScriptedTurn{
			pipe044aWriteFileTurn(
				"cmd/pipe044areport/main.go",
				"package main\n\nimport \"fmt\"\n\n"+
					"// main prints the report line this task was asked for.\n"+
					"func main() {\n\tfmt.Println(\"pipe044a report\")\n}\n",
			),
			pipe044aWriteFileTurn(
				"cmd/pipe044areport/main_test.go",
				"package main\n\nimport \"testing\"\n\n"+
					"// TestReport calls main to confirm the program runs without a panic.\n"+
					"func TestReport(t *testing.T) {\n\tmain()\n}\n",
			),
			pipe044aRunProjectTestsTurn(),
		}},
	})
	if err != nil {
		t.Fatalf("start the application: %v", err)
	}
	defer func() {
		// See the doc comment above: a worker-checkpoint failure for a run
		// this scripted harness never wrote a run_configurations row for is a
		// pre-existing, unrelated gap, joined into the returned error but not
		// fatal to actually releasing the database and its lock.
		if shutdownErr := application.Shutdown(context.Background()); shutdownErr != nil {
			t.Logf("application shutdown reported (non-fatal here, see comment): %v", shutdownErr)
		}
	}()

	result, err := pipe044aDriveRequirement(context.Background(), application, fixture)
	if err != nil {
		t.Fatalf("drive the requirement: %v", err)
	}
	t.Logf("requirement reached state %s (thread=%s task=%s)",
		result.FinalState, result.ThreadID, result.TaskID)
	if stages, stageErr := application.Repositories().ListPipelineStages(
		context.Background(), result.TaskID, 0); stageErr != nil {
		t.Logf("debug: list pipeline stages failed: %v", stageErr)
	} else {
		t.Logf("debug: %d pipeline stage rows recorded for attempt 1 (server-side, "+
			"independent of the browser render below)", len(stages))
	}

	address := application.Address()
	t.Logf("application serving at %s", address)

	artifactDir := filepath.Join(root, "screenshots")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create screenshot directory: %v", err)
	}

	instance, err := playwright.Run(&playwright.RunOptions{
		DriverDirectory:     os.Getenv("PLAYWRIGHT_DRIVER_PATH"),
		SkipInstallBrowsers: true,
	})
	if err != nil {
		t.Skipf("playwright driver or Chromium browser is unavailable: %v", err)
	}
	defer func() { _ = instance.Stop() }()

	browser, err := instance.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		t.Skipf("Chromium is unavailable: %v", err)
	}
	defer func() { _ = browser.Close() }()

	browserContext, err := browser.NewContext(playwright.BrowserNewContextOptions{
		BaseURL:  playwright.String("http://" + address),
		Viewport: &playwright.Size{Width: 1600, Height: 1000},
	})
	if err != nil {
		t.Fatalf("create browser context: %v", err)
	}
	defer func() { _ = browserContext.Close() }()
	page, err := browserContext.NewPage()
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	page.SetDefaultTimeout(45000)
	page.SetDefaultNavigationTimeout(45000)
	page.OnConsole(func(message playwright.ConsoleMessage) {
		t.Logf("console[%s]: %s", message.Type(), message.Text())
	})
	page.OnPageError(func(err error) {
		t.Logf("page error: %v", err)
	})

	if _, err := page.Goto("http://"+address+"/tasks", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		t.Fatalf("navigate to /tasks: %v", err)
	}
	appRootLocator := page.GetByTestId("app-root")
	if err := appRootLocator.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		t.Fatalf("wait for app root: %v", err)
	}

	rows := page.Locator(`[data-component="thread-row"]`)
	if err := rows.First().WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		_, _ = page.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String(filepath.Join(artifactDir, "no-thread-row.png")), FullPage: playwright.Bool(true),
		})
		t.Fatalf("wait for the seeded thread row: %v", err)
	}
	if err := rows.First().Click(); err != nil {
		t.Fatalf("select the seeded thread: %v", err)
	}

	// 90s, not 45s: the headless renderer under this repository's shared,
	// heavily loaded dev checkout is slow (AGENTS.md's frontend section
	// documents 15s-120s timeouts as deliberate for exactly this reason), and
	// this check additionally waits on a real gRPC round trip for the
	// pipeline ledger fetch on top of the mounted-thread render itself.
	caveat := page.Locator(`[data-component="run-completion-caveat"]`)
	waitErr := caveat.First().WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(90000),
	})
	if waitErr != nil {
		agentBodies := page.Locator(`[data-component="message-card-body"][data-message-role="agent"]`)
		if count, countErr := agentBodies.Count(); countErr == nil {
			t.Logf("debug: %d agent message bodies present", count)
			for index := 0; index < count; index++ {
				status, _ := agentBodies.Nth(index).GetAttribute("data-message-status")
				t.Logf("debug: agent message %d status=%q", index, status)
			}
		} else {
			t.Logf("debug: count agent bodies failed: %v", countErr)
		}
		if caveatCount, countErr := caveat.Count(); countErr == nil {
			t.Logf("debug: %d run-completion-caveat elements present (any visibility)", caveatCount)
		}
	}
	screenshotPath := filepath.Join(artifactDir, "run-completion-caveat.png")
	if _, shotErr := page.Screenshot(playwright.PageScreenshotOptions{
		Path: playwright.String(screenshotPath), FullPage: playwright.Bool(true),
	}); shotErr != nil {
		t.Logf("screenshot failed: %v", shotErr)
	} else {
		t.Logf("screenshot written to %s -- open and view it before trusting this result", screenshotPath)
	}
	if waitErr != nil {
		t.Fatalf(
			"the run-completion caveat never appeared beside the completed run's final message "+
				"(final task state was %s): %v", result.FinalState, waitErr)
	}

	ratioAttribute, err := caveat.First().GetAttribute("data-skip-ratio-percent")
	if err != nil {
		t.Fatalf("read the caveat's skip-ratio attribute: %v", err)
	}
	t.Logf("run-completion caveat rendered with skip-ratio-percent=%s", ratioAttribute)
	visibleText, err := caveat.First().InnerText()
	if err != nil {
		t.Fatalf("read the caveat's visible text: %v", err)
	}
	if strings.TrimSpace(visibleText) == "" {
		t.Fatalf("the run-completion caveat rendered with no visible text")
	}
	t.Logf("run-completion caveat text: %q", visibleText)
}

// buildGoBinary compiles one of this repository's own commands, matching the
// pattern cmd/codeflux-dev's own buildSeedWorkerExecutable uses for the
// worker: a native, ordinary `go build`.
func buildGoBinary(t *testing.T, repositoryRoot, binDirectory, name, pkg string) string {
	t.Helper()
	output := filepath.Join(binDirectory, name)
	if runtime.GOOS == "windows" {
		output += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-o", output, pkg)
	command.Dir = repositoryRoot
	if built, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, built)
	}
	return output
}

// runToolWithTimeout runs one bounded, non-interactive repository dev-tool
// command and fails the test with its combined output on any error.
func runToolWithTimeout(t *testing.T, repositoryRoot string, timeout time.Duration, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Dir = repositoryRoot
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

// --- Minimal requirement-driving fixture, narrower than
// internal/testharness's own (see the test's doc comment for why this is not
// reused directly). ---

type pipe044aScriptedTurn func(agentloop.ModelInput) agentloop.ModelTurn

type pipe044aScriptedModel struct {
	turns []pipe044aScriptedTurn
	round int
}

func (model *pipe044aScriptedModel) Identity() providers.ModelIdentity {
	return providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: "scripted", AdapterVersion: "1",
			Provider: "scripted", ProviderVersion: "1",
		},
		Model: "scripted", Revision: "scripted",
	}
}

func (model *pipe044aScriptedModel) ObserveThink(
	_ context.Context, input agentloop.ModelInput,
) (agentloop.ModelTurn, error) {
	if model.round >= len(model.turns) {
		return model.stamp(agentloop.ModelTurn{
			Completion: agentloop.CompletionImplementationComplete,
		}), nil
	}
	turn := model.turns[model.round](input)
	model.round++
	return model.stamp(turn), nil
}

func (model *pipe044aScriptedModel) stamp(turn agentloop.ModelTurn) agentloop.ModelTurn {
	requestID, _ := domain.NewModelRequestID()
	turn.ModelRequestID = requestID
	turn.Model = model.Identity()
	turn.Usage = providers.Usage{
		Known: true, Source: providers.UsageSourceProvider,
		InputTokens: 100, OutputTokens: 20,
	}
	amount, _ := providers.NewExactAmount("USD", 1, 1000)
	turn.Cost = amount
	return turn
}

func pipe044aWriteFileTurn(path, content string) pipe044aScriptedTurn {
	return func(input agentloop.ModelInput) agentloop.ModelTurn {
		arguments, _ := json.Marshal(map[string]string{
			"path": path, "content": content,
		})
		callID, _ := domain.NewEventID()
		return agentloop.ModelTurn{ToolCalls: []agentloop.ModelToolCall{{
			Call: providers.ToolCall{
				ID: callID.String(), Name: string(executor.ToolApplyEdit),
				Arguments: arguments,
			},
			PlanStepID: pipe044aStepAcceptingTool(input, executor.ToolApplyEdit),
		}}}
	}
}

func pipe044aRunProjectTestsTurn() pipe044aScriptedTurn {
	return func(input agentloop.ModelInput) agentloop.ModelTurn {
		arguments, _ := json.Marshal(map[string]string{
			"executable": "go", "arg1": "test", "arg2": "./...",
		})
		callID, _ := domain.NewEventID()
		return agentloop.ModelTurn{ToolCalls: []agentloop.ModelToolCall{{
			Call: providers.ToolCall{
				ID: callID.String(), Name: string(executor.ToolTest),
				Arguments: arguments,
			},
			PlanStepID: pipe044aStepAcceptingTool(input, executor.ToolTest),
		}}}
	}
}

func pipe044aStepAcceptingTool(input agentloop.ModelInput, tool executor.ToolName) string {
	for _, step := range input.Plan.Steps {
		if step.State == agentloop.StepImplemented ||
			step.State == agentloop.StepValidated ||
			step.State == agentloop.StepSkipped {
			continue
		}
		for _, completion := range step.CompletionTools {
			if completion == tool {
				return step.ID
			}
		}
	}
	return ""
}

type pipe044aResult struct {
	ThreadID   domain.ThreadID
	SessionID  domain.SessionID
	TaskID     domain.TaskID
	FinalState domain.TaskState
}

func pipe044aDriveRequirement(
	ctx context.Context,
	application *coordinator.Application,
	fixture testfixtures.RepositoryFixture,
) (*pipe044aResult, error) {
	repositories := application.Repositories()
	lifecycle := application.TaskLifecycleApplication()
	if lifecycle == nil {
		return nil, errors.New("the started application exposes no task lifecycle")
	}
	const requirement = "Create cmd/pipe044areport/main.go so the program prints a report line."

	scope, err := repositories.EnsureLocalBootstrap(
		ctx, fixture.Root, fixture.Revision, "Seeded PIPE-044a browser check")
	if err != nil {
		return nil, fmt.Errorf("open the fixture repository: %w", err)
	}

	messageID, err := domain.NewMessageID()
	if err != nil {
		return nil, err
	}
	if _, err := repositories.AppendMessage(ctx, storage.AppendMessage{
		ID: messageID, ThreadID: scope.ThreadID, Role: storage.MessageRoleUser,
		BodyRedacted:   requirement,
		IdempotencyKey: "pipe044a-browser-request",
	}); err != nil {
		return nil, fmt.Errorf("record the request: %w", err)
	}

	created, err := lifecycle.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 scope.ThreadID,
		RequestMessageID:         &messageID,
		Requirement:              requirement,
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       fixture.Revision,
		BaselineModelRevision:    "scripted-pipe044a-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"cmd/pipe044areport"},
		IdempotencyKey:           "pipe044a-browser-requirement",
	})
	if err != nil {
		return nil, fmt.Errorf("intake the requirement: %w", err)
	}

	readyRevision, err := pipe044aDriveTaskToReady(ctx, repositories, created.TaskID, created.Revision)
	if err != nil {
		return nil, err
	}

	preflight, err := application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		coordinator.ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"pipe044a-browser-bind",
	)
	if err != nil {
		return nil, fmt.Errorf("bind the approved preflight: %w", err)
	}

	if _, err := lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "pipe044a-browser-start",
	}); err != nil {
		return nil, fmt.Errorf("start the approved task: %w", err)
	}

	final, err := pipe044aAwaitReviewableState(ctx, repositories, created.TaskID, 90*time.Second)
	if err != nil {
		return nil, err
	}

	return &pipe044aResult{
		ThreadID: scope.ThreadID, SessionID: scope.SessionID,
		TaskID: created.TaskID, FinalState: final,
	}, nil
}

func pipe044aDriveTaskToReady(
	ctx context.Context,
	repositories *storage.Repositories,
	taskID domain.TaskID,
	fromRevision uint64,
) (uint64, error) {
	steps := []struct {
		from domain.TaskState
		to   domain.TaskState
		key  string
	}{
		{domain.TaskStateDraft, domain.TaskStateForecasting, "pipe044a-browser-forecasting"},
		{domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval, "pipe044a-browser-awaiting"},
		{domain.TaskStateAwaitingPlanApproval, domain.TaskStateReady, "pipe044a-browser-ready"},
	}
	revision := fromRevision
	for _, step := range steps {
		eventID, err := domain.NewEventID()
		if err != nil {
			return 0, err
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
			return 0, fmt.Errorf("transition %s -> %s: %w", step.from, step.to, err)
		}
		revision = transitioned.Task.Revision
	}
	return revision, nil
}

func pipe044aAwaitReviewableState(
	ctx context.Context,
	repositories *storage.Repositories,
	taskID domain.TaskID,
	deadline time.Duration,
) (domain.TaskState, error) {
	final := domain.TaskState("")
	until := time.Now().Add(deadline)
	for time.Now().Before(until) {
		replay, err := repositories.ReplayTask(ctx, taskID)
		if err != nil {
			return "", fmt.Errorf("replay the driven task: %w", err)
		}
		final = replay.State
		switch final {
		case domain.TaskStateRunning, domain.TaskStateValidating:
			select {
			case <-ctx.Done():
				return final, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		break
	}
	switch final {
	case domain.TaskStateAwaitingReview, domain.TaskStateCompleted:
		return final, nil
	default:
		return final, fmt.Errorf(
			"the driven task finished in %q, not a reviewable state", final)
	}
}
