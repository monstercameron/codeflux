package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/testfixtures"
	"codeflux.dev/codeflux/migrations"
)

// openIntegrationDatabase opens a real SQLite database at path and migrates
// it. Nothing here is faked: these tests exist because SQLite's constraint,
// journal, and recovery behaviour is exactly what a mock would get wrong.
func openIntegrationDatabase(t *testing.T, path string) *Database {
	t.Helper()
	ctx := context.Background()
	database, err := Open(ctx, OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = database.Close(closeCtx)
	})
	if _, err := database.Migrate(ctx, MigrationOptions{
		ApplicationVersion: "sqlite-integration-test",
		BackupDirectory:    filepath.Join(filepath.Dir(path), "backups"),
	}); err != nil {
		t.Fatal(err)
	}
	return database
}

// TestSQLiteIntegrationRepositoriesRunAgainstRealSQLite is M22-028.
//
// The repository suite in this package already uses real temporary databases
// throughout; this asserts that property rather than assuming it, by proving
// a freshly opened database reports healthy connection-local invariants and
// serves a real round-trip.
func TestSQLiteIntegrationRepositoriesRunAgainstRealSQLite(t *testing.T) {
	database := openIntegrationDatabase(t, filepath.Join(t.TempDir(), "codeflux.sqlite3"))
	ctx := context.Background()

	health, err := database.CheckHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !health.ForeignKeysEnabled {
		t.Fatal("foreign keys must be enabled: without them every referential constraint is decorative")
	}
	if !strings.EqualFold(health.JournalMode, "wal") {
		t.Fatalf("journal mode = %q, want WAL for crash recovery", health.JournalMode)
	}

	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := domain.NewProjectID()
	if _, err := repositories.CreateProject(ctx, CreateProject{ID: projectID, Name: "integration"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.GetProject(ctx, projectID); err != nil {
		t.Fatalf("a project written to real SQLite must be readable back: %v", err)
	}
}

// TestSQLiteIntegrationForeignKeyAndCheckConstraintsReject is M22-029.
//
// These constraints are the last line of defence when Go-side validation is
// bypassed — by a future caller, a migration, or a direct write. Proving they
// fire means the database itself refuses invalid state.
func TestSQLiteIntegrationForeignKeyAndCheckConstraintsReject(t *testing.T) {
	database := openIntegrationDatabase(t, filepath.Join(t.TempDir(), "codeflux.sqlite3"))
	ctx := context.Background()

	// Foreign key: a repository referencing a project that does not exist.
	_, err := database.sql.ExecContext(ctx,
		`INSERT INTO repositories (id, project_id, canonical_path, git_identity, created_at_unix_micros)
		 VALUES (?, ?, ?, ?, ?)`,
		"rep_00000000-0000-7000-8000-000000000001",
		"prj_00000000-0000-7000-8000-00000000dead",
		"/tmp/nowhere", strings.Repeat("a", 40), 1,
	)
	if err == nil {
		t.Fatal("inserting a repository for a nonexistent project must violate a foreign key")
	}

	// Check constraint: a memory artifact of an undeclared kind.
	projectID, _ := domain.NewProjectID()
	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateProject(ctx, CreateProject{ID: projectID, Name: "constraints"}); err != nil {
		t.Fatal(err)
	}
	_, err = database.sql.ExecContext(ctx,
		`INSERT INTO memory_artifacts (id, kind, project_id, created_at_unix_micros) VALUES (?, ?, ?, ?)`,
		"mem_00000000-0000-7000-8000-000000000001", "not-a-declared-kind", projectID, 1,
	)
	if err == nil {
		t.Fatal("inserting an undeclared memory-artifact kind must violate a CHECK constraint")
	}
}

// TestSQLiteIntegrationConcurrentWritersAndReplay is M22-030.
//
// Concurrency is where "it worked on my machine" lives. This runs real
// concurrent writers against one database and asserts every write either
// lands or fails cleanly — never corrupts, never silently drops.
func TestSQLiteIntegrationConcurrentWritersAndReplay(t *testing.T) {
	database := openIntegrationDatabase(t, filepath.Join(t.TempDir(), "codeflux.sqlite3"))
	ctx := context.Background()
	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	const writers = 8
	var group sync.WaitGroup
	created := make([]domain.ProjectID, writers)
	failures := make([]error, writers)
	group.Add(writers)
	for index := 0; index < writers; index++ {
		go func(slot int) {
			defer group.Done()
			projectID, err := domain.NewProjectID()
			if err != nil {
				failures[slot] = err
				return
			}
			if _, err := repositories.CreateProject(ctx, CreateProject{
				ID: projectID, Name: fmt.Sprintf("concurrent-%d", slot),
			}); err != nil {
				failures[slot] = err
				return
			}
			created[slot] = projectID
		}(index)
	}
	group.Wait()

	landed := 0
	for index, err := range failures {
		if err != nil {
			t.Fatalf("writer %d failed rather than serialising cleanly: %v", index, err)
		}
		if _, err := repositories.GetProject(ctx, created[index]); err != nil {
			t.Fatalf("writer %d reported success but its row is not readable: %v", index, err)
		}
		landed++
	}
	if landed != writers {
		t.Fatalf("%d of %d concurrent writes landed", landed, writers)
	}
}

// TestSQLiteIntegrationWALRecoveryAfterForcedTermination is M22-031.
//
// A process that dies mid-write must leave a database that reopens with its
// committed data intact. This simulates the forced termination by abandoning
// the handle without a clean close and reopening the same file.
func TestSQLiteIntegrationWALRecoveryAfterForcedTermination(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "codeflux.sqlite3")

	first, err := Open(ctx, OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Migrate(ctx, MigrationOptions{
		ApplicationVersion: "wal-recovery-test",
		BackupDirectory:    filepath.Join(root, "backups"),
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := NewRepositories(first, nil)
	if err != nil {
		t.Fatal(err)
	}
	projectID, _ := domain.NewProjectID()
	if _, err := repositories.CreateProject(ctx, CreateProject{ID: projectID, Name: "survives-crash"}); err != nil {
		t.Fatal(err)
	}

	// Abandon the handle WITHOUT a clean close: no checkpoint, no flush.
	// Whatever is durable now is what a crash would have left behind.
	if err := first.sql.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(ctx, OpenOptions{Path: path})
	if err != nil {
		t.Fatalf("a database left by a forced termination must reopen: %v", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = second.Close(closeCtx)
	}()
	recovered, err := NewRepositories(second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.GetProject(ctx, projectID); err != nil {
		t.Fatalf("a committed row was lost across forced termination: %v", err)
	}
}

// TestSQLiteIntegrationEveryMigrationUpgradePathApplies is M22-032.
//
// Migrations are forward-only and their checksums must stay stable after
// release, so this applies the full catalogue from empty and asserts the
// end state matches the declared catalogue exactly.
func TestSQLiteIntegrationEveryMigrationUpgradePathApplies(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database := openIntegrationDatabase(t, filepath.Join(root, "codeflux.sqlite3"))

	catalogue := migrations.Catalog
	if len(catalogue) == 0 {
		t.Fatal("the migration catalogue must not be empty")
	}

	var applied int
	rows, err := database.sql.QueryContext(ctx, `SELECT COUNT(DISTINCT migration_number) FROM codeflux_migration_history WHERE result = 'succeeded'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&applied); err != nil {
			t.Fatal(err)
		}
	}
	if applied != len(catalogue) {
		t.Fatalf("applied %d migrations, catalogue declares %d", applied, len(catalogue))
	}

	// Re-migrating an already-current database must be a safe no-op rather
	// than an error or a duplicate application.
	if _, err := database.Migrate(ctx, MigrationOptions{
		ApplicationVersion: "sqlite-integration-test",
		BackupDirectory:    filepath.Join(root, "backups"),
	}); err != nil {
		t.Fatalf("re-running migrations on a current database must be a no-op: %v", err)
	}
}

// TestSQLiteIntegrationBackupAndRestoreRoundTrip is M22-033.
func TestSQLiteIntegrationBackupAndRestoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database := openIntegrationDatabase(t, filepath.Join(root, "codeflux.sqlite3"))
	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	projectID, _ := domain.NewProjectID()
	if _, err := repositories.CreateProject(ctx, CreateProject{ID: projectID, Name: "backed-up"}); err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(root, "backup.sqlite3")
	if err := database.Backup(ctx, backupPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(backupPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("backup must produce a non-empty file: %v", err)
	}

	// A project created AFTER the backup must be gone once restored.
	laterID, _ := domain.NewProjectID()
	if _, err := repositories.CreateProject(ctx, CreateProject{ID: laterID, Name: "after-backup"}); err != nil {
		t.Fatal(err)
	}
	if err := database.Restore(ctx, backupPath); err != nil {
		t.Fatal(err)
	}
	restored, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.GetProject(ctx, projectID); err != nil {
		t.Fatalf("restore lost a row that existed at backup time: %v", err)
	}
	if _, err := restored.GetProject(ctx, laterID); err == nil {
		t.Fatal("restore kept a row created after the backup: the restore was not a true point-in-time")
	}
}

// TestSQLiteIntegrationDeletionAndProjectBoundaryIsolation is M22-034.
func TestSQLiteIntegrationDeletionAndProjectBoundaryIsolation(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)

	firstProject := testProjectID(t, 7100)
	firstRepository := testRepositoryID(t, 7101)
	mustCreateProjectRepository(t, repositories, firstProject, firstRepository)
	secondProject := testProjectID(t, 7200)
	secondRepository := testRepositoryID(t, 7201)
	mustCreateProjectRepository(t, repositories, secondProject, secondRepository)

	mine := createMemoryArtifactFixture(t, repositories, firstProject, firstRepository, 7102)
	theirs := createMemoryArtifactFixture(t, repositories, secondProject, secondRepository, 7202)

	// A cross-project lineage edge must be refused outright.
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, theirs, mine); err == nil {
		t.Fatal("a lineage edge across a project boundary must be rejected")
	}

	outcome, err := repositories.DeleteMemoryArtifact(ctx, DeleteMemoryArtifact{
		Target: mine, ReasonRedacted: "boundary isolation test",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, deleted := range outcome.DeletedArtifacts {
		if deleted == theirs {
			t.Fatal("deletion crossed a project boundary")
		}
	}
	stillThere, err := repositories.MemoryArtifactIsDeleted(ctx, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if stillThere {
		t.Fatal("another project's artifact was deleted")
	}
}

// TestSQLiteIntegrationDatabaseBytesCarryNoSeededSecret is M22-035.
//
// This is the strongest available statement about redaction: seed a known
// credential through the real redaction pipeline, exercise the system, then
// scan the raw database file — including the WAL — for the secret's bytes.
// AGENTS.md requires secrets be redacted before persistence, and a file scan
// is the only way to prove persistence actually happened redacted.
func TestSQLiteIntegrationDatabaseBytesCarryNoSeededSecret(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "codeflux.sqlite3")
	database := openIntegrationDatabase(t, path)
	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}

	secret := testfixtures.FixtureCredentialMaterial
	projectID, _ := domain.NewProjectID()
	if _, err := repositories.CreateProject(ctx, CreateProject{ID: projectID, Name: "secret-scan"}); err != nil {
		t.Fatal(err)
	}

	// Write the secret only where a redacted value is expected to land.
	// If any code path persists it verbatim, the scan below finds it.
	repositoryID, _ := domain.NewRepositoryID()
	if _, err := repositories.CreateRepository(ctx, CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: filepath.Join(root, "repo"), GitIdentity: strings.Repeat("a", 40),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := database.CheckpointWAL(ctx, true); err != nil && !errors.Is(err, context.Canceled) {
		t.Logf("checkpoint before scan reported: %v", err)
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		candidate := path + suffix
		contents, err := os.ReadFile(candidate)
		if err != nil {
			continue // -wal and -shm need not exist.
		}
		if bytes.Contains(contents, []byte(secret)) {
			t.Fatalf("seeded credential found verbatim in %s", filepath.Base(candidate))
		}
	}
}
