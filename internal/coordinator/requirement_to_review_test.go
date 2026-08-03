package coordinator

import (
	"context"
	"encoding/json"
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

// TestAUDIT020_ARequirementReachesAwaitingReviewThroughTheRealApplication
// covers AUDIT-020, reconciling M14-021.
//
// TestRequirementReachesAStartedRunThroughTheRealApplication stops once a
// worktree exists. That proves the run was scheduled and a worker was asked
// for; it does not prove anything then happened, so the run it accepts is a
// recorded-but-idle one. M14-021 claims the observe-think-act-result loop
// around the fixed provider.
//
// This carries the same journey through to the end: the loop acts, the work
// lands on disk, tool results and a plan are recorded, and the task reaches a
// terminal reviewable state rather than sitting in running.
func TestAUDIT020_ARequirementReachesAwaitingReviewThroughTheRealApplication(t *testing.T) {
	// The gate is removed (2026-08-02): completion is wired —
	// buildAgentExecutionCompletion constructs RepairCompletionService in
	// buildAgentExecution, and AgentExecution.Run calls
	// completeRunIfPossible at the end of every run — and this
	// reproduction now passes in single-digit seconds, not the 480+ second
	// hang the previous attempt left behind. See TODOS.md AUDIT-020 for the
	// measured evidence and what actually caused the earlier hang: not the
	// re-run "go test ./..." the first attempt suspected, but a
	// tool-request ID mismatch between agentToolJournal and
	// AgentExecutionPersistence that made every run's first plan-step
	// transition fail its durable consistency trigger and left the run
	// polling forever for a file that could never be written.

	const requirement = "Create cmd/report/main.go so the program prints a report line."
	// Both functions carry a doc comment on purpose: the loop's own
	// completeness gate (agent_stage_completeness.go) refuses to call
	// implementation complete over an undocumented function, and this
	// fixture's model is scripted rather than real, so it cannot respond to
	// that feedback the way a real model would. Content that would earn a
	// "go back for this" round elsewhere in the test suite (see
	// agent_convergence_test.go's "1 function has no doc comment: main")
	// would leave this fixture cycling through its retry ladder forever
	// instead of reaching awaiting-review.
	const program = "package main\n\nimport \"fmt\"\n\n" +
		"// main prints the report line this task was asked for.\n" +
		"func main() {\n\tfmt.Println(\"report\")\n}\n"
	const programTest = "package main\n\nimport \"testing\"\n\n" +
		"// TestReport calls main to confirm the program runs without a panic.\n" +
		"func TestReport(t *testing.T) {\n\tmain()\n}\n"

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			// A real requirement's plan names an edit step per file
			// (agentPlanSteps/withTestsFor adds a companion test file
			// automatically) and a verify step that runs the project's own
			// tests. completeRunIfPossible only attempts completion once the
			// run's own required validation already passed (compiles &&
			// verified), so the scripted run has to actually call the test
			// tool — writing the file alone is not enough for this test.
			writeFile("cmd/report/main.go", program),
			writeFile("cmd/report/main_test.go", programTest),
			runFinalTests,
		},
	})
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
		AffectedPackages:         []string{"cmd/report"},
		IdempotencyKey:           "audit020-requirement",
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
		"audit020-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "audit020-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	binding, err := engine.repositories.GetWorktreeBinding(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("starting a task must create its worktree: %v", err)
	}

	// The link the original test stops before: the loop acted.
	written := filepath.Join(binding.WorktreePath, "cmd", "report", "main.go")
	engine.waitFor(t, "the agent to write the file it was asked for", func() bool {
		_, statErr := os.Stat(written)
		return statErr == nil
	})
	produced, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("the run reported work it did not do: %v", err)
	}
	if string(produced) != program {
		t.Errorf("the file on disk is not what the tool was given:\n%s", produced)
	}

	// Observe-think-act-result leaves durable evidence of each part: a plan it
	// followed and the tool results it produced.
	engine.waitFor(t, "the run to record its plan and tool results", func() bool {
		return engine.rows("agent_plan_revisions") > 0 && engine.rows("agent_tool_results") > 0
	})

	// And the task leaves running. A run that acts and stays running is the
	// state a person cannot act on.
	//
	// The wait is bounded tightly rather than using the fixture's thirty
	// minute budget: by this point the work is already on disk and the plan
	// and tool results are recorded, so the only thing outstanding is a state
	// transition. Waiting half an hour for one would turn a clear defect into
	// a hung suite.
	//
	// "Validating" is deliberately not a stopping condition here, only
	// "running" is: completion is two durable steps (TransitionRunToValidation,
	// then PrepareCompletion), not one, so a task legitimately passes through
	// validating on its way to awaiting-review. Breaking on "not running"
	// caught that transient window under load — a slower machine (this
	// package's full suite sharing a host with concurrent work) stretches it
	// long enough for a 250ms poll to sample the task mid-transition — and
	// reported the transient state as the final one, which is a race in this
	// test, not a defect in the run it is observing.
	final := domain.TaskState("")
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		final = taskStateFor(t, engine.repositories, created.TaskID)
		switch final {
		case domain.TaskStateRunning, domain.TaskStateValidating:
			time.Sleep(250 * time.Millisecond)
			continue
		}
		break
	}
	switch final {
	case domain.TaskStateAwaitingReview, domain.TaskStateCompleted:
	default:
		t.Fatalf("the task finished in %q; a completed run must be reviewable.\nThe run said:\n%s",
			final, engine.narration())
	}
	t.Logf("task %s reached %s", created.TaskID, final)
}

func taskStateFor(
	t *testing.T,
	repositories *storage.Repositories,
	taskID domain.TaskID,
) domain.TaskState {
	t.Helper()
	replay, err := repositories.ReplayTask(t.Context(), taskID)
	if err != nil {
		t.Fatalf("replaying the task: %v", err)
	}
	return replay.State
}

// runFinalTests is one scripted turn that runs the project's own test
// command, attributed to whichever open step accepts it — the plan's verify
// step, in every fixture that uses it.
//
// engine_refinement_test.go declares an equivalent runTests, but it is built
// only under the "integration" tag and so is not available to this file's
// ordinary build.
func runFinalTests(input agentloop.ModelInput) agentloop.ModelTurn {
	arguments, _ := json.Marshal(map[string]string{
		"executable": "go", "arg1": "test", "arg2": "./...",
	})
	callID, _ := domain.NewEventID()
	return agentloop.ModelTurn{ToolCalls: []agentloop.ModelToolCall{{
		Call: providers.ToolCall{
			ID: callID.String(), Name: string(executor.ToolTest),
			Arguments: arguments,
		},
		PlanStepID: stepAccepting(input, executor.ToolTest),
	}}}
}
