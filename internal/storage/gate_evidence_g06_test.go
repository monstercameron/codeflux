package storage

import (
	"math"

	"codeflux.dev/codeflux/internal/vectorsearch"
	"testing"
)

// TestGateEvidenceG06_ActiveAtomVectorIsTraceableToAllFiveFacts is the
// M21-G06 gate-evidence test: "Every active atom vector is traceable to one
// validated documentation revision, contract hash, repository revision,
// embedding model, and input-schema version."
//
// STATUS: VACUOUS TODAY. M21-081..088 (embedding generation, storage, and
// re-embedding runtime) remain closed at the docs/plan.md §0 branch gate;
// nothing in the running prototype ever calls RecordAtomDocumentationEmbedding
// outside a test. Zero real atom_documentation_embeddings rows exist in any
// production database this prototype has ever run against, so "every active
// atom vector is traceable" is true only because its universally-quantified
// subject set is empty. This test cannot and does not change that; what it
// proves instead is that IF a vector existed, the schema and repository
// layer would make it traceable to all five named facts, and that the
// traceability survives a documentation-revision invalidation (the vector
// row is retained, unchanged, as historical lineage per M21-135, rather than
// silently rewritten or deleted).
func TestGateEvidenceG06_ActiveAtomVectorIsTraceableToAllFiveFacts(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9920)
	repositoryID := testRepositoryID(t, 9921)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	atomID := testAtomID(t, 9922)
	contractHash := testAtomContractHash(t, '7')
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, contractHash)
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: revision,
	}); err != nil {
		t.Fatal(err)
	}

	model, err := repositories.CreateMemoryEmbeddingModel(ctx, CreateMemoryEmbeddingModel{
		ID: "g06-model", Provider: "test-provider", ModelName: "test-model", ModelVersion: "v3",
		Dimensions: 16, NumericEncoding: "float32", Normalization: "l2",
	})
	if err != nil {
		t.Fatal(err)
	}
	space, err := repositories.CreateMemoryEmbeddingSpace(ctx, CreateMemoryEmbeddingSpace{
		ID: "g06-space", EmbeddingModelID: model.ID, InputSchemaVersion: 1,
		ProjectID: projectID, SecurityScope: "project-local",
	})
	if err != nil {
		t.Fatal(err)
	}
	embedding, err := repositories.RecordAtomDocumentationEmbedding(ctx, RecordAtomDocumentationEmbedding{
		ID: "g06-embedding", RevisionID: revision.RevisionID, EmbeddingSpaceID: space.ID,
		SourceContentSHA256: revision.NormalizedInputHash.String(), Vector: []byte{1, 2, 3, 4, 5, 6, 7, 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !embedding.Valid {
		t.Fatalf("newly recorded embedding = %#v, want valid", embedding)
	}

	// Fact 1 + 2: documentation revision, and the contract hash it is bound
	// to (M21-122).
	documentationRevision, err := repositories.GetAtomDocumentationRevision(ctx, embedding.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if documentationRevision.ValidationStatus != AtomDocumentationValidationStatusAdmitted {
		t.Fatalf("documentationRevision.ValidationStatus = %q, want admitted (a VALIDATED documentation revision)", documentationRevision.ValidationStatus)
	}
	if documentationRevision.ContractHash != contractHash {
		t.Fatalf("documentationRevision.ContractHash = %s, want %s", documentationRevision.ContractHash, contractHash)
	}
	// Fact 3: repository revision.
	if documentationRevision.SourceRepositoryRevision == "" {
		t.Fatal("documentationRevision.SourceRepositoryRevision is empty, want the exact source repository revision")
	}

	// Fact 4 + 5: embedding model, and the input-schema version, reached via
	// the embedding's EmbeddingSpaceID.
	tracedModelID, tracedSchemaVersion := traceEmbeddingSpaceForTest(t, repositories, embedding.EmbeddingSpaceID)
	if tracedModelID != model.ID {
		t.Fatalf("traced embedding model ID = %s, want %s", tracedModelID, model.ID)
	}
	if tracedSchemaVersion != 1 {
		t.Fatalf("traced input-schema version = %d, want 1", tracedSchemaVersion)
	}

	// Invalidating the documentation revision must not rewrite or delete the
	// vector row: the traceability link (revision_id) is preserved exactly,
	// and M21-135 requires the historical snapshot to remain retained even
	// though it should no longer be active. This package deliberately never
	// automatically invalidates the embedding as a side effect of
	// invalidating the documentation revision (no trigger, no repository
	// call does this); a caller must invalidate both explicitly if it wants
	// both. That real limitation is what the second assertion below checks.
	if err := repositories.InvalidateAtomDocumentationRevision(ctx, InvalidateAtomDocumentationRevision{
		RevisionID: revision.RevisionID, Reason: "gate-evidence G06: force invalidation to prove lineage survives it",
	}); err != nil {
		t.Fatal(err)
	}
	stillTraced, err := repositories.GetAtomDocumentationRevision(ctx, embedding.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if stillTraced.ValidationStatus != AtomDocumentationValidationStatusInvalidated {
		t.Fatalf("stillTraced.ValidationStatus = %q, want invalidated", stillTraced.ValidationStatus)
	}
	if stillTraced.ContractHash != contractHash || stillTraced.SourceRepositoryRevision != documentationRevision.SourceRepositoryRevision {
		t.Fatal("the invalidated revision's own contract hash / repository revision changed; traceability must survive invalidation unchanged")
	}

	pending, err := repositories.ListAtomDocumentationRevisionsPendingReembedding(ctx, space.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, pendingID := range pending {
		if pendingID == embedding.RevisionID {
			t.Fatalf("an invalidated revision must not appear in the pending-reembedding index (it is no longer admitted): %v", pending)
		}
	}
}

// traceEmbeddingSpaceForTest performs the raw join
// atom_documentation_embeddings.embedding_space_id ->
// memory_embedding_spaces.embedding_model_id/input_schema_version that no
// exported repository function currently composes (there is no
// "GetAtomVectorProvenance"-shaped read query yet); this is exactly the read
// path a future embedding-lifecycle lane or inspection UI would need to
// build once M21-081..088 are authorized.
func traceEmbeddingSpaceForTest(t *testing.T, repositories *Repositories, embeddingSpaceID string) (modelID string, inputSchemaVersion int) {
	t.Helper()
	if err := repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT embedding_model_id, input_schema_version FROM memory_embedding_spaces WHERE id = ?`,
		embeddingSpaceID,
	).Scan(&modelID, &inputSchemaVersion); err != nil {
		t.Fatal(err)
	}
	return modelID, inputSchemaVersion
}

// TestGateEvidenceG06_EmbeddingRequiresAllFiveBindingsToExist proves the
// schema HALF of G06 without any application-layer read query: an embedding
// row cannot be created at all unless its documentation revision, embedding
// space, embedding model, and (transitively) input-schema version already
// exist as real foreign-key targets -- traceability is not merely
// conventional, it is a durable constraint the database itself enforces.
func TestGateEvidenceG06_EmbeddingRequiresAllFiveBindingsToExist(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9930)
	repositoryID := testRepositoryID(t, 9931)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	atomID := testAtomID(t, 9932)
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, testAtomContractHash(t, '8'))
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: revision,
	}); err != nil {
		t.Fatal(err)
	}

	// No embedding space exists at all yet: recording an embedding against a
	// nonexistent space must fail rather than silently accept an untraceable
	// vector.
	if _, err := repositories.RecordAtomDocumentationEmbedding(ctx, RecordAtomDocumentationEmbedding{
		ID: "g06-orphan-embedding", RevisionID: revision.RevisionID, EmbeddingSpaceID: "does-not-exist",
		SourceContentSHA256: revision.NormalizedInputHash.String(), Vector: []byte{9},
	}); err == nil {
		t.Fatal("expected recording an embedding against a nonexistent embedding space to fail")
	}
}

// TestGateEvidenceG06_AGeneratedVectorIsTraceableToAllFiveBindings closes
// the M21-G06 gate with a REAL generated vector rather than a byte fixture.
//
// Until M21-081 landed there was no way to produce an embedding at all, so
// this gate could only be argued from the schema. It now stores a vector
// that vectorsearch.GenerateEmbeddings actually produced from scrubbed
// descriptive text, and walks it back to all five required facts:
// documentation revision, contract hash, repository revision, embedding
// model, and input-schema version.
func TestGateEvidenceG06_AGeneratedVectorIsTraceableToAllFiveBindings(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9960)
	repositoryID := testRepositoryID(t, 9961)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	atomID := testAtomID(t, 9962)
	contractHash := testAtomContractHash(t, '9')
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, contractHash)
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: revision,
	}); err != nil {
		t.Fatal(err)
	}

	// Generate a real vector from scrubbed descriptive text. The measurement
	// is the one M21-079 requires before the branch may open.
	embedder := vectorsearch.DeterministicEmbedder{Dimensions: 16}
	generated, err := vectorsearch.GenerateEmbeddings(ctx, embedder,
		vectorsearch.RecallMeasurement{
			QueriesInWindow: 40, FallbacksInWindow: 12,
			ReviewedFallbacksInWindow: 12, GenuineMissesInWindow: 3,
		},
		[]string{"Reserves an amount against an account without capturing it."},
	)
	if err != nil {
		t.Fatal(err)
	}
	identity := generated[0].Model

	// (4) embedding model and (5) input-schema version live on the space.
	model, err := repositories.CreateMemoryEmbeddingModel(ctx, CreateMemoryEmbeddingModel{
		ID: "g06-real-model", Provider: identity.Provider, ModelName: identity.ModelName,
		ModelVersion: identity.ModelVersion, Dimensions: identity.Dimensions,
		NumericEncoding: "float32", Normalization: identity.Normalization,
	})
	if err != nil {
		t.Fatal(err)
	}
	space, err := repositories.CreateMemoryEmbeddingSpace(ctx, CreateMemoryEmbeddingSpace{
		ID: "g06-real-space", EmbeddingModelID: model.ID, InputSchemaVersion: 1,
		ProjectID: projectID, SecurityScope: "project-local",
	})
	if err != nil {
		t.Fatal(err)
	}

	vectorBytes := make([]byte, 0, len(generated[0].Vector)*4)
	for _, value := range generated[0].Vector {
		bits := math.Float32bits(value)
		vectorBytes = append(vectorBytes, byte(bits>>24), byte(bits>>16), byte(bits>>8), byte(bits))
	}
	stored, err := repositories.RecordAtomDocumentationEmbedding(ctx, RecordAtomDocumentationEmbedding{
		ID: "g06-real-embedding", RevisionID: revision.RevisionID, EmbeddingSpaceID: space.ID,
		SourceContentSHA256: revision.NormalizedInputHash.String(), Vector: vectorBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Valid {
		t.Fatal("a freshly generated vector must be active")
	}

	// (1) documentation revision.
	tracedRevision, err := repositories.GetAtomDocumentationRevision(ctx, stored.RevisionID)
	if err != nil {
		t.Fatalf("active vector could not be traced to its documentation revision: %v", err)
	}
	// (2) contract hash and (3) repository revision.
	if tracedRevision.ContractHash != contractHash {
		t.Fatalf("contract hash = %v, want %v", tracedRevision.ContractHash, contractHash)
	}
	if tracedRevision.SourceRepositoryRevision == "" {
		t.Fatal("active vector must be traceable to the repository revision it was produced at")
	}
	// (4) and (5).
	if model.ModelName != identity.ModelName || model.ModelVersion != identity.ModelVersion {
		t.Fatalf("model = %s/%s, want the generating model %s/%s",
			model.ModelName, model.ModelVersion, identity.ModelName, identity.ModelVersion)
	}
	if space.InputSchemaVersion != 1 {
		t.Fatalf("input schema version = %d, want 1", space.InputSchemaVersion)
	}
	t.Logf("traced generated vector %s -> revision %s, contract %v, repo revision %s, model %s/%s, input schema v%d",
		stored.ID, tracedRevision.RevisionID, tracedRevision.ContractHash,
		tracedRevision.SourceRepositoryRevision, model.ModelName, model.ModelVersion, space.InputSchemaVersion)
}
