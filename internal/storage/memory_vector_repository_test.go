package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestMemoryArtifactEmbeddingSchemaStoresAndInvalidatesVectorsWithinProjectBoundary(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1300)
	repositoryID := testRepositoryID(t, 1301)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1302)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}

	model, err := repositories.CreateMemoryEmbeddingModel(ctx, CreateMemoryEmbeddingModel{
		ID: "model-fixture-1300", Provider: "fixture-provider", ModelName: "fixture-model",
		ModelVersion: "v1", Dimensions: 8, NumericEncoding: "float32", Normalization: "l2",
	})
	if err != nil {
		t.Fatal(err)
	}
	space, err := repositories.CreateMemoryEmbeddingSpace(ctx, CreateMemoryEmbeddingSpace{
		ID: "space-fixture-1300", EmbeddingModelID: model.ID, InputSchemaVersion: 1,
		ProjectID: projectID, SecurityScope: "project-local",
	})
	if err != nil {
		t.Fatal(err)
	}

	embedding, err := repositories.CreateMemoryArtifactEmbedding(ctx, CreateMemoryArtifactEmbedding{
		ID: "embedding-fixture-1300", RevisionID: revision.RevisionID, EmbeddingSpaceID: space.ID,
		SourceContentSHA256: revision.ContentSHA256, Vector: []byte{1, 2, 3, 4, 5, 6, 7, 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !embedding.Valid {
		t.Fatalf("created embedding = %#v, want valid", embedding)
	}

	embeddings, err := repositories.ListMemoryArtifactEmbeddings(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(embeddings) != 1 || embeddings[0].ID != embedding.ID {
		t.Fatalf("listed embeddings = %#v", embeddings)
	}

	if err := repositories.InvalidateMemoryArtifactEmbedding(ctx, embedding.ID); err != nil {
		t.Fatal(err)
	}
	afterInvalidate, err := repositories.ListMemoryArtifactEmbeddings(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if afterInvalidate[0].Valid || afterInvalidate[0].InvalidatedAtMicros == nil {
		t.Fatalf("embedding after invalidation = %#v", afterInvalidate[0])
	}

	// A space scoped to a different project must never be usable for this
	// revision's embeddings (M21-027 project-boundary predicate).
	otherProjectID := testProjectID(t, 1310)
	otherRepositoryID := testRepositoryID(t, 1311)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)
	otherSpace, err := repositories.CreateMemoryEmbeddingSpace(ctx, CreateMemoryEmbeddingSpace{
		ID: "space-fixture-1310", EmbeddingModelID: model.ID, InputSchemaVersion: 1,
		ProjectID: otherProjectID, SecurityScope: "project-local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryArtifactEmbedding(ctx, CreateMemoryArtifactEmbedding{
		ID: "embedding-fixture-1310", RevisionID: revision.RevisionID, EmbeddingSpaceID: otherSpace.ID,
		SourceContentSHA256: revision.ContentSHA256, Vector: []byte{1, 2, 3, 4},
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project embedding space error = %v, want constraint", err)
	}
}

func TestCreateMemoryArtifactEmbeddingRejectsEmptyVector(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1320)
	repositoryID := testRepositoryID(t, 1321)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1322)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	model, err := repositories.CreateMemoryEmbeddingModel(ctx, CreateMemoryEmbeddingModel{
		ID: "model-fixture-1320", Provider: "fixture", ModelName: "fixture", ModelVersion: "v1",
		Dimensions: 4, NumericEncoding: "float32", Normalization: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	space, err := repositories.CreateMemoryEmbeddingSpace(ctx, CreateMemoryEmbeddingSpace{
		ID: "space-fixture-1320", EmbeddingModelID: model.ID, InputSchemaVersion: 1,
		ProjectID: projectID, SecurityScope: "project-local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryArtifactEmbedding(ctx, CreateMemoryArtifactEmbedding{
		ID: "embedding-fixture-1320", RevisionID: revision.RevisionID, EmbeddingSpaceID: space.ID,
		SourceContentSHA256: strings.Repeat("a", 64), Vector: nil,
	}); err == nil {
		t.Fatal("expected an empty vector to be rejected")
	}
}

// TestM21_088_DeletingAnArtifactInvalidatesItsDerivedVectors proves a
// deleted artifact's vectors cannot remain active retrieval candidates.
// The rows are retained rather than dropped, per M21-135's separation of
// retention from eligibility.
func TestM21_088_DeletingAnArtifactInvalidatesItsDerivedVectors(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 8800)
	repositoryID := testRepositoryID(t, 8801)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 8802)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	model, err := repositories.CreateMemoryEmbeddingModel(ctx, CreateMemoryEmbeddingModel{
		ID: "model-fixture-8800", Provider: "fixture-provider", ModelName: "fixture-model",
		ModelVersion: "v1", Dimensions: 8, NumericEncoding: "float32", Normalization: "l2",
	})
	if err != nil {
		t.Fatal(err)
	}
	space, err := repositories.CreateMemoryEmbeddingSpace(ctx, CreateMemoryEmbeddingSpace{
		ID: "space-fixture-8800", EmbeddingModelID: model.ID, InputSchemaVersion: 1,
		ProjectID: projectID, SecurityScope: "project-local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryArtifactEmbedding(ctx, CreateMemoryArtifactEmbedding{
		ID: "embedding-fixture-8800", RevisionID: revision.RevisionID, EmbeddingSpaceID: space.ID,
		SourceContentSHA256: revision.ContentSHA256, Vector: []byte{1, 2, 3, 4, 5, 6, 7, 8},
	}); err != nil {
		t.Fatal(err)
	}

	outcome, err := repositories.DeleteMemoryArtifact(ctx, DeleteMemoryArtifact{
		Target: artifact, ReasonRedacted: "M21-088 fixture deletion",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.InvalidatedEmbeddings != 1 {
		t.Fatalf("InvalidatedEmbeddings = %d, want 1", outcome.InvalidatedEmbeddings)
	}

	embeddings, err := repositories.ListMemoryArtifactEmbeddings(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(embeddings) != 1 {
		t.Fatalf("embeddings = %d, want the row retained for lineage", len(embeddings))
	}
	if embeddings[0].Valid {
		t.Fatal("a deleted artifact's vector must not remain valid")
	}
	if embeddings[0].InvalidatedAtMicros == nil {
		t.Fatal("invalidation must be timestamped")
	}
}
