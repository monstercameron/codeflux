package coordinator

import (
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
)

// TestACreatingStepCanStillPatchTheFileItJustCreated is the transition that
// cost ladder rung 9 an attempt.
//
// The step kind is resolved once, when the plan is built, from whether the file
// is on disk. In an empty worktree every write step is an edit, and that stops
// being true the moment the step's own first call lands: the file is there now,
// and the rest of the attempt is revising it.
//
// The two write tools then disagreed with each other. apply-edit refuses a
// second wholesale rewrite of a file and tells the run to send a patch instead
// — correct advice, on a step whose only permitted tool was apply-edit. There
// was no call the run could make that would change the file.
//
// Proven to discriminate: against the previous implementation, where
// writeToolsFor returned one tool for the edit kind, this records
// "an edit step permits [apply-edit], so the run has no way to revise the file
// its first call created". Rung 9's pass 4 on 2026-08-03 is what that looks
// like when it is paid for: the fix for its one failing test was written at
// 41.9s and refused, rewritten at 50.8s and refused again, and the attempt
// ended reporting that its tests did not pass.
func TestACreatingStepCanStillPatchTheFileItJustCreated(t *testing.T) {
	tools := writeToolsFor(agentloop.StepKindEdit)
	if !containsTool(tools, executor.ToolApplyEdit) {
		t.Fatalf("an edit step must permit %s: it is how the file is created "+
			"at all, and there is no context to patch against, got %v",
			executor.ToolApplyEdit, tools)
	}
	if !containsTool(tools, executor.ToolApplyPatch) {
		t.Fatalf("an edit step permits %v, so the run has no way to revise the "+
			"file its first call created", tools)
	}
	if tools[0] != executor.ToolApplyEdit {
		t.Errorf("the tool the step is named for should come first, so a run "+
			"reaches for it before the file exists, got %v", tools)
	}
}

// TestAPatchStepIsNotWidenedTheSameWay is the control, and it is what keeps the
// change from being a hole rather than a correction.
//
// The widening is about a fact that changes during the attempt. On a patch step
// that fact does not change: the file existed when the plan was built and it
// exists now. Admitting a wholesale rewrite there would re-open the churn the
// patch tool was added to stop — every line re-sent to move a few, and every
// line re-sent a chance to drop something that was working.
func TestAPatchStepIsNotWidenedTheSameWay(t *testing.T) {
	tools := writeToolsFor(agentloop.StepKindPatch)
	if containsTool(tools, executor.ToolApplyEdit) {
		t.Fatalf("a patch step must not permit %s: the file exists, so a "+
			"wholesale rewrite is the churn the patch tool exists to stop, "+
			"got %v", executor.ToolApplyEdit, tools)
	}
	if len(tools) != 1 || tools[0] != executor.ToolApplyPatch {
		t.Errorf("a patch step is completed by %s alone, got %v",
			executor.ToolApplyPatch, tools)
	}
}

// TestTheLoopAcceptsBothToolsOnAnEditStep is the same agreement
// TestEveryPlanStepIsAcceptedByTheLoopThatWillValidateIt asserts, stated
// directly against the pair.
//
// The builder and the loop's contract validator hold one fact in two places,
// and they have drifted before: the builder listed two tools for the edit kind
// while the validator required exactly the one its kind named, and every plan
// was refused before the first prompt was sent. Widening the builder without
// widening the validator would reproduce that exactly.
func TestTheLoopAcceptsBothToolsOnAnEditStep(t *testing.T) {
	step := agentloop.PlanStep{
		ID: "edit-1", Kind: agentloop.StepKindEdit,
		State:              agentloop.StepPending,
		SummaryRedacted:    "Write cmd/generated/main.go",
		MaterialEdit:       true,
		ValidationRequired: true,
		ExpectedFiles:      []string{"cmd/generated/main.go"},
		CompletionTools:    writeToolsFor(agentloop.StepKindEdit),
	}
	if err := agentloop.ValidatePlanStep(step); err != nil {
		t.Fatalf("the loop refuses the plan the builder produces, so no prompt "+
			"is ever sent: %v", err)
	}

	// And the widening is a pair, not a licence: a patch step declaring the
	// rewrite tool as well is still refused.
	step.Kind = agentloop.StepKindPatch
	step.CompletionTools = []executor.ToolName{
		executor.ToolApplyPatch, executor.ToolApplyEdit,
	}
	if err := agentloop.ValidatePlanStep(step); err == nil {
		t.Error("a patch step declaring the rewrite tool must be refused, or " +
			"the contract admits any pairing somebody writes")
	}
}

// containsTool is the membership test these three cases share.
func containsTool(tools []executor.ToolName, wanted executor.ToolName) bool {
	for _, tool := range tools {
		if tool == wanted {
			return true
		}
	}
	return false
}
