package storage

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/redact"
)

// atomDocumentationFixtureSource is a synthetic, stable schema-v1 atom
// comment with substantive, non-empty content in all nineteen fields, used
// to prove GetAtomDocumentationRevision round-trips the complete document
// without discarding anything (M21-104). It deliberately mirrors
// internal/atomdoc's own validFixtureSource shape but is kept local so this
// package does not depend on atomdoc's internal test fixtures.
const atomDocumentationFixtureSource = `package fixture

// ReserveWidgetInventoryUntilCheckoutExpires reserves a count of widget
// inventory against a checkout session without committing the sale.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Hold scarce widget inventory for one checkout session so two shoppers
//     cannot both complete a sale for the same physical unit.
//   Use when:
//     A checkout session has a stable session identity and the caller needs
//     a short-lived hold before payment capture completes.
//   Do not use when:
//     The caller wants a permanent stock decrement; use CommitWidgetSale for
//     that outcome instead, since a reservation alone never decrements stock.
//   Semantics:
//     Reserves the requested count atomically against available inventory
//     and returns a hold identity; a hold that is never captured or released
//     expires automatically after its configured lifetime.
//   Inputs:
//     - SessionID identifies the checkout session; it must be a previously
//       issued, non-expired session identity.
//     - Count is the number of physical units requested; it must be a
//       positive integer bounded by the catalog's per-order maximum.
//   Outputs:
//     - HoldID identifies the created reservation and is required by the
//       matching release or capture call.
//     - InsufficientInventory indicates the requested count exceeds the
//       currently available count; no hold is created in this case.
//   Preconditions:
//     - The checkout session must exist and must not already hold a
//       reservation for this catalog item.
//   Postconditions:
//     - On success, the reserved count is subtracted from available
//       inventory until the hold is released, captured, or expires.
//   Effects:
//     - Writes one inventory hold row scoped to the checkout session with a
//       single logical reservation identity per session and catalog item.
//   Failure semantics:
//     - InsufficientInventory is a safe, retryable outcome; a storage
//       failure before the hold is durably written is also safe to retry
//       using the same idempotency key.
//   Determinism:
//     The reservation count is deterministic given the same inventory state;
//     the generated hold identity is not deterministic across retries.
//   Idempotency and retry:
//     Logical operation identity is the pair of session identity and catalog
//     item; a retry with the same pair returns the existing hold rather than
//     creating a second one for its configured key lifetime.
//   Reconciliation and compensation:
//     An expired, uncaptured hold is reconciled by an automatic release job;
//     no manual compensation step exists for this atom.
//   Security and privacy:
//     The session identity is treated as a capability-scoped reference and
//     is never logged alongside catalog pricing details.
//   Dependencies and bindings:
//     Depends on the inventory ledger's compare-and-set primitive; behavior
//     assumes the ledger enforces per-item serializable writes.
//   Complexity and limits:
//     Bounded to the catalog's configured per-order maximum count and a
//     fixed maximum hold lifetime measured in minutes.
//   Examples:
//     - A shopper reserving three units of one catalog item during checkout
//       is the representative use.
//     - Requesting zero units is a non-example; the caller must request at
//       least one unit.
//   Verification:
//     Covered by a real-storage integration test asserting exactly one hold
//     row per session and item under concurrent reservation attempts.
//   Retrieval concepts:
//     Inventory hold, checkout reservation, stock lock, oversell prevention.
func ReserveWidgetInventoryUntilCheckoutExpires() {}
`

func mustAtomDocumentationCandidate(t *testing.T, source string) atomdoc.SourceCandidate {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse fixture source: %v", err)
	}
	candidates, err := atomdoc.LocateAtomDeclarationCandidates(fset, file)
	if err != nil {
		t.Fatalf("locate atom declaration candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected exactly one candidate, got %d", len(candidates))
	}
	return candidates[0]
}

func mustTestRedactionPipeline(t *testing.T) *redact.Pipeline {
	t.Helper()
	pipeline, err := redact.NewPipeline(nil, redact.Limits{
		MaximumInputBytes:  32 * 1024,
		MaximumOutputBytes: 16 * 1024,
	})
	if err != nil {
		t.Fatalf("construct redaction pipeline: %v", err)
	}
	t.Cleanup(pipeline.Close)
	return pipeline
}

// mustAdmittedAtomDocumentationRevision drives the real
// internal/atomdoc.AdmitSourceAtomDocumentation pipeline end to end so the
// storage layer is tested against a genuinely admitted revision rather than
// a hand-rolled one, matching the M21-103..107 instruction to build tables
// for the Go record shapes atomdoc already defines.
func mustAdmittedAtomDocumentationRevision(
	t *testing.T,
	atomID domain.AtomID,
	atomVersionNumber uint32,
	contractHash atomdoc.ContractHash,
) atomdoc.DocumentationRevision {
	t.Helper()
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, atomVersionNumber)
	if err != nil {
		t.Fatalf("new atom version id: %v", err)
	}
	result, err := atomdoc.AdmitSourceAtomDocumentation(context.Background(), atomdoc.SourceAdmissionInput{
		Candidate:                mustAtomDocumentationCandidate(t, atomDocumentationFixtureSource),
		AtomID:                   atomID,
		AtomVersion:              atomVersion,
		SourceRepositoryRevision: strings.Repeat("f", 40),
		ContractHash:             contractHash,
		DependencyBindings: []atomdoc.DependencyBinding{
			{Name: "inventory-ledger", Version: "v3"},
		},
		RedactionPipeline: mustTestRedactionPipeline(t),
	})
	if err != nil {
		t.Fatalf("admit atom documentation: %v", err)
	}
	if result.Status != atomdoc.AdmissionStatusAdmitted || result.Revision == nil {
		t.Fatalf("admission result = %#v", result)
	}
	return *result.Revision
}

func testAtomID(t *testing.T, number int) domain.AtomID {
	t.Helper()
	id, err := domain.ParseAtomID("atm_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testAtomContractHash(t *testing.T, fill byte) atomdoc.ContractHash {
	t.Helper()
	hash, err := atomdoc.ParseContractHash(strings.Repeat(string(fill), 64))
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// TestAtomDocumentationFieldSpecsMatchCanonicalLabels defends against schema
// drift: atomDocumentationFieldSpecs is a hand-written copy of atomdoc's
// unexported field table (schema.go does not export a generic
// field-iteration accessor), so this test fails loudly if atomdoc's
// schema-v1 field set, order, or list/prose kind ever changes without a
// matching update here.
func TestAtomDocumentationFieldSpecsMatchCanonicalLabels(t *testing.T) {
	specs := atomDocumentationFieldSpecs(atomdoc.Document{})
	canonical := atomdoc.CanonicalFieldLabels()
	if len(specs) != len(canonical) {
		t.Fatalf("field spec count = %d, want %d", len(specs), len(canonical))
	}
	listFields := map[string]bool{
		"Inputs": true, "Outputs": true, "Preconditions": true, "Postconditions": true,
		"Effects": true, "Failure semantics": true, "Examples": true,
	}
	for index, spec := range specs {
		if spec.Label != canonical[index] {
			t.Fatalf("field %d label = %q, want %q", index, spec.Label, canonical[index])
		}
		wantKind := "prose"
		if listFields[spec.Label] {
			wantKind = "list"
		}
		if spec.Kind != wantKind {
			t.Errorf("field %q kind = %q, want %q", spec.Label, spec.Kind, wantKind)
		}
	}
}

func TestCreateAtomDocumentationRevisionPersistsIdentityAndFieldsIdempotently(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 3900)
	repositoryID := testRepositoryID(t, 3901)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 3902)
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, testAtomContractHash(t, 'a'))

	input := CreateAtomDocumentationRevision{ProjectID: projectID, Revision: revision}
	first, err := repositories.CreateAtomDocumentationRevision(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.RevisionID != revision.RevisionID || first.ProjectID != projectID ||
		first.AtomID != atomID || first.AtomVersion != revision.AtomVersion ||
		first.Authoring != atomdoc.AuthoringSourceAuthored ||
		first.ValidationStatus != AtomDocumentationValidationStatusAdmitted ||
		first.SourceCommentHash != revision.SourceCommentHash ||
		first.NormalizedInputHash != revision.NormalizedInputHash ||
		first.ContractHash != revision.ContractHash ||
		len(first.DependencyBindings) != 1 || first.DependencyBindings[0] != revision.DependencyBindings[0] {
		t.Fatalf("first revision = %#v", first)
	}

	retried, err := repositories.CreateAtomDocumentationRevision(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if retried.RevisionID != first.RevisionID {
		t.Fatalf("idempotent retry = %#v, want %#v", retried, first)
	}

	otherProjectID := testProjectID(t, 3910)
	otherRepositoryID := testRepositoryID(t, 3911)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)
	conflicting := input
	conflicting.ProjectID = otherProjectID
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, conflicting); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-project retry error = %v, want conflict", err)
	}
}

func TestCreateAtomDocumentationRevisionRejectsGeneratedProjectionAuthoring(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 3920)
	repositoryID := testRepositoryID(t, 3921)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 3922)
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, testAtomContractHash(t, 'b'))
	revision.Authoring = atomdoc.AuthoringGeneratedProjection

	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: revision,
	}); err == nil {
		t.Fatal("expected generated-projection authoring to be rejected")
	}
}

func TestGetAtomDocumentationRevisionRoundTripsAllNineteenFields(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 3930)
	repositoryID := testRepositoryID(t, 3931)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 3932)
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, testAtomContractHash(t, 'c'))
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: revision,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := repositories.GetAtomDocumentationRevision(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	wantSpecs := atomDocumentationFieldSpecs(revision.Document)
	gotSpecs := atomDocumentationFieldSpecs(got.Document)
	for index, want := range wantSpecs {
		got := gotSpecs[index]
		if got.Value.None != want.Value.None || got.Value.Reason != want.Value.Reason ||
			got.Value.Text != want.Value.Text || len(got.Value.Items) != len(want.Value.Items) {
			t.Fatalf("field %q = %#v, want %#v", want.Label, got.Value, want.Value)
		}
		for itemIndex := range want.Value.Items {
			if got.Value.Items[itemIndex] != want.Value.Items[itemIndex] {
				t.Fatalf("field %q item %d = %q, want %q", want.Label, itemIndex, got.Value.Items[itemIndex], want.Value.Items[itemIndex])
			}
		}
	}
}

func TestInvalidateAtomDocumentationRevisionIsOneWay(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 3940)
	repositoryID := testRepositoryID(t, 3941)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 3942)
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, testAtomContractHash(t, 'd'))
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: revision,
	}); err != nil {
		t.Fatal(err)
	}

	if err := repositories.InvalidateAtomDocumentationRevision(ctx, InvalidateAtomDocumentationRevision{
		RevisionID: revision.RevisionID, Reason: "comment changed",
	}); err != nil {
		t.Fatal(err)
	}
	invalidated, err := repositories.GetAtomDocumentationRevision(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if invalidated.ValidationStatus != AtomDocumentationValidationStatusInvalidated ||
		invalidated.InvalidatedReason != "comment changed" || invalidated.InvalidatedAt == nil {
		t.Fatalf("invalidated revision = %#v", invalidated)
	}

	// Repeated invalidation is rejected rather than silently accepted.
	if err := repositories.InvalidateAtomDocumentationRevision(ctx, InvalidateAtomDocumentationRevision{
		RevisionID: revision.RevisionID, Reason: "second attempt",
	}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("repeated invalidation error = %v, want stale revision", err)
	}

	// Raw-SQL attack: directly flip validation_status back to admitted.
	// The one-way trigger must reject this.
	_, rawErr := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE atom_documentation_revisions SET validation_status = 'admitted', invalidated_reason = NULL, invalidated_at_unix_micros = NULL WHERE id = ?`,
		revision.RevisionID.String(),
	)
	if !errors.Is(classify("attack: re-admit invalidated atom documentation revision", rawErr), ErrConstraint) {
		t.Fatalf("raw SQL re-admission error = %v, want constraint", rawErr)
	}
}

func TestAtomDocumentationEmbeddingLinksToExactRevisionAndSupportsPendingReembedding(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 3950)
	repositoryID := testRepositoryID(t, 3951)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	atomIDA := testAtomID(t, 3952)
	revisionA := mustAdmittedAtomDocumentationRevision(t, atomIDA, 1, testAtomContractHash(t, 'e'))
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{ProjectID: projectID, Revision: revisionA}); err != nil {
		t.Fatal(err)
	}
	atomIDB := testAtomID(t, 3953)
	revisionB := mustAdmittedAtomDocumentationRevision(t, atomIDB, 1, testAtomContractHash(t, 'f'))
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{ProjectID: projectID, Revision: revisionB}); err != nil {
		t.Fatal(err)
	}

	model, err := repositories.CreateMemoryEmbeddingModel(ctx, CreateMemoryEmbeddingModel{
		ID: "model-atomdoc-1", Provider: "test-provider", ModelName: "test-model", ModelVersion: "v1",
		Dimensions: 8, NumericEncoding: "float32", Normalization: "l2",
	})
	if err != nil {
		t.Fatal(err)
	}
	space, err := repositories.CreateMemoryEmbeddingSpace(ctx, CreateMemoryEmbeddingSpace{
		ID: "space-atomdoc-1", EmbeddingModelID: model.ID, InputSchemaVersion: 1,
		ProjectID: projectID, SecurityScope: "project-local",
	})
	if err != nil {
		t.Fatal(err)
	}

	pending, err := repositories.ListAtomDocumentationRevisionsPendingReembedding(ctx, space.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending count before any embedding = %d, want 2", len(pending))
	}

	embedding, err := repositories.RecordAtomDocumentationEmbedding(ctx, RecordAtomDocumentationEmbedding{
		ID: "embedding-atomdoc-1", RevisionID: revisionA.RevisionID, EmbeddingSpaceID: space.ID,
		SourceContentSHA256: revisionA.NormalizedInputHash.String(), Vector: []byte{1, 2, 3, 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if embedding.RevisionID != revisionA.RevisionID || !embedding.Valid {
		t.Fatalf("recorded embedding = %#v", embedding)
	}

	pending, err = repositories.ListAtomDocumentationRevisionsPendingReembedding(ctx, space.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0] != revisionB.RevisionID {
		t.Fatalf("pending after embedding A = %#v, want only %s", pending, revisionB.RevisionID)
	}

	if err := repositories.InvalidateAtomDocumentationEmbedding(ctx, embedding.ID); err != nil {
		t.Fatal(err)
	}
	pending, err = repositories.ListAtomDocumentationRevisionsPendingReembedding(ctx, space.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending after invalidating A's embedding = %d, want 2", len(pending))
	}
}

func TestAtomDocumentationEmbeddingRejectsCrossProjectSpace(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 3960)
	repositoryID := testRepositoryID(t, 3961)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	otherProjectID := testProjectID(t, 3962)
	otherRepositoryID := testRepositoryID(t, 3963)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)

	atomID := testAtomID(t, 3964)
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, testAtomContractHash(t, '1'))
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{ProjectID: projectID, Revision: revision}); err != nil {
		t.Fatal(err)
	}

	model, err := repositories.CreateMemoryEmbeddingModel(ctx, CreateMemoryEmbeddingModel{
		ID: "model-atomdoc-2", Provider: "test-provider", ModelName: "test-model", ModelVersion: "v1",
		Dimensions: 8, NumericEncoding: "float32", Normalization: "l2",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherSpace, err := repositories.CreateMemoryEmbeddingSpace(ctx, CreateMemoryEmbeddingSpace{
		ID: "space-atomdoc-2", EmbeddingModelID: model.ID, InputSchemaVersion: 1,
		ProjectID: otherProjectID, SecurityScope: "project-local",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = repositories.RecordAtomDocumentationEmbedding(ctx, RecordAtomDocumentationEmbedding{
		ID: "embedding-atomdoc-3", RevisionID: revision.RevisionID, EmbeddingSpaceID: otherSpace.ID,
		SourceContentSHA256: revision.NormalizedInputHash.String(), Vector: []byte{9},
	})
	if !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project embedding error = %v, want constraint", err)
	}
}

// TestAtomDocumentationRevisionsRawSQLAttacks proves M21-106's uniqueness
// claim and the immutability of atom_documentation_revisions/
// atom_documentation_fields against direct SQL, not merely against the Go
// API.
func TestAtomDocumentationRevisionsRawSQLAttacks(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 3970)
	repositoryID := testRepositoryID(t, 3971)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 3972)
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, testAtomContractHash(t, '2'))
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{ProjectID: projectID, Revision: revision}); err != nil {
		t.Fatal(err)
	}

	// Attack 1: a second row claiming the exact same normalized-document
	// identity tuple with a different id must be rejected (M21-106).
	forgedID := "adr_" + strings.Repeat("9", 64)
	_, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO atom_documentation_revisions (
			id, project_id, atom_id, atom_version_number, schema_version, authoring,
			source_repository_revision, source_comment_hash, normalized_input_hash,
			contract_hash, dependency_bindings_json, validation_status, created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '[]', 'admitted', 1)`,
		forgedID, projectID, revision.AtomID, revision.AtomVersion.Number(), revision.SchemaVersion,
		string(revision.Authoring), revision.SourceRepositoryRevision,
		revision.SourceCommentHash.String(), revision.NormalizedInputHash.String(), revision.ContractHash.String(),
	)
	if !errors.Is(repositoryWriteError("attack: duplicate normalized document tuple", err), ErrConflict) {
		t.Fatalf("duplicate normalized document tuple error = %v, want conflict", err)
	}

	// Attack 2: mutate an existing revision's content in place.
	_, err = repositories.database.sql.ExecContext(
		ctx, `UPDATE atom_documentation_revisions SET contract_hash = ? WHERE id = ?`,
		strings.Repeat("0", 64), revision.RevisionID.String(),
	)
	if !errors.Is(classify("attack: mutate atom documentation revision content", err), ErrConstraint) {
		t.Fatalf("content mutation error = %v, want constraint", err)
	}

	// Attack 3: delete an immutable revision.
	_, err = repositories.database.sql.ExecContext(
		ctx, `DELETE FROM atom_documentation_revisions WHERE id = ?`, revision.RevisionID.String(),
	)
	if !errors.Is(classify("attack: delete atom documentation revision", err), ErrConstraint) {
		t.Fatalf("delete error = %v, want constraint", err)
	}

	// Attack 4: mutate a field row.
	_, err = repositories.database.sql.ExecContext(
		ctx, `UPDATE atom_documentation_fields SET field_text = 'tampered' WHERE revision_id = ? AND field_label = 'Purpose'`,
		revision.RevisionID.String(),
	)
	if !errors.Is(classify("attack: mutate atom documentation field", err), ErrConstraint) {
		t.Fatalf("field mutation error = %v, want constraint", err)
	}

	// Attack 5: insert a field row whose is_none/kind/text/items shape is
	// internally inconsistent (list kind claiming prose text).
	_, err = repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO atom_documentation_fields (
			revision_id, field_label, field_kind, is_none, none_reason, field_text, field_items_json
		) VALUES (?, 'Inputs', 'list', 0, NULL, 'not a list', NULL)`,
		revision.RevisionID.String(),
	)
	if !errors.Is(classify("attack: malshaped documentation field", err), ErrConstraint) {
		t.Fatalf("malshaped field error = %v, want constraint", err)
	}

	// Attack 6: two atom_documentation_revisions rows for the same atom_id
	// under different projects (project-stability trigger).
	otherProjectID := testProjectID(t, 3980)
	otherRepositoryID := testRepositoryID(t, 3981)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)
	otherRevision := mustAdmittedAtomDocumentationRevision(t, atomID, 2, testAtomContractHash(t, '3'))
	_, err = repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: otherProjectID, Revision: otherRevision,
	})
	if !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project same-atom revision error = %v, want constraint", err)
	}
}
