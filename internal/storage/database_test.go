package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestOpenConfiguresAndHealthChecksRealSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "codeflux.sqlite3")
	database, err := Open(context.Background(), OpenOptions{
		Path:               path,
		BusyTimeout:        750 * time.Millisecond,
		MaximumConnections: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	report, err := database.CheckHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Readable ||
		!report.Writable ||
		!report.ForeignKeysEnabled ||
		report.JournalMode != "wal" ||
		report.Synchronous != 2 ||
		report.BusyTimeout != 750*time.Millisecond {
		t.Fatalf("health report = %#v", report)
	}
	if got := database.Path(); got != path {
		t.Fatalf("database path = %q, want %q", got, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file: %v", err)
	}
	if maximum := database.sql.Stats().MaxOpenConnections; maximum != 3 {
		t.Fatalf("maximum connections = %d, want 3", maximum)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if permissions := info.Mode().Perm(); permissions != 0o600 {
			t.Fatalf("database permissions = %04o, want 0600", permissions)
		}
	}
}

func TestOpenRejectsInvalidOptions(t *testing.T) {
	tests := []OpenOptions{
		{},
		{Path: filepath.Join(t.TempDir(), "db"), BusyTimeout: -time.Millisecond},
		{Path: filepath.Join(t.TempDir(), "db"), BusyTimeout: time.Microsecond},
		{Path: filepath.Join(t.TempDir(), "db"), MaximumConnections: -1},
	}
	for _, options := range tests {
		if database, err := Open(context.Background(), options); err == nil {
			_ = database.Close(context.Background())
			t.Fatalf("invalid options accepted: %#v", options)
		}
	}
}

func TestOpenClassifiesNonDatabaseContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-database.sqlite3")
	if err := os.WriteFile(path, []byte("this is not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := Open(context.Background(), OpenOptions{Path: path})
	if database != nil {
		_ = database.Close(context.Background())
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("open error = %v, want corrupt classification", err)
	}
	var storageError *Error
	if !errors.As(err, &storageError) {
		t.Fatalf("open error type = %T, want *storage.Error", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	database, err := Open(context.Background(), OpenOptions{
		Path: filepath.Join(t.TempDir(), "codeflux.sqlite3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	first := database.Close(context.Background())
	second := database.Close(context.Background())
	if first != nil || second != nil {
		t.Fatalf("close errors = %v, %v", first, second)
	}
}

func TestBusyTimeoutBoundsWriteContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codeflux.sqlite3")
	const timeout = 150 * time.Millisecond
	first, err := Open(context.Background(), OpenOptions{
		Path:               path,
		BusyTimeout:        timeout,
		MaximumConnections: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(context.Background())
	second, err := Open(context.Background(), OpenOptions{
		Path:               path,
		BusyTimeout:        timeout,
		MaximumConnections: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())

	blocking, err := first.sql.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocking.Rollback()

	started := time.Now()
	contending, err := second.sql.BeginTx(context.Background(), nil)
	elapsed := time.Since(started)
	if contending != nil {
		_ = contending.Rollback()
	}
	classified := classify("begin contending write", err)
	if !errors.Is(classified, ErrBusy) {
		t.Fatalf("contention error = %v, want busy", classified)
	}
	if elapsed < 100*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("contention elapsed = %v, want bounded wait near %v", elapsed, timeout)
	}
}

func TestClassifyUnreadableFile(t *testing.T) {
	err := classify("open fixture", os.ErrPermission)
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("classification = %v, want unreadable", err)
	}
}
