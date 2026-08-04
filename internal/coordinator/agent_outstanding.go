package coordinator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	// askedForCoverage records whether this instruction named uncovered lines,
	// so the caller can count the round it has spent.
	askedForCoverage bool
	// askedForAdversarial records whether this instruction named hostile-input
	// failures, so the caller can count the round it has spent.
	askedForAdversarial bool
	// askedForMutation records whether this instruction named defects the
	// tests did not notice, so the caller can count the round it has spent.
	askedForMutation bool
	// askedForFuzz records whether this instruction named a decoding boundary
	// with no fuzz target, so the caller can count the round it has spent.
	askedForFuzz bool
	// askedForProperty records whether this instruction asked for a property
	// test, which is asked at most once per run.
	askedForProperty bool
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
// propertyRoundBackstop bounds how many times one run is asked for a property
// test.
//
// More than once, because atom-property-tests is a hard gate and a demand
// raised early gets buried. Rung 18 on 2026-08-04 was asked at attempt four,
// spent five more attempts on other gates, was never reminded, and failed the
// run on this two hundred seconds later — having been told in that same message
// that "every one of these is checked again, together, on the next attempt",
// which was true of the checking and false of the asking.
//
// Not unboundedly: a run told three times what a property test is, and given
// the shape wanted in plain Go, will not write one on the fourth telling.
const propertyRoundBackstop = 3

func (execution *AgentExecution) outstandingWork(
	ctx context.Context,
	scope agentScope,
	caseRounds int,
	// documentationRounds is how many times this run has already been asked
	// for the atom schema. It is asked once; see the block that uses it.
	documentationRounds int,
	// coverageRounds is how many times this run has already been shown its
	// uncovered lines.
	coverageRounds int,
	// propertyRounds is how many times this run has already been asked for a
	// property test.
	propertyRounds int,
	// adversarialRounds is how many times this run has already been shown the
	// ways it mishandles input nobody intended.
	adversarialRounds int,
	// mutationRounds is how many times this run has already been shown the
	// deliberate defects its tests did not notice.
	mutationRounds int,
	// fuzzRounds is how many times this run has already been asked for a fuzz
	// target over a decoding boundary.
	fuzzRounds int,
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
	// A property over a set of inputs, while there are still attempts to add
	// one.
	//
	// This was measured only after the loop. Ladder rung 6 ended on
	// "atom-property-tests failed" having never once been asked for a property
	// test: the gate was hard, the run had attempts left, and nothing told it.
	// That is the same late-gate defect path coverage had, in a second place.
	//
	// Bounded like the coverage ladder rather than asked exactly once.
	//
	// Once was the rule, on the reasoning that a run told this precisely and
	// not doing it will not do it on the third telling. That holds when the
	// tellings are consecutive. It does not hold when the demand is raised and
	// then buried: rung 18 on 2026-08-04 was asked at attempt four, spent
	// attempts five through nine on integration-tests, path-coverage and
	// atom-documentation, was never reminded, and failed the run on this gate
	// two hundred seconds later. The instruction it had been given says "every
	// one of these is checked again, together, on the next attempt" — which is
	// true of the checking and was not true of the asking.
	//
	// Still bounded, and still only when nothing else is outstanding, so it
	// cannot crowd out work that has to happen first.
	// Asked alongside whatever else is outstanding, for the same reason the
	// hostile-input probe is.
	//
	// It waited for nothing else to be outstanding, which is right for a
	// refinement and wrong for a hard gate: a run that never states a property
	// fails the rung whatever else is true of it. Deferring it meant it could
	// go unasked entirely. Rung 19 on 2026-08-04 spent six sendbacks on other
	// gates, was never once told about this, and then failed on it with a
	// program that built, ran, printed exactly what was asked and survived
	// every hostile input.
	//
	// Still bounded, and the instruction says whether the tests currently pass,
	// because being asked for a property while the build is broken should read
	// as the second job it is.
	if propertyRounds < propertyRoundBackstop {
		if outcome := checkPropertyTests(scope.worktree); !outcome.Held &&
			!outcome.Skipped {
			if work.gate == "" {
				work.gate = "atom-property-tests"
				work.because = "nothing states a property over a set of inputs"
			}
			work.askedForProperty = true
			parts = append(parts,
				propertyTestInstruction(outcome.Detail, testsPassed))
			summaries = append(summaries,
				"no test states a property over a set of inputs")
		} else if outcome.Held {
			satisfied = append(satisfied,
				"At least one test examines a set of inputs rather than one "+
					"example.")
		}
	}

	// Uncovered changed lines, while there are still attempts to fix them.
	//
	// This was measured only after the loop, in examineStructure, so a run
	// converged, finished, and was then told that five of its thirty-nine
	// changed lines are executed by nothing. Rung 2 did exactly that. A
	// repairable failure discovered during finalisation is the one shape of
	// failure the refinement loop exists to prevent, and it had no way of
	// knowing: nothing asked.
	//
	// Bounded like the case ladder, and for the same reason. Coverage of
	// generated code has a floor below which the remaining lines are error
	// branches nothing can reach without contorting the program, and a run that
	// has spent four rounds on it is not going to find the fifth.
	const coverageRoundBackstop = 3
	if len(parts) == 0 && coverageRounds < coverageRoundBackstop {
		if uncovered := uncoveredChangedLines(
			ctx, scope.worktree, attribution,
		); len(uncovered) > 0 {
			work.gate = "path-coverage"
			work.because = "some of what it wrote is executed by nothing"
			work.askedForCoverage = true
			parts = append(parts, uncoveredLineInstruction(uncovered))
			summaries = append(summaries, fmt.Sprintf(
				"%d changed line(s) no test executes", len(uncovered)))
		} else {
			satisfied = append(satisfied,
				"Every line this run wrote is executed by a test.")
		}
	}

	// Hostile input, while there are still attempts to answer it.
	//
	// The probe runs every produced command against input nobody intended, and
	// it is a hard gate: a program that accepts nothing at all and exits zero
	// fails the rung. It was measured only after the loop, in
	// examineStructure, so a run converged, finished, and was then failed for
	// something it was never asked about.
	//
	// Ladder rung 4 on 2026-08-03 lost every pass to this. The stage failed at
	// 254 seconds with "given nothing at all: accepted it, printed nothing, and
	// exited zero"; the last send-back had happened at 250 seconds, for
	// something else. Six attempts, none of them told.
	//
	// Bounded like the others. The probe costs a build and a run of each
	// produced command, so a run that has been shown its hostile-input
	// failures twice and not fixed them will not be helped by a third telling.
	const adversarialRoundBackstop = 2
	// Asked alongside whatever else is outstanding, not after it.
	//
	// The neighbouring checks defer themselves behind len(parts) == 0 because
	// they are refinements: a run with a broken build does not need to hear
	// about an untried empty slice yet. This one is not a refinement. It is a
	// hard gate, and a program that accepts input nobody intended and exits
	// zero fails the rung whatever else is true of it.
	//
	// Deferring it meant it was never reached: rung 4 on 2026-08-03 always had
	// something softer outstanding — completeness, then property tests — so
	// the probe went unasked for six attempts and then failed the run at 203
	// seconds for a defect nothing had ever mentioned.
	if adversarialRounds < adversarialRoundBackstop {
		if findings, probed, probeErr := execution.probeProducedCommands(
			ctx, scope.worktree,
		); probeErr == nil && probed > 0 && len(findings) > 0 {
			work.gate = "adversarial"
			work.because = "it mishandles input nobody intended"
			work.askedForAdversarial = true
			parts = append(parts, hostileInputInstruction(findings, testsPassed))
			summaries = append(summaries, fmt.Sprintf(
				"%d way(s) it mishandles unintended input", len(findings)))
		} else if probeErr == nil && probed > 0 {
			satisfied = append(satisfied,
				"Every produced command survives input nobody intended.")
		}
	}

	// A decoding boundary with nothing fuzzing it, while attempts remain.
	//
	// atom-fuzz is a hard gate and it was measured only after the loop, so a
	// run was failed for a fuzz target nobody had asked it to write. Ladder
	// rung 6 on 2026-08-03 ended on "1 decoding boundary(ies) were produced and
	// no fuzz target was written for any of them", first said at 159 seconds
	// with the last send-back at 93.
	//
	// Asked alongside the rest rather than deferred, because the question here
	// is only whether a FuzzXxx function exists — a scan of the produced
	// sources. The expensive part of that stage, running the fuzzer, is reached
	// only once a target exists, and this asks before there is one.
	const fuzzRoundBackstop = 2
	if fuzzRounds < fuzzRoundBackstop {
		if missing := boundariesWithoutFuzzTargets(scope.worktree); len(missing) > 0 {
			work.gate = "atom-fuzz"
			work.because = "a decoding boundary has nothing fuzzing it"
			work.askedForFuzz = true
			parts = append(parts, fuzzTargetInstruction(missing, testsPassed))
			summaries = append(summaries, fmt.Sprintf(
				"%d decoding boundary(ies) with no fuzz target", len(missing)))
		}
	}

	// Tests that cannot detect a defect, once everything else holds.
	//
	// The mutation stage plants deliberate defects and asks whether the suite
	// notices. It is a hard gate — a suite that catches three of eight fails
	// the rung — and like the hostile-input probe it was measured only after
	// the loop, so a run was failed for a suite nobody had told it was weak.
	// Ladder rung 4 on 2026-08-03: "the tests caught 3 of 8 deliberate defects
	// (43%)", first mentioned at 237 seconds, with the last send-back at 141.
	//
	// Deferred behind everything else, unlike the probe, and for a reason the
	// probe does not share: this costs a whole suite execution per planted
	// defect, ten seconds where the probe costs one. A run with a broken build
	// or an untried empty slice has cheaper things to fix first, and the
	// measurement would be answering a question about a suite that is about to
	// change anyway.
	const mutationRoundBackstop = 2
	if len(parts) == 0 && mutationRounds < mutationRoundBackstop {
		outcome := execution.checkMutations(ctx, scope.worktree, attribution)
		if survivors, present := outcome.Evidence["survivors"].([]string); present &&
			!outcome.Held && len(survivors) > 0 {
			work.gate = "atom-mutation"
			work.because = "its tests cannot detect a defect"
			work.askedForMutation = true
			parts = append(parts, survivingDefectInstruction(survivors, testsPassed))
			summaries = append(summaries, fmt.Sprintf(
				"%d deliberate defect(s) its tests did not notice", len(survivors)))
		} else if outcome.Held {
			satisfied = append(satisfied,
				"The tests notice a deliberate defect in what this run wrote.")
		}
	}

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

// uncoveredChangedLines names the lines this run wrote that nothing executes.
//
// It reuses the stage's own measurement rather than a second one of its own,
// so the refinement loop and the terminal ledger cannot disagree about what is
// covered — which is the failure mode that put this check after the loop in the
// first place.
func uncoveredChangedLines(
	ctx context.Context, worktree string, attribution changeAttribution,
) []string {
	outcome := checkFunctionCoverage(ctx, worktree, attribution)
	if outcome.Held || outcome.Skipped || outcome.Evidence == nil {
		return nil
	}
	raw, present := outcome.Evidence["uncovered_changed_lines"]
	if !present {
		return nil
	}
	lines, ok := raw.([]string)
	if !ok {
		return nil
	}
	return lines
}

// uncoveredLineInstruction asks for a test that reaches each line.
//
// It names the lines rather than the percentage. A run told "coverage is 87%"
// has to work out which lines the number is about before it can do anything;
// a run told "main.go:41 is executed by nothing" already knows.
func uncoveredLineInstruction(uncovered []string) string {
	return fmt.Sprintf(
		"These lines were written by this run and are executed by no test: "+
			"%s.\n\nAdd a test that reaches each one. Where a line is an error "+
			"branch, make the error happen rather than deleting the branch: a "+
			"path nothing exercises is a path nothing has ever shown to work. "+
			"If a line genuinely cannot be reached — a defensive check for a "+
			"state the types forbid — write \"//codeflux:unreachable <why>\" "+
			"on the line above it and leave the branch alone. The reason is "+
			"required and is read by a person, not matched: say what makes the "+
			"state impossible. A marker with nothing after it exempts "+
			"nothing.",
		strings.Join(uncovered, ", "))
}

// propertyTestInstruction asks for one test that states a relationship.
//
// It says what a property is in terms of the code rather than in terms of
// testing vocabulary. A run told "add a property-based test" reaches for a
// fuzzing library it does not have; a run told "write one table over several
// inputs and assert the relationship that holds for all of them" writes the
// thing that was wanted, in plain Go, in ten lines.
func propertyTestInstruction(detail string, testsPassed bool) string {
	return validationPreamble(testsPassed) +
		"no test states a property over a set of inputs — every test " +
		"checks one example and its one expected answer. " + detail +
		"\n\nAdd one test that asserts a relationship which must hold across " +
		"a range of inputs, rather than a value for a single input. Write it " +
		"as an ordinary table in plain Go over a handful of chosen inputs; no " +
		"fuzzing library is needed or wanted.\n\n" +
		"A property is a statement that stays true as the input varies: that " +
		"a decoded value round-trips to what encoded it, that a sorted result " +
		"holds the same elements as its input, that a total equals the sum of " +
		"its parts, that an operation on two values agrees with the operator " +
		"it implements. Keep the values small enough that overflow is not the " +
		"thing under test.\n\n" +
		// The shape, not only the idea.
		//
		// Described and not shown, this was read and not acted on. Ladder rung
		// 18 on 2026-08-04 was told twice, in exactly the words above, and
		// wrote twenty-two tests without a loop in any of them — on a rung
		// whose requirement asks for the monad laws, which are properties in
		// the only sense this gate means. What was missing was the five lines
		// that say what an answer looks like.
		"The shape wanted, which is all it has to be:\n\n" +
		"\tfor _, in := range []int{0, 1, 2, 7, 50} {\n" +
		"\t\tif got := Decode(Encode(in)); got != in {\n" +
		"\t\t\tt.Errorf(\"round trip of %d gave %d\", in, got)\n" +
		"\t\t}\n" +
		"\t}\n\n" +
		"One loop, several inputs, one relationship asserted for every one of " +
		"them. The relationship is the part to think about; the loop is not."
}

// hostileInputInstruction is what a run is told about input it mishandles.
//
// The findings name the command and what it did, which is the actionable part:
// "given nothing at all: accepted it, printed nothing, and exited zero" says
// both the input and the wrong behaviour, and a run told that can decide what
// the right behaviour is. A run told only "it is not robust" cannot.
func hostileInputInstruction(findings []string, testsPassed bool) string {
	var instruction strings.Builder
	instruction.WriteString(validationPreamble(testsPassed) +
		"the program mishandles input nobody intended. Each of these is the " +
		"built command actually run against that input:\n\n")
	for _, finding := range findings {
		instruction.WriteString("  " + finding + "\n")
	}
	instruction.WriteString(
		"\nDecide what each case should do and make it do that. Refusing an " +
			"input is a correct answer and so is handling it; accepting it " +
			"silently and exiting zero is not, because a caller cannot tell " +
			"that from success. Do not change what the acceptance examples " +
			"already produce.")
	return instruction.String()
}

// survivingDefectInstruction is what a run is told about defects its tests
// missed.
//
// Each survivor names the file and the change that was made to it, which is the
// actionable part: a run told "the tests caught three of eight" knows only that
// it scored badly, and a run told which line was altered without anything
// noticing knows what to assert.
func survivingDefectInstruction(survivors []string, testsPassed bool) string {
	var instruction strings.Builder
	instruction.WriteString(validationPreamble(testsPassed) +
		"a deliberate defect was planted in what this run wrote and the tests " +
		"did not notice. Each of these was changed, the suite was run, and it " +
		"still passed:\n\n")
	for _, survivor := range survivors {
		instruction.WriteString("  " + survivor + "\n")
	}
	instruction.WriteString(
		"\nAdd or strengthen a test so each one would fail. Assert the value " +
			"the code produces, not merely that it produced something: a test " +
			"that runs a function and checks nothing about the answer passes " +
			"whatever the function does. Do not change the production code to " +
			"suit a test.")
	return instruction.String()
}

// boundariesWithoutFuzzTargets names the decoding boundaries this run produced
// when nothing fuzzes any of them.
//
// All or nothing, deliberately, because that is what the gate measures: it
// fails when there are boundaries and no target at all, and once one target
// exists it moves on to running the fuzzer. Naming every boundary when the
// count is zero tells the run what the targets would be for; naming them once
// one exists would be asking for work the gate is not waiting on.
func boundariesWithoutFuzzTargets(worktree string) []string {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil
	}
	targets := 0
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(worktree, file))
		if readErr != nil {
			continue
		}
		targets += strings.Count(string(body), "func Fuzz")
	}
	if targets > 0 {
		return nil
	}
	boundaries, err := decodingBoundaries(worktree)
	if err != nil {
		return nil
	}
	return boundaries
}

// fuzzTargetInstruction asks for a fuzz target over each decoding boundary.
func fuzzTargetInstruction(boundaries []string, testsPassed bool) string {
	var instruction strings.Builder
	instruction.WriteString(validationPreamble(testsPassed) +
		"these functions decode input and nothing fuzzes them:\n\n")
	for _, boundary := range boundaries {
		instruction.WriteString("  " + boundary + "\n")
	}
	instruction.WriteString(
		"\nWrite a Go fuzz target for each — func FuzzXxx(f *testing.F), with " +
			"f.Add for a seed or two and f.Fuzz for the body. Assert that it " +
			"does not panic and that whatever it returns is self-consistent; a " +
			"decoder is allowed to refuse input, so a returned error is a pass, " +
			"not a failure. Do not change the decoder to make fuzzing easier.")
	return instruction.String()
}
