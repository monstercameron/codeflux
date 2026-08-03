package coordinator

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestPIPE019a_UntrimmedRequirementStallsSilently reproduces the defect
// PIPE-019a was opened to diagnose.
//
// A requirement carrying an <<<ACCEPTANCE ... >>> block on its own line ends
// in a trailing newline. storage.AnalyzeTaskRequirement refuses any body for
// which strings.TrimSpace(body) != body, and two independent call sites hit
// that refusal:
//
//   - agent_execution.go's ambiguity check (Run, around the clarification
//     stage) swallows the error silently: the analysis is simply not used,
//     clarification is recorded not-implemented instead of satisfied, and
//     nothing says why.
//   - agent_plan_record.go's recordDurablePlan calls the same function and
//     does not swallow the error, so the run's plan is never recorded. Its
//     Revision stays zero, and every step downstream that needs a durable
//     plan revision — starting with StageContracts — has nothing to bind to
//     and is never reached. The ledger then closes every remaining stage as
//     not-implemented, which reads exactly like "this build performs six of
//     thirty-seven stages" rather than "this run's plan could not be
//     recorded because of one whitespace byte".
//
// This is the "several stages later, reporting a constraint rather than a
// whitespace problem" PIPE-019a's ticket text describes.
func TestPIPE019a_UntrimmedRequirementStallsSilently(t *testing.T) {
	// Trailing newline, the way an appended <<<ACCEPTANCE ... >>> block on its
	// own line leaves the requirement.
	const requirement = "Create cmd/wrong/main.go so the program prints the answer.\n"
	const program = "package main\n\nimport \"fmt\"\n\n" +
		"func main() {\n\tfmt.Println(\"unverified\")\n}\n"

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

	// The silent skip: clarification's own ambiguity check errors on the
	// untrimmed body and is swallowed, so the stage is never recorded at all
	// instead of being recorded satisfied or failed.
	if state, recorded := states[pipeline.StageClarification]; !recorded ||
		state != pipeline.StateNotImplemented {
		t.Errorf("clarification = %q (recorded=%v), want not-implemented: the "+
			"untrimmed body should make its own AnalyzeTaskRequirement call "+
			"error and that error is swallowed silently", state, recorded)
	}

	// The stall: recordDurablePlan's own AnalyzeTaskRequirement call errors,
	// so no plan is ever recorded, and every stage from contracts onward never
	// runs.
	if state, recorded := states[pipeline.StageContracts]; !recorded ||
		state != pipeline.StateNotImplemented {
		t.Errorf("contracts = %q (recorded=%v), want not-implemented: with no "+
			"durable plan recorded, nothing downstream of decomposition-coverage "+
			"can run", state, recorded)
	}
}

// TestPIPE019a_TrimmingRequirementOfAloneDoesNotFixIt is the diagnosis's
// second half.
//
// Trimming inside AgentExecution.requirementOf looks like the obvious fix for
// the stall above, and it was tried and reverted: it makes every run that
// tries it stop early, because it only changes what
// AgentExecution.recordDurablePlan computes from — it does nothing about
// storage.RecordTaskRequirement, which independently re-reads the *original*
// message row straight from SQL and re-derives its own analysis from those
// exact untrimmed bytes, then requires the caller's analysis to equal that
// recomputation byte-for-byte.
//
// This proves the mechanism directly: even with a hand-trimmed analysis
// supplied to RecordTaskRequirement, the store's own recomputation from the
// still-untrimmed stored message fails the same way. Trimming
// scope.requirement earlier does not touch this call, so it does not fix the
// defect — it only moves which of the two identical guard failures is the one
// that fires, from storage.AnalyzeTaskRequirement's own clear error
// ("analyse the requirement") to RecordTaskRequirement's generic constraint
// error ("record task requirement"), and the run still fails to record a
// plan either way. The real fix has to make every caller of
// storage.AnalyzeTaskRequirement agree on the same normalized text — either
// by normalizing where the message body is first persisted, or inside
// storage.AnalyzeTaskRequirement itself — and both live in internal/storage,
// outside this package.
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

	// The analysis a trimming fix at requirementOf would have produced: a real
	// analysis of the trimmed text, computed the same way recordDurablePlan
	// computes it, just with the fix applied.
	analysis, err := storage.AnalyzeTaskRequirement(strings.TrimSpace(requirement))
	if err != nil {
		t.Fatalf("the trimmed body should analyse cleanly: %v", err)
	}

	// RecordTaskRequirement does not trust that analysis. It re-reads
	// task.request_message_id's own row and recomputes. That row still holds
	// the original, untrimmed body, because trimming requirementOf changes
	// nothing about what was persisted when the message was created.
	_, recordErr := engine.repositories.RecordTaskRequirement(ctx,
		storage.RecordTaskRequirement{
			TaskID: created.TaskID, MessageID: requestID,
			Analysis:       analysis,
			IdempotencyKey: "pipe019a-trim-does-not-fix-it-requirement",
		})
	if recordErr == nil {
		t.Fatal("expected RecordTaskRequirement to refuse a trimmed analysis " +
			"against an untrimmed stored message; if this now succeeds, the " +
			"store has been changed to normalize the body itself and " +
			"requirementOf's comment recording this finding should be revisited")
	}
	if !errors.Is(recordErr, storage.ErrConstraint) {
		t.Errorf("got %v, want a storage.ErrConstraint: the store's own "+
			"re-derivation from the untrimmed row should be what refuses this, "+
			"the same failure that fires today without any trim at all",
			recordErr)
	}
}
