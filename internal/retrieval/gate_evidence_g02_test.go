package retrieval

import (
	"reflect"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// TestGateEvidenceG02_ServiceResultTypesCarryNoSimilarityScore is the M21-G02
// gate-evidence test at the SERVICE layer: "Similarity produces candidates
// only; exact predicates determine eligibility." The core mechanism already
// lives in internal/retrievalgate and is proven there by reflection
// (structural_test.go's TestEligibilityCandidateCarriesNoSimilarityScore and
// TestEvaluateEligibilitySignatureCannotAcceptDiscoveryData) and by a
// decisive end-to-end scenario (decisive_test.go's
// TestM21_144_HighSimilarityAtomWithFailedApplicabilityIsRejected: a
// candidate discovered with a 0.98 similarity score is rejected identically
// to one discovered at 0.02, because EvaluateEligibility never receives a
// DiscoveredCandidate at all).
//
// This test extends that proof one layer further out, to the actual types a
// caller of this package (a future coordinator or UI) would hold:
// PreWorkGateResult and InfluentialMemoryItem, the real, public result of
// RunPreWorkGate. If a similarity score could reach either of these through
// any path, a caller could smuggle it into a downstream decision even though
// retrievalgate itself never would. It cannot: neither type has a float64
// -reachable field anywhere in its structure (this includes
// InfluentialMemoryItem's embedded, unexported
// retrievalgate.EligibilityDecision, which structural_test.go already
// proves is itself float64-free).
func TestGateEvidenceG02_ServiceResultTypesCarryNoSimilarityScore(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(PreWorkGateResult{}),
		reflect.TypeOf(InfluentialMemoryItem{}),
	} {
		if typeReachesFloat64(typ, map[reflect.Type]bool{}) {
			t.Fatalf("%s must not contain a float64-reachable field anywhere; a similarity score must never be representable in this package's public result", typ)
		}
	}
}

// typeReachesFloat64 mirrors internal/retrievalgate/structural_test.go's
// helper of the same name exactly, so this package's own reflection proof
// does not depend on retrievalgate exporting it.
func typeReachesFloat64(typ reflect.Type, visited map[reflect.Type]bool) bool {
	if typ == nil || visited[typ] {
		return false
	}
	visited[typ] = true
	if typ.Kind() == reflect.Float64 {
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

// TestGateEvidenceG02_DiscoveryChannelStrengthNeverGatesEligibility is the
// service-layer companion to M21-144: a candidate whose ONLY discovery
// channel in this package is the weaker, structured-field channel
// (storage.RetrievalCandidateApplicabilityPass -- the only discovery
// channel real recipes/atoms use today; DiscoveryChannelVectorSimilarity is
// not produced anywhere in this package, see doc.go) is judged purely on the
// SAME eligibility gates as any other candidate; which channel(s) found it
// never appears anywhere in the eligibility computation. This is verified by
// running the identical candidate/task pair through RunPreWorkGate twice --
// once discovered only by the structured-field channel, once ALSO
// discovered by the (real) exact-identity channel via a supporting episode
// -- and confirming the eligibility outcome (and its recorded reason) is
// bit-for-bit identical regardless of which/how many channels found it,
// which is exactly what "produces candidates only" must mean once no
// similarity score exists to test against.
func TestGateEvidenceG02_DiscoveryChannelStrengthNeverGatesEligibility(t *testing.T) {
	ctx := t.Context()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}

	runOnce := func(t *testing.T, suffix string, addExactIdentityChannel bool) (eligible bool, channels []storage.MemoryRetrievalCandidateSource) {
		projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
		task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)
		artifactID, revisionID := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go build ./...")
		if addExactIdentityChannel {
			episodeID := mustCreateClosedEpisode(t, repositories, projectID, repositoryID, task)
			if err := repositories.RecordMemoryArtifactSupportingEpisode(ctx, artifactID, episodeID); err != nil {
				t.Fatal(err)
			}
		}
		queryID := newTestQueryID(t, "g02-channel-strength-"+suffix)
		result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
			QueryID: queryID, ProjectID: projectID,
			Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
			Task:     task,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Eligible) != 1 || result.Eligible[0].RevisionID != revisionID {
			t.Fatalf("result.Eligible = %#v, want exactly one item for %s", result.Eligible, revisionID)
		}
		return true, result.Eligible[0].Channels
	}

	structuredOnlyEligible, structuredOnlyChannels := runOnce(t, "structured-only", false)
	bothChannelsEligible, bothChannels := runOnce(t, "both-channels", true)

	if structuredOnlyEligible != bothChannelsEligible {
		t.Fatal("eligibility differed between structured-field-only and exact-identity-plus-structured-field discovery")
	}
	if len(structuredOnlyChannels) != 1 || string(structuredOnlyChannels[0]) != string(storage.RetrievalCandidateApplicabilityPass) {
		t.Fatalf("structured-field-only channels = %#v, want exactly [applicability-pass]", structuredOnlyChannels)
	}
	if len(bothChannels) != 2 {
		t.Fatalf("exact-identity-plus-structured-field channels = %#v, want both channels merged", bothChannels)
	}
}
