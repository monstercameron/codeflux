package coordinator

import (
	"context"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
)

// describeContracts states what each produced function promises.
//
// A contract here is derived from the code rather than agreed before it, which
// is the weaker of the two possible orders and is said plainly rather than
// dressed up: a contract read off an implementation cannot disagree with it,
// so it cannot catch the implementation being wrong. What it can do is make
// the promise explicit, comparable between runs, and available to a later run
// deciding whether an atom it needs already exists.
func describeContracts(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	contracts := map[string]any{}
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		contracts[function.Name] = map[string]any{
			"file":       function.File,
			"signature":  function.Signature,
			"inputs":     function.Parameters,
			"outputs":    function.Results,
			"errors":     function.ReturnsError,
			"effects":    effectsOf(function),
			"pure":       function.Pure,
			"exported":   function.Exported,
			"calls":      function.Calls,
			"branches":   function.Branches,
			"complexity": complexityLabel(function.LoopDepth),
		}
	}
	if len(contracts) == 0 {
		return skipped("the run produced no function to describe")
	}
	return held(fmt.Sprintf(
		"%d function contract(s) recorded, derived from the code rather than "+
			"agreed before it", len(contracts)),
		map[string]any{
			"contracts":     contracts,
			"derived_after": true,
		})
}

// recallKnownAtoms moved to agent_stage_recall.go (PIPE-050/PIPE-051): the
// recall stage is now bound rather than advisory, and it belongs beside the
// keys it matches on.

// assembleEvidence gathers everything the run can show for itself.
//
// The bundle is not a summary. It states what held, what did not, and — the
// part that makes it worth reading — what was never examined at all, because a
// reader deciding whether to trust this work needs the third list more than
// the first two.
func (execution *AgentExecution) assembleEvidence(
	ctx context.Context,
	taskID domain.TaskID,
	attempt uint64,
) (stageOutcome, bool) {
	// The attempt is passed in rather than assumed to be one (PIPE-003). A
	// bundle assembled from attempt one while the run wrote attempt two
	// describes the previous run and says nothing about this one.
	recorded, err := execution.repositories.ListPipelineStages(ctx, taskID, attempt)
	if err != nil {
		return broke("the run's own record could not be read: "+err.Error(), nil), false
	}
	counts := map[pipeline.State]int{}
	var failed, unexamined []string
	for _, record := range recorded {
		counts[record.State]++
		switch record.State {
		case pipeline.StateFailed:
			failed = append(failed, record.Name)
		case pipeline.StateNotImplemented, pipeline.StateBlocked,
			pipeline.StateSkipped:
			unexamined = append(unexamined, record.Name)
		}
	}
	evidence := map[string]any{
		"stages_total":     len(recorded),
		"satisfied":        counts[pipeline.StateSatisfied],
		"failed":           failed,
		"not_examined":     unexamined,
		"artifacts_stored": countArtifacts(ctx, execution.repositories, taskID),
	}
	// The bundle is complete whether or not the news is good. Refusing to
	// assemble it on a failing run would remove the evidence exactly when
	// somebody most needs to read it.
	// The bundle is assembled before the delivery stages are recorded, so it
	// counts what came before it and says so. Both numbers are derived from
	// len(pipeline.Flow) rather than written out: claiming a total the flow no
	// longer has would be the bundle's first inaccuracy, in the one artifact
	// whose whole purpose is to be accurate (PIPE-007).
	evidence["stages_not_yet_recorded"] = len(pipeline.Flow) - len(recorded)
	return held(fmt.Sprintf(
		"of the %d stage(s) recorded before this one: %d satisfied, %d failed, "+
			"%d never examined; the %d delivery stage(s) after it are recorded "+
			"separately",
		len(recorded), counts[pipeline.StateSatisfied], len(failed),
		len(unexamined), len(pipeline.Flow)-len(recorded)),
		evidence), len(failed) == 0
}

// countArtifacts reports how much of the work survives the worktree.
func countArtifacts(
	ctx context.Context,
	repositories *storage.Repositories,
	taskID domain.TaskID,
) int {
	stored, err := repositories.ListTaskArtifacts(ctx, taskID)
	if err != nil {
		return 0
	}
	return len(stored)
}

// effectsOf names what a function does beyond returning a value.
//
// The contract's gate asks for declared effects, and "pure" alone does not say
// what an impure function actually touches. This is coarse — it distinguishes
// reaching outside from not — and coarse and true beats precise and invented.
func effectsOf(function producedFunction) []string {
	if function.Pure {
		return []string{}
	}
	return []string{"reaches outside its arguments"}
}
