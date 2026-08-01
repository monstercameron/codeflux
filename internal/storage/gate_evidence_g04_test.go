package storage

import (
	"reflect"
	"testing"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/atomname"
	"codeflux.dev/codeflux/internal/domain"
)

// TestGateEvidenceG04_InvalidationCascadesTransitively is the M21-G04
// gate-evidence test: "Changed support invalidates dependent facts and
// vectors transitively."
//
// HISTORY: this test previously asserted the opposite, documenting that
// invalidation stopped at the artifact it was called on while its
// derived_from descendants kept standing. That gap is now closed by
// Repositories.CascadeMemoryArtifactInvalidationTransitively
// (memory_cascade_repository.go), and this test was rewritten to prove the
// cascade rather than deleted — exactly as its previous comment required.
//
// The facts half of the gate now PASSES. The vectors half remains vacuous:
// zero atom vectors exist because M21-079/081 are held closed at the §0
// branch gate, so there is no vector for changed support to invalidate.
// TestGateEvidenceG04_AtomEmbeddingLifecycleTriggersHaveNoCrossAtomParameter
// below still records that the atom-vector lifecycle functions are
// identity-blind and could not propagate across atoms even if vectors did
// exist.
//
// The cascade reuses the same bounded, cycle-safe derived_from traversal
// that DeleteMemoryArtifact already relied on
// (memoryArtifactDerivedFromDescendantsCTE), so invalidation and deletion
// agree on what "descendant" means. Per docs/plan.md §31 "Descendant
// Contamination", only semantic dependents are quarantined automatically;
// contextually influenced descendants are reported and left untouched, and
// crossing five percent of active artifacts flags that permanent bulk
// invalidation needs assurance-owner review. Those behaviours are covered
// in memory_cascade_repository_test.go.
func TestGateEvidenceG04_InvalidationCascadesTransitively(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9900)
	repositoryID := testRepositoryID(t, 9901)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	artifactA := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9902)
	artifactB := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9905)
	artifactC := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9908)
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, artifactB, artifactA); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, artifactC, artifactB); err != nil {
		t.Fatal(err)
	}

	revisionA, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifactA)
	if err != nil {
		t.Fatal(err)
	}
	revisionB, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifactB)
	if err != nil {
		t.Fatal(err)
	}
	revisionC, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifactC)
	if err != nil {
		t.Fatal(err)
	}
	if revisionA.Maturity != domain.MaturityStateCandidate || revisionB.Maturity != domain.MaturityStateCandidate || revisionC.Maturity != domain.MaturityStateCandidate {
		t.Fatalf("fixture precondition: all three must start Candidate, got A=%s B=%s C=%s", revisionA.Maturity, revisionB.Maturity, revisionC.Maturity)
	}

	// "Changed support" for A: invalidate it directly, exactly what
	// UpsertExtractedMemoryFact's own M21-048 branch does to a fact whose
	// supporting content changed.
	if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revisionA.RevisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateInvalidated,
		ReasonKind:     MemoryArtifactInvalidationReasonBindingChanged,
		DetailRedacted: "gate-evidence G04: A's support changed",
		IdempotencyKey: "g04-invalidate-a",
	}); err != nil {
		t.Fatal(err)
	}

	afterA, err := repositories.GetMemoryArtifactRevision(ctx, revisionA.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if afterA.Maturity != domain.MaturityStateInvalidated {
		t.Fatalf("A.Maturity = %s, want Invalidated", afterA.Maturity)
	}

	afterB, err := repositories.GetMemoryArtifactRevision(ctx, revisionB.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	afterC, err := repositories.GetMemoryArtifactRevision(ctx, revisionC.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	// A bare single-artifact transition still touches only its own revision.
	// That is deliberate: cascading is an explicit, reason-bearing call, not
	// a hidden side effect of every maturity change.
	if afterB.Maturity != domain.MaturityStateCandidate || afterC.Maturity != domain.MaturityStateCandidate {
		t.Fatalf("a bare TransitionMemoryArtifactMaturity must not cascade implicitly, got B=%s C=%s", afterB.Maturity, afterC.Maturity)
	}

	// THE GATE: the explicit cascade must reach every transitive
	// derived_from descendant, not just the first hop. C is depth 2 and is
	// the case a one-hop implementation misses.
	outcome, err := repositories.CascadeMemoryArtifactInvalidationTransitively(ctx, CascadeMemoryArtifactInvalidation{
		ProjectID:        projectID,
		OriginArtifactID: artifactA,
		ReasonKind:       MemoryArtifactInvalidationReasonBindingChanged,
		DetailRedacted:   "gate-evidence G04: cascade from A's changed support",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.QuarantinedArtifacts) != 2 {
		t.Fatalf("QuarantinedArtifacts = %d, want 2 (B at depth 1 and C at depth 2)", len(outcome.QuarantinedArtifacts))
	}

	cascadedB, err := repositories.GetMemoryArtifactRevision(ctx, revisionB.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	cascadedC, err := repositories.GetMemoryArtifactRevision(ctx, revisionC.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if cascadedB.Maturity != domain.MaturityStateQuarantined {
		t.Fatalf("B.Maturity = %s, want Quarantined: B derives from invalidated A", cascadedB.Maturity)
	}
	if cascadedC.Maturity != domain.MaturityStateQuarantined {
		t.Fatalf("C.Maturity = %s, want Quarantined: C derives from B, which derives from invalidated A (transitive)", cascadedC.Maturity)
	}
	// Quarantine is terminal FOR AUTHORITY specifically: MaturityState.IsTerminal
	// covers only Invalidated/Retired, while a quarantined artifact may still
	// move to those. The guarantee that matters here is that neither cascaded
	// descendant can ever reach Validated or PreferredForExperiment again.
	if cascadedB.Maturity.CanReachAuthority() || cascadedC.Maturity.CanReachAuthority() {
		t.Fatalf("cascaded descendants must be unable to reach authority, got B=%s C=%s", cascadedB.Maturity, cascadedC.Maturity)
	}
}

// TestGateEvidenceG04_AtomEmbeddingLifecycleTriggersHaveNoCrossAtomParameter
// is the second half of the M21-G04 finding, at the atom-vector layer.
// atomname.DetermineAtomEmbeddingLifecycleTriggers (which extends
// atomdoc.DetermineDocumentationEmbeddingLifecycleTriggers) is the pure
// decision function a future embedding-lifecycle lane would call to decide
// whether ONE atom's own derived vector must be invalidated/re-embedded. Its
// signature takes only a before/after naming-input pair (plain text
// segments, M21-127) and a documentation change-surface value (five plain
// booleans) -- neither carries an AtomID anywhere, at any depth. The
// function is therefore identity-blind by construction: it cannot even name
// the ONE atom it is deciding about, let alone a second, dependent atom or a
// dependency-graph parameter it would need to decide "and therefore atom Y,
// which depends on this atom, must also invalidate its vector." This is
// proven structurally (by reflection over the function's real signature)
// rather than merely asserted in prose, so it stays red if a future
// signature change silently adds an identity or dependency-graph parameter
// without a matching test proving cross-atom propagation actually happens.
func TestGateEvidenceG04_AtomEmbeddingLifecycleTriggersHaveNoCrossAtomParameter(t *testing.T) {
	signature := reflect.TypeOf(atomname.DetermineAtomEmbeddingLifecycleTriggers)
	wantParams := []reflect.Type{
		reflect.TypeOf(atomname.NameEmbeddingInput{}),
		reflect.TypeOf(atomname.NameEmbeddingInput{}),
		reflect.TypeOf(atomdoc.EmbeddingLifecycleChangeSurface{}),
	}
	if signature.NumIn() != len(wantParams) {
		t.Fatalf("DetermineAtomEmbeddingLifecycleTriggers has %d parameters, want exactly %d: %s", signature.NumIn(), len(wantParams), signature)
	}
	for i, want := range wantParams {
		if signature.In(i) != want {
			t.Fatalf("parameter %d = %s, want %s", i, signature.In(i), want)
		}
	}
	atomIDType := reflect.TypeOf(domain.AtomID{})
	for i := 0; i < signature.NumIn(); i++ {
		if typeReachesTypeForG04(signature.In(i), atomIDType, map[reflect.Type]bool{}) {
			t.Fatalf("parameter %d (%s) is reachable from an AtomID; a function that could name a SECOND atom would need such a field to model cross-atom propagation, and none exists today", i, signature.In(i))
		}
	}
}

// typeReachesTypeForG04 walks typ's structure (fields, slice/array/map/
// pointer element types) looking for target, mirroring the same walk shape
// internal/retrievalgate/structural_test.go uses for float64, specialized
// here to a caller-supplied target type instead.
func typeReachesTypeForG04(typ reflect.Type, target reflect.Type, visited map[reflect.Type]bool) bool {
	if typ == nil || visited[typ] {
		return false
	}
	visited[typ] = true
	if typ == target {
		return true
	}
	switch typ.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Chan:
		return typeReachesTypeForG04(typ.Elem(), target, visited)
	case reflect.Map:
		return typeReachesTypeForG04(typ.Key(), target, visited) || typeReachesTypeForG04(typ.Elem(), target, visited)
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			if typeReachesTypeForG04(typ.Field(i).Type, target, visited) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
