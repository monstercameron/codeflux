package retrievalgate

import (
	"reflect"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// This file machine-checks the structural claims doc.go and the individual
// gate/candidate doc comments make in prose, by inspecting Go's own type
// information rather than trusting a comment to stay accurate. If any of
// these tests ever fails, the structural guarantee it names has regressed.

var float64Type = reflect.TypeOf(float64(0))

// typeReachesFloat64 reports whether typ, or anything reachable from typ by
// walking struct fields, slice/array element types, map key/value types, or
// pointer element types, is float64. It is depth-guarded by a visited set
// so a self-referential type can never loop forever (none of the types
// under test are self-referential, but the guard costs nothing).
func typeReachesFloat64(typ reflect.Type, visited map[reflect.Type]bool) bool {
	if typ == nil || visited[typ] {
		return false
	}
	visited[typ] = true
	if typ == float64Type {
		return true
	}
	switch typ.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Chan:
		return typeReachesFloat64(typ.Elem(), visited)
	case reflect.Map:
		return typeReachesFloat64(typ.Key(), visited) || typeReachesFloat64(typ.Elem(), visited)
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			if typeReachesFloat64(typ.Field(i).Type, visited) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// TestEligibilityCandidateCarriesNoSimilarityScore is the structural half
// of M21-077 on the input side: EligibilityCandidate -- the only type
// EvaluateEligibility and every gate predicate in gates.go can see -- must
// have no field, at any depth, of type float64. A similarity score is
// always a float64 (see DiscoveredCandidate.SimilarityScore and storage's
// similarity_score REAL column), so if this test ever fails, some field
// somewhere under EligibilityCandidate could carry one, and M21-077's
// "cannot type-check" claim would no longer be true.
func TestEligibilityCandidateCarriesNoSimilarityScore(t *testing.T) {
	typ := reflect.TypeOf(EligibilityCandidate{})
	if typeReachesFloat64(typ, map[reflect.Type]bool{}) {
		t.Fatal("EligibilityCandidate must not contain a float64-reachable field anywhere; a similarity score must never be representable here")
	}
}

// TestApplicabilityPredicateCarriesNoSimilarityScore extends the same
// check to ApplicabilityPredicate, the payload EligibilityCandidate.
// Applicability carries into EvaluateApplicabilityPredicate.
func TestApplicabilityPredicateCarriesNoSimilarityScore(t *testing.T) {
	typ := reflect.TypeOf(ApplicabilityPredicate{})
	if typeReachesFloat64(typ, map[reflect.Type]bool{}) {
		t.Fatal("ApplicabilityPredicate must not contain a float64-reachable field anywhere")
	}
}

// TestEvaluateEligibilitySignatureCannotAcceptDiscoveryData is the
// structural half of M21-077 on the function-signature side: no parameter
// of EvaluateEligibility can be a DiscoveredCandidate, a float64, or a
// *float64, so a similarity score cannot be passed to it at all -- not
// merely "is not currently passed."
func TestEvaluateEligibilitySignatureCannotAcceptDiscoveryData(t *testing.T) {
	signature := reflect.TypeOf(EvaluateEligibility)
	discoveredCandidateType := reflect.TypeOf(DiscoveredCandidate{})
	float64PtrType := reflect.TypeOf((*float64)(nil))
	for i := 0; i < signature.NumIn(); i++ {
		parameter := signature.In(i)
		if parameter == discoveredCandidateType || parameter == float64Type || parameter == float64PtrType {
			t.Fatalf("EvaluateEligibility parameter %d has type %s, which must never be accepted by an eligibility function", i, parameter)
		}
		if typeReachesFloat64(parameter, map[reflect.Type]bool{}) {
			t.Fatalf("EvaluateEligibility parameter %d (%s) is float64-reachable; a similarity score must never be able to reach an eligibility decision", i, parameter)
		}
	}
}

// TestGateOutcomeFunctionsAcceptNoDiscoveryData proves the same property
// for every individual gate predicate this package exports (068-071 plus
// the applicability evaluator): none of them can accept a
// DiscoveredCandidate, a float64, or a *float64.
func TestGateOutcomeFunctionsAcceptNoDiscoveryData(t *testing.T) {
	discoveredCandidateType := reflect.TypeOf(DiscoveredCandidate{})
	float64PtrType := reflect.TypeOf((*float64)(nil))
	gateFunctions := map[string]any{
		"EvaluateProjectBoundary":         EvaluateProjectBoundary,
		"EvaluateToolchainCompatibility":  EvaluateToolchainCompatibility,
		"EvaluateDependencyCompatibility": EvaluateDependencyCompatibility,
		"EvaluateInvalidatedEvidence":     EvaluateInvalidatedEvidence,
		"EvaluateAssurance":               EvaluateAssurance,
		"EvaluateApplicabilityPredicate":  EvaluateApplicabilityPredicate,
	}
	for name, fn := range gateFunctions {
		signature := reflect.TypeOf(fn)
		for i := 0; i < signature.NumIn(); i++ {
			parameter := signature.In(i)
			if parameter == discoveredCandidateType || parameter == float64Type || parameter == float64PtrType {
				t.Fatalf("%s parameter %d has type %s, which must never be accepted by a gate predicate", name, i, parameter)
			}
		}
	}
}

// TestEvaluateAssuranceSignatureAcceptsOnlyAssuranceLevels is the
// structural half of M21-145: EvaluateAssurance, the function that decides
// whether achieved assurance meets the task's requirement, has exactly two
// domain.AssuranceLevel parameters and nothing else -- in particular no
// atomdoc.Document, no string, and no candidate struct of any kind, so
// documentation richness has no parameter through which it could reach
// this decision.
func TestEvaluateAssuranceSignatureAcceptsOnlyAssuranceLevels(t *testing.T) {
	signature := reflect.TypeOf(EvaluateAssurance)
	assuranceLevelType := reflect.TypeOf(domain.AssuranceLevel(""))
	if signature.NumIn() != 2 {
		t.Fatalf("EvaluateAssurance takes %d parameters, want exactly 2", signature.NumIn())
	}
	for i := 0; i < signature.NumIn(); i++ {
		if signature.In(i) != assuranceLevelType {
			t.Fatalf("EvaluateAssurance parameter %d has type %s, want %s", i, signature.In(i), assuranceLevelType)
		}
	}
}

// TestAchievedAssuranceFromEvidenceSignatureAcceptsOnlyEvidenceLevels is
// the other structural half of M21-145: the ONLY function in this package
// that produces an achieved-assurance value takes exactly one parameter,
// []domain.AssuranceLevel -- never an atomdoc.Document or any documentation
// -shaped type.
func TestAchievedAssuranceFromEvidenceSignatureAcceptsOnlyEvidenceLevels(t *testing.T) {
	signature := reflect.TypeOf(AchievedAssuranceFromEvidence)
	wantParam := reflect.TypeOf([]domain.AssuranceLevel(nil))
	if signature.NumIn() != 1 || signature.In(0) != wantParam {
		t.Fatalf("AchievedAssuranceFromEvidence signature = %s, want exactly one parameter of type %s", signature, wantParam)
	}
}

// TestDiscoveryChannelAndRejectionReasonAreDistinctTypes confirms the two
// mirrored vocabularies really are distinct Go types (not type aliases of
// the same underlying type used interchangeably), so a DiscoveryChannel
// value and a RejectionReason value can never be silently substituted for
// one another even though a few string values coincide in spelling by
// convention.
func TestDiscoveryChannelAndRejectionReasonAreDistinctTypes(t *testing.T) {
	channelType := reflect.TypeOf(DiscoveryChannel(""))
	reasonType := reflect.TypeOf(RejectionReason(""))
	if channelType == reasonType {
		t.Fatal("DiscoveryChannel and RejectionReason must be distinct Go types")
	}
}
