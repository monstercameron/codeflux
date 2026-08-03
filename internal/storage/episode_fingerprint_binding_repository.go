// Package storage: the exact task fingerprint carry (MEM-001). See
// migrations/000035_episode_lifecycle_wiring.sql's doc comment on
// task_exact_fingerprint_bindings for why this table exists: the exact
// fingerprint (schema version + hash) OpenEpisode needs is computed once,
// long before a run -- and so long before an episode -- exists, by
// TaskPreflightService.ForecastTask, from inputs
// (internal/coordinator/task_intake.go's AffectedPaths, AffectedPackages,
// AffectedSymbols, ToolchainBindings, RequestedAuthority) that are never
// persisted onto the task row itself and so cannot be recomputed at
// episode-open time. This file is the immutable carry between the two.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// TaskExactFingerprintBinding is one task's immutable exact-fingerprint
// identity, recorded once at forecast time.
type TaskExactFingerprintBinding struct {
	TaskID                   domain.TaskID
	FingerprintSchemaVersion uint32
	FingerprintHash          string
	RecordedAt               time.Time
}

// RecordTaskExactFingerprintBinding declares one input record for
// (*Repositories).RecordTaskExactFingerprintBinding.
type RecordTaskExactFingerprintBinding struct {
	TaskID                   domain.TaskID
	FingerprintSchemaVersion uint32
	FingerprintHash          string
}

// RecordTaskExactFingerprintBinding persists taskID's exact fingerprint the
// first time it is computed, or confirms an identical retry. A task's exact
// fingerprint does not change across its lifetime (internal/fingerprint.
// ExactFingerprint binds the base revision and the task's declared shape at
// intake), so a second call naming a DIFFERENT schema version or hash for a
// task that already has a binding is a typed conflict rather than a silent
// overwrite -- the storage-layer immutability triggers on
// task_exact_fingerprint_bindings would refuse the UPDATE this function
// deliberately never attempts.
func (repositories *Repositories) RecordTaskExactFingerprintBinding(
	ctx context.Context,
	input RecordTaskExactFingerprintBinding,
) (TaskExactFingerprintBinding, error) {
	switch {
	case input.TaskID.IsZero():
		return TaskExactFingerprintBinding{}, errors.New("task ID must not be empty")
	case input.FingerprintSchemaVersion == 0:
		return TaskExactFingerprintBinding{}, errors.New("fingerprint schema version must not be zero")
	}
	if !validSHA256(input.FingerprintHash) {
		return TaskExactFingerprintBinding{}, errors.New("fingerprint hash must be a lowercase hex sha256 digest")
	}
	now, micros := repositories.timestamp()
	var binding TaskExactFingerprintBinding
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findTaskExactFingerprintBinding(ctx, transaction.sql, input.TaskID)
		if err != nil {
			return err
		}
		if found {
			if existing.FingerprintSchemaVersion != input.FingerprintSchemaVersion ||
				existing.FingerprintHash != input.FingerprintHash {
				return typedError(ErrConflict, "record task exact fingerprint binding",
					errors.New("task already has a different exact fingerprint binding"))
			}
			binding = existing
			return nil
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO task_exact_fingerprint_bindings (
				task_id, fingerprint_schema_version, fingerprint_hash, recorded_at_unix_micros
			) VALUES (?, ?, ?, ?)`,
			input.TaskID, input.FingerprintSchemaVersion, input.FingerprintHash, micros,
		); err != nil {
			return repositoryWriteError("record task exact fingerprint binding", err)
		}
		binding = TaskExactFingerprintBinding{
			TaskID: input.TaskID, FingerprintSchemaVersion: input.FingerprintSchemaVersion,
			FingerprintHash: input.FingerprintHash, RecordedAt: now,
		}
		return nil
	})
	if err != nil {
		return TaskExactFingerprintBinding{}, err
	}
	return binding, nil
}

// GetTaskExactFingerprintBinding reads taskID's exact fingerprint binding,
// if one was ever recorded.
func (repositories *Repositories) GetTaskExactFingerprintBinding(
	ctx context.Context,
	taskID domain.TaskID,
) (TaskExactFingerprintBinding, error) {
	if taskID.IsZero() {
		return TaskExactFingerprintBinding{}, errors.New("task ID must not be empty")
	}
	binding, found, err := findTaskExactFingerprintBinding(ctx, repositories.database.sql, taskID)
	if err != nil {
		return TaskExactFingerprintBinding{}, err
	}
	if !found {
		return TaskExactFingerprintBinding{}, typedError(ErrNotFound, "get task exact fingerprint binding",
			errors.New("task has no recorded exact fingerprint binding"))
	}
	return binding, nil
}

func findTaskExactFingerprintBinding(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
) (TaskExactFingerprintBinding, bool, error) {
	var (
		binding                  TaskExactFingerprintBinding
		fingerprintSchemaVersion int64
		recordedMicros           int64
	)
	err := queries.QueryRowContext(
		ctx,
		`SELECT task_id, fingerprint_schema_version, fingerprint_hash, recorded_at_unix_micros
		 FROM task_exact_fingerprint_bindings WHERE task_id = ?`,
		taskID,
	).Scan(&binding.TaskID, &fingerprintSchemaVersion, &binding.FingerprintHash, &recordedMicros)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskExactFingerprintBinding{}, false, nil
	}
	if err != nil {
		return TaskExactFingerprintBinding{}, false, classify("find task exact fingerprint binding", err)
	}
	binding.FingerprintSchemaVersion = uint32(fingerprintSchemaVersion)
	binding.RecordedAt = repositoryTime(recordedMicros)
	return binding, true, nil
}
