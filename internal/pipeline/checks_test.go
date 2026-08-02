package pipeline

import (
	"strings"
	"testing"
)

// TestPIPE004_EveryStageIsBoundToTheCheckThatPerformsIt covers PIPE-004.
//
// stages_test.go guards ordering, gaplessness, and naming. Nothing guarded
// gate-to-check agreement, which is what allowed a stage to record satisfied
// for a gate its check does not establish. This binds the two together so a
// disagreement is a failing build rather than something review has to notice.
func TestPIPE004_EveryStageIsBoundToTheCheckThatPerformsIt(t *testing.T) {
	if findings := ValidateChecks(); len(findings) > 0 {
		t.Fatalf("the flow and its check bindings disagree:\n  %s",
			strings.Join(findings, "\n  "))
	}
	if len(Checks) != len(Flow) {
		t.Fatalf("%d bindings for %d stages", len(Checks), len(Flow))
	}
}

// TestPIPE004_AStageWithNoCheckMayNotBeSatisfied is the rule the binding
// exists to carry.
//
// Human acceptance and delivery are decisions a run cannot make for itself.
// Neither has a check, so neither may ever record satisfied; the only honest
// outcomes are blocked, skipped, or not-implemented.
func TestPIPE004_AStageWithNoCheckMayNotBeSatisfied(t *testing.T) {
	uncheckable := 0
	for _, check := range Checks {
		if check.MaySatisfy {
			continue
		}
		uncheckable++
		if check.Unestablished != "" {
			t.Errorf("stage %d may not be satisfied and also names a repair "+
				"(%s); a stage with no check has no gate to establish",
				check.Stage, check.Unestablished)
		}
	}
	if uncheckable == 0 {
		t.Fatal("no stage is marked as having no check, so this rule is vacuous")
	}
}

// TestPIPE004_EveryUnestablishedStageNamesItsRepair keeps the known-weak set
// honest.
//
// docs/plan.md §33 names six stages that record satisfied for gates they only
// partly perform. Recording them here rather than in prose gives the repairs
// one place to flip and gives a reader a way to find out that a satisfied row
// is weaker than it looks. An entry with no TODO would be a claim nobody is
// going to act on.
func TestPIPE004_EveryUnestablishedStageNamesItsRepair(t *testing.T) {
	gaps := UnestablishedStages()
	if len(gaps) == 0 {
		t.Fatal("no stage is recorded as unestablished; §33 names six, so " +
			"either the repairs all landed or the table has been emptied")
	}
	for stage, todo := range gaps {
		if !strings.HasPrefix(todo, "PIPE-") {
			t.Errorf("stage %d names %q, which is not a PIPE ticket", stage, todo)
		}
		check, found := CheckFor(stage)
		if !found {
			t.Errorf("stage %d is unestablished but not bound", stage)
			continue
		}
		if !check.MaySatisfy {
			t.Errorf("stage %d cannot be satisfied, so it has no gate to "+
				"establish and should not name %s", stage, todo)
		}
	}

	// Each of the six stages §33 names as partly performed must be accounted
	// for: still flagged with its repair, or resolved by being made unable to
	// claim satisfied at all. What must not happen is a stage quietly leaving
	// the list while still able to report a pass nobody checked.
	for _, stage := range []Number{
		StageAtomOptimization, StageAtomComplexity, StagePlatformMatrix,
		StageNonFunctional, StageCompositionObligations, StageControlObligations,
	} {
		if _, flagged := gaps[stage]; flagged {
			continue
		}
		if ticket, resolved := Section33Resolved[stage]; resolved {
			if !strings.HasPrefix(ticket, "PIPE-") {
				t.Errorf("stage %d records %q as its repair, which is not a "+
					"PIPE ticket", stage, ticket)
			}
			continue
		}
		check, found := CheckFor(stage)
		if !found {
			t.Errorf("stage %d is named by §33 but is not bound at all", stage)
			continue
		}
		t.Errorf("stage %d is named by §33 as partly performed but is neither "+
			"flagged as unestablished nor recorded as resolved; it may%s report "+
			"satisfied", stage, map[bool]string{true: "", false: " not"}[check.MaySatisfy])
	}
}

// TestPIPE004_EveryPerformerIsNamedOnce catches a copied row.
//
// Two stages sharing a performer is normal — AgentExecution.Run decides
// several — but a stage bound to the wrong neighbour's check would read as
// correct and send a reader to the wrong function.
func TestPIPE004_EveryPerformerIsNamedOnce(t *testing.T) {
	for _, check := range Checks {
		if strings.TrimSpace(check.Performer) == "" {
			t.Errorf("stage %d has an empty performer", check.Stage)
		}
		if strings.Contains(check.Performer, " ") {
			t.Errorf("stage %d names %q, which is not a function identifier",
				check.Stage, check.Performer)
		}
	}
}

// TestPIPE005_TheExaminedSetIsNamedRatherThanARange covers PIPE-005.
//
// The blocking sweep for a module that does not build selected stages by
// numeric range. That is correct only while the flow's numbering happens to
// hold: a stage inserted inside the range is silently enrolled and one moved
// out is silently dropped, with nothing failing either way.
func TestPIPE005_TheExaminedSetIsNamedRatherThanARange(t *testing.T) {
	declared := make(map[Number]struct{}, len(Flow))
	for _, stage := range Flow {
		declared[stage.Number] = struct{}{}
	}

	seen := make(map[Number]struct{}, len(ExaminesProducedSource))
	for _, stage := range ExaminesProducedSource {
		if _, exists := declared[stage]; !exists {
			t.Errorf("the examined set names stage %d, which is not in the flow", stage)
		}
		if _, repeated := seen[stage]; repeated {
			t.Errorf("stage %d is named twice in the examined set", stage)
		}
		seen[stage] = struct{}{}
	}
	if len(ExaminesProducedSource) == 0 {
		t.Fatal("the examined set is empty, so a failing build would block nothing")
	}

	// Evidence-bundle must stay out: it reads the ledger rather than the
	// worktree and is assembled whether or not anything built. The old range
	// ended on it, so its exclusion is the change most likely to be undone by
	// someone restoring the range.
	if _, included := seen[StageEvidenceBundle]; included {
		t.Error("evidence-bundle is in the examined set; it reads the ledger, " +
			"not the produced source, and must be assembled even when nothing built")
	}

	// Stages before contracts describe the request rather than the output, so
	// a failing build says nothing about them.
	for _, early := range []Number{
		StageInstructions, StageClarification, StageAtomicInstructions,
		StageDecompositionCover,
	} {
		if _, included := seen[early]; included {
			t.Errorf("stage %d describes the request, not the produced source, "+
				"and must not be blocked by a failing build", early)
		}
	}
}

// TestPIPE005_TheExaminedSetExcludesStagesRecordedElsewhere guards the
// boundary the old numeric range got wrong.
//
// The range ran from contracts to evidence-bundle and so swept up six stages
// that are written at their own sites. With a duplicate write now refused
// rather than silently dropped (PIPE-002), enrolling one of them here would
// turn a harmless overlap into a caught defect — but the honest fix is to keep
// them out, because examineStructure does not decide them.
func TestPIPE005_TheExaminedSetExcludesStagesRecordedElsewhere(t *testing.T) {
	set := ExaminesProducedSourceSet()
	for stage, why := range map[Number]string{
		StageAssembly:         "recorded by the build gate before examineStructure runs",
		StageProgram:          "recorded beside the build gate",
		StageIntegrationTests: "gated before examineStructure runs",
		StageEndToEndTests:    "decided by the acceptance check",
		StageAdversarial:      "has its own not-built branch",
		StageRecall:           "has its own not-built branch",
		StageEvidenceBundle:   "reads the ledger, not the worktree",
	} {
		if _, included := set[stage]; included {
			t.Errorf("stage %d is in the examined set but is %s, so blocking it "+
				"here would be a second write", stage, why)
		}
	}
}
