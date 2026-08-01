package retrievalgate

import (
	"errors"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
)

// -----------------------------------------------------------------------
// M21-068: reject on project-boundary mismatch
// -----------------------------------------------------------------------

// EvaluateProjectBoundary rejects a candidate whose owning scope falls
// outside the query's project boundary (M21-068). It is a thin, individually
// testable wrapper over domain.RequireMemoryQueryProjectBoundary (already
// the one call every memory-query implementation routes cross-project
// isolation through); this function's only job is to turn that domain-level
// check into a typed GateOutcome for the eligibility suite. A malformed
// boundary or scope (not a real decision, a caller precondition failure) is
// returned as an error, distinct from a legitimate rejection.
func EvaluateProjectBoundary(boundary domain.MemoryQueryProjectBoundary, scope domain.MemoryProjectScope) (GateOutcome, error) {
	err := domain.RequireMemoryQueryProjectBoundary(boundary, scope)
	if err == nil {
		return eligible(), nil
	}
	if errors.Is(err, domain.ErrCrossProjectMemoryAccess) {
		return rejected(RejectionReasonProjectBoundaryMismatch, "candidate project scope is outside the query's project boundary"), nil
	}
	return GateOutcome{}, err
}

// -----------------------------------------------------------------------
// M21-069: reject on toolchain/dependency mismatch
// -----------------------------------------------------------------------

// bindingsSatisfied reports whether every entry in required has a
// same-Name, same-Version match in available, per fingerprint.go's exact
// Name+Version binding shape. Per the codebase-wide "unknown is never
// treated as compatible" pattern (RevisionBinding Known/UnknownReason,
// fingerprint's non-nil-list requirement), a required binding whose Name is
// simply absent from available is a mismatch, not a silent pass; so is a
// present Name pinned to a different Version. It returns the first
// unsatisfied required binding for the caller's Detail text.
func bindingsSatisfied(required, available []fingerprint.ToolchainBinding) (bool, fingerprint.ToolchainBinding) {
	byName := make(map[string]string, len(available))
	for _, binding := range available {
		byName[binding.Name] = binding.Version
	}
	for _, need := range required {
		version, present := byName[need.Name]
		if !present || version != need.Version {
			return false, need
		}
	}
	return true, fingerprint.ToolchainBinding{}
}

// EvaluateToolchainCompatibility rejects a candidate that requires a
// language/toolchain binding the task fingerprint's Bindings does not
// currently satisfy at the exact pinned version (M21-069). required is the
// candidate's declared toolchain requirements; available is the task
// fingerprint's ExactFingerprint.Bindings.
func EvaluateToolchainCompatibility(required, available []fingerprint.ToolchainBinding) GateOutcome {
	if ok, missing := bindingsSatisfied(required, available); !ok {
		return rejected(RejectionReasonToolchainMismatch, "required toolchain binding "+missing.Name+"@"+missing.Version+" is not satisfied by the task fingerprint")
	}
	return eligible()
}

// EvaluateDependencyCompatibility rejects a candidate that requires a
// dependency binding the task fingerprint's Bindings does not currently
// satisfy at the exact pinned version (M21-069). It is deliberately a
// separate, individually testable function and typed reason from
// EvaluateToolchainCompatibility, even though both compare the same
// fingerprint.ToolchainBinding shape: fingerprint.go's M21-056 field
// deliberately combines "language/toolchain/dependency bindings" into one
// list, so the toolchain/dependency distinction is drawn by which
// requirement set the caller places in required, not by the binding shape
// itself.
func EvaluateDependencyCompatibility(required, available []fingerprint.ToolchainBinding) GateOutcome {
	if ok, missing := bindingsSatisfied(required, available); !ok {
		return rejected(RejectionReasonDependencyMismatch, "required dependency binding "+missing.Name+"@"+missing.Version+" is not satisfied by the task fingerprint")
	}
	return eligible()
}

// -----------------------------------------------------------------------
// M21-070: reject on invalidated evidence
// -----------------------------------------------------------------------

// EvaluateInvalidatedEvidence rejects a candidate whose supporting evidence
// has failed (M21-070), per docs/plan.md §31 "Assurance Evidence Failure":
// "When evidence fails: 1. suspend the affected evidence version... 2. mark
// the supported obligation unresolved." Two independent signals both
// invalidate a candidate:
//
//   - its memory-artifact revision's own maturity is Quarantined or
//     Invalidated (domain.MaturityState; the Artifact Failure Protocol
//     terminal-for-authority states caused by an evidence failure), or
//   - any of its linked supporting evidence currently carries
//     domain.AssuranceLevelInvalidated (a downstream re-derivation from
//     the Assurance Evidence Failure protocol reached this specific
//     evidence record).
//
// maturity must be a declared domain.MaturityState; an invalid value is a
// caller precondition failure, returned as an error rather than a decision.
func EvaluateInvalidatedEvidence(maturity domain.MaturityState, supportingEvidenceAssurance []domain.AssuranceLevel) (GateOutcome, error) {
	if !maturity.IsValid() {
		return GateOutcome{}, fieldError("retrievalgate.invalidated_evidence.maturity", "is not a declared maturity state")
	}
	if maturity == domain.MaturityStateQuarantined || maturity == domain.MaturityStateInvalidated {
		return rejected(RejectionReasonInvalidatedEvidence, "candidate memory-artifact revision maturity is "+string(maturity)), nil
	}
	for _, level := range supportingEvidenceAssurance {
		if !level.IsValid() {
			return GateOutcome{}, fieldError("retrievalgate.invalidated_evidence.supporting_evidence_assurance", "contains an undeclared assurance level")
		}
		if level == domain.AssuranceLevelInvalidated {
			return rejected(RejectionReasonInvalidatedEvidence, "candidate has supporting evidence currently carrying assurance level invalidated"), nil
		}
	}
	return eligible(), nil
}

// -----------------------------------------------------------------------
// M21-071: reject when assurance is below the current task requirement
// -----------------------------------------------------------------------

// assuranceRank orders domain.AssuranceLevel from weakest (0) to strongest,
// mirroring the strength ordering docs/plan.md §22 and §31 use for
// guarantee downgrade paths (fully-evaluated is the strongest achievable
// claim; runtime-only the weakest still-achievable claim).
// AssuranceLevelInvalidated has no rank: it is never an achieved level, only
// a failure marker handled by EvaluateInvalidatedEvidence, so it (and any
// undeclared value) always reports ok=false here and therefore can never
// satisfy any requirement in EvaluateAssurance.
func assuranceRank(level domain.AssuranceLevel) (rank int, ok bool) {
	switch level {
	case domain.AssuranceLevelRuntimeOnly:
		return 0, true
	case domain.AssuranceLevelContractChecked:
		return 1, true
	case domain.AssuranceLevelModelVerified:
		return 2, true
	case domain.AssuranceLevelFullyEvaluated:
		return 3, true
	default:
		return -1, false
	}
}

// EvaluateAssurance rejects a candidate whose achieved assurance level is
// weaker than required (M21-071). Its signature accepts only two
// domain.AssuranceLevel values -- nothing else can be passed here, in
// particular no atomdoc.Document, no free-form text, and no candidate
// struct of any kind, which is the structural half of M21-145 ("a richly
// documented atom cannot self-promote its assurance level"): there is no
// parameter through which documentation could reach this decision. The
// other half -- computing achieved honestly from real evidence, never from
// documentation -- is AchievedAssuranceFromEvidence's job; see
// structural_test.go for both properties proven by reflection.
func EvaluateAssurance(achieved, required domain.AssuranceLevel) (GateOutcome, error) {
	if !required.IsValid() || required == domain.AssuranceLevelInvalidated {
		return GateOutcome{}, fieldError("retrievalgate.assurance.required", "must be a valid non-invalidated level")
	}
	requiredRank, _ := assuranceRank(required) // required already validated non-invalidated and declared, so ok is always true here
	achievedRank, ok := assuranceRank(achieved)
	if !ok || achievedRank < requiredRank {
		return rejected(RejectionReasonAssuranceBelowRequirement, "candidate achieved assurance "+string(achieved)+" does not meet required assurance "+string(required)), nil
	}
	return eligible(), nil
}

// AchievedAssuranceFromEvidence derives one candidate's achieved assurance
// level purely from its linked supporting evidence's currently known
// assurance levels -- the strongest non-invalidated level present, or
// domain.AssuranceLevelInvalidated if evidence is invalidated
// (EvaluateInvalidatedEvidence rejects that case separately) or
// domain.AssuranceLevelRuntimeOnly-and-below is unavailable because there is
// no evidence at all, in which case it returns the weakest declared level so
// EvaluateAssurance fails closed rather than passing an assurance
// requirement with no evidence behind it. This is the ONLY place in this
// package that produces an "achieved assurance" value, and its input type
// is exactly []domain.AssuranceLevel: there is no documentation-shaped
// parameter anywhere on this function, so a richly documented but weakly
// evidenced candidate always resolves to the same achieved level as a bare
// one with identical evidence (M21-145; see structural_test.go).
func AchievedAssuranceFromEvidence(supportingEvidenceAssurance []domain.AssuranceLevel) domain.AssuranceLevel {
	if len(supportingEvidenceAssurance) == 0 {
		return domain.AssuranceLevelRuntimeOnly
	}
	best := domain.AssuranceLevelInvalidated
	bestRank := -1
	for _, level := range supportingEvidenceAssurance {
		if level == domain.AssuranceLevelInvalidated {
			continue
		}
		rank, ok := assuranceRank(level)
		if !ok {
			continue
		}
		if rank > bestRank {
			bestRank = rank
			best = level
		}
	}
	if bestRank < 0 {
		// Every supplied level was invalidated or undeclared: there is no
		// honest non-invalidated achieved level to report.
		return domain.AssuranceLevelInvalidated
	}
	return best
}

// -----------------------------------------------------------------------
// Applicability predicates (M21-022 structured records; M21-136 requires
// this gate to run, in order, after candidate discovery)
// -----------------------------------------------------------------------

// ApplicabilityPredicateKind classifies the shape a stored applicability
// predicate checks. It mirrors
// storage.MemoryArtifactApplicabilityPredicateKind's wire vocabulary by
// string value, deliberately without importing internal/storage; see doc.go.
type ApplicabilityPredicateKind string

const (
	ApplicabilityPredicateKindRepositoryMatch       ApplicabilityPredicateKind = "repository-match"
	ApplicabilityPredicateKindDependencyBinding     ApplicabilityPredicateKind = "dependency-binding"
	ApplicabilityPredicateKindCapabilityRequirement ApplicabilityPredicateKind = "capability-requirement"
	ApplicabilityPredicateKindEffectRequirement     ApplicabilityPredicateKind = "effect-requirement"
	ApplicabilityPredicateKindPathScope             ApplicabilityPredicateKind = "path-scope"
	ApplicabilityPredicateKindCustom                ApplicabilityPredicateKind = "custom"
)

func (value ApplicabilityPredicateKind) isValid() bool {
	switch value {
	case ApplicabilityPredicateKindRepositoryMatch,
		ApplicabilityPredicateKindDependencyBinding,
		ApplicabilityPredicateKindCapabilityRequirement,
		ApplicabilityPredicateKindEffectRequirement,
		ApplicabilityPredicateKindPathScope,
		ApplicabilityPredicateKindCustom:
		return true
	default:
		return false
	}
}

// ApplicabilityUnknownFieldBehavior declares how a predicate behaves when
// the fingerprint schema version it was authored against no longer matches
// the task's, per docs/plan.md §31 "Every predicate declares its expected
// fingerprint-schema version... and behavior when fields are absent" and
// "Incompatible schema versions never match silently." It mirrors
// storage.MemoryArtifactApplicabilityUnknownFieldBehavior's wire vocabulary
// by string value, deliberately without importing internal/storage.
type ApplicabilityUnknownFieldBehavior string

const (
	ApplicabilityUnknownFieldReject ApplicabilityUnknownFieldBehavior = "reject"
	ApplicabilityUnknownFieldSkip   ApplicabilityUnknownFieldBehavior = "skip"
	ApplicabilityUnknownFieldWarn   ApplicabilityUnknownFieldBehavior = "warn"
)

func (value ApplicabilityUnknownFieldBehavior) isValid() bool {
	switch value {
	case ApplicabilityUnknownFieldReject, ApplicabilityUnknownFieldSkip, ApplicabilityUnknownFieldWarn:
		return true
	default:
		return false
	}
}

// ApplicabilityPredicate is this package's pure, decoded form of one stored
// M21-022 applicability-predicate record. Exactly the field(s) matching
// Kind are meaningful; EvaluateApplicabilityPredicate reports a malformed
// input error otherwise. FingerprintSchemaVersion is the schema version the
// predicate was authored against; a mismatch against the task fingerprint's
// own schema version is resolved by UnknownFieldBehavior rather than a
// silent reinterpretation.
type ApplicabilityPredicate struct {
	Kind                     ApplicabilityPredicateKind
	FingerprintSchemaVersion uint32
	UnknownFieldBehavior     ApplicabilityUnknownFieldBehavior

	// RepositoryMatch is meaningful only when Kind is
	// ApplicabilityPredicateKindRepositoryMatch.
	RepositoryMatch domain.RepositoryID
	// RequiredBindings is meaningful only when Kind is
	// ApplicabilityPredicateKindDependencyBinding: every listed binding
	// must be satisfied by the task fingerprint's Bindings.
	RequiredBindings []fingerprint.ToolchainBinding
	// RequiredAuthority is meaningful only when Kind is
	// ApplicabilityPredicateKindCapabilityRequirement or
	// ApplicabilityPredicateKindEffectRequirement: every listed authority
	// class must be present in the task fingerprint's RequestedAuthority,
	// so the candidate never assumes authority the task never requested.
	RequiredAuthority []fingerprint.AuthorityClass
	// PathPrefixes is meaningful only when Kind is
	// ApplicabilityPredicateKindPathScope: every one of the task
	// fingerprint's AffectedPaths must match at least one declared prefix.
	PathPrefixes []string
}

// EvaluateApplicabilityPredicate deterministically evaluates one stored
// applicability predicate against the task's exact fingerprint (M21-136).
// It performs no vector similarity and consults no descriptive or
// documentation text. A malformed predicate (undeclared Kind or
// UnknownFieldBehavior, or a Kind whose required payload field is empty) is
// a caller precondition failure, returned as an error rather than a
// decision, per docs/plan.md §31 "Applicability conditions must be
// deterministic predicates rather than prose": an uninterpretable predicate
// must never be silently treated as passing or failing.
func EvaluateApplicabilityPredicate(predicate ApplicabilityPredicate, task fingerprint.ExactFingerprint) (GateOutcome, error) {
	if !predicate.Kind.isValid() {
		return GateOutcome{}, fieldError("retrievalgate.applicability.kind", "is not declared")
	}
	if !predicate.UnknownFieldBehavior.isValid() {
		return GateOutcome{}, fieldError("retrievalgate.applicability.unknown_field_behavior", "is not declared")
	}
	if predicate.FingerprintSchemaVersion != task.SchemaVersion {
		return applyUnknownFieldBehavior(predicate.UnknownFieldBehavior, "predicate fingerprint schema version does not match the task fingerprint's schema version"), nil
	}
	if predicate.Kind == ApplicabilityPredicateKindCustom {
		// A custom predicate's semantics are not interpretable by this
		// generic evaluator; never silently reinterpret it as passing.
		return applyUnknownFieldBehavior(predicate.UnknownFieldBehavior, "custom applicability predicates are not mechanically interpretable by this evaluator"), nil
	}

	switch predicate.Kind {
	case ApplicabilityPredicateKindRepositoryMatch:
		if predicate.RepositoryMatch.IsZero() {
			return GateOutcome{}, fieldError("retrievalgate.applicability.repository_match", "must not be empty for a repository-match predicate")
		}
		if predicate.RepositoryMatch != task.Repository {
			return rejected(RejectionReasonApplicabilityPredicateFailed, "candidate is scoped to a different repository than the task fingerprint"), nil
		}
		return eligible(), nil

	case ApplicabilityPredicateKindDependencyBinding:
		if len(predicate.RequiredBindings) == 0 {
			return GateOutcome{}, fieldError("retrievalgate.applicability.required_bindings", "must not be empty for a dependency-binding predicate")
		}
		if ok, missing := bindingsSatisfied(predicate.RequiredBindings, task.Bindings); !ok {
			return rejected(RejectionReasonApplicabilityPredicateFailed, "required dependency binding "+missing.Name+"@"+missing.Version+" is not satisfied by the task fingerprint"), nil
		}
		return eligible(), nil

	case ApplicabilityPredicateKindCapabilityRequirement, ApplicabilityPredicateKindEffectRequirement:
		if len(predicate.RequiredAuthority) == 0 {
			return GateOutcome{}, fieldError("retrievalgate.applicability.required_authority", "must not be empty for a capability/effect-requirement predicate")
		}
		granted := make(map[fingerprint.AuthorityClass]struct{}, len(task.RequestedAuthority))
		for _, class := range task.RequestedAuthority {
			granted[class] = struct{}{}
		}
		var missing []fingerprint.AuthorityClass
		for _, need := range predicate.RequiredAuthority {
			if _, ok := granted[need]; !ok {
				missing = append(missing, need)
			}
		}
		if len(missing) > 0 {
			names := make([]string, len(missing))
			for i, class := range missing {
				names[i] = string(class)
			}
			sort.Strings(names)
			return rejected(RejectionReasonApplicabilityPredicateFailed, "candidate requires authority not requested by the task: "+strings.Join(names, ", ")), nil
		}
		return eligible(), nil

	case ApplicabilityPredicateKindPathScope:
		if len(predicate.PathPrefixes) == 0 {
			return GateOutcome{}, fieldError("retrievalgate.applicability.path_prefixes", "must not be empty for a path-scope predicate")
		}
		for _, path := range task.AffectedPaths {
			if !matchesAnyPrefix(path, predicate.PathPrefixes) {
				return rejected(RejectionReasonApplicabilityPredicateFailed, "task affected path "+path+" is outside the candidate's declared path scope"), nil
			}
		}
		return eligible(), nil

	default:
		// Unreachable given the isValid()/Custom checks above; fail closed
		// rather than silently pass an unrecognized kind.
		return rejected(RejectionReasonApplicabilityPredicateFailed, "unrecognized applicability predicate kind"), nil
	}
}

func matchesAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// applyUnknownFieldBehavior resolves an unknown-field situation (a schema
// version mismatch or an uninterpretable custom predicate) into a
// GateOutcome per the predicate's declared behavior: reject fails closed,
// skip passes through with no constraint applied, warn passes through but
// flags the situation in Detail for the caller to surface.
func applyUnknownFieldBehavior(behavior ApplicabilityUnknownFieldBehavior, detail string) GateOutcome {
	switch behavior {
	case ApplicabilityUnknownFieldReject:
		return rejected(RejectionReasonApplicabilityPredicateFailed, detail)
	case ApplicabilityUnknownFieldWarn:
		return GateOutcome{Eligible: true, Detail: "warning: " + detail}
	default: // ApplicabilityUnknownFieldSkip
		return eligible()
	}
}
