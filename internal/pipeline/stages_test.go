package pipeline

import "testing"

// TestTheFlowIsOrderedGaplessAndUniquelyNamed guards the vocabulary itself.
//
// The ledger records a stage by number and by name, and a reader trusts the
// number for ordering and the name for identity. Both of those only mean
// anything if the flow is a total order with no repeats: a gap makes "how far
// did it get" unanswerable, and a duplicated name makes two different checks
// indistinguishable in the record.
func TestTheFlowIsOrderedGaplessAndUniquelyNamed(t *testing.T) {
	seenNames := map[string]Number{}
	for index, stage := range Flow {
		expected := Number(index + 1)
		if stage.Number != expected {
			t.Fatalf("stage %q sits at position %d but is numbered %d",
				stage.Name, expected, stage.Number)
		}
		if stage.Name == "" {
			t.Fatalf("stage %d has no name, so its evidence cannot be identified",
				stage.Number)
		}
		if stage.Gate == "" {
			t.Fatalf("stage %q states no gate, so nothing says what it checks",
				stage.Name)
		}
		if earlier, repeated := seenNames[stage.Name]; repeated {
			t.Fatalf("stages %d and %d are both named %q",
				earlier, stage.Number, stage.Name)
		}
		seenNames[stage.Name] = stage.Number
	}
}

// TestAnAtomIsDocumentedOnlyAfterItIsChecked pins the placement of the
// documentation stage, which is the whole reason it sits where it does.
//
// Documentation written beside the implementation records what its author
// meant. Documentation written after the atom has been tested, fuzzed, and
// mutation-scored records what the atom is known to do — including the
// behaviour at the edges, which is exactly the part that only shows up under
// checking and exactly the part a later reader needs.
func TestAnAtomIsDocumentedOnlyAfterItIsChecked(t *testing.T) {
	for _, check := range []struct {
		name  string
		stage Number
	}{
		{"the atom exists", StageAtoms},
		{"its tests pass", StageAtomVerification},
		{"it survives fuzzing", StageAtomFuzz},
		{"its tests are known to detect defects", StageAtomMutation},
	} {
		if check.stage >= StageAtomDocumentation {
			t.Errorf("an atom is documented before %s, so the documentation "+
				"describes an intention rather than a verified behaviour",
				check.name)
		}
	}
	if StageAtomDocumentation >= StageCompositionObligations {
		t.Error("atoms are composed before they are documented, so a molecule " +
			"is built out of parts nothing has described")
	}
}

// TestDocumentationFeedsRecallRatherThanThisRun records why the stage exists at
// all beyond being tidy.
//
// Recall happens near the start of the flow and documentation near the end of
// the atom phase, so an atom documented by this run is reused by the next one.
// That ordering is the point: the registry is built by finished work and spent
// by work that has not started. A build that documented nothing would make
// recall permanently empty and every atom would be written again from nothing.
func TestDocumentationFeedsRecallRatherThanThisRun(t *testing.T) {
	if StageRecall >= StageAtomDocumentation {
		t.Fatal("recall runs after documentation, which would mean a run can " +
			"only reuse what it has already written itself")
	}
	documentation, found := StageByNumber(StageAtomDocumentation)
	if !found {
		t.Fatal("the documentation stage is not in the flow")
	}
	for _, required := range []string{"purpose", "inputs", "outputs", "algorithm"} {
		if !containsWord(documentation.Gate, required) {
			t.Errorf("the documentation gate does not require %q, so an atom "+
				"could satisfy it while leaving out the thing a reader needs",
				required)
		}
	}
}

// containsWord reports whether a gate mentions one required element.
func containsWord(gate, word string) bool {
	for index := 0; index+len(word) <= len(gate); index++ {
		if gate[index:index+len(word)] == word {
			return true
		}
	}
	return false
}

// TestAnAtomIsOptimisedOnlyOnceItsTestsCanCatchAMistake is the ordering that
// makes an optimiser safe rather than a defect generator.
//
// Rewriting code is only as safe as the tests guarding it. Optimising before
// the mutation score is known means rewriting under tests nobody has shown can
// detect a fault — which is precisely how an optimisation that quietly changes
// behaviour arrives at delivery with a green suite behind it.
func TestAnAtomIsOptimisedOnlyOnceItsTestsCanCatchAMistake(t *testing.T) {
	for _, earlier := range []struct {
		name  string
		stage Number
	}{
		{"the atom is implemented", StageAtoms},
		{"its tests pass", StageAtomVerification},
		{"it survives fuzzing", StageAtomFuzz},
		{"its tests are known to detect defects", StageAtomMutation},
	} {
		if earlier.stage >= StageAtomOptimization {
			t.Errorf("an atom is rewritten before %s, so nothing can tell "+
				"whether the rewrite changed what it does", earlier.name)
		}
	}
	optimisation, found := StageByNumber(StageAtomOptimization)
	if !found {
		t.Fatal("the optimisation stage is not in the flow")
	}
	// The gate has to be about behaviour, not size. An optimiser judged on
	// how much smaller the code got is an optimiser rewarded for breaking it.
	if !containsWord(optimisation.Gate, "still passes") {
		t.Errorf("the optimisation gate does not require the tests to still "+
			"pass: %q", optimisation.Gate)
	}
}

// TestComplexityIsMeasuredOnWhatWillActuallyShip fixes the other ordering.
//
// A bound measured before optimisation describes code that was then rewritten,
// which is a number about something nobody will run. And the label belongs in
// the documentation, so it has to exist before the atom is written up.
func TestComplexityIsMeasuredOnWhatWillActuallyShip(t *testing.T) {
	if StageAtomComplexity <= StageAtomOptimization {
		t.Error("complexity is measured before the atom is optimised, so the " +
			"recorded bound describes code that no longer exists")
	}
	if StageAtomComplexity >= StageAtomDocumentation {
		t.Error("the atom is documented before its complexity is known, so " +
			"the documentation cannot carry the bound")
	}
	complexity, found := StageByNumber(StageAtomComplexity)
	if !found {
		t.Fatal("the complexity stage is not in the flow")
	}
	// Claimed and measured, and required to agree. A bound read off the
	// structure alone is a guess; growth measured alone is a sample. Only
	// together do they catch the case where the code says linear and the
	// machine says quadratic.
	for _, required := range []string{"time", "space", "measured"} {
		if !containsWord(complexity.Gate, required) {
			t.Errorf("the complexity gate does not require %q: %q",
				required, complexity.Gate)
		}
	}
}
