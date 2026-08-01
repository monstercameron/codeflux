package atomdoc

import (
	"strings"
	"testing"
)

// segmentsByRole indexes segments by role for assertions that only care
// about one role's contribution.
func segmentsByRole(segments []EmbeddingInputSegment, role EmbeddingInputFieldRole) []EmbeddingInputSegment {
	var found []EmbeddingInputSegment
	for _, segment := range segments {
		if segment.Role == role {
			found = append(found, segment)
		}
	}
	return found
}

// TestComposeDocumentationEmbeddingInputIncludesDefaultFullWeightFields is
// M21-128: Purpose, Use when, Do not use when, Semantics, input/output
// meaning, Effects, Failure semantics, and Retrieval concepts all enter
// embedding input verbatim.
func TestComposeDocumentationEmbeddingInputIncludesDefaultFullWeightFields(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	input, err := ComposeDocumentationEmbeddingInput(doc)
	if err != nil {
		t.Fatalf("ComposeDocumentationEmbeddingInput failed: %v", err)
	}
	if input.SchemaVersion != EmbeddingInputSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", input.SchemaVersion, EmbeddingInputSchemaVersion)
	}

	cases := []struct {
		role EmbeddingInputFieldRole
		want string
	}{
		{EmbeddingInputFieldRolePurpose, doc.Purpose.Text},
		{EmbeddingInputFieldRoleUseWhen, doc.UseWhen.Text},
		{EmbeddingInputFieldRoleDoNotUseWhen, doc.DoNotUseWhen.Text},
		{EmbeddingInputFieldRoleSemantics, doc.Semantics.Text},
		{EmbeddingInputFieldRoleEffects, strings.Join(doc.Effects.Items, " ")},
		{EmbeddingInputFieldRoleFailureSemantics, strings.Join(doc.FailureSemantics.Items, " ")},
		{EmbeddingInputFieldRoleRetrievalConcepts, doc.RetrievalConcepts.Text},
	}
	for _, tc := range cases {
		found := segmentsByRole(input.Segments, tc.role)
		if len(found) != 1 {
			t.Fatalf("role %q: expected exactly one segment, got %d", tc.role, len(found))
		}
		if found[0].Text != tc.want {
			t.Errorf("role %q text = %q, want %q", tc.role, found[0].Text, tc.want)
		}
		if found[0].Normalized {
			t.Errorf("role %q: expected full-weight (Normalized=false), got Normalized=true", tc.role)
		}
	}

	// Inputs and Outputs both contribute to the combined input/output
	// meaning role, each verbatim.
	ioSegments := segmentsByRole(input.Segments, EmbeddingInputFieldRoleInputOutputMeaning)
	if len(ioSegments) != 2 {
		t.Fatalf("expected exactly two input-output-meaning segments (Inputs, Outputs), got %d", len(ioSegments))
	}
	wantInputs := strings.Join(doc.Inputs.Items, " ")
	wantOutputs := strings.Join(doc.Outputs.Items, " ")
	if ioSegments[0].Text != wantInputs || ioSegments[1].Text != wantOutputs {
		t.Errorf("input-output-meaning segments = %q, %q; want %q, %q", ioSegments[0].Text, ioSegments[1].Text, wantInputs, wantOutputs)
	}
}

// TestComposeDocumentationEmbeddingInputExcludesProcessAndEvidenceFields
// proves Preconditions, Postconditions, Determinism, Examples, and
// Verification never produce a segment: AGENTS.md and docs/plan.md §7 do not
// name them as embedding-input material.
func TestComposeDocumentationEmbeddingInputExcludesProcessAndEvidenceFields(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	input, err := ComposeDocumentationEmbeddingInput(doc)
	if err != nil {
		t.Fatalf("ComposeDocumentationEmbeddingInput failed: %v", err)
	}
	excludedText := []string{
		strings.Join(doc.Preconditions.Items, " "),
		strings.Join(doc.Postconditions.Items, " "),
		doc.Determinism.Text,
		strings.Join(doc.Examples.Items, " "),
		doc.Verification.Text,
	}
	for _, segment := range input.Segments {
		for _, excluded := range excludedText {
			if excluded != "" && segment.Text == excluded {
				t.Errorf("segment %q:%q carries content from an excluded process/evidence field", segment.Role, segment.Text)
			}
		}
	}
}

// TestComposeDocumentationEmbeddingInputNormalizesContextFieldsConcisely is
// M21-129: retry, reconciliation, security, dependency, and limit fields
// enter only through concise semantic normalization, never as a verbatim
// dump. A deliberately long, multi-sentence field must come out shorter than
// it went in, proving this is a real reduction and not a pass-through.
func TestComposeDocumentationEmbeddingInputNormalizesContextFieldsConcisely(t *testing.T) {
	longField := strings.Repeat("This dependency binding sentence repeats itself with extra verbose filler words to exceed the concise bound. ", 6)
	doc := fullSyntheticDocumentForTest()
	doc.DependenciesAndBindings = Field{Text: longField}
	doc.SecurityAndPrivacy = Field{Text: longField}
	doc.IdempotencyAndRetry = Field{Text: longField}
	doc.ReconciliationAndCompensation = Field{Text: longField}
	doc.ComplexityAndLimits = Field{Text: longField}

	input, err := ComposeDocumentationEmbeddingInput(doc)
	if err != nil {
		t.Fatalf("ComposeDocumentationEmbeddingInput failed: %v", err)
	}

	normalizedRoles := []EmbeddingInputFieldRole{
		EmbeddingInputFieldRoleRetryContext,
		EmbeddingInputFieldRoleReconciliationContext,
		EmbeddingInputFieldRoleSecurityContext,
		EmbeddingInputFieldRoleDependencyContext,
		EmbeddingInputFieldRoleLimitContext,
	}
	for _, role := range normalizedRoles {
		found := segmentsByRole(input.Segments, role)
		if len(found) != 1 {
			t.Fatalf("role %q: expected exactly one segment, got %d", role, len(found))
		}
		segment := found[0]
		if !segment.Normalized {
			t.Errorf("role %q: expected Normalized=true", role)
		}
		if len(segment.Text) >= len(longField) {
			t.Errorf("role %q: normalized text (%d bytes) is not shorter than the source field (%d bytes); looks like a verbatim dump", role, len(segment.Text), len(longField))
		}
		if len(segment.Text) > MaximumNormalizedContextFieldBytes {
			t.Errorf("role %q: normalized text exceeds MaximumNormalizedContextFieldBytes (%d > %d)", role, len(segment.Text), MaximumNormalizedContextFieldBytes)
		}
	}
}

// TestComposeDocumentationEmbeddingInputConciseNormalizationIsRuneSafe
// proves normalizeContextField's truncation never splits a multi-byte UTF-8
// rune even when the cap lands mid-sentence with no ASCII sentence
// terminator nearby.
func TestComposeDocumentationEmbeddingInputConciseNormalizationIsRuneSafe(t *testing.T) {
	// No '.', '!', or '?' present, so the whole (long) collapsed string is
	// the "sentence" candidate and truncation must engage.
	longUnicode := strings.Repeat("café résumé naïve  ", 30)
	doc := fullSyntheticDocumentForTest()
	doc.SecurityAndPrivacy = Field{Text: longUnicode}

	input, err := ComposeDocumentationEmbeddingInput(doc)
	if err != nil {
		t.Fatalf("ComposeDocumentationEmbeddingInput failed: %v", err)
	}
	found := segmentsByRole(input.Segments, EmbeddingInputFieldRoleSecurityContext)
	if len(found) != 1 {
		t.Fatalf("expected exactly one security-context segment, got %d", len(found))
	}
	if !isValidUTF8(found[0].Text) {
		t.Fatalf("normalized text is not valid UTF-8, truncation split a rune: %q", found[0].Text)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

// TestComposeDocumentationEmbeddingInputPreservesNegativeSelectionExamples
// is M21-131: a populated "Do not use when" field is never dropped and
// always produces its own full-weight segment, so semantically close but
// invalid atoms stay distinguishable at retrieval time.
func TestComposeDocumentationEmbeddingInputPreservesNegativeSelectionExamples(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	doc.DoNotUseWhen = Field{Text: "Do not use this atom for a permanent decrement; a reservation alone never commits the sale, use CommitWidgetSale for that outcome instead."}

	input, err := ComposeDocumentationEmbeddingInput(doc)
	if err != nil {
		t.Fatalf("ComposeDocumentationEmbeddingInput failed: %v", err)
	}
	found := segmentsByRole(input.Segments, EmbeddingInputFieldRoleDoNotUseWhen)
	if len(found) != 1 {
		t.Fatalf("expected exactly one do-not-use-when segment, got %d", len(found))
	}
	if found[0].Text != doc.DoNotUseWhen.Text {
		t.Fatalf("do-not-use-when segment text = %q, want verbatim %q", found[0].Text, doc.DoNotUseWhen.Text)
	}
}

// TestComposeDocumentationEmbeddingInputOmitsExplainedNoneFields proves a
// field recorded as an explained "None" absence contributes no segment: it
// has no substantive domain content to embed.
func TestComposeDocumentationEmbeddingInputOmitsExplainedNoneFields(t *testing.T) {
	doc := fullSyntheticDocumentForTest()
	// ReconciliationAndCompensation is already None in the fixture; assert
	// no segment carries its role.
	input, err := ComposeDocumentationEmbeddingInput(doc)
	if err != nil {
		t.Fatalf("ComposeDocumentationEmbeddingInput failed: %v", err)
	}
	if found := segmentsByRole(input.Segments, EmbeddingInputFieldRoleReconciliationContext); len(found) != 0 {
		t.Fatalf("expected no reconciliation-context segment for an explained-None field, got %#v", found)
	}
}

// TestComposeDocumentationEmbeddingInputRejectsEmptyDocument proves a
// zero-value (unadmitted) Document is rejected rather than silently
// producing an empty or partial embedding input.
func TestComposeDocumentationEmbeddingInputRejectsEmptyDocument(t *testing.T) {
	if _, err := ComposeDocumentationEmbeddingInput(Document{}); err == nil {
		t.Fatal("expected composing embedding input from a zero-value Document to be rejected")
	}
}

// TestComposeDocumentationEmbeddingInputExcludesUnstableSourceMetadata is
// M21-130: source paths, line numbers, timestamps, evidence run IDs,
// hashes, and repeated field labels must never leak into embedding input.
// This test proves exclusion structurally, not by convention: it admits a
// real DocumentationRevision carrying distinct, real metadata values in
// every excluded category (revision identity, both hashes, source
// repository revision, creation timestamp), composes embedding input from
// ONLY revision.Document (the one value ComposeDocumentationEmbeddingInput
// can ever see), and fails if any of that metadata — or any of the schema's
// nineteen field-label strings — appears anywhere in the composed text.
func TestComposeDocumentationEmbeddingInputExcludesUnstableSourceMetadata(t *testing.T) {
	input := mustAdmissionInput(t, validFixtureSource, nil)
	result, err := AdmitSourceAtomDocumentation(t.Context(), input)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if result.Status != AdmissionStatusAdmitted {
		t.Fatalf("expected admission, got status=%s reason=%s", result.Status, result.RejectionReason)
	}
	revision := result.Revision

	embeddingInput, err := ComposeDocumentationEmbeddingInput(revision.Document)
	if err != nil {
		t.Fatalf("ComposeDocumentationEmbeddingInput failed: %v", err)
	}

	var allText strings.Builder
	for _, segment := range embeddingInput.Segments {
		allText.WriteString(segment.Text)
		allText.WriteString("\n")
	}
	composed := allText.String()

	excludedValues := map[string]string{
		"documentation-revision identity": revision.RevisionID.String(),
		"source-comment hash":             revision.SourceCommentHash.String(),
		"normalized-input hash":           revision.NormalizedInputHash.String(),
		"contract hash":                   revision.ContractHash.String(),
		"source repository revision":      revision.SourceRepositoryRevision,
		"atom identity":                   revision.AtomID.String(),
		"atom-version identity":           revision.AtomVersion.String(),
	}
	for label, value := range excludedValues {
		if value == "" {
			t.Fatalf("test fixture setup produced an empty %s, cannot prove exclusion", label)
		}
		if strings.Contains(composed, value) {
			t.Errorf("composed embedding input leaked %s (%q)", label, value)
		}
	}
	if strings.Contains(composed, revision.CreatedAt.Format("2006-01-02")) {
		t.Errorf("composed embedding input leaked the creation timestamp")
	}

	for _, label := range CanonicalFieldLabels() {
		if strings.Contains(composed, label+":") {
			t.Errorf("composed embedding input leaked a repeated field label %q", label+":")
		}
	}
}
