package retrieval

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGateEvidenceG07_RichDocumentationTextIsNotYetConsultedByDiscovery is
// half of the M21-G07 gate-evidence test: "Rich atom comments improve
// candidate discovery without bypassing exact applicability, evidence, or
// assurance checks."
//
// The BYPASS half is already proven, at the decisive-scenario level, by
// internal/retrievalgate/decisive_test.go's
// TestM21_145_RichlyDocumentedAtomCannotSelfPromoteAssurance (a candidate
// with an extremely rich atomdoc.Document and one with a minimal comment,
// carrying IDENTICAL weak evidence, resolve to the identical achieved
// assurance level and the identical eligibility rejection) and
// TestM21_145_EvaluateAssuranceNeverSeesDocumentation (EvaluateAssurance's
// signature has no documentation-shaped parameter at all). This package's
// own G01 gate-evidence test additionally proves the same non-bypass
// property end to end through the real service and real storage.
//
// The DISCOVERY-IMPROVEMENT half is now wired: discoverByAtomDocumentation
// (atom_documentation_discovery.go) matches the task's structured
// affected-symbol and affected-package hints against the retrieval-bearing
// prose of every ADMITTED atom documentation revision, proven end to end by
// TestGateEvidenceG07_RichDocumentationImprovesDiscoveryWithoutChangingEligibility.
//
// This test guards the boundary that keeps the two halves apart. Production
// code in this package reads documentation only as PLAIN TEXT, through
// storage.AtomDocumentationDiscoveryText, which carries no assurance,
// maturity, applicability, or evidence field. It never imports
// internal/atomdoc, so it cannot read documentation SEMANTICS -- contract
// hashes, admission status reasoning, quality flags -- and therefore cannot
// let a well-written comment argue its own way past a gate. The test fails
// the moment a production file in this package imports internal/atomdoc,
// which is the moment that separation would need re-proving.
func TestGateEvidenceG07_DiscoveryReadsDocumentationTextButNeverItsSemantics(t *testing.T) {
	root := repositoryRootForTest(t)
	packageDir := filepath.Join(root, "internal", "retrieval")
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		found = true
		source, err := os.ReadFile(filepath.Join(packageDir, name))
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				continue
			}
			if value == "codeflux.dev/codeflux/internal/atomdoc" {
				t.Fatalf(
					"%s imports internal/atomdoc: production retrieval code must read documentation as plain "+
						"text through storage.AtomDocumentationDiscoveryText only, never as atomdoc semantics, "+
						"so a rich comment cannot reason its own way past an eligibility gate.",
					name,
				)
			}
		}
	}
	if !found {
		t.Fatal("found zero production (non-test) .go files in internal/retrieval; this check did not actually run over anything")
	}
}
