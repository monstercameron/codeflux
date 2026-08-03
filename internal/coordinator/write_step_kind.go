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
