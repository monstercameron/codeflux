package atomdoc

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestGeneratedCommentRoundTripsThroughASTExtractionWithSemanticIdentity is
// the decisive M21-125 test: it generates a full schema-v1 Go doc comment
// from a structured Document (as SQLite-authored atom documentation would
// be projected into generated Go, M21-124), embeds the generated text as the
// doc comment of a real Go function declaration, re-parses that source with
// go/parser, re-locates the declaration via LocateAtomDeclarationCandidates,
// and re-extracts the Document via ParseAtomDocumentationComment plus
// ValidateAtomDocumentationSchema — the exact same pipeline used for
// source-authored atoms (M21-108..116).
//
// It then proves semantic identity survived the round trip by comparing:
//  1. the reparsed Document to the original field-by-field (every prose
//     field, every list item in order, and the one explained-None field);
//  2. the normalized-input hash (M21-121) computed from each side, which
//     must match exactly since the schema declares hash identity as the
//     definition of "same normalized content".
func TestGeneratedCommentRoundTripsThroughASTExtractionWithSemanticIdentity(t *testing.T) {
	const identifier = "ReserveWidgetInventoryUntilCheckoutExpires"
	const openingSentence = identifier + " reserves a count of widget inventory against a checkout session without committing the sale."

	original := fullSyntheticDocumentForTest()

	generatedComment, err := GenerateAtomDocumentationComment(identifier, openingSentence, SchemaVersion, original)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := "package fixture\n\n" + generatedComment + "func " + identifier + "() {}\n"

	candidate := mustLocateSingleCandidate(t, source)
	if candidate.Identifier != identifier {
		t.Fatalf("unexpected re-extracted identifier %q", candidate.Identifier)
	}

	parsed, err := ParseAtomDocumentationComment(candidate)
	if err != nil {
		t.Fatalf("re-parse generated comment: %v", err)
	}
	if parsed.OpeningSentence != openingSentence {
		t.Fatalf("opening sentence did not round-trip:\n got:  %q\n want: %q", parsed.OpeningSentence, openingSentence)
	}
	if parsed.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version did not round-trip: got %d want %d", parsed.SchemaVersion, SchemaVersion)
	}

	reparsed, err := ValidateAtomDocumentationSchema(parsed.Fields)
	if err != nil {
		t.Fatalf("re-validate generated comment: %v", err)
	}

	// 1. Field-by-field semantic comparison.
	for _, spec := range fieldSpecs {
		want := *spec.get(&original)
		got := *spec.get(&reparsed)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("field %q did not round-trip:\n got:  %#v\n want: %#v", spec.Label, got, want)
		}
	}
	if !reflect.DeepEqual(original, reparsed) {
		t.Fatalf("full document did not round-trip:\n got:  %#v\n want: %#v", reparsed, original)
	}

	// 2. Normalized-input hash identity: the schema's own definition of
	// "same normalized content" must hold across the round trip.
	originalHash := ComputeNormalizedDocumentationInputHash(SchemaVersion, original)
	reparsedHash := ComputeNormalizedDocumentationInputHash(SchemaVersion, reparsed)
	if originalHash != reparsedHash {
		t.Fatalf("normalized-input hash did not round-trip: got %s want %s", reparsedHash, originalHash)
	}

	// Regenerating from the reparsed document must also be byte-identical to
	// the first generation, proving the projection is a stable fixed point.
	regeneratedComment, err := GenerateAtomDocumentationComment(identifier, openingSentence, SchemaVersion, reparsed)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if regeneratedComment != generatedComment {
		t.Fatalf("regenerated comment is not a stable fixed point:\n got:\n%s\nwant:\n%s", regeneratedComment, generatedComment)
	}
}

// TestGeneratedCommentRoundTripAdmitsThroughFullPipeline proves the
// generated-then-reparsed comment is not just structurally equal but also
// admits cleanly through the full AdmitSourceAtomDocumentation pipeline,
// including hashing, scrubbing, and revision binding.
func TestGeneratedCommentRoundTripAdmitsThroughFullPipeline(t *testing.T) {
	const identifier = "ReserveWidgetInventoryUntilCheckoutExpires"
	const openingSentence = identifier + " reserves a count of widget inventory against a checkout session without committing the sale."

	generatedComment, err := GenerateAtomDocumentationComment(identifier, openingSentence, SchemaVersion, fullSyntheticDocumentForTest())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	source := "package fixture\n\n" + generatedComment + "func " + identifier + "() {}\n"

	input := mustAdmissionInput(t, source, nil)
	result, err := AdmitSourceAtomDocumentation(t.Context(), input)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if result.Status != AdmissionStatusAdmitted {
		t.Fatalf("expected the round-tripped comment to admit cleanly, got status=%s reason=%s detail=%s",
			result.Status, result.RejectionReason, result.RejectionDetail)
	}
}

// --- Adversarial round-trip fixtures ---
//
// TestGeneratedCommentRoundTripsThroughASTExtractionWithSemanticIdentity uses
// a punctuation-light, unicode-free, boundary-length-free fixture
// (fullSyntheticDocumentForTest), so it cannot exercise either reproduced
// defect (embedded canonical-hash separator bytes; regex field-header
// misparsing of sub-header prose) or a range of other adversarial content.
// The tests below drive the exact same generate -> embed -> locate -> parse
// -> validate pipeline with adversarial field content and record, per case,
// whether it round-trips cleanly or is rejected (a typed rejection is an
// acceptable outcome; silent corruption is not).

// roundTripDocument runs doc through the full generate -> embed -> locate ->
// parse -> validate pipeline (the same path M21-125 exercises) and returns
// the reparsed Document, failing the test if any stage rejects it.
func roundTripDocument(t *testing.T, doc Document) Document {
	t.Helper()
	const identifier = "ReserveWidgetInventoryUntilCheckoutExpires"
	const openingSentence = identifier + " reserves a count of widget inventory against a checkout session without committing the sale."
	comment, err := GenerateAtomDocumentationComment(identifier, openingSentence, SchemaVersion, doc)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	source := "package fixture\n\n" + comment + "func " + identifier + "() {}\n"
	candidate := mustLocateSingleCandidate(t, source)
	parsed, err := ParseAtomDocumentationComment(candidate)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	reparsed, err := ValidateAtomDocumentationSchema(parsed.Fields)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	return reparsed
}

// roundTripDocumentRejection runs doc through the same pipeline as
// roundTripDocument but returns the first error encountered (from generate,
// parse, or validate) instead of failing the test, for adversarial cases
// that are expected to be rejected.
func roundTripDocumentRejection(t *testing.T, doc Document) error {
	t.Helper()
	const identifier = "ReserveWidgetInventoryUntilCheckoutExpires"
	const openingSentence = identifier + " reserves a count of widget inventory against a checkout session without committing the sale."
	comment, err := GenerateAtomDocumentationComment(identifier, openingSentence, SchemaVersion, doc)
	if err != nil {
		return err
	}
	source := "package fixture\n\n" + comment + "func " + identifier + "() {}\n"
	candidate := mustLocateSingleCandidate(t, source)
	parsed, err := ParseAtomDocumentationComment(candidate)
	if err != nil {
		return err
	}
	_, err = ValidateAtomDocumentationSchema(parsed.Fields)
	return err
}

// TestAdversarialRoundTripColonsInValues proves a colon embedded mid-line
// inside ordinary prose (not alone on its own line) round-trips cleanly:
// only a line matching the whole-line field-header shape at header depth is
// ever treated as a field boundary.
func TestAdversarialRoundTripColonsInValues(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.Purpose = Field{Text: "Hold scarce widget inventory at a ratio: 3:1 versus demand, and note: oversubscription must never occur for this atom."}
	reparsed := roundTripDocument(t, doc)
	if reparsed.Purpose.Text != doc.Purpose.Text {
		t.Fatalf("colon-bearing value did not round-trip:\n got:  %q\n want: %q", reparsed.Purpose.Text, doc.Purpose.Text)
	}
}

// TestAdversarialRoundTripUnicodeCombiningMarksAndRTL proves field content
// carrying combining marks and right-to-left script (including an explicit
// RTL mark codepoint) round-trips byte-identically: none of these are C0
// control bytes, so the DEFECT 1 rejection must not false-positive on them.
func TestAdversarialRoundTripUnicodeCombiningMarksAndRTL(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.RetrievalConcepts = Field{Text: "Café résumé identifiers, Arabic اختبار and Hebrew בדיקה with an RTL mark ‏ embedded, three ascii words present here."}
	reparsed := roundTripDocument(t, doc)
	if reparsed.RetrievalConcepts.Text != doc.RetrievalConcepts.Text {
		t.Fatalf("unicode value did not round-trip byte-identically:\n got:  %q\n want: %q", reparsed.RetrievalConcepts.Text, doc.RetrievalConcepts.Text)
	}
}

// TestAdversarialRoundTripGoCommentTerminatorLookingContent proves field
// content containing the literal byte sequence "*/" round-trips cleanly:
// GenerateAtomDocumentationComment always emits "//" line comments, so "*/"
// inside a value is inert text, never a block-comment terminator.
func TestAdversarialRoundTripGoCommentTerminatorLookingContent(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.Semantics = Field{Text: "A value containing the literal sequence */ must never be treated as ending this line comment early during parsing."}
	reparsed := roundTripDocument(t, doc)
	if reparsed.Semantics.Text != doc.Semantics.Text {
		t.Fatalf("comment-terminator-looking value did not round-trip:\n got:  %q\n want: %q", reparsed.Semantics.Text, doc.Semantics.Text)
	}
}

// fixedLengthProseText builds a substantive (>= 3 ASCII words) prose string
// of exactly totalBytes length, for exercising the declared Maximum* bounds.
func fixedLengthProseText(totalBytes int) string {
	const prefix = "alpha bravo charlie "
	if totalBytes <= len(prefix) {
		return strings.Repeat("a", totalBytes)
	}
	return prefix + strings.Repeat("x", totalBytes-len(prefix))
}

// syntheticListItems builds n distinct, substantive list items, for
// exercising the declared MaximumListItems bound.
func syntheticListItems(n int) []string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf("List entry number %d describes example content for bound testing.", i)
	}
	return items
}

// TestAdversarialRoundTripProseFieldAtMaximumLengthBound proves a prose
// field exactly at MaximumFieldTextBytes round-trips cleanly.
func TestAdversarialRoundTripProseFieldAtMaximumLengthBound(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.Purpose = Field{Text: fixedLengthProseText(MaximumFieldTextBytes)}
	reparsed := roundTripDocument(t, doc)
	if reparsed.Purpose.Text != doc.Purpose.Text {
		t.Fatalf("maximum-length prose value did not round-trip: got length %d want length %d", len(reparsed.Purpose.Text), len(doc.Purpose.Text))
	}
}

// TestAdversarialRoundTripProseFieldExceedingMaximumLengthBoundIsRejected
// proves a prose field one byte over MaximumFieldTextBytes is rejected
// (typed rejection, not silent truncation or corruption).
func TestAdversarialRoundTripProseFieldExceedingMaximumLengthBoundIsRejected(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.Purpose = Field{Text: fixedLengthProseText(MaximumFieldTextBytes + 1)}
	err := roundTripDocumentRejection(t, doc)
	if err == nil || !strings.Contains(err.Error(), "exceeds the maximum length") {
		t.Fatalf("expected an exceeds-maximum-length rejection, got: %v", err)
	}
}

// TestAdversarialRoundTripListItemAtMaximumLengthBound proves a single list
// item exactly at MaximumListItemBytes round-trips cleanly.
func TestAdversarialRoundTripListItemAtMaximumLengthBound(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.Examples = Field{Items: []string{fixedLengthProseText(MaximumListItemBytes)}}
	reparsed := roundTripDocument(t, doc)
	if len(reparsed.Examples.Items) != 1 || reparsed.Examples.Items[0] != doc.Examples.Items[0] {
		t.Fatalf("maximum-length list item did not round-trip: got %#v want %#v", reparsed.Examples.Items, doc.Examples.Items)
	}
}

// TestAdversarialRoundTripListItemExceedingMaximumLengthBoundIsRejected
// proves a single list item one byte over MaximumListItemBytes is rejected.
func TestAdversarialRoundTripListItemExceedingMaximumLengthBoundIsRejected(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.Examples = Field{Items: []string{fixedLengthProseText(MaximumListItemBytes + 1)}}
	err := roundTripDocumentRejection(t, doc)
	if err == nil || !strings.Contains(err.Error(), "exceeding the maximum length") {
		t.Fatalf("expected a list-item exceeds-maximum-length rejection, got: %v", err)
	}
}

// TestAdversarialRoundTripListAtMaximumItemCountBound proves a list field
// with exactly MaximumListItems items round-trips cleanly.
func TestAdversarialRoundTripListAtMaximumItemCountBound(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.Examples = Field{Items: syntheticListItems(MaximumListItems)}
	reparsed := roundTripDocument(t, doc)
	if len(reparsed.Examples.Items) != MaximumListItems {
		t.Fatalf("expected %d round-tripped items, got %d", MaximumListItems, len(reparsed.Examples.Items))
	}
	if !reflect.DeepEqual(reparsed.Examples.Items, doc.Examples.Items) {
		t.Fatal("maximum-count list items did not round-trip identically")
	}
}

// TestAdversarialRoundTripListExceedingMaximumItemCountBoundIsRejected
// proves a list field with one more than MaximumListItems items is rejected.
func TestAdversarialRoundTripListExceedingMaximumItemCountBoundIsRejected(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.Examples = Field{Items: syntheticListItems(MaximumListItems + 1)}
	err := roundTripDocumentRejection(t, doc)
	if err == nil || !strings.Contains(err.Error(), "exceeds the maximum list-item bound") {
		t.Fatalf("expected a list-item-count rejection, got: %v", err)
	}
}
