package coordinator

import (
	"context"

	"codeflux.dev/codeflux/internal/pipeline"
)

// RecheckedStage is one stage decided against an existing worktree.
type RecheckedStage struct {
	Number pipeline.Number
	Name   string
	Gate   string
	State  pipeline.State
	Detail string
}

// RecheckWorktree decides every stage that reads only the worktree, against a
// worktree some earlier run already produced.
//
// It exists because the two halves of a run cost wildly different amounts. The
// model calls that produce the code take minutes and real money; the checks
// that judge it take seconds and nothing. Iterating on a gate meant paying for
// the first half again every time, so a one-line fix to a check cost a full
// paid run to evaluate — and on a defect that only appears at stage seven,
// several of them.
//
// This runs the second half alone. Point it at the worktree a run left behind
// and it reports what the flow would say about that code now.
//
// It is deliberately not the whole flow. Stages that need a run's own context
// — its plan, its acceptance examples, its attribution, its provider — are not
// decided here and are absent from the result rather than guessed at. A
// diagnostic that reported thirty-one stages as though it had judged
// forty-one would be exactly the kind of overclaim the ledger exists to
// prevent.
func RecheckWorktree(ctx context.Context, worktree string) []RecheckedStage {
	cache := newProducedFunctionCache(worktree)
	// Nothing is attributed, because attribution is a fact about a run and
	// there is no run here. The zero value fails toward including everything,
	// which is the same direction the flow itself takes when a base revision
	// cannot be established, so a stage judged here is held to the wider
	// standard rather than a silently narrower one.
	var attribution changeAttribution

	decided := []struct {
		stage   pipeline.Number
		outcome stageOutcome
	}{
		{pipeline.StageAtomCaseSynthesis, checkCaseCoverage(worktree)},
		{pipeline.StageAtomExampleTests, checkAtomTests(worktree, cache)},
		{pipeline.StageAtomPropertyTests, checkPropertyTests(worktree)},
		{pipeline.StageAtoms, checkAtoms(worktree, cache)},
		{pipeline.StageAtomVerification, checkAtomVerification(ctx, worktree)},
		{pipeline.StageAtomFuzz, checkFuzzing(ctx, worktree)},
		{pipeline.StageAntiPatterns, checkAntiPatterns(worktree, attribution)},
		{pipeline.StageAtomOptimization, checkSimplification(worktree, attribution)},
		{pipeline.StageAtomComplexity, checkComplexity(worktree, attribution)},
		{pipeline.StageAtomDocumentation, checkAtomDocumentation(worktree, cache)},
		{pipeline.StageMoleculeTests, checkMoleculeTests(worktree)},
		{pipeline.StageMolecules, checkMolecules(worktree, cache)},
		{pipeline.StageControlTests, checkControlTests(worktree)},
		{pipeline.StageControlFlow, checkControlFlow(worktree)},
		{pipeline.StageRepetition, checkRepetition(ctx, worktree)},
	}

	results := make([]RecheckedStage, 0, len(decided))
	for _, item := range decided {
		name, gate := "unknown", ""
		if declared, found := pipeline.StageByNumber(item.stage); found {
			name, gate = declared.Name, declared.Gate
		}
		results = append(results, RecheckedStage{
			Number: item.stage, Name: name, Gate: gate,
			State:  stateOfOutcome(item.outcome),
			Detail: item.outcome.Detail,
		})
	}
	return results
}

// stateOfOutcome renders one check's verdict in the ledger's vocabulary.
func stateOfOutcome(outcome stageOutcome) pipeline.State {
	switch {
	case outcome.Skipped:
		return pipeline.StateSkipped
	case outcome.Held:
		return pipeline.StateSatisfied
	default:
		return pipeline.StateFailed
	}
}
