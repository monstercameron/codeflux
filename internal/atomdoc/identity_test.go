package atomdoc

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// TestAtomVersionIDDistinctFromAtomID proves M21-102: atom identity and
// atom-version identity are distinct Go types that cannot be silently
// interchanged, even though one is derived from the other.
func TestAtomVersionIDDistinctFromAtomID(t *testing.T) {
	atomID, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("new atom id: %v", err)
	}
	version, err := NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatalf("new atom-version id: %v", err)
	}
	if version.AtomID() != atomID {
		t.Fatalf("expected AtomVersionID to retain its owning atom identity")
	}
	if version.String() == atomID.String() {
		t.Fatal("expected the atom-version identity string to differ from the bare atom identity")
	}
	if _, err := NewAtomVersionID(domain.AtomID{}, 1); err == nil {
		t.Fatal("expected an error for a zero atom identity")
	}
	if _, err := NewAtomVersionID(atomID, 0); err == nil {
		t.Fatal("expected an error for a zero version number")
	}
}

// TestDeriveDocumentationRevisionIDIsContentAddressedAndDistinct proves
// M21-102: the documentation-revision identity is a third identity, distinct
// from both the atom and atom-version identity, and is deterministically
// derived from the bound content rather than randomly assigned.
func TestDeriveDocumentationRevisionIDIsContentAddressedAndDistinct(t *testing.T) {
	atomID, err := domain.NewAtomID()
	if err != nil {
		t.Fatalf("new atom id: %v", err)
	}
	version, err := NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatalf("new atom-version id: %v", err)
	}
	contractHash, err := ParseContractHash(strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("parse contract hash: %v", err)
	}
	normalizedHash := ComputeNormalizedDocumentationInputHash(SchemaVersion, validDocumentForTest())

	first := deriveDocumentationRevisionID(atomID, version, SchemaVersion, normalizedHash, contractHash)
	second := deriveDocumentationRevisionID(atomID, version, SchemaVersion, normalizedHash, contractHash)
	if first != second {
		t.Fatal("expected the same inputs to derive the same revision identity")
	}
	if first.String() == atomID.String() || first.String() == version.String() {
		t.Fatal("expected the revision identity to differ from the atom and atom-version identity strings")
	}

	otherContractHash, err := ParseContractHash(strings.Repeat("c", 64))
	if err != nil {
		t.Fatalf("parse contract hash: %v", err)
	}
	third := deriveDocumentationRevisionID(atomID, version, SchemaVersion, normalizedHash, otherContractHash)
	if first == third {
		t.Fatal("expected a different contract hash to derive a different revision identity")
	}
}

// TestParseDocumentationRevisionIDValidatesShape proves the parser rejects
// malformed revision identities rather than accepting arbitrary strings.
func TestParseDocumentationRevisionIDValidatesShape(t *testing.T) {
	if _, err := ParseDocumentationRevisionID("not-a-revision-id"); err == nil {
		t.Fatal("expected an error for a malformed revision identity")
	}
	valid := documentationRevisionIDPrefix + strings.Repeat("0", 64)
	parsed, err := ParseDocumentationRevisionID(valid)
	if err != nil {
		t.Fatalf("expected a well-formed revision identity to parse, got: %v", err)
	}
	if parsed.String() != valid {
		t.Fatalf("expected round-trip string equality, got %q", parsed.String())
	}
}

// TestDependencyBindingValidateRejectsIncompleteBindings proves a
// dependency binding cannot silently omit its name or version.
func TestDependencyBindingValidateRejectsIncompleteBindings(t *testing.T) {
	if err := (DependencyBinding{Name: "", Version: "v1"}).Validate(); err == nil {
		t.Fatal("expected an error for an empty dependency name")
	}
	if err := (DependencyBinding{Name: "inventory-ledger", Version: ""}).Validate(); err == nil {
		t.Fatal("expected an error for an empty dependency version")
	}
	if err := (DependencyBinding{Name: "inventory-ledger", Version: "v3"}).Validate(); err != nil {
		t.Fatalf("expected a complete binding to validate, got: %v", err)
	}
}
