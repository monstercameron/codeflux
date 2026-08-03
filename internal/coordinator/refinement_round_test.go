package coordinator

import (
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
)

// TestScopeFollowsTheFindingsNotTheGate is the defect rung 6 showed.
//
// Its review found a real defect in main and the next attempt was restricted to
// test files, because the rule keyed on the gate — one word about a whole
// review — and the finding knew better.
func TestScopeFollowsTheFindingsNotTheGate(t *testing.T) {
	defect := adversarialFinding{
		Kind: findingDefect, Where: "main",
		What:    "calls something that can fail and then exits zero",
		Lineage: findingLineageSwallowedError,
	}
	blindSpot := adversarialFinding{
		Kind: findingBlindSpot, Where: "evaluate",
		What:    "is never given an empty expression",
		Lineage: findingLineageSynthesizedCase,
	}

	if got := scopeForFindings([]adversarialFinding{defect}); got != editAnything {
		t.Errorf("a round fixing a production defect may only edit %s", got)
	}
	if got := scopeForFindings([]adversarialFinding{blindSpot}); got != editTestsOnly {
		t.Errorf("a round adding a missing test may edit %s", got)
	}
	// A defect anywhere in the set opens production, because that is what a
	// production defect is.
	mixed := []adversarialFinding{blindSpot, defect}
	if got := scopeForFindings(mixed); got != editAnything {
		t.Errorf("a round holding a defect was restricted to %s", got)
	}
}

// TestARoundTakesTwoRelatedFindings covers the bisection rule.
//
// A round given ten findings makes ten changes and verifies once, so a failure
// afterwards says only that one of ten was wrong.
func TestARoundTakesTwoRelatedFindings(t *testing.T) {
	findings := []adversarialFinding{
		{Where: "evaluate", What: "a", Lineage: findingLineageSynthesizedCase},
		{Where: "main", What: "b", Lineage: findingLineageMutationSurvivor},
		{Where: "main", What: "c", Lineage: findingLineageSwallowedError},
		{Where: "parse", What: "d", Lineage: findingLineageBoundary},
	}
	selected, remaining := selectRoundFindings(findings)
	if len(selected) != maximumFindingsPerRound {
		t.Fatalf("%d finding(s) selected, wanted %d",
			len(selected), maximumFindingsPerRound)
	}
	// The measured defect leads, and its companion shares its subject: fixing
	// two things in one function is one edit; fixing one here and one there is
	// two, and the second undoes the verification the first earned.
	if selected[0].Lineage != findingLineageMutationSurvivor {
		t.Errorf("the round leads with %s rather than the measured defect",
			selected[0].Lineage)
	}
	for _, finding := range selected {
		if finding.Where != "main" {
			t.Errorf("the round mixes subjects: %s alongside main", finding.Where)
		}
	}
	if len(remaining) != 2 {
		t.Errorf("%d finding(s) left open, wanted 2", len(remaining))
	}
}

// TestAFindingAddressedTwiceStopsBeingAsked keeps a run from resending the same
// prose and hoping.
func TestAFindingAddressedTwiceStopsBeingAsked(t *testing.T) {
	finding := adversarialFinding{
		Where: "main", What: "calls run, which can fail",
		Lineage: findingLineageSwallowedError,
	}
	ledger := newFindingLedger()

	workable, exhausted := ledger.reconcile([]adversarialFinding{finding})
	if len(workable) != 1 || len(exhausted) != 0 {
		t.Fatalf("first pass: %d workable, %d exhausted",
			len(workable), len(exhausted))
	}
	ledger.record(workable)

	workable, exhausted = ledger.reconcile([]adversarialFinding{finding})
	if len(workable) != 1 {
		t.Fatalf("second pass dropped a finding tried only once")
	}
	ledger.record(workable)

	workable, exhausted = ledger.reconcile([]adversarialFinding{finding})
	if len(workable) != 0 || len(exhausted) != 1 {
		t.Errorf("third pass: %d workable, %d exhausted — a finding addressed "+
			"twice and still raised is not going to be fixed by a fourth "+
			"attempt", len(workable), len(exhausted))
	}
}

// TestAFindingThatStopsBeingRaisedIsVerified is the other half: progress has to
// be observable, or nothing can tell fixed from forgotten.
func TestAFindingThatStopsBeingRaisedIsVerified(t *testing.T) {
	finding := adversarialFinding{
		Where: "main", What: "a defect", Lineage: findingLineageSwallowedError,
	}
	ledger := newFindingLedger()
	workable, _ := ledger.reconcile([]adversarialFinding{finding})
	ledger.record(workable)

	// The next review does not raise it.
	if _, _ = ledger.reconcile(nil); len(ledger.verified) != 1 {
		t.Errorf("%d finding(s) verified after one stopped being raised",
			len(ledger.verified))
	}
}

// TestTheSuiteIsWithheldWhenNothingChanged covers the premature test run.
//
// A round that has changed nothing would learn nothing from another run, and
// the model cannot see that: it re-ran the same command against the same bytes
// and each run looked to it like a step forward. Withholding the tool is more
// honest than letting it be called and explaining afterwards that the answer
// was already known.
func TestTheSuiteIsWithheldWhenNothingChanged(t *testing.T) {
	offers := func(tools []agentloop.ApprovedTool) bool {
		for _, tool := range tools {
			if tool.Descriptor.Name == executor.ToolTest {
				return true
			}
		}
		return false
	}
	if !offers(agentApprovedTools(true)) {
		t.Error("a round where something may have changed was not offered the suite")
	}
	if offers(agentApprovedTools(false)) {
		t.Error("a round that changed nothing was offered the suite anyway")
	}
	// The write tools are offered either way: a round that cannot test can
	// still be a round that writes, and withholding those would leave it
	// nothing to do at all.
	for _, offered := range [][]agentloop.ApprovedTool{
		agentApprovedTools(true), agentApprovedTools(false),
	} {
		if len(offered) == 0 {
			t.Fatal("a round was offered no tools at all")
		}
	}
}
