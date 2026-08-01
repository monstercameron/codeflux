package storage

import (
	"path/filepath"
	"testing"

	"codeflux.dev/codeflux/internal/atomname"
	"codeflux.dev/codeflux/migrations"
)

// TestAtomDocumentationAndNamingMigrationAppliesForwardFromPreviousSchema
// mirrors TestTaskGraphStorageMigrationAppliesForwardFromPreviousSchema: it
// proves migration 000026 is a genuine forward step from 000025's committed
// schema (the new tables do not exist before it applies and do exist,
// exactly once, after it applies).
func TestAtomDocumentationAndNamingMigrationAppliesForwardFromPreviousSchema(t *testing.T) {
	database := openMigrationTestDatabase(t)
	sources, err := migrations.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) < 27 || sources[26].Descriptor.Name != "000026_atom_documentation_and_naming.sql" {
		t.Fatalf("migration sources = %#v", sources)
	}
	for _, source := range sources[:26] {
		if _, err := database.sql.ExecContext(t.Context(), source.SQL); err != nil {
			t.Fatalf("apply %s: %v", source.Descriptor.Name, err)
		}
	}
	newTables := []string{
		"atom_documentation_revisions", "atom_documentation_fields",
		"atom_documentation_embeddings", "atom_names", "atom_name_aliases",
	}
	for _, table := range newTables {
		var before int
		if err := database.sql.QueryRowContext(t.Context(), `
			SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if before != 0 {
			t.Fatalf("table %s existed before migration 000026", table)
		}
	}

	if _, err := database.sql.ExecContext(t.Context(), sources[26].SQL); err != nil {
		t.Fatal(err)
	}

	for _, table := range newTables {
		var count int
		if err := database.sql.QueryRowContext(t.Context(), `
			SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s count after migration = %d, want 1", table, count)
		}
	}
	for _, index := range []string{
		"atom_documentation_revisions_by_atom", "atom_documentation_revisions_by_comment_hash",
		"atom_documentation_revisions_by_contract_hash", "atom_documentation_revisions_by_validity",
		"atom_documentation_embeddings_by_revision_validity",
		"atom_names_one_valid_per_atom_version", "atom_names_by_active_scope_phrase",
		"atom_name_aliases_by_normalized_text",
	} {
		var count int
		if err := database.sql.QueryRowContext(t.Context(), `
			SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("index %s count after migration = %d, want 1", index, count)
		}
	}
}

// TestAtomDocumentationAndNamingBackupAndRestore proves the online backup
// API (already exercised generically in backup_test.go) round-trips real
// atom-documentation and atom-naming rows written through this lane's
// repository API: a snapshot taken before a later mutation restores the
// pre-mutation state exactly, including the immutable revision content and
// the mutable validation-status/valid overlays.
func TestAtomDocumentationAndNamingBackupAndRestore(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	database := repositories.database
	projectID := testProjectID(t, 4200)
	repositoryID := testRepositoryID(t, 4201)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	atomID := testAtomID(t, 4202)
	docRevision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, testAtomContractHash(t, '5'))
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: docRevision,
	}); err != nil {
		t.Fatal(err)
	}

	atomVersion := docRevision.AtomVersion
	scope := mustNamingScope(t, projectID, "reservations")
	canonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	nameRecord := mustAtomNameRecord(t, atomID, atomVersion, scope, canonical, 1, atomname.AtomNameRevisionID{}, fakeNow(t, repositories))
	if _, err := repositories.CreateAtomNameRevision(ctx, nameRecord); err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(t.TempDir(), "atom-doc-naming-snapshot.sqlite3")
	if err := database.Backup(ctx, backupPath); err != nil {
		t.Fatal(err)
	}

	// Mutate both overlays after the snapshot.
	if err := repositories.InvalidateAtomDocumentationRevision(ctx, InvalidateAtomDocumentationRevision{
		RevisionID: docRevision.RevisionID, Reason: "post-backup mutation",
	}); err != nil {
		t.Fatal(err)
	}
	renamed := mustCanonicalName(t, "ReserveWidgetInventoryUntilCartExpires")
	secondName := mustAtomNameRecord(t, atomID, atomVersion, scope, renamed, 2, nameRecord.RevisionID, fakeNow(t, repositories))
	if _, err := repositories.CreateAtomNameRevision(ctx, secondName); err != nil {
		t.Fatal(err)
	}

	if err := database.Restore(ctx, backupPath); err != nil {
		t.Fatal(err)
	}

	restoredDoc, err := repositories.GetAtomDocumentationRevision(ctx, docRevision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if restoredDoc.ValidationStatus != AtomDocumentationValidationStatusAdmitted {
		t.Fatalf("restored documentation revision status = %s, want admitted (pre-mutation)", restoredDoc.ValidationStatus)
	}
	restoredName, err := repositories.GetCurrentAtomName(ctx, atomVersion)
	if err != nil {
		t.Fatal(err)
	}
	if restoredName.RevisionID != nameRecord.RevisionID || restoredName.CanonicalName != canonical {
		t.Fatalf("restored current atom name = %#v, want the pre-rename revision", restoredName)
	}
}
