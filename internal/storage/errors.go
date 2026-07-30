package storage

import (
	"errors"
	"fmt"
	"io/fs"

	"modernc.org/sqlite"
)

var (
	// ErrBusy means SQLite could not acquire a required lock within policy.
	ErrBusy = errors.New("database busy")
	// ErrCorrupt means SQLite reported malformed or non-database content.
	ErrCorrupt = errors.New("database corrupt")
	// ErrUnreadable means the database path or content cannot be read safely.
	ErrUnreadable = errors.New("database unreadable")
	// ErrConstraint means a durable database constraint rejected a write.
	ErrConstraint = errors.New("database constraint")
	// ErrMigrationLocked means another process owns schema migration authority.
	ErrMigrationLocked = errors.New("database migration locked")
	// ErrMigrationFailed means a prior or current migration failed safely.
	ErrMigrationFailed = errors.New("database migration failed")
	// ErrSchemaNewer means the database requires a newer Codeflux binary.
	ErrSchemaNewer = errors.New("database schema newer than binary")
	// ErrMigrationChecksum means immutable migration history does not match.
	ErrMigrationChecksum = errors.New("database migration checksum mismatch")
	// ErrDiskSpace means backup and migration safety space is unavailable.
	ErrDiskSpace = errors.New("insufficient disk space for database migration")
)

// Error adds a stable storage classification and bounded operation name.
type Error struct {
	Kind      error
	Operation string
	Cause     error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %v", e.Operation, e.Kind)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func (e *Error) Is(target error) bool {
	return target == e.Kind || errors.Is(e.Cause, target)
}

func classify(operation string, err error) error {
	if err == nil {
		return nil
	}
	kind := classifyKind(err)
	if kind == nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return &Error{Kind: kind, Operation: operation, Cause: err}
}

func classifyKind(err error) error {
	var sqliteError *sqlite.Error
	if errors.As(err, &sqliteError) {
		switch sqliteError.Code() & 0xff {
		case 5, 6:
			return ErrBusy
		case 11, 26:
			return ErrCorrupt
		case 3, 8, 10, 14:
			return ErrUnreadable
		case 19:
			return ErrConstraint
		}
	}
	if errors.Is(err, fs.ErrPermission) {
		return ErrUnreadable
	}
	return nil
}
