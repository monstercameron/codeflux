// Package storage: retrieval-candidate and retrieval-decision logs
// (M21-026). Schema-backed logging only; the M21-064..077 retrieval gate
// decides what to write here. Per AGENTS.md, vector similarity never
// establishes eligibility by itself.
package storage

import (
	"context"
	"database/sql"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

// MemoryRetrievalQueryKind classifies one retrieval query.
type MemoryRetrievalQueryKind string

const (
	RetrievalQueryExactIdentity       MemoryRetrievalQueryKind = "exact-identity"
	RetrievalQueryApplicabilityFilter MemoryRetrievalQueryKind = "applicability-filter"
	RetrievalQueryVectorSimilarity    MemoryRetrievalQueryKind = "vector-similarity"
)

// MemoryRetrievalCandidateSource classifies how a candidate entered a
// retrieval result set.
type MemoryRetrievalCandidateSource string

const (
	RetrievalCandidateExactMatch        MemoryRetrievalCandidateSource = "exact-match"
	RetrievalCandidateApplicabilityPass MemoryRetrievalCandidateSource = "applicability-pass"
	RetrievalCandidateVectorSimilarity  MemoryRetrievalCandidateSource = "vector-similarity"
)

// MemoryRetrievalDecisionReason classifies why one candidate was accepted
// or rejected.
type MemoryRetrievalDecisionReason string

const (
	RetrievalReasonProjectBoundaryMismatch      MemoryRetrievalDecisionReason = "project-boundary-mismatch"
	RetrievalReasonToolchainMismatch            MemoryRetrievalDecisionReason = "toolchain-mismatch"
	RetrievalReasonDependencyMismatch           MemoryRetrievalDecisionReason = "dependency-mismatch"
	RetrievalReasonInvalidatedEvidence          MemoryRetrievalDecisionReason = "invalidated-evidence"
	RetrievalReasonAssuranceBelowRequirement    MemoryRetrievalDecisionReason = "assurance-below-requirement"
	RetrievalReasonApplicabilityPredicateFailed MemoryRetrievalDecisionReason = "applicability-predicate-failed"
	RetrievalReasonEligibleAndUsed              MemoryRetrievalDecisionReason = "eligible-and-used"
	RetrievalReasonEligibleAndAdapted           MemoryRetrievalDecisionReason = "eligible-and-adapted"
	RetrievalReasonEligibleAndRejectedByAgent   MemoryRetrievalDecisionReason = "eligible-and-rejected-by-agent"
	RetrievalReasonNoEligibleItem               MemoryRetrievalDecisionReason = "no-eligible-item"
)

// CreateMemoryRetrievalQuery declares one executed retrieval query.
type CreateMemoryRetrievalQuery struct {
	ID                       string
	ProjectID                domain.ProjectID
	TaskID                   *domain.TaskID
	FingerprintSchemaVersion int
	QueryKind                MemoryRetrievalQueryKind
}

// CreateMemoryRetrievalCandidate declares one logged candidate, discovered
// through one or more channels in the same retrieval pass (M21-137: "Record
// whether an atom was retrieved from exact identity, structured fields,
// vector similarity, or several channels"). Channels must name at least one
// distinct, declared MemoryRetrievalCandidateSource; SimilarityScore must be
// present if and only if RetrievalCandidateVectorSimilarity is among them,
// matching the CHECK constraint every individual channel row carries.
type CreateMemoryRetrievalCandidate struct {
	ID              string
	QueryID         string
	RevisionID      domain.MemoryArtifactRevisionID
	Rank            int
	Channels        []MemoryRetrievalCandidateSource
	SimilarityScore *float64
}

// CreateMemoryRetrievalDecision declares one logged accept/reject
// decision for a previously logged candidate.
type CreateMemoryRetrievalDecision struct {
	ID                   string
	CandidateID          string
	Decision             string
	ReasonKind           MemoryRetrievalDecisionReason
	ReasonDetailRedacted *string
}

// CreateMemoryRetrievalQuery persists one retrieval query.
func (repositories *Repositories) CreateMemoryRetrievalQuery(
	ctx context.Context,
	input CreateMemoryRetrievalQuery,
) error {
	switch {
	case input.ID == "":
		return errors.New("memory retrieval query ID must not be empty")
	case input.ProjectID.IsZero():
		return errors.New("memory retrieval query project ID must not be empty")
	case input.FingerprintSchemaVersion < 1:
		return errors.New("memory retrieval query fingerprint schema version must be at least 1")
	}
	_, micros := repositories.timestamp()
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_retrieval_queries (
				id, project_id, task_id, fingerprint_schema_version, query_kind, requested_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?)`,
			input.ID, input.ProjectID, nullableTaskID(input.TaskID), input.FingerprintSchemaVersion, input.QueryKind, micros,
		)
		return repositoryWriteError("create memory retrieval query", err)
	})
}

// CreateMemoryRetrievalCandidate persists one logged candidate and the full
// set of channels that discovered it (M21-137). The legacy single-valued
// candidate_source column on memory_retrieval_candidates still receives
// exactly one value -- the strongest channel present, via
// strongestCandidateSource, mirroring internal/retrievalgate.
// CollapseChannelsForStorage's same "structured fields outrank embeddings"
// ordering (docs/plan.md §31 "Structured database fields are
// authoritative. Embeddings provide candidate recall only") -- but every
// channel in input.Channels is additionally preserved, losslessly, as its
// own row in memory_retrieval_candidate_channels (migration 000028). Per
// AGENTS.md, a vector-similarity channel must always carry a similarity
// score and never implies eligibility by itself; a subsequent
// CreateMemoryRetrievalDecision row still governs whether the candidate was
// used.
func (repositories *Repositories) CreateMemoryRetrievalCandidate(
	ctx context.Context,
	input CreateMemoryRetrievalCandidate,
) error {
	switch {
	case input.ID == "":
		return errors.New("memory retrieval candidate ID must not be empty")
	case input.QueryID == "":
		return errors.New("memory retrieval candidate query ID must not be empty")
	case input.RevisionID.IsZero():
		return errors.New("memory retrieval candidate revision ID must not be empty")
	case input.Rank < 1:
		return errors.New("memory retrieval candidate rank must be at least 1")
	case len(input.Channels) == 0:
		return errors.New("memory retrieval candidate must declare at least one discovery channel")
	}
	seen := make(map[MemoryRetrievalCandidateSource]struct{}, len(input.Channels))
	hasVectorSimilarity := false
	for _, channel := range input.Channels {
		if !isValidMemoryRetrievalCandidateSource(channel) {
			return errors.New("memory retrieval candidate channel is not declared")
		}
		if _, duplicate := seen[channel]; duplicate {
			return errors.New("memory retrieval candidate must not repeat the same channel")
		}
		seen[channel] = struct{}{}
		if channel == RetrievalCandidateVectorSimilarity {
			hasVectorSimilarity = true
		}
	}
	if hasVectorSimilarity != (input.SimilarityScore != nil) {
		return errors.New("memory retrieval candidate similarity score must be present if and only if vector-similarity is one of its channels")
	}
	_, micros := repositories.timestamp()
	var score any
	if input.SimilarityScore != nil {
		score = *input.SimilarityScore
	}
	strongest := strongestCandidateSource(input.Channels)
	// memory_retrieval_candidates' own CHECK constraint (migration 000025)
	// requires similarity_score to be non-NULL if and only if the LEGACY
	// single-valued candidate_source is itself 'vector-similarity' -- not
	// merely present somewhere in the full channel set. When a stronger
	// channel (exact-match or applicability-pass) also discovered this
	// candidate, the legacy column legitimately carries that stronger
	// value with a NULL score; the real score is never lost, because it is
	// still recorded on the vector-similarity row in
	// memory_retrieval_candidate_channels below.
	var legacyScore any
	if strongest == RetrievalCandidateVectorSimilarity {
		legacyScore = score
	}
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_retrieval_candidates (
				id, query_id, revision_id, rank, candidate_source, similarity_score, created_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.QueryID, input.RevisionID, input.Rank, strongest, legacyScore, micros,
		); err != nil {
			return repositoryWriteError("create memory retrieval candidate", err)
		}
		for _, channel := range input.Channels {
			var channelScore any
			if channel == RetrievalCandidateVectorSimilarity {
				channelScore = score
			}
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO memory_retrieval_candidate_channels (
					candidate_id, channel, similarity_score, created_at_unix_micros
				 ) VALUES (?, ?, ?, ?)`,
				input.ID, channel, channelScore, micros,
			); err != nil {
				return repositoryWriteError("create memory retrieval candidate channel", err)
			}
		}
		return nil
	})
}

// ListMemoryRetrievalCandidateChannels reads the complete, lossless
// discovery-channel set recorded for one candidate (M21-137), in the order
// they were persisted.
func (repositories *Repositories) ListMemoryRetrievalCandidateChannels(
	ctx context.Context,
	candidateID string,
) ([]MemoryRetrievalCandidateSource, error) {
	if candidateID == "" {
		return nil, errors.New("memory retrieval candidate ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT channel FROM memory_retrieval_candidate_channels
		 WHERE candidate_id = ? ORDER BY created_at_unix_micros, channel`,
		candidateID,
	)
	if err != nil {
		return nil, classify("list memory retrieval candidate channels", err)
	}
	defer rows.Close()
	var channels []MemoryRetrievalCandidateSource
	for rows.Next() {
		var channel MemoryRetrievalCandidateSource
		if err := rows.Scan(&channel); err != nil {
			return nil, classify("scan memory retrieval candidate channel", err)
		}
		channels = append(channels, channel)
	}
	return channels, classify("list memory retrieval candidate channels", rows.Err())
}

func isValidMemoryRetrievalCandidateSource(value MemoryRetrievalCandidateSource) bool {
	switch value {
	case RetrievalCandidateExactMatch, RetrievalCandidateApplicabilityPass, RetrievalCandidateVectorSimilarity:
		return true
	default:
		return false
	}
}

// candidateSourcePriority orders channels from strongest to weakest evidence
// of a real match, mirroring internal/retrievalgate's channelPriority
// (duplicated rather than imported: this package must not depend on
// internal/retrievalgate, see doc.go). Used only to pick the single legacy
// candidate_source value; it never influences an eligibility decision.
func candidateSourcePriority(channel MemoryRetrievalCandidateSource) int {
	switch channel {
	case RetrievalCandidateExactMatch:
		return 0
	case RetrievalCandidateApplicabilityPass:
		return 1
	case RetrievalCandidateVectorSimilarity:
		return 2
	default:
		return 3
	}
}

// strongestCandidateSource picks the one legacy candidate_source value for
// a candidate discovered through channels, per candidateSourcePriority.
func strongestCandidateSource(channels []MemoryRetrievalCandidateSource) MemoryRetrievalCandidateSource {
	strongest := channels[0]
	for _, channel := range channels[1:] {
		if candidateSourcePriority(channel) < candidateSourcePriority(strongest) {
			strongest = channel
		}
	}
	return strongest
}

// CreateMemoryRetrievalDecision persists one logged accept/reject decision
// for a candidate.
func (repositories *Repositories) CreateMemoryRetrievalDecision(
	ctx context.Context,
	input CreateMemoryRetrievalDecision,
) error {
	switch {
	case input.ID == "":
		return errors.New("memory retrieval decision ID must not be empty")
	case input.CandidateID == "":
		return errors.New("memory retrieval decision candidate ID must not be empty")
	case input.Decision != "accepted" && input.Decision != "rejected":
		return errors.New("memory retrieval decision must be accepted or rejected")
	}
	_, micros := repositories.timestamp()
	var detail any
	if input.ReasonDetailRedacted != nil {
		detail = *input.ReasonDetailRedacted
	}
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_retrieval_decisions (
				id, candidate_id, decision, reason_kind, reason_detail_redacted, decided_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?)`,
			input.ID, input.CandidateID, input.Decision, input.ReasonKind, detail, micros,
		)
		return repositoryWriteError("create memory retrieval decision", err)
	})
}

// MemoryRetrievalDecisionRecord is one durable, logged accept/reject
// decision for a candidate.
type MemoryRetrievalDecisionRecord struct {
	ID                   string
	CandidateID          string
	Decision             string
	ReasonKind           MemoryRetrievalDecisionReason
	ReasonDetailRedacted *string
	DecidedAtMicros      int64
}

// GetMemoryRetrievalDecision reads the decision logged for one candidate,
// if any (a candidate carries at most one decision: candidate_id is
// UNIQUE). Callers use this to confirm what was actually recorded -- for
// example, that a rejected candidate's decision never silently changes to
// an "eligible-and-*" reason merely because an unrelated candidate in the
// same query was later used.
func (repositories *Repositories) GetMemoryRetrievalDecision(
	ctx context.Context,
	candidateID string,
) (MemoryRetrievalDecisionRecord, bool, error) {
	if candidateID == "" {
		return MemoryRetrievalDecisionRecord{}, false, errors.New("memory retrieval candidate ID must not be empty")
	}
	var (
		record MemoryRetrievalDecisionRecord
		detail sql.NullString
	)
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, candidate_id, decision, reason_kind, reason_detail_redacted, decided_at_unix_micros
		 FROM memory_retrieval_decisions WHERE candidate_id = ?`,
		candidateID,
	).Scan(&record.ID, &record.CandidateID, &record.Decision, &record.ReasonKind, &detail, &record.DecidedAtMicros)
	if errors.Is(err, sql.ErrNoRows) {
		return MemoryRetrievalDecisionRecord{}, false, nil
	}
	if err != nil {
		return MemoryRetrievalDecisionRecord{}, false, classify("get memory retrieval decision", err)
	}
	if detail.Valid {
		record.ReasonDetailRedacted = &detail.String
	}
	return record, true, nil
}

// -----------------------------------------------------------------------
// M21-073, G03: read-side enumeration so a completed task's full retrieval
// history can be reconstructed after the fact ("The user can identify every
// memory item that influenced a completed task"). Every write path above
// already durably records this; these two queries are the missing read
// path that makes it queryable by TaskID rather than only by an
// already-known QueryID/CandidateID.
// -----------------------------------------------------------------------

// MemoryRetrievalQueryRecord is one durable retrieval query, read back by
// task.
type MemoryRetrievalQueryRecord struct {
	ID                       string
	ProjectID                domain.ProjectID
	TaskID                   *domain.TaskID
	FingerprintSchemaVersion int
	QueryKind                MemoryRetrievalQueryKind
	RequestedAtMicros        int64
}

// ListMemoryRetrievalQueriesByTask reads every retrieval query recorded
// against taskID, oldest first. Combined with
// ListMemoryRetrievalCandidatesForQuery and GetMemoryRetrievalDecision, this
// is the complete, storage-layer answer to "every memory item that
// influenced (or was considered for and rejected from influencing) this
// task" (G03): for each returned query, list its candidates, and for each
// candidate, read its decision. No UI or coordinator wiring surfaces this
// today; this function only proves the underlying data is durably
// reconstructable by TaskID.
func (repositories *Repositories) ListMemoryRetrievalQueriesByTask(
	ctx context.Context,
	taskID domain.TaskID,
) ([]MemoryRetrievalQueryRecord, error) {
	if taskID.IsZero() {
		return nil, errors.New("task ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, project_id, task_id, fingerprint_schema_version, query_kind, requested_at_unix_micros
		 FROM memory_retrieval_queries WHERE task_id = ? ORDER BY requested_at_unix_micros, id`,
		taskID,
	)
	if err != nil {
		return nil, classify("list memory retrieval queries by task", err)
	}
	defer rows.Close()
	var records []MemoryRetrievalQueryRecord
	for rows.Next() {
		var (
			record MemoryRetrievalQueryRecord
			task   sql.NullString
		)
		if err := rows.Scan(&record.ID, &record.ProjectID, &task, &record.FingerprintSchemaVersion, &record.QueryKind, &record.RequestedAtMicros); err != nil {
			return nil, classify("scan memory retrieval query by task", err)
		}
		if task.Valid {
			id, err := domain.ParseTaskID(task.String)
			if err != nil {
				return nil, typedError(ErrCorrupt, "scan memory retrieval query by task", err)
			}
			record.TaskID = &id
		}
		records = append(records, record)
	}
	return records, classify("list memory retrieval queries by task", rows.Err())
}

// MemoryRetrievalCandidateRecord is one durable retrieval candidate, read
// back by query.
type MemoryRetrievalCandidateRecord struct {
	ID              string
	QueryID         string
	RevisionID      domain.MemoryArtifactRevisionID
	Rank            int
	CandidateSource MemoryRetrievalCandidateSource
	SimilarityScore *float64
	CreatedAtMicros int64
}

// ListMemoryRetrievalCandidatesForQuery reads every candidate logged for
// queryID, in rank order (G03; see ListMemoryRetrievalQueriesByTask's doc
// comment for how this composes into a complete per-task retrieval-
// influence read).
func (repositories *Repositories) ListMemoryRetrievalCandidatesForQuery(
	ctx context.Context,
	queryID string,
) ([]MemoryRetrievalCandidateRecord, error) {
	if queryID == "" {
		return nil, errors.New("memory retrieval query ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, query_id, revision_id, rank, candidate_source, similarity_score, created_at_unix_micros
		 FROM memory_retrieval_candidates WHERE query_id = ? ORDER BY rank, id`,
		queryID,
	)
	if err != nil {
		return nil, classify("list memory retrieval candidates for query", err)
	}
	defer rows.Close()
	var records []MemoryRetrievalCandidateRecord
	for rows.Next() {
		var (
			record MemoryRetrievalCandidateRecord
			score  sql.NullFloat64
		)
		if err := rows.Scan(&record.ID, &record.QueryID, &record.RevisionID, &record.Rank, &record.CandidateSource, &score, &record.CreatedAtMicros); err != nil {
			return nil, classify("scan memory retrieval candidate for query", err)
		}
		if score.Valid {
			value := score.Float64
			record.SimilarityScore = &value
		}
		records = append(records, record)
	}
	return records, classify("list memory retrieval candidates for query", rows.Err())
}

func nullableTaskID(id *domain.TaskID) any {
	if id == nil {
		return nil
	}
	return *id
}

// -----------------------------------------------------------------------
// M21-076: fall back to ordinary planning when no eligible item exists
// -----------------------------------------------------------------------

// CreateMemoryRetrievalFallback declares one query-scoped record of one
// query. It never carries a candidate_id: unlike every
// memory_retrieval_decisions row, this fact is not about any one candidate
// -- it is the query-level fact that zero eligible candidates existed,
// whether because none were even discovered or because every discovered
// candidate was individually rejected for its own already-logged reason
// (M21-076: "Fall back to ordinary planning when no eligible item exists
// ... Falling back is a normal outcome, not an error, and must be recorded
// as such").
type CreateMemoryRetrievalFallback struct {
	ID                   string
	QueryID              string
	CandidatesConsidered int
}

// MemoryRetrievalFallback is one durable, query-scoped fallback record.
type MemoryRetrievalFallback struct {
	ID                   string
	QueryID              string
	CandidatesConsidered int
	RecordedAtMicros     int64
}

// CreateMemoryRetrievalFallback persists one fallback record, or returns the
// existing record on an idempotent retry for the same query (the table's
// UNIQUE query_id column makes at most one fallback record possible per
// query, matching "one retrieval pass either does or does not fall back").
// A retry that disagrees on CandidatesConsidered is rejected as a conflict
// rather than silently accepted.
func (repositories *Repositories) CreateMemoryRetrievalFallback(
	ctx context.Context,
	input CreateMemoryRetrievalFallback,
) (MemoryRetrievalFallback, error) {
	switch {
	case input.ID == "":
		return MemoryRetrievalFallback{}, errors.New("memory retrieval fallback ID must not be empty")
	case input.QueryID == "":
		return MemoryRetrievalFallback{}, errors.New("memory retrieval fallback query ID must not be empty")
	case input.CandidatesConsidered < 0:
		return MemoryRetrievalFallback{}, errors.New("memory retrieval fallback candidates-considered count must not be negative")
	}
	_, micros := repositories.timestamp()
	var record MemoryRetrievalFallback
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findMemoryRetrievalFallbackByQuery(ctx, transaction.sql, input.QueryID)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID || existing.CandidatesConsidered != input.CandidatesConsidered {
				return typedError(ErrConflict, "create memory retrieval fallback", errors.New("query already has a different fallback record"))
			}
			record = existing
			return nil
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_retrieval_fallbacks (
				id, query_id, candidates_considered, recorded_at_unix_micros
			 ) VALUES (?, ?, ?, ?)`,
			input.ID, input.QueryID, input.CandidatesConsidered, micros,
		); err != nil {
			return repositoryWriteError("create memory retrieval fallback", err)
		}
		record = MemoryRetrievalFallback{
			ID: input.ID, QueryID: input.QueryID,
			CandidatesConsidered: input.CandidatesConsidered, RecordedAtMicros: micros,
		}
		return nil
	})
	if err != nil {
		return MemoryRetrievalFallback{}, err
	}
	return record, nil
}

// GetMemoryRetrievalFallback reads the fallback record for one query, if
// any was recorded.
func (repositories *Repositories) GetMemoryRetrievalFallback(
	ctx context.Context,
	queryID string,
) (MemoryRetrievalFallback, bool, error) {
	if queryID == "" {
		return MemoryRetrievalFallback{}, false, errors.New("memory retrieval fallback query ID must not be empty")
	}
	return findMemoryRetrievalFallbackByQuery(ctx, repositories.database.sql, queryID)
}

func findMemoryRetrievalFallbackByQuery(ctx context.Context, queries queryRower, queryID string) (MemoryRetrievalFallback, bool, error) {
	var record MemoryRetrievalFallback
	err := queries.QueryRowContext(
		ctx,
		`SELECT id, query_id, candidates_considered, recorded_at_unix_micros
		 FROM memory_retrieval_fallbacks WHERE query_id = ?`,
		queryID,
	).Scan(&record.ID, &record.QueryID, &record.CandidatesConsidered, &record.RecordedAtMicros)
	if errors.Is(err, sql.ErrNoRows) {
		return MemoryRetrievalFallback{}, false, nil
	}
	if err != nil {
		return MemoryRetrievalFallback{}, false, classify("find memory retrieval fallback", err)
	}
	return record, true, nil
}
