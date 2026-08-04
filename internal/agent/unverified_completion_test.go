package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/providers"
)

// TestAnAttemptMayNotEndOnAnUntestedWrite is the round the loop was missing.
//
// The coordinator re-runs the suite over whatever an attempt leaves behind, and
// when that fails the refinement is discarded and the worktree put back to the
// last revision that passed. That policy is right and is not what this guards:
// a broken candidate really does make a worse baseline than the verified
// revision it replaced.
//
// What it guards is the run stopping one call short of finding out. The test
// tool was available the whole time, the failure would have been its own, in
// the file it had just written, and fixing it inside the attempt costs one
// round against the whole attempt afterwards.
//
// Ladder rung 3 on 2026-08-03 lost attempts 5 and 7 this way, about 200
// seconds, both reported as "files were written after the run's own last test,
// so the suite was run again here and did not pass" — having reached 30 of 36
// synthesised cases and handed the work back twice.
func TestAnAttemptMayNotEndOnAnUntestedWrite(t *testing.T) {
	harness := newLoopHarness(t)
	harness.input.ApprovedTools = []ApprovedTool{
		approvedApplyEditTool(t), approvedTestTool(t),
	}
	harness.input.Plan.Steps = append(harness.input.Plan.Steps, PlanStep{
		ID: "verification", Kind: StepKindTest,
		SummaryRedacted:    "Run the tests",
		State:              StepPending,
		ValidationRequired: true,
		CompletionTools:    []executor.ToolName{executor.ToolTest},
	})
	harness.rebuildLoop(t)

	requestOne := newModelRequestID(t)
	requestTwo := newModelRequestID(t)
	requestThree := newModelRequestID(t)
	var observed []ModelInput
	harness.model.steps = []modelStep{
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, requestOne, "edit-1",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"package main"}`,
				"implementation", false,
			), nil
		},
		// Claims the work is finished without ever running the tests.
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return completionTurn(
				harness.model.identity, requestTwo,
				CompletionImplementationComplete,
			), nil
		},
		// Must be reached: the loop refuses the ending above and asks again.
		// The run does what it was told, then finishes.
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, requestThree, "test-1",
				executor.ToolTest,
				`{"executable":"go","arg1":"test","arg2":"./..."}`,
				"verification", false,
			), nil
		},
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return completionTurn(
				harness.model.identity, newModelRequestID(t),
				CompletionImplementationComplete,
			), nil
		},
	}
	harness.tools.resultFor = func(request executor.ToolRequest) executor.ToolResult {
		return executor.ToolResult{
			RequestID: request.ID, SchemaVersion: executor.ToolSchemaVersion,
			State: "succeeded", ExitCode: 0, Summary: "ok",
		}
	}

	outcome, err := harness.loop.Run(t.Context(), harness.input)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != 4 {
		t.Fatalf("the loop saw %d round(s); a completion claimed over an "+
			"untested write was accepted rather than sent back for one more",
			len(observed))
	}

	// The refusal has to say what to do, not merely decline.
	told := observed[2].PreviousResults
	if len(told) != 1 {
		t.Fatalf("the refused round was given %d result(s), so it was not told "+
			"why it is being asked again", len(told))
	}
	if !told[0].IsError || told[0].Tool != string(executor.ToolTest) {
		t.Errorf("the refusal is not attributed to the tests: %+v", told[0])
	}
	if !strings.Contains(told[0].StdoutRedacted, "Run the tests now") {
		t.Errorf("the refusal does not name the action it wants:\n%s",
			told[0].StdoutRedacted)
	}
	// It still finishes. The guard buys a round; it does not fail the run.
	if outcome.Kind != OutcomeImplementationComplete {
		t.Errorf("outcome = %v, want the run to complete after verifying",
			outcome.Kind)
	}
}

// TestARunThatTestsItsWriteEndsWhenItSaysSo is the first control.
//
// The guard turns on there being an untested write, not on the model claiming
// completion. A run that writes, tests, and then says it is finished has done
// exactly what is being asked, and must not be sent round again — an extra
// round costs a model call and teaches nothing.
func TestARunThatTestsItsWriteEndsWhenItSaysSo(t *testing.T) {
	harness := newLoopHarness(t)
	harness.input.ApprovedTools = []ApprovedTool{
		approvedApplyEditTool(t), approvedTestTool(t),
	}
	// A step the test call can be attributed to. Without one the run cannot
	// verify at all, which is the other half of the same problem and not what
	// this control is measuring.
	harness.input.Plan.Steps = append(harness.input.Plan.Steps, PlanStep{
		ID: "verification", Kind: StepKindTest,
		SummaryRedacted:    "Run the tests",
		State:              StepPending,
		ValidationRequired: true,
		CompletionTools:    []executor.ToolName{executor.ToolTest},
	})
	harness.rebuildLoop(t)

	var observed []ModelInput
	harness.model.steps = []modelStep{
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "edit-1",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"package main"}`,
				"implementation", false,
			), nil
		},
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "test-1",
				executor.ToolTest,
				`{"executable":"go","arg1":"test","arg2":"./..."}`,
				"verification", false,
			), nil
		},
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return completionTurn(
				harness.model.identity, newModelRequestID(t),
				CompletionImplementationComplete,
			), nil
		},
	}
	harness.tools.resultFor = func(request executor.ToolRequest) executor.ToolResult {
		return executor.ToolResult{
			RequestID: request.ID, SchemaVersion: executor.ToolSchemaVersion,
			State: "succeeded", ExitCode: 0, Summary: "ok",
		}
	}

	if _, err := harness.loop.Run(t.Context(), harness.input); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 3 {
		t.Fatalf("the loop saw %d round(s); a run that tested its own write "+
			"before finishing was sent round again anyway", len(observed))
	}
}

// TestAStepStaysUsableAfterItHasBeenCompleted is what makes the guard above
// followable, and what keeps a refinement attempt able to work at all.
//
// A tool is offered only while some open step names it, so closing the
// verification step on its first call took the suite away for the rest of the
// attempt. A run that tested and then kept patching could not check itself
// again, which meant the completion guard would ask for something the run no
// longer had — the unfollowable instruction this loop has already paid for
// twice.
//
// Proven to discriminate: against the previous implementation the verification
// step read implemented after the test call, and the tool that offers it is
// filtered on exactly that.
func TestAStepStaysUsableAfterItHasBeenCompleted(t *testing.T) {
	harness := newLoopHarness(t)
	harness.input.ApprovedTools = []ApprovedTool{
		approvedApplyEditTool(t), approvedTestTool(t),
	}
	harness.input.Plan.Steps = append(harness.input.Plan.Steps, PlanStep{
		ID: "verification", Kind: StepKindTest,
		SummaryRedacted:    "Run the tests",
		State:              StepPending,
		ValidationRequired: true,
		CompletionTools:    []executor.ToolName{executor.ToolTest},
	})
	harness.rebuildLoop(t)

	var observed []ModelInput
	harness.model.steps = []modelStep{
		// Test first, the way a refinement attempt does.
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "test-1",
				executor.ToolTest,
				`{"executable":"go","arg1":"test","arg2":"./..."}`,
				"verification", false,
			), nil
		},
		// Then change something, which is what makes the answer stale.
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "edit-1",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"package main"}`,
				"implementation", false,
			), nil
		},
		// And test again. This is the call that used to have nowhere to bind.
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "test-2",
				executor.ToolTest,
				`{"executable":"go","arg1":"test","arg2":"./..."}`,
				"verification", false,
			), nil
		},
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return completionTurn(
				harness.model.identity, newModelRequestID(t),
				CompletionImplementationComplete,
			), nil
		},
	}
	harness.tools.resultFor = func(request executor.ToolRequest) executor.ToolResult {
		return executor.ToolResult{
			RequestID: request.ID, SchemaVersion: executor.ToolSchemaVersion,
			State: "succeeded", ExitCode: 0, Summary: "ok",
		}
	}

	outcome, err := harness.loop.Run(t.Context(), harness.input)
	if err != nil {
		t.Fatalf("a second run of the suite had nowhere to bind: %v", err)
	}
	if len(observed) != 4 {
		t.Fatalf("the loop saw %d round(s), want 4", len(observed))
	}
	// Both suite runs actually happened. The step closes on the first, which
	// is what keeps completion reachable, and the second still binds to it.
	testRuns := 0
	for _, start := range harness.journal.starts {
		if start.ToolName == executor.ToolTest {
			testRuns++
		}
	}
	if testRuns != 2 {
		t.Errorf("the suite ran %d time(s); the second call after a write had "+
			"nowhere to bind", testRuns)
	}
	// Every step still reaches a closed state, which is what
	// implementationStepsComplete reads before completion may be declared. A
	// check held open forever would make completion unreachable, which is the
	// regression the first attempt at this caused.
	for _, step := range outcome.Plan.Steps {
		if step.State != StepImplemented && step.State != StepValidated &&
			step.State != StepSkipped {
			t.Errorf("step %q read %q, so completion could not be declared",
				step.ID, step.State)
		}
	}
}

// TestCompletionIsNotRefusedWhenTheSuiteCannotBeRun is the guard's own control.
//
// A guard that demands a tool the run does not have is the unfollowable
// instruction this loop has paid for twice: rung 3 was told main must return an
// error, which Go forbids, and spent its budget failing to comply. So the guard
// asks whether an open step can still accept the test tool, and stands down
// when none can, rather than asking the approved list — which still names the
// tool after the last step that could bind it has closed.
func TestCompletionIsNotRefusedWhenTheSuiteCannotBeRun(t *testing.T) {
	harness := newLoopHarness(t)
	// Only a write step: nothing in this plan can accept a test call.
	harness.input.ApprovedTools = []ApprovedTool{
		approvedApplyEditTool(t), approvedTestTool(t),
	}
	harness.rebuildLoop(t)

	var observed []ModelInput
	harness.model.steps = []modelStep{
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "edit-1",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"package main"}`,
				"implementation", false,
			), nil
		},
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return completionTurn(
				harness.model.identity, newModelRequestID(t),
				CompletionImplementationComplete,
			), nil
		},
	}
	harness.tools.result = executor.ToolResult{
		RequestID: "edit-1", SchemaVersion: executor.ToolSchemaVersion,
		State: "succeeded", ExitCode: 0, Summary: "edit applied",
	}

	if _, err := harness.loop.Run(t.Context(), harness.input); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 {
		t.Fatalf("the loop saw %d round(s); it asked for a suite run that no "+
			"open step could have accepted", len(observed))
	}
}

// TestASilentTurnCostsARoundNotTheAttempt is the cheapest correction the loop
// can make.
//
// Every malformed turn ends the attempt, which is right for a turn that
// proposed something impossible — the next attempt has to be told. It is far
// too expensive for a turn that proposed nothing at all: an attempt is thirty
// to sixty seconds against a round's four, and the very next round fixes it.
//
// Ladder rung 4 on 2026-08-03 lost attempt 2 to this, the whole diagnosis being
// "turn contains neither tool calls nor completion".
func TestASilentTurnCostsARoundNotTheAttempt(t *testing.T) {
	harness := newLoopHarness(t)
	var observed []ModelInput
	harness.model.steps = []modelStep{
		// Says nothing at all.
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return ModelTurn{
				ModelRequestID: newModelRequestID(t),
				Model:          harness.model.identity,
				Usage:          knownUsage(),
				Cost: providers.ExactAmount{
					Currency: "USD", Numerator: 1, Denominator: 100, Known: true,
				},
			}, nil
		},
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "edit-1",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"package main"}`,
				"implementation", false,
			), nil
		},
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return completionTurn(
				harness.model.identity, newModelRequestID(t),
				CompletionImplementationComplete,
			), nil
		},
	}
	harness.tools.resultFor = func(request executor.ToolRequest) executor.ToolResult {
		return executor.ToolResult{
			RequestID: request.ID, SchemaVersion: executor.ToolSchemaVersion,
			State: "succeeded", ExitCode: 0, Summary: "ok",
		}
	}

	if _, err := harness.loop.Run(t.Context(), harness.input); err != nil {
		t.Fatalf("a turn that said nothing ended the attempt: %v", err)
	}
	if len(observed) != 3 {
		t.Fatalf("the loop saw %d round(s), want 3", len(observed))
	}
	// The round after has to be told what went wrong, or it will do it again.
	told := observed[1].PreviousResults
	if len(told) != 1 || !told[0].IsError {
		t.Fatalf("the silent turn was not reported to the next round: %+v", told)
	}
	// Naming the open steps is what makes it actionable. A turn whose every
	// tool call named a closed step arrives indistinguishable from one that
	// said nothing — the adapter discards a call it cannot bind rather than
	// failing the round — so "nothing happened" alone leaves the run to guess
	// which of the two it did.
	if !strings.Contains(told[0].StdoutRedacted, "nothing happened") {
		t.Errorf("the round was not told its turn achieved nothing: %s",
			told[0].StdoutRedacted)
	}
	if !strings.Contains(told[0].StdoutRedacted, "Still open: implementation") {
		t.Errorf("the round was not told which steps it could act on: %s",
			told[0].StdoutRedacted)
	}
}

// TestATurnThatContradictsTheContractStillCostsTheAttempt is the control.
//
// Only the empty turn is answered in place. A turn carrying both tool calls and
// a completion signal is the model disagreeing with the contract rather than
// losing its place, and the next attempt genuinely needs to be told.
func TestATurnThatContradictsTheContractStillCostsTheAttempt(t *testing.T) {
	harness := newLoopHarness(t)
	harness.model.steps = []modelStep{
		func(_ context.Context, _ ModelInput) (ModelTurn, error) {
			turn := successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "edit-1",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"package main"}`,
				"implementation", false,
			)
			turn.Completion = CompletionImplementationComplete
			return turn, nil
		},
	}

	_, err := harness.loop.Run(t.Context(), harness.input)
	if !errors.Is(err, ErrMalformedModelTurn) {
		t.Fatalf("a turn that both acts and declares completion was absorbed "+
			"rather than costing the attempt: %v", err)
	}
}

// TestAFileMayBePatchedMoreThanOnceInOneAttempt is the defect that cost rung 4
// three passes.
//
// Completing a step is not the same as freezing what it names. A patch that
// fixes the compile error the previous patch introduced is the ordinary case
// of refinement, not an anomaly.
//
// Proven to discriminate: against the previous implementation the second write
// to the same file recorded "plan step \"implementation\" is not executable",
// and in a real run it was worse than an error — the adapter discarded the call
// before the loop saw it, so the turn arrived indistinguishable from one where
// the model had said nothing, and the whole attempt was spent. Ladder rung 4 on
// 2026-08-03: after two successful patches the offered tools collapsed from
// [apply-patch test] to [test] and the run emitted five unbindable turns in a
// row.
func TestAFileMayBePatchedMoreThanOnceInOneAttempt(t *testing.T) {
	harness := newLoopHarness(t)
	var observed []ModelInput
	write := func(content string) modelStep {
		return func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t),
				"edit-"+content, executor.ToolApplyEdit,
				`{"path":"main.go","content":"`+content+`"}`,
				"implementation", false,
			), nil
		}
	}
	harness.model.steps = []modelStep{
		write("first"),
		write("second"),
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return completionTurn(
				harness.model.identity, newModelRequestID(t),
				CompletionImplementationComplete,
			), nil
		},
	}
	harness.tools.resultFor = func(request executor.ToolRequest) executor.ToolResult {
		return executor.ToolResult{
			RequestID: request.ID, SchemaVersion: executor.ToolSchemaVersion,
			State: "succeeded", ExitCode: 0, Summary: "ok",
		}
	}

	if _, err := harness.loop.Run(t.Context(), harness.input); err != nil {
		t.Fatalf("a second write to a file the plan names was refused: %v", err)
	}
	if len(observed) != 3 {
		t.Fatalf("the loop saw %d round(s), want 3", len(observed))
	}
	writes := 0
	for _, start := range harness.journal.starts {
		if start.ToolName == executor.ToolApplyEdit {
			writes++
		}
	}
	if writes != 2 {
		t.Errorf("%d write(s) reached the journal; the second had nowhere to "+
			"bind", writes)
	}
}

// TestAFailedStepIsNotReused is the control.
//
// "Completed" and "spent" are different. A step that failed is a route the run
// has been told not to take again, and reopening it would turn the one state
// that means stop into another word for carry on.
func TestAFailedStepIsNotReused(t *testing.T) {
	for _, state := range []StepState{StepFailed, StepSkipped} {
		if StepMayAcceptAnotherCall(state) {
			t.Errorf("a %s step was offered for reuse", state)
		}
	}
	for _, state := range []StepState{
		StepPending, StepInProgress, StepImplemented, StepValidated,
	} {
		if !StepMayAcceptAnotherCall(state) {
			t.Errorf("a %s step was refused another call", state)
		}
	}
}
