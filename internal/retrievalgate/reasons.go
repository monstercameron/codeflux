package retrievalgate

import (
	"errors"
	"fmt"
)

// ErrInvalidRetrievalGateInput classifies a malformed argument passed to a
// gate or provenance function in this package: a caller-side precondition
// failure (an invalid enum, a missing required field), never a legitimate
// "this candidate is ineligible" decision. Decisions are always returned as
// a GateOutcome or EligibilityDecision value, never as this error, so a
// caller can never confuse "the candidate was rejected" with "the call was
// malformed" by checking error-ness alone.
var ErrInvalidRetrievalGateInput = errors.New("invalid retrieval gate input")

// FieldError identifies one invalid input field without exposing
// caller-supplied content in the error's identity, mirroring
// internal/fingerprint.FieldError.
type FieldError struct {
	Field  string
	Reason string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s is invalid: %s", e.Field, e.Reason)
}

// Unwrap allows errors.Is(err, ErrInvalidRetrievalGateInput).
func (e *FieldError) Unwrap() error {
	return ErrInvalidRetrievalGateInput
}

func fieldError(field, reason string) error {
	return &FieldError{Field: field, Reason: reason}
}

// DiscoveryChannel classifies one way a candidate entered a retrieval
// result set (M21-072, M21-137, M21-168). It mirrors
// storage.MemoryRetrievalCandidateSource's wire vocabulary by string value,
// deliberately without importing internal/storage (this package stays
// storage-independent; see doc.go). Because it is a distinct Go type, no
// DiscoveryChannel value can be passed where a RejectionReason is expected,
// or vice versa, even though a few string values happen to differ.
type DiscoveryChannel string

const (
	// DiscoveryChannelExactIdentity mirrors storage's "exact-match": the
	// candidate's exact fingerprint hash, canonical name, or normalized
	// phrase matched the query exactly.
	DiscoveryChannelExactIdentity DiscoveryChannel = "exact-match"
	// DiscoveryChannelStructuredFields mirrors storage's
	// "applicability-pass": the candidate was found through a deterministic
	// structured-field filter (an M21-022 applicability predicate, a
	// reviewed alias, a path/package/symbol hint), never through similarity.
	DiscoveryChannelStructuredFields DiscoveryChannel = "applicability-pass"
	// DiscoveryChannelVectorSimilarity mirrors storage's
	// "vector-similarity": the candidate was found by embedding-vector
	// cosine similarity. Per AGENTS.md, this channel alone never confers
	// eligibility; see EligibilityCandidate and structural_test.go.
	DiscoveryChannelVectorSimilarity DiscoveryChannel = "vector-similarity"
)

// IsValid reports whether the discovery channel is declared.
func (value DiscoveryChannel) IsValid() bool {
	switch value {
	case DiscoveryChannelExactIdentity, DiscoveryChannelStructuredFields, DiscoveryChannelVectorSimilarity:
		return true
	default:
		return false
	}
}

// channelPriority orders channels from strongest to weakest evidence of a
// real match, per docs/plan.md §31 "Structured database fields are
// authoritative. Embeddings provide candidate recall only." Used only by
// CollapseChannelsForStorage (provenance.go) to pick one representative
// channel for the current single-valued storage schema; it is never used by
// any eligibility computation.
func channelPriority(channel DiscoveryChannel) int {
	switch channel {
	case DiscoveryChannelExactIdentity:
		return 0
	case DiscoveryChannelStructuredFields:
		return 1
	case DiscoveryChannelVectorSimilarity:
		return 2
	default:
		return 3
	}
}

// RejectionReason classifies why one candidate failed a gate, or (for the
// eligible-and-* values) what the agent went on to do with an eligible
// candidate (M21-072, M21-138). It mirrors
// storage.MemoryRetrievalDecisionReason's wire vocabulary by string value,
// deliberately without importing internal/storage; see doc.go.
type RejectionReason string

const (
	// RejectionReasonProjectBoundaryMismatch: M21-068.
	RejectionReasonProjectBoundaryMismatch RejectionReason = "project-boundary-mismatch"
	// RejectionReasonToolchainMismatch: M21-069.
	RejectionReasonToolchainMismatch RejectionReason = "toolchain-mismatch"
	// RejectionReasonDependencyMismatch: M21-069.
	RejectionReasonDependencyMismatch RejectionReason = "dependency-mismatch"
	// RejectionReasonInvalidatedEvidence: M21-070.
	RejectionReasonInvalidatedEvidence RejectionReason = "invalidated-evidence"
	// RejectionReasonAssuranceBelowRequirement: M21-071.
	RejectionReasonAssuranceBelowRequirement RejectionReason = "assurance-below-requirement"
	// RejectionReasonApplicabilityPredicateFailed: the M21-022 structured
	// applicability-predicate gate (docs/plan.md §31 "Applicability
	// conditions must be deterministic predicates rather than prose"),
	// required to run after discovery by M21-136.
	RejectionReasonApplicabilityPredicateFailed RejectionReason = "applicability-predicate-failed"
	// RejectionReasonEligibleAndUsed: the candidate passed every gate and
	// the agent used it as-is (M21-138).
	RejectionReasonEligibleAndUsed RejectionReason = "eligible-and-used"
	// RejectionReasonEligibleAndAdapted: the candidate passed every gate
	// and the agent adapted it before use (M21-138).
	RejectionReasonEligibleAndAdapted RejectionReason = "eligible-and-adapted"
	// RejectionReasonEligibleAndRejectedByAgent: the candidate passed every
	// gate, yet the agent chose not to use it (M21-138). This is the fact
	// that distinguishes "was retrieved" from "influenced the outcome":
	// eligibility alone never implies influence.
	RejectionReasonEligibleAndRejectedByAgent RejectionReason = "eligible-and-rejected-by-agent"
	// RejectionReasonNoEligibleItem: the retrieval query produced zero
	// eligible candidates (mirrors storage's "no-eligible-item"; recording
	// this fact is M21-076's concern, out of this package's scope, but the
	// shared vocabulary value is declared here for completeness).
	RejectionReasonNoEligibleItem RejectionReason = "no-eligible-item"
)

// IsRejection reports whether reason denotes an outright gate failure
// (never reached the agent-influence stage at all).
func (value RejectionReason) IsRejection() bool {
	switch value {
	case RejectionReasonProjectBoundaryMismatch,
		RejectionReasonToolchainMismatch,
		RejectionReasonDependencyMismatch,
		RejectionReasonInvalidatedEvidence,
		RejectionReasonAssuranceBelowRequirement,
		RejectionReasonApplicabilityPredicateFailed,
		RejectionReasonNoEligibleItem:
		return true
	default:
		return false
	}
}

// GateOutcome is the typed result of one individual eligibility predicate.
// Reason and Detail are the zero value whenever Eligible is true: an
// eligible outcome never carries a rejection reason.
type GateOutcome struct {
	Eligible bool
	Reason   RejectionReason
	Detail   string
}

// eligible builds a passing GateOutcome.
func eligible() GateOutcome {
	return GateOutcome{Eligible: true}
}

// rejected builds a failing GateOutcome with a required reason and detail.
func rejected(reason RejectionReason, detail string) GateOutcome {
	return GateOutcome{Eligible: false, Reason: reason, Detail: detail}
}
