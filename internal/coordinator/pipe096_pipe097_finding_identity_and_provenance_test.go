package coordinator

import "testing"

// --- PIPE-096: findings carry a stable proof-obligation identity. ---
// --- PIPE-097: findings carry evidence-level and lineage provenance. ---

// TestPIPE096_TheSameFindingGetsTheSameIDAcrossTwoReviewRuns is the
// discrimination proof for identity stability.
//
// Proven to discriminate: before this ticket, adversarialFinding carried no
// ID field at all -- there was nothing to compare. A finding produced by two
// independent runs of the same review over unchanged code must resolve to
// the same identity, or a later persistence layer (PIPE-098, not built by
// this lane) could never tell "the same weakness, still present" apart from
// "a new weakness that happens to look similar."
func TestPIPE096_TheSameFindingGetsTheSameIDAcrossTwoReviewRuns(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe096/stable\n\ngo 1.26.0\n",
		"lib.go": `package lib

import "os"

func WarmCache(path string) {
	os.Open(path)
}
`,
	})
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	first := findUnhandledFailures(functions)
	second := findUnhandledFailures(functions)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("want exactly one finding each run, got %d and %d",
			len(first), len(second))
	}
	// findUnhandledFailures itself does not assign IDs -- that happens in
	// reviewAdversariallyWithPlan's finalize step, so this test drives the
	// same computation the finalize step uses.
	firstID := findingObligationID(first[0])
	secondID := findingObligationID(second[0])
	if firstID == "" {
		t.Fatal("findingObligationID produced an empty identity")
	}
	if firstID != secondID {
		t.Errorf("the same weakness, found twice, got two different IDs: "+
			"%q vs %q", firstID, secondID)
	}
}

// TestPIPE096_DistinctFindingsGetDistinctIDs is the companion discrimination
// case: two findings differing in Where or What must not collide onto the
// same identity, or a discharge of one would incorrectly discharge the
// other.
func TestPIPE096_DistinctFindingsGetDistinctIDs(t *testing.T) {
	sameFunctionDifferentClaim := adversarialFinding{
		Kind: findingDefect, Where: "WarmCache", What: "calls os.Open",
	}
	differentFunctionSameClaim := adversarialFinding{
		Kind: findingDefect, Where: "LoadIndex", What: "calls os.Open",
	}
	base := adversarialFinding{
		Kind: findingDefect, Where: "WarmCache", What: "calls os.Open",
	}

	baseID := findingObligationID(base)
	if id := findingObligationID(sameFunctionDifferentClaim); id != baseID {
		t.Errorf("identical Kind/Where/What must hash identically: %q vs %q",
			id, baseID)
	}
	if id := findingObligationID(differentFunctionSameClaim); id == baseID {
		t.Errorf("a different Where collided onto the same ID: %q", id)
	}
	claimChanged := adversarialFinding{
		Kind: findingDefect, Where: "WarmCache", What: "calls os.ReadFile",
	}
	if id := findingObligationID(claimChanged); id == baseID {
		t.Errorf("a different What collided onto the same ID: %q", id)
	}
}

// TestPIPE097_ReviewAdversariallyAttachesProvenanceToEveryFinding proves
// every finding reviewAdversariallyWithPlan returns carries a non-empty ID,
// EvidenceLevel, and Lineage -- not merely the raw producer functions tested
// individually above, but the actual aggregation and finalize path a caller
// uses.
//
// Proven to discriminate: before this ticket, EvidenceLevel and Lineage did
// not exist on adversarialFinding, so every finding's provenance was
// unavailable by construction; reverting the finalize loop in
// reviewAdversariallyWithPlan (`findings[index].ID = findingObligationID(...)`)
// reproduces empty IDs immediately.
func TestPIPE097_ReviewAdversariallyAttachesProvenanceToEveryFinding(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/pipe097/provenance\n\ngo 1.26.0\n",
		"lib.go": `package lib

import "os"

func WarmCache(path string) {
	os.Open(path)
}
`,
	})
	execution := &AgentExecution{}
	findings, err := execution.reviewAdversariallyWithPlan(
		t.Context(), worktree, adversarialCheckPlan{MutationAnalysis: false})
	if err != nil {
		t.Fatalf("reviewAdversariallyWithPlan: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected at least the swallowed os.Open finding")
	}
	for _, finding := range findings {
		if finding.ID == "" {
			t.Errorf("finding %+v has no ID", finding)
		}
		if finding.EvidenceLevel == "" {
			t.Errorf("finding %+v has no EvidenceLevel", finding)
		}
		if finding.Lineage == "" {
			t.Errorf("finding %+v has no Lineage", finding)
		}
	}
}
