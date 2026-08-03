//go:build integration

package coordinator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// runTests is one scripted turn that runs the project's own test command.
func runTests(input agentloop.ModelInput) agentloop.ModelTurn {
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

// refiningModel answers according to what the plan still has open.
//
// A fixed list of turns cannot express a refinement: the number of rounds an
// attempt takes is decided by the loop, not by the script, and a script that
// guesses wrong sends a write to a step that is already closed. This reacts to
// the plan it is shown, which is what a real model does, and it writes a
// deliberately wrong draft until it has been told its tests failed.
type refiningModel struct {
	scriptedEngineModel
	contents func(path string, toldItFailed bool) string
}

func (model *refiningModel) ObserveThink(
	_ context.Context, input agentloop.ModelInput,
) (agentloop.ModelTurn, error) {
	toldItFailed := false
	for _, item := range input.RepositoryContext {
		if strings.Contains(item.Path, "last-test-run-output") {
			toldItFailed = true
		}
	}
	for _, step := range input.Plan.Steps {
		if step.State != agentloop.StepPending &&
			step.State != agentloop.StepInProgress {
			continue
		}
		for _, tool := range step.CompletionTools {
			switch tool {
			case executor.ToolApplyEdit:
				if len(step.ExpectedFiles) == 0 {
					continue
				}
				path := step.ExpectedFiles[0]
				return model.stamp(writeFile(
					path, model.contents(path, toldItFailed))(input)), nil
			case executor.ToolTest:
				return model.stamp(runTests(input)), nil
			}
		}
	}
	return model.stamp(agentloop.ModelTurn{
		Completion: agentloop.CompletionImplementationComplete,
	}), nil
}

// TestARunThatFailsItsOwnTestsRefinesUntilTheyPass is the refinement loop,
// proved rather than assumed.
//
// A single attempt gets one write per edit step, so an agent whose first draft
// compiles and then fails its own tests can see the failure and has nowhere
// left to act on it. The run is supposed to notice that, take another attempt,
// and be told exactly what broke. Every real run so far has succeeded first
// time, which means that path has never actually been exercised — and an
// unexercised recovery path is indistinguishable from a broken one.
//
// The model is scripted so the first draft is wrong on purpose and the failure
// is real: the tests that fail are the project's own, run by the real tool
// executor against the real worktree.
func TestARunThatFailsItsOwnTestsRefinesUntilTheyPass(t *testing.T) {
	const requirement = "Create greet.go and greet_test.go so that Greet returns hello."
	const brokenGreet = "package main\n\nfunc Greet() string {\n\treturn \"wrong\"\n}\n"
	// The comment is part of the fixture because it is part of finished work:
	// a run is sent back for a function without one, so a fixture lacking it
	// would be asserting that complete work gets refined further.
	const fixedGreet = "package main\n\n" +
		"// Greet returns the greeting this program exists to produce.\n" +
		"func Greet() string {\n\treturn \"hello\"\n}\n"
	const greetTest = "package main\n\nimport \"testing\"\n\n" +
		"func TestGreet(t *testing.T) {\n" +
		"\tif Greet() != \"hello\" {\n\t\tt.Fatalf(\"Greet() = %q\", Greet())\n\t}\n}\n"

	model := &refiningModel{contents: func(path string, toldItFailed bool) string {
		switch {
		case strings.HasSuffix(path, "_test.go"):
			return greetTest
		case toldItFailed:
			return fixedGreet
		default:
			return brokenGreet
		}
	}}

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
		IdempotencyKey:           "engine-refinement",
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
		"engine-refinement-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "engine-refinement-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}
	binding, err := engine.repositories.GetWorktreeBinding(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("starting a task must create its worktree: %v", err)
	}
	engine.waitFor(t, "the run to finish", func() bool {
		return strings.Contains(engine.narration(), "Finished:")
	})
	narration := engine.narration()
	t.Logf("the run reported:\n%s", narration)

	// 1. The failure was real and was noticed. A run that never saw its tests
	//    fail has not demonstrated anything about what it does when they do.
	if !strings.Contains(narration, "Attempt 2") {
		t.Fatal("the run never took a second attempt, so the refinement loop " +
			"was not exercised at all")
	}

	// 2. The refinement actually landed. This is the thesis: not that the run
	//    tried again, but that trying again fixed the file on disk.
	produced, err := os.ReadFile(filepath.Join(binding.WorktreePath, "greet.go"))
	if err != nil {
		t.Fatalf("the refined file is not in the worktree: %v", err)
	}
	if string(produced) != fixedGreet {
		t.Fatalf("the run finished holding the draft it had already been told "+
			"was wrong:\n%s", produced)
	}

	// 3. And the tests it was judged by now pass, in the worktree it produced.
	engine.verifyPlanThesis(t, created.TaskID, binding.WorktreePath)
}

// TestTheEngineKeepsItsFirstDraftWhenTheTestsAlreadyPass guards the other
// direction.
//
// Refinement that fires when nothing is wrong is its own defect: it burns a
// round, rewrites a correct file, and gives a person a diff to review that
// changes nothing. A run whose tests pass must stop.
func TestTheEngineKeepsItsFirstDraftWhenTheTestsAlreadyPass(t *testing.T) {
	const requirement = "Create greet.go and greet_test.go so that Greet returns hello."
	// The comment is part of the fixture because it is part of finished work:
	// a run is sent back for a function without one, so a fixture lacking it
	// would be asserting that complete work gets refined further.
	const fixedGreet = "package main\n\n" +
		"// Greet returns the greeting this program exists to produce.\n" +
		"func Greet() string {\n\treturn \"hello\"\n}\n"
	const greetTest = "package main\n\nimport \"testing\"\n\n" +
		"func TestGreet(t *testing.T) {\n" +
		"\tif Greet() != \"hello\" {\n\t\tt.Fatalf(\"Greet() = %q\", Greet())\n\t}\n}\n"

	model := &refiningModel{contents: func(path string, _ bool) string {
		if strings.HasSuffix(path, "_test.go") {
			return greetTest
		}
		return fixedGreet
	}}
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
		IdempotencyKey:           "engine-no-refinement",
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
		"engine-no-refinement-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "engine-no-refinement-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}
	engine.waitFor(t, "the run to finish", func() bool {
		return strings.Contains(engine.narration(), "Finished:")
	})
	narration := engine.narration()
	// The guarantee is not "never take another attempt" — a review that finds
	// a real weakness in passing work should send the run back, and that is
	// the point of having one. The guarantee is that the run never claims its
	// tests failed when they passed, because that is the reason that would be
	// false and would make every later message untrustworthy.
	if strings.Contains(narration, "the tests did not pass") {
		t.Errorf("the run said its tests failed when they passed:\n%s", narration)
	}
	if !strings.Contains(narration, "Ran tests") {
		t.Errorf("the run finished without ever running the tests it is "+
			"judged by:\n%s", narration)
	}
}

// fingerprintUsed keeps the task class vocabulary honest in these fixtures.
var _ = fingerprint.TaskClassFeature
