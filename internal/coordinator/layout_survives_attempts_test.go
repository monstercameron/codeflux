package coordinator

import (
	"slices"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
)

// TestTheLayoutSurvivesTheSecondAttempt is the defect that made every rung
// above 15 unwinnable.
//
// A run decides where its work goes once. Every attempt after the first
// rebuilds its steps so the step kinds can follow the filesystem — attempt one
// creates a file, attempt three patches the file attempt one wrote — and that
// rebuild re-derived the layout as well, through the fallback parser, which
// answers cmd/generated/main.go for any request naming no path.
//
// So a run whose planner chose cmd/stats and stats wrote there on attempt one
// and was told to write somewhere else on attempt two, abandoning everything it
// had built. Ladder rung 16 on 2026-08-04 shows it exactly: the planner's
// layout is in all seven attempts' context, and the model is editing
// cmd/generated, asking for direction about cmd/generated, and rewriting
// package clauses trying to reconcile the two.
//
// Proven to discriminate: agentPlanSteps on this requirement returns the
// fallback pair, which is a different directory from the one the run started
// in.
func TestTheLayoutSurvivesTheSecondAttempt(t *testing.T) {
	worktree := t.TempDir()
	const requirement = "Write a program in the module codeflux.test/workspace. " +
		"The layout is yours. One package exports Mean and Max; the command " +
		"imports that package and prints them."

	// What a planner layout produces on the first attempt.
	chosen := layoutFromPlanner([]string{
		"stats/stats.go", "cmd/stats/main.go",
	})
	if len(chosen) == 0 {
		t.Fatal("the fixture needs a layout")
	}
	first := agentPlanStepsForFiles(worktree, requirement, chosen)
	layout := filesInSteps(first)

	// What the second attempt used to do, and what it does now.
	rederived := filesInSteps(agentPlanSteps(worktree, requirement))
	rebuilt := filesInSteps(agentPlanStepsForFiles(worktree, requirement, layout))

	if slices.Equal(rederived, layout) {
		t.Fatal("the fixture cannot discriminate: re-deriving happens to " +
			"produce the same layout, so nothing here is being asserted")
	}
	if !slices.Equal(rebuilt, layout) {
		t.Errorf("the second attempt writes %v where the first wrote %v",
			rebuilt, layout)
	}
	for _, file := range rebuilt {
		if !slices.Contains(layout, file) {
			t.Errorf("%s is not a file this run ever planned to write", file)
		}
	}
}

// TestTheStepKindsStillFollowTheFilesystem is the control.
//
// Holding the layout fixed must not freeze the step kinds with it. Which files
// a run writes is a decision; whether each one is created or patched is a fact
// about the disk, and it changes between attempts precisely because the first
// attempt created them.
func TestTheStepKindsStillFollowTheFilesystem(t *testing.T) {
	worktree := t.TempDir()
	layout := []string{"cmd/x/main.go", "cmd/x/main_test.go"}

	before := agentPlanStepsForFiles(worktree, "Write a program.", layout)
	if !hasKind(before, agentloop.StepKindEdit) {
		t.Fatal("a file that does not exist has to be created")
	}

	writeWorktree(t, map[string]string{
		"cmd/x/main.go":      "package main\n\nfunc main() {}\n",
		"cmd/x/main_test.go": "package main\n",
	})
	// Same fixture directory, so the files are now present for this worktree.
	present := t.TempDir()
	_ = present
	after := agentPlanStepsForFiles(worktreeWithFiles(t, layout), "Write a program.", layout)
	if !hasKind(after, agentloop.StepKindPatch) {
		t.Error("a file that exists is rewritten whole instead of patched, " +
			"which is the churn the patch tool exists to stop")
	}
}

func hasKind(steps []agentloop.PlanStep, kind agentloop.StepKind) bool {
	for _, step := range steps {
		if step.Kind == kind {
			return true
		}
	}
	return false
}

// worktreeWithFiles is a worktree in which every named path already exists.
func worktreeWithFiles(t *testing.T, files []string) string {
	t.Helper()
	contents := map[string]string{}
	for _, file := range files {
		contents[file] = "package main\n"
	}
	return writeWorktree(t, contents)
}
