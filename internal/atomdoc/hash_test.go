package atomdoc

import (
	"strings"
	"testing"
)

// TestComputeSourceCommentHashIsExactAndSensitiveToFormatting proves
// M21-120: the source-comment hash is computed over the exact raw text and
// changes for any byte-level difference, including whitespace-only changes.
func TestComputeSourceCommentHashIsExactAndSensitiveToFormatting(t *testing.T) {
	a := ComputeSourceCommentHash("// Purpose: hold inventory.")
	b := ComputeSourceCommentHash("// Purpose:  hold inventory.")
	if a == b {
		t.Fatal("expected a single extra space to change the source-comment hash")
	}
	if a.IsZero() || b.IsZero() {
		t.Fatal("expected both hashes to be non-zero")
	}
	if _, err := ParseSourceCommentHash(a.String()); err != nil {
		t.Fatalf("expected the computed hash to parse as valid, got: %v", err)
	}
}

// TestComputeNormalizedDocumentationInputHashIgnoresFormattingButNotContent
// proves M21-121: the normalized hash is stable across formatting-only
// differences already absorbed by normalizeField, but changes when
// substantive content changes.
func TestComputeNormalizedDocumentationInputHashIgnoresFormattingButNotContent(t *testing.T) {
	docA := validDocumentForTest()
	docB := validDocumentForTest()
	splitAt := strings.Index(docA.Purpose.Text, " ")
	if splitAt < 0 {
		t.Fatal("test fixture Purpose text must contain a space to split on")
	}
	// Re-derive the same field from two raw comment lines that wrap at a
	// different point but normalize back to identical text: this is the
	// formatting-only difference the normalized hash must absorb.
	docB.Purpose = normalizeField(FieldKindProse, []string{
		docA.Purpose.Text[:splitAt], docA.Purpose.Text[splitAt+1:],
	})
	if docB.Purpose.Text != docA.Purpose.Text {
		t.Fatalf("test fixture setup failed to reconstruct identical text: got %q want %q", docB.Purpose.Text, docA.Purpose.Text)
	}
	hashA := ComputeNormalizedDocumentationInputHash(SchemaVersion, docA)
	hashB := ComputeNormalizedDocumentationInputHash(SchemaVersion, docB)
	if hashA != hashB {
		t.Fatal("expected re-normalizing equivalent split content to produce the same hash")
	}

	docC := validDocumentForTest()
	docC.Purpose = Field{Text: docA.Purpose.Text + " with one additional substantive clause added."}
	hashC := ComputeNormalizedDocumentationInputHash(SchemaVersion, docC)
	if hashA == hashC {
		t.Fatal("expected changed substantive content to change the normalized-input hash")
	}
}

// TestCanonicalizeDocumentSeparatesFieldBoundaries proves the canonical hash
// input cannot be forged by moving content across a field boundary: two
// documents with the same concatenated text split differently hash
// differently.
func TestCanonicalizeDocumentSeparatesFieldBoundaries(t *testing.T) {
	docA := validDocumentForTest()
	docA.Purpose = Field{Text: "alpha beta gamma"}
	docA.UseWhen = Field{Text: "delta epsilon zeta"}

	docB := validDocumentForTest()
	docB.Purpose = Field{Text: "alpha beta gamma delta"}
	docB.UseWhen = Field{Text: "epsilon zeta"}

	hashA := ComputeNormalizedDocumentationInputHash(SchemaVersion, docA)
	hashB := ComputeNormalizedDocumentationInputHash(SchemaVersion, docB)
	if hashA == hashB {
		t.Fatal("expected different field-boundary splits of the same words to hash differently")
	}
}

// TestCanonicalizeDocumentItemBoundaryForgeryWithRealSeparatorBytes extends
// TestCanonicalizeDocumentSeparatesFieldBoundaries (which only exercises
// cross-FIELD forgery using ordinary words) to item-boundary forgery using
// the actual canonical separator byte. canonicalizeDocument itself performs
// no escaping of the control bytes it uses as item/field separators, so an
// (illegally unvalidated) Field.Items slice carrying a raw
// canonicalItemSeparator byte inside one item's content produces
// byte-identical canonical hash input to the same content split across two
// ordinary items — this is the DEFECT 1 collision primitive. It is
// ValidateAtomDocumentationSchema's firstDisallowedControlByte check (not
// canonicalizeDocument) that closes this collision in the real pipeline by
// rejecting any Field before it ever reaches this function; see
// TestValidateAtomDocumentationSchemaRejectsControlBytesInFieldContent and
// TestAdmitSourceAtomDocumentationRejectsEmbeddedSeparatorByteForgingNormalizedHashCollision
// for the enforcement point.
func TestCanonicalizeDocumentItemBoundaryForgeryWithRealSeparatorBytes(t *testing.T) {
	docA := validDocumentForTest()
	docA.Inputs = Field{Items: []string{"session identity value one", "count field value two"}}

	docB := validDocumentForTest()
	docB.Inputs = Field{Items: []string{"session identity value one" + canonicalItemSeparator + "count field value two"}}

	hashA := ComputeNormalizedDocumentationInputHash(SchemaVersion, docA)
	hashB := ComputeNormalizedDocumentationInputHash(SchemaVersion, docB)
	if hashA != hashB {
		t.Fatal("expected embedding the raw canonical item-separator byte inside one item to forge an identical canonical hash as the honest two-item split; this is exactly the collision validateFieldContent must reject before a Field ever reaches canonicalizeDocument")
	}
}

// TestSourceCommentHashAndNormalizedInputHashAreDistinctTypes proves the two
// hashes cannot be assigned to each other's typed parameters, enforced at
// compile time; this test exercises the runtime parse boundary between them.
func TestSourceCommentHashAndNormalizedInputHashAreDistinctTypes(t *testing.T) {
	raw := ComputeSourceCommentHash("// example")
	if _, err := ParseNormalizedInputHash(raw.String()); err != nil {
		t.Fatalf("expected the same 64-hex shape to satisfy both parsers, got: %v", err)
	}
	// The types remain distinct even though their string shape overlaps;
	// SourceCommentHash and NormalizedInputHash are never the same Go type,
	// so a caller cannot pass one where the other is required without an
	// explicit conversion through their string representation.
}
