package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// TestListMemoryRetrievalQueriesByTaskAndCandidatesForQuery is the G03
// storage-layer read-path proof: given only a TaskID, every retrieval query
// run for that task, and every candidate considered within each query, is
// reconstructable durably -- the missing half of "the user can identify
// every memory item that influenced a completed task" that
// GetMemoryRetrievalDecision alone (keyed by an already-known candidate ID)
// cannot answer on its own.
func TestListMemoryRetrievalQueriesByTaskAndCandidatesForQuery(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 9800)
	repositoryID := testRepositoryID(t, 9801)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 9802)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	threadID := testThreadID(t, 9805)
	if _, err := repositories.CreateThread(ctx, CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "G03 fixture",
	}); err != nil {
		t.Fatal(err)
	}
	taskID := testTaskID(t, 9806)
	if _, err := repositories.CreateTask(ctx, CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "g03-fixture-task",
	}); err != nil {
		t.Fatal(err)
	}

	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-9800", ProjectID: projectID, TaskID: &taskID, FingerprintSchemaVersion: 1, QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-9800", QueryID: "query-9800", RevisionID: revision.RevisionID,
		Rank: 1, Channels: []MemoryRetrievalCandidateSource{RetrievalCandidateExactMatch},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalDecision(ctx, CreateMemoryRetrievalDecision{
		ID: "decision-9800", CandidateID: "candidate-9800", Decision: "accepted", ReasonKind: RetrievalReasonEligibleAndUsed,
	}); err != nil {
		t.Fatal(err)
	}

	// A second, unrelated query for a DIFFERENT task must never appear.
	otherTaskID := testTaskID(t, 9807)
	if _, err := repositories.CreateTask(ctx, CreateTask{
		ID: otherTaskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "g03-fixture-other-task",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: "query-9810", ProjectID: projectID, TaskID: &otherTaskID, FingerprintSchemaVersion: 1, QueryKind: RetrievalQueryExactIdentity,
	}); err != nil {
		t.Fatal(err)
	}

	queries, err := repositories.ListMemoryRetrievalQueriesByTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 || queries[0].ID != "query-9800" {
		t.Fatalf("queries for task = %#v, want exactly query-9800", queries)
	}
	if queries[0].TaskID == nil || *queries[0].TaskID != taskID {
		t.Fatalf("queries[0].TaskID = %v, want %s", queries[0].TaskID, taskID)
	}

	candidates, err := repositories.ListMemoryRetrievalCandidatesForQuery(ctx, queries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != "candidate-9800" || candidates[0].RevisionID != revision.RevisionID {
		t.Fatalf("candidates for query = %#v", candidates)
	}

	decision, found, err := repositories.GetMemoryRetrievalDecision(ctx, candidates[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || decision.Decision != "accepted" || string(decision.ReasonKind) != string(RetrievalReasonEligibleAndUsed) {
		t.Fatalf("decision for candidate = %#v (found=%v)", decision, found)
	}
}

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
		Rank: 1, Channels: []MemoryRetrievalCandidateSource{RetrievalCandidateExactMatch},
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
		Rank: 2, Channels: []MemoryRetrievalCandidateSource{RetrievalCandidateVectorSimilarity},
	}); err == nil {
		t.Fatal("expected a vector-similarity candidate without a score to be rejected")
	}
	score := 0.87
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: "candidate-fixture-1402", QueryID: "query-fixture-1400", RevisionID: secondRevision.RevisionID,
		Rank: 2, Channels: []MemoryRetrievalCandidateSource{RetrievalCandidateVectorSimilarity}, SimilarityScore: &score,
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
		Rank: 1, Channels: []MemoryRetrievalCandidateSource{RetrievalCandidateExactMatch},
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project retrieval candidate error = %v, want constraint", err)
	}
}
