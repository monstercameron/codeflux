package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"modernc.org/sqlite"
)

type onlineBackuper interface {
	NewBackup(string) (*sqlite.Backup, error)
}

type onlineRestorer interface {
	NewRestore(string) (*sqlite.Backup, error)
}

// Backup creates a new restrictive SQLite-consistent snapshot. The destination
// must not already exist.
func (database *Database) Backup(ctx context.Context, destination string) error {
	resolved, err := prepareBackupDestination(database.path, destination)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(resolved)
		}
	}()
	connection, err := database.sql.Conn(ctx)
	if err != nil {
		return classify("acquire backup connection", err)
	}
	defer connection.Close()
	err = connection.Raw(func(driverConnection any) error {
		backuper, ok := driverConnection.(onlineBackuper)
		if !ok {
			return errors.New("SQLite driver does not expose online backup")
		}
		backup, err := backuper.NewBackup(resolved)
		if err != nil {
			return err
		}
		stepErr := stepBackup(ctx, backup)
		return errors.Join(stepErr, backup.Finish())
	})
	if err != nil {
		return classify("create SQLite backup", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(resolved, 0o600); err != nil {
			return classify("restrict SQLite backup", err)
		}
	}
	success = true
	return nil
}

// Restore replaces the open database content from a verified SQLite snapshot.
// Callers must hold migration authority and prevent application traffic.
func (database *Database) Restore(ctx context.Context, source string) error {
	resolved, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve restore source: %w", err)
	}
	if resolved == database.path {
		return errors.New("restore source must differ from the database")
	}
	if err := verifySQLiteFile(ctx, resolved); err != nil {
		return fmt.Errorf("verify restore source: %w", err)
	}
	connection, err := database.sql.Conn(ctx)
	if err != nil {
		return classify("acquire restore connection", err)
	}
	defer connection.Close()
	err = connection.Raw(func(driverConnection any) error {
		restorer, ok := driverConnection.(onlineRestorer)
		if !ok {
			return errors.New("SQLite driver does not expose online restore")
		}
		restore, err := restorer.NewRestore(resolved)
		if err != nil {
			return err
		}
		stepErr := stepBackup(ctx, restore)
		return errors.Join(stepErr, restore.Finish())
	})
	if err != nil {
		return classify("restore SQLite backup", err)
	}
	return database.verifyConnectionPolicy(ctx)
}

func prepareBackupDestination(databasePath, destination string) (string, error) {
	if destination == "" {
		return "", errors.New("backup destination must not be empty")
	}
	resolved, err := filepath.Abs(destination)
	if err != nil {
		return "", fmt.Errorf("resolve backup destination: %w", err)
	}
	if resolved == databasePath {
		return "", errors.New("backup destination must differ from the database")
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o700); err != nil {
		return "", classify("create backup directory", err)
	}
	if err := restrictToCurrentUser(filepath.Dir(resolved)); err != nil {
		return "", err
	}
	file, err := os.OpenFile(resolved, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return "", classify("reserve backup destination", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(resolved)
		return "", classify("close backup destination", err)
	}
	// A backup is a byte-for-byte copy of the authoritative store, so it needs
	// the same grant. Restricting it before the copy runs means it is never
	// readable, rather than readable for the duration of the copy.
	if err := restrictToCurrentUser(resolved); err != nil {
		_ = os.Remove(resolved)
		return "", err
	}
	return resolved, nil
}

func stepBackup(ctx context.Context, backup *sqlite.Backup) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		more, err := backup.Step(128)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

func verifySQLiteFile(ctx context.Context, path string) error {
	database, err := Open(ctx, OpenOptions{Path: path, MaximumConnections: 1})
	if err != nil {
		return err
	}
	defer database.Close(context.Background())
	var result string
	if err := database.sql.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return classify("integrity-check SQLite file", err)
	}
	if result != "ok" {
		return &Error{
			Kind:      ErrCorrupt,
			Operation: "integrity-check SQLite file",
			Cause:     errors.New("SQLite integrity check did not return ok"),
		}
	}
	return nil
}
