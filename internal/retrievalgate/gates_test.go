package retrievalgate

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
)

// -----------------------------------------------------------------------
// M21-068: reject on project-boundary mismatch
// -----------------------------------------------------------------------

func TestEvaluateProjectBoundary_RejectsMismatch(t *testing.T) {
	queryProject := mustProjectID(t)
	otherProject := mustProjectID(t)
	boundary := domain.MemoryQueryProjectBoundary{Project: queryProject}
	scope := domain.MemoryProjectScope{Project: otherProject}

	outcome, err := EvaluateProjectBoundary(boundary, scope)
	if err != nil {
		t.Fatalf("EvaluateProjectBoundary: unexpected error %v", err)
	}
	if outcome.Eligible {
		t.Fatal("EvaluateProjectBoundary: want reject for cross-project scope, got eligible")
	}
	if outcome.Reason != RejectionReasonProjectBoundaryMismatch {
		t.Fatalf("EvaluateProjectBoundary: Reason = %q, want %q", outcome.Reason, RejectionReasonProjectBoundaryMismatch)
	}
}

func TestEvaluateProjectBoundary_AcceptsMatch(t *testing.T) {
	project := mustProjectID(t)
	boundary := domain.MemoryQueryProjectBoundary{Project: project}
	scope := domain.MemoryProjectScope{Project: project}

	outcome, err := EvaluateProjectBoundary(boundary, scope)
	if err != nil {
		t.Fatalf("EvaluateProjectBoundary: unexpected error %v", err)
	}
	if !outcome.Eligible {
		t.Fatalf("EvaluateProjectBoundary: want eligible for same-project scope, got reject reason %q", outcome.Reason)
	}
}

func TestEvaluateProjectBoundary_MalformedInputIsError(t *testing.T) {
	if _, err := EvaluateProjectBoundary(domain.MemoryQueryProjectBoundary{}, domain.MemoryProjectScope{Project: mustProjectID(t)}); err == nil {
		t.Fatal("EvaluateProjectBoundary: want error for zero-value boundary, got nil")
	}
}

// -----------------------------------------------------------------------
// M21-069: reject on toolchain/dependency mismatch
// -----------------------------------------------------------------------

func TestEvaluateToolchainCompatibility_RejectsMissingBinding(t *testing.T) {
	required := []fingerprint.ToolchainBinding{{Name: "go", Version: "1.26.0"}}
	available := []fingerprint.ToolchainBinding{{Name: "go", Version: "1.25.0"}}

	outcome := EvaluateToolchainCompatibility(required, available)
	if outcome.Eligible {
		t.Fatal("EvaluateToolchainCompatibility: want reject for version mismatch, got eligible")
	}
	if outcome.Reason != RejectionReasonToolchainMismatch {
		t.Fatalf("EvaluateToolchainCompatibility: Reason = %q, want %q", outcome.Reason, RejectionReasonToolchainMismatch)
	}
}

func TestEvaluateToolchainCompatibility_AcceptsExactMatch(t *testing.T) {
	required := []fingerprint.ToolchainBinding{{Name: "go", Version: "1.26.0"}}
	available := []fingerprint.ToolchainBinding{{Name: "go", Version: "1.26.0"}, {Name: "codeflux.dev/codeflux", Version: "v0.0.0"}}

	outcome := EvaluateToolchainCompatibility(required, available)
	if !outcome.Eligible {
		t.Fatalf("EvaluateToolchainCompatibility: want eligible for exact match, got reject reason %q", outcome.Reason)
	}
}

func TestEvaluateToolchainCompatibility_EmptyRequiredNeverRejects(t *testing.T) {
	outcome := EvaluateToolchainCompatibility(nil, nil)
	if !outcome.Eligible {
		t.Fatal("EvaluateToolchainCompatibility: a candidate declaring no toolchain requirement must never be rejected")
	}
}

func TestEvaluateDependencyCompatibility_RejectsUnknownDependency(t *testing.T) {
	required := []fingerprint.ToolchainBinding{{Name: "golang.org/x/text", Version: "v0.14.0"}}
	outcome := EvaluateDependencyCompatibility(required, nil)
	if outcome.Eligible {
		t.Fatal("EvaluateDependencyCompatibility: want reject when the task fingerprint has no matching binding, got eligible")
	}
	if outcome.Reason != RejectionReasonDependencyMismatch {
		t.Fatalf("EvaluateDependencyCompatibility: Reason = %q, want %q", outcome.Reason, RejectionReasonDependencyMismatch)
	}
}

func TestEvaluateDependencyCompatibility_AcceptsExactMatch(t *testing.T) {
	required := []fingerprint.ToolchainBinding{{Name: "golang.org/x/text", Version: "v0.14.0"}}
	available := []fingerprint.ToolchainBinding{{Name: "golang.org/x/text", Version: "v0.14.0"}}
	outcome := EvaluateDependencyCompatibility(required, available)
	if !outcome.Eligible {
		t.Fatalf("EvaluateDependencyCompatibility: want eligible for exact match, got reject reason %q", outcome.Reason)
	}
}

func TestToolchainAndDependencyMismatchAreDistinctReasons(t *testing.T) {
	// M21-069 asks for two separately attributable reasons even though both
	// gates share the same binding-comparison logic.
	toolchain := EvaluateToolchainCompatibility([]fingerprint.ToolchainBinding{{Name: "go", Version: "1.26.0"}}, nil)
	dependency := EvaluateDependencyCompatibility([]fingerprint.ToolchainBinding{{Name: "go", Version: "1.26.0"}}, nil)
	if toolchain.Reason == dependency.Reason {
		t.Fatalf("toolchain and dependency mismatches must carry distinct reasons, both got %q", toolchain.Reason)
	}
}

// -----------------------------------------------------------------------
// M21-070: reject on invalidated evidence
// -----------------------------------------------------------------------

func TestEvaluateInvalidatedEvidence_RejectsQuarantinedMaturity(t *testing.T) {
	outcome, err := EvaluateInvalidatedEvidence(domain.MaturityStateQuarantined, nil)
	if err != nil {
		t.Fatalf("EvaluateInvalidatedEvidence: unexpected error %v", err)
	}
	if outcome.Eligible {
		t.Fatal("EvaluateInvalidatedEvidence: want reject for quarantined maturity, got eligible")
	}
	if outcome.Reason != RejectionReasonInvalidatedEvidence {
		t.Fatalf("Reason = %q, want %q", outcome.Reason, RejectionReasonInvalidatedEvidence)
	}
}

func TestEvaluateInvalidatedEvidence_RejectsInvalidatedMaturity(t *testing.T) {
	outcome, err := EvaluateInvalidatedEvidence(domain.MaturityStateInvalidated, nil)
	if err != nil {
		t.Fatalf("EvaluateInvalidatedEvidence: unexpected error %v", err)
	}
	if outcome.Eligible {
		t.Fatal("EvaluateInvalidatedEvidence: want reject for invalidated maturity, got eligible")
	}
}

func TestEvaluateInvalidatedEvidence_RejectsInvalidatedSupportingEvidence(t *testing.T) {
	outcome, err := EvaluateInvalidatedEvidence(domain.MaturityStateValidated, []domain.AssuranceLevel{
		domain.AssuranceLevelContractChecked,
		domain.AssuranceLevelInvalidated,
	})
	if err != nil {
		t.Fatalf("EvaluateInvalidatedEvidence: unexpected error %v", err)
	}
	if outcome.Eligible {
		t.Fatal("EvaluateInvalidatedEvidence: want reject when any supporting evidence is invalidated, got eligible")
	}
	if outcome.Reason != RejectionReasonInvalidatedEvidence {
		t.Fatalf("Reason = %q, want %q", outcome.Reason, RejectionReasonInvalidatedEvidence)
	}
}

func TestEvaluateInvalidatedEvidence_AcceptsValidatedWithGoodEvidence(t *testing.T) {
	outcome, err := EvaluateInvalidatedEvidence(domain.MaturityStateValidated, []domain.AssuranceLevel{
		domain.AssuranceLevelFullyEvaluated,
	})
	if err != nil {
		t.Fatalf("EvaluateInvalidatedEvidence: unexpected error %v", err)
	}
	if !outcome.Eligible {
		t.Fatalf("EvaluateInvalidatedEvidence: want eligible for validated maturity with good evidence, got reject reason %q", outcome.Reason)
	}
}

func TestEvaluateInvalidatedEvidence_MalformedMaturityIsError(t *testing.T) {
	if _, err := EvaluateInvalidatedEvidence(domain.MaturityState("not-a-state"), nil); err == nil {
		t.Fatal("EvaluateInvalidatedEvidence: want error for undeclared maturity, got nil")
	}
}

// -----------------------------------------------------------------------
// M21-071: reject when assurance is below the current task requirement
// -----------------------------------------------------------------------

func TestEvaluateAssurance_RejectsBelowRequirement(t *testing.T) {
	outcome, err := EvaluateAssurance(domain.AssuranceLevelRuntimeOnly, domain.AssuranceLevelFullyEvaluated)
	if err != nil {
		t.Fatalf("EvaluateAssurance: unexpected error %v", err)
	}
	if outcome.Eligible {
		t.Fatal("EvaluateAssurance: want reject when achieved is weaker than required, got eligible")
	}
	if outcome.Reason != RejectionReasonAssuranceBelowRequirement {
		t.Fatalf("Reason = %q, want %q", outcome.Reason, RejectionReasonAssuranceBelowRequirement)
	}
}

func TestEvaluateAssurance_AcceptsAtOrAboveRequirement(t *testing.T) {
	for _, achieved := range []domain.AssuranceLevel{domain.AssuranceLevelContractChecked, domain.AssuranceLevelModelVerified, domain.AssuranceLevelFullyEvaluated} {
		outcome, err := EvaluateAssurance(achieved, domain.AssuranceLevelContractChecked)
		if err != nil {
			t.Fatalf("EvaluateAssurance(%s): unexpected error %v", achieved, err)
		}
		if !outcome.Eligible {
			t.Fatalf("EvaluateAssurance(%s): want eligible at/above contract-checked requirement, got reject reason %q", achieved, outcome.Reason)
		}
	}
}

func TestEvaluateAssurance_InvalidatedAchievedNeverSatisfiesAnyRequirement(t *testing.T) {
	outcome, err := EvaluateAssurance(domain.AssuranceLevelInvalidated, domain.AssuranceLevelRuntimeOnly)
	if err != nil {
		t.Fatalf("EvaluateAssurance: unexpected error %v", err)
	}
	if outcome.Eligible {
		t.Fatal("EvaluateAssurance: an invalidated achieved level must never satisfy even the weakest requirement")
	}
}

func TestEvaluateAssurance_RequiredInvalidatedIsError(t *testing.T) {
	if _, err := EvaluateAssurance(domain.AssuranceLevelFullyEvaluated, domain.AssuranceLevelInvalidated); err == nil {
		t.Fatal("EvaluateAssurance: want error when required assurance is invalidated, got nil")
	}
}

func TestAchievedAssuranceFromEvidence_PicksStrongestNonInvalidated(t *testing.T) {
	got := AchievedAssuranceFromEvidence([]domain.AssuranceLevel{
		domain.AssuranceLevelRuntimeOnly,
		domain.AssuranceLevelInvalidated,
		domain.AssuranceLevelModelVerified,
		domain.AssuranceLevelContractChecked,
	})
	if got != domain.AssuranceLevelModelVerified {
		t.Fatalf("AchievedAssuranceFromEvidence = %q, want %q", got, domain.AssuranceLevelModelVerified)
	}
}

func TestAchievedAssuranceFromEvidence_AllInvalidatedYieldsInvalidated(t *testing.T) {
	got := AchievedAssuranceFromEvidence([]domain.AssuranceLevel{domain.AssuranceLevelInvalidated})
	if got != domain.AssuranceLevelInvalidated {
		t.Fatalf("AchievedAssuranceFromEvidence = %q, want %q", got, domain.AssuranceLevelInvalidated)
	}
}

func TestAchievedAssuranceFromEvidence_NoEvidenceYieldsWeakestLevel(t *testing.T) {
	got := AchievedAssuranceFromEvidence(nil)
	if got != domain.AssuranceLevelRuntimeOnly {
		t.Fatalf("AchievedAssuranceFromEvidence(nil) = %q, want %q (fail closed, not silently strong)", got, domain.AssuranceLevelRuntimeOnly)
	}
}

// -----------------------------------------------------------------------
// Applicability predicates
// -----------------------------------------------------------------------

func TestEvaluateApplicabilityPredicate_RepositoryMatch(t *testing.T) {
	project := mustProjectID(t)
	repository := mustRepositoryID(t)
	otherRepository := mustRepositoryID(t)
	task := sampleFingerprint(t, project, repository)

	reject, err := EvaluateApplicabilityPredicate(ApplicabilityPredicate{
		Kind:                     ApplicabilityPredicateKindRepositoryMatch,
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
		RepositoryMatch:          otherRepository,
	}, task)
	if err != nil {
		t.Fatalf("EvaluateApplicabilityPredicate: unexpected error %v", err)
	}
	if reject.Eligible {
		t.Fatal("EvaluateApplicabilityPredicate: want reject for a different repository, got eligible")
	}
	if reject.Reason != RejectionReasonApplicabilityPredicateFailed {
		t.Fatalf("Reason = %q, want %q", reject.Reason, RejectionReasonApplicabilityPredicateFailed)
	}

	accept, err := EvaluateApplicabilityPredicate(ApplicabilityPredicate{
		Kind:                     ApplicabilityPredicateKindRepositoryMatch,
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
		RepositoryMatch:          repository,
	}, task)
	if err != nil {
		t.Fatalf("EvaluateApplicabilityPredicate: unexpected error %v", err)
	}
	if !accept.Eligible {
		t.Fatalf("EvaluateApplicabilityPredicate: want eligible for the same repository, got reject reason %q", accept.Reason)
	}
}

func TestEvaluateApplicabilityPredicate_PathScope(t *testing.T) {
	task := sampleFingerprint(t, mustProjectID(t), mustRepositoryID(t))

	reject, err := EvaluateApplicabilityPredicate(ApplicabilityPredicate{
		Kind:                     ApplicabilityPredicateKindPathScope,
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
		PathPrefixes:             []string{"web/frontend/"},
	}, task)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if reject.Eligible {
		t.Fatal("want reject: task's affected path is outside the declared path scope")
	}

	accept, err := EvaluateApplicabilityPredicate(ApplicabilityPredicate{
		Kind:                     ApplicabilityPredicateKindPathScope,
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
		PathPrefixes:             []string{"internal/retrievalgate/"},
	}, task)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !accept.Eligible {
		t.Fatalf("want eligible: task's affected path is inside the declared path scope, got reject reason %q", accept.Reason)
	}
}

func TestEvaluateApplicabilityPredicate_CapabilityRequirement(t *testing.T) {
	task := sampleFingerprint(t, mustProjectID(t), mustRepositoryID(t)) // requests only AuthorityClassTaskWrite

	reject, err := EvaluateApplicabilityPredicate(ApplicabilityPredicate{
		Kind:                     ApplicabilityPredicateKindCapabilityRequirement,
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
		RequiredAuthority:        []fingerprint.AuthorityClass{fingerprint.AuthorityClassDestructive},
	}, task)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if reject.Eligible {
		t.Fatal("want reject: candidate needs authority the task never requested")
	}

	accept, err := EvaluateApplicabilityPredicate(ApplicabilityPredicate{
		Kind:                     ApplicabilityPredicateKindCapabilityRequirement,
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
		RequiredAuthority:        []fingerprint.AuthorityClass{fingerprint.AuthorityClassTaskWrite},
	}, task)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !accept.Eligible {
		t.Fatalf("want eligible: candidate needs only authority the task requested, got reject reason %q", accept.Reason)
	}
}

func TestEvaluateApplicabilityPredicate_SchemaVersionMismatchHonorsUnknownFieldBehavior(t *testing.T) {
	task := sampleFingerprint(t, mustProjectID(t), mustRepositoryID(t))
	mismatched := ApplicabilityPredicate{
		Kind:                     ApplicabilityPredicateKindRepositoryMatch,
		FingerprintSchemaVersion: task.SchemaVersion + 1,
		RepositoryMatch:          task.Repository,
	}

	mismatched.UnknownFieldBehavior = ApplicabilityUnknownFieldReject
	reject, err := EvaluateApplicabilityPredicate(mismatched, task)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if reject.Eligible {
		t.Fatal("want reject: schema version mismatch under reject behavior must never silently pass")
	}

	mismatched.UnknownFieldBehavior = ApplicabilityUnknownFieldSkip
	skip, err := EvaluateApplicabilityPredicate(mismatched, task)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !skip.Eligible {
		t.Fatal("want eligible: schema version mismatch under skip behavior contributes no constraint")
	}
}

func TestEvaluateApplicabilityPredicate_CustomKindFailsClosedByDefault(t *testing.T) {
	task := sampleFingerprint(t, mustProjectID(t), mustRepositoryID(t))
	outcome, err := EvaluateApplicabilityPredicate(ApplicabilityPredicate{
		Kind:                     ApplicabilityPredicateKindCustom,
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
	}, task)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if outcome.Eligible {
		t.Fatal("want reject: an uninterpretable custom predicate under reject behavior must never silently pass")
	}
}

func TestEvaluateApplicabilityPredicate_MalformedKindIsError(t *testing.T) {
	task := sampleFingerprint(t, mustProjectID(t), mustRepositoryID(t))
	_, err := EvaluateApplicabilityPredicate(ApplicabilityPredicate{
		Kind:                     "not-a-kind",
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
	}, task)
	if !errors.Is(err, ErrInvalidRetrievalGateInput) {
		t.Fatalf("want ErrInvalidRetrievalGateInput for an undeclared predicate kind, got %v", err)
	}
}
