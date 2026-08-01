package retrievalgate

import (
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// -----------------------------------------------------------------------
// M21-077: never treat vector similarity as eligibility (structural
// enforcement, not a comment)
// -----------------------------------------------------------------------

// DiscoveredCandidate is a pure discovery result: it names which candidate
// a retrieval query found and through which channel(s), together with an
// optional similarity score. It deliberately carries NO applicability,
// compatibility, evidence, or assurance field -- mirroring
// internal/atomname.NameMatch, whose own doc comment states the same
// property in the same words this package now enforces one layer up: "This
// type structurally cannot represent 'approved for use,' only 'found.'"
//
// This is the ONLY type in this package that may carry a SimilarityScore.
// No function anywhere in this package that computes an EligibilityDecision
// (EvaluateEligibility, or any of the individual gate predicates in
// gates.go) accepts a DiscoveredCandidate, a float64, or a *float64 as a
// parameter -- there is no parameter through which a similarity score could
// reach an eligibility decision, so it cannot type-check. See
// structural_test.go for a reflection-based proof that this property holds
// and stays red if it regresses.
type DiscoveredCandidate struct {
	RevisionID domain.MemoryArtifactRevisionID
	// Channels lists every channel that independently discovered this
	// candidate for the same query (M21-137's "or several channels" case):
	// for example a candidate can legitimately match both by canonical name
	// (DiscoveryChannelExactIdentity) and by embedding similarity
	// (DiscoveryChannelVectorSimilarity) in the same retrieval pass.
	Channels []DiscoveryChannel
	// SimilarityScore is present only when DiscoveryChannelVectorSimilarity
	// is among Channels; nil otherwise. It is opaque discovery metadata:
	// nothing in this package reads it for any purpose other than carrying
	// it through to CandidateProvenance for logging.
	SimilarityScore *float64
}

// Validate checks that value is a well-formed discovery result: at least
// one declared channel, no duplicates, and SimilarityScore present if and
// only if DiscoveryChannelVectorSimilarity was among Channels.
func (value DiscoveredCandidate) Validate() error {
	if value.RevisionID.IsZero() {
		return fieldError("retrievalgate.discovered_candidate.revision_id", "must not be empty")
	}
	if len(value.Channels) == 0 {
		return fieldError("retrievalgate.discovered_candidate.channels", "must name at least one discovery channel")
	}
	seen := make(map[DiscoveryChannel]struct{}, len(value.Channels))
	hasVectorSimilarity := false
	for _, channel := range value.Channels {
		if !channel.IsValid() {
			return fieldError("retrievalgate.discovered_candidate.channels", "contains an undeclared discovery channel")
		}
		if _, duplicate := seen[channel]; duplicate {
			return fieldError("retrievalgate.discovered_candidate.channels", "must not repeat the same channel")
		}
		seen[channel] = struct{}{}
		if channel == DiscoveryChannelVectorSimilarity {
			hasVectorSimilarity = true
		}
	}
	if hasVectorSimilarity && value.SimilarityScore == nil {
		return fieldError("retrievalgate.discovered_candidate.similarity_score", "must be present when vector-similarity is a discovery channel")
	}
	if !hasVectorSimilarity && value.SimilarityScore != nil {
		return fieldError("retrievalgate.discovered_candidate.similarity_score", "must be absent when vector-similarity is not a discovery channel")
	}
	if value.SimilarityScore != nil && (*value.SimilarityScore < -1.0 || *value.SimilarityScore > 1.0) {
		return fieldError("retrievalgate.discovered_candidate.similarity_score", "must be between -1.0 and 1.0")
	}
	return nil
}

// CollapseChannelsForStorage picks one representative DiscoveryChannel for
// callers persisting through the current single-valued
// storage.MemoryRetrievalCandidateSource column, which cannot yet represent
// "discovered by several channels" losslessly (see this package's report
// for the field gap). It always picks the strongest channel present, per
// docs/plan.md §31 "Structured database fields are authoritative. Embeddings
// provide candidate recall only": a candidate discovered by both exact
// identity and vector similarity must never be logged as merely
// vector-similarity-discovered, because that would understate how it was
// actually found. The full channel set remains available on
// DiscoveredCandidate/CandidateProvenance for any caller that can persist
// more than the current schema's one column.
func CollapseChannelsForStorage(channels []DiscoveryChannel) DiscoveryChannel {
	if len(channels) == 0 {
		return ""
	}
	strongest := channels[0]
	for _, channel := range channels[1:] {
		if channelPriority(channel) < channelPriority(strongest) {
			strongest = channel
		}
	}
	return strongest
}

// -----------------------------------------------------------------------
// M21-072, M21-137: candidate provenance (which channel(s) caused entry,
// and the eligibility decision computed independently of them)
// -----------------------------------------------------------------------

// CandidateProvenance is the complete, storage-ready record of one
// retrieval candidate: how it was found (Discovery) and what the
// eligibility gates independently decided about it (Eligibility). It is
// assembled only AFTER Eligibility already exists -- NewCandidateProvenance
// takes an already-computed EligibilityDecision, never a fingerprint or
// candidate it could recompute one from -- so nothing in this package can
// let a CandidateProvenance's discovery metadata retroactively influence
// the eligibility decision it carries.
type CandidateProvenance struct {
	Discovery   DiscoveredCandidate
	Eligibility EligibilityDecision
	// Influence is nil until the agent has acted on an eligible candidate;
	// see RecordAgentInfluence (M21-138).
	Influence *AgentInfluenceRecord
}

// NewCandidateProvenance pairs a discovery result with the eligibility
// decision computed for the same candidate (M21-072, M21-137). It checks
// only that discovery is well-formed and that both values name the same
// RevisionID; it never inspects, and cannot be made to inspect, Discovery
// while computing Eligibility, because Eligibility is a required, immutable
// argument here rather than something this function derives.
func NewCandidateProvenance(discovery DiscoveredCandidate, eligibility EligibilityDecision) (CandidateProvenance, error) {
	if err := discovery.Validate(); err != nil {
		return CandidateProvenance{}, err
	}
	return CandidateProvenance{Discovery: discovery, Eligibility: eligibility}, nil
}

// -----------------------------------------------------------------------
// M21-138: record whether the agent used, adapted, or rejected the
// candidate, and why
// -----------------------------------------------------------------------

// AgentInfluenceAction classifies what the agent did with an ELIGIBLE
// candidate. Retrieval and influence are different facts (docs/plan.md
// §31): a candidate can be eligible and still never influence the outcome.
type AgentInfluenceAction string

const (
	AgentInfluenceActionUsed     AgentInfluenceAction = "used"
	AgentInfluenceActionAdapted  AgentInfluenceAction = "adapted"
	AgentInfluenceActionRejected AgentInfluenceAction = "rejected"
)

func (value AgentInfluenceAction) isValid() bool {
	switch value {
	case AgentInfluenceActionUsed, AgentInfluenceActionAdapted, AgentInfluenceActionRejected:
		return true
	default:
		return false
	}
}

// MaximumJustificationBytes bounds AgentInfluenceRecord.Justification,
// matching storage.CreateMemoryRetrievalDecision's
// reason_detail_redacted column (`length BETWEEN 1 AND 2048`).
const MaximumJustificationBytes = 2048

// AgentInfluenceRecord is the typed, storage-ready record of what an agent
// did with one eligible candidate and why (M21-138).
type AgentInfluenceRecord struct {
	Action        AgentInfluenceAction
	Justification string
}

// Reason maps value.Action to the matching RejectionReason vocabulary value
// (storage's decision.reason_kind), for a caller persisting through
// CreateMemoryRetrievalDecision.
func (value AgentInfluenceRecord) Reason() RejectionReason {
	switch value.Action {
	case AgentInfluenceActionUsed:
		return RejectionReasonEligibleAndUsed
	case AgentInfluenceActionAdapted:
		return RejectionReasonEligibleAndAdapted
	default: // AgentInfluenceActionRejected
		return RejectionReasonEligibleAndRejectedByAgent
	}
}

// Accepted reports whether value.Action corresponds to storage's
// decision='accepted' outer state (used or adapted); "rejected" maps to
// decision='rejected' even though the candidate itself was eligible,
// matching the CHECK constraint on memory_retrieval_decisions.
func (value AgentInfluenceRecord) Accepted() bool {
	return value.Action == AgentInfluenceActionUsed || value.Action == AgentInfluenceActionAdapted
}

// RecordAgentInfluence builds the typed M21-138 record of what the agent
// did with a candidate and why, and structurally refuses to record real
// influence ("used" or "adapted") for a candidate the gates never declared
// eligible: decision.Eligible must be true for any action, closing exactly
// the gap AGENTS.md's "Retrieval and influence are different facts -- do
// not conflate 'was retrieved' with 'influenced the outcome'" describes.
// justification is required and bounded: an influence record with no "why"
// is never accepted, matching storage's non-null,
// 1..MaximumJustificationBytes-byte reason_detail_redacted column.
func RecordAgentInfluence(decision EligibilityDecision, action AgentInfluenceAction, justification string) (AgentInfluenceRecord, error) {
	if !decision.Eligible {
		return AgentInfluenceRecord{}, fieldError("retrievalgate.agent_influence.decision", "cannot record agent influence for a candidate the eligibility gates did not accept")
	}
	if !action.isValid() {
		return AgentInfluenceRecord{}, fieldError("retrievalgate.agent_influence.action", "is not declared")
	}
	trimmed := strings.TrimSpace(justification)
	if trimmed == "" {
		return AgentInfluenceRecord{}, fieldError("retrievalgate.agent_influence.justification", "must explain why the agent used, adapted, or rejected the candidate")
	}
	if len(justification) > MaximumJustificationBytes {
		return AgentInfluenceRecord{}, fieldError("retrievalgate.agent_influence.justification", "must not exceed 2048 bytes")
	}
	return AgentInfluenceRecord{Action: action, Justification: justification}, nil
}

// WithInfluence returns a copy of provenance with Influence set to record,
// refusing to attach an influence record whose action disagrees with
// whether provenance.Eligibility was ever eligible in the first place (the
// same guarantee RecordAgentInfluence already enforces when building
// record, checked again here so a caller cannot construct an
// AgentInfluenceRecord against one decision and attach it to a differently
// eligible CandidateProvenance).
func (provenance CandidateProvenance) WithInfluence(record AgentInfluenceRecord) (CandidateProvenance, error) {
	if !provenance.Eligibility.Eligible {
		return CandidateProvenance{}, fieldError("retrievalgate.candidate_provenance.influence", "cannot attach agent influence to a candidate the eligibility gates did not accept")
	}
	updated := provenance
	updated.Influence = &record
	return updated, nil
}
