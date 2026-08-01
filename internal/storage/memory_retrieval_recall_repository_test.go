package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// mustCreateFallenBackQuery creates a memory retrieval query and its
// fallback record (M21-076: zero eligible candidates), the minimum real
// state a M21-078 recall review may attach to.
func mustCreateFallenBackQuery(t *testing.T, repositories *Repositories, projectID domain.ProjectID, queryID string, candidatesConsidered int) {
	t.Helper()
	ctx := t.Context()
	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: queryID, ProjectID: projectID, FingerprintSchemaVersion: 1, QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryRetrievalFallback(ctx, CreateMemoryRetrievalFallback{
		ID: queryID + "-fallback", QueryID: queryID, CandidatesConsidered: candidatesConsidered,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestMeasureDeterministicRetrievalRecallReportsNoDataUntilReviewed is the
// M21-078 decisive test: "this is the instrument, not the verdict -- do not
// fabricate a result." A fresh project with no retrieval queries at all, and
// a project with one recorded fallback that has never been reviewed, must
// both report MissRate ok=false, never a fabricated 0%.
func TestMeasureDeterministicRetrievalRecallReportsNoDataUntilReviewed(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9700)
	repositoryID := testRepositoryID(t, 9701)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	measurement, err := repositories.MeasureDeterministicRetrievalRecall(ctx, projectID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.TotalQueriesAllTime != 0 || measurement.QueriesInWindow != 0 || measurement.FallbacksInWindow != 0 {
		t.Fatalf("measurement over an empty project = %#v, want all zero", measurement)
	}
	if _, ok := measurement.MissRate(); ok {
		t.Fatal("MissRate reported ok=true over zero reviewed fallbacks; must never fabricate a rate")
	}

	mustCreateFallenBackQuery(t, repositories, projectID, "query-9700-unreviewed", 0)
	measurement, err = repositories.MeasureDeterministicRetrievalRecall(ctx, projectID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.TotalQueriesAllTime != 1 || measurement.FallbacksInWindow != 1 || measurement.ReviewedFallbacksInWindow != 0 {
		t.Fatalf("measurement with one unreviewed fallback = %#v", measurement)
	}
	if _, ok := measurement.MissRate(); ok {
		t.Fatal("MissRate reported ok=true with zero human review; an unreviewed fallback must not count as a measured miss rate of any value")
	}
}

func TestCreateMemoryRetrievalRecallReviewRequiresFallback(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9710)
	repositoryID := testRepositoryID(t, 9711)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	// A query that never fell back (it simply does not exist here) cannot
	// receive a recall review: M21-078 is scoped exactly to queries whose
	// exact/structured channels returned nothing eligible.
	if _, err := repositories.CreateMemoryRetrievalRecallReview(ctx, CreateMemoryRetrievalRecallReview{
		ID: "review-9710", QueryID: "query-9710-does-not-exist",
		Verdict:                  MemoryRetrievalRecallVerdictNoReusableArtifactExisted,
		ReviewerIdentityRedacted: "reviewer-a", DetailRedacted: "no fallback exists for this query",
	}); !errors.Is(err, ErrMemoryRetrievalRecallReviewRequiresFallback) {
		t.Fatalf("error = %v, want ErrMemoryRetrievalRecallReviewRequiresFallback", err)
	}

	// A real query that discovered and used an eligible candidate (so it
	// never fell back) is equally out of scope.
	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-9710-no-fallback", ProjectID: projectID, FingerprintSchemaVersion: 1, QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryRetrievalRecallReview(ctx, CreateMemoryRetrievalRecallReview{
		ID: "review-9711", QueryID: "query-9710-no-fallback",
		Verdict:                  MemoryRetrievalRecallVerdictNoReusableArtifactExisted,
		ReviewerIdentityRedacted: "reviewer-a", DetailRedacted: "this query never fell back",
	}); !errors.Is(err, ErrMemoryRetrievalRecallReviewRequiresFallback) {
		t.Fatalf("error = %v, want ErrMemoryRetrievalRecallReviewRequiresFallback", err)
	}
}

func TestCreateMemoryRetrievalRecallReviewVerdictReferenceInvariant(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9720)
	repositoryID := testRepositoryID(t, 9721)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	mustCreateFallenBackQuery(t, repositories, projectID, "query-9720", 1)

	if _, err := repositories.CreateMemoryRetrievalRecallReview(ctx, CreateMemoryRetrievalRecallReview{
		ID: "review-9720-a", QueryID: "query-9720", Verdict: MemoryRetrievalRecallVerdictGenuineMiss,
		ReviewerIdentityRedacted: "reviewer-a", DetailRedacted: "a genuine miss with no reference",
	}); err == nil {
		t.Fatal("expected a genuine-miss verdict with no missed-artifact reference to be rejected")
	}
	if _, err := repositories.CreateMemoryRetrievalRecallReview(ctx, CreateMemoryRetrievalRecallReview{
		ID: "review-9720-b", QueryID: "query-9720", Verdict: MemoryRetrievalRecallVerdictNoReusableArtifactExisted,
		MissedArtifactReference: "mem_should-not-be-set", ReviewerIdentityRedacted: "reviewer-a", DetailRedacted: "not a genuine miss",
	}); err == nil {
		t.Fatal("expected a non-genuine-miss verdict carrying a missed-artifact reference to be rejected")
	}

	review, err := repositories.CreateMemoryRetrievalRecallReview(ctx, CreateMemoryRetrievalRecallReview{
		ID: "review-9720-c", QueryID: "query-9720", Verdict: MemoryRetrievalRecallVerdictGenuineMiss,
		MissedArtifactReference: "mem_the-reusable-artifact", ReviewerIdentityRedacted: "reviewer-a", DetailRedacted: "the reused command already existed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if review.MissedArtifactReference == nil || *review.MissedArtifactReference != "mem_the-reusable-artifact" {
		t.Fatalf("review.MissedArtifactReference = %v, want the recorded reference", review.MissedArtifactReference)
	}

	fetched, found, err := repositories.GetMemoryRetrievalRecallReview(ctx, "query-9720")
	if err != nil {
		t.Fatal(err)
	}
	if !found || fetched.Verdict != MemoryRetrievalRecallVerdictGenuineMiss {
		t.Fatalf("fetched review = %#v (found=%v)", fetched, found)
	}

	// One review per query: a second attempt for the same already-reviewed
	// query must not silently succeed or overwrite.
	if _, err := repositories.CreateMemoryRetrievalRecallReview(ctx, CreateMemoryRetrievalRecallReview{
		ID: "review-9720-d", QueryID: "query-9720", Verdict: MemoryRetrievalRecallVerdictInconclusive,
		ReviewerIdentityRedacted: "reviewer-b", DetailRedacted: "a second reviewer disagrees",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second review for the same query error = %v, want conflict", err)
	}
}

func TestMemoryRetrievalRecallReviewsAreImmutable(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9730)
	repositoryID := testRepositoryID(t, 9731)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	mustCreateFallenBackQuery(t, repositories, projectID, "query-9730", 2)
	if _, err := repositories.CreateMemoryRetrievalRecallReview(ctx, CreateMemoryRetrievalRecallReview{
		ID: "review-9730", QueryID: "query-9730", Verdict: MemoryRetrievalRecallVerdictInconclusive,
		ReviewerIdentityRedacted: "reviewer-a", DetailRedacted: "could not determine either way",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE memory_retrieval_recall_reviews SET verdict = 'genuine-miss' WHERE id = ?`, "review-9730",
	); err == nil {
		t.Fatal("expected direct UPDATE of an immutable recall review to fail")
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx, `DELETE FROM memory_retrieval_recall_reviews WHERE id = ?`, "review-9730",
	); err == nil {
		t.Fatal("expected direct DELETE of an immutable recall review to fail")
	}
}

// TestMeasureDeterministicRetrievalRecallComputesRate is the M21-078
// instrument's populated-data proof: a mix of reviewed genuine misses,
// reviewed non-misses, and one still-unreviewed fallback must compute
// exactly the rate over what was actually reviewed.
func TestMeasureDeterministicRetrievalRecallComputesRate(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9740)
	repositoryID := testRepositoryID(t, 9741)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	mustCreateFallenBackQuery(t, repositories, projectID, "query-9740-miss-1", 1)
	mustCreateFallenBackQuery(t, repositories, projectID, "query-9740-miss-2", 1)
	mustCreateFallenBackQuery(t, repositories, projectID, "query-9740-no-miss", 1)
	mustCreateFallenBackQuery(t, repositories, projectID, "query-9740-unreviewed", 0)

	for _, review := range []struct {
		id, queryID string
		verdict     MemoryRetrievalRecallVerdict
		reference   string
	}{
		{"review-9740-1", "query-9740-miss-1", MemoryRetrievalRecallVerdictGenuineMiss, "mem_a"},
		{"review-9740-2", "query-9740-miss-2", MemoryRetrievalRecallVerdictGenuineMiss, "mem_b"},
		{"review-9740-3", "query-9740-no-miss", MemoryRetrievalRecallVerdictNoReusableArtifactExisted, ""},
	} {
		if _, err := repositories.CreateMemoryRetrievalRecallReview(ctx, CreateMemoryRetrievalRecallReview{
			ID: review.id, QueryID: review.queryID, Verdict: review.verdict,
			MissedArtifactReference: review.reference, ReviewerIdentityRedacted: "reviewer-a", DetailRedacted: "test verdict",
		}); err != nil {
			t.Fatal(err)
		}
	}

	measurement, err := repositories.MeasureDeterministicRetrievalRecall(ctx, projectID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.TotalQueriesAllTime != 4 || measurement.QueriesInWindow != 4 {
		t.Fatalf("query counts = %#v, want 4/4", measurement)
	}
	if measurement.FallbacksInWindow != 4 {
		t.Fatalf("FallbacksInWindow = %d, want 4", measurement.FallbacksInWindow)
	}
	if measurement.ReviewedFallbacksInWindow != 3 {
		t.Fatalf("ReviewedFallbacksInWindow = %d, want 3", measurement.ReviewedFallbacksInWindow)
	}
	if measurement.GenuineMissesInWindow != 2 {
		t.Fatalf("GenuineMissesInWindow = %d, want 2", measurement.GenuineMissesInWindow)
	}
	rate, ok := measurement.MissRate()
	if !ok {
		t.Fatal("MissRate ok = false, want true once fallbacks have been reviewed")
	}
	if rate != 2.0/3.0 {
		t.Fatalf("MissRate = %v, want 2/3", rate)
	}
}

// TestMeasureDeterministicRetrievalRecallWindowBounds proves the read stays
// bounded (AGENTS.md "Avoid unbounded ... database reads"): with more
// queries than windowSize, only the most recently requested windowSize
// queries are reflected in the detailed fallback/review counts, while
// TotalQueriesAllTime still reports the true cumulative total the §30
// hundred/five-hundred-task thresholds are measured against.
func TestMeasureDeterministicRetrievalRecallWindowBounds(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9750)
	repositoryID := testRepositoryID(t, 9751)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	// Three older, reviewed genuine misses that the small window below must
	// exclude from its detailed counts.
	for i := 0; i < 3; i++ {
		queryID := "query-9750-old-" + string(rune('a'+i))
		mustCreateFallenBackQuery(t, repositories, projectID, queryID, 1)
		if _, err := repositories.CreateMemoryRetrievalRecallReview(ctx, CreateMemoryRetrievalRecallReview{
			ID: "review-9750-old-" + string(rune('a'+i)), QueryID: queryID, Verdict: MemoryRetrievalRecallVerdictGenuineMiss,
			MissedArtifactReference: "mem_old", ReviewerIdentityRedacted: "reviewer-a", DetailRedacted: "old reviewed miss",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// One newer, unreviewed fallback.
	mustCreateFallenBackQuery(t, repositories, projectID, "query-9750-new", 1)

	measurement, err := repositories.MeasureDeterministicRetrievalRecall(ctx, projectID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.TotalQueriesAllTime != 4 {
		t.Fatalf("TotalQueriesAllTime = %d, want 4 (unbounded by the window)", measurement.TotalQueriesAllTime)
	}
	if measurement.QueriesInWindow != 1 {
		t.Fatalf("QueriesInWindow = %d, want 1", measurement.QueriesInWindow)
	}
	if measurement.FallbacksInWindow != 1 || measurement.ReviewedFallbacksInWindow != 0 {
		t.Fatalf("windowed counts = %#v, want the single newest (unreviewed) query only", measurement)
	}
	if _, ok := measurement.MissRate(); ok {
		t.Fatal("MissRate ok = true, want false: the one query inside this bounded window was never reviewed")
	}
}

// TestMeasureDeterministicRetrievalReuse proves the §30 "Exact Reuse
// Failure" reuse-rate measurement is fully derivable from data the pre-work
// retrieval gate already records, with no new schema.
func TestMeasureDeterministicRetrievalReuse(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9760)
	repositoryID := testRepositoryID(t, 9761)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	measurement, err := repositories.MeasureDeterministicRetrievalReuse(ctx, projectID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := measurement.ReuseRate(); ok {
		t.Fatal("ReuseRate ok = true over an empty project, want false")
	}
	if measurement.ReadyForInterimReview() || measurement.ReadyForStopDecision() {
		t.Fatal("an empty project must not already be ready for the §30 interim review or stop decision")
	}

	artifactUsed := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9762)
	revisionUsed, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifactUsed)
	if err != nil {
		t.Fatal(err)
	}
	artifactRejected := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9765)
	revisionRejected, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifactRejected)
	if err != nil {
		t.Fatal(err)
	}

	// Query A: one candidate discovered and actually reused.
	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-9760-reused", ProjectID: projectID, FingerprintSchemaVersion: 1, QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-9760-reused", QueryID: "query-9760-reused", RevisionID: revisionUsed.RevisionID,
		Rank: 1, Channels: []MemoryRetrievalCandidateSource{RetrievalCandidateExactMatch},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalDecision(ctx, CreateMemoryRetrievalDecision{
		ID: "decision-9760-reused", CandidateID: "candidate-9760-reused",
		Decision: "accepted", ReasonKind: RetrievalReasonEligibleAndUsed,
	}); err != nil {
		t.Fatal(err)
	}

	// Query B: one candidate discovered, eligible, but the agent rejected it.
	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-9760-not-reused", ProjectID: projectID, FingerprintSchemaVersion: 1, QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-9760-not-reused", QueryID: "query-9760-not-reused", RevisionID: revisionRejected.RevisionID,
		Rank: 1, Channels: []MemoryRetrievalCandidateSource{RetrievalCandidateExactMatch},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalDecision(ctx, CreateMemoryRetrievalDecision{
		ID: "decision-9760-not-reused", CandidateID: "candidate-9760-not-reused",
		Decision: "rejected", ReasonKind: RetrievalReasonEligibleAndRejectedByAgent,
	}); err != nil {
		t.Fatal(err)
	}

	// Query C: a plain fallback -- discovered nothing, so it is not an
	// "eligible task" for the reuse denominator at all.
	mustCreateFallenBackQuery(t, repositories, projectID, "query-9760-fallback", 0)

	measurement, err = repositories.MeasureDeterministicRetrievalReuse(ctx, projectID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.TotalQueriesAllTime != 3 {
		t.Fatalf("TotalQueriesAllTime = %d, want 3", measurement.TotalQueriesAllTime)
	}
	if measurement.EligibleQueriesInWindow != 2 {
		t.Fatalf("EligibleQueriesInWindow = %d, want 2 (the fallback query discovered nothing)", measurement.EligibleQueriesInWindow)
	}
	if measurement.ReusedQueriesInWindow != 1 {
		t.Fatalf("ReusedQueriesInWindow = %d, want 1", measurement.ReusedQueriesInWindow)
	}
	rate, ok := measurement.ReuseRate()
	if !ok || rate != 0.5 {
		t.Fatalf("ReuseRate = (%v, %v), want (0.5, true)", rate, ok)
	}
}
