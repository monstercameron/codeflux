package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOnlineBackupAndRestore(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, OpenOptions{
		Path: filepath.Join(t.TempDir(), "codeflux.sqlite3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close(ctx)
	if _, err := database.sql.ExecContext(
		ctx,
		`CREATE TABLE durable_value (id INTEGER PRIMARY KEY, value TEXT NOT NULL) STRICT`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO durable_value (id, value) VALUES (1, 'before')`,
	); err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(t.TempDir(), "snapshot.sqlite3")
	if err := database.Backup(ctx, backupPath); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`UPDATE durable_value SET value = 'after' WHERE id = 1`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Restore(ctx, backupPath); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := database.sql.QueryRowContext(
		ctx,
		`SELECT value FROM durable_value WHERE id = 1`,
	).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "before" {
		t.Fatalf("restored value = %q, want before", value)
	}
}

func TestBackupRefusesExistingDestination(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, OpenOptions{
		Path: filepath.Join(t.TempDir(), "codeflux.sqlite3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close(ctx)
	destination := filepath.Join(t.TempDir(), "existing.sqlite3")
	if err := os.WriteFile(destination, []byte("retain"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := database.Backup(ctx, destination); err == nil {
		t.Fatal("existing backup destination was overwritten")
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "retain" {
		t.Fatalf("existing destination changed to %q", content)
	}
}
