package storage

import (
	"context"
	"database/sql"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

func (repositories *Repositories) CreateCheckpoint(
	ctx context.Context,
	input CreateCheckpoint,
) (Checkpoint, error) {
	switch {
	case input.ID.IsZero():
		return Checkpoint{}, errors.New("checkpoint ID must not be empty")
	case input.TaskID.IsZero():
		return Checkpoint{}, errors.New("task ID must not be empty")
	case !input.State.IsValid():
		return Checkpoint{}, errors.New("checkpoint state is invalid")
	case len(input.WorktreeDiffHash) != 64:
		return Checkpoint{}, errors.New("worktree diff hash must contain 64 characters")
	}
	if err := validateBounded("repository revision", input.RepositoryRevision, 255); err != nil {
		return Checkpoint{}, err
	}
	if err := validateBounded("checkpoint idempotency key", input.IdempotencyKey, 255); err != nil {
		return Checkpoint{}, err
	}
	now, micros := repositories.timestamp()
	checkpoint := Checkpoint{
		ID:                 input.ID,
		TaskID:             input.TaskID,
		RunID:              input.RunID,
		State:              input.State,
		RepositoryRevision: input.RepositoryRevision,
		WorktreeDiffHash:   input.WorktreeDiffHash,
		EventSequence:      input.EventSequence,
		IdempotencyKey:     input.IdempotencyKey,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findCheckpointByIdempotency(
			ctx,
			transaction,
			input.TaskID,
			input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID ||
				!sameRunID(existing.RunID, input.RunID) ||
				existing.State != input.State ||
				existing.RepositoryRevision != input.RepositoryRevision ||
				existing.WorktreeDiffHash != input.WorktreeDiffHash ||
				existing.EventSequence != input.EventSequence {
				return typedError(
					ErrConflict,
					"create idempotent checkpoint",
					errors.New("idempotency key belongs to different checkpoint"),
				)
			}
			checkpoint = existing
			return nil
		}
		if input.RunID != nil {
			if err := verifyRunBelongsToTask(
				ctx,
				transaction,
				*input.RunID,
				input.TaskID,
				"create checkpoint",
			); err != nil {
				return err
			}
		}
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO checkpoints (
				id, task_id, run_id, state, repository_revision,
				worktree_diff_hash, event_sequence, idempotency_key,
				created_at_unix_micros, updated_at_unix_micros, revision
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			input.ID,
			input.TaskID,
			nullableRunID(input.RunID),
			input.State,
			input.RepositoryRevision,
			input.WorktreeDiffHash,
			input.EventSequence,
			input.IdempotencyKey,
			micros,
			micros,
		)
		return repositoryWriteError("create checkpoint", err)
	})
	return checkpoint, err
}

func (repositories *Repositories) GetCheckpoint(
	ctx context.Context,
	id domain.CheckpointID,
) (Checkpoint, error) {
	if id.IsZero() {
		return Checkpoint{}, errors.New("checkpoint ID must not be empty")
	}
	return scanCheckpoint(repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, task_id, run_id, state, repository_revision,
		        worktree_diff_hash, event_sequence, idempotency_key,
		        created_at_unix_micros, updated_at_unix_micros, revision
		 FROM checkpoints
		 WHERE id = ?`,
		id,
	), "get checkpoint")
}

func (repositories *Repositories) CreateValidation(
	ctx context.Context,
	input CreateValidation,
) (Validation, error) {
	switch {
	case input.ID.IsZero():
		return Validation{}, errors.New("validation ID must not be empty")
	case input.TaskID.IsZero():
		return Validation{}, errors.New("task ID must not be empty")
	case !input.State.IsValid():
		return Validation{}, errors.New("validation state is invalid")
	case !input.Severity.IsValid():
		return Validation{}, errors.New("validation severity is invalid")
	}
	if err := validateBounded("validation profile", input.ProfileName, 255); err != nil {
		return Validation{}, err
	}
	now, micros := repositories.timestamp()
	validation := Validation{
		ID:              input.ID,
		TaskID:          input.TaskID,
		RunID:           input.RunID,
		ArtifactID:      input.ArtifactID,
		State:           input.State,
		Severity:        input.Severity,
		ProfileName:     input.ProfileName,
		SummaryRedacted: input.SummaryRedacted,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if input.RunID != nil {
			if err := verifyRunBelongsToTask(
				ctx,
				transaction,
				*input.RunID,
				input.TaskID,
				"create validation",
			); err != nil {
				return err
			}
		}
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO validations (
				id, task_id, run_id, artifact_id, state, severity,
				profile_name, summary_redacted, created_at_unix_micros,
				updated_at_unix_micros, revision
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			input.ID,
			input.TaskID,
			nullableRunID(input.RunID),
			nullableArtifactID(input.ArtifactID),
			input.State,
			input.Severity,
			input.ProfileName,
			nullableString(input.SummaryRedacted),
			micros,
			micros,
		)
		return repositoryWriteError("create validation", err)
	})
	return validation, err
}

func (repositories *Repositories) CreateEvidence(
	ctx context.Context,
	input CreateEvidence,
) (Evidence, error) {
	switch {
	case input.ID.IsZero():
		return Evidence{}, errors.New("evidence ID must not be empty")
	case input.ValidationID.IsZero():
		return Evidence{}, errors.New("validation ID must not be empty")
	case input.TaskID.IsZero():
		return Evidence{}, errors.New("task ID must not be empty")
	case !input.AssuranceLevel.IsValid():
		return Evidence{}, errors.New("assurance level is invalid")
	case len(input.ContentHash) != 64:
		return Evidence{}, errors.New("evidence content hash must contain 64 characters")
	}
	if err := validateBounded("evidence type", input.EvidenceType, 255); err != nil {
		return Evidence{}, err
	}
	now, micros := repositories.timestamp()
	evidence := Evidence{
		ID:              input.ID,
		ValidationID:    input.ValidationID,
		TaskID:          input.TaskID,
		ArtifactID:      input.ArtifactID,
		AssuranceLevel:  input.AssuranceLevel,
		EvidenceType:    input.EvidenceType,
		ContentHash:     input.ContentHash,
		SummaryRedacted: input.SummaryRedacted,
		CreatedAt:       now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var validationTask domain.TaskID
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT task_id FROM validations WHERE id = ?`,
			input.ValidationID,
		).Scan(&validationTask); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return typedError(ErrConstraint, "create evidence", err)
			}
			return classify("verify evidence validation", err)
		}
		if validationTask != input.TaskID {
			return typedError(
				ErrConstraint,
				"create evidence",
				errors.New("validation does not belong to task"),
			)
		}
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO evidence (
				id, validation_id, task_id, artifact_id, assurance_level,
				evidence_type, content_hash, summary_redacted,
				created_at_unix_micros, revision
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			input.ID,
			input.ValidationID,
			input.TaskID,
			nullableArtifactID(input.ArtifactID),
			input.AssuranceLevel,
			input.EvidenceType,
			input.ContentHash,
			input.SummaryRedacted,
			micros,
		)
		return repositoryWriteError("create evidence", err)
	})
	return evidence, err
}

func findCheckpointByIdempotency(
	ctx context.Context,
	transaction *Transaction,
	taskID domain.TaskID,
	key string,
) (Checkpoint, bool, error) {
	var (
		checkpoint    Checkpoint
		runRaw        sql.NullString
		createdMicros int64
		updatedMicros int64
	)
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT id, task_id, run_id, state, repository_revision,
		        worktree_diff_hash, event_sequence, idempotency_key,
		        created_at_unix_micros, updated_at_unix_micros, revision
		 FROM checkpoints WHERE task_id = ? AND idempotency_key = ?`,
		taskID,
		key,
	).Scan(
		&checkpoint.ID,
		&checkpoint.TaskID,
		&runRaw,
		&checkpoint.State,
		&checkpoint.RepositoryRevision,
		&checkpoint.WorktreeDiffHash,
		&checkpoint.EventSequence,
		&checkpoint.IdempotencyKey,
		&createdMicros,
		&updatedMicros,
		&checkpoint.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, classify("find idempotent checkpoint", err)
	}
	if runRaw.Valid {
		id, err := domain.ParseRunID(runRaw.String)
		if err != nil {
			return Checkpoint{}, false, typedError(ErrCorrupt, "parse checkpoint run", err)
		}
		checkpoint.RunID = &id
	}
	checkpoint.CreatedAt = repositoryTime(createdMicros)
	checkpoint.UpdatedAt = repositoryTime(updatedMicros)
	return checkpoint, true, nil
}

func scanCheckpoint(
	row rowScanner,
	operation string,
) (Checkpoint, error) {
	var (
		checkpoint    Checkpoint
		runRaw        sql.NullString
		createdMicros int64
		updatedMicros int64
	)
	err := row.Scan(
		&checkpoint.ID,
		&checkpoint.TaskID,
		&runRaw,
		&checkpoint.State,
		&checkpoint.RepositoryRevision,
		&checkpoint.WorktreeDiffHash,
		&checkpoint.EventSequence,
		&checkpoint.IdempotencyKey,
		&createdMicros,
		&updatedMicros,
		&checkpoint.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return Checkpoint{}, classify(operation, err)
	}
	if runRaw.Valid {
		id, err := domain.ParseRunID(runRaw.String)
		if err != nil {
			return Checkpoint{}, classify(operation, err)
		}
		checkpoint.RunID = &id
	}
	checkpoint.CreatedAt = repositoryTime(createdMicros)
	checkpoint.UpdatedAt = repositoryTime(updatedMicros)
	return checkpoint, nil
}

func nullableArtifactID(id *domain.ArtifactID) any {
	if id == nil {
		return nil
	}
	return *id
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
