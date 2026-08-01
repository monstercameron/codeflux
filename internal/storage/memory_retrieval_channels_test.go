package storage

import (
	"errors"
	"testing"
)

// TestMemoryRetrievalCandidateChannelsPreserveFullDiscoverySet proves the
// M21-137 storage gap is closed: a candidate discovered through several
// channels at once is stored losslessly (every channel readable back), even
// though the legacy single-valued candidate_source column still receives
// exactly one ("strongest") value for existing single-value queries.
func TestMemoryRetrievalCandidateChannelsPreserveFullDiscoverySet(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1420)
	repositoryID := testRepositoryID(t, 1421)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1422)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}

	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-fixture-1420", ProjectID: projectID, FingerprintSchemaVersion: 1,
		QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}

	score := 0.42
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-fixture-1420", QueryID: "query-fixture-1420", RevisionID: revision.RevisionID,
		Rank: 1,
		Channels: []MemoryRetrievalCandidateSource{
			RetrievalCandidateVectorSimilarity, RetrievalCandidateExactMatch, RetrievalCandidateApplicabilityPass,
		},
		SimilarityScore: &score,
	}); err != nil {
		t.Fatal(err)
	}

	channels, err := repositories.ListMemoryRetrievalCandidateChannels(ctx, "candidate-fixture-1420")
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 3 {
		t.Fatalf("channels = %#v, want all 3 discovery channels preserved losslessly", channels)
	}
	found := map[MemoryRetrievalCandidateSource]bool{}
	for _, channel := range channels {
		found[channel] = true
	}
	for _, want := range []MemoryRetrievalCandidateSource{
		RetrievalCandidateExactMatch, RetrievalCandidateApplicabilityPass, RetrievalCandidateVectorSimilarity,
	} {
		if !found[want] {
			t.Fatalf("channels = %#v, missing %q", channels, want)
		}
	}

	// The legacy single-valued column still exists and picks the strongest
	// (exact-match outranks applicability-pass outranks vector-similarity),
	// per docs/plan.md §31 "Structured database fields are authoritative."
	var legacySource string
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT candidate_source FROM memory_retrieval_candidates WHERE id = ?`, "candidate-fixture-1420",
	).Scan(&legacySource); err != nil {
		t.Fatal(err)
	}
	if legacySource != string(RetrievalCandidateExactMatch) {
		t.Fatalf("legacy candidate_source = %q, want %q (strongest channel)", legacySource, RetrievalCandidateExactMatch)
	}
}

// TestMemoryRetrievalCandidateChannelsRejectDuplicateOrUndeclared proves the
// write path refuses malformed channel sets rather than silently accepting
// them.
func TestMemoryRetrievalCandidateChannelsRejectDuplicateOrUndeclared(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1430)
	repositoryID := testRepositoryID(t, 1431)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1432)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-fixture-1430", ProjectID: projectID, FingerprintSchemaVersion: 1,
		QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}

	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-fixture-1430", QueryID: "query-fixture-1430", RevisionID: revision.RevisionID,
		Rank: 1, Channels: []MemoryRetrievalCandidateSource{RetrievalCandidateExactMatch, RetrievalCandidateExactMatch},
	}); err == nil {
		t.Fatal("expected a duplicated channel to be rejected")
	}
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-fixture-1431", QueryID: "query-fixture-1430", RevisionID: revision.RevisionID,
		Rank: 1, Channels: []MemoryRetrievalCandidateSource{"not-a-real-channel"},
	}); err == nil {
		t.Fatal("expected an undeclared channel to be rejected")
	}
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-fixture-1432", QueryID: "query-fixture-1430", RevisionID: revision.RevisionID,
		Rank: 1, Channels: nil,
	}); err == nil {
		t.Fatal("expected an empty channel set to be rejected")
	}
}

// TestMemoryRetrievalFallbackRecordsNoEligibleItemAsNormalOutcome proves the
// M21-076 storage need: a query-scoped fallback fact can be recorded even
// when there is no candidate to attach a decision to, is idempotent on
// retry, and rejects a conflicting retry.
func TestMemoryRetrievalFallbackRecordsNoEligibleItemAsNormalOutcome(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1440)
	repositoryID := testRepositoryID(t, 1441)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-fixture-1440", ProjectID: projectID, FingerprintSchemaVersion: 1,
		QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}

	if _, found, err := repositories.GetMemoryRetrievalFallback(ctx, "query-fixture-1440"); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("expected no fallback record before one is created")
	}

	record, err := repositories.CreateMemoryRetrievalFallback(ctx, CreateMemoryRetrievalFallback{
		ID: "fallback-fixture-1440", QueryID: "query-fixture-1440", CandidatesConsidered: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.CandidatesConsidered != 3 {
		t.Fatalf("fallback = %#v, want candidates_considered 3", record)
	}

	// Idempotent retry with the same facts.
	again, err := repositories.CreateMemoryRetrievalFallback(ctx, CreateMemoryRetrievalFallback{
		ID: "fallback-fixture-1440", QueryID: "query-fixture-1440", CandidatesConsidered: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != record.ID {
		t.Fatalf("idempotent retry = %#v, want the original record", again)
	}

	// A conflicting retry (different considered-count) is rejected.
	if _, err := repositories.CreateMemoryRetrievalFallback(ctx, CreateMemoryRetrievalFallback{
		ID: "fallback-fixture-1440", QueryID: "query-fixture-1440", CandidatesConsidered: 5,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting fallback retry error = %v, want conflict", err)
	}

	found, ok, err := repositories.GetMemoryRetrievalFallback(ctx, "query-fixture-1440")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || found.CandidatesConsidered != 3 {
		t.Fatalf("get fallback = %#v, ok=%v, want the original record", found, ok)
	}
}
