package atomdoc

import (
	"strings"
	"testing"
)

func mustParseFields(t *testing.T, source string) []RawField {
	t.Helper()
	candidate := mustLocateSingleCandidate(t, source)
	parsed, err := ParseAtomDocumentationComment(candidate)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return parsed.Fields
}

// TestValidateAtomDocumentationSchemaAcceptsValidFixture proves the full
// nineteen-field fixture parses and validates cleanly end to end.
func TestValidateAtomDocumentationSchemaAcceptsValidFixture(t *testing.T) {
	fields := mustParseFields(t, validFixtureSource)
	doc, err := ValidateAtomDocumentationSchema(fields)
	if err != nil {
		t.Fatalf("expected the valid fixture to validate, got: %v", err)
	}
	if doc.Purpose.Text == "" {
		t.Fatal("expected Purpose to be populated")
	}
	if len(doc.RetrievalConcepts.Text) == 0 {
		t.Fatal("expected RetrievalConcepts to be populated")
	}
}

// TestValidateAtomDocumentationSchemaRejectsMissingField proves M21-114:
// omitting a required field is rejected rather than silently accepted.
func TestValidateAtomDocumentationSchemaRejectsMissingField(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Verification:\n//     Covered by a real-storage integration test asserting exactly one hold\n//     row per session and item under concurrent reservation attempts.\n",
		"",
		1,
	)
	fields := mustParseFields(t, source)
	_, err := ValidateAtomDocumentationSchema(fields)
	if err == nil || !strings.Contains(err.Error(), `missing required field "Verification"`) {
		t.Fatalf("expected a missing-field error for Verification, got: %v", err)
	}
}

// TestValidateAtomDocumentationSchemaRejectsDuplicateField proves M21-114:
// declaring the same field twice is rejected.
func TestValidateAtomDocumentationSchemaRejectsDuplicateField(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Verification:\n",
		"//   Purpose:\n//     A duplicated purpose paragraph restating the same field twice.\n//   Verification:\n",
		1,
	)
	fields := mustParseFields(t, source)
	_, err := ValidateAtomDocumentationSchema(fields)
	if err == nil || !strings.Contains(err.Error(), `field "Purpose" is duplicated`) {
		t.Fatalf("expected a duplicated-field error for Purpose, got: %v", err)
	}
}

// TestValidateAtomDocumentationSchemaRejectsUnknownField proves M21-114: a
// field label outside the canonical schema-v1 set is rejected rather than
// silently merged into the previous field's content.
func TestValidateAtomDocumentationSchemaRejectsUnknownField(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Verification:\n",
		"//   Notes:\n//     An unsupported extra field a well-meaning author might add.\n//   Verification:\n",
		1,
	)
	fields := mustParseFields(t, source)
	_, err := ValidateAtomDocumentationSchema(fields)
	if err == nil || !strings.Contains(err.Error(), `unknown field "Notes"`) {
		t.Fatalf("expected an unknown-field error for Notes, got: %v", err)
	}
}

// TestValidateAtomDocumentationSchemaRejectsOutOfOrderField proves M21-114:
// fields must appear in exactly the canonical AGENTS.md order.
func TestValidateAtomDocumentationSchemaRejectsOutOfOrderField(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Semantics:\n//     Reserves the requested count atomically against available inventory\n//     and returns a hold identity; a hold that is never captured or released\n//     expires automatically after its configured lifetime.\n//   Inputs:\n",
		"//   Inputs:\n",
		1,
	)
	source = strings.Replace(source,
		"//   Outputs:\n",
		"//   Semantics:\n//     Reserves the requested count atomically against available inventory\n//     and returns a hold identity; a hold that is never captured or released\n//     expires automatically after its configured lifetime.\n//   Outputs:\n",
		1,
	)
	fields := mustParseFields(t, source)
	_, err := ValidateAtomDocumentationSchema(fields)
	if err == nil || !strings.Contains(err.Error(), "is out of order") {
		t.Fatalf("expected an out-of-order error, got: %v", err)
	}
}

// TestValidateAtomDocumentationSchemaRejectsEmptyField proves M21-115: an
// empty field body (no content, no explained None) is rejected.
func TestValidateAtomDocumentationSchemaRejectsEmptyField(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Verification:\n//     Covered by a real-storage integration test asserting exactly one hold\n//     row per session and item under concurrent reservation attempts.\n",
		"//   Verification:\n",
		1,
	)
	fields := mustParseFields(t, source)
	_, err := ValidateAtomDocumentationSchema(fields)
	if err == nil || !strings.Contains(err.Error(), `field "Verification" is empty`) {
		t.Fatalf("expected an empty-field error for Verification, got: %v", err)
	}
}

// TestValidateAtomDocumentationSchemaRejectsUnexplainedNone proves M21-115:
// AGENTS.md allows "None" only together with a substantive reason.
func TestValidateAtomDocumentationSchemaRejectsUnexplainedNone(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Verification:\n//     Covered by a real-storage integration test asserting exactly one hold\n//     row per session and item under concurrent reservation attempts.\n",
		"//   Verification:\n//     None: unclear.\n",
		1,
	)
	fields := mustParseFields(t, source)
	_, err := ValidateAtomDocumentationSchema(fields)
	if err == nil || !strings.Contains(err.Error(), "None without a substantive reason") {
		t.Fatalf("expected an unexplained-None error for Verification, got: %v", err)
	}
}

// TestValidateAtomDocumentationSchemaAcceptsExplainedNone proves an
// explained None is accepted for a field where the property genuinely does
// not apply.
func TestValidateAtomDocumentationSchemaAcceptsExplainedNone(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Reconciliation and compensation:\n//     An expired, uncaptured hold is reconciled by an automatic release job;\n//     no manual compensation step exists for this atom.\n",
		"//   Reconciliation and compensation:\n//     None: the hold expires automatically and no compensation is defined.\n",
		1,
	)
	fields := mustParseFields(t, source)
	doc, err := ValidateAtomDocumentationSchema(fields)
	if err != nil {
		t.Fatalf("expected the explained-None fixture to validate, got: %v", err)
	}
	if !doc.ReconciliationAndCompensation.None {
		t.Fatal("expected ReconciliationAndCompensation to be marked None")
	}
}

// TestValidateAtomDocumentationOpeningSentence proves M21-116.
func TestValidateAtomDocumentationOpeningSentence(t *testing.T) {
	if err := ValidateAtomDocumentationOpeningSentence(
		"ReserveWidgetInventoryUntilCheckoutExpires",
		"ReserveWidgetInventoryUntilCheckoutExpires reserves a count of widget inventory against a checkout session.",
	); err != nil {
		t.Fatalf("expected a valid opening sentence to pass, got: %v", err)
	}

	t.Run("wrong identifier prefix", func(t *testing.T) {
		err := ValidateAtomDocumentationOpeningSentence(
			"ReserveWidgetInventoryUntilCheckoutExpires",
			"Reserves a count of widget inventory against a checkout session.",
		)
		if err == nil || !strings.Contains(err.Error(), "must begin with") {
			t.Fatalf("expected a prefix error, got: %v", err)
		}
	})

	t.Run("no terminal punctuation", func(t *testing.T) {
		err := ValidateAtomDocumentationOpeningSentence(
			"ReserveWidgetInventoryUntilCheckoutExpires",
			"ReserveWidgetInventoryUntilCheckoutExpires reserves widget inventory",
		)
		if err == nil || !strings.Contains(err.Error(), "complete sentence") {
			t.Fatalf("expected a complete-sentence error, got: %v", err)
		}
	})
}

// TestFlagAtomDocumentationQualityIssuesDetectsKeywordStuffing proves
// M21-117: a field dominated by one repeated word is flagged, not silently
// embedded as clean, and not dropped from the returned document.
func TestFlagAtomDocumentationQualityIssuesDetectsKeywordStuffing(t *testing.T) {
	doc := Document{}
	*fieldByLabelForTest(&doc, "Retrieval concepts") = Field{
		Text: "widget widget widget widget widget widget widget inventory hold",
	}
	fillRemainingFieldsForTest(&doc)
	flags := FlagAtomDocumentationQualityIssues(doc)
	found := false
	for _, flag := range flags {
		if flag.Field == "Retrieval concepts" && flag.Kind == QualityFlagKeywordStuffing {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a keyword-stuffing flag on Retrieval concepts, got: %#v", flags)
	}
	// Flagging must not drop the field's content.
	if fieldByLabelForTest(&doc, "Retrieval concepts").Text == "" {
		t.Fatal("flagged field content must not be dropped")
	}
}

// TestFlagAtomDocumentationQualityIssuesDetectsRepeatedBoilerplate proves
// M21-117 for copy-pasted boilerplate spanning multiple distinct fields.
func TestFlagAtomDocumentationQualityIssuesDetectsRepeatedBoilerplate(t *testing.T) {
	doc := Document{}
	boilerplate := Field{Text: "This atom behaves exactly as described by its declared typed signature."}
	*fieldByLabelForTest(&doc, "Purpose") = boilerplate
	*fieldByLabelForTest(&doc, "Semantics") = boilerplate
	fillRemainingFieldsForTest(&doc)
	flags := FlagAtomDocumentationQualityIssues(doc)
	found := false
	for _, flag := range flags {
		if flag.Kind == QualityFlagRepeatedBoilerplate && strings.Contains(flag.Field, "Purpose") && strings.Contains(flag.Field, "Semantics") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a repeated-boilerplate flag spanning Purpose and Semantics, got: %#v", flags)
	}
}

// TestValidateAtomDocumentationSchemaRejectsControlBytesInFieldContent proves
// the DEFECT 1 fix: a C0 control byte (or DEL) embedded inside a list item,
// a prose field, or an explained-None reason is rejected outright rather
// than silently admitted. This specifically covers the exact bytes
// canonicalizeDocument (hash.go) uses as canonical-hash-input separators
// (0x1D, 0x1E, 0x1F): before this check existed, a list item carrying a raw
// 0x1E byte could splice two items' content into one item and forge a
// NormalizedInputHash identical to an honest document whose two phrases were
// two separate list items (see
// TestAdmitSourceAtomDocumentationRejectsEmbeddedSeparatorByteForgingNormalizedHashCollision
// in admit_test.go for the full-pipeline reproduction).
func TestValidateAtomDocumentationSchemaRejectsControlBytesInFieldContent(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    byte
	}{
		{"null-0x00", 0x00},
		{"unit-separator-0x1f", 0x1f},
		{"group-separator-0x1d", 0x1d},
		{"item-separator-0x1e", 0x1e},
		{"escape-0x1b", 0x1b},
		{"del-0x7f", 0x7f},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := mustParseFields(t, validFixtureSource)
			mutated := make([]RawField, len(fields))
			for i, field := range fields {
				bodyLines := append([]string(nil), field.BodyLines...)
				if field.Label == "Inputs" && len(bodyLines) > 0 {
					bodyLines[0] = bodyLines[0] + string(tc.b) + "extra"
				}
				mutated[i] = RawField{Label: field.Label, BodyLines: bodyLines}
			}
			_, err := ValidateAtomDocumentationSchema(mutated)
			if err == nil {
				t.Fatalf("expected control byte 0x%02x embedded in a list item to be rejected", tc.b)
			}
			if !strings.Contains(err.Error(), "disallowed control byte") {
				t.Fatalf("expected a disallowed-control-byte rejection message, got: %v", err)
			}
		})
	}
}

// TestFirstDisallowedControlByteAllowsTabAndNewlineButNotOtherC0 is a direct
// unit test of the DEFECT 1 fix's carve-out: a literal tab or newline byte
// is allowed (defensive only — normalizeField's collapseSpaces already turns
// every real tab/newline into an ordinary space before validateFieldContent
// ever runs, so this branch is not reachable through the real comment
// parser), while the canonical-hash separator bytes and other C0 control
// bytes are reported as disallowed.
func TestFirstDisallowedControlByteAllowsTabAndNewlineButNotOtherC0(t *testing.T) {
	if _, bad := firstDisallowedControlByte("alpha\tbravo\ncharlie"); bad {
		t.Fatal("expected a literal tab and newline to be allowed by firstDisallowedControlByte")
	}
	if b, bad := firstDisallowedControlByte("alpha" + canonicalItemSeparator + "bravo"); !bad || b != 0x1e {
		t.Fatalf("expected the canonical item-separator byte to be reported as disallowed, got byte=0x%02x disallowed=%v", b, bad)
	}
	if b, bad := firstDisallowedControlByte("alpha" + canonicalGroupSeparator + "bravo"); !bad || b != 0x1d {
		t.Fatalf("expected the canonical group-separator byte to be reported as disallowed, got byte=0x%02x disallowed=%v", b, bad)
	}
	if b, bad := firstDisallowedControlByte("alpha" + canonicalLabelSeparator + "bravo"); !bad || b != 0x1f {
		t.Fatalf("expected the canonical label-separator byte to be reported as disallowed, got byte=0x%02x disallowed=%v", b, bad)
	}
}

// TestValidateAtomDocumentationSchemaRejectsEmptyListField proves the
// list-kind branch of M21-115 (the existing
// TestValidateAtomDocumentationSchemaRejectsEmptyField only exercises the
// prose branch): a list field whose header is present but which carries zero
// "-" items is rejected as empty. Adversarial round-trip coverage requested
// alongside the DEFECT 1/2 review.
func TestValidateAtomDocumentationSchemaRejectsEmptyListField(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Preconditions:\n//     - The checkout session must exist and must not already hold a\n//       reservation for this catalog item.\n",
		"//   Preconditions:\n",
		1,
	)
	if source == validFixtureSource {
		t.Fatal("test fixture setup failed to strip the Preconditions list item")
	}
	fields := mustParseFields(t, source)
	_, err := ValidateAtomDocumentationSchema(fields)
	if err == nil || !strings.Contains(err.Error(), `field "Preconditions" is empty`) {
		t.Fatalf("expected an empty-list rejection for Preconditions, got: %v", err)
	}
}

// TestValidateAtomDocumentationSchemaAcceptsSingleItemList proves a
// single-item list field (the fixture's Preconditions field) is accepted,
// not just multi-item lists.
func TestValidateAtomDocumentationSchemaAcceptsSingleItemList(t *testing.T) {
	fields := mustParseFields(t, validFixtureSource)
	doc, err := ValidateAtomDocumentationSchema(fields)
	if err != nil {
		t.Fatalf("expected the fixture to validate, got: %v", err)
	}
	if len(doc.Preconditions.Items) != 1 {
		t.Fatalf("expected Preconditions to be a single-item list in the fixture, got %d items", len(doc.Preconditions.Items))
	}
}

// TestValidateAtomDocumentationSchemaAcceptsSubHeaderProseInsideFieldBody
// proves the DEFECT 2 fix: a colon-terminated, header-shaped line written as
// a natural-language sub-heading INSIDE a field's body — "Phase One:" and
// "Phase Two:", each alone on a line, indented at ordinary body depth rather
// than field-header depth — is parsed as ordinary Semantics content instead
// of being misparsed as two bogus new top-level fields (which previously
// rejected the whole document with `unknown field "Phase One"`).
func TestValidateAtomDocumentationSchemaAcceptsSubHeaderProseInsideFieldBody(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Semantics:\n//     Reserves the requested count atomically against available inventory\n//     and returns a hold identity; a hold that is never captured or released\n//     expires automatically after its configured lifetime.\n",
		"//   Semantics:\n//     Phase One:\n//     Reserves the requested count atomically against available inventory.\n//     Phase Two:\n//     Returns a hold identity; a hold that is never captured or released\n//     expires automatically after its configured lifetime.\n",
		1,
	)
	if source == validFixtureSource {
		t.Fatal("test fixture setup failed to insert sub-headers into Semantics")
	}
	fields := mustParseFields(t, source)
	doc, err := ValidateAtomDocumentationSchema(fields)
	if err != nil {
		t.Fatalf("expected a legitimate sub-headed Semantics field to validate cleanly, got: %v", err)
	}
	if !strings.Contains(doc.Semantics.Text, "Phase One:") || !strings.Contains(doc.Semantics.Text, "Phase Two:") {
		t.Fatalf("expected the sub-header prose to be preserved as Semantics content, got: %q", doc.Semantics.Text)
	}
}

// TestValidateAtomDocumentationSchemaRejectsColonTerminatedProseAtHeaderDepth
// proves the DEFECT 2 fix does not simply disable header-shaped-line
// detection: a colon-terminated line accidentally written at genuine
// field-header indentation (as a real author's stray line, not sub-header
// prose) is still treated as a field-boundary candidate and, if its label is
// not one of the nineteen canonical fields, still rejected as unknown
// (M21-114) exactly as before — proving the existing
// TestValidateAtomDocumentationSchemaRejectsUnknownField guarantee survives
// the structural-depth fix.
func TestValidateAtomDocumentationSchemaRejectsColonTerminatedProseAtHeaderDepth(t *testing.T) {
	source := strings.Replace(validFixtureSource,
		"//   Verification:\n",
		"//   Notes:\n//     An unsupported extra field a well-meaning author might add.\n//   Verification:\n",
		1,
	)
	fields := mustParseFields(t, source)
	_, err := ValidateAtomDocumentationSchema(fields)
	if err == nil || !strings.Contains(err.Error(), `unknown field "Notes"`) {
		t.Fatalf("expected an unknown-field error for a header-depth 'Notes:' line, got: %v", err)
	}
}

// fieldByLabelForTest and fillRemainingFieldsForTest are test-only helpers
// that build a synthetic Document without going through the full comment
// parser, so quality-flag behavior can be tested in isolation.
func fieldByLabelForTest(doc *Document, label string) *Field {
	for _, spec := range fieldSpecs {
		if spec.Label == label {
			return spec.get(doc)
		}
	}
	panic("unknown field label: " + label)
}

func fillRemainingFieldsForTest(doc *Document) {
	for _, spec := range fieldSpecs {
		field := spec.get(doc)
		if !field.IsEmpty() {
			continue
		}
		if spec.Kind == FieldKindList {
			*field = Field{Items: []string{"a distinct, substantive placeholder list item for this field"}}
			continue
		}
		*field = Field{Text: "a distinct, substantive placeholder paragraph for the " + spec.Label + " field"}
	}
}
