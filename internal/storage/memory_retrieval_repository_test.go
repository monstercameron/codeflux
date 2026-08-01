package storage

import (
	"errors"
	"testing"
)

func TestMemoryRetrievalLogsRecordCandidatesAndDecisionsWithinProjectBoundary(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1400)
	repositoryID := testRepositoryID(t, 1401)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1402)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	secondArtifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1405)
	secondRevision, err := repositories.GetLatestMemoryArtifactRevision(ctx, secondArtifact)
	if err != nil {
		t.Fatal(err)
	}

	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-fixture-1400", ProjectID: projectID, FingerprintSchemaVersion: 1,
		QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-fixture-1400", QueryID: "query-fixture-1400", RevisionID: revision.RevisionID,
		Rank: 1, CandidateSource: RetrievalCandidateExactMatch,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalDecision(ctx, CreateMemoryRetrievalDecision{
		ID: "decision-fixture-1400", CandidateID: "candidate-fixture-1400",
		Decision: "accepted", ReasonKind: RetrievalReasonEligibleAndUsed,
	}); err != nil {
		t.Fatal(err)
	}

	// A vector-similarity candidate must always carry a similarity score.
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-fixture-1401", QueryID: "query-fixture-1400", RevisionID: secondRevision.RevisionID,
		Rank: 2, CandidateSource: RetrievalCandidateVectorSimilarity,
	}); err == nil {
		t.Fatal("expected a vector-similarity candidate without a score to be rejected")
	}
	score := 0.87
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-fixture-1402", QueryID: "query-fixture-1400", RevisionID: secondRevision.RevisionID,
		Rank: 2, CandidateSource: RetrievalCandidateVectorSimilarity, SimilarityScore: &score,
	}); err != nil {
		t.Fatal(err)
	}
	// Vector similarity alone never establishes eligibility: it must still
	// be logged through a decision using an "eligible" reason to be used.
	if err := repositories.CreateMemoryRetrievalDecision(ctx, CreateMemoryRetrievalDecision{
		ID: "decision-fixture-1402", CandidateID: "candidate-fixture-1402",
		Decision: "rejected", ReasonKind: RetrievalReasonApplicabilityPredicateFailed,
	}); err != nil {
		t.Fatal(err)
	}

	otherProjectID := testProjectID(t, 1410)
	otherRepositoryID := testRepositoryID(t, 1411)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)
	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-fixture-1410", ProjectID: otherProjectID, FingerprintSchemaVersion: 1,
		QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-fixture-1410", QueryID: "query-fixture-1410", RevisionID: revision.RevisionID,
		Rank: 1, CandidateSource: RetrievalCandidateExactMatch,
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project retrieval candidate error = %v, want constraint", err)
	}
}
