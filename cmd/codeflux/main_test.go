package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/storage"
)

func TestVersionReportsEveryIdentityField(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"version"})
	if code != exitOK {
		t.Fatalf("version exit = %d, want %d; stderr=%q", code, exitOK, stderr.String())
	}
	for _, field := range []string{
		"version:",
		"commit:",
		"build-date:",
		"go-version:",
		"schema-version:",
		"frontend-version:",
	} {
		if !strings.Contains(stdout.String(), field) {
			t.Errorf("version output omits %q: %s", field, stdout.String())
		}
	}
}

func TestDoctorIsHonestAboutUnavailableSubsystems(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "missing.sqlite3")
	code := run(&stdout, &stderr, []string{"doctor", "--database", missing})
	if code != exitUnavailable {
		t.Fatalf("doctor exit = %d, want %d", code, exitUnavailable)
	}
	for _, subsystem := range []string{"storage: missing", "credential-store: unavailable", "browser-transport: unavailable"} {
		if !strings.Contains(stdout.String(), subsystem) {
			t.Errorf("doctor output omits %q: %s", subsystem, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("doctor stderr = %q, want empty", stderr.String())
	}
}

func TestDoctorReportsSafeDatabaseDiagnostics(t *testing.T) {
	secretShapedDirectory := "sk-" + strings.Repeat("X", 24)
	path := createCLITestDatabase(t, secretShapedDirectory)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor", "--database", path})
	if code != exitUnavailable {
		t.Fatalf("doctor exit = %d, want %d; stderr=%q", code, exitUnavailable, stderr.String())
	}
	for _, field := range []string{
		"storage: ok",
		"git: ok",
		"database-bytes:",
		"sqlite-total-bytes:",
		"schema-version:",
		"supported-schema-version:",
		"successful-migrations:",
		"failed-migrations:",
	} {
		if !strings.Contains(stdout.String(), field) {
			t.Errorf("doctor output omits %q: %s", field, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), secretShapedDirectory) ||
		strings.Contains(stderr.String(), secretShapedDirectory) {
		t.Fatal("doctor output exposed the selected database path")
	}
	if strings.Contains(stdout.String(), "git.exe") {
		t.Fatal("doctor output exposed the resolved Git executable path")
	}
}

func TestBackupAndIntegrityCommandsUseExplicitDatabase(t *testing.T) {
	path := createCLITestDatabase(t, "database")
	backup := filepath.Join(t.TempDir(), "user-backup.sqlite3")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run(
		&stdout,
		&stderr,
		[]string{"backup", "--database", path, "--output", backup},
	); code != exitOK {
		t.Fatalf("backup exit = %d, stderr=%q", code, stderr.String())
	}
	if stdout.String() != "backup: ok\n" {
		t.Fatalf("backup stdout = %q", stdout.String())
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup file: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(
		&stdout,
		&stderr,
		[]string{"integrity", "--database", backup},
	); code != exitOK {
		t.Fatalf("integrity exit = %d, stderr=%q", code, stderr.String())
	}
	if stdout.String() != "integrity: ok\n" {
		t.Fatalf("integrity stdout = %q", stdout.String())
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"unknown"})
	if code != exitUsage {
		t.Fatalf("unknown exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Fatalf("unknown stderr = %q", stderr.String())
	}
}

func createCLITestDatabase(t *testing.T, directory string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), directory)
	path := filepath.Join(root, "codeflux.sqlite3")
	database, err := storage.Open(context.Background(), storage.OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Migrate(context.Background(), storage.MigrationOptions{
		ApplicationVersion: "cli-test",
		BackupDirectory:    filepath.Join(root, "migration-backups"),
	}); err != nil {
		_ = database.Close(context.Background())
		t.Fatal(err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	return path
}
