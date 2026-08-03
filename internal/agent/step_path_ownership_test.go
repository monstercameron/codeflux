package agent

import (
	"testing"

	"codeflux.dev/codeflux/internal/executor"
)

// planWithSteps builds a two-step plan whose steps each name one file.
func planWithSteps() PlanProjection {
	return PlanProjection{
		Revision: 1,
		Steps: []PlanStep{
			{
				ID: "step-001", Kind: StepKindEdit, State: StepPending,
				ExpectedFiles: []string{"cmd/generated/main.go"},
			},
			{
				ID: "step-002", Kind: StepKindEdit, State: StepPending,
				ExpectedFiles: []string{"cmd/generated/main_test.go"},
			},
		},
	}
}

// writeTo builds an apply-edit request for one path.
func writeTo(path string) executor.ToolRequest {
	return executor.ToolRequest{
		Name: executor.ToolApplyEdit,
		Arguments: []executor.ToolArgument{
			{Name: "path", Value: path},
			{Name: "content", Value: "package main\n"},
		},
	}
}

// TestAMislabelledWriteIsBoundToTheStepThatOwnsItsPath is the correction the
// loop needed.
//
// Observed on ladder rung 5 on 2026-08-03: two of six attempts were discarded
// with "tool path is outside plan step \"step-002\" scope" — a third of the
// run's budget — because the model attributed a write of main.go to the step
// that names main_test.go. The plan was not being violated; the file was one
// the plan itself asked for, written by a call that misfiled itself.
func TestAMislabelledWriteIsBoundToTheStepThatOwnsItsPath(t *testing.T) {
	plan := planWithSteps()
	index, ok := stepOwningPath(plan, writeTo("cmd/generated/main.go"))
	if !ok {
		t.Fatal("exactly one step names this path, so it can be bound to it")
	}
	if plan.Steps[index].ID != "step-001" {
		t.Errorf("bound to %q, want step-001", plan.Steps[index].ID)
	}
}

// TestAPathNoStepNamesIsStillRefused is the first control.
//
// The contract is unchanged: a file the plan never asked for has no step to
// belong to, and reaching outside the plan is exactly what the step scope
// exists to refuse.
func TestAPathNoStepNamesIsStillRefused(t *testing.T) {
	if _, ok := stepOwningPath(planWithSteps(), writeTo("cmd/other/main.go")); ok {
		t.Error("a path no step names must not be bound to any step")
	}
}

// TestAnAmbiguousPathIsStillRefused is the second control, and the one that
// keeps this from being a hole.
//
// When several steps name the same file, picking one attributes work by luck.
// That is the failure the step scope exists to prevent, not one worth trading
// away for convenience.
func TestAnAmbiguousPathIsStillRefused(t *testing.T) {
	plan := planWithSteps()
	plan.Steps[1].ExpectedFiles = []string{"cmd/generated/main.go"}
	if _, ok := stepOwningPath(plan, writeTo("cmd/generated/main.go")); ok {
		t.Error("two steps name this path, so ownership is ambiguous and must " +
			"be refused rather than guessed")
	}
}

// TestACompletedStepDoesNotClaimAPath pins that only executable steps count.
//
// A step already implemented has had its write; binding a second one to it
// would let a turn rewrite finished work under a step that is closed.
func TestACompletedStepDoesNotClaimAPath(t *testing.T) {
	plan := planWithSteps()
	plan.Steps[0].State = StepImplemented
	if _, ok := stepOwningPath(plan, writeTo("cmd/generated/main.go")); ok {
		t.Error("a step that is no longer executable must not claim a path")
	}
}
