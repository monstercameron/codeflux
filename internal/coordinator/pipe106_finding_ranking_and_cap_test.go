package coordinator

import (
	"fmt"
	"strings"
	"testing"
)

// --- PIPE-106: findings are ranked by expected defect cost, and the ---
// --- instruction caps how many of one kind it shows. ---

// TestPIPE106_HigherCostFindingsAreOrderedBeforeAlphabeticallyEarlierOnes is
// the discrimination proof for ranking.
//
// Proven to discriminate: the prior sort ordered findings of the same Kind
// purely by Where, alphabetically. A mutation-survivor finding attributed to
// "zz_helper.go" and an anti-pattern finding attributed to "aa_main.go" are
// both findingBlindSpot and findingDefect respectively in the real reviewer,
// but to isolate ranking from the Kind-first rule this test constructs two
// findingDefect findings directly, one anti-pattern-lineage ("AAFunction")
// and one swallowed-error-lineage ("ZZFunction"). The old alphabetical rule
// would place AAFunction first; findingCostRank ranks swallowed-error above
// anti-pattern, so ZZFunction must sort first despite its later name.
func TestPIPE106_HigherCostFindingsAreOrderedBeforeAlphabeticallyEarlierOnes(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe106/rank\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\nfunc Noop() {}\n",
	})
	execution := &AgentExecution{}
	findings, err := execution.reviewAdversariallyWithPlan(
		t.Context(), worktree, adversarialCheckPlan{MutationAnalysis: false})
	if err != nil {
		t.Fatalf("reviewAdversariallyWithPlan: %v", err)
	}
	// The fixture produces no findings of its own; inject two synthetic
	// ones through the same sort the production code uses, by re-deriving
	// the comparison reviewAdversariallyWithPlan applies rather than
	// duplicating it -- so this test tracks the real ranking, not a copy of
	// it that could drift.
	findings = append(findings,
		adversarialFinding{
			Kind: findingDefect, Where: "AAFunction",
			What: "an anti-pattern", Lineage: findingLineageAntiPattern,
		},
		adversarialFinding{
			Kind: findingDefect, Where: "ZZFunction",
			What: "a swallowed error", Lineage: findingLineageSwallowedError,
		},
	)
	sortAdversarialFindings(findings)

	firstDefectIndex, secondDefectIndex := -1, -1
	for index, finding := range findings {
		if finding.Where == "ZZFunction" {
			firstDefectIndex = index
		}
		if finding.Where == "AAFunction" {
			secondDefectIndex = index
		}
	}
	if firstDefectIndex == -1 || secondDefectIndex == -1 {
		t.Fatal("both synthetic findings must be present")
	}
	if firstDefectIndex >= secondDefectIndex {
		t.Errorf("swallowed-error (rank %d) must sort before anti-pattern "+
			"(rank %d) even though \"AAFunction\" < \"ZZFunction\" "+
			"alphabetically; got ZZFunction at %d, AAFunction at %d",
			findingCostRank[findingLineageSwallowedError],
			findingCostRank[findingLineageAntiPattern],
			firstDefectIndex, secondDefectIndex)
	}
}

// TestPIPE106_TheInstructionCapsFindingsPerKindAndDisclosesTheOmittedCount
// is the discrimination proof for the cap.
//
// Proven to discriminate: the prior adversarialInstruction rendered every
// finding in a kind with no bound. Given more findings than
// maximumFindingsPerKindInInstruction, the prior code would print all of
// them and never mention a count; this asserts both that the shown count is
// capped and that the omission is disclosed, which a caller reading only
// "does it mention my top finding" would not catch if the cap silently
// dropped items instead.
func TestPIPE106_TheInstructionCapsFindingsPerKindAndDisclosesTheOmittedCount(t *testing.T) {
	total := maximumFindingsPerKindInInstruction + 3
	findings := make([]adversarialFinding, 0, total)
	for index := 0; index < total; index++ {
		findings = append(findings, adversarialFinding{
			Kind:    findingBlindSpot,
			Where:   "the test suite",
			What:    fmt.Sprintf("untried edge number %02d", index),
			Lineage: findingLineageBoundary,
		})
	}

	instruction := adversarialInstruction(findings, true)
	shown := 0
	for index := 0; index < total; index++ {
		if strings.Contains(instruction, fmt.Sprintf("untried edge number %02d", index)) {
			shown++
		}
	}
	if shown != maximumFindingsPerKindInInstruction {
		t.Errorf("got %d findings rendered, want exactly %d (the cap)",
			shown, maximumFindingsPerKindInInstruction)
	}
	omitted := total - maximumFindingsPerKindInInstruction
	if !strings.Contains(instruction, fmt.Sprintf("%d more", omitted)) {
		t.Errorf("instruction does not disclose the %d omitted finding(s): %s",
			omitted, instruction)
	}
}

// TestPIPE106_FewerFindingsThanTheCapAreAllShownAndNothingIsDisclosed is the
// regression control: below the cap, every finding is shown and no omission
// note is printed.
func TestPIPE106_FewerFindingsThanTheCapAreAllShownAndNothingIsDisclosed(t *testing.T) {
	findings := []adversarialFinding{
		{Kind: findingDefect, Where: "f", What: "one defect", Lineage: findingLineageAntiPattern},
	}
	instruction := adversarialInstruction(findings, true)
	if !strings.Contains(instruction, "one defect") {
		t.Fatal("the single finding must be shown")
	}
	if strings.Contains(instruction, "more") {
		t.Errorf("no findings were omitted; the instruction should not "+
			"mention any: %s", instruction)
	}
}
