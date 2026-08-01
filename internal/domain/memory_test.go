package domain

import (
	"errors"
	"strings"
	"testing"
)

func mustProjectID(t *testing.T) ProjectID {
	t.Helper()
	id, err := NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRepositoryID(t *testing.T) RepositoryID {
	t.Helper()
	id, err := NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustEvidenceID(t *testing.T) EvidenceID {
	t.Helper()
	id, err := NewEvidenceID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustMemoryArtifactID(t *testing.T) MemoryArtifactID {
	t.Helper()
	id, err := NewMemoryArtifactID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustMemoryArtifactRevisionID(t *testing.T) MemoryArtifactRevisionID {
	t.Helper()
	id, err := NewMemoryArtifactRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustEpisodeID(t *testing.T) EpisodeID {
	t.Helper()
	id, err := NewEpisodeID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func validObservationContent(t *testing.T) MemoryArtifactContent {
	t.Helper()
	payload, err := NewObservationHypothesisContent(mustRepositoryID(t), "raw context growth correlates with slower repair rounds")
	if err != nil {
		t.Fatal(err)
	}
	content, err := NewObservationHypothesisMemoryContent(payload)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// -----------------------------------------------------------------------
// M21-011: agent self-report can never, by itself, authorize validated
// status, but its mere presence alongside real corroborated evidence does
// not veto that evidence either (adversarial-review defect 4: the plan's
// resolution is EXCLUDE, not POISON -- see AuthorizeMemoryArtifactMaturityGrant's
// doc comment and docs/plan.md §0 "agent self-report -> accepted outcome" /
// §31 evidence_strength: none).
// -----------------------------------------------------------------------

func TestAgentSelfReportEvidenceCanNeverAuthorizeValidatedOrPreferredStatus(t *testing.T) {
	selfReport := SupportingEvidenceRecord{
		Evidence: mustEvidenceID(t),
		Source:   EvidenceSourceKindAgentSelfReport,
		Strength: EvidenceStrengthNone,
	}
	corroborated := SupportingEvidenceRecord{
		Evidence: mustEvidenceID(t),
		Source:   EvidenceSourceKindAutomatedValidationRun,
		Strength: EvidenceStrengthCorroborated,
	}

	// Self-report alone, an empty set, or a real source that never reaches
	// corroborated strength can never produce a usable proof.
	rejected := [][]SupportingEvidenceRecord{
		{selfReport},
		nil,
		{{Evidence: mustEvidenceID(t), Source: EvidenceSourceKindAutomatedValidationRun, Strength: EvidenceStrengthNone}},
	}
	for index, evidence := range rejected {
		if _, err := AuthorizeMemoryArtifactMaturityGrant(mustMemoryArtifactRevisionID(t), evidence); err == nil {
			t.Fatalf("case %d: AuthorizeMemoryArtifactMaturityGrant(%#v) = nil error, want rejection", index, evidence)
		}
	}

	// EXCLUDE, not POISON: a self-report record sitting alongside genuine
	// corroborated evidence does not veto the grant. The plan's dependency
	// invariant prohibits self-report from CAUSING an outcome (M21-011:
	// "prohibit self-report from CREATING validated status"), not from
	// merely coexisting with evidence that legitimately does.
	revision := mustMemoryArtifactRevisionID(t)
	proof, err := AuthorizeMemoryArtifactMaturityGrant(revision, []SupportingEvidenceRecord{selfReport, corroborated})
	if err != nil {
		t.Fatalf("self-report alongside corroborated evidence: AuthorizeMemoryArtifactMaturityGrant = %v, want success (self-report contributes nothing but must not poison real evidence)", err)
	}
	if err := ValidateMemoryArtifactMaturityTransition(MaturityTransitionRequest{
		From: MaturityStateCandidate, To: MaturityStateValidated, Revision: revision, Proof: proof,
	}); err != nil {
		t.Fatalf("candidate -> validated using a proof earned alongside inert self-report evidence: %v", err)
	}

	// Even with a rejected/zero proof, the transition function must refuse
	// to grant authority: the zero-value authorityProof carried by a request
	// that never called AuthorizeMemoryArtifactMaturityGrant is exactly what
	// an external caller gets by default.
	var zeroProofRequest MaturityTransitionRequest
	zeroProofRequest.From = MaturityStateCandidate
	zeroProofRequest.To = MaturityStateValidated
	if err := ValidateMemoryArtifactMaturityTransition(zeroProofRequest); !errors.Is(err, ErrAuthorityProofRequired) {
		t.Fatalf("zero-proof transition error = %v, want ErrAuthorityProofRequired", err)
	}

	zeroProofRequest.To = MaturityStatePreferredForExperiment
	if err := ValidateMemoryArtifactMaturityTransition(zeroProofRequest); !errors.Is(err, ErrAuthorityProofRequired) {
		t.Fatalf("zero-proof preferred-for-experiment transition error = %v, want ErrAuthorityProofRequired", err)
	}
}

// TestAgentSelfReportOnlyEvidenceNeverAuthorizes is the narrow boundary case
// requested by the adversarial-review defect-4 fix: a set containing ONLY
// self-report evidence (no other record at all) must still fail, even
// though self-report no longer poisons a set that also contains real
// evidence.
func TestAgentSelfReportOnlyEvidenceNeverAuthorizes(t *testing.T) {
	evidence := []SupportingEvidenceRecord{
		{Evidence: mustEvidenceID(t), Source: EvidenceSourceKindAgentSelfReport, Strength: EvidenceStrengthNone},
		{Evidence: mustEvidenceID(t), Source: EvidenceSourceKindAgentSelfReport, Strength: EvidenceStrengthNone},
	}
	if _, err := AuthorizeMemoryArtifactMaturityGrant(mustMemoryArtifactRevisionID(t), evidence); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("self-report-only evidence error = %v, want ErrInvalidDomainValue", err)
	}
}

func TestSupportingEvidenceRecordRejectsSelfReportWithNonNoneStrength(t *testing.T) {
	record := SupportingEvidenceRecord{
		Evidence: mustEvidenceID(t),
		Source:   EvidenceSourceKindAgentSelfReport,
		Strength: EvidenceStrengthCorroborated,
	}
	if err := record.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
	}
	record.Strength = EvidenceStrengthNone
	if err := record.Validate(); err != nil {
		t.Fatalf("self-report at evidence_strength none: %v", err)
	}
}

func TestAuthorizedGrantReachesValidatedAndPreferredForExperiment(t *testing.T) {
	revision := mustMemoryArtifactRevisionID(t)
	evidence := []SupportingEvidenceRecord{
		{Evidence: mustEvidenceID(t), Source: EvidenceSourceKindAutomatedValidationRun, Strength: EvidenceStrengthCorroborated},
	}
	proof, err := AuthorizeMemoryArtifactMaturityGrant(revision, evidence)
	if err != nil {
		t.Fatal(err)
	}
	for _, to := range []MaturityState{MaturityStateValidated, MaturityStatePreferredForExperiment} {
		if err := ValidateMemoryArtifactMaturityTransition(MaturityTransitionRequest{
			From:     MaturityStateCandidate,
			To:       to,
			Revision: revision,
			Proof:    proof,
		}); err != nil {
			t.Fatalf("candidate -> %s with authorized proof: %v", to, err)
		}
	}
}

// -----------------------------------------------------------------------
// M21-009: observation/hypothesis always carries evidence_strength none.
// -----------------------------------------------------------------------

func TestObservationHypothesisContentAlwaysCarriesEvidenceStrengthNone(t *testing.T) {
	repository := mustRepositoryID(t)
	content, err := NewObservationHypothesisContent(repository, "agents that skim the diff summary seem to skip repair rounds more often")
	if err != nil {
		t.Fatal(err)
	}
	if content.Strength != EvidenceStrengthNone {
		t.Fatalf("Strength = %v, want EvidenceStrengthNone", content.Strength)
	}

	forged := content
	forged.Strength = EvidenceStrengthCorroborated
	if err := forged.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("forged non-none strength error = %v, want ErrInvalidDomainValue", err)
	}
}

// -----------------------------------------------------------------------
// M21-010: maturity is a validated transition machine, not a bare enum.
// -----------------------------------------------------------------------

func TestEveryAllowedMaturityTransitionPasses(t *testing.T) {
	revision := mustMemoryArtifactRevisionID(t)
	evidence := []SupportingEvidenceRecord{
		{Evidence: mustEvidenceID(t), Source: EvidenceSourceKindIndependentReplayComparison, Strength: EvidenceStrengthCorroborated},
	}
	proof, err := AuthorizeMemoryArtifactMaturityGrant(revision, evidence)
	if err != nil {
		t.Fatal(err)
	}
	assertAllowedTransitions(t, "memory artifact maturity", maturityTransitions, func(from, to MaturityState) error {
		request := MaturityTransitionRequest{From: from, To: to}
		if to.IsAuthorityBearing() {
			request.Revision = revision
			request.Proof = proof
		}
		return ValidateMemoryArtifactMaturityTransition(request)
	})
}

func TestRepresentativeForbiddenMaturityTransitionsReturnTypedErrors(t *testing.T) {
	cases := []struct {
		name string
		from MaturityState
		to   MaturityState
	}{
		{name: "invalidated is fully terminal", from: MaturityStateInvalidated, to: MaturityStateCandidate},
		{name: "retired is fully terminal", from: MaturityStateRetired, to: MaturityStateValidated},
		{name: "quarantined cannot become candidate", from: MaturityStateQuarantined, to: MaturityStateCandidate},
		{name: "quarantined cannot become validated", from: MaturityStateQuarantined, to: MaturityStateValidated},
		{name: "quarantined cannot become preferred-for-experiment", from: MaturityStateQuarantined, to: MaturityStatePreferredForExperiment},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateMemoryArtifactMaturityTransition(MaturityTransitionRequest{From: test.from, To: test.to})
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("error = %v, want ErrInvalidTransition", err)
			}
		})
	}
}

func TestUnknownMaturityStatesReturnTypedErrors(t *testing.T) {
	err := ValidateMemoryArtifactMaturityTransition(MaturityTransitionRequest{
		From: MaturityState("invented"),
		To:   MaturityStateCandidate,
	})
	if !errors.Is(err, ErrUnknownState) {
		t.Fatalf("error = %v, want ErrUnknownState", err)
	}
}

// TestQuarantineIsTerminalForAuthority is the boundary test for the plan's
// explicit rule: "Quarantine is terminal for the exposed version: a failed
// artifact is never repaired in place or restored with inherited authority;
// a successor gets new identity and no inherited confidence." No amount of
// evidence, even a maximally strong authorized proof, can move a quarantined
// artifact back into an authority-bearing state.
func TestQuarantineIsTerminalForAuthority(t *testing.T) {
	if MaturityStateQuarantined.CanReachAuthority() {
		t.Fatal("MaturityStateQuarantined.CanReachAuthority() = true, want false")
	}
	if MaturityStateInvalidated.CanReachAuthority() || MaturityStateRetired.CanReachAuthority() {
		t.Fatal("invalidated/retired must not be able to reach authority")
	}
	if !MaturityStateCandidate.CanReachAuthority() || !MaturityStateValidated.CanReachAuthority() || !MaturityStatePreferredForExperiment.CanReachAuthority() {
		t.Fatal("candidate/validated/preferred-for-experiment must retain a path to authority")
	}

	revision := mustMemoryArtifactRevisionID(t)
	evidence := []SupportingEvidenceRecord{
		{Evidence: mustEvidenceID(t), Source: EvidenceSourceKindRegressionOracleRun, Strength: EvidenceStrengthCorroborated},
	}
	proof, err := AuthorizeMemoryArtifactMaturityGrant(revision, evidence)
	if err != nil {
		t.Fatal(err)
	}
	for _, to := range []MaturityState{MaturityStateCandidate, MaturityStateValidated, MaturityStatePreferredForExperiment} {
		err := ValidateMemoryArtifactMaturityTransition(MaturityTransitionRequest{
			From:     MaturityStateQuarantined,
			To:       to,
			Revision: revision,
			Proof:    proof,
		})
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("quarantined -> %s (even with an authorized proof) error = %v, want ErrInvalidTransition", to, err)
		}
	}
}

func TestTransitionMemoryArtifactMaturityUpdatesRevision(t *testing.T) {
	revision, err := NewMemoryArtifactRevision(mustMemoryArtifactID(t), mustMemoryArtifactRevisionID(t), MemoryProjectScope{Project: mustProjectID(t)}, validObservationContent(t))
	if err != nil {
		t.Fatal(err)
	}
	if revision.Maturity != MaturityStateCandidate {
		t.Fatalf("initial maturity = %v, want candidate", revision.Maturity)
	}

	proof, err := AuthorizeMemoryArtifactMaturityGrant(revision.RevisionID, []SupportingEvidenceRecord{
		{Evidence: mustEvidenceID(t), Source: EvidenceSourceKindUserApproval, Strength: EvidenceStrengthCorroborated},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := TransitionMemoryArtifactMaturity(revision, MaturityStateValidated, proof)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Maturity != MaturityStateValidated {
		t.Fatalf("maturity = %v, want validated", updated.Maturity)
	}
	if revision.Maturity != MaturityStateCandidate {
		t.Fatal("TransitionMemoryArtifactMaturity mutated the original revision")
	}

	if _, err := TransitionMemoryArtifactMaturity(revision, MaturityStateValidated, authorityProof{}); !errors.Is(err, ErrAuthorityProofRequired) {
		t.Fatalf("unauthorized transition error = %v, want ErrAuthorityProofRequired", err)
	}
}

// TestAuthorityProofCannotBeReplayedAgainstAnUnrelatedRevision is the
// adversarial-review defect-2 regression: a proof legitimately earned from
// artifact A's evidence must never promote an unrelated artifact B. Before
// the fix, authorityProof carried no artifact/revision identity at all, so
// any non-empty proof satisfied ValidateMemoryArtifactMaturityTransition for
// every revision.
func TestAuthorityProofCannotBeReplayedAgainstAnUnrelatedRevision(t *testing.T) {
	revisionA, err := NewMemoryArtifactRevision(mustMemoryArtifactID(t), mustMemoryArtifactRevisionID(t), MemoryProjectScope{Project: mustProjectID(t)}, validObservationContent(t))
	if err != nil {
		t.Fatal(err)
	}
	revisionB, err := NewMemoryArtifactRevision(mustMemoryArtifactID(t), mustMemoryArtifactRevisionID(t), MemoryProjectScope{Project: mustProjectID(t)}, validObservationContent(t))
	if err != nil {
		t.Fatal(err)
	}

	// Evidence legitimately about A only.
	proofForA, err := AuthorizeMemoryArtifactMaturityGrant(revisionA.RevisionID, []SupportingEvidenceRecord{
		{Evidence: mustEvidenceID(t), Source: EvidenceSourceKindAutomatedValidationRun, Strength: EvidenceStrengthCorroborated},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Replaying A's proof against completely unrelated revision B must fail.
	if _, err := TransitionMemoryArtifactMaturity(revisionB, MaturityStateValidated, proofForA); !errors.Is(err, ErrAuthorityProofMismatch) {
		t.Fatalf("replaying A's proof against B: error = %v, want ErrAuthorityProofMismatch", err)
	}
	if err := ValidateMemoryArtifactMaturityTransition(MaturityTransitionRequest{
		From: MaturityStateCandidate, To: MaturityStatePreferredForExperiment, Revision: revisionB.RevisionID, Proof: proofForA,
	}); !errors.Is(err, ErrAuthorityProofMismatch) {
		t.Fatalf("replaying A's proof against B for preferred-for-experiment: error = %v, want ErrAuthorityProofMismatch", err)
	}

	// The same proof legitimately applied to A (the revision it was earned
	// for) must still succeed.
	if _, err := TransitionMemoryArtifactMaturity(revisionA, MaturityStateValidated, proofForA); err != nil {
		t.Fatalf("legitimate use of A's own proof against A: %v", err)
	}
}

// -----------------------------------------------------------------------
// M21-002..008: artifact-kind content types.
// -----------------------------------------------------------------------

func TestMemoryArtifactContentRequiresExactlyOneMatchingPayload(t *testing.T) {
	repository := mustRepositoryID(t)
	fact := RepositoryFactContent{
		Repository: repository,
		Category:   RepositoryFactCategoryBuildCommand,
		Statement:  "the module builds with `go build ./...`",
		Binding:    RevisionBinding{Known: true, ExactRevision: "deadbeef"},
	}
	command := ReviewedCommandContent{
		Repository:       repository,
		Purpose:          CommandPurposeBuild,
		Argv:             []string{"go", "build", "./..."},
		WorkingDirectory: ".",
		Binding:          RevisionBinding{Known: true, ExactRevision: "deadbeef"},
	}

	valid, err := NewRepositoryFactMemoryContent(fact)
	if err != nil {
		t.Fatal(err)
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("well-formed content: %v", err)
	}

	// Two populated payloads, even if each is individually valid, is a
	// structurally inconsistent tagged union.
	inconsistent := valid
	inconsistent.ReviewedCommand = &command
	if err := inconsistent.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("two-payload error = %v, want ErrInvalidDomainValue", err)
	}

	// Kind that disagrees with the populated payload.
	mismatched := MemoryArtifactContent{Kind: MemoryArtifactKindReviewedCommand, RepositoryFact: &fact}
	if err := mismatched.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("mismatched-kind error = %v, want ErrInvalidDomainValue", err)
	}

	// No payload at all.
	empty := MemoryArtifactContent{Kind: MemoryArtifactKindRepositoryFact}
	if err := empty.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("empty-payload error = %v, want ErrInvalidDomainValue", err)
	}
}

func TestArtifactKindContentValidationBoundaryCases(t *testing.T) {
	repository := mustRepositoryID(t)
	knownBinding := RevisionBinding{Known: true, ExactRevision: "deadbeef"}

	t.Run("repository fact rejects blank statement", func(t *testing.T) {
		_, err := NewRepositoryFactMemoryContent(RepositoryFactContent{
			Repository: repository,
			Category:   RepositoryFactCategoryTopology,
			Statement:  "   ",
			Binding:    knownBinding,
		})
		if !errors.Is(err, ErrInvalidDomainValue) {
			t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
		}
	})

	t.Run("reviewed command rejects empty argv", func(t *testing.T) {
		_, err := NewReviewedCommandMemoryContent(ReviewedCommandContent{
			Repository: repository,
			Purpose:    CommandPurposeTest,
			Binding:    knownBinding,
		})
		if !errors.Is(err, ErrInvalidDomainValue) {
			t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
		}
	})

	t.Run("file-to-test mapping requires at least one test path", func(t *testing.T) {
		_, err := NewFileToTestMappingMemoryContent(FileToTestMappingContent{
			Repository: repository,
			SourcePath: "internal/domain/memory.go",
			Binding:    knownBinding,
		})
		if !errors.Is(err, ErrInvalidDomainValue) {
			t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
		}
	})

	t.Run("repository convention requires scope", func(t *testing.T) {
		_, err := NewRepositoryConventionMemoryContent(RepositoryConventionContent{
			Repository: repository,
			Statement:  "keep exported domain types alongside their transition validators",
			Binding:    knownBinding,
		})
		if !errors.Is(err, ErrInvalidDomainValue) {
			t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
		}
	})

	t.Run("accepted regression case requires demonstrated-failure evidence", func(t *testing.T) {
		_, err := NewAcceptedRegressionCaseMemoryContent(AcceptedRegressionCaseContent{
			Repository:        repository,
			Classification:    RegressionClassificationSemantic,
			ReproducibleInput: "fixtures/negative_money_overflow.json",
			Oracle:            "internal/domain/policy_test.go:TestMoneyAddOverflow",
			ExpectedOutcome:   "Add returns ValueError instead of wrapping",
			Binding:           knownBinding,
		})
		if !errors.Is(err, ErrInvalidDomainValue) {
			t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
		}
	})

	t.Run("execution recipe validates observed performance shape", func(t *testing.T) {
		_, err := NewExecutionRecipeMemoryContent(ExecutionRecipeContent{
			Repository:                repository,
			ApplicabilityStatement:    "task touches internal/domain and requires no SQLite access",
			PlanSummary:               "edit typed values, run go test ./internal/domain/...",
			RequiredValidationProfile: "domain-fast",
			ObservedPerformance: ForecastRange{
				LatencyKnown:     true,
				LatencyP90Millis: -1,
			},
		})
		if !errors.Is(err, ErrInvalidDomainValue) {
			t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
		}
	})

	t.Run("executable atom reference requires an atom identity", func(t *testing.T) {
		_, err := NewExecutableAtomReferenceMemoryContent(ExecutableAtomReferenceContent{
			Repository:    repository,
			CanonicalName: "ValidateTaskBudgetBeforeModelRequest",
			Binding:       knownBinding,
		})
		if !errors.Is(err, ErrInvalidDomainValue) {
			t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
		}
	})
}

// TestRevisionBindingRejectsOversizedFields is the adversarial-review
// defect-5 regression: RevisionBinding's free-form revision-identifier
// strings had no length bound, unlike every peer identity/binding field
// elsewhere in the codebase (internal/fingerprint.MaximumBindingFieldLength,
// the "repository revision" bound in internal/forecast). A caller-supplied
// value far larger than any real revision identifier -- the review's
// reproduction used 960,000 bytes -- was accepted and hashed into the
// artifact's content digest.
func TestRevisionBindingRejectsOversizedFields(t *testing.T) {
	oversized := strings.Repeat("a", MaximumRevisionFieldBytes+1)

	cases := map[string]RevisionBinding{
		"exact_revision":       {Known: true, ExactRevision: oversized},
		"valid_from_revision":  {Known: true, ValidFromRevision: oversized},
		"valid_until_revision": {Known: true, ValidFromRevision: "deadbeef", ValidUntilRevision: oversized},
		"unknown_reason":       {Known: false, UnknownReason: oversized},
	}
	for name, binding := range cases {
		t.Run(name, func(t *testing.T) {
			if err := binding.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
				t.Fatalf("%s = %d bytes: error = %v, want ErrInvalidDomainValue", name, len(oversized), err)
			}
		})
	}

	// A field at exactly the bound is still accepted.
	atBound := strings.Repeat("a", MaximumRevisionFieldBytes)
	if err := (RevisionBinding{Known: true, ExactRevision: atBound}).Validate(); err != nil {
		t.Fatalf("exact_revision at exactly %d bytes: %v", MaximumRevisionFieldBytes, err)
	}
}

// -----------------------------------------------------------------------
// M21-012: project ownership and cross-project isolation.
// -----------------------------------------------------------------------

func TestMemoryQueryProjectBoundaryRejectsCrossProjectArtifacts(t *testing.T) {
	projectA := mustProjectID(t)
	projectB := mustProjectID(t)
	repository := mustRepositoryID(t)

	boundary := MemoryQueryProjectBoundary{Project: projectA}
	sameProject := MemoryProjectScope{Project: projectA, Repository: repository}
	otherProject := MemoryProjectScope{Project: projectB, Repository: repository}

	if !boundary.Allows(sameProject) {
		t.Fatal("boundary rejected an artifact from its own project")
	}
	if boundary.Allows(otherProject) {
		t.Fatal("boundary allowed an artifact from a different project")
	}
	if err := RequireMemoryQueryProjectBoundary(boundary, otherProject); !errors.Is(err, ErrCrossProjectMemoryAccess) {
		t.Fatalf("error = %v, want ErrCrossProjectMemoryAccess", err)
	}
	if err := RequireMemoryQueryProjectBoundary(boundary, sameProject); err != nil {
		t.Fatalf("same-project boundary check: %v", err)
	}
}

func TestMemoryQueryProjectBoundaryRepositoryFilterIsExclusive(t *testing.T) {
	project := mustProjectID(t)
	included := mustRepositoryID(t)
	excluded := mustRepositoryID(t)

	boundary := MemoryQueryProjectBoundary{Project: project, IncludeRepositories: []RepositoryID{included}}
	if !boundary.Allows(MemoryProjectScope{Project: project, Repository: included}) {
		t.Fatal("boundary rejected an included repository")
	}
	if boundary.Allows(MemoryProjectScope{Project: project, Repository: excluded}) {
		t.Fatal("boundary allowed a repository outside its include list")
	}
}

func TestMemoryQueryProjectBoundaryValidateRejectsEmptyProject(t *testing.T) {
	if err := (MemoryQueryProjectBoundary{}).Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
	}
}

// TestMemoryQueryProjectBoundaryAllowsFailsClosedOnZeroProject is the
// adversarial-review defect-6 regression: a zero-value MemoryQueryProjectBoundary
// and a zero-value MemoryProjectScope compare Project == Project trivially
// (both zero), so Allows must not treat that as a match. Fail closed, not
// open, on an absent project on either side.
func TestMemoryQueryProjectBoundaryAllowsFailsClosedOnZeroProject(t *testing.T) {
	if (MemoryQueryProjectBoundary{}).Allows(MemoryProjectScope{}) {
		t.Fatal("zero-value boundary allowed a zero-value scope; must fail closed")
	}
	realProject := mustProjectID(t)
	if (MemoryQueryProjectBoundary{Project: realProject}).Allows(MemoryProjectScope{}) {
		t.Fatal("real boundary allowed a zero-project scope; must fail closed")
	}
	if (MemoryQueryProjectBoundary{}).Allows(MemoryProjectScope{Project: realProject}) {
		t.Fatal("zero-value boundary allowed a real-project scope; must fail closed")
	}
}

// -----------------------------------------------------------------------
// M21-013: correction, deletion preview, and export.
// -----------------------------------------------------------------------

func TestReviseMemoryArtifactWithUserCorrectionCreatesNewRevisionAndResetsMaturity(t *testing.T) {
	artifact := mustMemoryArtifactID(t)
	firstRevision := mustMemoryArtifactRevisionID(t)
	scope := MemoryProjectScope{Project: mustProjectID(t)}

	revision, err := NewMemoryArtifactRevision(artifact, firstRevision, scope, validObservationContent(t))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := AuthorizeMemoryArtifactMaturityGrant(firstRevision, []SupportingEvidenceRecord{
		{Evidence: mustEvidenceID(t), Source: EvidenceSourceKindAutomatedValidationRun, Strength: EvidenceStrengthCorroborated},
	})
	if err != nil {
		t.Fatal(err)
	}
	revision, err = TransitionMemoryArtifactMaturity(revision, MaturityStateValidated, proof)
	if err != nil {
		t.Fatal(err)
	}

	secondRevision := mustMemoryArtifactRevisionID(t)
	corrected, err := ReviseMemoryArtifactWithUserCorrection(revision, secondRevision, validObservationContent(t))
	if err != nil {
		t.Fatal(err)
	}

	if corrected.RevisionID != secondRevision {
		t.Fatalf("RevisionID = %v, want %v", corrected.RevisionID, secondRevision)
	}
	if corrected.SupersedesRevision != firstRevision {
		t.Fatalf("SupersedesRevision = %v, want %v", corrected.SupersedesRevision, firstRevision)
	}
	if !corrected.CreatedFromCorrection {
		t.Fatal("CreatedFromCorrection = false, want true")
	}
	if corrected.Maturity != MaturityStateCandidate {
		t.Fatalf("corrected Maturity = %v, want candidate (authority must not carry forward)", corrected.Maturity)
	}
	if revision.Maturity != MaturityStateValidated || revision.RevisionID != firstRevision {
		t.Fatal("correction mutated the prior (historical) revision")
	}

	if _, err := ReviseMemoryArtifactWithUserCorrection(revision, firstRevision, validObservationContent(t)); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("reusing the prior revision id error = %v, want ErrInvalidDomainValue", err)
	}
}

// TestReviseMemoryArtifactWithUserCorrectionRefusesTerminalForAuthorityPriors
// is the adversarial-review defect-1 regression: a correction of a
// quarantined, invalidated, or retired revision must never produce a new
// revision under the SAME MemoryArtifactID that can reach an
// authority-bearing state again. Per §31 Artifact Failure Protocol,
// "Quarantine is terminal for the exposed lesson version... An independently
// derived successor receives a new identity." Before the fix,
// ReviseMemoryArtifactWithUserCorrection never read prior.Maturity at all
// and unconditionally reset it to Candidate, so a quarantined/invalidated/
// retired artifact could be "corrected" straight back onto the path to
// Validated under its original identity.
func TestReviseMemoryArtifactWithUserCorrectionRefusesTerminalForAuthorityPriors(t *testing.T) {
	terminalStates := []MaturityState{MaturityStateQuarantined, MaturityStateInvalidated, MaturityStateRetired}
	for _, terminal := range terminalStates {
		t.Run(string(terminal), func(t *testing.T) {
			artifact := mustMemoryArtifactID(t)
			firstRevision := mustMemoryArtifactRevisionID(t)
			scope := MemoryProjectScope{Project: mustProjectID(t)}

			revision, err := NewMemoryArtifactRevision(artifact, firstRevision, scope, validObservationContent(t))
			if err != nil {
				t.Fatal(err)
			}
			// Quarantine, invalidation, and retirement are never
			// authority-bearing destinations, so the zero-value proof
			// returned by a rejected AuthorizeMemoryArtifactMaturityGrant
			// call is sufficient here, matching how a real caller (e.g.
			// internal/storage's non-authority-bearing transition branch)
			// reaches these states.
			revision, err = TransitionMemoryArtifactMaturity(revision, terminal, authorityProof{})
			if err != nil {
				t.Fatalf("setup: reach %s: %v", terminal, err)
			}
			if revision.Maturity != terminal {
				t.Fatalf("setup: Maturity = %v, want %v", revision.Maturity, terminal)
			}

			corrected, err := ReviseMemoryArtifactWithUserCorrection(revision, mustMemoryArtifactRevisionID(t), validObservationContent(t))
			if !errors.Is(err, ErrMemoryArtifactCorrectionRequiresNewIdentity) {
				t.Fatalf("correction from %s: error = %v, want ErrMemoryArtifactCorrectionRequiresNewIdentity", terminal, err)
			}
			if corrected.ArtifactID == artifact {
				t.Fatalf("correction from %s unexpectedly returned a non-zero revision under the original ArtifactID", terminal)
			}

			// Confirm the bypass is fully closed: even chaining through to
			// AuthorizeMemoryArtifactMaturityGrant and TransitionMemoryArtifactMaturity
			// is moot, because ReviseMemoryArtifactWithUserCorrection itself
			// never produced a revision to feed them.
		})
	}
}

func TestPreviewMemoryArtifactDeletionSeparatesDerivedFromAndInfluencedBy(t *testing.T) {
	target := mustMemoryArtifactID(t)
	semanticDependent := mustMemoryArtifactID(t)
	contextuallyInfluenced := mustMemoryArtifactID(t)
	unrelated := mustMemoryArtifactID(t)

	index := map[MemoryArtifactID]MemoryArtifactLineage{
		target:                 {ArtifactID: target},
		semanticDependent:      {ArtifactID: semanticDependent, DerivedFrom: []MemoryArtifactID{target}},
		contextuallyInfluenced: {ArtifactID: contextuallyInfluenced, InfluencedBy: []MemoryArtifactID{target}},
		unrelated:              {ArtifactID: unrelated},
	}

	preview, err := PreviewMemoryArtifactDeletion(target, index)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.DirectDependents) != 1 || preview.DirectDependents[0] != semanticDependent {
		t.Fatalf("DirectDependents = %v, want [%v]", preview.DirectDependents, semanticDependent)
	}
	if len(preview.ContextuallyInfluenced) != 1 || preview.ContextuallyInfluenced[0] != contextuallyInfluenced {
		t.Fatalf("ContextuallyInfluenced = %v, want [%v]", preview.ContextuallyInfluenced, contextuallyInfluenced)
	}

	if _, err := PreviewMemoryArtifactDeletion(MemoryArtifactID{}, index); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("empty target error = %v, want ErrInvalidDomainValue", err)
	}
}

// TestPreviewMemoryArtifactDeletionIsTransitiveOverDerivedFrom is the
// adversarial-review defect-3 regression: PreviewMemoryArtifactDeletion
// previously walked only one hop of derived_from, but
// internal/storage's cascadeMemoryArtifactDerivedFrom deletes the full
// transitive closure. A caller previewing (and acknowledging) only the
// one-hop set would lose a deeper subtree they were never shown.
func TestPreviewMemoryArtifactDeletionIsTransitiveOverDerivedFrom(t *testing.T) {
	target := mustMemoryArtifactID(t)
	directChild := mustMemoryArtifactID(t)
	grandchild := mustMemoryArtifactID(t)
	unrelated := mustMemoryArtifactID(t)

	index := map[MemoryArtifactID]MemoryArtifactLineage{
		directChild: {ArtifactID: directChild, DerivedFrom: []MemoryArtifactID{target}},
		grandchild:  {ArtifactID: grandchild, DerivedFrom: []MemoryArtifactID{directChild}},
		unrelated:   {ArtifactID: unrelated},
	}

	preview, err := PreviewMemoryArtifactDeletion(target, index)
	if err != nil {
		t.Fatal(err)
	}
	found := map[MemoryArtifactID]bool{}
	for _, id := range preview.DirectDependents {
		found[id] = true
	}
	if len(preview.DirectDependents) != 2 || !found[directChild] || !found[grandchild] {
		t.Fatalf("DirectDependents = %v, want exactly the transitive closure [%v %v]", preview.DirectDependents, directChild, grandchild)
	}
}

// TestPreviewMemoryArtifactDeletionTerminatesOnDerivedFromCycle guards the
// visited-set requirement in the transitive walk: a two-node derived_from
// cycle is legal per MemoryArtifactLineage.Validate (which only forbids
// direct self-reference), so the traversal must terminate rather than loop
// forever.
func TestPreviewMemoryArtifactDeletionTerminatesOnDerivedFromCycle(t *testing.T) {
	a := mustMemoryArtifactID(t)
	b := mustMemoryArtifactID(t)
	index := map[MemoryArtifactID]MemoryArtifactLineage{
		a: {ArtifactID: a, DerivedFrom: []MemoryArtifactID{b}},
		b: {ArtifactID: b, DerivedFrom: []MemoryArtifactID{a}},
	}
	preview, err := PreviewMemoryArtifactDeletion(a, index)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.DirectDependents) != 1 || preview.DirectDependents[0] != b {
		t.Fatalf("DirectDependents = %v, want [%v]", preview.DirectDependents, b)
	}
}

func TestValidateMemoryArtifactDeletionRequiresAcknowledgedDependents(t *testing.T) {
	target := mustMemoryArtifactID(t)
	dependent := mustMemoryArtifactID(t)
	index := map[MemoryArtifactID]MemoryArtifactLineage{
		dependent: {ArtifactID: dependent, DerivedFrom: []MemoryArtifactID{target}},
	}
	preview, err := PreviewMemoryArtifactDeletion(target, index)
	if err != nil {
		t.Fatal(err)
	}

	unacknowledged := MemoryArtifactDeletionRequest{Target: target, Preview: preview}
	if err := ValidateMemoryArtifactDeletion(unacknowledged, index); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("error = %v, want ErrInvalidDomainValue", err)
	}

	acknowledged := unacknowledged
	acknowledged.AcknowledgedDependents = []MemoryArtifactID{dependent}
	if err := ValidateMemoryArtifactDeletion(acknowledged, index); err != nil {
		t.Fatalf("acknowledged deletion: %v", err)
	}

	// Acknowledging a different identity than the actual dependent must not
	// substitute for acknowledging the real previewed set (identity-bound
	// acknowledgement, not a bare bool).
	wrongIdentity := unacknowledged
	wrongIdentity.AcknowledgedDependents = []MemoryArtifactID{mustMemoryArtifactID(t)}
	if err := ValidateMemoryArtifactDeletion(wrongIdentity, index); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("wrong-identity acknowledgement error = %v, want ErrInvalidDomainValue", err)
	}

	emptyIndex := map[MemoryArtifactID]MemoryArtifactLineage{}
	noDependentsPreview, err := PreviewMemoryArtifactDeletion(target, emptyIndex)
	if err != nil {
		t.Fatal(err)
	}
	noDependents := MemoryArtifactDeletionRequest{Target: target, Preview: noDependentsPreview}
	if err := ValidateMemoryArtifactDeletion(noDependents, emptyIndex); err != nil {
		t.Fatalf("no-dependent deletion without acknowledgement: %v", err)
	}

	mismatched := MemoryArtifactDeletionRequest{Target: target, Preview: MemoryArtifactDeletionPreview{Target: dependent}}
	if err := ValidateMemoryArtifactDeletion(mismatched, index); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("mismatched-preview error = %v, want ErrInvalidDomainValue", err)
	}
}

// TestValidateMemoryArtifactDeletionRejectsForgedOrStalePreview is the
// adversarial-review defect-3 regression: ValidateMemoryArtifactDeletion
// previously checked only Preview.Target == Target, so a hand-built preview
// claiming zero dependents validated even when the real lineage index shows
// a genuine dependent (whether forged outright, or merely stale relative to
// a lineage change since the preview was computed). It must now recompute
// the preview from the caller-supplied current index and reject any
// mismatch.
func TestValidateMemoryArtifactDeletionRejectsForgedOrStalePreview(t *testing.T) {
	target := mustMemoryArtifactID(t)
	dependent := mustMemoryArtifactID(t)
	realIndex := map[MemoryArtifactID]MemoryArtifactLineage{
		dependent: {ArtifactID: dependent, DerivedFrom: []MemoryArtifactID{target}},
	}

	forged := MemoryArtifactDeletionPreview{Target: target} // claims zero dependents
	request := MemoryArtifactDeletionRequest{Target: target, Preview: forged}
	if err := ValidateMemoryArtifactDeletion(request, realIndex); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("forged/stale preview error = %v, want ErrInvalidDomainValue", err)
	}

	// The freshly recomputed, correctly acknowledged preview still succeeds.
	fresh, err := PreviewMemoryArtifactDeletion(target, realIndex)
	if err != nil {
		t.Fatal(err)
	}
	valid := MemoryArtifactDeletionRequest{Target: target, Preview: fresh, AcknowledgedDependents: []MemoryArtifactID{dependent}}
	if err := ValidateMemoryArtifactDeletion(valid, realIndex); err != nil {
		t.Fatalf("fresh, acknowledged preview: %v", err)
	}
}

func TestNewMemoryArtifactExportRecordRequiresMatchingLineage(t *testing.T) {
	artifact := mustMemoryArtifactID(t)
	revision, err := NewMemoryArtifactRevision(artifact, mustMemoryArtifactRevisionID(t), MemoryProjectScope{Project: mustProjectID(t)}, validObservationContent(t))
	if err != nil {
		t.Fatal(err)
	}
	// A freshly observed artifact with no derived_from ancestry: known to
	// have no origin/roots ancestor (it is one itself), not merely absent.
	lineage := MemoryArtifactLineage{ArtifactID: artifact, OriginKnown: true, LineageRootsKnown: true, SupportingEpisodesKnown: true}

	record, err := NewMemoryArtifactExportRecord(revision, lineage)
	if err != nil {
		t.Fatal(err)
	}
	if record.ArtifactID != artifact || record.RevisionID != revision.RevisionID {
		t.Fatalf("export record identity = %+v, want to match revision %+v", record, revision)
	}

	mismatched := MemoryArtifactLineage{ArtifactID: mustMemoryArtifactID(t), OriginKnown: true, LineageRootsKnown: true}
	if _, err := NewMemoryArtifactExportRecord(revision, mismatched); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("mismatched-lineage error = %v, want ErrInvalidDomainValue", err)
	}
}

// -----------------------------------------------------------------------
// §31 Influence Lineage: derived_from vs influenced_by, evidence families,
// and independent confirmation.
// -----------------------------------------------------------------------

func TestMemoryArtifactLineageRejectsAmbiguousDualRelationship(t *testing.T) {
	self := mustMemoryArtifactID(t)
	ancestor := mustMemoryArtifactID(t)

	if err := (MemoryArtifactLineage{ArtifactID: self, DerivedFrom: []MemoryArtifactID{self}}).Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("self-reference error = %v, want ErrInvalidDomainValue", err)
	}
	ambiguous := MemoryArtifactLineage{
		ArtifactID:   self,
		DerivedFrom:  []MemoryArtifactID{ancestor},
		InfluencedBy: []MemoryArtifactID{ancestor},
	}
	if err := ambiguous.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("dual-relationship error = %v, want ErrInvalidDomainValue", err)
	}
}

// TestMemoryArtifactLineageOriginAndRootsRequireKnownOrExplainedUnknown is
// the adversarial-review defect-7 regression: OriginArtifactID and
// LineageRootIDs previously had no Known/UnknownReason companion (unlike
// RevisionBinding, the package's own established convention), so a bare
// zero origin/roots value was indistinguishable from "not yet determined"
// and rendered as an unexplained blank in web/frontend/memoryinspector.
func TestMemoryArtifactLineageOriginAndRootsRequireKnownOrExplainedUnknown(t *testing.T) {
	self := mustMemoryArtifactID(t)
	origin := mustMemoryArtifactID(t)
	root := mustMemoryArtifactID(t)

	// Unknown without a reason must be rejected for both origin and roots.
	if err := (MemoryArtifactLineage{ArtifactID: self, LineageRootsKnown: true}).Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("unexplained unknown origin error = %v, want ErrInvalidDomainValue", err)
	}
	if err := (MemoryArtifactLineage{ArtifactID: self, OriginKnown: true}).Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("unexplained unknown lineage roots error = %v, want ErrInvalidDomainValue", err)
	}

	// Unknown with a reason, and no contradicting value, is valid.
	explained := MemoryArtifactLineage{
		ArtifactID:                      self,
		OriginUnknownReason:             "origin predates lineage tracking",
		LineageRootsUnknownReason:       "roots predate lineage tracking",
		SupportingEpisodesUnknownReason: "supporting episodes predate episode tracking",
	}
	if err := explained.Validate(); err != nil {
		t.Fatalf("explained-unknown lineage: %v", err)
	}

	// Unknown must not also carry a value (contradiction).
	contradictoryOrigin := explained
	contradictoryOrigin.OriginArtifactID = origin
	if err := contradictoryOrigin.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("unknown origin with a value error = %v, want ErrInvalidDomainValue", err)
	}
	contradictoryRoots := explained
	contradictoryRoots.LineageRootIDs = []MemoryArtifactID{root}
	if err := contradictoryRoots.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("unknown lineage roots with values error = %v, want ErrInvalidDomainValue", err)
	}

	// Known must not also carry an unknown reason (contradiction).
	knownWithReason := MemoryArtifactLineage{ArtifactID: self, OriginKnown: true, OriginUnknownReason: "should not be set", SupportingEpisodesKnown: true}
	if err := knownWithReason.Validate(); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("known origin with an unknown reason error = %v, want ErrInvalidDomainValue", err)
	}

	// Known with a real value (self-originated: zero origin, no roots
	// besides itself) or a populated value are both valid.
	if err := (MemoryArtifactLineage{ArtifactID: self, OriginKnown: true, LineageRootsKnown: true, SupportingEpisodesKnown: true}).Validate(); err != nil {
		t.Fatalf("known, self-originated lineage: %v", err)
	}
	populated := MemoryArtifactLineage{
		ArtifactID: self, DerivedFrom: []MemoryArtifactID{origin},
		OriginKnown: true, OriginArtifactID: origin,
		LineageRootsKnown: true, LineageRootIDs: []MemoryArtifactID{root},
		SupportingEpisodesKnown: true,
	}
	if err := populated.Validate(); err != nil {
		t.Fatalf("known, populated origin/roots: %v", err)
	}
}

// TestConfirmsMemoryArtifactIndependentlyRejectsSameEvidenceFamily
// operationalizes §31: "Descendants of a pattern do not independently
// confirm their ancestor. Independent confirmation must come from episodes
// never exposed to that lineage."
func TestConfirmsMemoryArtifactIndependentlyRejectsSameEvidenceFamily(t *testing.T) {
	ancestor := mustMemoryArtifactID(t)
	descendant := mustMemoryArtifactID(t)
	exposedEpisode := mustEpisodeID(t)
	freshEpisode := mustEpisodeID(t)

	index := map[MemoryArtifactID]MemoryArtifactLineage{
		ancestor: {
			ArtifactID:              ancestor,
			SupportingEpisodes:      []EpisodeID{exposedEpisode},
			SupportingEpisodesKnown: true,
		},
		descendant: {
			ArtifactID:              descendant,
			DerivedFrom:             []MemoryArtifactID{ancestor},
			SupportingEpisodes:      []EpisodeID{exposedEpisode},
			SupportingEpisodesKnown: true,
		},
	}

	// The descendant rewrote the pattern completely, but it only ever saw
	// the ancestor's own exposed episode: not independent.
	confirmed, err := ConfirmsMemoryArtifactIndependently(ancestor, index, []EpisodeID{exposedEpisode})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed {
		t.Fatal("exposed episode counted as independent confirmation")
	}

	// A genuinely unexposed episode does count.
	confirmed, err = ConfirmsMemoryArtifactIndependently(ancestor, index, []EpisodeID{freshEpisode})
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed {
		t.Fatal("unexposed episode did not count as independent confirmation")
	}

	// A mix still counts as long as at least one candidate episode is
	// outside the family's exposure.
	confirmed, err = ConfirmsMemoryArtifactIndependently(ancestor, index, []EpisodeID{exposedEpisode, freshEpisode})
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed {
		t.Fatal("mixed candidate episodes should still count as independent confirmation")
	}

	if _, err := ConfirmsMemoryArtifactIndependently(mustMemoryArtifactID(t), index, []EpisodeID{freshEpisode}); !errors.Is(err, ErrInvalidDomainValue) {
		t.Fatalf("unknown ancestor error = %v, want ErrInvalidDomainValue", err)
	}
}

// TestConfirmsMemoryArtifactIndependentlyFailsClosedOnUnknownExposure closes
// the landmine described in internal/storage/memory_lineage_repository.go's
// prior "SupportingEpisodes and LineageRootIDs are out of this lane's scope"
// state: before SupportingEpisodesKnown existed, an unpopulated
// SupportingEpisodes slice was indistinguishable from "checked; genuinely
// never exposed," so ConfirmsMemoryArtifactIndependently would always
// confirm independence for every candidate, silently defeating §31
// "Descendants of a pattern do not independently confirm their ancestor."
// This test proves the fixed behavior: an ancestor whose own exposure is
// merely unknown (SupportingEpisodesKnown false) must never let
// independence be confirmed; the caller gets ErrMemoryArtifactExposureUnknown
// instead of a false "yes, independent".
func TestConfirmsMemoryArtifactIndependentlyFailsClosedOnUnknownExposure(t *testing.T) {
	ancestor := mustMemoryArtifactID(t)
	descendant := mustMemoryArtifactID(t)
	freshEpisode := mustEpisodeID(t)

	index := map[MemoryArtifactID]MemoryArtifactLineage{
		ancestor: {
			ArtifactID:                      ancestor,
			SupportingEpisodesKnown:         false,
			SupportingEpisodesUnknownReason: "never computed by this caller",
		},
		descendant: {
			ArtifactID:              descendant,
			DerivedFrom:             []MemoryArtifactID{ancestor},
			SupportingEpisodesKnown: true,
		},
	}

	if _, err := ConfirmsMemoryArtifactIndependently(ancestor, index, []EpisodeID{freshEpisode}); !errors.Is(err, ErrMemoryArtifactExposureUnknown) {
		t.Fatalf("unknown ancestor exposure error = %v, want ErrMemoryArtifactExposureUnknown", err)
	}

	// Once exposure is genuinely known (even as "no episodes at all"),
	// independence can be confirmed again.
	known := index[ancestor]
	known.SupportingEpisodesKnown = true
	known.SupportingEpisodesUnknownReason = ""
	index[ancestor] = known
	confirmed, err := ConfirmsMemoryArtifactIndependently(ancestor, index, []EpisodeID{freshEpisode})
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed {
		t.Fatal("known-empty exposure should allow independent confirmation")
	}
}

func TestEvidenceFamilyIsTransitive(t *testing.T) {
	root := mustMemoryArtifactID(t)
	middle := mustMemoryArtifactID(t)
	leaf := mustMemoryArtifactID(t)

	index := map[MemoryArtifactID]MemoryArtifactLineage{
		root:   {ArtifactID: root},
		middle: {ArtifactID: middle, DerivedFrom: []MemoryArtifactID{root}},
		leaf:   {ArtifactID: leaf, InfluencedBy: []MemoryArtifactID{middle}},
	}

	family := index[leaf].EvidenceFamily(index)
	for _, id := range []MemoryArtifactID{root, middle, leaf} {
		if _, ok := family[id]; !ok {
			t.Fatalf("evidence family missing %v: %v", id, family)
		}
	}
}
