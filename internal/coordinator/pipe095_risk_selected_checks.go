package coordinator

import (
	"context"

	"codeflux.dev/codeflux/internal/domain"
)

// adversarialCheckPlan says which of reviewAdversariallyWithPlan's finders
// are worth their cost for one task (PIPE-095; plan.md §21: "Checks are
// selected from: task risk; changed obligation categories; effect types;
// dependency changes; security classification").
//
// MutationAnalysis gates findUnkilledMutants specifically, because it is the
// one finder in the review whose cost is not negligible: checkMutations runs
// up to twelve whole-suite executions per call (its own documented bound),
// where every other finder here is a single syntax-tree read. That is
// exactly the "expected defect cost justifies its expense" clause plan.md
// §21 states, applied to the one check in this review where expense is a
// real number rather than a rounding error.
//
// Only risk selects it today. The plan also names changed obligation
// categories, effect types, dependency changes, and security classification
// as selectors; none of those are added here because nothing upstream of
// this package currently computes or passes them to a review call, and
// inventing that classification from inside this file would be guessing at
// data this ticket does not ask this lane to produce. Risk is different:
// scope.riskLevel is already resolved by every run (task.RiskLevel, read in
// agent_execution.go) and simply never reaches this review, which is the gap
// this ticket names directly ("read the scope.riskLevel the run already
// resolves and never uses").
type adversarialCheckPlan struct {
	MutationAnalysis bool
}

// selectAdversarialChecks reads a task's risk level (PIPE-095) to decide
// whether reviewAdversariallyWithPlan's one costly finder is worth running
// for this task, rather than reviewAdversarially running every finder
// unconditionally regardless of what the task is.
//
// A routine task skips mutation analysis: the twelve-mutant, whole-suite
// cost is not justified for the ordinary case, and the review still runs
// its four free finders. An elevated or protected task, or a risk level
// this function does not recognise -- including the zero value, which is
// what an unset domain.RiskLevel is -- gets the full, pre-PIPE-095 policy.
// Defaulting the unrecognised case to "run everything" rather than "skip
// the costly one" is deliberate: a caller that has not stated a risk should
// not silently lose coverage because this function guessed low.
func selectAdversarialChecks(risk domain.RiskLevel) adversarialCheckPlan {
	if risk == domain.RiskLevelRoutine {
		return adversarialCheckPlan{MutationAnalysis: false}
	}
	return adversarialCheckPlan{MutationAnalysis: true}
}

// reviewAdversariallyForRisk is reviewAdversarially with its checks selected
// by task risk (PIPE-095), reading the risk level a caller has already
// resolved instead of running a fixed checklist against every task alike.
//
// This is not yet reached from the pipeline. agent_execution.go's call site
// currently reads `execution.reviewAdversarially(ctx, scope.worktree)`; using
// this instead would mean changing that line to
// `execution.reviewAdversariallyForRisk(ctx, scope.worktree, scope.riskLevel)`,
// and agent_execution.go is owned by another lane this session, so that
// change is not made here. reviewAdversarially's own behaviour is unchanged
// by this function's existence: it keeps calling reviewAdversariallyWithPlan
// with MutationAnalysis unconditionally true, which is the same coverage it
// had before this ticket. Wiring this in later is therefore additive, not a
// behaviour change to any caller that has not been updated to use it.
func (execution *AgentExecution) reviewAdversariallyForRisk(
	ctx context.Context,
	worktree string,
	risk domain.RiskLevel,
) ([]adversarialFinding, error) {
	return execution.reviewAdversariallyWithPlan(
		ctx, worktree, selectAdversarialChecks(risk))
}
