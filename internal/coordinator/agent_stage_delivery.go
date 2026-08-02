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

// recallKnownAtoms reports what this run could have reused.
//
// Reuse is the reason atoms are documented at all: a run that rebuilds a
// function the project already has spends tokens producing something it owned.
// The registry is the artifacts of earlier runs, so a first run recalls
// nothing and says so, and the answer only becomes interesting once the
// project has a history.
func (execution *AgentExecution) recallKnownAtoms(
	ctx context.Context,
	scope agentScope,
	worktree string,
) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	wanted := map[string]bool{}
	for _, function := range functions {
		if !isTestScaffolding(function) {
			wanted[function.Name] = true
		}
	}
	if len(wanted) == 0 {
		return skipped("the run needed no atom, so there was nothing to recall")
	}
	// Earlier work means earlier: the artifacts this run has already stored
	// are excluded. Without that, a run matches against the file it wrote
	// moments ago and reports that everything it needed already existed —
	// which is true, and is the least useful true thing it could say.
	known, err := execution.repositories.ListProjectSourceArtifactsExcludingTask(
		ctx, scope.projectID, scope.taskID, 200)
	if err != nil {
		return broke("the project's earlier work could not be read: "+
			err.Error(), nil)
	}
	var reusable []string
	for name := range wanted {
		for _, artifact := range known {
			if strings.Contains(string(artifact.Content), "func "+name+"(") {
				reusable = append(reusable, name)
				break
			}
		}
	}
	sort.Strings(reusable)
	evidence := map[string]any{
		"functions_needed":   len(wanted),
		"already_in_project": reusable,
		"artifacts_searched": len(known),
	}
	if len(known) == 0 {
		return held("the project holds no earlier work, so nothing could be "+
			"reused and nothing was rebuilt unnecessarily", evidence)
	}
	return held(fmt.Sprintf(
		"%d function(s) needed; %d already exist elsewhere in the project",
		len(wanted), len(reusable)), evidence)
}

// documentAtoms records what each produced function is, for the next run.
//
// The documentation is generated from what the checks established rather than
// from what anyone intended: whether the function reaches outside itself, what
// it composes, how deeply it loops. That is a narrower account than a person
// would write and it has the advantage of being true, and it is what makes an
// atom findable later by something other than its name.
func documentAtoms(worktree string) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
	}
	documented := map[string]any{}
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		documented[function.Name] = map[string]any{
			// The four things a reader needs, in the atom-documentation
			// schema's own vocabulary.
			"purpose": describePurpose(function),
			"inputs":  "declared by its signature in " + function.File,
			"outputs": "declared by its signature in " + function.File,
			"algorithm": fmt.Sprintf(
				"%d decision point(s), loop nesting %d, composes %s",
				function.Branches, function.LoopDepth,
				describeCalls(function.Calls)),
			"complexity": complexityLabel(function.LoopDepth),
			"pure":       function.Pure,
		}
	}
	if len(documented) == 0 {
		return skipped("the run produced no function to document")
	}
	return held(fmt.Sprintf(
		"%d function(s) documented with purpose, inputs, outputs, algorithm, "+
			"and complexity", len(documented)),
		map[string]any{"documented": documented})
}

// describePurpose says what a function is for, from what it is.
func describePurpose(function producedFunction) string {
	switch {
	case function.Name == "main":
		return "the program's entry point: reads input, calls the work, writes output"
	case function.Pure && len(function.Calls) == 0:
		return "a pure atom: computes its result from its arguments alone"
	case function.Pure:
		return "a pure composition: joins other functions without reaching outside"
	case len(function.Calls) == 0:
		return "an atom with effects: reaches outside itself to do its work"
	default:
		return "a composition with effects: joins other functions and reaches outside"
	}
}

// describeCalls renders what a function composes.
func describeCalls(calls []string) string {
	if len(calls) == 0 {
		return "nothing else"
	}
	return strings.Join(calls, ", ")
}

// assembleEvidence gathers everything the run can show for itself.
//
// The bundle is not a summary. It states what held, what did not, and — the
// part that makes it worth reading — what was never examined at all, because a
// reader deciding whether to trust this work needs the third list more than
// the first two.
func (execution *AgentExecution) assembleEvidence(
	ctx context.Context,
	taskID domain.TaskID,
) (stageOutcome, bool) {
	recorded, err := execution.repositories.ListPipelineStages(ctx, taskID, 1)
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
	// The bundle is assembled before the three delivery stages are recorded,
	// so it counts what came before it and says so. Reporting a total of
	// thirty-five while holding thirty-two would be the bundle's first
	// inaccuracy, in the one artifact whose whole purpose is to be accurate.
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
