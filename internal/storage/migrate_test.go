package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"codeflux.dev/codeflux/migrations"
)

var migrationTestTime = time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

func TestMigrateEmptyDatabaseAndEveryCommittedVersion(t *testing.T) {
	ctx := context.Background()
	database := openMigrationTestDatabase(t)
	options := migrationTestOptions(filepath.Join(t.TempDir(), "backups"), nil)

	first, err := database.Migrate(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.FromVersion != -1 ||
		first.ToVersion != migrations.LatestVersion() ||
		first.Applied != len(migrations.Catalog) ||
		first.BackupPath == "" {
		t.Fatalf("first migration result = %#v", first)
	}
	if _, err := verifyFileExists(first.BackupPath); err != nil {
		t.Fatal(err)
	}
	second, err := database.Migrate(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.FromVersion != migrations.LatestVersion() ||
		second.ToVersion != migrations.LatestVersion() ||
		second.Applied != 0 ||
		second.BackupPath != "" {
		t.Fatalf("second migration result = %#v", second)
	}
	var succeeded int
	if err := database.sql.QueryRowContext(
		ctx,
		`SELECT count(*) FROM codeflux_migration_history WHERE result = 'succeeded'`,
	).Scan(&succeeded); err != nil {
		t.Fatal(err)
	}
	if succeeded != len(migrations.Catalog) {
		t.Fatalf("successful migrations = %d, want %d", succeeded, len(migrations.Catalog))
	}
}

func TestMigrateAcrossEveryCommittedSchemaVersion(t *testing.T) {
	ctx := context.Background()
	sources, err := migrations.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for start := -1; start <= migrations.LatestVersion(); start++ {
		t.Run("from_"+stringVersion(start), func(t *testing.T) {
			database := openMigrationTestDatabase(t)
			backupDirectory := filepath.Join(t.TempDir(), "backups")
			if start >= 0 {
				prefix := append([]migrations.Source(nil), sources[:start+1]...)
				if _, err := database.Migrate(
					ctx,
					migrationTestOptions(backupDirectory, prefix),
				); err != nil {
					t.Fatalf("prepare schema version %d: %v", start, err)
				}
			}
			result, err := database.Migrate(
				ctx,
				migrationTestOptions(backupDirectory, sources),
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.FromVersion != start ||
				result.ToVersion != migrations.LatestVersion() ||
				result.Applied != migrations.LatestVersion()-start {
				t.Fatalf("migration result = %#v", result)
			}
		})
	}
}

func TestMigrateRefusesSchemaNewerThanBinary(t *testing.T) {
	ctx := context.Background()
	database := openMigrationTestDatabase(t)
	options := migrationTestOptions(filepath.Join(t.TempDir(), "backups"), nil)
	if err := database.ensureMigrationControl(ctx, options); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`UPDATE codeflux_schema_version SET version = ? WHERE singleton = 1`,
		migrations.LatestVersion()+1,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Migrate(ctx, options); !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("downgrade error = %v, want schema-newer", err)
	}
}

func TestMigrateValidatesDiskSpaceBeforeBackup(t *testing.T) {
	ctx := context.Background()
	database := openMigrationTestDatabase(t)
	options := migrationTestOptions(filepath.Join(t.TempDir(), "backups"), nil)
	options.AvailableBytes = func(string) (uint64, error) { return 0, nil }
	if _, err := database.Migrate(ctx, options); !errors.Is(err, ErrDiskSpace) {
		t.Fatalf("disk-space error = %v, want insufficient space", err)
	}
}

func TestFailedMigrationRestoresAndRefusesRepeatedMutation(t *testing.T) {
	ctx := context.Background()
	database := openMigrationTestDatabase(t)
	if _, err := database.sql.ExecContext(
		ctx,
		`CREATE TABLE retained (id INTEGER PRIMARY KEY, value TEXT NOT NULL) STRICT;
		 INSERT INTO retained (id, value) VALUES (1, 'retained');`,
	); err != nil {
		t.Fatal(err)
	}
	sources := []migrations.Source{
		testMigrationSource(
			0,
			"000000_failure.sql",
			`CREATE TABLE rolled_back (id INTEGER PRIMARY KEY) STRICT;
			 THIS IS NOT VALID SQL;`,
		),
	}
	options := migrationTestOptions(filepath.Join(t.TempDir(), "backups"), sources)
	if _, err := database.Migrate(ctx, options); !errors.Is(err, ErrMigrationFailed) {
		t.Fatalf("migration error = %v, want failed", err)
	}
	assertTableValue(t, database, "retained", "retained")
	assertTableAbsent(t, database, "rolled_back")
	assertSchemaVersion(t, database, -1)
	failures := migrationFailureCount(t, database)
	if failures != 1 {
		t.Fatalf("failure records = %d, want 1", failures)
	}

	if _, err := database.Migrate(ctx, options); !errors.Is(err, ErrMigrationFailed) {
		t.Fatalf("repeated migration error = %v, want stable failure", err)
	}
	if got := migrationFailureCount(t, database); got != failures {
		t.Fatalf("repeated attempt changed failures from %d to %d", failures, got)
	}
}

func TestRestartAfterInterruptedMigrationRestoresBackup(t *testing.T) {
	ctx := context.Background()
	database := openMigrationTestDatabase(t)
	sources := []migrations.Source{
		testMigrationSource(
			0,
			"000000_interrupted.sql",
			`CREATE TABLE intended (id INTEGER PRIMARY KEY) STRICT`,
		),
	}
	options := migrationTestOptions(filepath.Join(t.TempDir(), "backups"), sources)
	if err := database.ensureMigrationControl(ctx, options); err != nil {
		t.Fatal(err)
	}
	backupPath, err := database.prepareMigrationBackup(ctx, options, -1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.recordMigrationStarted(
		ctx,
		sources[0],
		options.ApplicationVersion,
		migrationTestTime.UnixMicro(),
		backupPath,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`CREATE TABLE interrupted_residue (id INTEGER PRIMARY KEY) STRICT`,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := database.Migrate(ctx, options); !errors.Is(err, ErrMigrationFailed) {
		t.Fatalf("interrupted migration error = %v, want failed after restore", err)
	}
	assertTableAbsent(t, database, "interrupted_residue")
	assertSchemaVersion(t, database, -1)
	if got := migrationFailureCount(t, database); got != 1 {
		t.Fatalf("interruption failure records = %d, want 1", got)
	}
}

func TestMigrationAuthorityIsCrossProcessLocked(t *testing.T) {
	ctx := context.Background()
	database := openMigrationTestDatabase(t)
	lock := flock.New(database.path+".migration.lock", flock.SetPermissions(0o600))
	locked, err := lock.TryLock()
	if err != nil || !locked {
		t.Fatalf("acquire fixture lock = %v, %v", locked, err)
	}
	defer lock.Close()

	waitCtx, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()
	options := migrationTestOptions(filepath.Join(t.TempDir(), "backups"), nil)
	if _, err := database.Migrate(waitCtx, options); !errors.Is(err, ErrMigrationLocked) {
		t.Fatalf("migration lock error = %v, want locked", err)
	}
}

func TestMigrationRejectsSourceChecksumMismatch(t *testing.T) {
	database := openMigrationTestDatabase(t)
	source := testMigrationSource(0, "000000_fixture.sql", "SELECT 1;")
	source.SQL = "SELECT 2;"
	options := migrationTestOptions(filepath.Join(t.TempDir(), "backups"), []migrations.Source{source})
	if _, err := database.Migrate(context.Background(), options); !errors.Is(err, ErrMigrationChecksum) {
		t.Fatalf("checksum error = %v, want mismatch", err)
	}
}

func openMigrationTestDatabase(t *testing.T) *Database {
	t.Helper()
	database, err := Open(context.Background(), OpenOptions{
		Path: filepath.Join(t.TempDir(), "codeflux.sqlite3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close migration database: %v", err)
		}
	})
	return database
}

func migrationTestOptions(
	backupDirectory string,
	sources []migrations.Source,
) MigrationOptions {
	return MigrationOptions{
		ApplicationVersion: "test-1.0.0",
		BackupDirectory:    backupDirectory,
		Sources:            sources,
		Now:                func() time.Time { return migrationTestTime },
		AvailableBytes: func(string) (uint64, error) {
			return math.MaxUint64, nil
		},
	}
}

func testMigrationSource(number int, name string, sql string) migrations.Source {
	sum := sha256.Sum256([]byte(sql))
	return migrations.Source{
		Descriptor: migrations.Descriptor{
			Number: number,
			Name:   name,
			SHA256: hex.EncodeToString(sum[:]),
		},
		SQL: sql,
	}
}

func verifyFileExists(path string) (string, error) {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(resolved); err != nil {
		return "", errors.New("backup file does not exist")
	}
	return resolved, nil
}

func stringVersion(version int) string {
	if version < 0 {
		return "empty"
	}
	return "v" + fmt.Sprint(version)
}

func assertSchemaVersion(t *testing.T, database *Database, want int) {
	t.Helper()
	got, err := database.currentSchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("schema version = %d, want %d", got, want)
	}
}

func assertTableValue(t *testing.T, database *Database, table string, want string) {
	t.Helper()
	var value string
	query := `SELECT value FROM ` + table + ` WHERE id = 1`
	if err := database.sql.QueryRowContext(context.Background(), query).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != want {
		t.Fatalf("%s value = %q, want %q", table, value, want)
	}
}

func assertTableAbsent(t *testing.T, database *Database, table string) {
	t.Helper()
	var count int
	if err := database.sql.QueryRowContext(
		context.Background(),
		`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`,
		table,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("table %s still exists", table)
	}
}

func migrationFailureCount(t *testing.T, database *Database) int {
	t.Helper()
	var count int
	if err := database.sql.QueryRowContext(
		context.Background(),
		`SELECT count(*) FROM codeflux_migration_history WHERE result = 'failed'`,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
