package storage

import (
	"context"
	"errors"
	"os"
	"time"

	"codeflux.dev/codeflux/migrations"
)

// Diagnostics is a path-free snapshot of local SQLite health and versions.
type Diagnostics struct {
	Health                 Health
	DatabaseBytes          uint64
	WALBytes               uint64
	SharedMemoryBytes      uint64
	TotalSQLiteBytes       uint64
	SchemaVersion          int
	SupportedSchemaVersion int
	SuccessfulMigrations   uint64
	FailedMigrations       uint64
}

// WALCheckpoint reports one safe checkpoint attempt.
type WALCheckpoint struct {
	Busy               bool
	LogFrames          int
	CheckpointedFrames int
}

// RetentionPolicy bounds verbose command output retained in SQLite.
type RetentionPolicy struct {
	VerboseOutputMaxAge       time.Duration
	MaxVerboseBytesPerCommand uint64
}

// RetentionResult reports rows removed without exposing their content.
type RetentionResult struct {
	ExpiredOutputChunks  uint64
	OversizeOutputChunks uint64
}

// DefaultRetentionPolicy returns the bounded local verbose-output policy.
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		VerboseOutputMaxAge:       30 * 24 * time.Hour,
		MaxVerboseBytesPerCommand: 8 * 1024 * 1024,
	}
}

// Diagnose performs path-free database health, size, schema, and history checks.
func (database *Database) Diagnose(ctx context.Context) (Diagnostics, error) {
	health, err := database.CheckHealth(ctx)
	if err != nil {
		return Diagnostics{}, err
	}
	diagnostics := Diagnostics{
		Health:                 health,
		SupportedSchemaVersion: migrations.LatestVersion(),
	}
	diagnostics.DatabaseBytes, err = fileSize(database.path)
	if err != nil {
		return Diagnostics{}, err
	}
	diagnostics.WALBytes, err = optionalFileSize(database.path + "-wal")
	if err != nil {
		return Diagnostics{}, err
	}
	diagnostics.SharedMemoryBytes, err = optionalFileSize(database.path + "-shm")
	if err != nil {
		return Diagnostics{}, err
	}
	diagnostics.TotalSQLiteBytes = diagnostics.DatabaseBytes +
		diagnostics.WALBytes +
		diagnostics.SharedMemoryBytes
	diagnostics.SchemaVersion, err = database.currentSchemaVersion(ctx)
	if err != nil {
		return Diagnostics{}, err
	}
	if err := database.sql.QueryRowContext(
		ctx,
		`SELECT
			coalesce(sum(result = 'succeeded'), 0),
			coalesce(sum(result = 'failed'), 0)
		 FROM codeflux_migration_history`,
	).Scan(
		&diagnostics.SuccessfulMigrations,
		&diagnostics.FailedMigrations,
	); err != nil {
		return Diagnostics{}, classify("read migration diagnostics", err)
	}
	return diagnostics, nil
}

// IntegrityCheck executes SQLite's full integrity check.
func (database *Database) IntegrityCheck(ctx context.Context) error {
	return database.integrityCheck(ctx)
}

// CheckpointWAL moves committed WAL frames into the main database. Truncate is
// appropriate during shutdown after new mutations have stopped.
func (database *Database) CheckpointWAL(
	ctx context.Context,
	truncate bool,
) (WALCheckpoint, error) {
	mode := "PASSIVE"
	if truncate {
		mode = "TRUNCATE"
	}
	var (
		busy         int
		logFrames    int
		checkpointed int
	)
	if err := database.sql.QueryRowContext(
		ctx,
		"PRAGMA wal_checkpoint("+mode+")",
	).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return WALCheckpoint{}, classify("checkpoint SQLite WAL", err)
	}
	return WALCheckpoint{
		Busy:               busy != 0,
		LogFrames:          logFrames,
		CheckpointedFrames: checkpointed,
	}, nil
}

// ApplyRetention removes expired and over-cap redacted output chunks.
func (database *Database) ApplyRetention(
	ctx context.Context,
	policy RetentionPolicy,
	now time.Time,
) (RetentionResult, error) {
	if policy.VerboseOutputMaxAge <= 0 {
		return RetentionResult{}, errors.New("verbose output maximum age must be positive")
	}
	if policy.MaxVerboseBytesPerCommand == 0 ||
		policy.MaxVerboseBytesPerCommand > uint64(^uint64(0)>>1) {
		return RetentionResult{}, errors.New("verbose output byte cap is invalid")
	}
	cutoff := now.UTC().Add(-policy.VerboseOutputMaxAge).UnixMicro()
	var retained RetentionResult
	err := database.RunInTransaction(ctx, func(transaction *Transaction) error {
		result, err := transaction.sql.ExecContext(
			ctx,
			`DELETE FROM redacted_outputs WHERE created_at_unix_micros < ?`,
			cutoff,
		)
		if err != nil {
			return repositoryWriteError("expire verbose output", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		retained.ExpiredOutputChunks = uint64(count)
		result, err = transaction.sql.ExecContext(
			ctx,
			`DELETE FROM redacted_outputs
			 WHERE id IN (
				SELECT id
				FROM (
					SELECT id,
					       sum(byte_count) OVER (
							PARTITION BY command_execution_id
							ORDER BY sequence DESC
					       ) AS newest_bytes
					FROM redacted_outputs
				)
				WHERE newest_bytes > ?
			 )`,
			policy.MaxVerboseBytesPerCommand,
		)
		if err != nil {
			return repositoryWriteError("cap verbose output", err)
		}
		count, err = result.RowsAffected()
		if err != nil {
			return err
		}
		retained.OversizeOutputChunks = uint64(count)
		return nil
	})
	return retained, err
}

// ValidateNoOrphans requires every declared ownership relation to resolve.
// Future graph and vector migrations must use foreign keys to join this check.
func (database *Database) ValidateNoOrphans(ctx context.Context) error {
	rows, err := database.sql.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return classify("check database ownership lineage", err)
	}
	defer rows.Close()
	if rows.Next() {
		return typedError(
			ErrConstraint,
			"check database ownership lineage",
			errors.New("foreign-key orphan exists"),
		)
	}
	if err := rows.Err(); err != nil {
		return classify("iterate database ownership lineage", err)
	}
	return nil
}

func fileSize(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, classify("inspect database size", err)
	}
	return uint64(info.Size()), nil
}

func optionalFileSize(path string) (uint64, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, classify("inspect SQLite sidecar size", err)
	}
	return uint64(info.Size()), nil
}
