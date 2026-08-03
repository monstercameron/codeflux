package coordinator

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
)

// describeContracts states what each produced function promises, and —
// PIPE-137/PIPE-140 — checks that promise against what the function actually
// does.
//
// A contract used to be read off the finished implementation: "effects" and
// "pure" were computed by re-walking the same function body checkAtoms would
// later re-walk to decide the very thing the contract claimed. Two facts
// computed from one source by the same method cannot disagree, so the
// contract could not fail to describe its own implementation — it was
// unfalsifiable by construction, whatever the code actually was.
//
// The declared half of a contract now comes from somewhere else entirely:
// declaredContracts reads the //codeflux:atom doc comment AGENTS.md's "Atom
// Documentation Style" already requires, which is text the atom's author
// wrote, independent of the AST walk that decides what the body observably
// does. Preconditions, postconditions, and declared effects come from there.
// Signature, parameter/result types, and whether a function returns an error
// are left as objective facts read from the type signature — those were
// never the vacuous half; a function's own declared parameter types cannot
// disagree with its parameter types.
//
// A function with no admitted directive, or an unparsable one, has no
// declared contract and is recorded as such rather than having one invented
// for it from its body — inventing one is exactly the defect being removed.
// When nothing produced carries a declared contract, this stage cannot yet
// establish the gate and records skipped, not satisfied, matching the
// vacuity guard PIPE-010 and PIPE-011 established. When a declared contract
// disagrees with what was observed — a function that declares no effects but
// reaches outside its own arguments — that is a real, checkable
// contradiction and the stage fails, naming it (PIPE-140): this is what
// makes the declared-effects half of an atom's contract worth anything,
// where before nothing ever compared it against reality.
func describeContracts(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	files, err := producedGoFiles(worktree)
	if err != nil {
		return broke("the produced source could not be read: "+err.Error(), nil)
	}
	declared, err := declaredContracts(worktree, files)
	if err != nil {
		return broke("the declared atom documentation could not be read: "+
			err.Error(), nil)
	}

	contracts := map[string]any{}
	var undeclared []string
	var disagreements []string
	declaredCount := 0
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		entry := map[string]any{
			"file":       function.File,
			"signature":  function.Signature,
			"inputs":     function.Parameters,
			"outputs":    function.Results,
			"errors":     function.ReturnsError,
			"exported":   function.Exported,
			"calls":      function.Calls,
			"branches":   function.Branches,
			"complexity": complexityLabel(function.LoopDepth),
			// The observed half: what the body was actually seen to do,
			// independent of anything declared about it.
			"observed_effects": function.Effects,
			"observed_pure":    function.Pure,
		}
		document, hasDeclaration := declared[function.Name]
		if !hasDeclaration {
			entry["declared"] = false
			contracts[function.Name] = entry
			undeclared = append(undeclared, function.Name)
			continue
		}
		declaredCount++
		declaredPure := declaredPurity(document)
		entry["declared"] = true
		entry["declared_effects"] = declaredEffectNames(document)
		entry["declared_pure"] = declaredPure
		entry["preconditions"] = document.Preconditions.Items
		entry["postconditions"] = document.Postconditions.Items
		contracts[function.Name] = entry
		if declaredPure && !function.Pure {
			disagreements = append(disagreements, fmt.Sprintf(
				"%s declares \"Effects: None: pure atom\" but its body reaches "+
					"outside its arguments: %s",
				function.Name, strings.Join(function.Effects, ", ")))
		}
	}
	sort.Strings(undeclared)
	sort.Strings(disagreements)
	evidence := map[string]any{
		"contracts": contracts,
		// false now for the entries this can vouch for: a declared entry's
		// effects field is text the author wrote before this check ever ran,
		// not a re-derivation of what the check itself just observed.
		"derived_after":    false,
		"declared_count":   declaredCount,
		"undeclared_count": len(undeclared),
		"undeclared":       undeclared,
	}
	if len(contracts) == 0 {
		return skipped("the run produced no function to describe")
	}
	if len(disagreements) > 0 {
		evidence["disagreements"] = disagreements
		return broke(fmt.Sprintf(
			"%d unit(s) declare effects their implementation contradicts: %s",
			len(disagreements), strings.Join(disagreements, "; ")), evidence)
	}
	if declaredCount == 0 {
		return skippedWith(fmt.Sprintf(
			"%d function(s) produced, none carries a //codeflux:atom declared "+
				"contract to check agreement against yet", len(contracts)),
			evidence)
	}
	return held(fmt.Sprintf(
		"%d of %d function contract(s) are declared in the source rather than "+
			"read off it, and every declared one agrees with what its body does",
		declaredCount, len(contracts)), evidence)
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
// It returns producedFunction.Effects directly: the named, per-call list
// observedEffects resolves (PIPE-139), rather than the single placeholder
// string "reaches outside its arguments" this used to substitute for any
// impurity at all. Callers outside this ticket's scope
// (agent_stage_recall.go) keep the same []string shape and now see which
// specific import and name a function reached for.
func effectsOf(function producedFunction) []string {
	return function.Effects
}
