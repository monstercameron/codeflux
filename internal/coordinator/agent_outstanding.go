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
		parts = append(parts, gaps.Instruction())
		summaries = append(summaries, fmt.Sprintf(
			"%d function(s) have no test, %d have no doc comment, %d are doing "+
				"too much",
			len(gaps.UntestedAtoms), len(gaps.UndocumentedAtoms),
			len(gaps.TangledFunctions)))
	}

	// The case ladder is bounded because it is derived rather than demanded:
	// every input comes from a signature, so a run can always be asked for one
	// more and the ask would never end. Two rounds is enough to close most of
	// it and few enough that it cannot become a standard nothing meets.
	const caseRoundLimit = 2
	if caseRounds < caseRoundLimit {
		if owed, err := untriedCases(scope.worktree); err == nil && len(owed) > 0 {
			if work.gate == "" {
				work.gate = "atom-case-synthesis"
				work.because = "inputs it was meant to try were never tried"
			}
			parts = append(parts, untriedCaseInstruction(owed))
			summaries = append(summaries, fmt.Sprintf(
				"%d function(s) have inputs nothing tries", len(owed)))
			work.askedForCases = true
		}
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
