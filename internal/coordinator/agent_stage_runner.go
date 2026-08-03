package coordinator

import (
	"context"
	"fmt"

	"codeflux.dev/codeflux/internal/pipeline"
)

// examineStructureUnconditionalStages is decided regardless of whether the
// module's suite passes, restricted against pipeline.Requirements to compute
// the concurrent schedule examineStructure actually runs (PIPE-058a).
//
// Declared at package level, rather than as a literal inside
// examineStructure, so a test can schedule against the exact list production
// uses instead of a hand-copied duplicate that could silently drift from it
// (TestPIPE017_OptimizationIsDecidedAfterMutation does exactly this for
// examineStructureVerifiedGatedStages below).
var examineStructureUnconditionalStages = []pipeline.Number{
	pipeline.StageContracts, pipeline.StageAtomCaseSynthesis,
	pipeline.StageAtomExampleTests, pipeline.StageAtomPropertyTests,
	pipeline.StageAtoms, pipeline.StageAtomVerification,
	pipeline.StageAntiPatterns, pipeline.StageAtomComplexity,
	pipeline.StageAtomDocumentation, pipeline.StageCompositionObligations,
	pipeline.StageMoleculeTests, pipeline.StageMolecules,
	pipeline.StageMoleculeVerification, pipeline.StageControlObligations,
	pipeline.StageControlTests, pipeline.StageControlFlow,
	pipeline.StagePathCoverage, pipeline.StageGlobalInvariants,
	pipeline.StagePlatformMatrix,
}

// examineStructureVerifiedGatedStages is decided only once the module's
// suite is known to pass, restricted against pipeline.Requirements the same
// way examineStructureUnconditionalStages is (PIPE-058a).
//
// atom-mutation is the flow's one exclusive-mutating stage (PIPE-059), so
// the scheduler always gives it a wave of its own; atom-optimization is the
// one stage in this set whose real Requirements edge (atom-mutation)
// survives RestrictToStages, so it never becomes ready until mutation's
// wave finishes, which is the ordering
// TestAnAtomIsOptimisedOnlyOnceItsTestsCanCatchAMistake and PIPE-017 require
// (see TestPIPE017_OptimizationIsDecidedAfterMutation).
var examineStructureVerifiedGatedStages = []pipeline.Number{
	pipeline.StageAtomFuzz, pipeline.StageAtomMutation,
	pipeline.StageRepetition, pipeline.StageNonFunctional,
	pipeline.StageAtomOptimization,
}

// examineStructure performs every stage that can be decided from what the run
// actually produced, under the flow's own default profile.
//
// No production caller declares anything but pipeline.ProfileDefault today:
// task intake carries no field a profile could be read from, and adding one
// is a separate concern from consulting the profile the flow already
// declares (PIPE-046a; see docs/plan.md's "Integration status" note under
// "Declared Run Profiles"). This wrapper is what keeps that gap named rather
// than silently closed by a caller-supplied default: examineStructureWithProfile
// is the one real implementation, and this calls it with ProfileDefault, which
// ValidateProfiles requires to decline nothing — so this method's behavior is
// unchanged from before PIPE-046a landed.
func (execution *AgentExecution) examineStructure(
	ctx context.Context,
	ledger *pipelineLedger,
	scope agentScope,
	compiles bool,
	verified bool,
) {
	execution.examineStructureWithProfile(ctx, ledger, scope, compiles, verified, pipeline.ProfileDefault)
}

// examineStructureWithProfile is examineStructure, extended with the run's
// declared profile (PIPE-046a): a stage the profile declines is recorded
// skipped, with the profile's own reason, before the wave that would
// otherwise have scheduled it, and is never handed to
// pipeline.RestrictToStages/RunConcurrently at all -- the same "never
// invoked" guarantee PIPE-046's TestPIPE046_AProfileSelectsADifferentStageSetFromTheSchedulerItself
// proved against the scheduler directly, now proved through this production
// call site.
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
func (execution *AgentExecution) examineStructureWithProfile(
	ctx context.Context,
	ledger *pipelineLedger,
	scope agentScope,
	compiles bool,
	verified bool,
	profile pipeline.RunProfile,
) {
	worktree := scope.worktree
	if !compiles {
		// Every stage that examines what was produced is blocked, derived from
		// the flow rather than hand-listed. The list used to be written out
		// here and fell nine short: those nine reached the closing sweep and
		// were recorded as not-implemented, which had been true when nothing
		// performed them and became a lie the moment they were built. A ledger
		// that misreports its own coverage is worse than no ledger.
		// The set is named in internal/pipeline rather than expressed as a
		// numeric range (PIPE-005). A range is correct only while the flow's
		// numbering happens to hold: inserting a stage inside it silently
		// enrols the new stage, and moving one out silently drops it.
		for _, stage := range pipeline.ExaminesProducedSource {
			ledger.blocked(ctx, stage,
				"the module does not build, so nothing about it can be examined")
		}
		return
	}

	// Derived once, from the base revision the worktree was created at
	// (PIPE-111/PIPE-111a), and threaded into every stage below that has been
	// re-scoped to answer only for what this run actually changed rather than
	// for everything a real repository's pre-existing code happens to expose.
	// Every re-scoped stage below takes the raw changed-line attribution and
	// derives its own declaration-level scope internally where it needs one,
	// so each stage enumerates files from attribution's own set rather than
	// from producedGoFiles' git-status view, which goes blind the moment a
	// run commits to its own worktree. Attribution fails toward including
	// everything when it could not be established, so a run whose base
	// revision is unknown is held to the old, whole-worktree standard rather
	// than a silently narrowed one.
	attribution := execution.resolveAttribution(ctx, scope)

	// Shared across every check below that parses what producedGoFiles' own
	// git-status view names, so checkAtoms, checkAtomTests, checkMolecules,
	// and checkAtomDocumentation shell out to `git status` and parse the
	// produced source once between them rather than once each (PIPE-057).
	// Safe because nothing in this pass writes to the worktree — see
	// producedFunctionCache's own doc for the lifetime rule that makes it
	// unsafe anywhere else, and for how it stays safe now that several of
	// its callers can run concurrently (PIPE-058a).
	cache := newProducedFunctionCache(worktree)

	// Obligations are raised durably so a later stage can discharge them by
	// name, rather than being stated in prose and reported satisfied for
	// having been stated (PIPE-016).
	obligations := obligationScope{
		Store:   execution.repositories,
		TaskID:  scope.taskID,
		Attempt: ledger.currentAttempt(),
	}

	// Structure, from the source itself. These hold whether or not the tests
	// pass, because they are statements about what was written rather than
	// about what it does.
	//
	// They used to be a straight line of ledger.decide calls in flow order.
	// PIPE-058a runs them instead through pipeline.RunConcurrently, restricted
	// to exactly this set (PIPE-058's Requirements table, minus every edge
	// that points at a stage outside it — StageAtomDocumentation's real
	// entry, for one, also names atom-fuzz and atom-mutation, which belong to
	// the suite-gated set below and are treated as already resolved outside
	// this call). None of these checks reads another one's ledger verdict —
	// each is a pure function of the worktree, the shared cache, attribution,
	// and (for the two obligation-discharging stages) the obligation store —
	// so restricting the real graph to this set and running its waves
	// concurrently reports the same verdicts a serial pass would, in less
	// wall-clock time (PIPE-064 proves the scheduler underneath this has that
	// property; TestPIPE057_ExamineStructureSharesOneCacheAcrossItsChecks and
	// the mutation-guarded cache above are what keep the shared state itself
	// safe to reach from more than one goroutine).
	unconditionalRun := func(
		runCtx context.Context, stage pipeline.Number,
	) (pipeline.State, string, map[string]any) {
		switch stage {
		case pipeline.StageAtoms:
			return outcomeParts(checkAtoms(worktree, cache))
		case pipeline.StageAtomCaseSynthesis:
			return outcomeParts(checkCaseCoverage(worktree))
		case pipeline.StageAtomExampleTests:
			return outcomeParts(checkAtomTests(worktree, cache))
		case pipeline.StageMolecules:
			return outcomeParts(checkMolecules(worktree, cache))
		case pipeline.StageAntiPatterns:
			return outcomeParts(checkAntiPatterns(worktree, attribution))
		case pipeline.StageAtomComplexity:
			return outcomeParts(checkComplexity(worktree, attribution))
		case pipeline.StageContracts:
			return outcomeParts(describeContracts(worktree))
		case pipeline.StageAtomDocumentation:
			return outcomeParts(checkAtomDocumentation(worktree, cache))
		case pipeline.StageAtomPropertyTests:
			return outcomeParts(checkPropertyTests(worktree))
		case pipeline.StageCompositionObligations:
			return outcomeParts(composeCompositionObligations(runCtx, worktree, obligations))
		case pipeline.StageMoleculeTests:
			return outcomeParts(checkMoleculeTests(worktree))
		case pipeline.StageControlObligations:
			return outcomeParts(composeControlObligations(runCtx, worktree, obligations))
		case pipeline.StageControlTests:
			return outcomeParts(checkControlTests(worktree))
		case pipeline.StageControlFlow:
			return outcomeParts(dischargeControlFlow(runCtx, worktree, obligations))
		case pipeline.StageGlobalInvariants:
			return outcomeParts(checkGlobalInvariants(
				worktree, execution.settings.ForbiddenCapabilities))
		case pipeline.StagePlatformMatrix:
			return outcomeParts(checkPlatformMatrix(
				runCtx, worktree, execution.platformTargets()))
		case pipeline.StageAtomVerification:
			return outcomeParts(checkAtomVerification(runCtx, worktree))
		case pipeline.StageMoleculeVerification:
			return outcomeParts(dischargeMoleculeVerification(runCtx, worktree, obligations))
		case pipeline.StagePathCoverage:
			return outcomeParts(checkFunctionCoverage(runCtx, worktree, attribution))
		default:
			return pipeline.StateFailed, fmt.Sprintf(
				"stage %d has no check wired into examineStructure's "+
					"unconditional wave", stage), nil
		}
	}
	unconditionalApplicable, unconditionalDeclines :=
		declineProfiledStages(examineStructureUnconditionalStages, profile)
	recordProfileDeclines(ctx, ledger, profile, unconditionalDeclines)
	for _, decision := range pipeline.RunConcurrently(
		ctx, pipeline.RestrictToStages(pipeline.Requirements, unconditionalApplicable),
		pipeline.ResourceClassMap(), unconditionalRun, pipeline.ScheduleOptions{},
	) {
		ledger.recordDecision(ctx, decision)
	}

	// The profile's decision is made before the run starts, so it takes
	// priority over "blocked because the suite failed": a stage the profile
	// already declined was never going to run regardless of what the suite
	// did, and recording it blocked would misattribute the reason.
	verifiedApplicable, verifiedDeclines :=
		declineProfiledStages(examineStructureVerifiedGatedStages, profile)
	recordProfileDeclines(ctx, ledger, profile, verifiedDeclines)

	if !verified {
		// What is left genuinely does need a passing suite: a mutation score
		// against a failing suite measures nothing, a timing against one
		// measures how long failure takes, and a repetition of it repeats a
		// failure.
		// atom-optimization joins these because it now runs after mutation
		// (PIPE-017): deciding what is worth rewriting without a mutation
		// score is deciding it under tests nobody has shown can detect a
		// fault, which is the thing the ordering exists to prevent.
		for _, stage := range verifiedApplicable {
			ledger.blocked(ctx, stage,
				"the suite does not pass, so nothing measured from running it "+
					"would mean anything")
		}
		return
	}

	// These five run through the same scheduler, restricted to this smaller
	// set. atom-mutation is the flow's one exclusive-mutating stage
	// (PIPE-059): it writes and restores a produced source file in place, so
	// RunConcurrently gives it a wave of its own before anything else in this
	// set is allowed to start, and atom-optimization — the ordering
	// TestAnAtomIsOptimisedOnlyOnceItsTestsCanCatchAMistake exists to enforce
	// (PIPE-017) — only becomes ready once that wave finishes, because it is
	// the one stage here whose real Requirements edge (atom-mutation) survives
	// RestrictToStages. Rewriting code is only as safe as the tests guarding
	// it, so deciding what is worth simplifying before the mutation score is
	// known would be deciding it under tests nobody has shown can detect a
	// fault. It is inert today because no rewrite is performed (PIPE-010),
	// which is exactly why the ordering is worth keeping correct now: the day
	// a rewrite lands, the sequence is already right rather than being
	// discovered to be wrong.
	verifiedGatedRun := func(
		runCtx context.Context, stage pipeline.Number,
	) (pipeline.State, string, map[string]any) {
		switch stage {
		case pipeline.StageAtomFuzz:
			return outcomeParts(checkFuzzing(runCtx, worktree))
		case pipeline.StageRepetition:
			return outcomeParts(checkRepetition(runCtx, worktree))
		case pipeline.StageNonFunctional:
			return outcomeParts(checkNonFunctional(runCtx, worktree, nonFunctionalScope{
				Baselines:          execution.repositories,
				ProjectID:          scope.projectID,
				RepositoryID:       scope.repositoryID,
				RepositoryRevision: scope.revision,
			}))
		case pipeline.StageAtomMutation:
			return outcomeParts(execution.checkMutations(runCtx, worktree, attribution))
		case pipeline.StageAtomOptimization:
			return outcomeParts(checkSimplification(worktree, attribution))
		default:
			return pipeline.StateFailed, fmt.Sprintf(
				"stage %d has no check wired into examineStructure's "+
					"suite-verified wave", stage), nil
		}
	}
	for _, decision := range pipeline.RunConcurrently(
		ctx, pipeline.RestrictToStages(pipeline.Requirements, verifiedApplicable),
		pipeline.ResourceClassMap(), verifiedGatedRun, pipeline.ScheduleOptions{},
	) {
		ledger.recordDecision(ctx, decision)
	}
}

// declineProfiledStages splits a candidate stage list into what the profile
// still asks examineStructureWithProfile to decide and what it has already
// decided, preserving the caller's order in both (PIPE-046a).
//
// This is deliberately checked before pipeline.RestrictToStages ever sees the
// list: a declined stage must never reach the scheduler at all, matching the
// guarantee TestPIPE046_AProfileSelectsADifferentStageSetFromTheSchedulerItself
// proved for RunConcurrently directly -- a stage the profile declines gets no
// Runner call, not a Runner call that happens to return skipped.
func declineProfiledStages(
	stages []pipeline.Number, profile pipeline.RunProfile,
) (applicable []pipeline.Number, declined []pipeline.ProfileDecline) {
	for _, stage := range stages {
		if reason, ok := profile.DeclinedReason(stage); ok {
			declined = append(declined, pipeline.ProfileDecline{Stage: stage, Reason: reason})
			continue
		}
		applicable = append(applicable, stage)
	}
	return applicable, declined
}

// recordProfileDeclines writes one skipped ledger row per stage the profile
// declined, before the wave that would otherwise have scheduled it
// (docs/plan.md's own integration note for PIPE-046a). The detail names both
// the profile and its stated reason, so a reader of the ledger sees a
// decision made before the run started rather than an unexplained skip.
func recordProfileDeclines(
	ctx context.Context,
	ledger *pipelineLedger,
	profile pipeline.RunProfile,
	declines []pipeline.ProfileDecline,
) {
	for _, decline := range declines {
		ledger.record(ctx, decline.Stage, pipeline.StateSkipped,
			fmt.Sprintf("declined in advance by run profile %q: %s", profile.Name, decline.Reason),
			nil)
	}
}

// outcomeParts renders a stageOutcome in pipeline.RunConcurrently's Runner
// vocabulary, following exactly the state mapping pipelineLedger.decide
// already uses — skipped stays skipped, held becomes satisfied, anything
// else becomes failed — so routing a check through the concurrent scheduler
// instead of straight into ledger.decide records the same state it always
// did.
func outcomeParts(outcome stageOutcome) (pipeline.State, string, map[string]any) {
	switch {
	case outcome.Skipped:
		return pipeline.StateSkipped, outcome.Detail, outcome.Evidence
	case outcome.Held:
		return pipeline.StateSatisfied, outcome.Detail, outcome.Evidence
	default:
		return pipeline.StateFailed, outcome.Detail, outcome.Evidence
	}
}
