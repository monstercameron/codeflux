package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/atomname"
	"codeflux.dev/codeflux/internal/domain"
)

// gateEvidenceG08RenamedFixtureSource is a second, DIFFERENT synthetic
// atom-comment source (a different opening identifier and different prose)
// representing the re-authored documentation an accepted rename would
// realistically be paired with -- deliberately not byte-identical to
// atomDocumentationFixtureSource, so admitting it produces a genuinely
// different NormalizedInputHash and therefore a different
// DocumentationRevisionID (M21-102), never the same revision silently
// reused under a new name.
const gateEvidenceG08RenamedFixtureSource = `package fixture

// ReserveWidgetInventoryUntilCartExpires reserves a count of widget
// inventory against a shopping cart without committing the sale.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Hold scarce widget inventory for one shopping cart so two shoppers
//     cannot both complete a sale for the same physical unit.
//   Use when:
//     A shopping cart has a stable cart identity and the caller needs a
//     short-lived hold before checkout begins.
//   Do not use when:
//     The caller wants a permanent stock decrement; use CommitWidgetSale for
//     that outcome instead, since a reservation alone never decrements stock.
//   Semantics:
//     Reserves the requested count atomically against available inventory
//     and returns a hold identity; a hold that is never captured or released
//     expires automatically after its configured lifetime.
//   Inputs:
//     - CartID identifies the shopping cart; it must be a previously issued,
//       non-expired cart identity.
//     - Count is the number of physical units requested; it must be a
//       positive integer bounded by the catalog's per-order maximum.
//   Outputs:
//     - HoldID identifies the created reservation and is required by the
//       matching release or capture call.
//     - InsufficientInventory indicates the requested count exceeds the
//       currently available count; no hold is created in this case.
//   Preconditions:
//     - The shopping cart must exist and must not already hold a
//       reservation for this catalog item.
//   Postconditions:
//     - On success, the reserved count is subtracted from available
//       inventory until the hold is released, captured, or expires.
//   Effects:
//     - Writes one inventory hold row scoped to the shopping cart with a
//       single logical reservation identity per cart and catalog item.
//   Failure semantics:
//     - InsufficientInventory is a safe, retryable outcome; a storage
//       failure before the hold is durably written is also safe to retry
//       using the same idempotency key.
//   Determinism:
//     The reservation count is deterministic given the same inventory state;
//     the generated hold identity is not deterministic across retries.
//   Idempotency and retry:
//     Logical operation identity is the pair of cart identity and catalog
//     item; a retry with the same pair returns the existing hold rather than
//     creating a second one for its configured key lifetime.
//   Reconciliation and compensation:
//     An expired, uncaptured hold is reconciled by an automatic release job;
//     no manual compensation step exists for this atom.
//   Security and privacy:
//     The cart identity is treated as a capability-scoped reference and is
//     never logged alongside catalog pricing details.
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
//     row per cart and item under concurrent reservation attempts.
//   Retrieval concepts:
//     Inventory hold, cart reservation, stock lock, oversell prevention.
func ReserveWidgetInventoryUntilCartExpires() {}
`

func mustAdmittedAtomDocumentationRevisionFromSource(
	t *testing.T,
	source string,
	atomID domain.AtomID,
	atomVersionNumber uint32,
	contractHash atomdoc.ContractHash,
) atomdoc.DocumentationRevision {
	t.Helper()
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, atomVersionNumber)
	if err != nil {
		t.Fatal(err)
	}
	result, err := atomdoc.AdmitSourceAtomDocumentation(context.Background(), atomdoc.SourceAdmissionInput{
		Candidate:                mustAtomDocumentationCandidate(t, source),
		AtomID:                   atomID,
		AtomVersion:              atomVersion,
		SourceRepositoryRevision: strings.Repeat("a", 40),
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

// TestGateEvidenceG08_RenameLineageBoundToEmbeddings is the M21-G08
// gate-evidence test: "Every reusable atom has a standalone-descriptive
// canonical name, deterministic display and normalized forms, collision
// control, and rename lineage bound to its embeddings."
//
// Standalone-descriptive canonical name, deterministic display/normalized
// forms, and collision control are already proven directly by
// internal/atomname's own test suite (grammar_test.go, identifier_test.go,
// scope_test.go) and by internal/storage's
// TestCreateAtomNameRevisionRejectsActiveCanonicalNameCollisionWithinScope
// and TestCreateAtomNameRevisionAllowsSameAtomDifferentVersionsToShareAName.
// This test's job is the piece nothing else combines: proving a real
// semantic-preserving rename's lineage actually composes with the real
// atom-vector storage layer -- the OLD documentation revision's embedding
// stays retained and traceable (M21-135) while the atom's naming-lane
// change is independently, correctly determined to require both queuing a
// new embedding and immediately invalidating the prior one's ACTIVE
// eligibility (M21-164), even though nothing automatically performs that
// invalidation as a side effect of the rename today (the same "pure logic
// proven correct, automatic wiring not yet built" pattern this report's
// M21-G04 finding also documents).
func TestGateEvidenceG08_RenameLineageBoundToEmbeddings(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9940)
	repositoryID := testRepositoryID(t, 9941)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	atomID := testAtomID(t, 9942)
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope := mustNamingScope(t, projectID, "reservations")

	// Original canonical name, matching atomDocumentationFixtureSource's
	// declared Go identifier.
	original := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	firstName := mustAtomNameRecord(t, atomID, atomVersion, scope, original, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	firstNameStored, err := repositories.CreateAtomNameRevision(ctx, firstName)
	if err != nil {
		t.Fatal(err)
	}

	contractHash := testAtomContractHash(t, '5')
	firstDoc := mustAdmittedAtomDocumentationRevision(t, atomID, 1, contractHash)
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: firstDoc,
	}); err != nil {
		t.Fatal(err)
	}

	model, err := repositories.CreateMemoryEmbeddingModel(ctx, CreateMemoryEmbeddingModel{
		ID: "g08-model", Provider: "test-provider", ModelName: "test-model", ModelVersion: "v1",
		Dimensions: 8, NumericEncoding: "float32", Normalization: "l2",
	})
	if err != nil {
		t.Fatal(err)
	}
	space, err := repositories.CreateMemoryEmbeddingSpace(ctx, CreateMemoryEmbeddingSpace{
		ID: "g08-space", EmbeddingModelID: model.ID, InputSchemaVersion: 1,
		ProjectID: projectID, SecurityScope: "project-local",
	})
	if err != nil {
		t.Fatal(err)
	}
	firstEmbedding, err := repositories.RecordAtomDocumentationEmbedding(ctx, RecordAtomDocumentationEmbedding{
		ID: "g08-embedding-1", RevisionID: firstDoc.RevisionID, EmbeddingSpaceID: space.ID,
		SourceContentSHA256: firstDoc.NormalizedInputHash.String(), Vector: []byte{1, 1, 1, 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	// A real, semantic-preserving rename (M21-157/158): new canonical name,
	// same atom and version, sequence 2 supersedes sequence 1.
	renamedCanonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCartExpires")
	secondName := mustAtomNameRecord(t, atomID, atomVersion, scope, renamedCanonical, 2, firstNameStored.RevisionID, time.Now().UTC())
	secondNameStored, err := repositories.CreateAtomNameRevision(ctx, secondName)
	if err != nil {
		t.Fatal(err)
	}
	if secondNameStored.SupersedesRevisionID != firstNameStored.RevisionID {
		t.Fatalf("secondNameStored.SupersedesRevisionID = %s, want %s", secondNameStored.SupersedesRevisionID, firstNameStored.RevisionID)
	}

	// M21-160: a new documentation revision after the accepted rename.
	secondDoc := mustAdmittedAtomDocumentationRevisionFromSource(t, gateEvidenceG08RenamedFixtureSource, atomID, 1, contractHash)
	if secondDoc.RevisionID == firstDoc.RevisionID {
		t.Fatal("test fixture is not actually a different documentation revision")
	}
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: secondDoc,
	}); err != nil {
		t.Fatal(err)
	}

	// M21-164: the naming-lane change alone (no documentation/contract/
	// dependency change modeled here) is independently determined to
	// require both queuing a new embedding and immediately invalidating the
	// prior one's active eligibility.
	beforeNameInput, err := atomname.ComposeNameEmbeddingInput(firstNameRecordFrom(firstNameStored), nil)
	if err != nil {
		t.Fatal(err)
	}
	afterNameInput, err := atomname.ComposeNameEmbeddingInput(firstNameRecordFrom(secondNameStored), nil)
	if err != nil {
		t.Fatal(err)
	}
	triggers := atomname.DetermineAtomEmbeddingLifecycleTriggers(beforeNameInput, afterNameInput, atomdoc.EmbeddingLifecycleChangeSurface{})
	if !atomdoc.RequiresQueuedReembedding(triggers) {
		t.Fatalf("triggers = %v, want RequiresQueuedReembedding true for a naming-lane rename", triggers)
	}
	if !atomdoc.RequiresImmediateInvalidation(triggers) {
		t.Fatalf("triggers = %v, want RequiresImmediateInvalidation true for a naming-lane rename", triggers)
	}

	// The OLD embedding is retained, unchanged, as historical lineage
	// (M21-135) -- never rewritten or deleted by the rename -- and the pure
	// eligibility function correctly reports it no longer active now that a
	// newer documentation revision exists for this atom, purely from
	// isCurrent=false, independent of the naming trigger.
	var (
		stillThereID    string
		stillThereValid bool
	)
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT id, valid FROM atom_documentation_embeddings WHERE revision_id = ?`, firstDoc.RevisionID.String(),
	).Scan(&stillThereID, &stillThereValid); err != nil {
		t.Fatal(err)
	}
	if stillThereID != firstEmbedding.ID || !stillThereValid {
		t.Fatalf("first embedding after rename = (id=%s valid=%v), want retained and still Valid=true (historical lineage, not deleted)", stillThereID, stillThereValid)
	}
	// The prior snapshot is never active again regardless of the trigger
	// set: DetermineEmbeddingRetrievalEligibility's isCurrent=false alone is
	// sufficient (M21-135's "every non-current snapshot is always
	// historical regardless of triggers, because a newer snapshot has
	// already superseded it").
	if got := atomdoc.DetermineEmbeddingRetrievalEligibility(false, triggers); got != atomdoc.EmbeddingRetrievalEligibilityHistorical {
		t.Fatalf("DetermineEmbeddingRetrievalEligibility(isCurrent=false, triggers) = %q, want historical", got)
	}
	// A freshly produced replacement embedding for secondDoc is evaluated
	// against ITS OWN (empty) trigger set -- nothing has changed relative to
	// itself yet -- and is active.
	if got := atomdoc.DetermineEmbeddingRetrievalEligibility(true, nil); got != atomdoc.EmbeddingRetrievalEligibilityActive {
		t.Fatalf("DetermineEmbeddingRetrievalEligibility(isCurrent=true, no triggers) = %q, want active for a freshly produced replacement", got)
	}

	// M21-107: the new documentation revision has no embedding of its own
	// yet -- it is exactly what a real (currently unbuilt) re-embedding lane
	// would need to act on.
	pending, err := repositories.ListAtomDocumentationRevisionsPendingReembedding(ctx, space.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundPendingSecond := false
	for _, id := range pending {
		if id == secondDoc.RevisionID {
			foundPendingSecond = true
		}
		if id == firstDoc.RevisionID {
			t.Fatalf("the OLD, already-embedded documentation revision must not appear as pending: %v", pending)
		}
	}
	if !foundPendingSecond {
		t.Fatalf("pending = %v, want the new post-rename documentation revision %s", pending, secondDoc.RevisionID)
	}
}

// firstNameRecordFrom adapts a stored AtomNameRevision back into the
// atomname.AtomNameRecord ComposeNameEmbeddingInput requires. This is the
// same lossless reshaping internal/retrieval/atom_name_discovery.go's
// atomNameRecordFromRevision performs, duplicated locally rather than
// exported test-only surface from another package.
func firstNameRecordFrom(revision AtomNameRevision) atomname.AtomNameRecord {
	return atomname.AtomNameRecord{
		RevisionID:           revision.RevisionID,
		AtomID:               revision.AtomID,
		AtomVersion:          revision.AtomVersion,
		Scope:                revision.Scope,
		SchemaVersion:        revision.SchemaVersion,
		CanonicalName:        revision.CanonicalName,
		DisplayName:          revision.DisplayName,
		NormalizedPhrase:     revision.NormalizedPhrase,
		Rationale:            revision.Rationale,
		RevisionSequence:     revision.RevisionSequence,
		SupersedesRevisionID: revision.SupersedesRevisionID,
		Valid:                revision.Valid,
		CreatedAt:            revision.CreatedAt,
	}
}
