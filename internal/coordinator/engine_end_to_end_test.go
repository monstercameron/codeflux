package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// scriptedEngineModel answers with a fixed sequence of turns.
//
// The engine is what is under test here, not the model. Calling a real
// provider would make this slow, cost money on every run, and turn a
// deterministic check of the machinery into a sample of one model's mood.
// Every turn it returns is shaped exactly like a real one — an identity, a
// request identity, known usage, and an exact cost — because the loop refuses
// turns carrying less, and a fixture that skipped them would prove nothing.
type scriptedEngineModel struct {
	turns []func(agentloop.ModelInput) agentloop.ModelTurn
	round int
}

func (model *scriptedEngineModel) Identity() providers.ModelIdentity {
	return providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: "scripted", AdapterVersion: "1",
			Provider: "scripted", ProviderVersion: "1",
		},
		Model: "scripted", Revision: "scripted",
	}
}

func (model *scriptedEngineModel) ObserveThink(
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

// stamp adds the accounting every turn must carry.
func (model *scriptedEngineModel) stamp(turn agentloop.ModelTurn) agentloop.ModelTurn {
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

// writeFile is one scripted turn that edits a file.
func writeFile(path, content string) func(agentloop.ModelInput) agentloop.ModelTurn {
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
			PlanStepID: stepAccepting(input, executor.ToolApplyEdit),
		}}}
	}
}

// stepAccepting names an open plan step that can accept one tool.
//
// A call attributed to a step that cannot complete with that tool is refused
// by the loop, which is the same rule a real model has to satisfy.
func stepAccepting(input agentloop.ModelInput, tool executor.ToolName) string {
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

// TestTheEngineCarriesARequirementThroughToRealWorkOnDisk is the engine's own
// end-to-end check, with no interface anywhere in it.
//
// It drives the real booted coordinator over real SQLite — intake, forecast,
// approval, preflight binding, start, a real Git worktree, the real agent
// loop, the real tool executor, and the durable journals — then asserts on
// what is actually on disk and actually in the database. Nothing is stubbed
// except the model's answers.
func TestTheEngineCarriesARequirementThroughToRealWorkOnDisk(t *testing.T) {
	const requirement = "Create cmd/hello/main.go so the program prints a greeting."
	const program = "package main\n\nimport \"fmt\"\n\n" +
		"func main() {\n\tfmt.Println(\"hello\")\n}\n"

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			writeFile("cmd/hello/main.go", program),
		},
	})
	ctx := context.Background()

	// 1. A person asks for something, and it becomes a forecasted task.
	requestID := engine.request(t, requirement)
	created, err := engine.lifecycle.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 engine.threadID,
		RequestMessageID:         &requestID,
		Requirement:              requirement,
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("1", 40),
		BaselineModelRevision:    "scripted-provider-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"cmd/hello"},
		IdempotencyKey:           "engine-requirement",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}

	// 2. A person approves it, and the exact reviewed preflight is bound.
	readyRevision := driveTaskToReady(t, engine.repositories, created.TaskID, created.Revision)
	preflight, err := engine.application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"engine-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}

	// 3. Starting it creates the worktree and hands the work to the agent.
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "engine-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}
	binding, err := engine.repositories.GetWorktreeBinding(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("starting a task must create its worktree: %v", err)
	}

	// The agent runs detached, so the durable record is polled rather than
	// waited on. That record is the authority: a run reporting success and
	// leaving nothing behind is exactly the failure this test exists to catch.
	written := filepath.Join(binding.WorktreePath, "cmd", "hello", "main.go")
	engine.waitFor(t, "the agent to write the file it was asked for", func() bool {
		_, err := os.Stat(written)
		return err == nil
	})
	produced, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("the agent reported work it did not do: %v", err)
	}
	if string(produced) != program {
		t.Errorf("the file on disk is not what the tool was given:\n%s", produced)
	}

	// 4. What it did was recorded: the requirement it worked from, the context
	//    it decided against, the plan it followed, the run bound to that plan,
	//    and a diagram of the work with the operation attributed to its step.
	//    It also recorded the provider it sent the work to and every tool it
	//    asked for, which is what makes a finished run auditable rather than
	//    merely finished.
	engine.waitFor(t, "the run to draw what it did", func() bool {
		return engine.rows("graph_edges") > 0 &&
			engine.rows("agent_tool_results") > 0
	})
	// The code itself must survive the worktree. A run that wrote a file into a
	// temporary directory and recorded only that it had done so leaves the
	// database able to say a file exists and unable to say what is in it.
	stored, err := engine.repositories.ListTaskArtifacts(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("reading what the run produced failed: %v", err)
	}
	if len(stored) == 0 {
		t.Fatal("the run wrote a program and stored none of it")
	}
	if string(stored[0].Content) != program {
		t.Errorf("the stored artifact is not the program that was written:\n%s",
			stored[0].Content)
	}
	if stored[0].MediaType != "text/x-go" {
		t.Errorf("a Go file was stored as %q", stored[0].MediaType)
	}

	for table, want := range map[string]int{
		"artifacts":                  1,
		"task_requirement_revisions": 1,
		"context_manifests":          1,
		"agent_plan_revisions":       1,
		"agent_plan_steps":           1,
		"run_plan_bindings":          1,
		"providers":                  1,
		"provider_logical_requests":  1,
		"agent_tool_requests":        1,
		"agent_tool_results":         1,
		"graph_nodes":                2,
		"graph_edges":                1,
		"graph_plan_bindings":        1,
		"graph_node_plan_step_links": 1,
	} {
		if got := engine.rows(table); got < want {
			t.Errorf("%s holds %d row(s), want at least %d.\nThe run said:\n%s",
				table, got, want, engine.narration())
		}
	}
}

// engineFixture is a booted coordinator with one repository and thread open.
type engineFixture struct {
	application  *Application
	repositories *storage.Repositories
	lifecycle    *TaskLifecycleAdapter
	threadID     domain.ThreadID
	repository   string
}

// startEngineFixture boots the real application against a real repository.
// startEngineFixture starts the product against one pinned model.
//
// A run built this way cannot escalate, which is right for a test driving a
// scripted model: escalation would build a second model the script knows
// nothing about. Tests that need the escalation path use
// startEscalatingEngineFixture.
func startEngineFixture(t *testing.T, model agentloop.FixedModel) engineFixture {
	t.Helper()
	return startEngineWith(t, ApplicationOptions{AgentModel: model})
}

// startEscalatingEngineFixture starts the product the way it ships: able to
// build any model on its ladder, and so able to climb it when a run stalls.
func startEscalatingEngineFixture(
	t *testing.T,
	factory func(named string) (agentloop.FixedModel, error),
) engineFixture {
	t.Helper()
	return startEngineWith(t, ApplicationOptions{AgentModelFactory: factory})
}

// durableEngineRootEnvironment names a directory the engine fixture should
// build its project under instead of a temporary one.
//
// t.TempDir is deleted when the test ends, which is right for a suite and
// wrong for the one case where the run itself is the thing being examined: a
// ladder rung whose database somebody wants to open afterwards, inspect, or
// serve to the browser client. Without this the atoms and memories a real run
// deposits are unreachable the moment the run that deposited them returns.
const durableEngineRootEnvironment = "CODEFLUX_ENGINE_ROOT"

// durableEngineRoot is the project root for one engine fixture.
//
// It is a temporary directory unless the environment names somewhere durable,
// and it refuses a non-empty durable directory rather than running against
// whatever a previous run left there: a shared-ladder run's whole point is
// that what it finds was put there by its own earlier rungs, and silently
// inheriting an unrelated project would make the recall stage's account of
// what it searched a lie.
func durableEngineRoot(t *testing.T) string {
	t.Helper()
	named := strings.TrimSpace(os.Getenv(durableEngineRootEnvironment))
	if named == "" {
		return t.TempDir()
	}
	if err := os.MkdirAll(named, 0o755); err != nil {
		t.Fatalf("create the durable engine root: %v", err)
	}
	entries, err := os.ReadDir(named)
	if err != nil {
		t.Fatalf("read the durable engine root: %v", err)
	}
	if len(entries) > 0 {
		t.Fatalf("the durable engine root %s is not empty: %d entry(s). "+
			"Remove it first, so what this run finds is what this run put "+
			"there", named, len(entries))
	}
	t.Logf("engine root: %s, kept rather than removed, so the database this "+
		"run writes can be opened afterwards", named)
	return named
}

// startEngineWith is the shared body, given whichever model arrangement.
func startEngineWith(t *testing.T, agent ApplicationOptions) engineFixture {
	t.Helper()
	root := durableEngineRoot(t)
	repositoryPath := filepath.Join(root, "repo")
	initializeCoordinatorGitRepository(t, repositoryPath)

	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		WorktreeRoot:      filepath.Join(root, "worktrees"),
		WorkerExecutable:  buildCoordinatorWorkerExecutable(t),
		TaskControls:      &applicationTaskControlStub{},
		AgentModel:        agent.AgentModel,
		AgentModelFactory: agent.AgentModelFactory,
		CredentialStore:   agent.CredentialStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	// The scope is opened the way starting the product opens it, rather than by
	// inserting rows. A hand-built thread has no session, and a run resolves its
	// scope through one: assembling the pieces here would test a shape the
	// product never actually produces.
	scope, err := application.repos.EnsureLocalBootstrap(
		context.Background(), repositoryPath, strings.Repeat("f", 40), "Engine")
	if err != nil {
		t.Fatalf("opening the repository failed: %v", err)
	}
	return engineFixture{
		application: application, repositories: application.repos,
		lifecycle: application.TaskLifecycleApplication(),
		threadID:  scope.ThreadID, repository: repositoryPath,
	}
}

// request records what the person asked for, as a message in the conversation.
//
// A task carries the identity of the message it came from, and the run reads
// its requirement back out of that message. A task created without one runs
// against an empty requirement, plans nothing, and reports success.
func (engine engineFixture) request(t *testing.T, body string) domain.MessageID {
	t.Helper()
	messageID, err := domain.NewMessageID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.repositories.AppendMessage(
		context.Background(), storage.AppendMessage{
			ID: messageID, ThreadID: engine.threadID, Role: storage.MessageRoleUser,
			BodyRedacted: body,
			// Keyed on the request itself, not a constant.
			//
			// It was "engine-request" for every message, so the second rung of
			// a shared ladder collided with the first: "idempotency key
			// belongs to different message content", refused before the rung
			// began. Shared mode is the only mode in which atoms and lessons
			// accumulate across tasks, so the compounding-effort thesis could
			// not be exercised at all — and the recall stage's report that the
			// project held no earlier work was true because no second rung had
			// ever run.
			//
			// Hashing the body gives idempotency its actual meaning here: the
			// same request twice is one message, and two different requests
			// are two.
			IdempotencyKey: "engine-request:" + shortDigest(body),
		},
	); err != nil {
		t.Fatalf("recording the request failed: %v", err)
	}
	return messageID
}

// rows reports how many rows one table holds, reporting none on error.
func (engine engineFixture) rows(table string) int {
	count, err := engine.repositories.CountRowsForTest(table)
	if err != nil {
		return 0
	}
	return count
}

// waitFor polls until something the agent does in the background happens.
//
// On timeout it reports what the run itself said, because the engine narrates
// its own failures into the conversation, and that narration is far more use
// than the name of a condition that never came true.
func (engine engineFixture) waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	// A run can now legitimately take much longer than it used to: it plans on
	// a high-effort model before writing anything, and a request that stalls
	// climbs four rungs with a fresh attempt allowance on each. Five minutes
	// was the budget for a single-model run with six attempts, and holding a
	// four-rung run to it would report the ladder as broken every time it did
	// the thing it was built to do.
	const budget = 30 * time.Minute
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("waited %s for %s.\nThe run said:\n%s",
		budget, what, engine.narration())
}

// narration is what the run reported into the conversation.
func (engine engineFixture) narration() string {
	page, err := engine.repositories.ListMessages(context.Background(), storage.ListMessages{
		ThreadID: engine.threadID, Limit: 40,
	})
	if err != nil {
		return "  (the conversation could not be read: " + err.Error() + ")"
	}
	var report strings.Builder
	for _, message := range page.Messages {
		fmt.Fprintf(&report, "  %s: %s\n", message.Role, message.BodyRedacted)
	}
	if report.Len() == 0 {
		return "  (nothing at all)"
	}
	return report.String()
}

// shortDigest is a stable identity for one request body.
func shortDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:8])
}
