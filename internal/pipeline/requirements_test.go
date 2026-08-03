package pipeline

import (
	"strings"
	"testing"
)

// cloneRequirements returns a deep copy of the real table so a test can
// break it without mutating the package variable other tests read.
func cloneRequirements() []Requirement {
	cloned := make([]Requirement, len(Requirements))
	for index, requirement := range Requirements {
		deps := make([]Number, len(requirement.Requires))
		copy(deps, requirement.Requires)
		cloned[index] = Requirement{Stage: requirement.Stage, Requires: deps}
	}
	return cloned
}

// withRequires returns table with one stage's Requires replaced, for tests
// that need to break a single entry without disturbing the rest.
func withRequires(table []Requirement, stage Number, requires ...Number) []Requirement {
	for index := range table {
		if table[index].Stage == stage {
			table[index].Requires = requires
			return table
		}
	}
	return table
}

// TestPIPE058_TheRealTableIsValid is the sanity check the rest of this file
// exists to give teeth to: the declared table, as shipped, satisfies every
// property ValidateRequirements checks, and it covers the flow completely.
func TestPIPE058_TheRealTableIsValid(t *testing.T) {
	if findings := ValidateRequirements(); len(findings) > 0 {
		t.Fatalf("the shipped Requirements table has findings:\n  %s",
			strings.Join(findings, "\n  "))
	}
	if len(Requirements) != len(Flow) {
		t.Fatalf("%d requirement entries for %d flow stages",
			len(Requirements), len(Flow))
	}
	if _, found := RequirementFor(StageInstructions); !found {
		t.Fatal("RequirementFor cannot find the root stage")
	}
	if _, found := RequirementFor(Number(999)); found {
		t.Fatal("RequirementFor reports a stage that does not exist")
	}
}

// TestPIPE058_EveryStageHasExactlyOneDeclaredEntry covers the coverage half
// of PIPE-058's property 4, and proves it discriminates: a table missing a
// stage, a table binding one twice, and a table binding a stage outside the
// flow are each caught, and none of those findings appear against the real
// table.
func TestPIPE058_EveryStageHasExactlyOneDeclaredEntry(t *testing.T) {
	if findings := validateRequirementsCoverFlow(Requirements); len(findings) > 0 {
		t.Fatalf("the real table fails coverage:\n  %s", strings.Join(findings, "\n  "))
	}

	t.Run("missing entry", func(t *testing.T) {
		broken := cloneRequirements()
		// Drop deliver's entry entirely, as a lane adding a new stage might
		// forget to add its row.
		for index, requirement := range broken {
			if requirement.Stage == StageDeliver {
				broken = append(broken[:index], broken[index+1:]...)
				break
			}
		}
		findings := validateRequirementsCoverFlow(broken)
		if !containsSubstring(findings, "stage 37 (deliver) has no entry") {
			t.Fatalf("dropping deliver's entry was not caught: %v", findings)
		}
	})

	t.Run("duplicate entry", func(t *testing.T) {
		broken := cloneRequirements()
		broken = append(broken, Requirement{Stage: StageInstructions})
		findings := validateRequirementsCoverFlow(broken)
		if !containsSubstring(findings, "stage 1 (instructions) is bound 2 times") {
			t.Fatalf("a duplicated entry was not caught: %v", findings)
		}
	})

	t.Run("entry for an unknown stage", func(t *testing.T) {
		broken := cloneRequirements()
		broken = append(broken, Requirement{Stage: Number(999)})
		findings := validateRequirementsCoverFlow(broken)
		if !containsSubstring(findings, "Requirements binds stage 999, which is not in the flow") {
			t.Fatalf("an entry for a stage outside the flow was not caught: %v", findings)
		}
	})
}

// TestPIPE058_EveryRequirementNamesAKnownStage covers PIPE-058's property 5
// and proves it discriminates from the coverage check above: a requirement
// naming a stage that does not exist is a different defect from a missing or
// duplicated entry, and needs its own check to be caught.
func TestPIPE058_EveryRequirementNamesAKnownStage(t *testing.T) {
	if findings := validateRequirementsNameKnownStages(Requirements); len(findings) > 0 {
		t.Fatalf("the real table names an unknown stage:\n  %s", strings.Join(findings, "\n  "))
	}

	broken := withRequires(cloneRequirements(), StageDeliver, StageHumanAcceptance, Number(999))
	findings := validateRequirementsNameKnownStages(broken)
	if !containsSubstring(findings, "stage 37 requires stage 999, which is not in the flow") {
		t.Fatalf("a requirement naming an unknown stage was not caught: %v", findings)
	}
}

// TestPIPE058_NoRequirementPointsForwardInTheFlow covers PIPE-058's property
// 3 — the property that keeps the declared graph consistent with the total
// order the ledger reports progress in — and proves it discriminates against
// both a forward-pointing edge and a self-reference, neither of which any
// other check would catch.
func TestPIPE058_NoRequirementPointsForwardInTheFlow(t *testing.T) {
	if findings := validateRequirementsDoNotPointForward(Requirements); len(findings) > 0 {
		t.Fatalf("the real table has a forward-pointing edge:\n  %s", strings.Join(findings, "\n  "))
	}

	t.Run("forward edge", func(t *testing.T) {
		// Clarification (2) may not require atoms (10): that stage has not
		// happened yet when clarification runs.
		broken := withRequires(cloneRequirements(), StageClarification, StageAtoms)
		findings := validateRequirementsDoNotPointForward(broken)
		if !containsSubstring(findings, "stage 2 requires stage 10, which is not earlier in the flow") {
			t.Fatalf("a forward-pointing edge was not caught: %v", findings)
		}
	})

	t.Run("self reference", func(t *testing.T) {
		broken := withRequires(cloneRequirements(), StageContracts, StageContracts)
		findings := validateRequirementsDoNotPointForward(broken)
		if !containsSubstring(findings, "stage 5 requires stage 5, which is not earlier in the flow") {
			t.Fatalf("a stage requiring itself was not caught: %v", findings)
		}
	})
}

// TestPIPE058_TheRequirementGraphIsAcyclic covers PIPE-058's property 1 and
// proves the cycle report names the actual stages involved, in the order the
// cycle runs, rather than only asserting a cycle exists.
func TestPIPE058_TheRequirementGraphIsAcyclic(t *testing.T) {
	if cycle := requirementsCycle(Requirements); cycle != nil {
		t.Fatalf("the real table contains a cycle: %v", cycle)
	}
	if findings := validateRequirementsAreAcyclic(Requirements); len(findings) > 0 {
		t.Fatalf("the real table is reported cyclic:\n  %s", strings.Join(findings, "\n  "))
	}

	// Build a three-stage cycle: atoms -> atom-verification -> atom-mutation
	// -> atoms. This is checked in isolation from the forward-pointing rule,
	// which would independently reject two of these three edges; the point
	// here is that cycle detection catches the cycle on its own terms, not
	// that this exact broken table would also fail other checks.
	broken := cloneRequirements()
	broken = withRequires(broken, StageAtoms, StageAtomMutation)
	broken = withRequires(broken, StageAtomVerification, StageAtoms)
	broken = withRequires(broken, StageAtomMutation, StageAtomVerification)

	cycle := requirementsCycle(broken)
	if cycle == nil {
		t.Fatal("the injected cycle was not detected")
	}
	seen := map[Number]bool{}
	for _, stage := range cycle {
		seen[stage] = true
	}
	for _, want := range []Number{StageAtoms, StageAtomVerification, StageAtomMutation} {
		if !seen[want] {
			t.Errorf("the reported cycle %v omits stage %d", cycle, want)
		}
	}

	findings := validateRequirementsAreAcyclic(broken)
	if len(findings) == 0 {
		t.Fatal("validateRequirementsAreAcyclic did not report the injected cycle")
	}
	for _, name := range []string{"atoms", "atom-verification", "atom-mutation"} {
		if !strings.Contains(findings[0], name) {
			t.Errorf("the cycle finding %q does not name stage %q", findings[0], name)
		}
	}
}

// TestPIPE058_EveryStageIsReachableFromTheRoot covers PIPE-058's property 2.
//
// "Reachable" is defined here, matching the ticket's own definition, as: a
// stage with no requirements is a root, and every non-root stage must be
// reachable from a root by following requirement edges forward. This table
// declares exactly one root, instructions, so the check is proven to
// discriminate against two distinct defects: a stage that wrongly declares
// no requirements (which would otherwise silently start a second root and
// present as trivially "reachable" from itself), and a stage whose only
// declared requirement does not resolve to anything, leaving it genuinely
// disconnected from the root.
func TestPIPE058_EveryStageIsReachableFromTheRoot(t *testing.T) {
	if findings := validateEveryStageIsReachable(Requirements); len(findings) > 0 {
		t.Fatalf("the real table has an unreachable stage:\n  %s", strings.Join(findings, "\n  "))
	}

	t.Run("a stage wrongly declares no requirements", func(t *testing.T) {
		broken := withRequires(cloneRequirements(), StageDeliver) // empty: a second root
		findings := validateEveryStageIsReachable(broken)
		if !containsSubstring(findings, "the declared roots are") {
			t.Fatalf("a spurious second root was not caught: %v", findings)
		}
	})

	t.Run("a requirement resolves to nothing", func(t *testing.T) {
		// Distinct from TestPIPE058_EveryRequirementNamesAKnownStage: that
		// test proves the unknown-stage reference itself is reported: this
		// proves the graph-level consequence — with no valid edge into it,
		// deliver has no path back to the root — is reported too, by a
		// different check, so removing either one still leaves the other
		// standing guard.
		broken := withRequires(cloneRequirements(), StageDeliver, Number(999))
		findings := validateEveryStageIsReachable(broken)
		if !containsSubstring(findings, "stage 37 (deliver) is not reachable from any root") {
			t.Fatalf("a genuinely disconnected stage was not caught: %v", findings)
		}
	})
}

// TestPIPE058_TheWidestFrontierIsWiderThanOne is the property the whole
// ticket is written against: a table that mechanically restated "each stage
// requires the previous one" would be acyclic, fully reachable, and forward
// clean, and would still be worthless to PIPE-058a, because it would permit
// no two stages to run at once.
//
// It levels the declared graph by earliest possible round — a stage's level
// is one past the deepest level among what it requires, and a root sits at
// level zero — which is the same leveling a round-based concurrent scheduler
// would compute. The real table's widest level names the five stages that
// become ready the moment the program is built: integration-tests,
// end-to-end-tests, global-invariants, adversarial, and platform-matrix.
func TestPIPE058_TheWidestFrontierIsWiderThanOne(t *testing.T) {
	level := make(map[Number]int, len(Requirements))
	for _, stage := range Flow {
		requirement, found := RequirementFor(stage.Number)
		if !found {
			t.Fatalf("stage %d has no requirement entry", stage.Number)
		}
		deepest := -1
		for _, dependency := range requirement.Requires {
			if level[dependency] > deepest {
				deepest = level[dependency]
			}
		}
		level[stage.Number] = deepest + 1
	}

	byLevel := map[int][]string{}
	for _, stage := range Flow {
		l := level[stage.Number]
		byLevel[l] = append(byLevel[l], stage.Name)
	}

	widest, widestLevel := 0, -1
	for l, names := range byLevel {
		if len(names) > widest {
			widest, widestLevel = len(names), l
		}
	}

	if widest <= 1 {
		t.Fatalf("the widest frontier is %d stage(s); a table that only "+
			"restates the total order would score exactly 1", widest)
	}
	if widest != 5 {
		t.Errorf("expected the widest frontier to be 5 stages, got %d at "+
			"level %d: %v", widest, widestLevel, byLevel[widestLevel])
	}

	want := map[string]bool{
		"integration-tests": true, "end-to-end-tests": true,
		"global-invariants": true, "adversarial": true, "platform-matrix": true,
	}
	got := map[string]bool{}
	for _, name := range byLevel[widestLevel] {
		got[name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("expected %q at the widest level %d, found %v", name, widestLevel, byLevel[widestLevel])
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("unexpected stage %q at the widest level %d", name, widestLevel)
		}
	}

	t.Logf("widest frontier: %d stages at level %d: %v", widest, widestLevel, byLevel[widestLevel])
}

// containsSubstring reports whether any finding contains the given text.
func containsSubstring(findings []string, substring string) bool {
	for _, finding := range findings {
		if strings.Contains(finding, substring) {
			return true
		}
	}
	return false
}
