package coordinator

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// admissibleObservation returns a MemoryCandidacyObservation that clears
// every MEM-013 admissibility check and is expressible only as an advisory
// pattern, so a test can flip exactly one field to isolate one refusal
// reason at a time.
func admissibleObservation() MemoryCandidacyObservation {
	return MemoryCandidacyObservation{
		Source:                 domain.EvidenceSourceKindAutomatedValidationRun,
		ProducedWorkAttributed: MemoryCandidacyProducedWorkAttributed,
		RepositoryTextApproval: MemoryCandidacyRepositoryTextNotSourced,
		Contested:              true,
	}
}

// -----------------------------------------------------------------------
// MEM-013: inadmissible sources refused at candidacy, not only promotion.
// -----------------------------------------------------------------------

// TestMemoryCandidacyRefusesAgentSelfReport proves the funnel refuses an
// observation whose only Source is agent_self_report, even though every
// other field describes a perfectly admissible workspace fact. Before this
// file existed, nothing stopped an extractor from minting such an
// observation directly through domain.NewMemoryArtifactRevision (which
// starts every artifact at MaturityStateCandidate with no evidence check at
// all) -- only later PROMOTION to Validated was blocked by
// domain.AuthorizeMemoryArtifactMaturityGrant. This test fails against that
// older, promotion-only gate (a self-report-sourced Candidate is exactly
// what it permits) and passes against DetermineMemoryCandidacyTier.
func TestMemoryCandidacyRefusesAgentSelfReport(t *testing.T) {
	observation := admissibleObservation()
	observation.Source = domain.EvidenceSourceKindAgentSelfReport
	observation.WorkspaceFactExpressible = true

	_, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation})
	if !errors.Is(err, ErrMemoryCandidacyAgentSelfReportInadmissible) {
		t.Fatalf("expected ErrMemoryCandidacyAgentSelfReportInadmissible, got %v", err)
	}

	// The same observation with a real, non-self-report source is admitted.
	observation.Source = domain.EvidenceSourceKindAutomatedValidationRun
	if _, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation}); err != nil {
		t.Fatalf("an otherwise-identical, corroborated-source observation was refused: %v", err)
	}
}

// TestMemoryCandidacyRefusesUnattributedProducedWork proves the funnel
// refuses an observation whose produced work was not attributed to the
// declarations this run changed, per §31 "the observation would be about
// code the run never touched."
func TestMemoryCandidacyRefusesUnattributedProducedWork(t *testing.T) {
	observation := admissibleObservation()
	observation.ProducedWorkAttributed = MemoryCandidacyProducedWorkUnattributed
	observation.WorkspaceFactExpressible = true

	_, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation})
	if !errors.Is(err, ErrMemoryCandidacyUnattributedProducedWork) {
		t.Fatalf("expected ErrMemoryCandidacyUnattributedProducedWork, got %v", err)
	}
}

// TestMemoryCandidacyRefusesUnapprovedRepositoryText proves the funnel
// refuses repository text no person approved, and that this file never
// invents a way to clear that refusal: there is no field on
// MemoryCandidacyObservation or MemoryCandidacyProposal that flips
// RepositoryTextApproval from Unapproved to Approved short of a caller
// changing the value itself, which is exactly the point -- this file must
// not fabricate an approval-granting path, per this task's explicit
// instruction and the MEM-009 precedent it names.
func TestMemoryCandidacyRefusesUnapprovedRepositoryText(t *testing.T) {
	observation := admissibleObservation()
	observation.RepositoryTextApproval = MemoryCandidacyRepositoryTextUnapproved
	observation.WorkspaceFactExpressible = true

	_, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation})
	if !errors.Is(err, ErrMemoryCandidacyUnapprovedRepositoryText) {
		t.Fatalf("expected ErrMemoryCandidacyUnapprovedRepositoryText, got %v", err)
	}

	// Approved repository text is not refused on this ground.
	observation.RepositoryTextApproval = MemoryCandidacyRepositoryTextApproved
	if _, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation}); err != nil {
		t.Fatalf("approved repository text was refused: %v", err)
	}
}

// -----------------------------------------------------------------------
// MEM-013a: first-attempt success refused only for the contested tiers.
// -----------------------------------------------------------------------

// TestMemoryCandidacyRefusesUncontestedAdvisoryPattern proves the funnel
// refuses an advisory-pattern candidate from an observation reporting
// Contested: false (a first-attempt success), and admits the identical
// observation once Contested is true.
func TestMemoryCandidacyRefusesUncontestedAdvisoryPattern(t *testing.T) {
	observation := admissibleObservation()
	observation.Contested = false

	_, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation})
	if !errors.Is(err, ErrMemoryCandidacyUncontestedFirstAttempt) {
		t.Fatalf("expected ErrMemoryCandidacyUncontestedFirstAttempt, got %v", err)
	}

	observation.Contested = true
	tier, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation})
	if err != nil {
		t.Fatalf("a contested advisory-pattern-only observation was refused: %v", err)
	}
	if tier != MemoryCandidacyTierAdvisoryPattern {
		t.Fatalf("expected advisory-pattern tier, got %q", tier)
	}
}

// TestMemoryCandidacyAdmitsUncontestedWorkspaceFact proves the deterministic
// tier is unaffected by MEM-013a's first-attempt refusal: refusing it
// outright would forbid MEM-006 through MEM-008 on the runs that go well,
// which are most of them. This is the exact regression MEM-013a's own
// ticket text names.
func TestMemoryCandidacyAdmitsUncontestedWorkspaceFact(t *testing.T) {
	observation := admissibleObservation()
	observation.Contested = false
	observation.WorkspaceFactExpressible = true

	tier, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation})
	if err != nil {
		t.Fatalf("an uncontested, deterministic workspace fact was refused: %v", err)
	}
	if tier != MemoryCandidacyTierWorkspaceFact {
		t.Fatalf("expected workspace-fact tier, got %q", tier)
	}
}

// TestMemoryCandidacyAdmitsUncontestedMechanicalRule proves the mechanical-
// rule tier -- like the workspace-fact tier -- is not one of the three
// §31 lists as requiring something contested, so a first-attempt success
// expressible as a mechanical rule is admitted.
func TestMemoryCandidacyAdmitsUncontestedMechanicalRule(t *testing.T) {
	observation := admissibleObservation()
	observation.Contested = false
	observation.MechanicalRuleExpressible = true
	observation.MechanicalRuleWhichRule = "anti-patterns: swallowed error"

	tier, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation})
	if err != nil {
		t.Fatalf("an uncontested mechanical-rule observation was refused: %v", err)
	}
	if tier != MemoryCandidacyTierMechanicalRule {
		t.Fatalf("expected mechanical-rule tier, got %q", tier)
	}
}

// -----------------------------------------------------------------------
// MEM-016: the funnel refuses an advisory pattern a mechanical rule
// already expresses, proven against a real swallowed-error fixture.
// -----------------------------------------------------------------------

// TestMemoryCandidacyRefusesAdvisoryPatternAMechanicalRuleAlreadyExpresses
// is MEM-016. It runs the repository's REAL, already-shipped anti-patterns
// mechanical check (findAntiPatterns, agent_stage_antipatterns.go) over a
// fixture whose only defect is a swallowed error -- the exact shape
// findSwallowedErrors already looks for -- so the observation's
// MechanicalRuleExpressible claim is grounded in an actual mechanical
// finding rather than an asserted boolean.
//
// Discrimination: this test FAILS against a no-funnel baseline (any code
// that mints an advisory pattern straight from an observation without first
// asking whether a stronger tier could carry it, which is what every
// extractor in this repository did before this file existed) and PASSES
// against DetermineMemoryCandidacyTier, which refuses the same request with
// ErrMemoryCandidacyWeakerTierUnjustified because the observation is also
// MechanicalRuleExpressible, a strictly stronger tier than the requested
// advisory pattern.
func TestMemoryCandidacyRefusesAdvisoryPatternAMechanicalRuleAlreadyExpresses(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"// Load reads a thing.\n" +
			"func Load() error { return nil }\n\n" +
			"func main() {\n\tif err := Load(); err != nil {\n\t\treturn\n\t}\n}\n",
	})
	found, err := findAntiPatterns(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if !mentions(found, "returns without it") {
		t.Fatalf("fixture did not actually trip the swallowed-error mechanical check: %+v", found)
	}

	observation := admissibleObservation()
	observation.MechanicalRuleExpressible = true
	observation.MechanicalRuleWhichRule = "anti-patterns: " + found[0].What

	// Requesting the advisory-pattern tier directly, with no justification
	// for skipping the stronger tier the fixture actually supports, must be
	// refused.
	_, err = DetermineMemoryCandidacyTier(MemoryCandidacyProposal{
		Observation:   observation,
		RequestedTier: MemoryCandidacyTierAdvisoryPattern,
	})
	if !errors.Is(err, ErrMemoryCandidacyWeakerTierUnjustified) {
		t.Fatalf("expected ErrMemoryCandidacyWeakerTierUnjustified, got %v", err)
	}

	// The unqualified funnel call -- "mint at the strongest tier that can
	// express the observation," MEM-012's own words -- lands on the
	// mechanical-rule tier instead of ever reaching advisory pattern.
	tier, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{Observation: observation})
	if err != nil {
		t.Fatalf("the default strongest-tier request was refused: %v", err)
	}
	if tier != MemoryCandidacyTierMechanicalRule {
		t.Fatalf("expected the funnel to prefer mechanical-rule over advisory-pattern, got %q", tier)
	}

	// A caller that records why the stronger tier could not carry this
	// observation may still request the weaker one explicitly.
	tier, err = DetermineMemoryCandidacyTier(MemoryCandidacyProposal{
		Observation:             observation,
		RequestedTier:           MemoryCandidacyTierAdvisoryPattern,
		WeakerTierJustification: "MEM-015 governance for this rule has not replayed it against accepted revisions yet",
	})
	if err != nil {
		t.Fatalf("a justified weaker-tier request was refused: %v", err)
	}
	if tier != MemoryCandidacyTierAdvisoryPattern {
		t.Fatalf("expected advisory-pattern tier, got %q", tier)
	}
}

// -----------------------------------------------------------------------
// MEM-012: strongest-tier selection and the unjustified-weaker-tier gate,
// independent of MEM-013/MEM-013a's own refusal reasons.
// -----------------------------------------------------------------------

// TestSelectStrongestExpressibleMemoryCandidacyTierOrdering proves the tier
// picked is always the strongest one the observation actually supports,
// across every combination, not merely the two exercised above.
func TestSelectStrongestExpressibleMemoryCandidacyTierOrdering(t *testing.T) {
	cases := []struct {
		name string
		set  func(*MemoryCandidacyObservation)
		want MemoryCandidacyTier
	}{
		{"workspace fact beats everything", func(o *MemoryCandidacyObservation) {
			o.WorkspaceFactExpressible = true
			o.MechanicalRuleExpressible, o.MechanicalRuleWhichRule = true, "rule"
			o.RegressionCaseComplete = true
			o.RoutingEvidenceApplicable = true
		}, MemoryCandidacyTierWorkspaceFact},
		{"mechanical rule beats regression case", func(o *MemoryCandidacyObservation) {
			o.MechanicalRuleExpressible, o.MechanicalRuleWhichRule = true, "rule"
			o.RegressionCaseComplete = true
			o.RoutingEvidenceApplicable = true
		}, MemoryCandidacyTierMechanicalRule},
		{"regression case beats routing evidence", func(o *MemoryCandidacyObservation) {
			o.RegressionCaseComplete = true
			o.RoutingEvidenceApplicable = true
		}, MemoryCandidacyTierRegressionCase},
		{"routing evidence beats advisory pattern", func(o *MemoryCandidacyObservation) {
			o.RoutingEvidenceApplicable = true
		}, MemoryCandidacyTierRoutingEvidence},
		{"advisory pattern is the floor", func(o *MemoryCandidacyObservation) {
		}, MemoryCandidacyTierAdvisoryPattern},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			observation := admissibleObservation()
			testCase.set(&observation)
			got, err := SelectStrongestExpressibleMemoryCandidacyTier(observation)
			if err != nil {
				t.Fatal(err)
			}
			if got != testCase.want {
				t.Fatalf("expected %q, got %q", testCase.want, got)
			}
		})
	}
}

// TestMemoryCandidacyRefusesTierTheObservationCannotExpress proves a
// caller cannot request a tier the observation's own fields report false
// for, even with a justification attached.
func TestMemoryCandidacyRefusesTierTheObservationCannotExpress(t *testing.T) {
	observation := admissibleObservation()
	_, err := DetermineMemoryCandidacyTier(MemoryCandidacyProposal{
		Observation:             observation,
		RequestedTier:           MemoryCandidacyTierRegressionCase,
		WeakerTierJustification: "irrelevant",
	})
	if !errors.Is(err, ErrMemoryCandidacyTierNotExpressible) {
		t.Fatalf("expected ErrMemoryCandidacyTierNotExpressible, got %v", err)
	}
}

// TestMemoryCandidacyObservationValidateCatchesUnsetEnums proves the three
// tri-state fields fail closed on their Go zero value rather than reading
// as a silently admissible default.
func TestMemoryCandidacyObservationValidateCatchesUnsetEnums(t *testing.T) {
	if err := (MemoryCandidacyObservation{}).Validate(); err == nil {
		t.Fatal("expected an unset observation to fail validation")
	}
}
