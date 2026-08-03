package coordinator

import (
	"strings"
	"testing"
)

// declaredAtomComment renders a minimal, schema-v1-valid //codeflux:atom doc
// comment (AGENTS.md "Atom Documentation Style") documenting identifier, with
// the Effects field set to effectsBody. Every other field is filled with the
// shortest content the schema accepts: what these tests examine is agreement
// between the declared Effects field and what the body actually does, not
// documentation quality, which is a different check's gate.
func declaredAtomComment(identifier, effectsBody string) string {
	return "// " + identifier + " exists for this test fixture.\n" +
		"//\n" +
		"//codeflux:atom\n" +
		"// Codeflux atom documentation (schema v1):\n" +
		"//   Purpose:\n" +
		"//     Exists to give this test fixture a declared contract.\n" +
		"//   Use when:\n" +
		"//     A test needs a function with a declared contract.\n" +
		"//   Do not use when:\n" +
		"//     None: this is a test fixture with no real near match.\n" +
		"//   Semantics:\n" +
		"//     Does exactly what its function body does.\n" +
		"//   Inputs:\n" +
		"//     - None: takes no meaningful input for this fixture.\n" +
		"//   Outputs:\n" +
		"//     - None: returns nothing meaningful for this fixture.\n" +
		"//   Preconditions:\n" +
		"//     - None: no precondition applies here.\n" +
		"//   Postconditions:\n" +
		"//     - None: no postcondition applies here.\n" +
		"//   Effects:\n" +
		"//     " + effectsBody + "\n" +
		"//   Failure semantics:\n" +
		"//     None: this fixture cannot fail.\n" +
		"//   Determinism:\n" +
		"//     Always deterministic for this fixture.\n" +
		"//   Idempotency and retry:\n" +
		"//     None: not applicable to this fixture.\n" +
		"//   Reconciliation and compensation:\n" +
		"//     None: not applicable to this fixture.\n" +
		"//   Security and privacy:\n" +
		"//     None: not applicable to this fixture.\n" +
		"//   Dependencies and bindings:\n" +
		"//     None: no dependency for this fixture.\n" +
		"//   Complexity and limits:\n" +
		"//     Runs in constant time for this fixture.\n" +
		"//   Examples:\n" +
		"//     - A single representative call with no arguments.\n" +
		"//   Verification:\n" +
		"//     Covered by this ticket's own fixture test.\n" +
		"//   Retrieval concepts:\n" +
		"//     declared contract test fixture\n"
}

// TestPIPE137_ADeclaredContractCanDisagreeWithItsImplementation is the
// discrimination proof PIPE-137 asks for: a contract "derived from the
// finished code" is satisfied by that code by construction and can never
// disagree with it, which is exactly what made the old describeContracts
// unfalsifiable. Here the declared contract comes from the doc comment, an
// independent source from the AST walk that decides what the body actually
// does, so the two can genuinely diverge — and this fixture makes them
// diverge on purpose: the doc comment declares "None: pure atom" while the
// body calls fmt.Println.
//
// Proven to discriminate: under the old describeContracts, "effects" and
// "pure" were both computed by re-walking this same function body, so they
// could never disagree with themselves — this exact fixture would have
// reported held with no disagreement, because there was nothing independent
// to compare against. Here it reports broke, naming the one function whose
// declaration its own body contradicts.
func TestPIPE137_ADeclaredContractCanDisagreeWithItsImplementation(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/contracts\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\nimport \"fmt\"\n\n" +
			declaredAtomComment("Greet", "None: pure atom.") +
			"func Greet() {\n\tfmt.Println(\"hello\")\n}\n",
	})

	outcome := describeContracts(worktree)
	if outcome.Held {
		t.Fatal("a declared-pure contract whose body prints was reported held; " +
			"the disagreement PIPE-140 exists to catch was not caught")
	}
	if outcome.Skipped {
		t.Fatal("a declared contract that disagrees with its body was skipped " +
			"rather than failed; a real, checkable disagreement exists here")
	}
	if !strings.Contains(outcome.Detail, "Greet") {
		t.Errorf("the disagreement does not name the function: %q", outcome.Detail)
	}
	disagreements, _ := outcome.Evidence["disagreements"].([]string)
	if len(disagreements) != 1 {
		t.Fatalf("disagreements = %v, want exactly one", disagreements)
	}
}

// TestPIPE137_ADeclaredContractThatAgreesHolds is the control: the same
// shape of fixture, but the declaration and the body actually agree, so the
// stage can genuinely say so.
func TestPIPE137_ADeclaredContractThatAgreesHolds(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/contracts\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\n" +
			declaredAtomComment("Double", "None: pure atom.") +
			"func Double(value int) int {\n\treturn value * 2\n}\n",
	})

	outcome := describeContracts(worktree)
	if !outcome.Held {
		t.Fatalf("a declared-pure contract whose body is pure did not hold: %+v",
			outcome)
	}
	if derivedAfter, present := outcome.Evidence["derived_after"]; !present ||
		derivedAfter != false {
		t.Errorf("derived_after is not recorded false: %+v", outcome.Evidence)
	}
	contracts, _ := outcome.Evidence["contracts"].(map[string]any)
	entry, ok := contracts["Double"].(map[string]any)
	if !ok {
		t.Fatal("the declared function has no contract entry")
	}
	if declared, _ := entry["declared"].(bool); !declared {
		t.Error("a function carrying a valid //codeflux:atom comment is not " +
			"recorded as declared")
	}
}

// TestPIPE137_NoDeclaredContractIsSkippedNotSatisfied covers the vacuity
// guard: when nothing produced carries a //codeflux:atom declaration, there
// is nothing yet to check agreement against, and PIPE-010/PIPE-011 already
// established that a check unable to establish its gate records skipped, not
// satisfied — never inventing a contract from the code to report a pass.
func TestPIPE137_NoDeclaredContractIsSkippedNotSatisfied(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/contracts\n\ngo 1.26.0\n",
		// initializeCoordinatorGitRepository already commits a base main.go
		// with byte-identical content to the module's usual placeholder, so a
		// fixture must write something that differs from it to read as
		// produced by git status — lib.go, undocumented, does that here.
		"lib.go": "package lib\n\nfunc Flat() int { return 1 }\n",
	})

	outcome := describeContracts(worktree)
	if outcome.Held {
		t.Fatal("a run with no declared contract anywhere reported satisfied; " +
			"nothing was checked, so nothing may be claimed held")
	}
	if !outcome.Skipped {
		t.Errorf("an undeclared run is neither held nor skipped: %+v", outcome)
	}
	if count, present := outcome.Evidence["declared_count"]; !present || count != 0 {
		t.Errorf("declared_count is not recorded as zero: %+v", outcome.Evidence)
	}
}
