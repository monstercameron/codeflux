package coordinator

import (
	"context"

	"codeflux.dev/codeflux/internal/pipeline"
)

// examineStructure performs every stage that can be decided from what the run
// actually produced.
//
// They are grouped here rather than scattered through the run because they
// share one precondition — there is something that builds — and because the
// order they are recorded in is the order of the flow, which is the order a
// person reads the ledger in.
//
// A stage that cannot be decided is recorded as blocked rather than passed
// over. Every one of these used to be absent, which made a run that examined
// nothing indistinguishable from a run that examined everything and found it
// good.
func (execution *AgentExecution) examineStructure(
	ctx context.Context,
	ledger *pipelineLedger,
	worktree string,
	compiles bool,
	verified bool,
) {
	if !compiles {
		// Every stage that examines what was produced is blocked, derived from
		// the flow rather than hand-listed. The list used to be written out
		// here and fell nine short: those nine reached the closing sweep and
		// were recorded as not-implemented, which had been true when nothing
		// performed them and became a lie the moment they were built. A ledger
		// that misreports its own coverage is worse than no ledger.
		for _, stage := range pipeline.Flow {
			if stage.Number < pipeline.StageContracts ||
				stage.Number > pipeline.StageEvidenceBundle {
				continue
			}
			ledger.blocked(ctx, stage.Number,
				"the module does not build, so nothing about it can be examined")
		}
		return
	}

	// Structure, from the source itself. These hold whether or not the tests
	// pass, because they are statements about what was written rather than
	// about what it does.
	ledger.decide(ctx, pipeline.StageAtoms, checkAtoms(worktree))
	ledger.decide(ctx, pipeline.StageAtomCaseSynthesis, checkCaseCoverage(worktree))
	ledger.decide(ctx, pipeline.StageAtomExampleTests, checkAtomTests(worktree))
	ledger.decide(ctx, pipeline.StageMolecules, checkMolecules(worktree))
	ledger.decide(ctx, pipeline.StageControlFlow, checkControlFlow(worktree))
	ledger.decide(ctx, pipeline.StageAntiPatterns, checkAntiPatterns(worktree))
	ledger.decide(ctx, pipeline.StageAtomComplexity, checkComplexity(worktree))
	ledger.decide(ctx, pipeline.StageContracts, describeContracts(worktree))
	ledger.decide(ctx, pipeline.StageAtomDocumentation, checkAtomDocumentation(worktree))
	ledger.decide(ctx, pipeline.StageAtomPropertyTests, checkPropertyTests(worktree))
	ledger.decide(ctx, pipeline.StageAtomOptimization, checkSimplification(worktree))
	ledger.decide(ctx, pipeline.StageCompositionObligations,
		stateCompositionObligations(worktree))
	ledger.decide(ctx, pipeline.StageMoleculeTests, checkMoleculeTests(worktree))
	ledger.decide(ctx, pipeline.StageControlObligations,
		stateControlObligations(worktree))
	ledger.decide(ctx, pipeline.StageControlTests, checkControlTests(worktree))
	ledger.decide(ctx, pipeline.StageGlobalInvariants,
		checkGlobalInvariants(worktree, execution.settings.ForbiddenCapabilities))
	ledger.decide(ctx, pipeline.StagePlatformMatrix,
		checkPlatformMatrix(ctx, worktree, execution.platformTargets()))

	// Each unit is proven on its own terms, whether or not the suite as a
	// whole passes. These used to be a single verdict over "go test ./...",
	// which meant one broken test anywhere blocked every one of them and none
	// of them ever said which unit was at fault. Nine working atoms and one
	// broken one is a materially different situation from ten broken ones, and
	// the old shape could not tell them apart.
	ledger.decide(ctx, pipeline.StageAtomVerification,
		checkAtomVerification(ctx, worktree))
	ledger.decide(ctx, pipeline.StageMoleculeVerification,
		checkMoleculeVerification(ctx, worktree))
	ledger.decide(ctx, pipeline.StagePathCoverage,
		checkFunctionCoverage(ctx, worktree))

	if !verified {
		// What is left genuinely does need a passing suite: a mutation score
		// against a failing suite measures nothing, a timing against one
		// measures how long failure takes, and a repetition of it repeats a
		// failure.
		for _, stage := range []pipeline.Number{
			pipeline.StageAtomFuzz, pipeline.StageAtomMutation,
			pipeline.StageRepetition, pipeline.StageNonFunctional,
		} {
			ledger.blocked(ctx, stage,
				"the suite does not pass, so nothing measured from running it "+
					"would mean anything")
		}
		return
	}

	ledger.decide(ctx, pipeline.StageAtomFuzz, checkFuzzing(ctx, worktree))
	ledger.decide(ctx, pipeline.StageRepetition, checkRepetition(ctx, worktree))
	ledger.decide(ctx, pipeline.StageNonFunctional,
		checkNonFunctional(ctx, worktree))
	// Mutation runs last of these: it is the most expensive, and it is the one
	// whose answer decides how much the others were worth.
	ledger.decide(ctx, pipeline.StageAtomMutation,
		execution.checkMutations(ctx, worktree))
}
