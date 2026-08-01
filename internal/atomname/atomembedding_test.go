package atomname

import (
	"context"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/redact"
)

// fullSyntheticDocumentationSourceForTest is a self-contained schema-v1 atom
// comment (mirroring internal/atomdoc's own fixtures) used only by this
// package's tests to admit a real atomdoc.DocumentationRevision through the
// exported AdmitSourceAtomDocumentation pipeline, without depending on
// atomdoc's unexported test helpers.
const fullSyntheticDocumentationSourceForTest = `package fixture

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

// mustAdmittedDocumentationRevision admits
// fullSyntheticDocumentationSourceForTest through the real, exported
// atomdoc pipeline and returns the resulting revision, failing the test on
// any error or rejection.
func mustAdmittedDocumentationRevision(t *testing.T) *atomdoc.DocumentationRevision {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", fullSyntheticDocumentationSourceForTest, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse fixture source: %v", err)
	}
	candidates, err := atomdoc.LocateAtomDeclarationCandidates(fset, file)
	if err != nil {
		t.Fatalf("locate candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected exactly one candidate, got %d", len(candidates))
	}

	atomVersion := mustAtomVersionID(t)
	contractHash, err := atomdoc.ParseContractHash(strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("parse contract hash: %v", err)
	}
	pipeline, err := redact.NewPipeline(nil, redact.Limits{MaximumInputBytes: 32 * 1024, MaximumOutputBytes: 16 * 1024})
	if err != nil {
		t.Fatalf("construct redaction pipeline: %v", err)
	}
	t.Cleanup(pipeline.Close)

	result, err := atomdoc.AdmitSourceAtomDocumentation(context.Background(), atomdoc.SourceAdmissionInput{
		Candidate:                candidates[0],
		AtomID:                   atomVersion.AtomID(),
		AtomVersion:              atomVersion,
		SourceRepositoryRevision: strings.Repeat("f", 40),
		ContractHash:             contractHash,
		DependencyBindings:       []atomdoc.DependencyBinding{{Name: "inventory-ledger", Version: "v3"}},
		RedactionPipeline:        pipeline,
	})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if result.Status != atomdoc.AdmissionStatusAdmitted {
		t.Fatalf("expected admission, got status=%s reason=%s detail=%s", result.Status, result.RejectionReason, result.RejectionDetail)
	}
	return result.Revision
}

func mustAtomEmbeddingInputTestRecord(t *testing.T, canonical string) (AtomNameRecord, atomdoc.Document) {
	t.Helper()
	revision := mustAdmittedDocumentationRevision(t)
	scope, err := NewNamingScope(mustProjectID(t), "internal/inventory")
	if err != nil {
		t.Fatalf("NewNamingScope failed: %v", err)
	}
	name, err := NewCanonicalName(canonical)
	if err != nil {
		t.Fatalf("NewCanonicalName(%q) failed: %v", canonical, err)
	}
	rationale, err := NewNamingRationale("nearest confusing alternative name", "distinguishing qualifier text")
	if err != nil {
		t.Fatalf("NewNamingRationale failed: %v", err)
	}
	record, err := NewAtomNameRecord(revision.AtomID, revision.AtomVersion, scope, name, rationale, 1, AtomNameRevisionID{}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewAtomNameRecord failed: %v", err)
	}
	return record, revision.Document
}

// TestComposeAtomEmbeddingInputIncludesCanonicalNameAndPhraseExactlyOnce is
// the AGENTS.md-binding requirement this whole lane must satisfy end to
// end: the FULL combined atom embedding input (naming segments plus
// documentation segments) carries the canonical name and normalized phrase
// exactly once each, even when a documentation field's prose happens to
// mention the atom's own display name in passing (a realistic Retrieval
// concepts entry), because the documentation lane structurally cannot
// contribute a canonical-name or normalized-phrase segment at all.
func TestComposeAtomEmbeddingInputIncludesCanonicalNameAndPhraseExactlyOnce(t *testing.T) {
	record, doc := mustAtomEmbeddingInputTestRecord(t, "ReserveWidgetInventoryUntilCheckoutExpires")

	// Adversarial: RetrievalConcepts mentions the canonical name's own
	// display-name phrase in ordinary domain prose.
	doc.RetrievalConcepts = atomdoc.Field{Text: "Also known as reserve widget inventory until checkout expires among the checkout team."}

	input, err := ComposeAtomEmbeddingInput(record, nil, doc)
	if err != nil {
		t.Fatalf("ComposeAtomEmbeddingInput failed: %v", err)
	}
	if input.SchemaVersion != atomdoc.EmbeddingInputSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", input.SchemaVersion, atomdoc.EmbeddingInputSchemaVersion)
	}

	canonicalCount, phraseCount := 0, 0
	for _, segment := range input.Name.Segments {
		switch segment.Role {
		case EmbeddingSegmentRoleCanonicalName:
			canonicalCount++
		case EmbeddingSegmentRoleNormalizedPhrase:
			phraseCount++
		}
	}
	if canonicalCount != 1 {
		t.Errorf("expected exactly one canonical-name segment across the combined input, got %d", canonicalCount)
	}
	if phraseCount != 1 {
		t.Errorf("expected exactly one normalized-phrase segment across the combined input, got %d", phraseCount)
	}

	// The documentation lane's segments can never carry a name/phrase role:
	// atomdoc.EmbeddingInputFieldRole has no such value, so this is a
	// structural (type-level), not merely observed, guarantee. This loop
	// documents that guarantee by construction: every documentation role in
	// use is one of the declared documentation-only roles.
	allowedDocumentationRoles := map[atomdoc.EmbeddingInputFieldRole]bool{
		atomdoc.EmbeddingInputFieldRolePurpose:               true,
		atomdoc.EmbeddingInputFieldRoleUseWhen:               true,
		atomdoc.EmbeddingInputFieldRoleDoNotUseWhen:          true,
		atomdoc.EmbeddingInputFieldRoleSemantics:             true,
		atomdoc.EmbeddingInputFieldRoleInputOutputMeaning:    true,
		atomdoc.EmbeddingInputFieldRoleEffects:               true,
		atomdoc.EmbeddingInputFieldRoleFailureSemantics:      true,
		atomdoc.EmbeddingInputFieldRoleRetrievalConcepts:     true,
		atomdoc.EmbeddingInputFieldRoleRetryContext:          true,
		atomdoc.EmbeddingInputFieldRoleReconciliationContext: true,
		atomdoc.EmbeddingInputFieldRoleSecurityContext:       true,
		atomdoc.EmbeddingInputFieldRoleDependencyContext:     true,
		atomdoc.EmbeddingInputFieldRoleLimitContext:          true,
	}
	for _, segment := range input.Documentation.Segments {
		if !allowedDocumentationRoles[segment.Role] {
			t.Errorf("unexpected documentation-lane role %q", segment.Role)
		}
	}
}

// TestComputeAtomEmbeddingInputHashDeterministicAndContentSensitive proves
// the combined input hash (M21-132) is stable for identical content and
// changes when either lane's content changes.
func TestComputeAtomEmbeddingInputHashDeterministicAndContentSensitive(t *testing.T) {
	record, doc := mustAtomEmbeddingInputTestRecord(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	inputA, err := ComposeAtomEmbeddingInput(record, nil, doc)
	if err != nil {
		t.Fatalf("ComposeAtomEmbeddingInput failed: %v", err)
	}
	inputB, err := ComposeAtomEmbeddingInput(record, nil, doc)
	if err != nil {
		t.Fatalf("ComposeAtomEmbeddingInput failed: %v", err)
	}
	if ComputeAtomEmbeddingInputHash(inputA) != ComputeAtomEmbeddingInputHash(inputB) {
		t.Error("expected identical composed input to hash identically")
	}

	changedDoc := doc
	changedDoc.Purpose = atomdoc.Field{Text: "A materially different purpose statement describing a different domain outcome entirely."}
	inputC, err := ComposeAtomEmbeddingInput(record, nil, changedDoc)
	if err != nil {
		t.Fatalf("ComposeAtomEmbeddingInput failed: %v", err)
	}
	if ComputeAtomEmbeddingInputHash(inputA) == ComputeAtomEmbeddingInputHash(inputC) {
		t.Error("expected a changed Purpose field to change the combined embedding-input hash")
	}
}

// TestComputeAtomEmbeddingInputHashResistsEmbeddedSeparatorByteForging
// proves the length-prefixed encoding closes the same collision class
// atomdoc/hash.go's canonicalizeDocument had to defend against (DEFECT 1):
// two segments "alpha","beta" must not hash identically to one segment
// "alpha\x1ebeta" splicing the same bytes together, even though naming-lane
// alias text carries no control-byte restriction the way documentation
// content does.
func TestComputeAtomEmbeddingInputHashResistsEmbeddedSeparatorByteForging(t *testing.T) {
	honest := AtomEmbeddingInput{
		SchemaVersion: atomdoc.EmbeddingInputSchemaVersion,
		Name: NameEmbeddingInput{Segments: []EmbeddingInputSegment{
			{Role: EmbeddingSegmentRoleAliasDiscovery, Text: "alpha", LowWeight: true},
			{Role: EmbeddingSegmentRoleAliasDiscovery, Text: "beta", LowWeight: true},
		}},
	}
	forged := AtomEmbeddingInput{
		SchemaVersion: atomdoc.EmbeddingInputSchemaVersion,
		Name: NameEmbeddingInput{Segments: []EmbeddingInputSegment{
			{Role: EmbeddingSegmentRoleAliasDiscovery, Text: "alpha\x1ebeta", LowWeight: true},
		}},
	}
	if ComputeAtomEmbeddingInputHash(honest) == ComputeAtomEmbeddingInputHash(forged) {
		t.Fatal("embedded-separator-byte forgery collided with the honest two-segment input")
	}
}

// TestNewEmbeddingProvenanceBindsModelDimensionsNormalizationAndInputHash is
// M21-132: the typed provenance record binds the embedding model,
// dimensions, normalization, input-schema version, and input hash together,
// with the hash always derived from the given input rather than accepted
// from the caller.
func TestNewEmbeddingProvenanceBindsModelDimensionsNormalizationAndInputHash(t *testing.T) {
	record, doc := mustAtomEmbeddingInputTestRecord(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	input, err := ComposeAtomEmbeddingInput(record, nil, doc)
	if err != nil {
		t.Fatalf("ComposeAtomEmbeddingInput failed: %v", err)
	}
	model, err := NewEmbeddingModelIdentity("openai", "text-embedding-3-large", "2024-01-01")
	if err != nil {
		t.Fatalf("NewEmbeddingModelIdentity failed: %v", err)
	}
	normalization, err := NewEmbeddingNormalization("l2-unit")
	if err != nil {
		t.Fatalf("NewEmbeddingNormalization failed: %v", err)
	}

	provenance, err := NewEmbeddingProvenance(model, 3072, normalization, input)
	if err != nil {
		t.Fatalf("NewEmbeddingProvenance failed: %v", err)
	}
	if !provenance.Model.Equal(model) {
		t.Errorf("Model = %v, want %v", provenance.Model, model)
	}
	if provenance.Dimensions != 3072 {
		t.Errorf("Dimensions = %d, want 3072", provenance.Dimensions)
	}
	if !provenance.Normalization.Equal(normalization) {
		t.Errorf("Normalization = %v, want %v", provenance.Normalization, normalization)
	}
	if provenance.InputSchemaVersion != atomdoc.EmbeddingInputSchemaVersion {
		t.Errorf("InputSchemaVersion = %d, want %d", provenance.InputSchemaVersion, atomdoc.EmbeddingInputSchemaVersion)
	}
	if provenance.InputHash != ComputeAtomEmbeddingInputHash(input) {
		t.Error("expected InputHash to equal ComputeAtomEmbeddingInputHash(input)")
	}

	if _, err := NewEmbeddingProvenance(EmbeddingModelIdentity{}, 3072, normalization, input); err == nil {
		t.Error("expected a zero-value model identity to be rejected")
	}
	if _, err := NewEmbeddingProvenance(model, 0, normalization, input); err == nil {
		t.Error("expected zero dimensions to be rejected")
	}
}

// TestEmbeddingModelChangeRegeneratesVectorWithoutRewritingHistoricalDocumentation
// is M21-143: an embedding-model change alone (dimensions/normalization
// changing is the realistic companion of a model change) must regenerate
// the derived vector — DetermineEmbeddingConfigChanged reports true and the
// combined trigger set requires queuing a new embedding — while the
// underlying atom-documentation revision is admitted exactly once in this
// test and never rewritten: both EmbeddingProvenance snapshots are derived
// from the SAME AtomEmbeddingInput (same InputHash), proving the
// documentation content is untouched by the config change.
func TestEmbeddingModelChangeRegeneratesVectorWithoutRewritingHistoricalDocumentation(t *testing.T) {
	record, doc := mustAtomEmbeddingInputTestRecord(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	input, err := ComposeAtomEmbeddingInput(record, nil, doc)
	if err != nil {
		t.Fatalf("ComposeAtomEmbeddingInput failed: %v", err)
	}

	normalization, err := NewEmbeddingNormalization("l2-unit")
	if err != nil {
		t.Fatalf("NewEmbeddingNormalization failed: %v", err)
	}
	modelBefore, err := NewEmbeddingModelIdentity("openai", "text-embedding-3-large", "2024-01-01")
	if err != nil {
		t.Fatalf("NewEmbeddingModelIdentity failed: %v", err)
	}
	modelAfter, err := NewEmbeddingModelIdentity("openai", "text-embedding-4", "2026-01-01")
	if err != nil {
		t.Fatalf("NewEmbeddingModelIdentity failed: %v", err)
	}

	before, err := NewEmbeddingProvenance(modelBefore, 3072, normalization, input)
	if err != nil {
		t.Fatalf("NewEmbeddingProvenance(before) failed: %v", err)
	}
	after, err := NewEmbeddingProvenance(modelAfter, 4096, normalization, input)
	if err != nil {
		t.Fatalf("NewEmbeddingProvenance(after) failed: %v", err)
	}

	if !DetermineEmbeddingConfigChanged(before, after) {
		t.Fatal("expected a model/dimensions change to be reported as an embedding-config change")
	}
	// The composed AtomEmbeddingInput — and therefore the documentation
	// content it derives from — is identical for both snapshots: only the
	// model config differs, so the hash of the underlying input is the same.
	if before.InputHash != after.InputHash {
		t.Fatal("expected the same underlying embedding input (unchanged documentation) for both provenance snapshots")
	}

	sameName, err := ComposeNameEmbeddingInput(record, nil)
	if err != nil {
		t.Fatalf("ComposeNameEmbeddingInput failed: %v", err)
	}
	triggers := DetermineAtomEmbeddingLifecycleTriggers(sameName, sameName, atomdoc.EmbeddingLifecycleChangeSurface{
		EmbeddingConfigChanged: DetermineEmbeddingConfigChanged(before, after),
	})
	if !atomdoc.RequiresQueuedReembedding(triggers) {
		t.Error("expected the embedding-model change to queue a new embedding")
	}
	if atomdoc.RequiresImmediateInvalidation(triggers) {
		t.Error("expected the embedding-model change alone not to require immediate invalidation: the prior vector still describes unchanged, currently valid semantics")
	}
}

// TestDetermineAtomEmbeddingLifecycleTriggersExtendsNamingTriggers is
// M21-164: a canonical-name (and therefore normalized-phrase) change with
// no documentation-lane change must still produce a combined trigger that
// both queues a new embedding and immediately invalidates the stale one,
// exactly like AGENTS.md's "invalidate and regenerate" language for naming
// changes — proven here by calling
// DetermineEmbeddingInvalidationTriggers (unchanged, unduplicated) through
// DetermineAtomEmbeddingLifecycleTriggers rather than reimplementing name
// comparison.
func TestDetermineAtomEmbeddingLifecycleTriggersExtendsNamingTriggers(t *testing.T) {
	recordBefore := mustEmbeddingTestRecord(t, "ReserveAccountFunds")
	recordAfter := mustEmbeddingTestRecord(t, "ReserveAccountFundsUntilAuthorizationExpires")

	nameBefore, err := ComposeNameEmbeddingInput(recordBefore, nil)
	if err != nil {
		t.Fatalf("ComposeNameEmbeddingInput(before) failed: %v", err)
	}
	nameAfter, err := ComposeNameEmbeddingInput(recordAfter, nil)
	if err != nil {
		t.Fatalf("ComposeNameEmbeddingInput(after) failed: %v", err)
	}

	// No documentation-lane change: an entirely unchanged surface.
	triggers := DetermineAtomEmbeddingLifecycleTriggers(nameBefore, nameAfter, atomdoc.EmbeddingLifecycleChangeSurface{})

	found := false
	for _, trigger := range triggers {
		if trigger == atomdoc.EmbeddingLifecycleTriggerNamingLaneChanged {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected EmbeddingLifecycleTriggerNamingLaneChanged in %v", triggers)
	}
	if !atomdoc.RequiresQueuedReembedding(triggers) {
		t.Error("expected a naming-lane change to queue a new embedding (M21-164)")
	}
	if !atomdoc.RequiresImmediateInvalidation(triggers) {
		t.Error("expected a naming-lane change to also immediately invalidate the stale vector (M21-164)")
	}

	// No change at all on either lane: no triggers.
	if unchanged := DetermineAtomEmbeddingLifecycleTriggers(nameBefore, nameBefore, atomdoc.EmbeddingLifecycleChangeSurface{}); len(unchanged) != 0 {
		t.Errorf("expected no triggers when neither lane changed, got %v", unchanged)
	}
}
