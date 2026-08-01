package retrievalgate

import (
	"testing"
)

func float64Ptr(value float64) *float64 { return &value }

// -----------------------------------------------------------------------
// DiscoveredCandidate (M21-072, M21-137)
// -----------------------------------------------------------------------

func TestDiscoveredCandidate_ValidateRequiresScoreOnlyWithVectorSimilarity(t *testing.T) {
	base := DiscoveredCandidate{
		RevisionID: mustMemoryArtifactRevisionID(t),
		Channels:   []DiscoveryChannel{DiscoveryChannelVectorSimilarity},
	}
	if err := base.Validate(); err == nil {
		t.Fatal("Validate: want error when vector-similarity is a channel but SimilarityScore is nil")
	}

	withScore := base
	withScore.SimilarityScore = float64Ptr(0.93)
	if err := withScore.Validate(); err != nil {
		t.Fatalf("Validate: unexpected error %v", err)
	}

	exactOnly := DiscoveredCandidate{
		RevisionID:      mustMemoryArtifactRevisionID(t),
		Channels:        []DiscoveryChannel{DiscoveryChannelExactIdentity},
		SimilarityScore: float64Ptr(0.5),
	}
	if err := exactOnly.Validate(); err == nil {
		t.Fatal("Validate: want error when SimilarityScore is present without a vector-similarity channel")
	}
}

func TestDiscoveredCandidate_SeveralChannelsAreRecorded(t *testing.T) {
	discovered := DiscoveredCandidate{
		RevisionID:      mustMemoryArtifactRevisionID(t),
		Channels:        []DiscoveryChannel{DiscoveryChannelExactIdentity, DiscoveryChannelVectorSimilarity},
		SimilarityScore: float64Ptr(0.87),
	}
	if err := discovered.Validate(); err != nil {
		t.Fatalf("Validate: unexpected error %v", err)
	}
	if len(discovered.Channels) != 2 {
		t.Fatalf("Channels = %v, want both exact-identity and vector-similarity recorded", discovered.Channels)
	}
}

func TestCollapseChannelsForStorage_PrefersStrongestChannel(t *testing.T) {
	got := CollapseChannelsForStorage([]DiscoveryChannel{DiscoveryChannelVectorSimilarity, DiscoveryChannelExactIdentity})
	if got != DiscoveryChannelExactIdentity {
		t.Fatalf("CollapseChannelsForStorage = %q, want %q (exact identity outranks vector similarity)", got, DiscoveryChannelExactIdentity)
	}
}

// -----------------------------------------------------------------------
// M21-138: record whether the agent used, adapted, or rejected the
// candidate, and why
// -----------------------------------------------------------------------

func TestRecordAgentInfluence_RefusesIneligibleCandidate(t *testing.T) {
	rejected := EligibilityDecision{Eligible: false, Reason: RejectionReasonAssuranceBelowRequirement}
	if _, err := RecordAgentInfluence(rejected, AgentInfluenceActionUsed, "looked like a fit"); err == nil {
		t.Fatal("RecordAgentInfluence: want error when recording real influence for an ineligible candidate, got nil")
	}
}

func TestRecordAgentInfluence_RequiresJustification(t *testing.T) {
	eligible := EligibilityDecision{Eligible: true}
	if _, err := RecordAgentInfluence(eligible, AgentInfluenceActionUsed, "   "); err == nil {
		t.Fatal("RecordAgentInfluence: want error for a blank justification, got nil")
	}
}

func TestRecordAgentInfluence_AcceptsEligibleCandidateWithJustification(t *testing.T) {
	eligible := EligibilityDecision{Eligible: true}
	record, err := RecordAgentInfluence(eligible, AgentInfluenceActionAdapted, "reused the retry policy, changed the backoff schedule")
	if err != nil {
		t.Fatalf("RecordAgentInfluence: unexpected error %v", err)
	}
	if record.Reason() != RejectionReasonEligibleAndAdapted {
		t.Fatalf("Reason() = %q, want %q", record.Reason(), RejectionReasonEligibleAndAdapted)
	}
	if !record.Accepted() {
		t.Fatal("Accepted() = false, want true for an adapted candidate")
	}
}

func TestRecordAgentInfluence_RejectedByAgentIsStillAboutAnEligibleCandidate(t *testing.T) {
	eligible := EligibilityDecision{Eligible: true}
	record, err := RecordAgentInfluence(eligible, AgentInfluenceActionRejected, "wrong shape for this call site")
	if err != nil {
		t.Fatalf("RecordAgentInfluence: unexpected error %v", err)
	}
	if record.Reason() != RejectionReasonEligibleAndRejectedByAgent {
		t.Fatalf("Reason() = %q, want %q", record.Reason(), RejectionReasonEligibleAndRejectedByAgent)
	}
	if record.Accepted() {
		t.Fatal("Accepted() = true, want false: the outer decision is 'rejected' even though the candidate was eligible")
	}
}

func TestCandidateProvenance_WithInfluenceRefusesIneligibleCandidate(t *testing.T) {
	discovery := DiscoveredCandidate{RevisionID: mustMemoryArtifactRevisionID(t), Channels: []DiscoveryChannel{DiscoveryChannelExactIdentity}}
	provenance, err := NewCandidateProvenance(discovery, EligibilityDecision{Eligible: false, Reason: RejectionReasonToolchainMismatch})
	if err != nil {
		t.Fatalf("NewCandidateProvenance: unexpected error %v", err)
	}
	record := AgentInfluenceRecord{Action: AgentInfluenceActionUsed, Justification: "ignoring eligibility"}
	if _, err := provenance.WithInfluence(record); err == nil {
		t.Fatal("WithInfluence: want error when attaching influence to an ineligible candidate's provenance, got nil")
	}
}

func TestCandidateProvenance_RoundTripsDiscoveryAndEligibility(t *testing.T) {
	discovery := DiscoveredCandidate{
		RevisionID:      mustMemoryArtifactRevisionID(t),
		Channels:        []DiscoveryChannel{DiscoveryChannelStructuredFields, DiscoveryChannelVectorSimilarity},
		SimilarityScore: float64Ptr(0.99),
	}
	decision := EligibilityDecision{Eligible: true}
	provenance, err := NewCandidateProvenance(discovery, decision)
	if err != nil {
		t.Fatalf("NewCandidateProvenance: unexpected error %v", err)
	}
	record, err := RecordAgentInfluence(provenance.Eligibility, AgentInfluenceActionUsed, "matched the requested repair shape exactly")
	if err != nil {
		t.Fatalf("RecordAgentInfluence: unexpected error %v", err)
	}
	provenance, err = provenance.WithInfluence(record)
	if err != nil {
		t.Fatalf("WithInfluence: unexpected error %v", err)
	}
	if provenance.Influence == nil || provenance.Influence.Action != AgentInfluenceActionUsed {
		t.Fatalf("provenance.Influence = %+v, want a recorded Used action", provenance.Influence)
	}
	if len(provenance.Discovery.Channels) != 2 {
		t.Fatalf("provenance.Discovery.Channels = %v, want both channels preserved", provenance.Discovery.Channels)
	}
}
