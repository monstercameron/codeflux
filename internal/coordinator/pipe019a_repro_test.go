package coordinator

import (
	"context"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestPIPE019a_UntrimmedRequirementStallsSilently reproduces the defect
// PIPE-019a was opened to diagnose, and now pins its fix.
//
// A requirement carrying an <<<ACCEPTANCE ... >>> block on its own line ends
// in a trailing newline. Before the fix, storage.AnalyzeTaskRequirement
// refused any body for which strings.TrimSpace(body) != body, and two
// independent call sites hit that refusal:
//
//   - agent_execution.go's ambiguity check (Run, around the clarification
//     stage) swallowed the error silently: the analysis was simply not used,
//     clarification was recorded not-implemented instead of satisfied or
//     failed, and nothing said why.
//   - agent_plan_record.go's recordDurablePlan called the same function and
//     did not swallow the error, so the run's plan was never recorded.
//
// The fix (PIPE-019a) is in storage.AnalyzeTaskRequirement itself: it now
// normalizes (trims) the body once, before deriving anything from it, so
// every caller reaches the same analysis regardless of which untrimmed copy
// it happened to be holding. This test proves the first half directly: the
// clarification stage is now recorded honestly (satisfied, here, since this
// requirement has no material ambiguity) instead of silently missing from
// the ledger.
//
// It does not go on to assert that this specific run reaches a bound plan.
// Fixing the refusal here uncovers a second, separate defect that this
// refusal used to mask: agent_narration.go's agentPlanSteps embeds the raw,
// untrimmed requirement into a plan step's DetailRedacted
// ("Write <file> — " + requirement) without normalizing it, and
// storage.AgentPlan.Validate requires DetailRedacted to carry no leading or
// trailing whitespace. Previously, recordDurablePlan's own
// AnalyzeTaskRequirement call always failed first, so BuildAgentPlan was
// never reached with an untrimmed body and this never surfaced. Now that the
// first call succeeds, this run reaches BuildAgentPlan and is explicitly
// refused there instead — loudly, not silently, which is the property this
// ticket is about — but agent_narration.go is not in PIPE-019a's file
// ownership (it is not one of the three computations this ticket names), so
// fixing it is out of scope here and is called out as a follow-up rather
// than patched incidentally. TestPIPE019a_MultiLineAcceptanceRequirementReachesABoundPlan
// below proves the actual "reaches a bound plan" claim at the storage layer,
// where PIPE-019a's fix is complete.
func TestPIPE019a_UntrimmedRequirementStallsSilently(t *testing.T) {
	// Trailing newline, the way an appended <<<ACCEPTANCE ... >>> block on its
	// own line leaves the requirement.
	const requirement = "Create cmd/wrong/main.go so the program prints the answer.\n"
	const program = "package main\n\nimport \"fmt\"\n\n" +
		"func main() {\n\tfmt.Println(\"unverified\")\n}\n"

	// The concrete claim the whole ticket turns on: the same computation that
	// used to be refused for this exact untrimmed body must now succeed, and
	// every caller (the ambiguity check, recordDurablePlan, and the store's
	// own re-derivation) goes through this one function.
	if _, err := storage.AnalyzeTaskRequirement(requirement); err != nil {
		t.Fatalf(
			"AnalyzeTaskRequirement must no longer refuse an untrimmed body: %v",
			err)
	}

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			writeFile("cmd/wrong/main.go", program),
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
		AffectedPackages:         []string{"cmd/wrong"},
		IdempotencyKey:           "pipe019a-repro-requirement",
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
		"pipe019a-repro-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "pipe019a-repro-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	engine.waitFor(t, "the run to close its ledger", func() bool {
		recorded, err := engine.repositories.ListPipelineStages(
			context.Background(), created.TaskID, 1)
		return err == nil && len(recorded) == len(pipeline.Flow)
	})

	states := pipelineStageStates(t, engine, created.TaskID)

	// The silent skip is fixed: clarification's own ambiguity check no longer
	// errors on the untrimmed body, so it runs for real and is recorded
	// satisfied (this requirement carries no material ambiguity) rather than
	// being silently absent from the ledger.
	if state, recorded := states[pipeline.StageClarification]; !recorded ||
		state != pipeline.StateSatisfied {
		t.Errorf("clarification = %q (recorded=%v), want satisfied: the "+
			"untrimmed body must no longer make AnalyzeTaskRequirement error, "+
			"and clarification must be recorded rather than silently skipped",
			state, recorded)
	}
}

// TestPIPE019a_TrimmingRequirementOfAloneDoesNotFixIt is the diagnosis's
// second half, and is updated here to assert the invariant PIPE-019a's real
// fix establishes rather than the partial-fix failure it used to pin.
//
// It originally proved that trimming AgentExecution.requirementOf alone
// could not work: storage.RecordTaskRequirement independently re-reads the
// original message row and re-derives its own analysis from those exact
// bytes, so a hand-trimmed analysis supplied by the caller was refused
// against the store's own untrimmed re-derivation with storage.ErrConstraint.
// That failure is real and is exactly why the fix could not live at either
// call site alone — see storage.AnalyzeTaskRequirement's own comment.
//
// Now that storage.AnalyzeTaskRequirement normalizes internally, the store's
// re-derivation from the untrimmed stored message and a caller's analysis of
// the same untrimmed message are the same computation, so they agree without
// either side needing to trim first. This test now proves that: it computes
// the analysis exactly as recordDurablePlan does — straight from the
// untrimmed requirement, no trimming at the call site — and asserts
// RecordTaskRequirement accepts it against the untrimmed stored message,
// which is the new invariant replacing the old "this must fail" pin.
func TestPIPE019a_TrimmingRequirementOfAloneDoesNotFixIt(t *testing.T) {
	const requirement = "Create cmd/wrong/main.go so the program prints the answer.\n"

	engine := startEngineFixture(t, &scriptedEngineModel{})
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
		AffectedPackages:         []string{"cmd/wrong"},
		IdempotencyKey:           "pipe019a-trim-does-not-fix-it",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}

	// The analysis recordDurablePlan itself computes: straight from the
	// untrimmed requirement, exactly as scope.requirement carries it. No trim
	// at this call site — the point of the fix is that none is needed.
	analysis, err := storage.AnalyzeTaskRequirement(requirement)
	if err != nil {
		t.Fatalf("the untrimmed requirement should analyse cleanly now: %v", err)
	}

	// RecordTaskRequirement re-reads task.request_message_id's own row and
	// recomputes its own analysis from those same untrimmed bytes. Before the
	// fix, that recomputation used to reject the untrimmed body outright, so
	// no analysis supplied by a caller could ever match it. Now both sides
	// normalize internally and derive the same analysis from the same bytes,
	// so this must succeed.
	recorded, recordErr := engine.repositories.RecordTaskRequirement(ctx,
		storage.RecordTaskRequirement{
			TaskID: created.TaskID, MessageID: requestID,
			Analysis:       analysis,
			IdempotencyKey: "pipe019a-trim-does-not-fix-it-requirement",
		})
	if recordErr != nil {
		t.Fatalf(
			"RecordTaskRequirement must accept an analysis of the untrimmed "+
				"requirement against the same untrimmed stored message now that "+
				"both normalize internally: %v", recordErr)
	}
	if recorded.Revision == 0 {
		t.Fatalf("recorded requirement has no revision: %#v", recorded)
	}
	if recorded.OriginalBodySHA256 == "" {
		t.Fatal("recorded requirement does not carry the original message digest")
	}
}

// TestPIPE019a_MultiLineAcceptanceRequirementReachesABoundPlan is the case
// this ticket exists to unblock: PIPE-019 makes a requirement's
// <<<ACCEPTANCE ... >>> block mandatory for the run to have anything to
// check, and that block, written on its own lines, leaves the requirement
// ending in a trailing newline. This proves such a requirement round-trips
// through AnalyzeTaskRequirement, RecordTaskRequirement, and
// RecordPlanRevision — the three computations PIPE-019a makes agree — and
// reaches a durably bound plan revision, rather than being refused the
// moment two of those three computations disagreed about whether the
// trailing newline was part of "the requirement".
//
// This exercises the storage layer directly rather than the full agent
// Run(), for the reason explained on
// TestPIPE019a_UntrimmedRequirementStallsSilently: Run()'s own plan-step
// drafting embeds the raw requirement into step text without normalizing it,
// which is a separate defect outside this ticket's file ownership. The claim
// this ticket makes — that the requirement's own analysis, recording, and
// plan-binding agree — is fully provable without going through that code,
// and this test proves exactly that claim.
func TestPIPE019a_MultiLineAcceptanceRequirementReachesABoundPlan(t *testing.T) {
	requirement := strings.Join([]string{
		"Create cmd/answer/main.go so the program prints the answer.",
		"",
		"<<<ACCEPTANCE",
		"args:",
		"expected: 42",
		">>>",
		"",
	}, "\n")

	engine := startEngineFixture(t, &scriptedEngineModel{})
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
		AffectedPackages:         []string{"cmd/answer"},
		IdempotencyKey:           "pipe019a-multiline-requirement",
	})
	if err != nil {
		t.Fatalf("intake refused the multi-line requirement: %v", err)
	}

	// The first computation: the same analysis every caller derives from the
	// raw, untrimmed message. A trailing newline from the acceptance block
	// must no longer make this fail.
	analysis, err := storage.AnalyzeTaskRequirement(requirement)
	if err != nil {
		t.Fatalf(
			"a multi-line acceptance requirement must analyse cleanly: %v", err)
	}

	// The second computation: the store's own re-derivation from the same
	// untrimmed row, which must agree with the first byte-for-byte.
	requirementRevision, err := engine.repositories.RecordTaskRequirement(ctx,
		storage.RecordTaskRequirement{
			TaskID: created.TaskID, MessageID: requestID,
			Analysis:       analysis,
			IdempotencyKey: "pipe019a-multiline-requirement-requirement",
		})
	if err != nil {
		t.Fatalf(
			"RecordTaskRequirement must agree with the caller's own analysis "+
				"of the same untrimmed multi-line message: %v", err)
	}

	task, err := engine.repositories.GetTask(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("reading the task failed: %v", err)
	}
	const repositoryRevision = "1111111111111111111111111111111111111111"
	nodeID, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := storage.BuildAgentPlan(analysis, storage.AgentPlanDraft{
		Goal:               "Print the expected answer.",
		Scope:              []string{"cmd/answer"},
		ExpectedFiles:      []string{"cmd/answer/main.go"},
		ValidationCommands: canonicalTestCommand(),
		Steps: []storage.AgentPlanStepDraft{{
			Kind:           storage.StepKindEdit,
			Title:          "Write cmd/answer/main.go",
			DetailRedacted: "Write the program that prints the expected answer.",
			ExpectedFiles:  []string{"cmd/answer/main.go"},
			GraphNodeIDs:   []domain.NodeID{nodeID},
		}},
		CompletionCriteria: []string{"The program prints the expected answer."},
	})
	if err != nil {
		t.Fatalf(
			"building the plan from the normalized analysis failed: %v", err)
	}

	manifestID := strings.Repeat("9", 64)
	if _, err := engine.repositories.RecordContextManifest(ctx,
		storage.RecordContextManifest{
			ID: manifestID, RepositoryID: task.RepositoryID,
			RepositoryRevision: repositoryRevision,
			MapRevision:        digestOf(repositoryRevision),
			// The third computation: the context this plan is made against must
			// point at the same original message the requirement was recorded
			// against, so this must equal what RecordTaskRequirement itself
			// derived from the untrimmed stored body.
			RequirementSHA256:  requirementRevision.OriginalBodySHA256,
			SelectionPolicy:    1,
			MaxFiles:           64,
			MaxBytes:           1 << 20,
			MaxEstimatedTokens: 200000,
		}); err != nil {
		t.Fatalf("recording the plan context failed: %v", err)
	}

	budget, err := engine.repositories.GetBudgetSnapshot(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("reading the task budget failed: %v", err)
	}

	recordedPlan, err := engine.repositories.RecordPlanRevision(ctx,
		storage.RecordPlanRevision{
			TaskID: created.TaskID, RequirementRevision: requirementRevision.Revision,
			RepositoryRevision:     repositoryRevision,
			ContextManifestID:      manifestID,
			PolicyRevision:         created.PolicyRevision,
			ForecastRevision:       created.ForecastRevision,
			BudgetID:               budget.BudgetID,
			BudgetLimitRevision:    budget.LimitRevision,
			BudgetSnapshotRevision: budget.Revision,
			Plan:                   plan,
			IdempotencyKey:         "pipe019a-multiline-plan",
		})
	if err != nil {
		t.Fatalf(
			"recording the plan for a multi-line acceptance requirement "+
				"failed: %v", err)
	}
	if recordedPlan.Revision == 0 {
		t.Fatalf("recorded plan has no revision: %#v", recordedPlan)
	}
	if recordedPlan.RequirementRevision != requirementRevision.Revision {
		t.Fatalf(
			"bound plan points at requirement revision %d, want %d",
			recordedPlan.RequirementRevision, requirementRevision.Revision)
	}
}
