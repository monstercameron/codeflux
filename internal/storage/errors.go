package storage

import (
	"context"
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
	// ErrNotFound means the requested durable identity does not exist.
	ErrNotFound = errors.New("database entity not found")
	// ErrConflict means an idempotency or uniqueness claim disagrees.
	ErrConflict = errors.New("database write conflict")
	// ErrStaleRevision means optimistic concurrency rejected an old revision.
	ErrStaleRevision = errors.New("database entity revision is stale")
)

// Error adds a stable storage classification and bounded operation name.
type Error struct {
	Kind      error
	Operation string
	Cause     error
}

// Error names the operation, its classification, and what the store actually
// said.
//
// The cause used to be kept but never printed, so every constraint failure
// read as "database constraint" and the one fact that identifies which rule was
// broken — the rule's own name — was carried all the way up and discarded at
// the point somebody would read it.
func (e *Error) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%s: %v", e.Operation, e.Kind)
	}
	return fmt.Sprintf("%s: %v: %v", e.Operation, e.Kind, e.Cause)
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	kind := classifyKind(err)
	if kind == nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return &Error{Kind: kind, Operation: operation, Cause: err}
}

func typedError(kind error, operation string, cause error) error {
	return &Error{Kind: kind, Operation: operation, Cause: cause}
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

func repositoryConstraintKind(err error) error {
	var sqliteError *sqlite.Error
	if !errors.As(err, &sqliteError) {
		return nil
	}
	switch sqliteError.Code() {
	case 1555, 2067:
		return ErrConflict
	default:
		if sqliteError.Code()&0xff == 19 {
			return ErrConstraint
		}
		return nil
	}
}
