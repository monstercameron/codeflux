package pipeline

import "testing"

// TestClassifyDeclineTreatsAnUnboundStageAsUnimplemented pins the exact case
// two copies of this rule disagreed about.
//
// The coordinator returned unimplemented for a stage with no declared check.
// The browser's copy folded that test in with the may-not-satisfy branch, so it
// answered no-check-by-design instead — or principled-decline, if the stage
// happened to appear in Section33Resolved. The same ledger therefore classified
// differently depending on who read it, and the difference was in the direction
// that flatters: a stage the flow gained without a performer would have been
// presented to a reader as a deliberate design decision.
//
// That is the failure the skip audit exists to prevent, reproduced inside the
// audit's own reporting.
func TestClassifyDeclineTreatsAnUnboundStageAsUnimplemented(t *testing.T) {
	// A number no stage in the flow uses, so CheckFor cannot bind it.
	var unbound Number = 9999
	for _, state := range []State{StateSkipped, StateNotImplemented} {
		class, ticket := ClassifyDecline(unbound, state)
		if class != DeclineUnimplemented {
			t.Errorf("an unbound stage in state %q classified %q, want %q; a "+
				"stage the flow gained without a performer is not a decision "+
				"anybody made, and must not read as one",
				state, class, DeclineUnimplemented)
		}
		if ticket != "" {
			t.Errorf("an unbound stage named ticket %q; there is no check to "+
				"own it", ticket)
		}
	}
}

// TestClassifyDeclineSeparatesThePrincipledFromTheUnbuilt covers the
// distinction the whole audit exists to preserve.
func TestClassifyDeclineSeparatesThePrincipledFromTheUnbuilt(t *testing.T) {
	// A stage §33 records as resolved by making its check unable to claim.
	class, ticket := ClassifyDecline(StageAtomOptimization, StateSkipped)
	if class != DeclinePrincipled {
		t.Errorf("atom-optimization classified %q, want %q", class, DeclinePrincipled)
	}
	if ticket == "" {
		t.Error("a principled decline resolved by §33 names no governing ticket, " +
			"so a reader cannot find out who owns it")
	}

	// A stage nothing can perform, because it is a person's decision.
	class, _ = ClassifyDecline(StageHumanAcceptance, StateSkipped)
	if class != DeclineNoCheckByDesign {
		t.Errorf("human-acceptance classified %q, want %q",
			class, DeclineNoCheckByDesign)
	}

	// A stage whose check may satisfy and simply did not run.
	class, _ = ClassifyDecline(StageAtoms, StateNotImplemented)
	if class != DeclineUnimplemented {
		t.Errorf("an unimplemented atoms row classified %q, want %q",
			class, DeclineUnimplemented)
	}
}
