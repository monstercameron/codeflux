package atomdoc

import (
	"strings"
	"testing"
)

func fullSyntheticDocumentForTest() Document {
	doc := Document{
		Purpose:      Field{Text: "Hold scarce widget inventory for one checkout session so two shoppers cannot both complete a sale for the same physical unit."},
		UseWhen:      Field{Text: "A checkout session has a stable session identity and the caller needs a short-lived hold before payment capture completes."},
		DoNotUseWhen: Field{Text: "The caller wants a permanent stock decrement; use CommitWidgetSale for that outcome instead, since a reservation alone never decrements stock."},
		Semantics:    Field{Text: "Reserves the requested count atomically against available inventory and returns a hold identity that expires automatically if never captured."},
		Inputs: Field{Items: []string{
			"SessionID identifies the checkout session and must be a previously issued, non-expired session identity.",
			"Count is the number of physical units requested and must be a positive integer bounded by the catalog maximum.",
		}},
		Outputs: Field{Items: []string{
			"HoldID identifies the created reservation and is required by the matching release or capture call.",
			"InsufficientInventory indicates the requested count exceeds the currently available count.",
		}},
		Preconditions: Field{Items: []string{
			"The checkout session must exist and must not already hold a reservation for this catalog item.",
		}},
		Postconditions: Field{Items: []string{
			"On success, the reserved count is subtracted from available inventory until released, captured, or expired.",
		}},
		Effects: Field{Items: []string{
			"Writes one inventory hold row scoped to the checkout session with a single logical reservation identity.",
		}},
		FailureSemantics: Field{Items: []string{
			"InsufficientInventory is a safe, retryable outcome that never creates a partial hold.",
		}},
		Determinism:                   Field{Text: "The reservation count is deterministic given the same inventory state; the generated hold identity is not."},
		IdempotencyAndRetry:           Field{Text: "Logical operation identity is the pair of session identity and catalog item for the configured key lifetime."},
		ReconciliationAndCompensation: Field{None: true, Reason: "the hold expires automatically and no compensation step is defined for this atom."},
		SecurityAndPrivacy:            Field{Text: "The session identity is treated as a capability-scoped reference and is never logged with pricing details."},
		DependenciesAndBindings:       Field{Text: "Depends on the inventory ledger's compare-and-set primitive under per-item serializable writes."},
		ComplexityAndLimits:           Field{Text: "Bounded to the catalog's configured per-order maximum count and a fixed maximum hold lifetime."},
		Examples: Field{Items: []string{
			"A shopper reserving three units of one catalog item during checkout is the representative use.",
			"Requesting zero units is a non-example; the caller must request at least one unit.",
		}},
		Verification:      Field{Text: "Covered by a real-storage integration test asserting exactly one hold row per session and item under concurrency."},
		RetrievalConcepts: Field{Text: "Inventory hold, checkout reservation, stock lock, oversell prevention."},
	}
	return doc
}

// TestGenerateAtomDocumentationCommentProducesParseableSchemaHeader proves
// M21-124: the generated text carries the marker and schema-version header
// that the extraction pipeline requires.
func TestGenerateAtomDocumentationCommentProducesParseableSchemaHeader(t *testing.T) {
	comment, err := GenerateAtomDocumentationComment(
		"ReserveWidgetInventoryUntilCheckoutExpires",
		"ReserveWidgetInventoryUntilCheckoutExpires reserves a count of widget inventory against a checkout session without committing the sale.",
		SchemaVersion,
		fullSyntheticDocumentForTest(),
	)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(comment, "//"+AtomDirectiveMarker+"\n") {
		t.Fatalf("expected the generated comment to carry the atom directive, got:\n%s", comment)
	}
	if !strings.Contains(comment, "// Codeflux atom documentation (schema v1):\n") {
		t.Fatalf("expected the generated comment to carry the schema-v1 header, got:\n%s", comment)
	}
	if !strings.Contains(comment, "//     None: the hold expires automatically") {
		t.Fatalf("expected the generated comment to render the explained None field, got:\n%s", comment)
	}
}

// TestGenerateAtomDocumentationCommentRejectsIncompleteDocument proves
// generation refuses to project an incomplete document rather than emitting
// a malformed comment.
func TestGenerateAtomDocumentationCommentRejectsIncompleteDocument(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.Verification = Field{}
	_, err := GenerateAtomDocumentationComment(
		"ReserveWidgetInventoryUntilCheckoutExpires",
		"ReserveWidgetInventoryUntilCheckoutExpires reserves widget inventory.",
		SchemaVersion,
		doc,
	)
	if err == nil {
		t.Fatal("expected an error for an incomplete document")
	}
}

// TestGenerateAtomDocumentationCommentRejectsBadOpeningSentence proves
// generation enforces the same opening-sentence convention as extraction.
func TestGenerateAtomDocumentationCommentRejectsBadOpeningSentence(t *testing.T) {
	_, err := GenerateAtomDocumentationComment(
		"ReserveWidgetInventoryUntilCheckoutExpires",
		"Reserves widget inventory.",
		SchemaVersion,
		fullSyntheticDocumentForTest(),
	)
	if err == nil {
		t.Fatal("expected an error when the opening sentence does not begin with the identifier")
	}
}
