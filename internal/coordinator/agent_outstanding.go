package coordinator

import (
	"context"
	"fmt"
	"strings"
)

// outstanding is everything a run still owes, gathered before it is asked for.
//
// The gates used to be asked one at a time, each returning on the first thing
// it found. That is what made them fight: a run told only to add the missing
// cases rewrote its test file and dropped a doc comment, was then told only
// about the doc comment, and lost the cases again. Rung 5 spent six attempts
// oscillating between those two, fixing each in turn and regressing the other,
// and because the two failures alternated neither ever looked like a stall.
//
// Naming everything at once costs nothing — the checks all run anyway — and it
// is the only version where a run can be right about the whole of its work at
// the same time.
type outstanding struct {
	gate        string
	because     string
	summary     string
	instruction string
	// askedForCases records whether this instruction included synthesised
	// cases, so the caller can count the round it has spent.
	askedForCases bool
	// owedCases is how many synthesised cases were still untried when this was
	// computed, so the next round can tell whether the run is closing the gap
	// or standing still. See the case ladder in outstandingWork.
	owedCases int
}

// any reports whether the run owes anything.
func (work outstanding) any() bool { return work.instruction != "" }

// outstandingWork collects every gap that can be described to the next attempt.
//
// Acceptance comes first and dominates the naming, because a program that does
// not do what was asked is wrong in a way that no amount of documentation and
// no number of test cases can compensate for — but the rest is still included,
// since a run rewriting the program anyway may as well carry the other work
// with it rather than regress it.
func (execution *AgentExecution) outstandingWork(
	ctx context.Context,
	scope agentScope,
	caseRounds int,
	// testsPassed is whether the run's own suite is actually known to have
	// passed. Three instruction builders used to assert that it had, whether or
	// not anything established it — see validationPreamble.
	testsPassed bool,
) outstanding {
	var work outstanding
	var parts []string
	var summaries []string

	examples := parseAcceptanceExamples(scope.requirement)
	if _, failures := execution.checkAcceptance(
		ctx, scope.worktree, scope.requirement, examples,
	); len(failures) > 0 {
		work.gate = "acceptance"
		work.because = "it did not do what was asked"
		parts = append(parts, acceptanceInstruction(examples, failures))
		summaries = append(summaries,
			"it does not do what was asked ("+failures[0]+")")
	}

	attribution := execution.resolveAttribution(ctx, scope)
	if gaps, err := findCompletenessGaps(
		scope.worktree, execution.settings, attribution,
	); err == nil && !gaps.Empty() {
		if work.gate == "" {
			work.gate = "completeness"
			work.because = "the work was not finished"
		}
		parts = append(parts, gaps.Instruction(testsPassed))
		summaries = append(summaries, fmt.Sprintf(
			"%d function(s) have no test, %d have no doc comment, %d are doing "+
				"too much",
			len(gaps.UntestedAtoms), len(gaps.UndocumentedAtoms),
			len(gaps.TangledFunctions)))
	}

	// The case ladder is bounded by the run's own attempt budget, not by a
	// round count of its own.
	//
	// It used to stop after two rounds, on the reasoning that every input is
	// derived from a signature so the ask could never end. That was right
	// about the risk and wrong about the remedy: the run then declared itself
	// complete with the case stage still failing, which is the ledger and the
	// run disagreeing about the same fact. Observed on ladder rung 2, where
	// three cases stayed untried and the run finished anyway.
	//
	// Bounding it by whether the debt is still falling was the next wrong
	// answer, and it was wrong in an instructive way: a run that plateaus is
	// exactly the run the model ladder exists for, and refusing to ask again
	// meant the escalation that would have brought a stronger model was never
	// reached. The ask now continues while the run has attempts left, and the
	// convergence tracker decides whether to escalate, decompose, or stop —
	// one mechanism for "this is not getting anywhere" instead of two that
	// disagree.
	//
	// The backstop is a cap on rounds rather than on progress, so a run that
	// oscillates cannot spend its whole ceiling here alone.
	const caseRoundBackstop = 8
	if owed, err := untriedCases(scope.worktree); err == nil && len(owed) > 0 {
		owedNow := 0
		for _, cases := range owed {
			owedNow += len(cases)
		}
		if caseRounds < caseRoundBackstop {
			if work.gate == "" {
				work.gate = "atom-case-synthesis"
				work.because = "inputs it was meant to try were never tried"
			}
			parts = append(parts, untriedCaseInstruction(owed, testsPassed))
			summaries = append(summaries, fmt.Sprintf(
				"%d function(s) have inputs nothing tries", len(owed)))
			work.askedForCases = true
		}
		work.owedCases = owedNow
	}

	if len(parts) == 0 {
		return outstanding{}
	}
	// Numbered, because a run handed three unlabelled blocks of prose treats
	// them as one thing to have an opinion about rather than three things to
	// do, and finishes the first.
	var instruction strings.Builder
	instruction.WriteString(fmt.Sprintf(
		"There %s %s still outstanding. Do all of it in this attempt, and do "+
			"not undo work already done to satisfy the others — every one of "+
			"these is checked again, together, on the next attempt.\n\n",
		wasOrWere(len(parts)), counted(len(parts), "thing")))
	for index, part := range parts {
		fmt.Fprintf(&instruction, "%d. %s\n\n", index+1, part)
	}
	work.instruction = strings.TrimRight(instruction.String(), "\n")
	work.summary = strings.Join(summaries, "; ") + "."
	return work
}

// wasOrWere keeps the sentence grammatical, because a message that reads as
// broken makes a reader doubt the numbers in it.
func wasOrWere(count int) string {
	if count == 1 {
		return "is"
	}
	return "are"
}
