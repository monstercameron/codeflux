package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAtomDocumentationFixtures(t *testing.T) {
	tests := []struct {
		name      string
		wantError string
	}{
		{name: "complete"},
		{name: "missing-field", wantError: "missing required field"},
		{name: "empty-field", wantError: "is empty"},
		{name: "malformed-version", wantError: "malformed documentation schema header"},
		{name: "keyword-stuffed", wantError: "keyword-stuffed"},
		{name: "identifier-mismatched", wantError: "must begin with ValidateTaskBudgetBeforeModelRequest"},
		{name: "wrapped-opening"},
		{name: "unterminated-opening", wantError: "never reaches a sentence terminator"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join("testdata", "atomdocs", test.name+".go")
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			err = validateAtomSource(path, source)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("valid fixture error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("fixture error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestValidateAtomNameFixtures(t *testing.T) {
	tests := []struct {
		name       string
		canonical  string
		exceptions atomNameExceptions
		wantError  string
	}{
		{name: "descriptive", canonical: "ValidateTaskBudgetBeforeModelRequest"},
		{name: "empty", wantError: "empty"},
		{name: "ambiguous", canonical: "Process", wantError: "single generic"},
		{name: "filler suffix", canonical: "ValidateTaskBudgetHelper", wantError: "filler suffix"},
		{name: "version encoded", canonical: "ValidateTaskBudgetV2", wantError: "version encoding"},
		{name: "hash encoded", canonical: "ValidateTaskBudgetDeadbeef", wantError: "hash encoding"},
		{name: "unrecognized action", canonical: "AuditTaskBudget", wantError: "concrete action verb"},
		{
			name:      "reviewed action",
			canonical: "AuditTaskBudget",
			exceptions: atomNameExceptions{
				actionVerb: "Audit is the domain's reviewed read-only action.",
			},
		},
		{name: "unexplained abbreviation", canonical: "LoadXYZRepository", wantError: "unexplained abbreviation"},
		{
			name:      "reviewed abbreviation",
			canonical: "LoadXYZRepository",
			exceptions: atomNameExceptions{
				abbreviations: map[string]string{"XYZ": "Synthetic established fixture protocol."},
			},
		},
		{name: "misleading guarantee", canonical: "ValidateAlwaysCorrectPayment", wantError: "guarantee claim"},
		{
			name:      "reviewed guarantee",
			canonical: "ValidateAlwaysCorrectPayment",
			exceptions: atomNameExceptions{
				guaranteeClaim: "A separate proof obligation supports this synthetic fixture.",
			},
		},
		{name: "provider specific", canonical: "SendOpenAIChatCompletion", wantError: "provider-specific"},
		{
			name:      "reviewed provider binding",
			canonical: "SendOpenAIChatCompletion",
			exceptions: atomNameExceptions{
				providerSpecific: "This synthetic atom is the provider adapter binding.",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateAtomName(test.canonical, test.exceptions)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validateAtomName() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validateAtomName() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestDeriveAtomNameRecordIsDeterministicAndIdentityBound(t *testing.T) {
	first, err := deriveAtomNameRecord("atom-123", "LoadHTTPRepositoryState", atomNameExceptions{})
	if err != nil {
		t.Fatalf("derive first record: %v", err)
	}
	second, err := deriveAtomNameRecord("atom-123", "LoadHTTPRepositoryState", atomNameExceptions{})
	if err != nil {
		t.Fatalf("derive second record: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("derivation is not deterministic: %#v != %#v", first, second)
	}
	if first.AtomIdentity != "atom-123" ||
		first.CanonicalGoName != "LoadHTTPRepositoryState" ||
		first.DisplayName != "Load HTTP Repository State" ||
		first.NormalizedPhrase != "load http repository state" {
		t.Fatalf("unexpected name record: %#v", first)
	}
}

func TestClassifyAtomRenameFixtures(t *testing.T) {
	base, err := deriveAtomNameRecord("atom-123", "ValidateTaskBudgetBeforeModelRequest", atomNameExceptions{})
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := deriveAtomNameRecord("atom-123", "CheckTaskBudgetBeforeModelRequest", atomNameExceptions{})
	if err != nil {
		t.Fatal(err)
	}
	breaking, err := deriveAtomNameRecord("atom-456", "CheckTaskBudgetBeforeModelRequest", atomNameExceptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := classifyAtomRename(base, preserved, true); got != atomRenameSemanticPreserving {
		t.Fatalf("semantic-preserving rename = %q", got)
	}
	if got := classifyAtomRename(base, preserved, false); got != atomRenameSemanticBreaking {
		t.Fatalf("unreviewed rename = %q", got)
	}
	if got := classifyAtomRename(base, breaking, true); got != atomRenameSemanticBreaking {
		t.Fatalf("identity-changing rename = %q", got)
	}
}

func TestValidateAtomSourceRejectsOrphanMarker(t *testing.T) {
	source := []byte("package fixture\n\n//codeflux:atom\n\nfunc OrdinaryFunction() {}\n")
	err := validateAtomSource("fixture.go", source)
	if err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("orphan marker error = %v", err)
	}
}

func BenchmarkSplitGoIdentifier(b *testing.B) {
	for b.Loop() {
		splitGoIdentifier("ValidateTaskBudgetBeforeModelRequest")
	}
}

func BenchmarkRenderMigrationCatalog(b *testing.B) {
	descriptors := []migrationDescriptor{{
		Number: 0,
		Name:   "000000_bootstrap.sql",
		SHA256: strings.Repeat("a", 64),
	}}
	for b.Loop() {
		renderMigrationCatalog(descriptors)
	}
}
