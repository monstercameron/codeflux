package coordinator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"codeflux.dev/codeflux/internal/workspace"
)

// collectedFixtureSource is one small module carrying a documented atom, a
// plain exported function that calls it, and an unexported one.
//
// It is a real module rather than a parsed string because the mapper runs the
// Go toolchain: a fixture that never compiles would test a repository nobody
// could have.
const collectedFixtureSource = `package reserve

import "errors"

// ReserveAccountFundsUntilAuthorizationExpires holds an amount without
// capturing it.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Hold funds against an account so a later capture cannot overdraw it.
//   Use when:
//     A payment is authorized before its final amount is known.
//   Do not use when:
//     The amount is final; capture directly instead.
//   Semantics:
//     The reservation expires without capture and releases the held amount.
//   Inputs:
//     - amount: exact minor units to hold, never a rounded float.
//   Outputs:
//     - the reservation identity a later capture must present.
//   Preconditions:
//     - the account exists and is not frozen.
//   Postconditions:
//     - the held amount is unavailable to other reservations.
//   Effects:
//     - Mutates the account's available balance.
//   Failure semantics:
//     - Returns an error when the account cannot cover the amount.
//   Determinism:
//     Deterministic given the account state it reads.
//   Idempotency and retry:
//     Keyed by the caller's reservation identity; safe to retry.
//   Reconciliation and compensation:
//     An expired reservation is released by the expiry sweep.
//   Security and privacy:
//     None: no credential or personal data crosses this boundary.
//   Dependencies and bindings:
//     None: pure account arithmetic.
//   Complexity and limits:
//     Constant time; one account row.
//   Examples:
//     - reserve 500 minor units against an account holding 900.
//   Verification:
//     Covered by the reservation arithmetic tests.
//   Retrieval concepts:
//     reservation, authorization hold, available balance
func ReserveAccountFundsUntilAuthorizationExpires(amount int64) (string, error) {
	if amount <= 0 {
		return "", errors.New("amount must be positive")
	}
	return "reservation", nil
}

// CaptureReservation settles a held amount.
func CaptureReservation(amount int64) error {
	_, err := ReserveAccountFundsUntilAuthorizationExpires(amount)
	return err
}

func unexportedHelper() bool { return true }
`

// initializeCollectedRepository creates the module the collection reads.
func initializeCollectedRepository(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "reserve"), 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(arguments ...string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", arguments...)
		command.Dir = path
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	run("init", "--initial-branch=main")
	files := map[string]string{
		"go.mod":           "module codeflux.test/collection\n\ngo 1.26.0\n",
		"reserve/funds.go": collectedFixtureSource,
		"main.go":          "package main\n\nfunc main() {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(
			filepath.Join(path, filepath.FromSlash(name)), []byte(content), 0o600,
		); err != nil {
			t.Fatal(err)
		}
		run("add", name)
	}
	run("-c", "user.name=Codeflux Test", "-c", "user.email=codeflux@example.invalid",
		"commit", "-m", "base")
}

// newCollectedFixture builds the application over a real repository row.
func newCollectedFixture(t *testing.T) (*codeCollectionApplication, domain.RepositoryID) {
	t.Helper()
	root := t.TempDir()
	repositoryPath := filepath.Join(root, "collection")
	initializeCollectedRepository(t, repositoryPath)

	database, err := storage.Open(context.Background(), storage.OpenOptions{
		Path: filepath.Join(root, "codeflux.sqlite3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	if _, err := database.Migrate(context.Background(), storage.MigrationOptions{
		ApplicationVersion: "code-collection-test",
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := domain.NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateProject(context.Background(), storage.CreateProject{
		ID: projectID, Name: "collection",
	}); err != nil {
		t.Fatal(err)
	}
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(context.Background(), storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: repositoryPath, GitIdentity: "collection-fixture",
	}); err != nil {
		t.Fatal(err)
	}
	worktrees, err := gitwork.NewService(
		filepath.Join(root, "worktrees"), repositories, gitwork.ExecRunner{}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	application, err := newCodeCollectionApplication(repositories, worktrees, workspace.ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	return application, repositoryID
}

// TestTheCodeCollectionReadsARealRepository is the directory's whole claim:
// that what it lists is what the repository actually contains at a named
// revision, rather than anything this product invented.
func TestTheCodeCollectionReadsARealRepository(t *testing.T) {
	application, repositoryID := newCollectedFixture(t)

	packages, err := application.ListCodePackages(t.Context(), transport.CodeCollectionQuery{
		RepositoryID: repositoryID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if packages.Revision.Revision == "" {
		t.Fatal("a collection that does not name its revision describes nothing in particular")
	}
	found := transport.CodePackageRecord{}
	for _, record := range packages.Packages {
		if strings.HasSuffix(record.ImportPath, "/reserve") {
			found = record
		}
	}
	if found.ImportPath == "" {
		t.Fatalf("the reserve package is missing: %+v", packages.Packages)
	}
	if found.Name != "reserve" || found.FileCount != 1 {
		t.Fatalf("package record lost a field: %+v", found)
	}
	if found.SymbolCount < 3 {
		t.Fatalf("want the package's three declarations, got %d", found.SymbolCount)
	}
	// One of the three carries the atom directive with documentation that
	// parses, and the count says so rather than counting every declaration.
	if found.AtomCount != 1 {
		t.Fatalf("atom count = %d, want 1", found.AtomCount)
	}
	if packages.TotalAtoms != 1 {
		t.Fatalf("collection atom total = %d, want 1", packages.TotalAtoms)
	}

	symbols, err := application.ListCodeSymbols(t.Context(), transport.CodeSymbolQuery{
		RepositoryID: repositoryID, ImportPath: found.ImportPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]transport.CodeSymbolRecord{}
	for _, symbol := range symbols.Symbols {
		byName[symbol.Name] = symbol
	}
	atom, present := byName["ReserveAccountFundsUntilAuthorizationExpires"]
	if !present || !atom.Atom || !atom.Exported || atom.Kind == "" {
		t.Fatalf("the atom declaration lost a field: %+v", atom)
	}
	if plain := byName["CaptureReservation"]; plain.Atom {
		t.Fatal("a declaration with no directive must not be reported as an atom")
	}
	if _, listed := byName["unexportedHelper"]; !listed {
		t.Fatal("an unexported declaration is still part of the collection")
	}

	// The atom directory is a lens on this collection rather than a separate
	// surface, so the same listing narrows to admitted atoms.
	atomsOnly, err := application.ListCodeSymbols(t.Context(), transport.CodeSymbolQuery{
		RepositoryID: repositoryID, AtomsOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(atomsOnly.Symbols) != 1 ||
		atomsOnly.Symbols[0].Name != "ReserveAccountFundsUntilAuthorizationExpires" {
		t.Fatalf("the atom lens lists %+v", atomsOnly.Symbols)
	}

	exportedOnly, err := application.ListCodeSymbols(t.Context(), transport.CodeSymbolQuery{
		RepositoryID: repositoryID, ImportPath: found.ImportPath, ExportedOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range exportedOnly.Symbols {
		if !symbol.Exported {
			t.Fatalf("an unexported declaration survived the filter: %+v", symbol)
		}
	}
}

// TestInspectingADeclarationShowsWhatTheSourceSays proves the detail comes
// from the file rather than from anything this product stored about it.
func TestInspectingADeclarationShowsWhatTheSourceSays(t *testing.T) {
	application, repositoryID := newCollectedFixture(t)
	listed, err := application.ListCodeSymbols(t.Context(), transport.CodeSymbolQuery{
		RepositoryID: repositoryID, AtomsOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Symbols) != 1 {
		t.Fatalf("want one atom, got %d", len(listed.Symbols))
	}
	detail, err := application.InspectCodeSymbol(t.Context(), transport.CodeSymbolInspection{
		RepositoryID: repositoryID, Key: listed.Symbols[0].Key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(detail.Signature, "func ReserveAccountFundsUntilAuthorizationExpires(") {
		t.Fatalf("signature = %q", detail.Signature)
	}
	// The body is not returned. This is a directory of what the collection
	// offers, not an editor.
	if strings.Contains(detail.Signature, "errors.New") {
		t.Fatalf("the declaration body was returned: %q", detail.Signature)
	}
	if detail.AtomSchemaVersion != 1 {
		t.Fatalf("atom schema version = %d", detail.AtomSchemaVersion)
	}
	if !strings.Contains(detail.AtomOpeningSentence, "holds an amount") {
		t.Fatalf("opening sentence = %q", detail.AtomOpeningSentence)
	}
	labels := map[string]transport.CodeAtomField{}
	for _, field := range detail.AtomFields {
		labels[field.Label] = field
	}
	purpose, present := labels["Purpose"]
	if !present || !strings.Contains(purpose.Text, "Hold funds") {
		t.Fatalf("purpose field = %+v", purpose)
	}
	inputs := labels["Inputs"]
	if len(inputs.Items) != 1 || !strings.Contains(inputs.Items[0], "exact minor units") {
		t.Fatalf("inputs field = %+v", inputs)
	}
	if len(labels) != 19 {
		t.Fatalf("want the nineteen documented fields, got %d", len(labels))
	}
	// The declaration is called from one place in the fixture, and the caller
	// is named rather than counted.
	callerNames := []string{}
	for _, caller := range detail.Callers {
		callerNames = append(callerNames, caller.Name)
	}
	if len(callerNames) == 0 {
		t.Fatal("a declaration called from the same package must report its caller")
	}

	// A key that names nothing is an absence, not a failure.
	if _, err := application.InspectCodeSymbol(t.Context(), transport.CodeSymbolInspection{
		RepositoryID: repositoryID, Key: "nothing/here.go#.Missing",
	}); err != transport.ErrCodeSymbolNotFound {
		t.Fatalf("unknown key = %v, want ErrCodeSymbolNotFound", err)
	}
}
