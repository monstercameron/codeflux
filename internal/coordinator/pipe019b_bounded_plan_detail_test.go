package coordinator

import (
	"context"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// multiLineAcceptanceRequirement is the shape PIPE-019 made mandatory: prose
// naming a file, then an <<<ACCEPTANCE ... >>> block on its own lines. That
// block leaves the requirement ending in a trailing newline, which is exactly
// what surfaced PIPE-019b once PIPE-019a stopped storage.AnalyzeTaskRequirement
// from refusing it first.
const multiLineAcceptanceRequirement = "Create cmd/answer/main.go so the " +
	"program prints the answer.\n\n<<<ACCEPTANCE\nargs:\nexpected: 42\n>>>\n"

// TestPIPE019b_AgentPlanStepsBoundsTheDetailNotTheRequirement pins the fix at
// the function that produced the defect: agentPlanSteps used to embed
// scope.requirement verbatim into a plan step's DetailRedacted
// ("Write <file> — " + requirement), and storage.AgentPlan.Validate refuses a
// DetailRedacted carrying leading or trailing whitespace, which an appended
// acceptance block reliably leaves behind.
//
// This proves the discriminating claim directly: it drives the exact plan
// construction recordDurablePlan performs (BuildAgentPlan over
// agentPlanSteps' own output) and requires it to succeed. Reverting
// requirementSummary's use in agentPlanSteps back to the raw requirement
// reproduces the original failure exactly — "agent plan step 1 is invalid" —
// confirming this test would have caught it.
func TestPIPE019b_AgentPlanStepsBoundsTheDetailNotTheRequirement(t *testing.T) {
	steps := agentPlanSteps(multiLineAcceptanceRequirement)
	if len(steps) < 2 {
		t.Fatalf("expected at least an edit step and a verify step, got %d",
			len(steps))
	}

	analysis, err := storage.AnalyzeTaskRequirement(multiLineAcceptanceRequirement)
	if err != nil {
		t.Fatalf("a multi-line acceptance requirement must analyse cleanly: %v",
			err)
	}

	files := agentPlanFiles(steps)
	draft := storage.AgentPlanDraft{
		Goal:               shortGraphLabel(multiLineAcceptanceRequirement, "the request"),
		Scope:              files,
		ExpectedFiles:      files,
		ValidationCommands: canonicalTestCommand(),
		CompletionCriteria: []string{"The declared files exist and the tests pass."},
	}
	for _, step := range steps {
		draft.Steps = append(draft.Steps, storage.AgentPlanStepDraft{
			Kind:           durablePlanStepKind(step),
			Title:          shortGraphLabel(step.SummaryRedacted, step.ID),
			DetailRedacted: step.SummaryRedacted,
			ExpectedFiles:  step.ExpectedFiles,
		})
	}

	// This is exactly what recordDurablePlan calls, and exactly where the
	// original defect surfaced: "agent plan step 1 is invalid" from
	// storage.AgentPlan.Validate's whitespace check on DetailRedacted.
	plan, err := storage.BuildAgentPlan(analysis, draft)
	if err != nil {
		t.Fatalf(
			"a multi-line acceptance requirement must reach a bound plan, not "+
				"be refused building it: %v", err)
	}
	if len(plan.Steps) == 0 {
		t.Fatal("the bound plan carries no steps")
	}

	for _, step := range plan.Steps {
		if strings.TrimSpace(step.DetailRedacted) != step.DetailRedacted {
			t.Errorf("step %s detail carries leading/trailing whitespace: %q",
				step.ID, step.DetailRedacted)
		}
		if step.DetailRedacted == "" {
			t.Errorf("step %s detail is empty", step.ID)
		}
		if len(step.DetailRedacted) > 4096 {
			t.Errorf("step %s detail is %d bytes, over the 4096 bound",
				step.ID, len(step.DetailRedacted))
		}
	}

	// The bound is not only about whitespace: the acceptance block itself must
	// not ride along into the label. A step whose detail still carried the
	// block would be "fixed" only by luck of trimming, which is the exact
	// half-measure the ticket calls out as leaving the real defect in place.
	editDetail := plan.Steps[0].DetailRedacted
	if strings.Contains(editDetail, "<<<ACCEPTANCE") {
		t.Errorf("step detail embeds the acceptance block verbatim: %q",
			editDetail)
	}
	if !strings.Contains(editDetail, "Create cmd/answer/main.go") {
		t.Errorf(
			"step detail lost the requirement's own description entirely: %q",
			editDetail)
	}
}

// TestPIPE019b_RequirementSummaryIsAlwaysABoundedLabel exercises
// requirementSummary directly against the shapes that broke agentPlanSteps
// and the shapes that could break it differently: a requirement that is
// nothing but an acceptance block, one whose prose line alone exceeds the
// bound, and one with no acceptance block at all.
func TestPIPE019b_RequirementSummaryIsAlwaysABoundedLabel(t *testing.T) {
	longLine := strings.Repeat("this behaviour matters a great deal ", 20)
	cases := map[string]string{
		"acceptance block only":           "<<<ACCEPTANCE\nargs:\nexpected: 1\n>>>\n",
		"multi-line with block":           multiLineAcceptanceRequirement,
		"prose with no block":             "Make the greeting friendlier.",
		"a very long single line":         longLine,
		"leading and trailing whitespace": "\n\n  Fix the off-by-one bug.  \n\n",
	}
	for name, requirement := range cases {
		t.Run(name, func(t *testing.T) {
			summary := requirementSummary(requirement)
			if summary == "" {
				t.Fatal("summary must never be empty: a plan step's detail " +
					"cannot be blank")
			}
			if strings.TrimSpace(summary) != summary {
				t.Errorf("summary carries leading/trailing whitespace: %q",
					summary)
			}
			if strings.Contains(summary, "<<<ACCEPTANCE") {
				t.Errorf("summary still embeds the acceptance block: %q", summary)
			}
			if strings.Contains(summary, "\n") {
				t.Errorf("summary is not one line: %q", summary)
			}
			const generousBound = 210 // 200 plus the ellipsis rune's bytes
			if len(summary) > generousBound {
				t.Errorf("summary is %d bytes, want at most %d: %q",
					len(summary), generousBound, summary)
			}
		})
	}
}

// TestPIPE019b_MultiLineAcceptanceRequirementReachesABoundPlanThroughRun is
// the end-to-end claim the ticket names directly: the full agent Run() path
// — not the storage layer in isolation — must reach a durably bound plan
// revision for exactly the requirement shape PIPE-019 made mandatory.
//
// Before this fix, recordDurablePlan reached BuildAgentPlan (PIPE-019a closed
// the earlier silent stall) and was refused there: no plan was ever recorded,
// GetCurrentPlanRevision found nothing, and agent_execution.go's own comment
// at the failure site says what a person was told instead — "The plan could
// not be recorded, so this run will not be diagrammed". This proves the
// opposite now holds.
func TestPIPE019b_MultiLineAcceptanceRequirementReachesABoundPlanThroughRun(t *testing.T) {
	const program = "package main\n\nimport \"fmt\"\n\n" +
		"func main() {\n\tfmt.Println(42)\n}\n"

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			writeFile("cmd/answer/main.go", program),
		},
	})
	ctx := context.Background()

	requestID := engine.request(t, multiLineAcceptanceRequirement)
	created, err := engine.lifecycle.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 engine.threadID,
		RequestMessageID:         &requestID,
		Requirement:              multiLineAcceptanceRequirement,
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("1", 40),
		BaselineModelRevision:    "scripted-provider-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"cmd/answer"},
		IdempotencyKey:           "pipe019b-multiline-requirement",
	})
	if err != nil {
		t.Fatalf("intake refused the multi-line requirement: %v", err)
	}
	readyRevision := driveTaskToReady(t, engine.repositories, created.TaskID, created.Revision)
	preflight, err := engine.application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"pipe019b-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "pipe019b-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	engine.waitFor(t, "the run to close its ledger", func() bool {
		recorded, err := engine.repositories.ListPipelineStages(
			context.Background(), created.TaskID, 1)
		return err == nil && len(recorded) == len(pipeline.Flow)
	})

	// The claim this ticket exists to prove: a durable plan revision exists
	// for this task. Before the fix, recordDurablePlan's BuildAgentPlan call
	// was refused and no plan was ever recorded, so this lookup found nothing.
	revision, err := engine.repositories.GetCurrentPlanRevision(ctx, created.TaskID)
	if err != nil {
		t.Fatalf(
			"a multi-line acceptance requirement must reach a bound plan "+
				"through the full Run() path: %v", err)
	}
	if revision.Revision == 0 {
		t.Fatal("recorded plan has no revision")
	}
	if len(revision.Plan.Steps) == 0 {
		t.Fatal("the bound plan carries no steps")
	}
	for _, step := range revision.Plan.Steps {
		if strings.TrimSpace(step.DetailRedacted) != step.DetailRedacted {
			t.Errorf("bound plan step %s detail carries leading/trailing "+
				"whitespace: %q", step.ID, step.DetailRedacted)
		}
		if strings.Contains(step.DetailRedacted, "<<<ACCEPTANCE") {
			t.Errorf("bound plan step %s detail embeds the acceptance block "+
				"verbatim: %q", step.ID, step.DetailRedacted)
		}
	}
}
