package coordinator

import (
	"context"
	"strings"
	"testing"
)

// undocumentedAtomSource is an atom worth registering that carries no schema.
//
// Pure, takes an argument, returns a value: it passes worthAdmitting, which is
// the population the registry is for. What it does not have is nineteen fields
// of documentation, which is the state every atom every run of this session has
// produced arrived in.
const undocumentedAtomSource = `package reserve

// AvailableBalance returns what is left of a balance after holds are taken off.
func AvailableBalance(balance int, holds []int) (int, error) {
	if balance < 0 {
		return 0, errNegativeBalance
	}
	available := balance
	for _, hold := range holds {
		available -= hold
	}
	return available, nil
}

var errNegativeBalance = errorString("balance must not be negative")

type errorString string

func (message errorString) Error() string { return string(message) }
`

// TestADerivedSchemaReachesTheRegistry is the acceptance criterion for the
// whole compounding-effort thesis: a produced atom, documented from measured
// facts rather than by asking a model, ends up as a row a later run can find.
//
// Every run of this session recorded "0 of N produced atom(s) carried schema-v1
// //codeflux:atom documentation", atom_names held zero rows, and every recall
// searched an empty project. The documentation was the missing link, and asking
// the model for it cost attempts and competed with the delivery gates.
func TestADerivedSchemaReachesTheRegistry(t *testing.T) {
	execution, scope := recallFixture(t)
	execution.redactor = registrationTestRedactionPipeline(t)
	scope.revision = "abc123def456"
	worktree := recallWorktree(t, undocumentedAtomSource)

	produced, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("the produced source could not be parsed: %v", err)
	}
	// Nothing carries documentation yet, which is the state this exists for.
	if missing := atomsWithoutRegistrableDocumentation(worktree, produced); len(missing) == 0 {
		t.Fatal("the fixture already carries documentation, so this proves nothing")
	}

	documented, err := deriveAtomDocumentation(worktree, produced)
	if err != nil {
		t.Fatalf("deriving the schema failed: %v", err)
	}
	if documented == 0 {
		t.Fatal("no atom was documented, so nothing can be registered")
	}

	// The file must still be Go. Enrichment that breaks the program is worse
	// than no enrichment: a registry row is not worth a working program.
	reparsed, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("the documented source no longer parses: %v", err)
	}
	if len(reparsed) != len(produced) {
		t.Errorf("documenting changed the function count from %d to %d",
			len(produced), len(reparsed))
	}
	if remaining := atomsWithoutRegistrableDocumentation(worktree, reparsed); len(remaining) > 0 {
		t.Errorf("these atoms are still undocumented after deriving: %v", remaining)
	}

	outcome, registration := execution.recallKnownAtoms(
		context.Background(), scope, worktree)
	if !outcome.Held {
		t.Fatalf("recall did not hold: %s", outcome.Detail)
	}
	if registered, _ := outcome.Evidence["atoms_registered"].(int); registered == 0 {
		t.Fatalf("nothing was registered from a documented atom (detail: %s)",
			registration.atom.Detail)
	}
	// The stage must say so too. Recording it as skipped is how a registry that
	// never filled went unnoticed for a whole session.
	if !registration.atom.Held {
		t.Errorf("the atom-registration stage did not hold: %s",
			registration.atom.Detail)
	}

	revisions, err := execution.repositories.ListAtomDocumentationRevisionsByProject(
		context.Background(), scope.projectID, 10)
	if err != nil {
		t.Fatalf("the registry could not be read: %v", err)
	}
	if len(revisions) == 0 {
		t.Fatal("the registry holds no documentation revision, so a later run " +
			"searching this project would find nothing")
	}
}

// TestADerivedSchemaCarriesTheFactsTheRunMeasured checks the fields are answers
// rather than headings.
//
// A schema whose every field reads "None" is a row that satisfies registration
// and helps nobody: the point of the retrieval-concepts field in particular is
// that a later task searches it, and a later task does not search for the name
// whoever wrote it happened to choose.
func TestADerivedSchemaCarriesTheFactsTheRunMeasured(t *testing.T) {
	atom := producedFunction{
		Name:         "AvailableBalance",
		Parameters:   []string{"int", "[]int"},
		Results:      []string{"int", "error"},
		ReturnsError: true,
		Pure:         true,
		LoopDepth:    1,
		Branches:     1,
	}
	comment := atomSchemaComment(atom)
	for _, wanted := range []string{
		"//codeflux:atom",
		"Codeflux atom documentation (schema v1):",
		"[]int",                           // the input types, from the signature
		"deterministic",                   // from purity
		"returned as an error",            // from ReturnsError
		"linear in the size of its input", // from LoopDepth
	} {
		if !strings.Contains(comment, wanted) {
			t.Errorf("the derived schema never says %q:\n%s", wanted, comment)
		}
	}
	// Nineteen fields, all of them present. A schema missing one is refused by
	// the parser, and being refused for a reason nobody can see is exactly the
	// failure that kept the registry empty.
	fields := 0
	for _, line := range strings.Split(comment, "\n") {
		trimmed := strings.TrimPrefix(line, "//   ")
		if trimmed != line && trimmed != "" &&
			trimmed[0] >= 'A' && trimmed[0] <= 'Z' {
			fields++
		}
	}
	if fields != 19 {
		t.Errorf("the derived schema carries %d field(s), wanted 19", fields)
	}
}
