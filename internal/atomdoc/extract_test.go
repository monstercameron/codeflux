package atomdoc

import (
	"strings"
	"testing"
)

// TestLocateAtomDeclarationCandidatesUsesSyntaxTree proves M21-108: the
// directive is found via go/ast traversal of func and type declarations, not
// by scanning raw source text with a regular expression.
func TestLocateAtomDeclarationCandidatesUsesSyntaxTree(t *testing.T) {
	candidate := mustLocateSingleCandidate(t, validFixtureSource)
	if candidate.Identifier != "ReserveWidgetInventoryUntilCheckoutExpires" {
		t.Fatalf("unexpected identifier %q", candidate.Identifier)
	}
	if candidate.DocGroup == nil {
		t.Fatal("expected a non-nil doc group attached to the declaration")
	}
}

// TestLocateAtomDeclarationCandidatesFindsTypeDeclarations proves the same
// AST-based location works for a named-type declaration, not only funcs.
func TestLocateAtomDeclarationCandidatesFindsTypeDeclarations(t *testing.T) {
	source := `package fixture

// WidgetHold is a reservation held against checkout inventory.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Represent one inventory hold created by a checkout reservation.
type WidgetHold struct{}
`
	candidate := mustLocateSingleCandidate(t, source)
	if candidate.Identifier != "WidgetHold" {
		t.Fatalf("unexpected identifier %q", candidate.Identifier)
	}
}

// TestLocateAtomDeclarationCandidatesRejectsUnattachedDirective proves
// M21-109: a directive present in a comment group that is not attached to a
// supported declaration is reported as an error rather than silently
// ignored or silently matched to the wrong declaration.
func TestLocateAtomDeclarationCandidatesRejectsUnattachedDirective(t *testing.T) {
	source := `package fixture

//codeflux:atom
// this directive floats above a non-declaration comment block, not a
// function or type declaration.

var widgetHoldCounter int
`
	fset, file := parseFixtureFile(t, source)
	_, err := LocateAtomDeclarationCandidates(fset, file)
	if err == nil {
		t.Fatal("expected an error for an unattached atom directive")
	}
	if !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("expected an unattached-directive error, got: %v", err)
	}
}

// TestParseAtomDocumentationCommentValidatesSchemaHeader proves M21-110: a
// missing or malformed schema-version header is rejected with a specific
// reason distinguishing "missing" from "malformed".
func TestParseAtomDocumentationCommentValidatesSchemaHeader(t *testing.T) {
	t.Run("missing header", func(t *testing.T) {
		source := `package fixture

// ReserveWidgetInventoryUntilCheckoutExpires reserves widget inventory.
//
//codeflux:atom
func ReserveWidgetInventoryUntilCheckoutExpires() {}
`
		candidate := mustLocateSingleCandidate(t, source)
		_, err := ParseAtomDocumentationComment(candidate)
		if err == nil || !strings.Contains(err.Error(), "missing schema-versioned") {
			t.Fatalf("expected a missing-header error, got: %v", err)
		}
	})

	t.Run("unsupported version", func(t *testing.T) {
		source := `package fixture

// ReserveWidgetInventoryUntilCheckoutExpires reserves widget inventory.
//
//codeflux:atom
// Codeflux atom documentation (schema v2):
//   Purpose:
//     Hold scarce widget inventory for one checkout session.
func ReserveWidgetInventoryUntilCheckoutExpires() {}
`
		candidate := mustLocateSingleCandidate(t, source)
		_, err := ParseAtomDocumentationComment(candidate)
		if err == nil || !strings.Contains(err.Error(), "unsupported documentation schema version") {
			t.Fatalf("expected an unsupported-schema-version error, got: %v", err)
		}
	})

	t.Run("malformed header text", func(t *testing.T) {
		source := `package fixture

// ReserveWidgetInventoryUntilCheckoutExpires reserves widget inventory.
//
//codeflux:atom
// Codeflux atom documentation schema 1:
//   Purpose:
//     Hold scarce widget inventory for one checkout session.
func ReserveWidgetInventoryUntilCheckoutExpires() {}
`
		candidate := mustLocateSingleCandidate(t, source)
		_, err := ParseAtomDocumentationComment(candidate)
		if err == nil || !strings.Contains(err.Error(), "malformed schema-version header") {
			t.Fatalf("expected a malformed-header error, got: %v", err)
		}
	})
}

// TestParseAtomDocumentationCommentPreservesListStructure proves M21-111:
// list fields (marked with leading "-") are split into distinct ordered
// items rather than flattened into one joined string, and multi-line
// continuation of one item is preserved as part of that same item.
func TestParseAtomDocumentationCommentPreservesListStructure(t *testing.T) {
	candidate := mustLocateSingleCandidate(t, validFixtureSource)
	parsed, err := ParseAtomDocumentationComment(candidate)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	doc, err := ValidateAtomDocumentationSchema(parsed.Fields)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(doc.Inputs.Items) != 2 {
		t.Fatalf("expected 2 Inputs items, got %d: %#v", len(doc.Inputs.Items), doc.Inputs.Items)
	}
	if !strings.HasPrefix(doc.Inputs.Items[0], "SessionID identifies the checkout session") {
		t.Fatalf("unexpected first Inputs item: %q", doc.Inputs.Items[0])
	}
	if !strings.Contains(doc.Inputs.Items[0], "non-expired session identity.") {
		t.Fatalf("expected multi-line continuation folded into the first item, got: %q", doc.Inputs.Items[0])
	}
	if !strings.HasPrefix(doc.Inputs.Items[1], "Count is the number of physical units requested") {
		t.Fatalf("unexpected second Inputs item: %q", doc.Inputs.Items[1])
	}
}

// TestNormalizeFieldCollapsesWhitespacePreservingContent proves M21-112 and
// M21-113: indentation and line-wrap whitespace are normalized away, while
// punctuation, domain terms, and a negative ("Do not use when") example
// survive normalization verbatim.
func TestNormalizeFieldCollapsesWhitespacePreservingContent(t *testing.T) {
	field := normalizeField(FieldKindProse, []string{
		"The caller wants a permanent stock decrement; use CommitWidgetSale for",
		"  that outcome instead, since a reservation alone never decrements stock.",
	})
	const want = "The caller wants a permanent stock decrement; use CommitWidgetSale for that outcome instead, since a reservation alone never decrements stock."
	if field.Text != want {
		t.Fatalf("normalized text mismatch:\n got:  %q\n want: %q", field.Text, want)
	}
}

// TestNormalizeFieldHandlesExplainedNone proves the "None: <reason>" absence
// form is recognized for both prose and list field kinds.
func TestNormalizeFieldHandlesExplainedNone(t *testing.T) {
	prose := normalizeField(FieldKindProse, []string{"None: this atom has no external effects to reconcile."})
	if !prose.None || prose.Reason != "this atom has no external effects to reconcile." {
		t.Fatalf("unexpected prose None field: %#v", prose)
	}
	list := normalizeField(FieldKindList, []string{"None: pure atom with no side effects to name."})
	if !list.None || list.Reason != "pure atom with no side effects to name." {
		t.Fatalf("unexpected list None field: %#v", list)
	}
}
