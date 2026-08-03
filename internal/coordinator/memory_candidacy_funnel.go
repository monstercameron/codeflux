// Package coordinator: the extraction candidacy funnel (MEM-012, MEM-013,
// MEM-013a). §31 "Extraction Triggers and the Candidacy Funnel" governs what
// a run may WRITE into project memory, as distinct from retrieval's rules
// about what a run may READ. This file is the funnel itself: a pure,
// storage-independent decision every extractor -- MEM-006/MEM-008 today,
// MEM-014/MEM-015 later -- should route an observation through before it
// becomes a domain.MemoryArtifactContent, so an inadmissible source, an
// uncontested first attempt, or a weaker tier than the observation actually
// supports can never reach storage through a NEW extractor this file never
// anticipated, rather than depending on every future extractor to remember
// the rule for itself.
//
// This file makes no domain.MemoryArtifactKind decision. Searching the
// repository before writing this (per this task's own instruction) found
// that only two of the five §31 tiers have a corresponding domain content
// type today: Workspace fact (domain.MemoryArtifactKindRepositoryFact /
// ReviewedCommand / FileToTestMapping / RepositoryConvention, all already
// minted by MEM-006/MEM-008) and Regression case
// (domain.MemoryArtifactKindAcceptedRegressionCase, not yet minted by any
// extractor -- that is MEM-014). Mechanical rule, Routing evidence, and
// Advisory pattern have no domain.MemoryArtifactContent payload at all: they
// are MEM-015, an unticketed routing-evidence content type, and MEM-017
// onward respectively. This file therefore decides WHICH tier and WHETHER a
// candidate may be minted at all; it leaves HOW to represent that tier in
// storage to the extractor that already owns the corresponding content type,
// or to whichever future ticket adds one for the three that do not exist
// yet. Domain.MaturityState and domain.AuthorizeMemoryArtifactMaturityGrant
// remain the only path from Candidate to an authority-bearing state; this
// file governs an earlier, narrower question -- whether an observation may
// become a domain.MaturityStateCandidate at all, and at what tier -- and
// does not duplicate or bypass that later machinery.
package coordinator

import (
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// MemoryCandidacyTier is the §31 candidacy funnel's ordered evidence-
// strength scale: Workspace fact, Mechanical rule, Regression case, Routing
// evidence, Advisory pattern, strongest first. It is deliberately its own
// type rather than domain.MemoryArtifactKind because, per this file's
// package doc, three of the five tiers have no domain content type to name.
type MemoryCandidacyTier string

// The five declared candidacy tiers, in the exact strongest-to-weakest order
// §31's "Extraction Triggers and the Candidacy Funnel" and MEM-012's own
// text both list them.
const (
	MemoryCandidacyTierWorkspaceFact   MemoryCandidacyTier = "workspace-fact"
	MemoryCandidacyTierMechanicalRule  MemoryCandidacyTier = "mechanical-rule"
	MemoryCandidacyTierRegressionCase  MemoryCandidacyTier = "regression-case"
	MemoryCandidacyTierRoutingEvidence MemoryCandidacyTier = "routing-evidence"
	MemoryCandidacyTierAdvisoryPattern MemoryCandidacyTier = "advisory-pattern"
)

// memoryCandidacyTierStrength orders the five declared tiers strongest
// (index 0) to weakest. Every strength comparison in this file walks this
// slice rather than encoding a second, independent ordering, so the tier
// order can never drift between two competing implementations.
var memoryCandidacyTierStrength = []MemoryCandidacyTier{
	MemoryCandidacyTierWorkspaceFact,
	MemoryCandidacyTierMechanicalRule,
	MemoryCandidacyTierRegressionCase,
	MemoryCandidacyTierRoutingEvidence,
	MemoryCandidacyTierAdvisoryPattern,
}

// IsValid reports whether tier is one of the five declared candidacy tiers.
func (tier MemoryCandidacyTier) IsValid() bool {
	return tier.strengthIndex() >= 0
}

// strengthIndex returns tier's position in memoryCandidacyTierStrength (0 is
// strongest), or -1 for a tier IsValid reports false for.
func (tier MemoryCandidacyTier) strengthIndex() int {
	for index, declared := range memoryCandidacyTierStrength {
		if declared == tier {
			return index
		}
	}
	return -1
}

// weakerThan reports whether tier is strictly weaker than other, i.e. sits
// later in memoryCandidacyTierStrength. An invalid tier (strengthIndex -1)
// compares as weaker than everything and never weaker-than nothing, so an
// undeclared value can never be mistaken for a real strength.
func (tier MemoryCandidacyTier) weakerThan(other MemoryCandidacyTier) bool {
	tierIndex, otherIndex := tier.strengthIndex(), other.strengthIndex()
	switch {
	case tierIndex < 0:
		return otherIndex >= 0
	case otherIndex < 0:
		return false
	default:
		return tierIndex > otherIndex
	}
}

// requiresContestedFirstAttempt reports whether tier is one of the three
// §31 names as needing "something to have been contested": advisory
// pattern, regression case, and routing evidence about where a run stalled
// (MEM-013a). Workspace fact and mechanical rule are deliberately absent: a
// command that ran and exited zero, or a produced shape a validator, lint
// rule, compiler diagnostic, or contract could already name, is an
// observation of what executed or what the code looks like, not a
// conclusion about what was hard to get right, so a first-attempt success
// carries exactly as much evidence for either of those two tiers as a
// contested one does. Refusing them here would forbid MEM-006 through
// MEM-008 on the runs that go well, which are most of them.
func requiresContestedFirstAttempt(tier MemoryCandidacyTier) bool {
	switch tier {
	case MemoryCandidacyTierAdvisoryPattern, MemoryCandidacyTierRegressionCase, MemoryCandidacyTierRoutingEvidence:
		return true
	default:
		return false
	}
}

// MemoryCandidacyProducedWorkAttribution is a tri-state account of whether
// an observation's produced work has been attributed to the exact
// declarations this run changed (MEM-013's "a run whose produced work was
// not attributed to the declarations it changed" refusal). A bare bool
// cannot distinguish "attribution ran and found this work unattributed"
// from "attribution is not this observation's concern at all" (e.g. a
// workspace fact about which build command succeeded names no produced
// declaration); collapsing those two into a false zero value would either
// wrongly refuse observations that were never about produced work, or --
// worse -- silently admit an unattributed one because a caller forgot to
// set the field. Every proposal in this file requires a caller to say which
// one applies; there is no valid zero value.
type MemoryCandidacyProducedWorkAttribution string

const (
	MemoryCandidacyProducedWorkNotApplicable MemoryCandidacyProducedWorkAttribution = "not-applicable"
	MemoryCandidacyProducedWorkAttributed    MemoryCandidacyProducedWorkAttribution = "attributed"
	MemoryCandidacyProducedWorkUnattributed  MemoryCandidacyProducedWorkAttribution = "unattributed"
)

// IsValid reports whether value is one of the three declared states.
func (value MemoryCandidacyProducedWorkAttribution) IsValid() bool {
	switch value {
	case MemoryCandidacyProducedWorkNotApplicable, MemoryCandidacyProducedWorkAttributed, MemoryCandidacyProducedWorkUnattributed:
		return true
	default:
		return false
	}
}

// MemoryCandidacyRepositoryTextApproval is the same tri-state shape for
// MEM-013's "unapproved repository text" refusal: repository text no person
// approved is untrusted input reaching the product's own memory (§31), but
// most observations are not repository-text-sourced at all, so "unapproved"
// must never be confused with "not repository text." There is no valid zero
// value, for the same reason MemoryCandidacyProducedWorkAttribution has
// none.
type MemoryCandidacyRepositoryTextApproval string

const (
	MemoryCandidacyRepositoryTextNotSourced MemoryCandidacyRepositoryTextApproval = "not-repository-text-sourced"
	MemoryCandidacyRepositoryTextApproved   MemoryCandidacyRepositoryTextApproval = "approved"
	MemoryCandidacyRepositoryTextUnapproved MemoryCandidacyRepositoryTextApproval = "unapproved"
)

// IsValid reports whether value is one of the three declared states.
func (value MemoryCandidacyRepositoryTextApproval) IsValid() bool {
	switch value {
	case MemoryCandidacyRepositoryTextNotSourced, MemoryCandidacyRepositoryTextApproved, MemoryCandidacyRepositoryTextUnapproved:
		return true
	default:
		return false
	}
}

// MemoryCandidacyObservation is everything the funnel needs to decide
// whether, and at what tier, one episode-close observation may become a
// memory-artifact candidate. Every field the funnel checks is caller-
// supplied and caller-attested: this file trusts none of its own inputs
// more than the caller earned them (it does not, for example, re-derive
// attribution itself). The guarantee this file provides is that a
// truthfully described observation cannot slip past its own admissibility
// rules, not that every caller is honest -- the same limitation every other
// admission gate in this repository has (RefuseInadmissibleMemoryCandidacySource
// cannot see through a caller that lies about MechanicalRuleExpressible any
// more than domain.RecordMemoryArtifactSupportingEvidence can see through a
// caller that lies about EvidenceSourceKind).
type MemoryCandidacyObservation struct {
	// Source names what actually backs this observation. Per MEM-013,
	// domain.EvidenceSourceKindAgentSelfReport is refused outright, at any
	// tier: it is the same rule §31 already applies to maturity promotion
	// (domain.AuthorizeMemoryArtifactMaturityGrant), moved upstream to the
	// point of candidacy itself, so self-report can never reach even
	// domain.MaturityStateCandidate through an extractor that never calls
	// the promotion path at all -- closing the exact "through an extractor
	// rather than through retrieval" back door MEM-013 names.
	Source domain.EvidenceSourceKind

	// ProducedWorkAttributed classifies whether this observation's produced
	// work traces to the declarations this run actually changed.
	ProducedWorkAttributed MemoryCandidacyProducedWorkAttribution

	// RepositoryTextApproval classifies whether this observation is
	// repository text a person approved.
	RepositoryTextApproval MemoryCandidacyRepositoryTextApproval

	// Contested reports whether reaching this observation required
	// something to fail or stall first within the run, as opposed to a
	// first-attempt success (MEM-013a).
	Contested bool

	// WorkspaceFactExpressible, MechanicalRuleExpressible,
	// RegressionCaseComplete, and RoutingEvidenceApplicable report whether
	// the named tier -- strongest to weakest, skipping Advisory pattern,
	// which §31 defines as "everything that survives the four tiers above"
	// and is therefore always expressible -- could express what was
	// observed. A tier reports true only when its own §31 definition is
	// actually met:
	//
	//   - WorkspaceFactExpressible requires a deterministic, attributable
	//     successful execution;
	//   - MechanicalRuleExpressible requires an already-checkable
	//     validator/lint/compiler-diagnostic/contract shape, named in
	//     MechanicalRuleWhichRule rather than left implicit;
	//   - RegressionCaseComplete requires all four of a reproducible input,
	//     a stable oracle, a demonstrated failure, and an expected outcome
	//     -- not three of the four;
	//   - RoutingEvidenceApplicable requires a real cost/rung/stall record
	//     for this task's shape.
	WorkspaceFactExpressible  bool
	MechanicalRuleExpressible bool
	MechanicalRuleWhichRule   string
	RegressionCaseComplete    bool
	RoutingEvidenceApplicable bool
}

// expressibleAtTier reports whether tier's own §31 definition is met by
// observation, per the field-to-tier mapping documented on
// MemoryCandidacyObservation. Advisory pattern always reports true: §31
// defines it as everything that survives the four stronger tiers.
func (observation MemoryCandidacyObservation) expressibleAtTier(tier MemoryCandidacyTier) bool {
	switch tier {
	case MemoryCandidacyTierWorkspaceFact:
		return observation.WorkspaceFactExpressible
	case MemoryCandidacyTierMechanicalRule:
		return observation.MechanicalRuleExpressible
	case MemoryCandidacyTierRegressionCase:
		return observation.RegressionCaseComplete
	case MemoryCandidacyTierRoutingEvidence:
		return observation.RoutingEvidenceApplicable
	case MemoryCandidacyTierAdvisoryPattern:
		return true
	default:
		return false
	}
}

// Validate checks that observation's declared enums are real values a
// caller could not have reached by leaving a field at its Go zero value by
// accident: Source, ProducedWorkAttributed, and RepositoryTextApproval each
// have no valid empty-string member, so an unset field fails closed here
// rather than silently reading as an admissible default somewhere later in
// the funnel.
func (observation MemoryCandidacyObservation) Validate() error {
	switch {
	case !observation.Source.IsValid():
		return errors.New("memory candidacy observation: source is not a declared evidence source kind")
	case !observation.ProducedWorkAttributed.IsValid():
		return errors.New("memory candidacy observation: produced-work attribution is not declared")
	case !observation.RepositoryTextApproval.IsValid():
		return errors.New("memory candidacy observation: repository-text approval is not declared")
	case observation.MechanicalRuleExpressible && strings.TrimSpace(observation.MechanicalRuleWhichRule) == "":
		return errors.New("memory candidacy observation: mechanical-rule expressibility requires naming which rule expresses it")
	default:
		return nil
	}
}

// Classified refusal reasons this file's funnel returns. Every one is a
// sentinel wrapped with errors.Is-compatible detail via fmt.Errorf's %w, so
// a caller can branch on which admissibility rule fired without parsing
// message text.
var (
	// ErrMemoryCandidacyAgentSelfReportInadmissible classifies a candidacy
	// attempt whose only Source is domain.EvidenceSourceKindAgentSelfReport.
	// Per §31, "agent_self_report ... carries evidence_strength: none and
	// cannot be repaired by being restated" -- MEM-013 requires this
	// refusal at the point of candidacy, not only at promotion.
	ErrMemoryCandidacyAgentSelfReportInadmissible = errors.New("memory candidacy: agent self-report can never become a memory-artifact candidate")

	// ErrMemoryCandidacyUnattributedProducedWork classifies a candidacy
	// attempt whose produced work was not attributed to the declarations
	// this run changed. Per §31, "the observation would be about code the
	// run never touched."
	ErrMemoryCandidacyUnattributedProducedWork = errors.New("memory candidacy: produced work was not attributed to the declarations this run changed")

	// ErrMemoryCandidacyUnapprovedRepositoryText classifies a candidacy
	// attempt sourced from repository text no person approved. Per §31,
	// this is "untrusted input reaching the product's own memory." This
	// file never grants the approval a caller would need to clear this
	// refusal -- see internal/coordinator/episode_lifecycle.go's MEM-009
	// handoff note; inventing an approval-granting path here would open
	// exactly the same prompt-injection route that ticket was deliberately
	// left uncalled to avoid.
	ErrMemoryCandidacyUnapprovedRepositoryText = errors.New("memory candidacy: repository text with no recorded user approval can never become a memory-artifact candidate")

	// ErrMemoryCandidacyUncontestedFirstAttempt classifies a candidacy
	// attempt at a tier that requires something to have been contested --
	// advisory pattern, regression case, or routing evidence about where a
	// run stalled -- from an observation whose Contested field is false
	// (MEM-013a).
	ErrMemoryCandidacyUncontestedFirstAttempt = errors.New("memory candidacy: this tier requires something to have been contested, and the observation reports a first-attempt success")

	// ErrMemoryCandidacyTierNotExpressible classifies a requested tier the
	// observation's own expressibility fields report false for: minting at
	// a tier that could not actually express what was observed would claim
	// more structure than the observation carries.
	ErrMemoryCandidacyTierNotExpressible = errors.New("memory candidacy: the requested tier cannot express this observation")

	// ErrMemoryCandidacyWeakerTierUnjustified classifies a requested tier
	// weaker than the strongest one the observation could be expressed at,
	// carrying no recorded reason the stronger tier could not carry it
	// (MEM-012).
	ErrMemoryCandidacyWeakerTierUnjustified = errors.New("memory candidacy: a stronger tier could express this observation and no reason was recorded for choosing a weaker one")
)

// RefuseInadmissibleMemoryCandidacySource applies MEM-013: it returns a
// classified error for exactly the three sources §31 names as never
// becoming a candidate at any tier -- agent_self_report, unattributed
// produced work, and unapproved repository text -- and nil otherwise. It is
// checked first by DetermineMemoryCandidacyTier so no tier-selection or
// first-attempt reasoning ever runs on an inadmissible observation, and it
// is also exported for standalone use by a caller that only needs the
// admissibility question answered.
func RefuseInadmissibleMemoryCandidacySource(observation MemoryCandidacyObservation) error {
	if err := observation.Validate(); err != nil {
		return err
	}
	switch {
	case observation.Source == domain.EvidenceSourceKindAgentSelfReport:
		return ErrMemoryCandidacyAgentSelfReportInadmissible
	case observation.ProducedWorkAttributed == MemoryCandidacyProducedWorkUnattributed:
		return ErrMemoryCandidacyUnattributedProducedWork
	case observation.RepositoryTextApproval == MemoryCandidacyRepositoryTextUnapproved:
		return ErrMemoryCandidacyUnapprovedRepositoryText
	default:
		return nil
	}
}

// SelectStrongestExpressibleMemoryCandidacyTier applies MEM-012's ordering:
// it returns the first tier, strongest to weakest, that observation's own
// expressibility fields report true for. Because Advisory pattern is always
// expressible (§31: "everything that survives the four tiers above"), this
// never fails to find a tier for a Validate-passing observation; the
// trailing error return exists only for a Validate failure and the
// defensive unreachable case documented on its own line below.
func SelectStrongestExpressibleMemoryCandidacyTier(observation MemoryCandidacyObservation) (MemoryCandidacyTier, error) {
	if err := observation.Validate(); err != nil {
		return "", err
	}
	for _, tier := range memoryCandidacyTierStrength {
		if observation.expressibleAtTier(tier) {
			return tier, nil
		}
	}
	// Unreachable while MemoryCandidacyTierAdvisoryPattern's expressibleAtTier
	// case returns true unconditionally, above. Kept as an explicit, typed
	// failure rather than a panic, per AGENTS.md "return typed domain
	// errors" -- a future edit narrowing that default case must fail loudly
	// here, not silently mint nothing.
	return "", errors.New("memory candidacy: no declared tier, including advisory pattern, reports itself expressible")
}

// MemoryCandidacyProposal is one caller's request to mint observation as a
// memory-artifact candidate, optionally at a specific tier.
// RequestedTier's zero value means "no preference: use the strongest
// expressible tier," which is the path every ordinary extractor should use.
// A caller that names a specific tier weaker than the strongest expressible
// one must also supply a non-empty WeakerTierJustification recording why the
// stronger tier could not carry this observation (MEM-012); an empty
// justification for a genuinely weaker request is refused.
type MemoryCandidacyProposal struct {
	Observation             MemoryCandidacyObservation
	RequestedTier           MemoryCandidacyTier
	WeakerTierJustification string
}

// DetermineMemoryCandidacyTier is the MEM-012/MEM-013/MEM-013a funnel entry
// point every extractor should route an observation through before minting
// anything into project memory. It checks, in order:
//
//  1. MEM-013 source admissibility (RefuseInadmissibleMemoryCandidacySource)
//     -- an inadmissible observation is refused before any tier reasoning
//     runs, so a self-report, an unattributed, or an unapproved observation
//     is refused identically whether or not a caller ever asks for a
//     specific tier;
//  2. that a named RequestedTier can actually express the observation
//     (ErrMemoryCandidacyTierNotExpressible);
//  3. MEM-012: that a RequestedTier weaker than the strongest expressible
//     tier carries a recorded WeakerTierJustification
//     (ErrMemoryCandidacyWeakerTierUnjustified);
//  4. MEM-013a: that the resulting tier's first-attempt requirement is met
//     (ErrMemoryCandidacyUncontestedFirstAttempt).
//
// On success it returns the tier the candidate should be minted at: either
// the strongest expressible tier (RequestedTier left at its zero value) or
// the justified RequestedTier.
func DetermineMemoryCandidacyTier(proposal MemoryCandidacyProposal) (MemoryCandidacyTier, error) {
	if err := RefuseInadmissibleMemoryCandidacySource(proposal.Observation); err != nil {
		return "", err
	}
	strongest, err := SelectStrongestExpressibleMemoryCandidacyTier(proposal.Observation)
	if err != nil {
		return "", err
	}

	resolved := strongest
	if proposal.RequestedTier != "" && proposal.RequestedTier != strongest {
		if !proposal.Observation.expressibleAtTier(proposal.RequestedTier) {
			return "", fmt.Errorf("%w: %q", ErrMemoryCandidacyTierNotExpressible, proposal.RequestedTier)
		}
		switch {
		case proposal.RequestedTier.weakerThan(strongest):
			if strings.TrimSpace(proposal.WeakerTierJustification) == "" {
				return "", fmt.Errorf("%w: requested %q but %q could express this observation",
					ErrMemoryCandidacyWeakerTierUnjustified, proposal.RequestedTier, strongest)
			}
		case strongest.weakerThan(proposal.RequestedTier):
			// Defensive only: SelectStrongestExpressibleMemoryCandidacyTier
			// already returns the FIRST expressible tier in strength order, so
			// no expressible tier can be stronger than strongest. Reaching this
			// branch would mean expressibleAtTier and the strength ordering
			// disagreed with each other.
			return "", fmt.Errorf("%w: %q is stronger than the strongest expressible tier %q",
				ErrMemoryCandidacyTierNotExpressible, proposal.RequestedTier, strongest)
		}
		resolved = proposal.RequestedTier
	}

	if requiresContestedFirstAttempt(resolved) && !proposal.Observation.Contested {
		return "", fmt.Errorf("%w: tier %q", ErrMemoryCandidacyUncontestedFirstAttempt, resolved)
	}
	return resolved, nil
}
