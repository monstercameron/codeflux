// Package storage: the M21-078 "measure deterministic retrieval misses
// before enabling embeddings" instrument, and the companion M21-078/§30
// "exact reuse" measurement that already-recorded retrieval data supports
// without any new schema.
//
// docs/plan.md §0 "Branch Points and Stop Gates" keeps vector discovery
// closed "unless deterministic retrieval has a measured recall problem."
// memory_retrieval_fallbacks (migration 000028) already durably records
// WHEN the exact/structured channels returned zero eligible candidates, but
// a fallback alone cannot say whether a genuinely reusable artifact existed
// and was missed, or whether none existed and falling back was simply
// correct. Only a human reviewer, later, can supply that verdict --
// automatically inferring it would require the very similarity search this
// measurement exists to justify (or not) in the first place. This file is
// the DURABLE RECORD and BOUNDED REPORT for that reviewer verdict
// (MeasureDeterministicRetrievalRecall); it is the measurement instrument,
// not the branch decision, and it never fabricates a rate when no review
// has happened yet.
//
// MeasureDeterministicRetrievalReuse is the separate, already-fully-derivable
// measurement docs/plan.md §30 "Exact Reuse Failure" needs: "meaningful
// reuse occurs in fewer than twenty percent of eligible tasks" is computed
// directly from memory_retrieval_queries/candidates/decisions, which
// M21-064..077's pre-work retrieval gate already writes durably; no new
// schema is required for it.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// -----------------------------------------------------------------------
// M21-078: deterministic-retrieval-recall review (the human verdict half)
// -----------------------------------------------------------------------

// MemoryRetrievalRecallVerdict classifies one human reviewer's answer to
// "did a genuinely reusable artifact exist for this fallen-back query?"
type MemoryRetrievalRecallVerdict string

const (
	// MemoryRetrievalRecallVerdictGenuineMiss: a reusable artifact existed
	// and the exact/structured channels failed to surface it as eligible.
	// This is the signal docs/plan.md §0 branch 1 ("a measured recall
	// problem") is watching for.
	MemoryRetrievalRecallVerdictGenuineMiss MemoryRetrievalRecallVerdict = "genuine-miss"
	// MemoryRetrievalRecallVerdictNoReusableArtifactExisted: falling back
	// was the correct outcome; nothing reusable existed yet.
	MemoryRetrievalRecallVerdictNoReusableArtifactExisted MemoryRetrievalRecallVerdict = "no-reusable-artifact-existed"
	// MemoryRetrievalRecallVerdictInconclusive: the reviewer could not
	// determine either way. Counted in ReviewedFallbacksInWindow but never
	// in GenuineMissesInWindow, so an inconclusive review can never quietly
	// inflate the measured miss rate.
	MemoryRetrievalRecallVerdictInconclusive MemoryRetrievalRecallVerdict = "inconclusive"
)

func (verdict MemoryRetrievalRecallVerdict) isValid() bool {
	switch verdict {
	case MemoryRetrievalRecallVerdictGenuineMiss, MemoryRetrievalRecallVerdictNoReusableArtifactExisted, MemoryRetrievalRecallVerdictInconclusive:
		return true
	default:
		return false
	}
}

// CreateMemoryRetrievalRecallReview declares one human reviewer's durable
// verdict for a query that already fell back (M21-078). ReviewerIdentityRedacted
// must name a real human reviewer (a redacted handle, never blank); there is
// no agent-self-report option, matching AGENTS.md's prohibition on the
// dependency "agent self-report -> accepted outcome".
type CreateMemoryRetrievalRecallReview struct {
	ID                       string
	QueryID                  string
	Verdict                  MemoryRetrievalRecallVerdict
	MissedArtifactReference  string
	ReviewerIdentityRedacted string
	DetailRedacted           string
}

// MemoryRetrievalRecallReview is one durable, immutable reviewer verdict.
type MemoryRetrievalRecallReview struct {
	ID                       string
	QueryID                  string
	Verdict                  MemoryRetrievalRecallVerdict
	MissedArtifactReference  *string
	ReviewerIdentityRedacted string
	DetailRedacted           string
	RecordedAtMicros         int64
}

// ErrMemoryRetrievalRecallReviewRequiresFallback classifies an attempt to
// record a review for a query that never fell back (M21-078 is scoped
// exactly to "the exact/structured channels return nothing eligible"
// queries). The migration's own trigger enforces this too; this Go-layer
// check exists to give the caller a clear, typed, pre-transaction error
// rather than a raw constraint failure.
var ErrMemoryRetrievalRecallReviewRequiresFallback = errors.New("deterministic retrieval recall review requires the query to have already recorded a fallback")

// CreateMemoryRetrievalRecallReview persists one reviewer verdict.
func (repositories *Repositories) CreateMemoryRetrievalRecallReview(
	ctx context.Context,
	input CreateMemoryRetrievalRecallReview,
) (MemoryRetrievalRecallReview, error) {
	switch {
	case input.ID == "":
		return MemoryRetrievalRecallReview{}, errors.New("memory retrieval recall review ID must not be empty")
	case input.QueryID == "":
		return MemoryRetrievalRecallReview{}, errors.New("memory retrieval recall review query ID must not be empty")
	case !input.Verdict.isValid():
		return MemoryRetrievalRecallReview{}, errors.New("memory retrieval recall review verdict is not declared")
	}
	if err := validateBounded("memory retrieval recall review reviewer identity", input.ReviewerIdentityRedacted, 255); err != nil {
		return MemoryRetrievalRecallReview{}, err
	}
	if err := validateBounded("memory retrieval recall review detail", input.DetailRedacted, 2048); err != nil {
		return MemoryRetrievalRecallReview{}, err
	}
	hasReference := strings.TrimSpace(input.MissedArtifactReference) != ""
	if input.Verdict == MemoryRetrievalRecallVerdictGenuineMiss && !hasReference {
		return MemoryRetrievalRecallReview{}, errors.New("a genuine-miss verdict requires a missed-artifact reference")
	}
	if input.Verdict != MemoryRetrievalRecallVerdictGenuineMiss && hasReference {
		return MemoryRetrievalRecallReview{}, errors.New("a missed-artifact reference is only meaningful for a genuine-miss verdict")
	}
	if len(input.MissedArtifactReference) > 512 {
		return MemoryRetrievalRecallReview{}, errors.New("memory retrieval recall review missed-artifact reference exceeds the maximum bound of 512 bytes")
	}

	_, found, err := findMemoryRetrievalFallbackByQuery(ctx, repositories.database.sql, input.QueryID)
	if err != nil {
		return MemoryRetrievalRecallReview{}, err
	}
	if !found {
		return MemoryRetrievalRecallReview{}, fmt.Errorf("%w: query %s", ErrMemoryRetrievalRecallReviewRequiresFallback, input.QueryID)
	}

	_, micros := repositories.timestamp()
	var reference any
	if hasReference {
		reference = input.MissedArtifactReference
	}
	review := MemoryRetrievalRecallReview{
		ID: input.ID, QueryID: input.QueryID, Verdict: input.Verdict,
		ReviewerIdentityRedacted: input.ReviewerIdentityRedacted, DetailRedacted: input.DetailRedacted,
		RecordedAtMicros: micros,
	}
	if hasReference {
		value := input.MissedArtifactReference
		review.MissedArtifactReference = &value
	}
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_retrieval_recall_reviews (
				id, query_id, verdict, missed_artifact_reference,
				reviewer_identity_redacted, detail_redacted, recorded_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.QueryID, input.Verdict, reference,
			input.ReviewerIdentityRedacted, input.DetailRedacted, micros,
		)
		return repositoryWriteError("create memory retrieval recall review", err)
	})
	if err != nil {
		return MemoryRetrievalRecallReview{}, err
	}
	return review, nil
}

// GetMemoryRetrievalRecallReview reads the review recorded for one query, if
// any (a query carries at most one review: query_id is UNIQUE).
func (repositories *Repositories) GetMemoryRetrievalRecallReview(
	ctx context.Context,
	queryID string,
) (MemoryRetrievalRecallReview, bool, error) {
	if queryID == "" {
		return MemoryRetrievalRecallReview{}, false, errors.New("memory retrieval recall review query ID must not be empty")
	}
	var (
		review    MemoryRetrievalRecallReview
		reference sql.NullString
	)
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, query_id, verdict, missed_artifact_reference,
		        reviewer_identity_redacted, detail_redacted, recorded_at_unix_micros
		 FROM memory_retrieval_recall_reviews WHERE query_id = ?`,
		queryID,
	).Scan(&review.ID, &review.QueryID, &review.Verdict, &reference,
		&review.ReviewerIdentityRedacted, &review.DetailRedacted, &review.RecordedAtMicros)
	if errors.Is(err, sql.ErrNoRows) {
		return MemoryRetrievalRecallReview{}, false, nil
	}
	if err != nil {
		return MemoryRetrievalRecallReview{}, false, classify("get memory retrieval recall review", err)
	}
	if reference.Valid {
		review.MissedArtifactReference = &reference.String
	}
	return review, true, nil
}

// maximumRetrievalMeasurementWindow bounds MeasureDeterministicRetrievalRecall
// and MeasureDeterministicRetrievalReuse's windowed read, per AGENTS.md
// "Avoid unbounded ... database reads."
const maximumRetrievalMeasurementWindow = 5000

// DeterministicRetrievalRecallMeasurement is the M21-078 instrument's
// bounded, honest report. It supports docs/plan.md §0 branch 1's
// qualitative "a measured recall problem" review; it deliberately computes
// no automatic continue/stop verdict of its own.
type DeterministicRetrievalRecallMeasurement struct {
	ProjectID domain.ProjectID
	// WindowSize is the caller-requested bound on how many of the most
	// recent queries this measurement inspects in detail.
	WindowSize int
	// QueriesInWindow is how many queries the windowed detail actually
	// covers (min(WindowSize, total queries for ProjectID)).
	QueriesInWindow int
	// TotalQueriesAllTime is a cheap aggregate count across every retrieval
	// query ever recorded for ProjectID, unbounded by WindowSize: it is what
	// the §30 "after one hundred/five hundred eligible tasks" cumulative
	// counts are measured against, even though the detailed fallback/review
	// breakdown below only covers the bounded window.
	TotalQueriesAllTime int
	// FallbacksInWindow is how many of the windowed queries recorded a
	// memory_retrieval_fallbacks row (M21-076).
	FallbacksInWindow int
	// ReviewedFallbacksInWindow is how many of those fallbacks have a
	// recorded human reviewer verdict.
	ReviewedFallbacksInWindow int
	// GenuineMissesInWindow is how many reviewed fallbacks were verdicted
	// genuine-miss.
	GenuineMissesInWindow int
}

// MissRate reports GenuineMissesInWindow / ReviewedFallbacksInWindow. ok is
// false when zero fallbacks have been reviewed yet: this measurement never
// fabricates a 0% (or any) rate from an empty review set, per M21-078 "this
// is the instrument, not the verdict."
func (measurement DeterministicRetrievalRecallMeasurement) MissRate() (rate float64, ok bool) {
	if measurement.ReviewedFallbacksInWindow == 0 {
		return 0, false
	}
	return float64(measurement.GenuineMissesInWindow) / float64(measurement.ReviewedFallbacksInWindow), true
}

// MeasureDeterministicRetrievalRecall computes the M21-078 instrument's
// current reading for projectID, bounded to at most windowSize of the most
// recently requested retrieval queries. windowSize must be positive and is
// further capped at maximumRetrievalMeasurementWindow.
func (repositories *Repositories) MeasureDeterministicRetrievalRecall(
	ctx context.Context,
	projectID domain.ProjectID,
	windowSize int,
) (DeterministicRetrievalRecallMeasurement, error) {
	if projectID.IsZero() {
		return DeterministicRetrievalRecallMeasurement{}, errors.New("project ID must not be empty")
	}
	if windowSize <= 0 {
		return DeterministicRetrievalRecallMeasurement{}, errors.New("measurement window size must be positive")
	}
	if windowSize > maximumRetrievalMeasurementWindow {
		windowSize = maximumRetrievalMeasurementWindow
	}

	measurement := DeterministicRetrievalRecallMeasurement{ProjectID: projectID, WindowSize: windowSize}
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT count(*) FROM memory_retrieval_queries WHERE project_id = ?`, projectID,
	).Scan(&measurement.TotalQueriesAllTime); err != nil {
		return DeterministicRetrievalRecallMeasurement{}, classify("count memory retrieval queries", err)
	}

	queryIDs, err := windowedMemoryRetrievalQueryIDs(ctx, repositories.database.sql, projectID, windowSize)
	if err != nil {
		return DeterministicRetrievalRecallMeasurement{}, err
	}
	measurement.QueriesInWindow = len(queryIDs)
	if len(queryIDs) == 0 {
		return measurement, nil
	}

	placeholders, args := stringInPlaceholders(queryIDs)
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT count(*) FROM memory_retrieval_fallbacks WHERE query_id IN (`+placeholders+`)`, args...,
	).Scan(&measurement.FallbacksInWindow); err != nil {
		return DeterministicRetrievalRecallMeasurement{}, classify("count memory retrieval fallbacks in window", err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT count(*) FROM memory_retrieval_recall_reviews WHERE query_id IN (`+placeholders+`)`, args...,
	).Scan(&measurement.ReviewedFallbacksInWindow); err != nil {
		return DeterministicRetrievalRecallMeasurement{}, classify("count memory retrieval recall reviews in window", err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT count(*) FROM memory_retrieval_recall_reviews WHERE query_id IN (`+placeholders+`) AND verdict = ?`,
		append(append([]any{}, args...), string(MemoryRetrievalRecallVerdictGenuineMiss))...,
	).Scan(&measurement.GenuineMissesInWindow); err != nil {
		return DeterministicRetrievalRecallMeasurement{}, classify("count memory retrieval recall genuine misses in window", err)
	}
	return measurement, nil
}

// windowedMemoryRetrievalQueryIDs reads at most windowSize of the most
// recently requested query IDs for projectID.
func windowedMemoryRetrievalQueryIDs(
	ctx context.Context,
	queries memoryLineageQueryer,
	projectID domain.ProjectID,
	windowSize int,
) ([]string, error) {
	rows, err := queries.QueryContext(
		ctx,
		`SELECT id FROM memory_retrieval_queries WHERE project_id = ?
		 ORDER BY requested_at_unix_micros DESC, id DESC LIMIT ?`,
		projectID, windowSize,
	)
	if err != nil {
		return nil, classify("list windowed memory retrieval queries", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, classify("scan windowed memory retrieval query", err)
		}
		ids = append(ids, id)
	}
	return ids, classify("list windowed memory retrieval queries", rows.Err())
}

// stringInPlaceholders builds a "?,?,..." placeholder list and matching
// argument slice for values, for callers building a bounded `IN (...)`
// clause from an already-bounded ID slice.
func stringInPlaceholders(values []string) (string, []any) {
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for i, value := range values {
		placeholders[i] = "?"
		args[i] = value
	}
	return strings.Join(placeholders, ","), args
}

// -----------------------------------------------------------------------
// §30 "Exact Reuse Failure": derivable directly from already-recorded data
// -----------------------------------------------------------------------

// Named thresholds from docs/plan.md §30 "Exact Reuse Failure": "After one
// hundred eligible tasks, conduct an interim review. After five hundred,
// stop investment in atom discovery and recommendation when ... meaningful
// reuse occurs in fewer than twenty percent of eligible tasks." These are
// comparison points a caller/report evaluates against
// DeterministicRetrievalReuseMeasurement; nothing in this package applies
// them automatically -- a failed gate's continue/narrow/redesign/stop
// decision is a human call, not a computed field.
const (
	ExactReuseFailureInterimReviewTaskCount = 100
	ExactReuseFailureStopTaskCount          = 500
	ExactReuseFailureMinimumReusePercent    = 20
)

// DeterministicRetrievalReuseMeasurement reports how often a retrieval
// query's eligible candidate was actually used or adapted (docs/plan.md §30
// "meaningful reuse"), computed entirely from data the M21-064..077 pre-work
// retrieval gate already records durably -- no new schema.
type DeterministicRetrievalReuseMeasurement struct {
	ProjectID           domain.ProjectID
	WindowSize          int
	QueriesInWindow     int
	TotalQueriesAllTime int
	// EligibleQueriesInWindow is how many windowed queries discovered at
	// least one candidate that reached the eligibility gates (whether or not
	// the agent went on to use it) -- docs/plan.md §30's "eligible tasks"
	// denominator.
	EligibleQueriesInWindow int
	// ReusedQueriesInWindow is how many of those queries recorded at least
	// one "eligible-and-used" or "eligible-and-adapted" decision.
	ReusedQueriesInWindow int
}

// ReuseRate reports ReusedQueriesInWindow / EligibleQueriesInWindow. ok is
// false when no query in the window ever discovered an eligible candidate at
// all, so an empty window can never be misread as "0% reuse."
func (measurement DeterministicRetrievalReuseMeasurement) ReuseRate() (rate float64, ok bool) {
	if measurement.EligibleQueriesInWindow == 0 {
		return 0, false
	}
	return float64(measurement.ReusedQueriesInWindow) / float64(measurement.EligibleQueriesInWindow), true
}

// ReadyForInterimReview reports whether TotalQueriesAllTime has reached
// ExactReuseFailureInterimReviewTaskCount.
func (measurement DeterministicRetrievalReuseMeasurement) ReadyForInterimReview() bool {
	return measurement.TotalQueriesAllTime >= ExactReuseFailureInterimReviewTaskCount
}

// ReadyForStopDecision reports whether TotalQueriesAllTime has reached
// ExactReuseFailureStopTaskCount.
func (measurement DeterministicRetrievalReuseMeasurement) ReadyForStopDecision() bool {
	return measurement.TotalQueriesAllTime >= ExactReuseFailureStopTaskCount
}

// MeasureDeterministicRetrievalReuse computes the §30 "Exact Reuse Failure"
// reuse-rate measurement for projectID, bounded to at most windowSize of the
// most recently requested retrieval queries.
func (repositories *Repositories) MeasureDeterministicRetrievalReuse(
	ctx context.Context,
	projectID domain.ProjectID,
	windowSize int,
) (DeterministicRetrievalReuseMeasurement, error) {
	if projectID.IsZero() {
		return DeterministicRetrievalReuseMeasurement{}, errors.New("project ID must not be empty")
	}
	if windowSize <= 0 {
		return DeterministicRetrievalReuseMeasurement{}, errors.New("measurement window size must be positive")
	}
	if windowSize > maximumRetrievalMeasurementWindow {
		windowSize = maximumRetrievalMeasurementWindow
	}

	measurement := DeterministicRetrievalReuseMeasurement{ProjectID: projectID, WindowSize: windowSize}
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT count(*) FROM memory_retrieval_queries WHERE project_id = ?`, projectID,
	).Scan(&measurement.TotalQueriesAllTime); err != nil {
		return DeterministicRetrievalReuseMeasurement{}, classify("count memory retrieval queries", err)
	}

	queryIDs, err := windowedMemoryRetrievalQueryIDs(ctx, repositories.database.sql, projectID, windowSize)
	if err != nil {
		return DeterministicRetrievalReuseMeasurement{}, err
	}
	measurement.QueriesInWindow = len(queryIDs)
	if len(queryIDs) == 0 {
		return measurement, nil
	}
	placeholders, args := stringInPlaceholders(queryIDs)

	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT count(DISTINCT query_id) FROM memory_retrieval_candidates WHERE query_id IN (`+placeholders+`)`,
		args...,
	).Scan(&measurement.EligibleQueriesInWindow); err != nil {
		return DeterministicRetrievalReuseMeasurement{}, classify("count memory retrieval queries with a discovered candidate in window", err)
	}

	reusedReasons := []any{string(RetrievalReasonEligibleAndUsed), string(RetrievalReasonEligibleAndAdapted)}
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT count(DISTINCT c.query_id)
		 FROM memory_retrieval_candidates AS c
		 JOIN memory_retrieval_decisions AS d ON d.candidate_id = c.id
		 WHERE c.query_id IN (`+placeholders+`) AND d.reason_kind IN (?, ?)`,
		append(append([]any{}, args...), reusedReasons...)...,
	).Scan(&measurement.ReusedQueriesInWindow); err != nil {
		return DeterministicRetrievalReuseMeasurement{}, classify("count memory retrieval queries with reuse in window", err)
	}
	return measurement, nil
}
