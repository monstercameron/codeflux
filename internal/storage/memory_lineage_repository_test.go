package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestMemoryArtifactLineageDistinguishesDerivedFromAndInfluencedBy(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 960)
	repositoryID := testRepositoryID(t, 961)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	descendant := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 962)
	semanticAncestor := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 965)
	contextualAncestor := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 968)

	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, descendant, semanticAncestor); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactInfluencedBy(ctx, descendant, contextualAncestor); err != nil {
		t.Fatal(err)
	}

	lineage, err := repositories.GetMemoryArtifactLineage(ctx, descendant)
	if err != nil {
		t.Fatal(err)
	}
	if len(lineage.DerivedFrom) != 1 || lineage.DerivedFrom[0] != semanticAncestor {
		t.Fatalf("derived_from = %#v", lineage.DerivedFrom)
	}
	if len(lineage.InfluencedBy) != 1 || lineage.InfluencedBy[0] != contextualAncestor {
		t.Fatalf("influenced_by = %#v", lineage.InfluencedBy)
	}
	if err := lineage.Validate(); err != nil {
		t.Fatalf("domain lineage validation failed: %v", err)
	}

	// The same ancestor cannot be claimed through both relations at once.
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, descendant, contextualAncestor); !errors.Is(err, ErrConstraint) {
		t.Fatalf("derived_from over influenced_by error = %v, want constraint", err)
	}
	if err := repositories.RecordMemoryArtifactInfluencedBy(ctx, descendant, semanticAncestor); !errors.Is(err, ErrConstraint) {
		t.Fatalf("influenced_by over derived_from error = %v, want constraint", err)
	}

	// Recording the identical edge again is an idempotent no-op.
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, descendant, semanticAncestor); err != nil {
		t.Fatal(err)
	}
	lineage, err = repositories.GetMemoryArtifactLineage(ctx, descendant)
	if err != nil {
		t.Fatal(err)
	}
	if len(lineage.DerivedFrom) != 1 {
		t.Fatalf("derived_from after duplicate insert = %#v", lineage.DerivedFrom)
	}
}

func TestMemoryArtifactLineageRejectsSelfReference(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 970)
	repositoryID := testRepositoryID(t, 971)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 972)

	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, artifact, artifact); err == nil {
		t.Fatal("expected self-referential derived_from to be rejected")
	}
	if err := repositories.RecordMemoryArtifactInfluencedBy(ctx, artifact, artifact); err == nil {
		t.Fatal("expected self-referential influenced_by to be rejected")
	}
}

func TestMemoryArtifactEvidenceFamilyFollowsBothLineageRelations(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 980)
	repositoryID := testRepositoryID(t, 981)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	root := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 982)
	viaDerived := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 985)
	viaInfluenced := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 988)
	unrelated := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 991)

	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, viaDerived, root); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactInfluencedBy(ctx, viaInfluenced, root); err != nil {
		t.Fatal(err)
	}

	index, err := loadMemoryArtifactLineageIndex(ctx, repositories.database.sql, projectID)
	if err != nil {
		t.Fatal(err)
	}
	// EvidenceFamily walks outward from one artifact to its own ancestors
	// (DerivedFrom/InfluencedBy), not to its descendants; viaDerived and
	// viaInfluenced each list root as an ancestor, so root belongs to both
	// of their evidence families, while an unrelated artifact's family
	// never reaches root.
	derivedFamily := index[viaDerived].EvidenceFamily(index)
	if _, ok := derivedFamily[root]; !ok {
		t.Fatalf("derived_from evidence family missing its semantic ancestor: %#v", derivedFamily)
	}
	influencedFamily := index[viaInfluenced].EvidenceFamily(index)
	if _, ok := influencedFamily[root]; !ok {
		t.Fatalf("influenced_by evidence family missing its contextual ancestor: %#v", influencedFamily)
	}
	unrelatedLineage := domain.MemoryArtifactLineage{ArtifactID: unrelated}
	unrelatedFamily := unrelatedLineage.EvidenceFamily(index)
	if _, ok := unrelatedFamily[root]; ok {
		t.Fatalf("unrelated artifact's evidence family incorrectly reaches root: %#v", unrelatedFamily)
	}

	confirmsIndependently, err := domain.ConfirmsMemoryArtifactIndependently(viaDerived, index, nil)
	if err != nil {
		t.Fatal(err)
	}
	if confirmsIndependently {
		t.Fatal("empty candidate episodes must not confirm independently")
	}
}

// TestLoadMemoryArtifactLineageIndexForDeletionIsScopedToReachableSubgraph
// covers the reviewed defect where DeleteMemoryArtifact called
// loadMemoryArtifactLineageIndex(ctx, tx, target.ProjectID), loading every
// derived_from/influenced_by edge for the ENTIRE owning project on every
// deletion, with no LIMIT and no relationship to whether those artifacts
// were reachable from target at all (AGENTS.md "Avoid unbounded ... graph
// queries"). loadMemoryArtifactLineageIndexForDeletion must instead read
// only target's own reachable subgraph: its transitive derived_from
// descendants plus artifacts directly influenced_by it.
func TestLoadMemoryArtifactLineageIndexForDeletionIsScopedToReachableSubgraph(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1210)
	repositoryID := testRepositoryID(t, 1211)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	target := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1212)
	dependent := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1215)
	influencedChild := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1218)
	unrelated := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1221)
	unrelatedAncestor := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1224)

	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, dependent, target); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactInfluencedBy(ctx, influencedChild, target); err != nil {
		t.Fatal(err)
	}
	// unrelated shares target's project and has a lineage edge of its own
	// (to unrelatedAncestor, not to target), so it shows up in the
	// whole-project index but must never show up in target's bounded,
	// reachable-subgraph index.
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, unrelated, unrelatedAncestor); err != nil {
		t.Fatal(err)
	}

	boundedIndex, err := loadMemoryArtifactLineageIndexForDeletion(ctx, repositories.database.sql, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := boundedIndex[unrelated]; present {
		t.Fatalf("bounded lineage load for deletion read an artifact outside target's reachable subgraph: %#v", boundedIndex)
	}
	if _, present := boundedIndex[dependent]; !present {
		t.Fatalf("bounded lineage load for deletion missed target's derived_from dependent: %#v", boundedIndex)
	}
	if _, present := boundedIndex[influencedChild]; !present {
		t.Fatalf("bounded lineage load for deletion missed target's direct influenced_by child: %#v", boundedIndex)
	}

	// Baseline: the whole-project loader (still correct for callers that
	// genuinely need the full graph, e.g. domain.ConfirmsMemoryArtifactIndependently)
	// does include the unrelated artifact, which is exactly the read the
	// bounded loader above must avoid.
	wholeProjectIndex, err := loadMemoryArtifactLineageIndex(ctx, repositories.database.sql, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := wholeProjectIndex[unrelated]; !present {
		t.Fatalf("expected the whole-project index to still contain every project artifact as a baseline for comparison: %#v", wholeProjectIndex)
	}

	preview, err := domain.PreviewMemoryArtifactDeletion(target, boundedIndex)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.DirectDependents) != 1 || preview.DirectDependents[0] != dependent {
		t.Fatalf("preview computed from the bounded index = %#v", preview.DirectDependents)
	}
	if len(preview.ContextuallyInfluenced) != 1 || preview.ContextuallyInfluenced[0] != influencedChild {
		t.Fatalf("preview computed from the bounded index = %#v", preview.ContextuallyInfluenced)
	}
}

// TestLoadMemoryArtifactLineageIndexForDeletionRejectsExceedingBound checks
// the bounded loader fails loudly with a typed error, rather than silently
// truncating the preview or cascade, when a target's reachable subgraph
// exceeds the read bound. It calls the *Bounded entry point with a tiny
// cap so the cap-exceeded path is cheap to exercise without constructing
// thousands of fixture artifacts.
func TestLoadMemoryArtifactLineageIndexForDeletionRejectsExceedingBound(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1230)
	repositoryID := testRepositoryID(t, 1231)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	target := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1232)
	dependentOne := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1235)
	dependentTwo := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1238)
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, dependentOne, target); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, dependentTwo, target); err != nil {
		t.Fatal(err)
	}

	// Three artifacts (target + 2 dependents) exceed a cap of 2.
	if _, err := loadMemoryArtifactLineageIndexForDeletionBounded(ctx, repositories.database.sql, target, 2); !errors.Is(err, ErrMemoryArtifactLineageTooLarge) {
		t.Fatalf("bounded lineage load error = %v, want ErrMemoryArtifactLineageTooLarge", err)
	}

	// A generous cap still succeeds.
	index, err := loadMemoryArtifactLineageIndexForDeletionBounded(ctx, repositories.database.sql, target, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 3 {
		t.Fatalf("bounded lineage load with a generous cap = %#v, want 3 artifacts", index)
	}
}
