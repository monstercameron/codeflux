package storage

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestAUDIT005a_NoDeletionPolicyIsDeclaredWithoutAnExecutor covers AUDIT-005a.
//
// DefaultDeletionPolicy declared tombstone-then-purge for projects and threads
// and hard-delete-lineage for learned artifacts, and nothing in the repository
// consumed it. AUDIT-005 had already proved the schema disagreed with the purge
// half: no migration declares ON DELETE for the graph or vector relations, and
// with foreign keys enforced by the DSN, deleting a project or graph with
// descendants is refused atomically. So the type stated user-data deletion
// semantics that the database actively refused to perform, and read as settled
// while being unimplemented.
//
// AUDIT-005a offered two resolutions, implement or delete. Delete was taken: a
// real project and thread purge path is a feature carrying a migration, an
// authority model, and irreversible effects on user data, and that belongs to
// its own governing task rather than to a reconciliation of the completion
// ledger.
//
// This test guards the direction of the repair rather than the deletion itself.
// A compiler already enforces that the symbols are gone; what it cannot enforce
// is that they stay gone until an executor exists. Re-declaring a deletion
// policy is only honest in the same change that implements and tests the
// deletion path it describes, at which point this test should be deleted and
// replaced by one that exercises that path against a real SQLite database.
//
// The pattern is cmd/codeflux-dev/vector_branch_test.go, from AUDIT-026: make
// the deferral executable, and fail with a message that says what must happen
// instead of leaving a future reader to delete an assertion they do not
// understand.
func TestAUDIT005a_NoDeletionPolicyIsDeclaredWithoutAnExecutor(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Fatalf("repository root %q has no go.mod: %v", root, statErr)
	}

	// DeletionModeHardDeleteLineage is deliberately absent from this list. It
	// named the one mode that is genuinely executed, by DeleteMemoryArtifact,
	// which reaches that behaviour without consulting a policy struct. What must
	// not return unimplemented is the policy and the purge mode it declared.
	removedSymbols := []string{
		"DeletionPolicy",
		"DefaultDeletionPolicy",
		"DeletionModeTombstoneThenPurge",
		"RequireOrphanCheck",
	}

	var hits []string
	for _, directory := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(root, directory),
			func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				// Production source only. Tests are where the history of this
				// removal is written down -- this file's own doc comment, the
				// narrowed TestMaintenancePoliciesAreExplicit, and the AUDIT-005
				// lineage tests all name these symbols in prose deliberately, so
				// that a reader finding the gap can find out why it is there.
				// Policing comments would delete the explanation along with the
				// code, and the explanation is the part worth keeping.
				if strings.HasSuffix(path, "_test.go") {
					return nil
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					return readErr
				}
				body := string(content)
				for _, symbol := range removedSymbols {
					if !strings.Contains(body, symbol) {
						continue
					}
					relative, relErr := filepath.Rel(root, path)
					if relErr != nil {
						return relErr
					}
					hits = append(hits, filepath.ToSlash(relative)+": "+symbol)
				}
				return nil
			})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", directory, walkErr)
		}
	}

	sort.Strings(hits)
	if len(hits) > 0 {
		t.Fatalf("a deletion policy is declared again at %d site(s):\n  %s\n\n"+
			"AUDIT-005a removed these because they described user-data deletion "+
			"semantics that nothing performed and the schema refused. If a real "+
			"project and thread purge path now exists, delete this test and replace "+
			"it with one that exercises that path against a real SQLite database, "+
			"and update TODOS.md AUDIT-005a to record implementation rather than "+
			"removal. Do not re-declare the policy ahead of its executor",
			len(hits), strings.Join(hits, "\n  "))
	}
}
