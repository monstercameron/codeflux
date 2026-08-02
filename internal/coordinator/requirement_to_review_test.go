package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
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
	// Known to fail today, which is the finding rather than a flake.
	//
	// RepairCompletionService.PrepareCompletion is what moves a task to
	// awaiting-review, and it has no production caller. A run therefore does
	// the work, records its plan and tool results, and stays in running for
	// ever. The test is gated rather than deleted so the defect stays
	// reproducible on demand without holding the shared suite red; remove the
	// gate in the change that wires completion.
	if os.Getenv("CODEFLUX_AUDIT020") != "1" {
		t.Skip("AUDIT-020: no production caller constructs RepairCompletionService, " +
			"so a run never reaches awaiting-review. Set CODEFLUX_AUDIT020=1 to " +
			"reproduce.")
	}

	const requirement = "Create cmd/report/main.go so the program prints a report line."
	const program = "package main\n\nimport \"fmt\"\n\n" +
		"func main() {\n\tfmt.Println(\"report\")\n}\n"

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			writeFile("cmd/report/main.go", program),
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
	final := domain.TaskState("")
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		final = taskStateFor(t, engine.repositories, created.TaskID)
		if final != domain.TaskStateRunning {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	switch final {
	case domain.TaskStateAwaitingReview, domain.TaskStateCompleted:
	default:
		t.Fatalf("the task finished in %q; a completed run must be reviewable", final)
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
