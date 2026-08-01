package retrievalgate

import (
	"strconv"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
)

// EligibilityCandidate is the ONLY input EvaluateEligibility (and the gate
// predicates it composes) can see. Deliberately by construction, this type
// has:
//
//   - no field capable of holding a similarity score, a rank, or a
//     discovery channel (those live exclusively on DiscoveredCandidate,
//     provenance.go, a type EvaluateEligibility never accepts anywhere in
//     its signature) -- the structural half of M21-077;
//   - no field capable of holding documentation text, a comment, or any
//     other descriptive-retrieval material (those live in
//     internal/atomdoc.Document and internal/fingerprint.
//     DescriptiveRetrievalText, neither of which this package imports) --
//     the structural half of M21-145.
//
// See structural_test.go for a reflection-based test that fails if either
// property regresses.
type EligibilityCandidate struct {
	// RevisionID identifies the candidate for logging/tracing only. It
	// carries no eligibility weight: EvaluateEligibility never branches on
	// it.
	RevisionID domain.MemoryArtifactRevisionID

	// Scope is the candidate's owning project/repository, checked by
	// EvaluateProjectBoundary (M21-068).
	Scope domain.MemoryProjectScope

	// RequiredToolchainBindings and RequiredDependencyBindings are checked
	// by EvaluateToolchainCompatibility and EvaluateDependencyCompatibility
	// respectively (M21-069).
	RequiredToolchainBindings  []fingerprint.ToolchainBinding
	RequiredDependencyBindings []fingerprint.ToolchainBinding

	// Applicability is every M21-022 structured predicate bound to this
	// candidate's revision, checked in order by EvaluateApplicabilityPredicate
	// (M21-136). A candidate with no predicates trivially passes this gate:
	// absence of a declared constraint is not the same as failing one.
	Applicability []ApplicabilityPredicate

	// Maturity and SupportingEvidenceAssurance are checked by
	// EvaluateInvalidatedEvidence (M21-070) and, via
	// AchievedAssuranceFromEvidence, by EvaluateAssurance (M21-071).
	// SupportingEvidenceAssurance is the CURRENT (possibly re-derived per
	// the Assurance Evidence Failure protocol) assurance level of every
	// piece of evidence linked to this candidate's revision -- never a
	// value read off documentation.
	Maturity                    domain.MaturityState
	SupportingEvidenceAssurance []domain.AssuranceLevel
}

// NamedGateOutcome pairs one gate's name with its GateOutcome, for the full
// audit trail EligibilityDecision.GateResults carries (M21-072: "record
// every accepted and rejected retrieval candidate with reason" is easiest
// to honor faithfully when every gate's individual result -- not only the
// one that finally decided the outcome -- is preserved for inspection).
type NamedGateOutcome struct {
	Gate    string
	Outcome GateOutcome
}

// EligibilityDecision is the terminal, typed result of running every
// required gate against one EligibilityCandidate. Reason and Detail are the
// zero value whenever Eligible is true.
type EligibilityDecision struct {
	Eligible    bool
	Reason      RejectionReason
	Detail      string
	GateResults []NamedGateOutcome
}

// EvaluateEligibility runs every required gate against candidate, in the
// fixed order M21-136 declares -- project, compatibility (toolchain then
// dependency), applicability, evidence, assurance -- and returns the
// aggregate decision (M21-136: "Require project, compatibility,
// applicability, evidence, and assurance gates to run AFTER candidate
// discovery. Discovery and eligibility are different phases and must not be
// collapsible.").
//
// Every gate always runs and is recorded in GateResults, even after an
// earlier gate has already failed, so the full picture is available for
// audit; the returned Reason is the first failing gate in the declared
// order, matching storage's single reason_kind column. When every gate
// passes, Eligible is true and Reason is the zero value: eligibility alone
// never implies the agent used, adapted, or rejected the candidate -- see
// RecordAgentInfluence in provenance.go for that separate, later fact
// (M21-138).
//
// task must already be a valid ExactFingerprint (BuildExactFingerprint or a
// value round-tripped through storage); an invalid fingerprint or candidate
// is a caller precondition failure, returned as an error rather than a
// decision.
func EvaluateEligibility(candidate EligibilityCandidate, boundary domain.MemoryQueryProjectBoundary, task fingerprint.ExactFingerprint) (EligibilityDecision, error) {
	if err := task.Validate(); err != nil {
		return EligibilityDecision{}, err
	}
	if !candidate.Maturity.IsValid() {
		return EligibilityDecision{}, fieldError("retrievalgate.eligibility.maturity", "is not a declared maturity state")
	}

	var trace []NamedGateOutcome
	record := func(name string, outcome GateOutcome) GateOutcome {
		trace = append(trace, NamedGateOutcome{Gate: name, Outcome: outcome})
		return outcome
	}

	project, err := EvaluateProjectBoundary(boundary, candidate.Scope)
	if err != nil {
		return EligibilityDecision{}, err
	}
	project = record("project-boundary", project)

	toolchain := record("toolchain-compatibility", EvaluateToolchainCompatibility(candidate.RequiredToolchainBindings, task.Bindings))
	dependency := record("dependency-compatibility", EvaluateDependencyCompatibility(candidate.RequiredDependencyBindings, task.Bindings))

	applicability := eligible()
	for index, predicate := range candidate.Applicability {
		outcome, err := EvaluateApplicabilityPredicate(predicate, task)
		if err != nil {
			return EligibilityDecision{}, err
		}
		trace = append(trace, NamedGateOutcome{Gate: applicabilityGateName(index), Outcome: outcome})
		if !outcome.Eligible {
			applicability = outcome
			break
		}
	}

	evidence, err := EvaluateInvalidatedEvidence(candidate.Maturity, candidate.SupportingEvidenceAssurance)
	if err != nil {
		return EligibilityDecision{}, err
	}
	evidence = record("invalidated-evidence", evidence)

	achieved := AchievedAssuranceFromEvidence(candidate.SupportingEvidenceAssurance)
	assurance, err := EvaluateAssurance(achieved, task.RequiredAssurance)
	if err != nil {
		return EligibilityDecision{}, err
	}
	assurance = record("assurance-below-requirement", assurance)

	for _, outcome := range []GateOutcome{project, toolchain, dependency, applicability, evidence, assurance} {
		if !outcome.Eligible {
			return EligibilityDecision{Eligible: false, Reason: outcome.Reason, Detail: outcome.Detail, GateResults: trace}, nil
		}
	}
	return EligibilityDecision{Eligible: true, GateResults: trace}, nil
}

func applicabilityGateName(index int) string {
	return "applicability[" + strconv.Itoa(index) + "]"
}
