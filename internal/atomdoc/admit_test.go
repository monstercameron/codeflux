package atomdoc

import (
	"context"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
)

func mustAdmissionInput(t *testing.T, source string, pipelineSecrets []credentials.Secret) SourceAdmissionInput {
	t.Helper()
	atomID, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("new atom id: %v", err)
	}
	atomVersion, err := NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatalf("new atom-version id: %v", err)
	}
	contractHash, err := ParseContractHash(strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("parse contract hash: %v", err)
	}
	return SourceAdmissionInput{
		Candidate:                mustLocateSingleCandidate(t, source),
		AtomID:                   atomID,
		AtomVersion:              atomVersion,
		SourceRepositoryRevision: strings.Repeat("f", 40),
		ContractHash:             contractHash,
		DependencyBindings: []DependencyBinding{
			{Name: "inventory-ledger", Version: "v3"},
		},
		RedactionPipeline: newTestPipeline(t, pipelineSecrets),
	}
}

// TestAdmitSourceAtomDocumentationAdmitsValidFixture proves M21-120..123 for
// the success path: both hashes are computed, the revision binds all three
// distinct identities plus the contract hash and dependency bindings, and
// the typed result reports AdmissionStatusAdmitted.
func TestAdmitSourceAtomDocumentationAdmitsValidFixture(t *testing.T) {
	input := mustAdmissionInput(t, validFixtureSource, nil)
	result, err := AdmitSourceAtomDocumentation(context.Background(), input)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if result.Status != AdmissionStatusAdmitted {
		t.Fatalf("expected AdmissionStatusAdmitted, got %s (%s: %s)", result.Status, result.RejectionReason, result.RejectionDetail)
	}
	revision := result.Revision
	if revision == nil {
		t.Fatal("expected a non-nil revision on admission")
	}
	if revision.AtomID != input.AtomID {
		t.Fatalf("atom identity mismatch: got %s want %s", revision.AtomID, input.AtomID)
	}
	if revision.AtomVersion != input.AtomVersion {
		t.Fatalf("atom-version identity mismatch: got %s want %s", revision.AtomVersion, input.AtomVersion)
	}
	if revision.RevisionID.IsZero() {
		t.Fatal("expected a non-zero documentation-revision identity")
	}
	// The three identities must be distinct values, not the same string.
	if revision.RevisionID.String() == revision.AtomID.String() || revision.RevisionID.String() == revision.AtomVersion.String() {
		t.Fatal("documentation-revision identity must be distinct from atom and atom-version identity")
	}
	if revision.SourceCommentHash.IsZero() {
		t.Fatal("expected a non-zero source-comment hash")
	}
	if revision.NormalizedInputHash.IsZero() {
		t.Fatal("expected a non-zero normalized-input hash")
	}
	if revision.SourceCommentHash.String() == revision.NormalizedInputHash.String() {
		t.Fatal("source-comment hash and normalized-input hash must not collide for a formatted fixture")
	}
	if revision.ContractHash != input.ContractHash {
		t.Fatal("expected the revision to bind the supplied contract hash")
	}
	if len(revision.DependencyBindings) != 1 || revision.DependencyBindings[0].Name != "inventory-ledger" {
		t.Fatalf("expected the revision to bind the supplied dependency bindings, got %#v", revision.DependencyBindings)
	}
	if revision.Authoring != AuthoringSourceAuthored {
		t.Fatalf("expected AuthoringSourceAuthored, got %s", revision.Authoring)
	}
}

// TestAdmitSourceAtomDocumentationSourceHashChangesOnFormattingOnlyEdit
// proves M21-120 distinguishes exact source identity from semantic content:
// a formatting-only edit changes the source-comment hash.
func TestAdmitSourceAtomDocumentationSourceHashChangesOnFormattingOnlyEdit(t *testing.T) {
	original := mustAdmissionInput(t, validFixtureSource, nil)
	originalResult, err := AdmitSourceAtomDocumentation(context.Background(), original)
	if err != nil || originalResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit original: err=%v result=%#v", err, originalResult)
	}

	reformatted := strings.Replace(validFixtureSource,
		"//   Purpose:\n//     Hold scarce widget inventory for one checkout session so two shoppers\n//     cannot both complete a sale for the same physical unit.\n",
		"//   Purpose:\n//     Hold scarce widget inventory for one checkout session so two   shoppers\n//     cannot both complete a sale for the same physical unit.\n",
		1,
	)
	if reformatted == validFixtureSource {
		t.Fatal("test fixture setup failed to introduce a formatting difference")
	}
	changed := mustAdmissionInput(t, reformatted, nil)
	changed.AtomID = original.AtomID
	changed.AtomVersion = original.AtomVersion
	changed.ContractHash = original.ContractHash
	changedResult, err := AdmitSourceAtomDocumentation(context.Background(), changed)
	if err != nil || changedResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit changed: err=%v result=%#v", err, changedResult)
	}

	if originalResult.Revision.SourceCommentHash == changedResult.Revision.SourceCommentHash {
		t.Fatal("expected the source-comment hash to change after a formatting-only edit")
	}
	if originalResult.Revision.NormalizedInputHash != changedResult.Revision.NormalizedInputHash {
		t.Fatal("expected the normalized-input hash to stay stable across a whitespace-only edit")
	}
	if originalResult.Revision.RevisionID != changedResult.Revision.RevisionID {
		t.Fatal("expected the same documentation-revision identity for the same normalized content and bindings")
	}
}

// TestAdmitSourceAtomDocumentationRejectsSchemaPolicyViolation proves the
// admission pipeline returns a typed rejection (not a Go error) for an
// otherwise well-formed but schema-invalid comment (M21-123).
func TestAdmitSourceAtomDocumentationRejectsSchemaPolicyViolation(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Verification:\n//     Covered by a real-storage integration test asserting exactly one hold\n//     row per session and item under concurrent reservation attempts.\n",
		"",
		1,
	)
	input := mustAdmissionInput(t, source, nil)
	result, err := AdmitSourceAtomDocumentation(context.Background(), input)
	if err != nil {
		t.Fatalf("expected a typed rejection rather than a Go error, got: %v", err)
	}
	if result.Status != AdmissionStatusRejected || result.RejectionReason != RejectionReasonSchemaPolicyViolation {
		t.Fatalf("expected a schema-policy rejection, got status=%s reason=%s detail=%s", result.Status, result.RejectionReason, result.RejectionDetail)
	}
	if result.Revision != nil {
		t.Fatal("expected no revision on a rejected admission")
	}
}

// TestAdmitSourceAtomDocumentationRejectsSensitiveContent proves M21-119
// end to end through the admission pipeline: a comment carrying a currently
// loaded fake credential is rejected rather than scrubbed and admitted.
func TestAdmitSourceAtomDocumentationRejectsSensitiveContent(t *testing.T) {
	const fakeLoadedSecret = "fake-loaded-provider-fixture-value-for-admission-test"
	secret, err := credentials.NewSecret([]byte(fakeLoadedSecret))
	if err != nil {
		t.Fatalf("construct fake secret: %v", err)
	}
	defer secret.Destroy()

	source := strings.Replace(validFixtureSource,
		"Inventory hold, checkout reservation, stock lock, oversell prevention.",
		"Inventory hold, checkout reservation, stock lock, "+fakeLoadedSecret+".",
		1,
	)
	input := mustAdmissionInput(t, source, []credentials.Secret{secret})
	result, err := AdmitSourceAtomDocumentation(context.Background(), input)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if result.Status != AdmissionStatusRejected || result.RejectionReason != RejectionReasonSensitiveContentDetected {
		t.Fatalf("expected a sensitive-content rejection, got status=%s reason=%s", result.Status, result.RejectionReason)
	}
	if result.Revision != nil {
		t.Fatal("expected no revision to be admitted when sensitive content is detected")
	}
}

// TestAdmitSourceAtomDocumentationRejectsEmbeddedSeparatorByteForgingNormalizedHashCollision
// is the full-pipeline reproduction of DEFECT 1. Before the fix: an Inputs
// field authored as two ordinary list items, and an Inputs field authored as
// ONE item that embeds a literal canonical item-separator byte (0x1E)
// splicing the exact same two phrases together, produced byte-identical
// canonicalizeDocument input (hash.go) and therefore an identical
// NormalizedInputHash — and both documents admitted. Since
// DocumentationRevisionID is derived from that hash (M21-102), this was an
// identity-collision primitive that defeated the exact-identity guarantee
// TestGeneratedCommentRoundTripsThroughASTExtractionWithSemanticIdentity
// (roundtrip_test.go, M21-125) claims to establish. After the fix, the
// spliced document is rejected outright with RejectionReasonSchemaPolicyViolation,
// so the collision can never manifest in an admitted revision.
func TestAdmitSourceAtomDocumentationRejectsEmbeddedSeparatorByteForgingNormalizedHashCollision(t *testing.T) {
	item1 := "SessionID identifies the checkout session; it must be a previously issued, non-expired session identity."
	item2 := "Count is the number of physical units requested; it must be a positive integer bounded by the catalog's per-order maximum."
	spliced := item1 + "\x1e" + item2

	honestSource := validFixtureSource
	forgedSource := strings.Replace(validFixtureSource,
		"//     - SessionID identifies the checkout session; it must be a previously\n"+
			"//       issued, non-expired session identity.\n"+
			"//     - Count is the number of physical units requested; it must be a\n"+
			"//       positive integer bounded by the catalog's per-order maximum.\n",
		"//     - "+spliced+"\n",
		1,
	)
	if forgedSource == honestSource {
		t.Fatal("test fixture setup failed to splice the Inputs field into one item carrying the embedded separator byte")
	}

	honestInput := mustAdmissionInput(t, honestSource, nil)
	forgedInput := mustAdmissionInput(t, forgedSource, nil)
	// Bind both documents to the same atom identity, version, and contract so
	// only the Inputs-field authoring difference distinguishes them.
	forgedInput.AtomID = honestInput.AtomID
	forgedInput.AtomVersion = honestInput.AtomVersion
	forgedInput.ContractHash = honestInput.ContractHash

	honestResult, err := AdmitSourceAtomDocumentation(context.Background(), honestInput)
	if err != nil || honestResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit honest two-item fixture: err=%v result=%#v", err, honestResult)
	}

	forgedResult, err := AdmitSourceAtomDocumentation(context.Background(), forgedInput)
	if err != nil {
		t.Fatalf("expected a typed rejection rather than a Go error for the embedded-separator-byte forgery, got: %v", err)
	}
	if forgedResult.Status != AdmissionStatusAdmitted {
		if forgedResult.Status != AdmissionStatusRejected || forgedResult.RejectionReason != RejectionReasonSchemaPolicyViolation {
			t.Fatalf("expected a schema-policy rejection for the embedded-separator-byte forgery, got status=%s reason=%s detail=%s",
				forgedResult.Status, forgedResult.RejectionReason, forgedResult.RejectionDetail)
		}
		return
	}
	// Pre-fix behavior (documented for the record, not the expected path):
	// the forgery was admitted and its NormalizedInputHash collided exactly
	// with the honest document's, even though the two documents' Inputs
	// field carries a different number of list items.
	if forgedResult.Revision.NormalizedInputHash == honestResult.Revision.NormalizedInputHash {
		t.Fatalf("DEFECT 1 reproduced: the embedded-separator-byte forgery was admitted with a NormalizedInputHash colliding with the honest document (%s)",
			forgedResult.Revision.NormalizedInputHash)
	}
	t.Fatal("expected the embedded-separator-byte forgery to be rejected, not admitted")
}

// TestAdmitSourceAtomDocumentationRespectsCancellation proves the admission
// entry point honors context cancellation rather than ignoring ctx.
func TestAdmitSourceAtomDocumentationRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := mustAdmissionInput(t, validFixtureSource, nil)
	_, err := AdmitSourceAtomDocumentation(ctx, input)
	if err == nil {
		t.Fatal("expected an error for an already-canceled context")
	}
}
