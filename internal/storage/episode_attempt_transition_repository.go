// Package storage: episode attempt transitions (MEM-002). See
// migrations/000035_episode_lifecycle_wiring.sql for the schema this file
// persists into, and that migration's doc comment for why this is not named
// "episode action events": that phrase already denotes the pre-existing
// M21-032 mechanism (*Repositories).ListEpisodeActionEvents reads from the
// task_events journal.
//
// Before this, the gate that evaluated an attempt, the failure that sent it
// back, the rung it ran on, and what happened next (sent back on the same
// rung, escalated, decomposed, stopped for approval, or converged) existed
// only as narrated chat strings inside internal/coordinator/agent_execution.
// go's sendBack closure -- readable by a person watching the session, but
// not a fact any extractor could read from the store. This file is where
// those facts become durable, append-only rows.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// EpisodeAttemptOutcome classifies what happened after one attempt was
// evaluated against its gate.
type EpisodeAttemptOutcome string

const (
	// EpisodeAttemptOutcomeSentBack: the attempt failed its gate and the
	// next attempt runs on the same rung.
	EpisodeAttemptOutcomeSentBack EpisodeAttemptOutcome = "sent-back"
	// EpisodeAttemptOutcomeEscalated: the attempt failed and the run moved
	// up its model ladder for the next attempt.
	EpisodeAttemptOutcomeEscalated EpisodeAttemptOutcome = "escalated"
	// EpisodeAttemptOutcomeDecomposed: the attempt failed and the run is
	// being broken into smaller pieces rather than retried as-is.
	EpisodeAttemptOutcomeDecomposed EpisodeAttemptOutcome = "decomposed"
	// EpisodeAttemptOutcomeAwaitingApproval: the attempt failed and the
	// ladder's next rung needs an approval this run will not take on its
	// own; the run stops rather than continuing unattended.
	EpisodeAttemptOutcomeAwaitingApproval EpisodeAttemptOutcome = "awaiting-approval"
	// EpisodeAttemptOutcomeConverged: the attempt satisfied its gate and
	// the run's own attempt loop ended.
	EpisodeAttemptOutcomeConverged EpisodeAttemptOutcome = "converged"
)

var allEpisodeAttemptOutcomes = []EpisodeAttemptOutcome{
	EpisodeAttemptOutcomeSentBack, EpisodeAttemptOutcomeEscalated, EpisodeAttemptOutcomeDecomposed,
	EpisodeAttemptOutcomeAwaitingApproval, EpisodeAttemptOutcomeConverged,
}

// IsValid reports whether the attempt outcome is one of the declared ones.
func (value EpisodeAttemptOutcome) IsValid() bool {
	for _, candidate := range allEpisodeAttemptOutcomes {
		if candidate == value {
			return true
		}
	}
	return false
}

// EpisodeAttemptTransition is one durable record of what happened to one
// attempt: the gate that evaluated it, the normalised failure that sent it
// back (nil exactly when Outcome is Converged), the rung it ran on, and the
// outcome.
type EpisodeAttemptTransition struct {
	ID                string
	EpisodeID         domain.EpisodeID
	Attempt           uint64
	Ordinal           uint64
	Gate              string
	NormalizedFailure *string
	Rung              string
	Outcome           EpisodeAttemptOutcome
	IdempotencyKey    string
	RecordedAt        time.Time
}

// RecordEpisodeAttemptTransition declares one input record for
// (*Repositories).RecordEpisodeAttemptTransition.
type RecordEpisodeAttemptTransition struct {
	ID                string
	EpisodeID         domain.EpisodeID
	Attempt           uint64
	Gate              string
	NormalizedFailure *string
	Rung              string
	Outcome           EpisodeAttemptOutcome
	IdempotencyKey    string
}

// RecordEpisodeAttemptTransition persists one attempt's transition,
// allocating the next ordinal for (episode, attempt) idempotently, or
// returning the existing row on a retried idempotency key (MEM-002). The
// storage-layer episode_attempt_transitions CHECK constraint enforces that
// NormalizedFailure is present if and only if Outcome is not Converged,
// surfaced here as ErrConstraint the same way every other episode
// sub-table's schema-level rule is.
func (repositories *Repositories) RecordEpisodeAttemptTransition(
	ctx context.Context,
	input RecordEpisodeAttemptTransition,
) (EpisodeAttemptTransition, error) {
	switch {
	case input.ID == "":
		return EpisodeAttemptTransition{}, errors.New("episode attempt transition ID must not be empty")
	case input.EpisodeID.IsZero():
		return EpisodeAttemptTransition{}, errors.New("episode ID must not be empty")
	case input.Attempt == 0:
		return EpisodeAttemptTransition{}, errors.New("attempt must be at least 1")
	}
	if err := validateBounded("episode attempt transition gate", input.Gate, 128); err != nil {
		return EpisodeAttemptTransition{}, err
	}
	if err := validateBounded("episode attempt transition rung", input.Rung, 64); err != nil {
		return EpisodeAttemptTransition{}, err
	}
	if !input.Outcome.IsValid() {
		return EpisodeAttemptTransition{}, errors.New("episode attempt transition outcome is not declared")
	}
	if err := validateBounded("episode attempt transition idempotency key", input.IdempotencyKey, 255); err != nil {
		return EpisodeAttemptTransition{}, err
	}
	if input.NormalizedFailure != nil {
		if err := validateBounded("episode attempt transition normalized failure", *input.NormalizedFailure, 2048); err != nil {
			return EpisodeAttemptTransition{}, err
		}
	}
	now, micros := repositories.timestamp()
	var record EpisodeAttemptTransition
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findEpisodeAttemptTransitionByIdempotency(ctx, transaction.sql, input.EpisodeID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID || existing.Attempt != input.Attempt || existing.Outcome != input.Outcome {
				return typedError(ErrConflict, "record episode attempt transition", errors.New("idempotency key belongs to a different attempt transition"))
			}
			record = existing
			return nil
		}
		var ordinal uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(ordinal), -1) + 1 FROM episode_attempt_transitions WHERE episode_id = ? AND attempt = ?`,
			input.EpisodeID, input.Attempt,
		).Scan(&ordinal); err != nil {
			return classify("allocate episode attempt transition ordinal", err)
		}
		var normalizedFailure any
		if input.NormalizedFailure != nil {
			normalizedFailure = *input.NormalizedFailure
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO episode_attempt_transitions (
				id, episode_id, attempt, ordinal, gate, normalized_failure, rung, outcome,
				idempotency_key, recorded_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.EpisodeID, input.Attempt, ordinal, input.Gate, normalizedFailure,
			input.Rung, input.Outcome, input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("record episode attempt transition", err)
		}
		record = EpisodeAttemptTransition{
			ID: input.ID, EpisodeID: input.EpisodeID, Attempt: input.Attempt, Ordinal: ordinal,
			Gate: input.Gate, NormalizedFailure: input.NormalizedFailure, Rung: input.Rung,
			Outcome: input.Outcome, IdempotencyKey: input.IdempotencyKey, RecordedAt: now,
		}
		return nil
	})
	if err != nil {
		return EpisodeAttemptTransition{}, err
	}
	return record, nil
}

// ListEpisodeAttemptTransitions reads every transition recorded for
// episodeID, ordered by attempt then ordinal.
func (repositories *Repositories) ListEpisodeAttemptTransitions(
	ctx context.Context,
	episodeID domain.EpisodeID,
) ([]EpisodeAttemptTransition, error) {
	if episodeID.IsZero() {
		return nil, errors.New("episode ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx, episodeAttemptTransitionSelect+` WHERE episode_id = ? ORDER BY attempt, ordinal`, episodeID,
	)
	if err != nil {
		return nil, classify("list episode attempt transitions", err)
	}
	defer rows.Close()
	return scanEpisodeAttemptTransitions(rows)
}

const episodeAttemptTransitionSelect = `SELECT
	id, episode_id, attempt, ordinal, gate, normalized_failure, rung, outcome,
	idempotency_key, recorded_at_unix_micros
 FROM episode_attempt_transitions`

func findEpisodeAttemptTransitionByIdempotency(
	ctx context.Context,
	queries interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	episodeID domain.EpisodeID,
	key string,
) (EpisodeAttemptTransition, bool, error) {
	rows, err := queries.QueryContext(
		ctx, episodeAttemptTransitionSelect+` WHERE episode_id = ? AND idempotency_key = ?`, episodeID, key,
	)
	if err != nil {
		return EpisodeAttemptTransition{}, false, classify("find episode attempt transition by idempotency", err)
	}
	defer rows.Close()
	records, err := scanEpisodeAttemptTransitions(rows)
	if err != nil {
		return EpisodeAttemptTransition{}, false, err
	}
	if len(records) == 0 {
		return EpisodeAttemptTransition{}, false, nil
	}
	return records[0], true, nil
}

func scanEpisodeAttemptTransitions(rows *sql.Rows) ([]EpisodeAttemptTransition, error) {
	var records []EpisodeAttemptTransition
	for rows.Next() {
		var (
			record            EpisodeAttemptTransition
			attempt, ordinal  int64
			normalizedFailure sql.NullString
			outcome           string
			recordedMicros    int64
		)
		if err := rows.Scan(
			&record.ID, &record.EpisodeID, &attempt, &ordinal, &record.Gate, &normalizedFailure,
			&record.Rung, &outcome, &record.IdempotencyKey, &recordedMicros,
		); err != nil {
			return nil, classify("scan episode attempt transition", err)
		}
		record.Attempt = uint64(attempt)
		record.Ordinal = uint64(ordinal)
		record.Outcome = EpisodeAttemptOutcome(outcome)
		if normalizedFailure.Valid {
			value := normalizedFailure.String
			record.NormalizedFailure = &value
		}
		record.RecordedAt = repositoryTime(recordedMicros)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list episode attempt transitions", err)
	}
	return records, nil
}
