package storage

import (
	"context"
	"testing"
	"time"

	"codeflux.dev/codeflux/migrations"
)

func TestDiagnoseReportsHealthSizeAndVersionsWithoutPath(t *testing.T) {
	database := openMigratedSchema(t)
	diagnostics, err := database.Diagnose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !diagnostics.Health.Readable ||
		!diagnostics.Health.Writable ||
		diagnostics.DatabaseBytes == 0 ||
		diagnostics.TotalSQLiteBytes < diagnostics.DatabaseBytes ||
		diagnostics.SchemaVersion != migrations.LatestVersion() ||
		diagnostics.SupportedSchemaVersion != migrations.LatestVersion() ||
		diagnostics.SuccessfulMigrations != uint64(len(migrations.Catalog)) ||
		diagnostics.FailedMigrations != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestIntegrityCheckpointAndOrphanChecks(t *testing.T) {
	database := openMigratedSchema(t)
	if err := database.IntegrityCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := database.CheckpointWAL(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Busy {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
	if err := database.ValidateNoOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRetentionExpiresAndCapsVerboseOutput(t *testing.T) {
	ctx := context.Background()
	database := openMigratedSchema(t)
	seedOperationalParents(t, database)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	if _, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO command_executions (
			id, task_id, run_id, state, command_name, arguments_redacted_json,
			working_directory_scope, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (
			'command_1', 'task_1', 'run_1', 'succeeded', 'go-test', '[]',
			'.', 'command-key', ?, ?
		)`,
		now.Add(-40*24*time.Hour).UnixMicro(),
		now.UnixMicro(),
	); err != nil {
		t.Fatal(err)
	}
	chunks := []struct {
		id       string
		sequence int
		bytes    int
		created  time.Time
	}{
		{id: "output_old", sequence: 1, bytes: 100, created: now.Add(-31 * 24 * time.Hour)},
		{id: "output_2", sequence: 2, bytes: 5, created: now.Add(-time.Hour)},
		{id: "output_3", sequence: 3, bytes: 5, created: now.Add(-time.Hour)},
		{id: "output_4", sequence: 4, bytes: 5, created: now.Add(-time.Hour)},
	}
	for _, chunk := range chunks {
		if _, err := database.sql.ExecContext(
			ctx,
			`INSERT INTO redacted_outputs (
				id, command_execution_id, stream, sequence, content_redacted,
				byte_count, truncated, created_at_unix_micros
			) VALUES (?, 'command_1', 'stdout', ?, 'fixture', ?, 0, ?)`,
			chunk.id,
			chunk.sequence,
			chunk.bytes,
			chunk.created.UnixMicro(),
		); err != nil {
			t.Fatal(err)
		}
	}
	result, err := database.ApplyRetention(ctx, RetentionPolicy{
		VerboseOutputMaxAge:       30 * 24 * time.Hour,
		MaxVerboseBytesPerCommand: 10,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExpiredOutputChunks != 1 || result.OversizeOutputChunks != 1 {
		t.Fatalf("retention result = %#v", result)
	}
	var remaining int
	if err := database.sql.QueryRowContext(
		ctx,
		`SELECT count(*) FROM redacted_outputs`,
	).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 2 {
		t.Fatalf("remaining output chunks = %d, want 2", remaining)
	}
}

// TestMaintenancePoliciesAreExplicit covers the retention policy only.
//
// It also asserted DefaultDeletionPolicy's four fields until AUDIT-005a removed
// that type. The assertion re-stated the declaration's own literal values and
// nothing executed them, so it could not fail for any reason that mattered:
// the schema refuses the deletions the policy called "tombstone-then-purge",
// and the test passed anyway.
func TestMaintenancePoliciesAreExplicit(t *testing.T) {
	retention := DefaultRetentionPolicy()
	if retention.VerboseOutputMaxAge <= 0 || retention.MaxVerboseBytesPerCommand == 0 {
		t.Fatalf("retention policy = %#v", retention)
	}
}
