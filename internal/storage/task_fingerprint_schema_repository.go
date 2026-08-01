// Package storage: the task-fingerprint schema-version registry (M21-023).
// The registry only tracks which fingerprint-schema version numbers exist
// so applicability predicates and retrieval logs can bind to one; the
// fingerprint field layout itself belongs to the M21-051..063 lane. See
// docs/plan.md §31 "Versioned Task Fingerprints".
package storage

import (
	"context"
	"database/sql"
	"errors"
)

// TaskFingerprintSchemaVersion is one registered fingerprint-schema
// version.
type TaskFingerprintSchemaVersion struct {
	Version            int
	Description        string
	IntroducedAtMicros int64
}

// GetTaskFingerprintSchemaVersion reads one registered schema version.
func (repositories *Repositories) GetTaskFingerprintSchemaVersion(
	ctx context.Context,
	version int,
) (TaskFingerprintSchemaVersion, error) {
	if version < 1 {
		return TaskFingerprintSchemaVersion{}, errors.New("task fingerprint schema version must be at least 1")
	}
	var record TaskFingerprintSchemaVersion
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT version, description, introduced_at_unix_micros FROM task_fingerprint_schema_versions WHERE version = ?`,
		version,
	).Scan(&record.Version, &record.Description, &record.IntroducedAtMicros)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskFingerprintSchemaVersion{}, typedError(ErrNotFound, "get task fingerprint schema version", err)
	}
	if err != nil {
		return TaskFingerprintSchemaVersion{}, classify("get task fingerprint schema version", err)
	}
	return record, nil
}

// ListTaskFingerprintSchemaVersions reads every registered schema version,
// ascending.
func (repositories *Repositories) ListTaskFingerprintSchemaVersions(
	ctx context.Context,
) ([]TaskFingerprintSchemaVersion, error) {
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT version, description, introduced_at_unix_micros FROM task_fingerprint_schema_versions ORDER BY version`,
	)
	if err != nil {
		return nil, classify("list task fingerprint schema versions", err)
	}
	defer rows.Close()
	var records []TaskFingerprintSchemaVersion
	for rows.Next() {
		var record TaskFingerprintSchemaVersion
		if err := rows.Scan(&record.Version, &record.Description, &record.IntroducedAtMicros); err != nil {
			return nil, classify("scan task fingerprint schema version", err)
		}
		records = append(records, record)
	}
	return records, classify("list task fingerprint schema versions", rows.Err())
}
