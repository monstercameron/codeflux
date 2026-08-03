package coordinator

import (
	"os"
	"path/filepath"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
)

// TestEveryPlanStepIsAcceptedByTheLoopThatWillValidateIt is the test that would
// have caught the defect before a run paid for it.
//
// The plan builder and the loop's validator hold the same fact — which tool
// completes which kind of step — in two places. They drifted: the builder
// listed two tools for the edit kind while the validator requires exactly the
// one its kind names, so every plan was refused before the first prompt was
// sent. Three attempts, zero files written, zero tests run, and a pipeline dump
// describing stages nothing had reached.
//
// Each file was individually defensible. Only the relationship was wrong, which
// is the kind of defect that comes back unless something asserts the
// relationship.
func TestEveryPlanStepIsAcceptedByTheLoopThatWillValidateIt(t *testing.T) {
	worktree := t.TempDir()
	requirement := "Write cmd/generated/main.go and cmd/generated/main_test.go " +
		"for a program that prints a line."

	// Before anything exists, and after: the plan is rebuilt every attempt, so
	// both shapes have to pass the same validator.
	for _, when := range []string{"empty worktree", "after the files exist"} {
		t.Run(when, func(t *testing.T) {
			for _, step := range agentPlanSteps(worktree, requirement) {
				if len(step.CompletionTools) == 0 {
					t.Errorf("step %q declares no completion tool, so no round "+
						"will ever offer a tool that finishes it", step.ID)
					continue
				}
				if err := agentloop.ValidatePlanStep(step); err != nil {
					t.Errorf("step %q (kind %s, tools %v) is refused by the "+
						"loop that validates it: %v",
						step.ID, step.Kind, step.CompletionTools, err)
				}
			}
		})
		// Create the files, so the second pass sees the refinement shape.
		for _, path := range []string{
			"cmd/generated/main.go", "cmd/generated/main_test.go",
		} {
			full := filepath.Join(worktree, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte("package main\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestAWriteStepAsksForTheToolItsKindNames pins the one mapping.
func TestAWriteStepAsksForTheToolItsKindNames(t *testing.T) {
	if got := writeToolFor(agentloop.StepKindEdit); got != executor.ToolApplyEdit {
		t.Errorf("the edit kind maps to %s", got)
	}
	if got := writeToolFor(agentloop.StepKindPatch); got != executor.ToolApplyPatch {
		t.Errorf("the patch kind maps to %s", got)
	}
}

// TestTheStepKindFollowsTheFilesystem is the create-then-modify transition.
//
// A file that does not exist has to be created, and creating it means sending
// its whole contents: there is no context to patch against. A file that does
// exist is being changed, and re-sending every line to change a few is the
// churn the patch tool exists to stop.
func TestTheStepKindFollowsTheFilesystem(t *testing.T) {
	worktree := t.TempDir()
	if got := writeStepKindFor(worktree, "cmd/generated/main.go"); got != agentloop.StepKindEdit {
		t.Errorf("a file that does not exist is planned as %s", got)
	}
	full := filepath.Join(worktree, "cmd", "generated", "main.go")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := writeStepKindFor(worktree, "cmd/generated/main.go"); got != agentloop.StepKindPatch {
		t.Errorf("a file that exists is planned as %s, so refinement would "+
			"rewrite it whole", got)
	}
}
