//go:build integration

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

// startLedgerRun drives one scripted requirement and returns its flow ledger.
func startLedgerRun(
	t *testing.T,
	model agentloop.FixedModel,
	requirement string,
	key string,
) (engineFixture, domain.TaskID) {
	t.Helper()
	engine := startEngineFixture(t, model)
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
		AffectedPackages:         []string{"workspace"},
		IdempotencyKey:           key,
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
		key+"-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    key + "-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}
	engine.waitFor(t, "the run to finish", func() bool {
		return strings.Contains(engine.narration(), "Finished:")
	})
	return engine, created.TaskID
}

// stageOf reads one stage out of a finished run's ledger.
func stageOf(
	t *testing.T,
	engine engineFixture,
	taskID domain.TaskID,
	stage pipeline.Number,
) storage.PipelineStageRecord {
	t.Helper()
	recorded, err := engine.repositories.ListPipelineStages(
		context.Background(), taskID, 1)
	if err != nil {
		t.Fatalf("reading the flow back failed: %v", err)
	}
	for _, record := range recorded {
		if record.Stage == stage {
			return record
		}
	}
	t.Fatalf("stage %d is absent from the ledger", stage)
	return storage.PipelineStageRecord{}
}

// TestCodeThatDoesNotCompileIsNotAProgram is the assembly gate.
//
// Nothing used to check that what a run wrote could be built. A model that
// produced Go which did not compile still reached implementation-complete, and
// the first person to discover otherwise was whoever tried to build it. Files
// that do not compile are not a program, and a run that reports producing one
// is reporting something that does not exist.
func TestCodeThatDoesNotCompileIsNotAProgram(t *testing.T) {
	const requirement = "Create greet.go so that Greet returns hello."
	// Valid Go syntax, invalid program: the function promises a string and
	// returns nothing. It is deliberately not a parse error — a run has to
	// fail on code that looks entirely reasonable, because that is the kind a
	// model actually produces.
	const broken = "package main\n\nfunc Greet() string {\n\treturn\n}\n"

	engine, taskID := startLedgerRun(t,
		&refiningModel{contents: func(string, bool) string { return broken }},
		requirement, "engine-assembly-broken")

	narration := engine.narration()
	t.Logf("the run reported:\n%s", narration)

	assembly := stageOf(t, engine, taskID, pipeline.StageAssembly)
	if assembly.State != pipeline.StateFailed {
		t.Errorf("assembly = %q, want failed: the module does not build",
			assembly.State)
	}
	if !strings.Contains(assembly.DetailRedacted, "does not compile") {
		t.Errorf("assembly recorded %q, which does not say the build failed",
			assembly.DetailRedacted)
	}

	program := stageOf(t, engine, taskID, pipeline.StageProgram)
	if program.State != pipeline.StateFailed {
		t.Errorf("program = %q, want failed: files that do not compile are "+
			"not a program", program.State)
	}

	// And the person is told, in the sentence they actually read.
	if !strings.Contains(narration, "NOT verified") {
		t.Errorf("the run reported completion on code that does not build:\n%s",
			narration)
	}
	// The compiler's own words have to reach the next attempt, or refinement
	// is guessing.
	if !strings.Contains(narration, "does not compile") {
		t.Errorf("the run never said the code failed to build:\n%s", narration)
	}
}

// TestCodeThatCompilesReportsAProgram is the same gate from the other side.
//
// A gate that only ever refuses is indistinguishable from one that is broken.
func TestCodeThatCompilesReportsAProgram(t *testing.T) {
	const requirement = "Create greet.go and greet_test.go so that Greet returns hello."
	const good = "package main\n\n" +
		"// Greet returns the greeting this program exists to produce.\n" +
		"func Greet() string {\n\treturn \"hello\"\n}\n"
	const test = "package main\n\nimport \"testing\"\n\n" +
		"func TestGreet(t *testing.T) {\n" +
		"\tif Greet() != \"hello\" {\n\t\tt.Fatal(Greet())\n\t}\n}\n"

	engine, taskID := startLedgerRun(t,
		&refiningModel{contents: func(path string, _ bool) string {
			if strings.HasSuffix(path, "_test.go") {
				return test
			}
			return good
		}},
		requirement, "engine-assembly-good")

	if assembly := stageOf(t, engine, taskID, pipeline.StageAssembly); assembly.State != pipeline.StateSatisfied {
		t.Errorf("assembly = %q on a module that builds: %s",
			assembly.State, assembly.DetailRedacted)
	}
	if program := stageOf(t, engine, taskID, pipeline.StageProgram); program.State != pipeline.StateSatisfied {
		t.Errorf("program = %q on a module that builds", program.State)
	}
	if narration := engine.narration(); strings.Contains(narration, "NOT verified") {
		t.Errorf("working, tested code was reported as unverified:\n%s", narration)
	}
}
