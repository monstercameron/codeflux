//go:build integration

package coordinator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// startEngineFixtureWithSettings is startEngineFixture/startEscalatingEngineFixture
// with an explicit pipeline.Settings, for tests in this file that need to
// prove something about a setting's effect on the running engine rather than
// about its default behaviour -- in particular, a small StallBeforeEscalation
// so a repeated review finding escalates the ladder in one repetition instead
// of pipeline.DefaultSettings' three, which is the difference between this
// suite finishing in seconds and it needing several real attempt cycles
// (worktree, worker subprocess, go build, go test, each one) under whatever
// contention the shared machine has at the moment.
//
// It duplicates startEngineWith's body (rather than changing that shared
// helper's signature, which every other engine test in this package also
// calls) because ApplicationOptions.Settings is a real field
// buildAgentExecution already reads -- see settingsOrDefaults's call site in
// agent_execution.go -- so this is the same production wiring every other
// engine fixture uses, only naming the fields the others leave zero.
func startEngineFixtureWithSettings(
	t *testing.T,
	agent ApplicationOptions,
	settings pipeline.Settings,
) engineFixture {
	t.Helper()
	root := t.TempDir()
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
		Settings:          settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
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

// --- PIPE-099 / PIPE-100 / PIPE-102: the review's send-back reaches the
// convergence tracker through the production path. ---
//
// Before this change, a review finding set failure and continued directly
// (agent_execution.go's review block), never calling sendBack. That meant
// progress.record was never invoked for it, so a run repeating the exact
// same review finding attempt after attempt neither escalated up the model
// ladder nor decomposed -- the one property the rest of the attempt loop is
// built to guarantee (agent_convergence.go's whole reason to exist) was
// invisible to this one gate. It also meant the review ran only when
// execution.settings.AdversarialReview was true (PIPE-102: plan.md §22
// forbids trading the reviewer away for a lower cost), and that the
// "reviewed" round-spent flag could in principle be set by a path that never
// actually sent anything back (PIPE-100).
//
// stubbornlyFlawedModel below never fixes the defect it is reviewed for --
// unlike refiningModel in engine_refinement_test.go, it ignores whatever
// context item PIPE-099's send-back attaches and rewrites the exact same
// flawed file every attempt. Real code, a real Git worktree, the real
// adversarial review (package-level mutable state, an anti-pattern
// findAntiPatternFindings raises for real, deterministically, with no
// mutation testing involved), and the real convergence tracker. The only
// thing scripted is the model's answers, exactly as every other engine test
// in this package scripts them -- nothing here can reach a real provider or
// spend money.

// stubbornlyFlawedModel writes the same package-level-mutable-state defect on
// every attempt regardless of what the run tells it, so the adversarial
// review's finding never changes: the mutable-global anti-pattern named at
// the same file, the same line, every single time.
type stubbornlyFlawedModel struct {
	scriptedEngineModel
	sourcePath, sourceBody string
	testPath, testBody     string
}

func (model *stubbornlyFlawedModel) ObserveThink(
	_ context.Context, input agentloop.ModelInput,
) (agentloop.ModelTurn, error) {
	for _, step := range input.Plan.Steps {
		if step.State != agentloop.StepPending && step.State != agentloop.StepInProgress {
			continue
		}
		for _, tool := range step.CompletionTools {
			switch tool {
			case executor.ToolApplyEdit:
				if len(step.ExpectedFiles) == 0 {
					continue
				}
				path := step.ExpectedFiles[0]
				content := model.sourceBody
				if path == model.testPath {
					content = model.testBody
				}
				return model.stamp(writeFile(path, content)(input)), nil
			case executor.ToolTest:
				return model.stamp(runTests(input)), nil
			}
		}
	}
	return model.stamp(agentloop.ModelTurn{
		Completion: agentloop.CompletionImplementationComplete,
	}), nil
}

// TestAdversarialReviewSendBackEscalatesTheModelLadderOnRepetition is the
// discriminating case for PIPE-099: a run stuck on the identical review
// finding for StallBeforeEscalation (3, pipeline.DefaultSettings) attempts
// in a row must climb the ladder through the real sendBack/convergence path,
// exactly as an identically-repeating compile or test failure already does.
// Reverting the review block to set failure and continue directly, without
// calling sendBack, makes this run forever on rung one instead.
func TestAdversarialReviewSendBackEscalatesTheModelLadderOnRepetition(t *testing.T) {
	const requirement = "Create pkg/work/work.go and pkg/work/work_test.go so that Greet returns hello."
	const source = "package work\n\n" +
		// A package-level mutable variable: findMutableGlobals's exact,
		// deterministic trigger (agent_stage_antipatterns.go), not a
		// sentinel and not requiring mutation testing to detect, so this
		// finding is identical, cheap, and reproducible on every attempt.
		"var greeting = \"hello\"\n\n" +
		"// Greet returns the greeting this program exists to produce.\n" +
		"func Greet() string {\n\treturn greeting\n}\n"
	const test = "package work\n\nimport \"testing\"\n\n" +
		"func TestGreet(t *testing.T) {\n" +
		"\tif Greet() != \"hello\" {\n\t\tt.Fatalf(\"Greet() = %q\", Greet())\n\t}\n}\n"

	model := &stubbornlyFlawedModel{
		sourcePath: "pkg/work/work.go", sourceBody: source,
		testPath: "pkg/work/work_test.go", testBody: test,
	}
	// A model factory, not a fixed model: this test needs execution.escalate
	// to be non-nil so sendBack's escalation branch in agent_execution.go can
	// actually build a stronger model, exactly as a real run does. The
	// factory hands back the same scripted model for every rung name -- what
	// is under test is that the coordinator asks for one, not which one a
	// real ladder would have chosen.
	//
	// StallBeforeEscalation is lowered from pipeline.DefaultSettings' 3 to 1:
	// the property under test is that a repeated review finding reaches
	// escalation at all, which one repetition already proves, and three real
	// attempt cycles -- worktree, worker subprocess, go build, go test, each
	// one -- cost real minutes under this machine's shared load.
	fastEscalation := pipeline.DefaultSettings()
	fastEscalation.StallBeforeEscalation = 1
	engine := startEngineFixtureWithSettings(t, ApplicationOptions{
		AgentModelFactory: func(string) (agentloop.FixedModel, error) {
			return model, nil
		},
	}, fastEscalation)
	ctx := context.Background()
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
		AffectedPackages:         []string{"pkg/work"},
		IdempotencyKey:           "engine-adversarial-sendback",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}
	readyRevision := driveTaskToReady(t, engine.repositories, created.TaskID, created.Revision)
	preflight, err := engine.application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"engine-adversarial-sendback-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "engine-adversarial-sendback-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	// The run either finishes (having escalated along the way, since the
	// model never stops producing the defect) or reports it is awaiting a
	// decision -- both are terminal narrations this fixture's model can
	// reach; either is enough to read the timeline for what happened before
	// that point.
	engine.waitFor(t, "the run to finish or stop for a decision", func() bool {
		narration := engine.narration()
		return strings.Contains(narration, "Finished:") ||
			strings.Contains(narration, "moving to")
	})
	narration := engine.narration()
	t.Logf("the run reported:\n%s", narration)

	// 1. The same review finding was actually seen more than once. A run
	//    that only ever saw it on attempt one proves nothing about
	//    repetition.
	if !strings.Contains(narration, "Attempt 2") {
		t.Fatal("the run never took a second attempt, so repeated review " +
			"findings were never exercised")
	}
	// The finding's own text ("package-level variable greeting...") travels
	// to the model as repository context (agent_execution.go's
	// last-test-run-output item), not as a published chat message, so it is
	// not asserted on here; what the chat narration does carry, and what is
	// asserted on, is the review having found a real defect rather than
	// nothing.
	if !strings.Contains(narration, "A review found 1 defect(s)") {
		t.Fatalf("the review never reported finding the scripted anti-pattern "+
			"as a defect, so nothing here exercised the send-back path:\n%s",
			narration)
	}

	// 2. The escalation the convergence tracker performs on a real repeated
	// gate failure fired for this one too. This is PIPE-099's exact claim:
	// the review's send-back reaches the same escalation machinery a
	// repeated compile or test failure already does. Convergence.record's
	// own Why text ("...moving to...") is asserted on literally, not
	// inferred, because that sentence is written only from inside sendBack's
	// escalation branch -- nothing else in this run's vocabulary produces it.
	if !strings.Contains(narration, "moving to") {
		t.Fatalf("the run repeated the identical review finding at least "+
			"three times and never escalated up the model ladder -- the "+
			"review's send-back is not reaching the convergence tracker:\n%s",
			narration)
	}
	if !strings.Contains(narration, "the same check failed the same way") {
		t.Fatalf("an escalation happened, but not the one this finding "+
			"should have caused:\n%s", narration)
	}
}

// TestAdversarialReviewRunsRegardlessOfTheAdversarialReviewSetting is the
// discriminating case for PIPE-102: turning execution.settings.AdversarialReview
// off must not delete the reviewer. Before this change, the whole review
// block in agent_execution.go was gated behind that flag, so a run configured
// with it false shipped a program carrying an anti-pattern the review exists
// to catch, entirely unreviewed.
func TestAdversarialReviewRunsRegardlessOfTheAdversarialReviewSetting(t *testing.T) {
	const requirement = "Create pkg/work/work.go and pkg/work/work_test.go so that Greet returns hello."
	const source = "package work\n\n" +
		"var greeting = \"hello\"\n\n" +
		"// Greet returns the greeting this program exists to produce.\n" +
		"func Greet() string {\n\treturn greeting\n}\n"
	const test = "package work\n\nimport \"testing\"\n\n" +
		"func TestGreet(t *testing.T) {\n" +
		"\tif Greet() != \"hello\" {\n\t\tt.Fatalf(\"Greet() = %q\", Greet())\n\t}\n}\n"

	model := &stubbornlyFlawedModel{
		sourcePath: "pkg/work/work.go", sourceBody: source,
		testPath: "pkg/work/work_test.go", testBody: test,
	}
	// AdversarialReview is disabled through the real production wiring:
	// ApplicationOptions.Settings flows straight into buildAgentExecution's
	// settingsOrDefaults(options.Settings) → AgentExecution.WithSettings, the
	// same path a person's saved configuration reaches the engine through.
	// Before PIPE-102 this switch, on its own, was enough to stop
	// agent_execution.go's review block from ever running at all.
	disabled := pipeline.DefaultSettings()
	disabled.AdversarialReview = false
	// Lowered for the same reason TestAdversarialReviewSendBackEscalatesTheModelLadderOnRepetition
	// lowers it: this test only needs the review to have run once, and a
	// smaller StallBeforeEscalation reaches a readable terminal narration in
	// one repetition instead of pipeline.DefaultSettings' three.
	disabled.StallBeforeEscalation = 1
	// A factory, not a fixed AgentModel: with a fixed model, execution.escalate
	// stays nil, and sendBack's escalation branch is skipped entirely when
	// there is no stronger model to build -- the run then just keeps
	// re-attempting on the same rung until it exhausts its whole attempt
	// ceiling, which is real but unrelated to what this test is proving. A
	// factory keeps escalation reachable so the run reaches a readable
	// terminal narration quickly, the same way the sibling escalation test
	// does.
	engine := startEngineFixtureWithSettings(t, ApplicationOptions{
		AgentModelFactory: func(string) (agentloop.FixedModel, error) {
			return model, nil
		},
	}, disabled)
	ctx := context.Background()

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
		AffectedPackages:         []string{"pkg/work"},
		IdempotencyKey:           "engine-adversarial-not-a-switch",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}
	readyRevision := driveTaskToReady(t, engine.repositories, created.TaskID, created.Revision)
	preflight, err := engine.application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"engine-adversarial-not-a-switch-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "engine-adversarial-not-a-switch-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}
	engine.waitFor(t, "the run to finish or stop for a decision", func() bool {
		narration := engine.narration()
		return strings.Contains(narration, "Finished:") ||
			strings.Contains(narration, "moving to")
	})
	narration := engine.narration()
	t.Logf("the run reported:\n%s", narration)
	if !strings.Contains(narration, "A review found 1 defect(s)") {
		t.Fatalf("AdversarialReview was set to false and the reviewer never "+
			"ran at all -- the setting deleted the reviewer instead of only "+
			"scaling it:\n%s", narration)
	}
}
