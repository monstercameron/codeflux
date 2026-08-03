//go:build integration

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

// TestEveryStageOfTheFlowIsRecordedIncludingTheOnesNothingImplements is the
// check that keeps this build honest about itself.
//
// This build performs a handful of the flow's stages. The
// danger is not the gap — it is that the gap leaves no trace: a run that
// derived no contracts, generated no atoms, wrote no test before any code,
// discharged no composition obligation, and measured no coverage still ends
// with a written file and a green result, which is the same evidence a run
// that did all of it would produce.
//
// So the ledger must account for every stage, and a stage nothing implements
// must say so rather than be absent. Absent reads as an oversight in the
// record; present and not-implemented is a statement about the product.
func TestEveryStageOfTheFlowIsRecordedIncludingTheOnesNothingImplements(t *testing.T) {
	const requirement = "Create cmd/hello/main.go so the program prints a greeting."
	const program = "package main\n\nimport \"fmt\"\n\n" +
		"func main() {\n\tfmt.Println(\"hello\")\n}\n"

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			writeFile("cmd/hello/main.go", program),
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
		AffectedPackages:         []string{"cmd/hello"},
		IdempotencyKey:           "engine-ledger",
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
		"engine-ledger-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "engine-ledger-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	engine.waitFor(t, "the run to record its whole flow", func() bool {
		recorded, err := engine.repositories.ListPipelineStages(
			ctx, created.TaskID, 1)
		return err == nil && len(recorded) == len(pipeline.Flow)
	})

	recorded, err := engine.repositories.ListPipelineStages(ctx, created.TaskID, 1)
	if err != nil {
		t.Fatalf("reading the flow back failed: %v", err)
	}

	// 1. Every stage is accounted for, in order, with nothing missing and
	//    nothing invented.
	if len(recorded) != len(pipeline.Flow) {
		t.Fatalf("the ledger holds %d of %d stages", len(recorded), len(pipeline.Flow))
	}
	byNumber := map[pipeline.Number]storage.PipelineStageRecord{}
	for index, record := range recorded {
		if record.Stage != pipeline.Flow[index].Number {
			t.Fatalf("stage %d of the ledger is %d, want %d",
				index, record.Stage, pipeline.Flow[index].Number)
		}
		if !record.State.Valid() {
			t.Errorf("stage %s recorded the invalid state %q",
				record.Name, record.State)
		}
		if strings.TrimSpace(record.Gate) == "" {
			t.Errorf("stage %s records no gate, so nothing says what was checked",
				record.Name)
		}
		byNumber[record.Stage] = record
	}

	// 2. The stages this build really performs said so.
	for _, expected := range []struct {
		stage pipeline.Number
		state pipeline.State
	}{
		{pipeline.StageInstructions, pipeline.StateSatisfied},
		{pipeline.StageClarification, pipeline.StateSatisfied},
		{pipeline.StageAtomicInstructions, pipeline.StateSatisfied},
		{pipeline.StageProgram, pipeline.StateSatisfied},
	} {
		if got := byNumber[expected.stage].State; got != expected.state {
			t.Errorf("stage %s = %q, want %q",
				byNumber[expected.stage].Name, got, expected.state)
		}
	}

	// 3. Every stage now performs something. The state a stage reports must be
	//    a real finding — satisfied, failed, skipped for a reason, or blocked
	//    by something upstream — and never "nothing here does this", because
	//    every one of them now does.
	for _, record := range recorded {
		if record.State == pipeline.StateNotImplemented {
			t.Errorf("stage %s reports not-implemented, but every stage of the "+
				"flow is performed: %s", record.Name, record.DetailRedacted)
		}
		if strings.TrimSpace(record.DetailRedacted) == "" {
			t.Errorf("stage %s reports %q with no detail, so the record says "+
				"what happened without saying anything about it",
				record.Name, record.State)
		}
	}

	// 4. And the summary a person would actually read.
	counts := map[pipeline.State]int{}
	for _, record := range recorded {
		counts[record.State]++
	}
	t.Logf("flow: %d satisfied, %d failed, %d skipped, %d blocked, %d not implemented (of %d)",
		counts[pipeline.StateSatisfied], counts[pipeline.StateFailed],
		counts[pipeline.StateSkipped], counts[pipeline.StateBlocked],
		counts[pipeline.StateNotImplemented], len(pipeline.Flow))
	if counts[pipeline.StateNotImplemented] != 0 {
		t.Errorf("%d stage(s) still report not-implemented",
			counts[pipeline.StateNotImplemented])
	}
	// A run of a one-file program cannot honestly satisfy every stage of the
	// flow, whatever its length. If it ever does, the gates have stopped
	// asking anything rather than the work having become perfect.
	if counts[pipeline.StateSatisfied] == len(pipeline.Flow) {
		t.Error("every stage reported satisfied, which no run of a single " +
			"scripted file should be able to claim")
	}
}
