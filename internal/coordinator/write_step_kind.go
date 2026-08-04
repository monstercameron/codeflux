package coordinator

import (
	"os"
	"path/filepath"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
)

// writeStepKindFor decides how a file is going to be written, from whether it
// is already there.
//
// A file that does not exist has to be created, and creating it means sending
// its whole contents: there is no context to patch against. A file that does
// exist is being changed, and changing it by re-sending every line is the churn
// the patch tool was added to stop.
//
// Resolved from the filesystem rather than declared, because the same plan step
// means different things on different attempts. Attempt one writes main.go into
// an empty worktree; attempt three is adjusting the main.go attempt one wrote,
// and the plan is rebuilt each attempt precisely so this can be noticed.
func writeStepKindFor(worktree, path string) agentloop.StepKind {
	if worktree == "" || path == "" {
		return agentloop.StepKindEdit
	}
	if _, err := os.Stat(
		filepath.Join(worktree, filepath.FromSlash(path)),
	); err != nil {
		return agentloop.StepKindEdit
	}
	return agentloop.StepKindPatch
}

// writeToolFor is the one mapping from step kind to write tool.
//
// One mapping, used by the plan builder, the round's tool allowlist, the
// completion check and the scope rules. They had drifted into two: the plan
// declared two tools for the edit kind while the validator required exactly the
// one its kind names, so every plan was refused before a single prompt was
// sent. Three attempts, zero files written, zero tests run, and a full pipeline
// dump describing stages nothing had reached.
//
// The loop's own planStepContract is the authority for what a kind means; this
// is the coordinator's side of the same fact, and the invariant test asserts
// they agree rather than trusting that they do.
func writeToolFor(kind agentloop.StepKind) executor.ToolName {
	if kind == agentloop.StepKindPatch {
		return executor.ToolApplyPatch
	}
	return executor.ToolApplyEdit
}

// writeToolsFor is what a write step actually declares, which for a step that
// creates a file is two tools rather than one.
//
// The kind is resolved once, when the plan is built, from whether the file is
// on disk. That fact does not survive the attempt: the step's own first call
// creates the file, and every call after it is revising one that exists. The
// step kept saying "edit" while the file had moved on, and the two write tools
// disagreed with each other about it — apply-edit refused the second wholesale
// rewrite and told the run to send a patch, on a step whose only tool was
// apply-edit. Ladder rung 9 lost an attempt to it on 2026-08-03: the correct
// fix for its failing test was written at 41.9s, refused, rewritten at 50.8s,
// refused again, and the attempt ended reporting that its tests did not pass.
//
// So the plan stops trying to predict which tool the round will need and
// declares both, and the tools decide: apply-edit refuses a rewrite it has
// already accepted once, and apply-patch has nothing to match against in a file
// that is not there. A patch step is not widened the same way, because there
// the file exists from the start and a wholesale rewrite is the churn the patch
// tool was added to stop.
func writeToolsFor(kind agentloop.StepKind) []executor.ToolName {
	if kind == agentloop.StepKindPatch {
		return []executor.ToolName{executor.ToolApplyPatch}
	}
	return []executor.ToolName{executor.ToolApplyEdit, executor.ToolApplyPatch}
}
