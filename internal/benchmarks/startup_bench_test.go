package benchmarks

import (
	"context"
	"path/filepath"
	"testing"

	"codeflux.dev/codeflux/internal/coordinator"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/migrations"
)

// startApplication boots one real coordinator against a database path.
func startApplication(tb testing.TB, databasePath, backupDirectory string) *coordinator.Application {
	tb.Helper()
	application, err := coordinator.StartApplication(context.Background(), coordinator.ApplicationOptions{
		DatabasePath:      databasePath,
		BackupDirectory:   backupDirectory,
		ListenAddress:     "127.0.0.1:0",
		TaskListenAddress: "127.0.0.1:0",
	})
	if err != nil {
		tb.Fatalf("start coordinator: %v", err)
	}
	return application
}

// BenchmarkColdCoordinatorStartup is M22-076: the first launch on a machine,
// where the database does not exist and every migration must run.
//
// This is the number a new user actually experiences, so the fixture is a fresh
// directory per iteration rather than a reused one.
func BenchmarkColdCoordinatorStartup(b *testing.B) {
	LogEnvironment(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		b.StopTimer()
		root := b.TempDir()
		databasePath := filepath.Join(root, "cold.sqlite3")
		backups := filepath.Join(root, "backups")
		b.StartTimer()

		application := startApplication(b, databasePath, backups)

		b.StopTimer()
		if err := application.Shutdown(context.Background()); err != nil {
			b.Fatalf("shutdown iteration %d: %v", index, err)
		}
		b.StartTimer()
	}
}

// BenchmarkWarmCoordinatorStartup is M22-077: every launch after the first,
// where the schema is already current and only verification runs.
//
// The database is built once and reopened, so the measurement isolates the
// warm path instead of re-measuring migration.
func BenchmarkWarmCoordinatorStartup(b *testing.B) {
	LogEnvironment(b)
	root := b.TempDir()
	databasePath := filepath.Join(root, "warm.sqlite3")
	backups := filepath.Join(root, "backups")

	first := startApplication(b, databasePath, backups)
	if err := first.Shutdown(context.Background()); err != nil {
		b.Fatalf("shutdown warm fixture: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		application := startApplication(b, databasePath, backups)
		b.StopTimer()
		if err := application.Shutdown(context.Background()); err != nil {
			b.Fatalf("shutdown iteration %d: %v", index, err)
		}
		b.StartTimer()
	}
}

// BenchmarkMigrationFromPriorSchema is M22-078.
//
// The prior schema is reached by applying every migration except the last, so
// the measured step is the real upgrade a user takes when they update the
// application — not a synthetic one that would drift from the catalog.
func BenchmarkMigrationFromPriorSchema(b *testing.B) {
	LogEnvironment(b)
	sources, err := migrations.Sources()
	if err != nil {
		b.Fatalf("load migration sources: %v", err)
	}
	if len(sources) < 2 {
		b.Skipf("migration benchmark needs at least two migrations, catalog has %d", len(sources))
	}
	// The prior schema is the real catalog with its final migration withheld,
	// so the measured step is exactly the upgrade a user takes on update.
	prior := sources[:len(sources)-1]

	migrate := func(databasePath string, catalog []migrations.Source) storage.MigrationResult {
		database, openErr := storage.Open(context.Background(), storage.OpenOptions{Path: databasePath})
		if openErr != nil {
			b.Fatalf("open %s: %v", databasePath, openErr)
		}
		defer func() {
			if closeErr := database.Close(context.Background()); closeErr != nil {
				b.Fatalf("close %s: %v", databasePath, closeErr)
			}
		}()
		result, migrateErr := database.Migrate(context.Background(), storage.MigrationOptions{
			ApplicationVersion: "benchmark",
			BackupDirectory:    filepath.Join(filepath.Dir(databasePath), "backups"),
			Sources:            catalog,
		})
		if migrateErr != nil {
			b.Fatalf("migrate %s: %v", databasePath, migrateErr)
		}
		return result
	}

	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		b.StopTimer()
		databasePath := filepath.Join(b.TempDir(), "upgrade.sqlite3")
		if result := migrate(databasePath, prior); result.ToVersion == 0 {
			b.Fatalf("prior schema for iteration %d did not apply", index)
		}
		b.StartTimer()

		result := migrate(databasePath, sources)

		b.StopTimer()
		if result.FromVersion == result.ToVersion {
			b.Fatalf("iteration %d measured no upgrade (from=%d to=%d)",
				index, result.FromVersion, result.ToVersion)
		}
		b.StartTimer()
	}
}
