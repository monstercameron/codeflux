package agent

import (
	"context"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/executor"
)

// TestAPatchToAClosedStepsFileIsRebound reproduces what rung 5 kept losing
// attempts to.
//
// By the time a run is refining, every write step is implemented: the files
// exist, so they were written, so their steps closed. A patch to one of those
// files labelled with the other step's ID is a mislabelled call, not a breach —
// the file is one the plan named — and stepOwningPath exists to rebind it.
//
// Ladder rung 5 on 2026-08-03 lost two attempts of five to "tool path is
// outside plan step \"step-001\" scope" with the path being
// cmd/generated/main.go, a file the plan names.
func TestAPatchToAClosedStepsFileIsRebound(t *testing.T) {
	plan := PlanProjection{
		Revision: 1,
		Steps: []PlanStep{
			{
				ID: "step-001", Kind: StepKindPatch, State: StepImplemented,
				MaterialEdit: true, ValidationRequired: true,
				ExpectedFiles:   []string{"cmd/generated/main_test.go"},
				CompletionTools: []executor.ToolName{executor.ToolApplyPatch},
			},
			{
				ID: "step-002", Kind: StepKindPatch, State: StepImplemented,
				MaterialEdit: true, ValidationRequired: true,
				ExpectedFiles:   []string{"cmd/generated/main.go"},
				CompletionTools: []executor.ToolName{executor.ToolApplyPatch},
			},
		},
	}
	request := executor.ToolRequest{
		Name: executor.ToolApplyPatch,
		Arguments: []executor.ToolArgument{
			{Name: "path", Value: "cmd/generated/main.go"},
			{Name: "patch", Value: "*** Update File: cmd/generated/main.go\n@@\n-a\n+b\n"},
		},
	}

	// The label the model sent is step-001, whose file this is not.
	if err := validateToolStepPathScope(plan.Steps[0], request); err == nil {
		t.Fatal("the fixture does not reproduce a mislabelled call, so it " +
			"proves nothing")
	}
	index, ok := stepOwningPath(plan, request)
	if !ok {
		t.Fatal("no step was found owning a path the plan names, so the call " +
			"is refused and the whole attempt is discarded")
	}
	if plan.Steps[index].ID != "step-002" {
		t.Errorf("rebound to %q, want step-002, which is the step that names "+
			"this file", plan.Steps[index].ID)
	}
}

// TestAPatchIsBoundByWhatItsPayloadSaysItChanges is the disagreement that
// discarded whole attempts.
//
// A patch states its target twice: the tool takes a path argument, and the diff
// opens with "*** Update File: <path>" — the line the tool descriptor tells the
// model to write, and the line the patch is actually applied against. When they
// differ, the argument is the label and the payload is the work.
//
// Ladder rung 5 on 2026-08-03 lost two attempts of five to "tool path is
// outside plan step scope" while every patch payload named a file the plan
// owned. A refused call never reaches the journal, so the mislabelled argument
// left no trace of itself either.
func TestAPatchIsBoundByWhatItsPayloadSaysItChanges(t *testing.T) {
	plan := PlanProjection{
		Revision: 1,
		Steps: []PlanStep{{
			ID: "step-001", Kind: StepKindPatch, State: StepImplemented,
			MaterialEdit: true, ValidationRequired: true,
			ExpectedFiles:   []string{"cmd/generated/main.go"},
			CompletionTools: []executor.ToolName{executor.ToolApplyPatch},
		}},
	}
	// The argument names the workspace stub; the payload names the plan's file.
	request := executor.ToolRequest{
		Name: executor.ToolApplyPatch,
		Arguments: []executor.ToolArgument{
			{Name: "path", Value: "main.go"},
			{Name: "patch", Value: "*** Begin Patch\n" +
				"*** Update File: cmd/generated/main.go\n@@\n-a\n+b\n"},
		},
	}

	index, ok := stepOwningPath(plan, request)
	if !ok {
		t.Fatal("a patch whose payload names a file the plan owns was refused, " +
			"discarding the attempt")
	}
	if plan.Steps[index].ID != "step-001" {
		t.Errorf("bound to %q, want step-001", plan.Steps[index].ID)
	}
}

// TestAPatchNamingNothingThePlanOwnsIsStillRefused is the control.
//
// Reading the payload widens which label is forgivable, not which files may be
// written. A patch whose target is named by no step under either reading is
// reaching outside the plan, and that is the contract this check exists to
// keep.
func TestAPatchNamingNothingThePlanOwnsIsStillRefused(t *testing.T) {
	plan := PlanProjection{
		Revision: 1,
		Steps: []PlanStep{{
			ID: "step-001", Kind: StepKindPatch, State: StepPending,
			MaterialEdit: true, ValidationRequired: true,
			ExpectedFiles:   []string{"cmd/generated/main.go"},
			CompletionTools: []executor.ToolName{executor.ToolApplyPatch},
		}},
	}
	request := executor.ToolRequest{
		Name: executor.ToolApplyPatch,
		Arguments: []executor.ToolArgument{
			{Name: "path", Value: "elsewhere.go"},
			{Name: "patch", Value: "*** Update File: also/elsewhere.go\n@@\n-a\n+b\n"},
		},
	}
	if _, ok := stepOwningPath(plan, request); ok {
		t.Error("a patch reaching outside every step's files was bound to one " +
			"anyway")
	}
}

// TestReachingOutsideThePlanCostsARoundNotTheAttempt is the price of a
// correctable mistake.
//
// The contract is unchanged and the write does not happen. What changes is what
// proposing it costs: ending the attempt spends sixty seconds to say what one
// round says better, while the run still has the file open.
//
// On a generated workspace the wrong file is nearly always the same one — the
// stub main.go the run is shown in its opening context, mistaken for the
// cmd/generated/main.go the plan owns. Ladder rung 9 on 2026-08-03 lost three
// of nine attempts to it.
func TestReachingOutsideThePlanCostsARoundNotTheAttempt(t *testing.T) {
	harness := newLoopHarness(t)
	var observed []ModelInput
	harness.model.steps = []modelStep{
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "wrong-file",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"package main"}`,
				"implementation", false,
			), nil
		},
		// Told which files it may change, it aims the same change correctly.
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			return successfulToolTurn(
				harness.model.identity, newModelRequestID(t), "right-file",
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
	// The plan owns a different file from the one the first turn reaches for.
	harness.input.Plan.Steps[0].ExpectedFiles = []string{"cmd/generated/main.go"}
	harness.tools.resultFor = func(request executor.ToolRequest) executor.ToolResult {
		return executor.ToolResult{
			RequestID: request.ID, SchemaVersion: executor.ToolSchemaVersion,
			State: "succeeded", ExitCode: 0, Summary: "ok",
		}
	}

	if _, err := harness.loop.Run(t.Context(), harness.input); err != nil {
		t.Fatalf("a write outside the plan ended the attempt: %v", err)
	}
	if len(observed) < 2 {
		t.Fatalf("the loop saw %d round(s); the refusal ended the attempt "+
			"rather than costing a round", len(observed))
	}
	// The answer has to name the files the run may change, or it is told only
	// that it was wrong.
	told := observed[1].PreviousResults
	if len(told) != 1 || !told[0].IsError {
		t.Fatalf("the refused round was not told why: %+v", told)
	}
	if !strings.Contains(told[0].StdoutRedacted, "cmd/generated/main.go") {
		t.Errorf("the refusal does not name the file the plan owns:\n%s",
			told[0].StdoutRedacted)
	}
	if !strings.Contains(told[0].StdoutRedacted, "Nothing was written") {
		t.Errorf("the refusal does not say the write did not happen:\n%s",
			told[0].StdoutRedacted)
	}
}
