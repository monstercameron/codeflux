package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestCreateMemoryArtifactPersistsIdentityAndFirstRevisionIdempotently(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 900)
	repositoryID := testRepositoryID(t, 901)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifactID := testMemoryArtifactID(t, 902)
	revisionID := testMemoryArtifactRevisionID(t, 903)
	content := testRepositoryFactContent(t, repositoryID, "go test ./...")

	input := CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: revisionID, ProjectID: projectID,
		Content: content, IdempotencyKey: "artifact-create",
	}
	first, err := repositories.CreateMemoryArtifact(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.RevisionNumber != 1 || first.Maturity != domain.MaturityStateCandidate ||
		first.Kind != domain.MemoryArtifactKindRepositoryFact || first.SupersedesRevisionID != nil ||
		first.CreatedFromCorrection || first.ContentRepositoryID != repositoryID {
		t.Fatalf("first revision = %#v", first)
	}

	retried, err := repositories.CreateMemoryArtifact(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if retried.RevisionID != first.RevisionID || retried.ContentSHA256 != first.ContentSHA256 {
		t.Fatalf("idempotent retry = %#v, want %#v", retried, first)
	}

	changed := input
	changed.Content = testRepositoryFactContent(t, repositoryID, "go test -race ./...")
	if _, err := repositories.CreateMemoryArtifact(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed retry error = %v, want conflict", err)
	}

	got, err := repositories.GetMemoryArtifactRevision(ctx, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentSHA256 != first.ContentSHA256 {
		t.Fatalf("round-tripped revision = %#v", got)
	}
	latest, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.RevisionID != revisionID {
		t.Fatalf("latest revision = %#v", latest)
	}
}

func TestCreateMemoryArtifactRejectsContentRepositoryOutsideProject(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 910)
	repositoryID := testRepositoryID(t, 911)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	otherProjectID := testProjectID(t, 912)
	otherRepositoryID := testRepositoryID(t, 913)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)

	_, err := repositories.CreateMemoryArtifact(ctx, CreateMemoryArtifact{
		ArtifactID: testMemoryArtifactID(t, 914), RevisionID: testMemoryArtifactRevisionID(t, 915),
		ProjectID: projectID, Content: testRepositoryFactContent(t, otherRepositoryID, "go build ./..."),
		IdempotencyKey: "cross-project-content",
	})
	if !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project content repository error = %v, want constraint", err)
	}
}

func TestCreateMemoryArtifactCorrectionSupersedesWithoutMutatingHistory(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 920)
	repositoryID := testRepositoryID(t, 921)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifactID := testMemoryArtifactID(t, 922)
	firstRevisionID := testMemoryArtifactRevisionID(t, 923)
	first, err := repositories.CreateMemoryArtifact(ctx, CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: firstRevisionID, ProjectID: projectID,
		Content: testRepositoryFactContent(t, repositoryID, "go test ./..."), IdempotencyKey: "correction-original",
	})
	if err != nil {
		t.Fatal(err)
	}

	secondRevisionID := testMemoryArtifactRevisionID(t, 924)
	corrected := testRepositoryFactContent(t, repositoryID, "go test -count=1 ./...")
	second, err := repositories.CreateMemoryArtifactCorrection(ctx, CreateMemoryArtifactCorrection{
		PriorRevisionID: firstRevisionID, NewRevisionID: secondRevisionID,
		CorrectedContent: corrected, IdempotencyKey: "correction-applied",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.RevisionNumber != 2 || second.SupersedesRevisionID == nil || *second.SupersedesRevisionID != firstRevisionID ||
		!second.CreatedFromCorrection || second.Maturity != domain.MaturityStateCandidate {
		t.Fatalf("correction revision = %#v", second)
	}

	// The prior revision's content must remain exactly as originally written.
	unchanged, err := repositories.GetMemoryArtifactRevision(ctx, firstRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.ContentSHA256 != first.ContentSHA256 {
		t.Fatalf("prior revision content changed: %#v", unchanged)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE memory_artifact_revisions SET content_json = '{}' WHERE id = ?`, firstRevisionID,
	); !errors.Is(classify("mutate memory artifact revision content", err), ErrConstraint) {
		t.Fatalf("direct content mutation error = %v, want constraint", err)
	}

	latest, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.RevisionID != secondRevisionID {
		t.Fatalf("latest revision after correction = %#v", latest)
	}
}

// TestDeleteMemoryArtifactCascadesDerivedFromNotInfluencedBy also covers the
// reviewed defect where this test used to assert
// len(outcome.Preview.DirectDependents) == 1 for a target -> dependent ->
// grandchild chain while simultaneously asserting the grandchild WAS
// deleted: PreviewMemoryArtifactDeletion is transitive (matching what the
// cascade below has always deleted), so the preview must report every
// transitive dependent, not just the one-hop dependent. This version adds
// a third hop (greatGrandchild) and a derived_from cycle back to target
// (individually legal -- only direct self-reference is rejected -- and
// must not hang or double-count target as its own dependent) so the
// preview/cascade agreement and cycle-safety are both exercised.
func TestDeleteMemoryArtifactCascadesDerivedFromNotInfluencedBy(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 930)
	repositoryID := testRepositoryID(t, 931)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	target := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 932)
	dependent := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 935)
	grandchild := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 938)
	greatGrandchild := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 944)
	exposed := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 941)

	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, dependent, target); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, grandchild, dependent); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, greatGrandchild, grandchild); err != nil {
		t.Fatal(err)
	}
	// Cycle: target "depends on" its own deepest descendant. Validate()
	// only rejects direct self-reference, so this must be accepted, and
	// the traversal below must terminate correctly rather than looping.
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, target, greatGrandchild); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactInfluencedBy(ctx, exposed, target); err != nil {
		t.Fatal(err)
	}

	// Deletion without acknowledging the transitive dependents must be
	// rejected, composing with
	// domain.PreviewMemoryArtifactDeletion/ValidateMemoryArtifactDeletion.
	if _, err := repositories.DeleteMemoryArtifact(ctx, DeleteMemoryArtifact{
		Target: target, ReasonRedacted: "superseded by a validated recipe",
	}); err == nil {
		t.Fatal("expected deletion to require acknowledging the transitive dependents")
	}

	// Acknowledging only the one-hop dependent (the pre-fix expectation)
	// must also be rejected: the preview is transitive, so a partial
	// acknowledgement no longer matches it.
	if _, err := repositories.DeleteMemoryArtifact(ctx, DeleteMemoryArtifact{
		Target: target, AcknowledgedDependents: []domain.MemoryArtifactID{dependent},
		ReasonRedacted: "superseded by a validated recipe",
	}); err == nil {
		t.Fatal("expected deletion to reject acknowledging only the one-hop dependent when deeper transitive dependents exist")
	}

	outcome, err := repositories.DeleteMemoryArtifact(ctx, DeleteMemoryArtifact{
		Target:                 target,
		AcknowledgedDependents: []domain.MemoryArtifactID{dependent, grandchild, greatGrandchild},
		ReasonRedacted:         "superseded by a validated recipe",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The fix: the preview must report every transitive dependent, so it
	// agrees with what the cascade actually deletes below.
	if len(outcome.Preview.DirectDependents) != 3 {
		t.Fatalf("preview direct dependents = %#v, want 3 (dependent, grandchild, greatGrandchild)", outcome.Preview.DirectDependents)
	}
	previewed := map[domain.MemoryArtifactID]bool{}
	for _, id := range outcome.Preview.DirectDependents {
		previewed[id] = true
	}
	if !previewed[dependent] || !previewed[grandchild] || !previewed[greatGrandchild] {
		t.Fatalf("preview direct dependents = %#v, want exactly {dependent, grandchild, greatGrandchild}", outcome.Preview.DirectDependents)
	}
	if len(outcome.Preview.ContextuallyInfluenced) != 1 || outcome.Preview.ContextuallyInfluenced[0] != exposed {
		t.Fatalf("preview contextually influenced = %#v", outcome.Preview.ContextuallyInfluenced)
	}
	deleted := map[domain.MemoryArtifactID]bool{}
	for _, id := range outcome.DeletedArtifacts {
		deleted[id] = true
	}
	if len(deleted) != 4 || !deleted[target] || !deleted[dependent] || !deleted[grandchild] || !deleted[greatGrandchild] {
		t.Fatalf("cascade did not delete exactly the full transitive derived_from chain (target, dependent, grandchild, greatGrandchild), cycle edge included: %#v", outcome.DeletedArtifacts)
	}
	if deleted[exposed] {
		t.Fatalf("cascade incorrectly deleted the influenced_by-only descendant: %#v", outcome.DeletedArtifacts)
	}

	exposedArtifact, found, err := findMemoryArtifact(ctx, repositories.database.sql, exposed)
	if err != nil || !found {
		t.Fatal(err)
	}
	if exposedArtifact.DeletedAt != nil {
		t.Fatalf("influenced_by descendant was deleted: %#v", exposedArtifact)
	}
}

func TestDeleteMemoryArtifactNeverCrossesProjectBoundary(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectA := testProjectID(t, 950)
	repositoryA := testRepositoryID(t, 951)
	mustCreateProjectRepository(t, repositories, projectA, repositoryA)
	projectB := testProjectID(t, 952)
	repositoryB := testRepositoryID(t, 953)
	mustCreateProjectRepository(t, repositories, projectB, repositoryB)

	artifactA := createMemoryArtifactFixture(t, repositories, projectA, repositoryA, 954)
	artifactB := createMemoryArtifactFixture(t, repositories, projectB, repositoryB, 957)

	// A lineage edge across the two projects must be structurally
	// impossible, which is what makes cross-project cascade impossible too.
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, artifactB, artifactA); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project derived_from error = %v, want constraint", err)
	}
	if err := repositories.RecordMemoryArtifactInfluencedBy(ctx, artifactB, artifactA); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project influenced_by error = %v, want constraint", err)
	}

	outcome, err := repositories.DeleteMemoryArtifact(ctx, DeleteMemoryArtifact{
		Target: artifactA, ReasonRedacted: "project A cleanup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.DeletedArtifacts) != 1 || outcome.DeletedArtifacts[0] != artifactA {
		t.Fatalf("deletion outcome = %#v, want only artifactA", outcome)
	}
	otherProjectArtifact, found, err := findMemoryArtifact(ctx, repositories.database.sql, artifactB)
	if err != nil || !found {
		t.Fatal(err)
	}
	if otherProjectArtifact.DeletedAt != nil {
		t.Fatalf("deletion in project A affected project B: %#v", otherProjectArtifact)
	}
}

func createMemoryArtifactFixture(
	t *testing.T,
	repositories *Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	number int,
) domain.MemoryArtifactID {
	t.Helper()
	artifactID := testMemoryArtifactID(t, number)
	revisionID := testMemoryArtifactRevisionID(t, number+1)
	if _, err := repositories.CreateMemoryArtifact(t.Context(), CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: revisionID, ProjectID: projectID,
		Content:        testRepositoryFactContent(t, repositoryID, "go vet ./..."),
		IdempotencyKey: revisionID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	return artifactID
}

func testRepositoryFactContent(t *testing.T, repositoryID domain.RepositoryID, statement string) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewRepositoryFactMemoryContent(domain.RepositoryFactContent{
		Repository: repositoryID,
		Category:   domain.RepositoryFactCategoryTestCommand,
		Statement:  statement,
		Binding:    domain.RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func testMemoryArtifactID(t *testing.T, number int) domain.MemoryArtifactID {
	t.Helper()
	id, err := domain.ParseMemoryArtifactID("mem_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testMemoryArtifactRevisionID(t *testing.T, number int) domain.MemoryArtifactRevisionID {
	t.Helper()
	id, err := domain.ParseMemoryArtifactRevisionID("mrv_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
