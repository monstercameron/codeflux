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
	// askedForDocumentation records whether this instruction asked for the
	// atom schema, which is asked at most once per run.
	askedForDocumentation bool
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
	// documentationRounds is how many times this run has already been asked
	// for the atom schema. It is asked once; see the block that uses it.
	documentationRounds int,
	// testsPassed is whether the run's own suite is actually known to have
	// passed. Three instruction builders used to assert that it had, whether or
	// not anything established it — see validationPreamble.
	testsPassed bool,
) outstanding {
	var work outstanding
	var parts []string
	var summaries []string
	// satisfied is what this attempt has already established, carried into the
	// instruction so the next one is told what its edit must not cost. Reaching
	// here at all means the package built: outstandingWork is only asked after
	// the assembly gate held.
	satisfied := []string{"The package compiles."}
	if testsPassed {
		satisfied = append(satisfied, "The project's own test suite passes.")
	}

	// Acceptance is not asked for here. It is checked and sent back on its own
	// before this is reached, because a program that does not do what was asked
	// is wrong in a way no amount of coverage compensates for, and listing it
	// beside three other asks invites a run to treat it as one item of four.
	// It appears below only as an invariant the next attempt must not break.
	if len(parseAcceptanceExamples(scope.requirement)) > 0 {
		satisfied = append(satisfied,
			"The acceptance examples match exactly, byte for byte.")
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
	} else if err == nil {
		satisfied = append(satisfied,
			"Every function has a test that names it and a doc comment.")
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
	if owed, err := untriedCases(scope.worktree); err == nil && len(owed) == 0 {
		satisfied = append(satisfied,
			"Every input case derived from a signature is tried by a test.")
	} else if err == nil && len(owed) > 0 {
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

	// Atoms nobody can register are atoms nobody can reuse.
	//
	// Registration refuses a declaration with no //codeflux:atom schema
	// comment, and nothing asked for one, so the registry stayed empty and the
	// recall stage searched an empty project on every run of this session.
	// That is the compounding-effort thesis unstarted rather than unproven: a
	// run cannot reuse what no run ever registered.
	//
	// Asked last, and only for leaf functions, because it is the most
	// expensive instruction in the set — nineteen fields per atom — and a run
	// still failing to compile has more urgent problems than being findable.
	// Only once everything else holds, and only once.
	//
	// This used to be asked alongside the delivery gates, and the two fought:
	// ladder rung 2 alternated atom-documentation, completeness, completeness,
	// atom-documentation on the lowest rung, fixing each in turn and regressing
	// the other. Neither is a reason a program cannot be reviewed — a correct,
	// tested, accepted program with no registry row is deliverable; an
	// unreviewable one with a perfect registry row is not — so enrichment
	// belongs after readiness rather than in competition with it.
	//
	// Asked once because it is a write of a comment, not a repair of a defect.
	// A run that was told exactly which atoms and exactly which nineteen fields
	// and did not do it will not do it on the fourth telling either, and the
	// run has somewhere better to spend the attempt.
	if len(parts) == 0 && documentationRounds == 0 {
		if produced, err := readProducedFunctions(scope.worktree); err == nil {
			if undocumented := atomsWithoutRegistrableDocumentation(
				scope.worktree, produced,
			); len(undocumented) > 0 {
				work.gate = "atom-documentation"
				work.because = "its atoms cannot be found by a later task"
				work.askedForDocumentation = true
				parts = append(parts, atomDocumentationInstruction(undocumented))
				summaries = append(summaries, fmt.Sprintf(
					"%d atom(s) carry no registrable documentation",
					len(undocumented)))
			}
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
	instruction.WriteString(invariantBlock(satisfied))
	work.instruction = strings.TrimRight(instruction.String(), "\n")
	work.summary = strings.Join(summaries, "; ") + "."
	return work
}

// invariantBlock names what the next attempt must not break.
//
// A run told to add three test cases rewrites the test file, and nothing in
// what it was told says the acceptance output still has to match afterwards.
// The gates all run again, so the regression is caught -- and caught costs an
// attempt, which a sentence would have saved. Rung 5 spent six attempts fixing
// two things in turn and regressing the other each time.
//
// Only obligations already established are listed. Naming one the run has not
// met yet would read as a fourth thing to do, hidden in the section that says
// it is not asking for anything.
func invariantBlock(satisfied []string) string {
	if len(satisfied) == 0 {
		return ""
	}
	var block strings.Builder
	block.WriteString(
		"These are already true and must stay true. They are not work; they " +
			"are what the work above must not cost:\n")
	for _, held := range satisfied {
		fmt.Fprintf(&block, "  - %s\n", held)
	}
	return block.String()
}

// wasOrWere keeps the sentence grammatical, because a message that reads as
// broken makes a reader doubt the numbers in it.
func wasOrWere(count int) string {
	if count == 1 {
		return "is"
	}
	return "are"
}
