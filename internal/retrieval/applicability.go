package retrieval

import (
	"encoding/json"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/retrievalgate"
	"codeflux.dev/codeflux/internal/storage"
)

// The JSON payload shapes below are this package's canonical encoding for
// storage.CreateMemoryArtifactApplicabilityPredicate.Predicate (an `any`
// field storage marshals verbatim). No other lane writes these predicates
// yet, so this package -- the first and only decoder -- defines the wire
// shape; a future writer (for example the M21-040..050 fact-extraction
// lane) must encode to this same shape for EvaluateApplicabilityPredicate
// to interpret it correctly.

type repositoryMatchPredicatePayload struct {
	Repository string `json:"repository"`
}

type dependencyBindingPredicatePayload struct {
	RequiredBindings []fingerprint.ToolchainBinding `json:"required_bindings"`
}

type authorityRequirementPredicatePayload struct {
	RequiredAuthority []string `json:"required_authority"`
}

type pathScopePredicatePayload struct {
	PathPrefixes []string `json:"path_prefixes"`
}

// decodeApplicabilityPredicate converts one stored, generically-JSON-
// encoded applicability-predicate record into internal/retrievalgate's
// typed ApplicabilityPredicate (M21-067: "route through
// EvaluateApplicabilityPredicate -- never a similarity threshold"). Kind
// and UnknownFieldBehavior mirror storage's wire vocabulary by identical
// string value (see internal/retrievalgate/doc.go), so those two fields
// convert by simple cast; only the free-form Predicate JSON payload needs
// shape-specific decoding, per PredicateKind. A custom predicate's payload
// is deliberately never decoded: EvaluateApplicabilityPredicate itself
// refuses to interpret ApplicabilityPredicateKindCustom, resolving it
// through UnknownFieldBehavior instead.
func decodeApplicabilityPredicate(record storage.MemoryArtifactApplicabilityPredicate) (retrievalgate.ApplicabilityPredicate, error) {
	predicate := retrievalgate.ApplicabilityPredicate{
		Kind:                     retrievalgate.ApplicabilityPredicateKind(record.PredicateKind),
		FingerprintSchemaVersion: uint32(record.FingerprintSchemaVersion),
		UnknownFieldBehavior:     retrievalgate.ApplicabilityUnknownFieldBehavior(record.UnknownFieldBehavior),
	}
	switch record.PredicateKind {
	case storage.ApplicabilityPredicateRepositoryMatch:
		var payload repositoryMatchPredicatePayload
		if err := json.Unmarshal([]byte(record.PredicateJSON), &payload); err != nil {
			return retrievalgate.ApplicabilityPredicate{}, fmt.Errorf("decode repository-match applicability predicate %s: %w", record.ID, err)
		}
		repositoryID, err := domain.ParseRepositoryID(payload.Repository)
		if err != nil {
			return retrievalgate.ApplicabilityPredicate{}, fmt.Errorf("decode repository-match applicability predicate %s: %w", record.ID, err)
		}
		predicate.RepositoryMatch = repositoryID

	case storage.ApplicabilityPredicateDependencyBinding:
		var payload dependencyBindingPredicatePayload
		if err := json.Unmarshal([]byte(record.PredicateJSON), &payload); err != nil {
			return retrievalgate.ApplicabilityPredicate{}, fmt.Errorf("decode dependency-binding applicability predicate %s: %w", record.ID, err)
		}
		predicate.RequiredBindings = payload.RequiredBindings

	case storage.ApplicabilityPredicateCapabilityRequirement, storage.ApplicabilityPredicateEffectRequirement:
		var payload authorityRequirementPredicatePayload
		if err := json.Unmarshal([]byte(record.PredicateJSON), &payload); err != nil {
			return retrievalgate.ApplicabilityPredicate{}, fmt.Errorf("decode capability/effect-requirement applicability predicate %s: %w", record.ID, err)
		}
		predicate.RequiredAuthority = make([]fingerprint.AuthorityClass, 0, len(payload.RequiredAuthority))
		for _, value := range payload.RequiredAuthority {
			predicate.RequiredAuthority = append(predicate.RequiredAuthority, fingerprint.AuthorityClass(value))
		}

	case storage.ApplicabilityPredicatePathScope:
		var payload pathScopePredicatePayload
		if err := json.Unmarshal([]byte(record.PredicateJSON), &payload); err != nil {
			return retrievalgate.ApplicabilityPredicate{}, fmt.Errorf("decode path-scope applicability predicate %s: %w", record.ID, err)
		}
		predicate.PathPrefixes = payload.PathPrefixes

	case storage.ApplicabilityPredicateCustom:
		// Deliberately not decoded; see the function comment above.

	default:
		return retrievalgate.ApplicabilityPredicate{}, fmt.Errorf("applicability predicate %s has an undeclared predicate kind %q", record.ID, record.PredicateKind)
	}
	return predicate, nil
}
