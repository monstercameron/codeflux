package coordinator

import (
	"strings"
	"testing"
)

// TestPIPE138_ADeclaredPureAtomThatReachesOutsideFailsTheAtomsGate is the
// discrimination proof for PIPE-138. The flow's own gate for the atoms stage
// says an atom "reads nothing outside its arguments", and until this ticket
// checkAtoms only counted how many atoms happened to be pure — it never
// failed on one that was not, however loudly it had declared purity.
//
// Proven to discriminate: with the purity-violation check removed from
// checkAtoms (reverting to counting alone), this exact fixture — an atom
// declaring "Effects: None: pure atom" whose body calls os.WriteFile — would
// report held, because len(atoms) > 0 was the only condition the old check
// ever tested. Here it reports broke, naming the atom and what it reaches
// for.
func TestPIPE138_ADeclaredPureAtomThatReachesOutsideFailsTheAtomsGate(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/purity\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\nimport \"os\"\n\n" +
			declaredAtomComment("WriteMarker", "None: pure atom.") +
			"func WriteMarker() error {\n" +
			"\treturn os.WriteFile(\"marker\", nil, 0o600)\n}\n",
	})

	outcome := checkAtoms(worktree, newProducedFunctionCache(worktree))
	if outcome.Held {
		t.Fatal("an atom that declares purity and reaches outside its own " +
			"arguments was reported held")
	}
	if !strings.Contains(outcome.Detail, "WriteMarker") {
		t.Errorf("the violation does not name the atom: %q", outcome.Detail)
	}
	violations, _ := outcome.Evidence["purity_violations"].([]string)
	if len(violations) != 1 {
		t.Fatalf("purity_violations = %v, want exactly one", violations)
	}
	if !strings.Contains(violations[0], "os.WriteFile") {
		t.Errorf("the violation does not name what the atom reaches for: %q",
			violations[0])
	}
}

// TestPIPE138_ADeclaredPureAtomThatIsActuallyPureHolds is the control case:
// declared and observed agree, so the gate can genuinely hold.
func TestPIPE138_ADeclaredPureAtomThatIsActuallyPureHolds(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/purity\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\n" +
			declaredAtomComment("Square", "None: pure atom.") +
			"func Square(value int) int {\n\treturn value * value\n}\n",
	})

	outcome := checkAtoms(worktree, newProducedFunctionCache(worktree))
	if !outcome.Held {
		t.Fatalf("a declared-pure atom that is actually pure did not hold: %+v",
			outcome)
	}
	if count, present := outcome.Evidence["declared_pure_atoms"]; !present ||
		count != 1 {
		t.Errorf("declared_pure_atoms is not recorded as one: %+v", outcome.Evidence)
	}
}

// TestPIPE138_AnUndeclaredImpureAtomIsNotJudged guards the boundary: this
// ticket enforces purity "where a contract declares it", not everywhere.
// Ordinary produced code carries no //codeflux:atom declaration at all — the
// entire existing engine test suite is exactly this shape — and an atom with
// nothing declared for it has nothing to contradict, so the gate must still
// hold on the strength of len(atoms) > 0 alone, the way it always has.
func TestPIPE138_AnUndeclaredImpureAtomIsNotJudged(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/purity\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\nimport \"os\"\n\n" +
			"func WriteMarker() error {\n" +
			"\treturn os.WriteFile(\"marker\", nil, 0o600)\n}\n",
	})

	outcome := checkAtoms(worktree, newProducedFunctionCache(worktree))
	if !outcome.Held {
		t.Fatalf("an impure atom with no declared contract was refused, but "+
			"nothing declared purity for it to contradict: %+v", outcome)
	}
	if count, present := outcome.Evidence["declared_pure_atoms"]; !present ||
		count != 0 {
		t.Errorf("declared_pure_atoms is not recorded as zero: %+v", outcome.Evidence)
	}
}
