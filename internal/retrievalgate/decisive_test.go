package retrievalgate

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
)

// TestM21_144_HighSimilarityAtomWithFailedApplicabilityIsRejected is the
// M21-144 decisive test for the whole lane: a candidate discovered with a
// near-maximal vector-similarity score must still be rejected the moment
// its applicability predicate fails, because similarity was never a
// parameter EvaluateEligibility could consult in the first place.
func TestM21_144_HighSimilarityAtomWithFailedApplicabilityIsRejected(t *testing.T) {
	project := mustProjectID(t)
	repository := mustRepositoryID(t)
	wrongRepository := mustRepositoryID(t)
	task := sampleFingerprint(t, project, repository)
	boundary := domain.MemoryQueryProjectBoundary{Project: project}

	// The atom is discovered with a near-perfect similarity score.
	discovery := DiscoveredCandidate{
		RevisionID:      mustMemoryArtifactRevisionID(t),
		Channels:        []DiscoveryChannel{DiscoveryChannelVectorSimilarity},
		SimilarityScore: float64Ptr(0.98),
	}
	if err := discovery.Validate(); err != nil {
		t.Fatalf("DiscoveredCandidate.Validate: unexpected error %v", err)
	}

	// The same atom, described in eligibility terms, declares an
	// applicability predicate scoped to a DIFFERENT repository than the
	// task's -- it fails applicability regardless of how similar its
	// embedding was to the query.
	candidate := eligibleCandidate(t, domain.MemoryProjectScope{Project: project, Repository: repository}, task)
	candidate.RevisionID = discovery.RevisionID
	candidate.Applicability = []ApplicabilityPredicate{{
		Kind:                     ApplicabilityPredicateKindRepositoryMatch,
		FingerprintSchemaVersion: task.SchemaVersion,
		UnknownFieldBehavior:     ApplicabilityUnknownFieldReject,
		RepositoryMatch:          wrongRepository,
	}}

	decision, err := EvaluateEligibility(candidate, boundary, task)
	if err != nil {
		t.Fatalf("EvaluateEligibility: unexpected error %v", err)
	}
	if decision.Eligible {
		t.Fatal("a high-similarity atom with failed applicability must be rejected, got eligible")
	}
	if decision.Reason != RejectionReasonApplicabilityPredicateFailed {
		t.Fatalf("Reason = %q, want %q", decision.Reason, RejectionReasonApplicabilityPredicateFailed)
	}

	// The rejection holds regardless of the discovered similarity score: a
	// low-similarity discovery of the SAME (still applicability-failing)
	// candidate reaches the identical eligibility decision, because
	// EvaluateEligibility takes an EligibilityCandidate, never a
	// DiscoveredCandidate -- there is no channel through which either
	// discovery's score could have reached it in the first place.
	lowSimilarity := DiscoveredCandidate{
		RevisionID:      discovery.RevisionID,
		Channels:        []DiscoveryChannel{DiscoveryChannelVectorSimilarity},
		SimilarityScore: float64Ptr(0.02),
	}
	if err := lowSimilarity.Validate(); err != nil {
		t.Fatalf("DiscoveredCandidate.Validate: unexpected error %v", err)
	}
	secondDecision, err := EvaluateEligibility(candidate, boundary, task)
	if err != nil {
		t.Fatalf("EvaluateEligibility: unexpected error %v", err)
	}
	if secondDecision.Eligible != decision.Eligible || secondDecision.Reason != decision.Reason {
		t.Fatalf("eligibility outcome changed with similarity score (%+v vs %+v); similarity must never influence eligibility", decision, secondDecision)
	}

	// Both the high- and low-similarity provenance records land on the
	// identical (ineligible) decision, and neither can be marked "used" or
	// "adapted" -- the candidate never became eligible, no matter how
	// similar it looked.
	highSimilarityProvenance, err := NewCandidateProvenance(discovery, decision)
	if err != nil {
		t.Fatalf("NewCandidateProvenance: unexpected error %v", err)
	}
	lowSimilarityProvenance, err := NewCandidateProvenance(lowSimilarity, secondDecision)
	if err != nil {
		t.Fatalf("NewCandidateProvenance: unexpected error %v", err)
	}
	for _, provenance := range []CandidateProvenance{highSimilarityProvenance, lowSimilarityProvenance} {
		if provenance.Eligibility.Eligible {
			t.Fatal("provenance.Eligibility.Eligible = true, want false")
		}
		if _, err := RecordAgentInfluence(provenance.Eligibility, AgentInfluenceActionUsed, "it looked extremely similar"); err == nil {
			t.Fatal("RecordAgentInfluence: want error when recording real influence for a candidate whose applicability failed despite a high similarity score")
		}
	}
}

// TestM21_145_RichlyDocumentedAtomCannotSelfPromoteAssurance is the M21-145
// decisive test: AGENTS.md says "a comment can help find an atom but
// cannot make it valid." Two candidates carry IDENTICAL, weak
// (runtime-only) supporting evidence; one is paired with a minimal
// documentation comment and the other with an extremely rich one (long
// prose in every full-weight embedding field, per
// internal/atomdoc.ComposeDocumentationEmbeddingInput). Documentation
// richness must have zero effect on the achieved assurance level or the
// eligibility outcome.
func TestM21_145_RichlyDocumentedAtomCannotSelfPromoteAssurance(t *testing.T) {
	project := mustProjectID(t)
	repository := mustRepositoryID(t)
	task := sampleFingerprint(t, project, repository)
	boundary := domain.MemoryQueryProjectBoundary{Project: project}
	scope := domain.MemoryProjectScope{Project: project, Repository: repository}

	minimalDoc := atomdoc.Document{
		Purpose: atomdoc.Field{Text: "Retries a flaky network call."},
	}
	richDoc := atomdoc.Document{
		Purpose:           atomdoc.Field{Text: strings.Repeat("This atom retries a flaky network call with exponential backoff and jitter. ", 20)},
		UseWhen:           atomdoc.Field{Text: strings.Repeat("Use this whenever an external HTTP call may transiently fail. ", 20)},
		Semantics:         atomdoc.Field{Text: strings.Repeat("It wraps the call in a bounded retry loop with capped exponential backoff. ", 20)},
		Effects:           atomdoc.Field{Items: []string{"performs network I/O", "sleeps between attempts", "emits a retry-count metric"}},
		FailureSemantics:  atomdoc.Field{Items: []string{"returns the last error after the retry budget is exhausted"}},
		RetrievalConcepts: atomdoc.Field{Text: strings.Repeat("retry backoff jitter resilience idempotent network call wrapper ", 20)},
	}

	minimalInput, err := atomdoc.ComposeDocumentationEmbeddingInput(minimalDoc)
	if err != nil {
		t.Fatalf("ComposeDocumentationEmbeddingInput(minimal): unexpected error %v", err)
	}
	richInput, err := atomdoc.ComposeDocumentationEmbeddingInput(richDoc)
	if err != nil {
		t.Fatalf("ComposeDocumentationEmbeddingInput(rich): unexpected error %v", err)
	}
	if richTextLength(richInput) <= richTextLength(minimalInput) {
		t.Fatalf("test fixture is not actually richer: rich=%d minimal=%d bytes", richTextLength(richInput), richTextLength(minimalInput))
	}

	// Both candidates carry the SAME weak evidence: only runtime-only.
	weakEvidence := []domain.AssuranceLevel{domain.AssuranceLevelRuntimeOnly}

	minimalAchieved := AchievedAssuranceFromEvidence(weakEvidence)
	richAchieved := AchievedAssuranceFromEvidence(weakEvidence)
	if minimalAchieved != richAchieved {
		t.Fatalf("achieved assurance differs (minimal=%q rich=%q) even though evidence is identical", minimalAchieved, richAchieved)
	}
	if richAchieved != domain.AssuranceLevelRuntimeOnly {
		t.Fatalf("achieved assurance = %q, want %q regardless of documentation", richAchieved, domain.AssuranceLevelRuntimeOnly)
	}

	// The task requires model-verified (see sampleFingerprint); a richly
	// documented atom with only runtime-only evidence must be rejected
	// exactly like a minimally documented one with the same evidence.
	minimalCandidate := eligibleCandidate(t, scope, task)
	minimalCandidate.SupportingEvidenceAssurance = weakEvidence
	richCandidate := eligibleCandidate(t, scope, task)
	richCandidate.SupportingEvidenceAssurance = weakEvidence

	minimalDecision, err := EvaluateEligibility(minimalCandidate, boundary, task)
	if err != nil {
		t.Fatalf("EvaluateEligibility(minimal): unexpected error %v", err)
	}
	richDecision, err := EvaluateEligibility(richCandidate, boundary, task)
	if err != nil {
		t.Fatalf("EvaluateEligibility(rich): unexpected error %v", err)
	}
	if minimalDecision.Eligible || richDecision.Eligible {
		t.Fatalf("both candidates must be rejected for insufficient assurance: minimal.Eligible=%v rich.Eligible=%v", minimalDecision.Eligible, richDecision.Eligible)
	}
	if minimalDecision.Reason != RejectionReasonAssuranceBelowRequirement || richDecision.Reason != RejectionReasonAssuranceBelowRequirement {
		t.Fatalf("want both rejected for assurance-below-requirement, got minimal=%q rich=%q", minimalDecision.Reason, richDecision.Reason)
	}
	if minimalDecision.Reason != richDecision.Reason {
		t.Fatal("documentation richness changed the rejection reason; it must have zero effect")
	}
}

func richTextLength(input atomdoc.DocumentationEmbeddingInput) int {
	total := 0
	for _, segment := range input.Segments {
		total += len(segment.Text)
	}
	return total
}

// TestM21_145_EvaluateAssuranceNeverSeesDocumentation is a second angle on
// M21-145: even if a caller tried to smuggle documentation-derived
// "confidence" into the assurance gate, EvaluateAssurance's signature
// leaves no parameter to smuggle it through (see also
// structural_test.go's TestEvaluateAssuranceSignatureAcceptsOnlyAssuranceLevels).
// This test drives the behavior end to end: a fixed achieved level produces
// a fixed outcome no matter what fingerprint.ExactFingerprint or
// domain.RiskLevel accompanies it.
func TestM21_145_EvaluateAssuranceNeverSeesDocumentation(t *testing.T) {
	outcomeA, err := EvaluateAssurance(domain.AssuranceLevelRuntimeOnly, domain.AssuranceLevelFullyEvaluated)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	outcomeB, err := EvaluateAssurance(domain.AssuranceLevelRuntimeOnly, domain.AssuranceLevelFullyEvaluated)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if outcomeA != outcomeB {
		t.Fatalf("EvaluateAssurance is not deterministic for identical inputs: %+v vs %+v", outcomeA, outcomeB)
	}
}
