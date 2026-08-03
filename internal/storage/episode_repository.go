// Package storage: chronological episode capture (Milestone 21,
// M21-029..039). See docs/plan.md §31 "Chronological Episodes" and
// internal/domain/episode.go for the boundary, freeze, and overlay rules
// this file persists, and migrations/000027_chronological_episodes.sql for
// the schema and triggers that mirror them.
//
// Design note (M21-032, generalized): an episode never copies another
// table's payload. Ordered actions and results already live in task_events,
// which already carries a per-task monotonic sequence; because one episode
// covers exactly one task, ListEpisodeActionEvents is a plain read over
// that existing journal, not a new link table. Requirement/plan revisions,
// forecast/actual metrics, validation/final decisions, and repair attempts
// (M21-030, M21-031, M21-033..036) already live in their own existing
// tables, all keyed by task_id; EpisodeFactReference is the one generic,
// uniform mechanism this file adds so an episode can name which row of
// those tables it is grounded in, by revision number, row identity, or
// both, without duplicating their content.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// Episode is one chronological record of a completed or terminated task
// (M21-029). EndingRevision and Outcome are nil while Status is Open and
// always present once Status is Closed (see domain.EpisodeClosure and the
// episodes table's all-or-nothing CHECK constraint).
type Episode struct {
	ID           domain.EpisodeID
	ProjectID    domain.ProjectID
	RepositoryID domain.RepositoryID
	TaskID       domain.TaskID
	// Attempt is the pipeline ledger's own attempt number this episode was
	// opened under (PIPE-003, storage.NextPipelineAttempt; MEM-003). A task
	// started twice has two episodes, one per attempt; neither inherits the
	// other's evidence.
	Attempt                  uint64
	FingerprintSchemaVersion uint32
	FingerprintHash          string
	StartingRevision         string
	EndingRevision           *string
	Status                   domain.EpisodeStatus
	Outcome                  *domain.EpisodeOutcome
	// AdvisoryExposure is the MEM-005 write-once transition: every episode
	// opens Unexposed, becomes Exposed the first time an advisory pattern
	// enters the run, and can never be relabelled Unexposed again.
	AdvisoryExposure domain.EpisodeAdvisoryExposure
	IdempotencyKey   string
	StartedAt        time.Time
	ClosedAt         *time.Time
}

// OpenEpisode declares the facts fixed at episode start (M21-029).
type OpenEpisode struct {
	ID           domain.EpisodeID
	ProjectID    domain.ProjectID
	RepositoryID domain.RepositoryID
	TaskID       domain.TaskID
	// Attempt binds this episode to the pipeline ledger's own attempt
	// number (MEM-003). The zero value defaults to 1, the same convention
	// storage.RecordPipelineStage uses for its own Attempt field, so a
	// caller opening a task's first episode does not have to know the
	// ledger's numbering to get it right.
	Attempt                  uint64
	FingerprintSchemaVersion uint32
	FingerprintHash          string
	StartingRevision         string
	IdempotencyKey           string
}

// CloseEpisode declares the facts fixed at an episode's terminal user
// decision (M21-034, M21-037). Closing is what freezes the episode
// (M21-038).
type CloseEpisode struct {
	EpisodeID      domain.EpisodeID
	EndingRevision string
	Outcome        domain.EpisodeOutcome
}

const episodeSelect = `SELECT
	id, project_id, repository_id, task_id, attempt, fingerprint_schema_version, fingerprint_hash,
	starting_revision, ending_revision, status, outcome, advisory_exposure, idempotency_key,
	started_at_unix_micros, closed_at_unix_micros
 FROM episodes`

// OpenEpisode persists a new open episode, or returns the existing episode
// on an idempotent retry. A task may have at most one episode PER ATTEMPT
// (episodes carries UNIQUE(task_id, attempt), not a bare task_id UNIQUE;
// MEM-003): retrying with the same task and attempt but a different
// idempotency key or episode ID is a conflict, never a silent second
// episode for that attempt. A task started again under a later attempt
// opens a genuinely separate episode that inherits nothing from the first.
func (repositories *Repositories) OpenEpisode(ctx context.Context, input OpenEpisode) (Episode, error) {
	if input.Attempt == 0 {
		input.Attempt = 1
	}
	if err := validateBounded("episode idempotency key", input.IdempotencyKey, 255); err != nil {
		return Episode{}, err
	}
	if err := (domain.NewEpisode{
		ID: input.ID, Project: input.ProjectID, Repository: input.RepositoryID, Task: input.TaskID,
		Attempt:                  input.Attempt,
		FingerprintSchemaVersion: input.FingerprintSchemaVersion, FingerprintHash: input.FingerprintHash,
		StartingRevision: input.StartingRevision,
	}).Validate(); err != nil {
		return Episode{}, err
	}
	now, micros := repositories.timestamp()
	var episode Episode
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findEpisodeByTaskAttempt(ctx, transaction.sql, input.TaskID, input.Attempt)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID || existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(ErrConflict, "open episode", errors.New("task already has a different episode for this attempt"))
			}
			episode = existing
			return nil
		}
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO episodes (
				id, project_id, repository_id, task_id, attempt, fingerprint_schema_version, fingerprint_hash,
				starting_revision, status, advisory_exposure, idempotency_key, started_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'open', 'unexposed', ?, ?)`,
			input.ID, input.ProjectID, input.RepositoryID, input.TaskID, input.Attempt,
			input.FingerprintSchemaVersion, input.FingerprintHash, input.StartingRevision,
			input.IdempotencyKey, micros,
		)
		if err != nil {
			return repositoryWriteError("open episode", err)
		}
		episode = Episode{
			ID: input.ID, ProjectID: input.ProjectID, RepositoryID: input.RepositoryID, TaskID: input.TaskID,
			Attempt:                  input.Attempt,
			FingerprintSchemaVersion: input.FingerprintSchemaVersion, FingerprintHash: input.FingerprintHash,
			StartingRevision: input.StartingRevision, Status: domain.EpisodeStatusOpen,
			AdvisoryExposure: domain.EpisodeAdvisoryExposureUnexposed,
			IdempotencyKey:   input.IdempotencyKey, StartedAt: now,
		}
		return nil
	})
	if err != nil {
		return Episode{}, err
	}
	return episode, nil
}

// CloseEpisode freezes an open episode with its terminal outcome
// (M21-034, M21-037, M21-038). Closing an already-closed episode is
// idempotent when the closure facts match exactly, and a typed conflict
// otherwise; either way the episode is never mutated a second time (see the
// episodes_immutable_after_close storage trigger, which this function never
// even attempts to violate: it UPDATEs only when the current row is still
// open).
func (repositories *Repositories) CloseEpisode(ctx context.Context, input CloseEpisode) (Episode, error) {
	if input.EpisodeID.IsZero() {
		return Episode{}, errors.New("episode ID must not be empty")
	}
	closure := domain.EpisodeClosure{EndingRevision: input.EndingRevision, Outcome: input.Outcome}
	if err := closure.Validate(); err != nil {
		return Episode{}, err
	}
	_, micros := repositories.timestamp()
	var episode Episode
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := scanEpisode(transaction.sql.QueryRowContext(ctx, episodeSelect+` WHERE id = ?`, input.EpisodeID), "read episode before close")
		if err != nil {
			return err
		}
		if current.Status == domain.EpisodeStatusClosed {
			if current.EndingRevision == nil || *current.EndingRevision != input.EndingRevision ||
				current.Outcome == nil || *current.Outcome != input.Outcome {
				return typedError(ErrConflict, "close episode", errors.New("episode already closed with a different outcome"))
			}
			episode = current
			return nil
		}
		if err := domain.ValidateEpisodeStatusTransition(current.Status, domain.EpisodeStatusClosed); err != nil {
			return err
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE episodes SET status = 'closed', outcome = ?, ending_revision = ?, closed_at_unix_micros = ?
			 WHERE id = ? AND status = 'open'`,
			input.Outcome, input.EndingRevision, micros, input.EpisodeID,
		)
		if err != nil {
			return repositoryWriteError("close episode", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return classify("close episode", err)
		}
		if affected != 1 {
			return typedError(ErrConflict, "close episode", errors.New("episode was closed concurrently"))
		}
		closedAt := repositoryTime(micros)
		outcome := input.Outcome
		endingRevision := input.EndingRevision
		episode = current
		episode.Status = domain.EpisodeStatusClosed
		episode.Outcome = &outcome
		episode.EndingRevision = &endingRevision
		episode.ClosedAt = &closedAt
		return nil
	})
	if err != nil {
		return Episode{}, err
	}
	return episode, nil
}

// GetEpisode reads one episode by its exact identity.
func (repositories *Repositories) GetEpisode(ctx context.Context, id domain.EpisodeID) (Episode, error) {
	if id.IsZero() {
		return Episode{}, errors.New("episode ID must not be empty")
	}
	return scanEpisode(repositories.database.sql.QueryRowContext(ctx, episodeSelect+` WHERE id = ?`, id), "get episode")
}

// GetEpisodeByTask reads the most recently opened episode for one task, if
// any (i.e. the one under the highest attempt number). MEM-003 means a task
// can now hold more than one episode, one per attempt; a caller that wants
// a specific attempt rather than the newest one calls
// GetEpisodeByTaskAttempt instead.
func (repositories *Repositories) GetEpisodeByTask(ctx context.Context, taskID domain.TaskID) (Episode, error) {
	if taskID.IsZero() {
		return Episode{}, errors.New("task ID must not be empty")
	}
	episode, err := scanEpisode(repositories.database.sql.QueryRowContext(
		ctx, episodeSelect+` WHERE task_id = ? ORDER BY attempt DESC LIMIT 1`, taskID,
	), "get episode by task")
	if errors.Is(err, ErrNotFound) {
		return Episode{}, typedError(ErrNotFound, "get episode by task", errors.New("task has no episode"))
	}
	return episode, err
}

// GetEpisodeByTaskAttempt reads the episode covering one task's specific
// attempt (MEM-003), if any.
func (repositories *Repositories) GetEpisodeByTaskAttempt(
	ctx context.Context,
	taskID domain.TaskID,
	attempt uint64,
) (Episode, error) {
	if taskID.IsZero() {
		return Episode{}, errors.New("task ID must not be empty")
	}
	if attempt == 0 {
		attempt = 1
	}
	episode, found, err := findEpisodeByTaskAttempt(ctx, repositories.database.sql, taskID, attempt)
	if err != nil {
		return Episode{}, err
	}
	if !found {
		return Episode{}, typedError(ErrNotFound, "get episode by task attempt", errors.New("task has no episode for this attempt"))
	}
	return episode, nil
}

func findEpisodeByTaskAttempt(ctx context.Context, queries queryRower, taskID domain.TaskID, attempt uint64) (Episode, bool, error) {
	episode, err := scanEpisode(queries.QueryRowContext(
		ctx, episodeSelect+` WHERE task_id = ? AND attempt = ?`, taskID, attempt,
	), "find episode by task attempt")
	if errors.Is(err, ErrNotFound) {
		return Episode{}, false, nil
	}
	return episode, err == nil, err
}

func scanEpisode(row rowScanner, operation string) (Episode, error) {
	var (
		episode                    Episode
		attempt                    int64
		fingerprintSchemaVersion   int64
		endingRevision, outcomeRaw sql.NullString
		advisoryExposure           string
		startedMicros              int64
		closedMicros               sql.NullInt64
	)
	err := row.Scan(
		&episode.ID, &episode.ProjectID, &episode.RepositoryID, &episode.TaskID, &attempt,
		&fingerprintSchemaVersion, &episode.FingerprintHash, &episode.StartingRevision,
		&endingRevision, &episode.Status, &outcomeRaw, &advisoryExposure, &episode.IdempotencyKey,
		&startedMicros, &closedMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Episode{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return Episode{}, classify(operation, err)
	}
	episode.Attempt = uint64(attempt)
	episode.FingerprintSchemaVersion = uint32(fingerprintSchemaVersion)
	episode.AdvisoryExposure = domain.EpisodeAdvisoryExposure(advisoryExposure)
	episode.StartedAt = repositoryTime(startedMicros)
	if endingRevision.Valid {
		value := endingRevision.String
		episode.EndingRevision = &value
	}
	if outcomeRaw.Valid {
		outcome := domain.EpisodeOutcome(outcomeRaw.String)
		episode.Outcome = &outcome
	}
	if closedMicros.Valid {
		closedAt := repositoryTime(closedMicros.Int64)
		episode.ClosedAt = &closedAt
	}
	return episode, nil
}

// RecordEpisodeAdvisoryExposure applies the MEM-005 write-once transition:
// an episode opens Unexposed and this is the only way it ever becomes
// Exposed. Calling it again on an already-Exposed episode is idempotent
// (domain.ValidateEpisodeAdvisoryExposureTransition treats the same target
// state as a no-op); the storage-layer episodes_advisory_exposure_write_once
// trigger is what makes the reverse -- an attempt to relabel an Exposed
// episode Unexposed -- structurally impossible, not merely refused here.
func (repositories *Repositories) RecordEpisodeAdvisoryExposure(
	ctx context.Context,
	episodeID domain.EpisodeID,
) (Episode, error) {
	if episodeID.IsZero() {
		return Episode{}, errors.New("episode ID must not be empty")
	}
	var episode Episode
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := scanEpisode(transaction.sql.QueryRowContext(
			ctx, episodeSelect+` WHERE id = ?`, episodeID,
		), "read episode before advisory exposure")
		if err != nil {
			return err
		}
		if err := domain.ValidateEpisodeAdvisoryExposureTransition(
			current.AdvisoryExposure, domain.EpisodeAdvisoryExposureExposed,
		); err != nil {
			return err
		}
		if current.AdvisoryExposure == domain.EpisodeAdvisoryExposureExposed {
			episode = current
			return nil
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE episodes SET advisory_exposure = 'exposed' WHERE id = ? AND advisory_exposure = 'unexposed'`,
			episodeID,
		)
		if err != nil {
			return repositoryWriteError("record episode advisory exposure", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return classify("record episode advisory exposure", err)
		}
		if affected != 1 {
			return typedError(ErrConflict, "record episode advisory exposure", errors.New("episode advisory exposure changed concurrently"))
		}
		current.AdvisoryExposure = domain.EpisodeAdvisoryExposureExposed
		episode = current
		return nil
	})
	if err != nil {
		return Episode{}, err
	}
	return episode, nil
}

// -----------------------------------------------------------------------
// Ordered actions and results by event reference (M21-032)
// -----------------------------------------------------------------------

// ListEpisodeActionEvents reads episodeID's ordered actions and results
// directly from the existing task_events journal, ordered by the journal's
// own monotonic per-task sequence. This never copies an event's payload
// into episode storage; episodes carries no payload column at all for this
// purpose.
func (repositories *Repositories) ListEpisodeActionEvents(ctx context.Context, episodeID domain.EpisodeID) ([]TaskEvent, error) {
	if episodeID.IsZero() {
		return nil, errors.New("episode ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT task_events.id, task_events.task_id, task_events.run_id, task_events.sequence,
		        task_events.event_type, task_events.payload_json, task_events.idempotency_key,
		        task_events.created_at_unix_micros
		 FROM task_events
		 JOIN episodes ON episodes.task_id = task_events.task_id
		 WHERE episodes.id = ?
		 ORDER BY task_events.sequence`,
		episodeID,
	)
	if err != nil {
		return nil, classify("list episode action events", err)
	}
	defer rows.Close()
	var events []TaskEvent
	for rows.Next() {
		var (
			event         TaskEvent
			runRaw        sql.NullString
			createdMicros int64
		)
		if err := rows.Scan(
			&event.ID, &event.TaskID, &runRaw, &event.Sequence, &event.EventType,
			&event.PayloadJSON, &event.IdempotencyKey, &createdMicros,
		); err != nil {
			return nil, classify("scan episode action event", err)
		}
		if runRaw.Valid {
			id, err := domain.ParseRunID(runRaw.String)
			if err != nil {
				return nil, typedError(ErrCorrupt, "parse episode action event run ID", err)
			}
			event.RunID = &id
		}
		event.CreatedAt = repositoryTime(createdMicros)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list episode action events", err)
	}
	return events, nil
}

// -----------------------------------------------------------------------
// Episode fact references (M21-030, M21-031, M21-033..036)
// -----------------------------------------------------------------------

// EpisodeFactReference is one persisted reference from an episode to an
// already-durable fact recorded elsewhere.
type EpisodeFactReference struct {
	ID             string
	EpisodeID      domain.EpisodeID
	FactTaskID     domain.TaskID
	Kind           domain.EpisodeFactKind
	Revision       *uint64
	ReferenceID    string
	Ordinal        uint64
	IdempotencyKey string
	RecordedAt     time.Time
}

// RecordEpisodeFactReference declares one input record for
// (*Repositories).RecordEpisodeFactReference.
type RecordEpisodeFactReference struct {
	ID             string
	EpisodeID      domain.EpisodeID
	FactTaskID     domain.TaskID
	Kind           domain.EpisodeFactKind
	Revision       *uint64
	ReferenceID    string
	IdempotencyKey string
}

// RecordEpisodeFactReference persists one fact reference, allocating the
// next ordinal for (episode, kind) idempotently, or returning the existing
// row on a retried idempotency key. Per M21-036, a 'repair-attempt' fact
// for an episode with no 'pre-repair-failure' fact recorded first is
// rejected by the storage-layer
// episode_fact_references_repair_requires_prior_failure trigger, which this
// function surfaces as ErrConstraint.
func (repositories *Repositories) RecordEpisodeFactReference(
	ctx context.Context,
	input RecordEpisodeFactReference,
) (EpisodeFactReference, error) {
	switch {
	case input.ID == "":
		return EpisodeFactReference{}, errors.New("episode fact reference ID must not be empty")
	case input.EpisodeID.IsZero():
		return EpisodeFactReference{}, errors.New("episode ID must not be empty")
	case input.FactTaskID.IsZero():
		return EpisodeFactReference{}, errors.New("episode fact reference task ID must not be empty")
	}
	if err := validateBounded("episode fact reference idempotency key", input.IdempotencyKey, 255); err != nil {
		return EpisodeFactReference{}, err
	}
	if err := (domain.EpisodeFactReference{Kind: input.Kind, Revision: input.Revision, ReferenceID: input.ReferenceID}).Validate(); err != nil {
		return EpisodeFactReference{}, err
	}
	now, micros := repositories.timestamp()
	var fact EpisodeFactReference
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findEpisodeFactReferenceByIdempotency(ctx, transaction.sql, input.EpisodeID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID || existing.Kind != input.Kind {
				return typedError(ErrConflict, "record episode fact reference", errors.New("idempotency key belongs to a different fact reference"))
			}
			fact = existing
			return nil
		}
		var ordinal uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(ordinal), -1) + 1 FROM episode_fact_references WHERE episode_id = ? AND fact_kind = ?`,
			input.EpisodeID, input.Kind,
		).Scan(&ordinal); err != nil {
			return classify("allocate episode fact reference ordinal", err)
		}
		var referenceID any
		if input.ReferenceID != "" {
			referenceID = input.ReferenceID
		}
		var revision any
		if input.Revision != nil {
			revision = *input.Revision
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO episode_fact_references (
				id, episode_id, fact_task_id, fact_kind, fact_revision, fact_reference_id,
				ordinal, idempotency_key, recorded_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.EpisodeID, input.FactTaskID, input.Kind, revision, referenceID,
			ordinal, input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("record episode fact reference", err)
		}
		fact = EpisodeFactReference{
			ID: input.ID, EpisodeID: input.EpisodeID, FactTaskID: input.FactTaskID, Kind: input.Kind,
			Revision: input.Revision, ReferenceID: input.ReferenceID, Ordinal: ordinal,
			IdempotencyKey: input.IdempotencyKey, RecordedAt: now,
		}
		return nil
	})
	if err != nil {
		return EpisodeFactReference{}, err
	}
	return fact, nil
}

// ListEpisodeFactReferences reads every fact reference recorded for
// episodeID, ordered by fact kind then ordinal.
func (repositories *Repositories) ListEpisodeFactReferences(
	ctx context.Context,
	episodeID domain.EpisodeID,
) ([]EpisodeFactReference, error) {
	if episodeID.IsZero() {
		return nil, errors.New("episode ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, episode_id, fact_task_id, fact_kind, fact_revision, fact_reference_id,
		        ordinal, idempotency_key, recorded_at_unix_micros
		 FROM episode_fact_references WHERE episode_id = ? ORDER BY fact_kind, ordinal`,
		episodeID,
	)
	if err != nil {
		return nil, classify("list episode fact references", err)
	}
	defer rows.Close()
	facts, err := scanEpisodeFactReferences(rows)
	if err != nil {
		return nil, err
	}
	return facts, nil
}

func findEpisodeFactReferenceByIdempotency(
	ctx context.Context,
	queries interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	episodeID domain.EpisodeID,
	key string,
) (EpisodeFactReference, bool, error) {
	rows, err := queries.QueryContext(
		ctx,
		`SELECT id, episode_id, fact_task_id, fact_kind, fact_revision, fact_reference_id,
		        ordinal, idempotency_key, recorded_at_unix_micros
		 FROM episode_fact_references WHERE episode_id = ? AND idempotency_key = ?`,
		episodeID, key,
	)
	if err != nil {
		return EpisodeFactReference{}, false, classify("find episode fact reference by idempotency", err)
	}
	defer rows.Close()
	facts, err := scanEpisodeFactReferences(rows)
	if err != nil {
		return EpisodeFactReference{}, false, err
	}
	if len(facts) == 0 {
		return EpisodeFactReference{}, false, nil
	}
	return facts[0], true, nil
}

func scanEpisodeFactReferences(rows *sql.Rows) ([]EpisodeFactReference, error) {
	var facts []EpisodeFactReference
	for rows.Next() {
		var (
			fact           EpisodeFactReference
			revisionRaw    sql.NullInt64
			referenceIDRaw sql.NullString
			recordedMicros int64
		)
		if err := rows.Scan(
			&fact.ID, &fact.EpisodeID, &fact.FactTaskID, &fact.Kind, &revisionRaw, &referenceIDRaw,
			&fact.Ordinal, &fact.IdempotencyKey, &recordedMicros,
		); err != nil {
			return nil, classify("scan episode fact reference", err)
		}
		if revisionRaw.Valid {
			value := uint64(revisionRaw.Int64)
			fact.Revision = &value
		}
		if referenceIDRaw.Valid {
			fact.ReferenceID = referenceIDRaw.String
		}
		fact.RecordedAt = repositoryTime(recordedMicros)
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list episode fact references", err)
	}
	return facts, nil
}

// -----------------------------------------------------------------------
// Invalidation overlays (M21-039)
// -----------------------------------------------------------------------

// EpisodeInvalidationOverlay is one append-only correction layered on top
// of a closed episode.
type EpisodeInvalidationOverlay struct {
	ID                 string
	EpisodeID          domain.EpisodeID
	Sequence           uint64
	Kind               domain.EpisodeInvalidationOverlayKind
	Assessment         domain.EpisodeCurrentAssessment
	ReasonRedacted     string
	ReplacementEpisode *domain.EpisodeID
	IdempotencyKey     string
	RecordedAt         time.Time
}

// RecordEpisodeInvalidationOverlay declares one input record for
// (*Repositories).RecordEpisodeInvalidationOverlay.
type RecordEpisodeInvalidationOverlay struct {
	ID                 string
	EpisodeID          domain.EpisodeID
	Kind               domain.EpisodeInvalidationOverlayKind
	Assessment         domain.EpisodeCurrentAssessment
	ReasonRedacted     string
	ReplacementEpisode *domain.EpisodeID
	IdempotencyKey     string
}

// RecordEpisodeInvalidationOverlay layers one correction on top of a closed
// episode (M21-039), never mutating the episode's own frozen row. It fails
// with the domain validation error whenever the episode is not yet closed
// (domain.ValidateEpisodeInvalidationOverlay), matching the storage-layer
// episode_invalidation_overlays_requires_closed_episode trigger.
func (repositories *Repositories) RecordEpisodeInvalidationOverlay(
	ctx context.Context,
	input RecordEpisodeInvalidationOverlay,
) (EpisodeInvalidationOverlay, error) {
	switch {
	case input.ID == "":
		return EpisodeInvalidationOverlay{}, errors.New("episode invalidation overlay ID must not be empty")
	case input.EpisodeID.IsZero():
		return EpisodeInvalidationOverlay{}, errors.New("episode ID must not be empty")
	}
	if err := validateBounded("episode invalidation overlay idempotency key", input.IdempotencyKey, 255); err != nil {
		return EpisodeInvalidationOverlay{}, err
	}
	overlay := domain.EpisodeInvalidationOverlay{
		Kind: input.Kind, Assessment: input.Assessment, ReasonRedacted: input.ReasonRedacted,
	}
	if input.ReplacementEpisode != nil {
		overlay.ReplacementEpisode = *input.ReplacementEpisode
	}
	now, micros := repositories.timestamp()
	var record EpisodeInvalidationOverlay
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		episode, err := scanEpisode(transaction.sql.QueryRowContext(ctx, episodeSelect+` WHERE id = ?`, input.EpisodeID), "read episode before overlay")
		if err != nil {
			return err
		}
		if err := domain.ValidateEpisodeInvalidationOverlay(episode.Status, overlay); err != nil {
			return err
		}
		existing, found, err := findEpisodeInvalidationOverlayByIdempotency(ctx, transaction.sql, input.EpisodeID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID || existing.Kind != input.Kind {
				return typedError(ErrConflict, "record episode invalidation overlay", errors.New("idempotency key belongs to a different overlay"))
			}
			record = existing
			return nil
		}
		var sequence uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(sequence), 0) + 1 FROM episode_invalidation_overlays WHERE episode_id = ?`,
			input.EpisodeID,
		).Scan(&sequence); err != nil {
			return classify("allocate episode invalidation overlay sequence", err)
		}
		var replacement any
		if input.ReplacementEpisode != nil {
			replacement = *input.ReplacementEpisode
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO episode_invalidation_overlays (
				id, episode_id, sequence, overlay_kind, current_assessment, reason_redacted,
				replacement_episode_id, idempotency_key, recorded_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.EpisodeID, sequence, input.Kind, input.Assessment, input.ReasonRedacted,
			replacement, input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("record episode invalidation overlay", err)
		}
		record = EpisodeInvalidationOverlay{
			ID: input.ID, EpisodeID: input.EpisodeID, Sequence: sequence, Kind: input.Kind,
			Assessment: input.Assessment, ReasonRedacted: input.ReasonRedacted,
			ReplacementEpisode: input.ReplacementEpisode, IdempotencyKey: input.IdempotencyKey, RecordedAt: now,
		}
		return nil
	})
	if err != nil {
		return EpisodeInvalidationOverlay{}, err
	}
	return record, nil
}

// ListEpisodeInvalidationOverlays reads every overlay recorded for
// episodeID, ordered oldest first.
func (repositories *Repositories) ListEpisodeInvalidationOverlays(
	ctx context.Context,
	episodeID domain.EpisodeID,
) ([]EpisodeInvalidationOverlay, error) {
	if episodeID.IsZero() {
		return nil, errors.New("episode ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		episodeInvalidationOverlaySelect+` WHERE episode_id = ? ORDER BY sequence`,
		episodeID,
	)
	if err != nil {
		return nil, classify("list episode invalidation overlays", err)
	}
	defer rows.Close()
	return scanEpisodeInvalidationOverlays(rows)
}

// GetEpisodeCurrentAssessment reads the most recently recorded overlay's
// current assessment for episodeID. found is false when no overlay has ever
// been recorded, meaning the episode's original outcome still stands
// unchallenged; it is never a substitute for "unknown" (an episode with no
// overlay is a known, reliable state, not an unexamined one).
func (repositories *Repositories) GetEpisodeCurrentAssessment(
	ctx context.Context,
	episodeID domain.EpisodeID,
) (assessment domain.EpisodeCurrentAssessment, found bool, err error) {
	if episodeID.IsZero() {
		return "", false, errors.New("episode ID must not be empty")
	}
	var value string
	scanErr := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT current_assessment FROM episode_invalidation_overlays
		 WHERE episode_id = ? ORDER BY sequence DESC LIMIT 1`,
		episodeID,
	).Scan(&value)
	if errors.Is(scanErr, sql.ErrNoRows) {
		return "", false, nil
	}
	if scanErr != nil {
		return "", false, classify("get episode current assessment", scanErr)
	}
	return domain.EpisodeCurrentAssessment(value), true, nil
}

const episodeInvalidationOverlaySelect = `SELECT
	id, episode_id, sequence, overlay_kind, current_assessment, reason_redacted,
	replacement_episode_id, idempotency_key, recorded_at_unix_micros
 FROM episode_invalidation_overlays`

func findEpisodeInvalidationOverlayByIdempotency(
	ctx context.Context,
	queries interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	episodeID domain.EpisodeID,
	key string,
) (EpisodeInvalidationOverlay, bool, error) {
	rows, err := queries.QueryContext(ctx, episodeInvalidationOverlaySelect+` WHERE episode_id = ? AND idempotency_key = ?`, episodeID, key)
	if err != nil {
		return EpisodeInvalidationOverlay{}, false, classify("find episode invalidation overlay by idempotency", err)
	}
	defer rows.Close()
	overlays, err := scanEpisodeInvalidationOverlays(rows)
	if err != nil {
		return EpisodeInvalidationOverlay{}, false, err
	}
	if len(overlays) == 0 {
		return EpisodeInvalidationOverlay{}, false, nil
	}
	return overlays[0], true, nil
}

func scanEpisodeInvalidationOverlays(rows *sql.Rows) ([]EpisodeInvalidationOverlay, error) {
	var overlays []EpisodeInvalidationOverlay
	for rows.Next() {
		var (
			overlay        EpisodeInvalidationOverlay
			replacementRaw sql.NullString
			recordedMicros int64
		)
		if err := rows.Scan(
			&overlay.ID, &overlay.EpisodeID, &overlay.Sequence, &overlay.Kind, &overlay.Assessment,
			&overlay.ReasonRedacted, &replacementRaw, &overlay.IdempotencyKey, &recordedMicros,
		); err != nil {
			return nil, classify("scan episode invalidation overlay", err)
		}
		if replacementRaw.Valid {
			id, err := domain.ParseEpisodeID(replacementRaw.String)
			if err != nil {
				return nil, typedError(ErrCorrupt, "parse episode invalidation overlay replacement", err)
			}
			overlay.ReplacementEpisode = &id
		}
		overlay.RecordedAt = repositoryTime(recordedMicros)
		overlays = append(overlays, overlay)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list episode invalidation overlays", err)
	}
	return overlays, nil
}
