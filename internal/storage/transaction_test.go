package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRunInTransactionCommitsOnlySuccessfulOperation(t *testing.T) {
	database := openMigratedSchema(t)
	ctx := context.Background()
	if err := database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO projects (
				id, name, created_at_unix_micros, updated_at_unix_micros
			) VALUES ('project_commit', 'Committed', 1, 1)`,
		)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.sql.QueryRowContext(
		ctx,
		`SELECT count(*) FROM projects WHERE id = 'project_commit'`,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("committed projects = %d, want 1", count)
	}
}

func TestRunInTransactionRollsBackFailureHalfwayThroughMultiTableWrite(t *testing.T) {
	database := openMigratedSchema(t)
	ctx := context.Background()
	injected := errors.New("injected halfway failure")
	err := database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO projects (
				id, name, created_at_unix_micros, updated_at_unix_micros
			) VALUES ('project_rollback', 'Rollback', 1, 1)`,
		); err != nil {
			return err
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO repositories (
				id, project_id, canonical_path, git_identity,
				created_at_unix_micros, updated_at_unix_micros
			) VALUES (
				'repo_rollback', 'project_rollback', '/rollback', 'git-rollback', 1, 1
			)`,
		); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("transaction error = %v, want injected failure", err)
	}
	for _, table := range []string{"projects", "repositories"} {
		var count int
		if err := database.sql.QueryRowContext(
			ctx,
			`SELECT count(*) FROM `+table+` WHERE id LIKE '%_rollback'`,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d rolled-back rows", table, count)
		}
	}
}

func TestRunInTransactionCancelsBlockedDatabaseCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codeflux.sqlite3")
	first, err := Open(context.Background(), OpenOptions{
		Path:               path,
		BusyTimeout:        5 * time.Second,
		MaximumConnections: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(context.Background())
	second, err := Open(context.Background(), OpenOptions{
		Path:               path,
		BusyTimeout:        5 * time.Second,
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

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = second.RunInTransaction(ctx, func(*Transaction) error { return nil })
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked transaction error = %v, want deadline", err)
	}
	if elapsed > time.Second {
		t.Fatalf("blocked transaction cancellation took %v", elapsed)
	}
	var restoredMillis int64
	if err := second.sql.QueryRow("PRAGMA busy_timeout").Scan(&restoredMillis); err != nil {
		t.Fatal(err)
	}
	if restoredMillis != (5 * time.Second).Milliseconds() {
		t.Fatalf("restored busy timeout = %d ms, want 5000", restoredMillis)
	}
}

func TestTypedStorageErrorsAreStable(t *testing.T) {
	for _, kind := range []error{
		ErrNotFound,
		ErrConflict,
		ErrStaleRevision,
		ErrBusy,
		ErrCorrupt,
		ErrConstraint,
	} {
		err := typedError(kind, "fixture operation", errors.New("fixture cause"))
		if !errors.Is(err, kind) {
			t.Errorf("%v does not match its typed error", kind)
		}
		var storageError *Error
		if !errors.As(err, &storageError) {
			t.Errorf("%v has type %T, want *storage.Error", kind, err)
		}
	}
}
