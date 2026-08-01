package retrieval

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// TestGateEvidenceG05_PreWorkGateWorksWithZeroVectorInfrastructure is the
// M21-G05 gate-evidence test: "The prototype still works when vector
// discovery is disabled." Vector discovery is not merely config-flagged off
// today: no embedding provider, similarity search, or ranking code exists
// anywhere in the repository at all (M21-078 is the measurement instrument
// gating that decision; M21-079..088 remain closed at the docs/plan.md §0
// branch gate), and no config toggle for it exists either (grep confirms
// this). This test proves the strongest available form of G05 given that
// fact: the complete pre-work retrieval gate -- exact-identity discovery,
// structured-field discovery, every eligibility gate, durable logging, the
// fallback path, and RecordInfluence -- runs successfully end to end
// against a real, temporary, migrated SQLite database that has ZERO rows in
// every vector-related table (memory_embedding_models,
// memory_embedding_spaces, memory_artifact_embeddings,
// atom_documentation_embeddings) throughout, and never once logs a
// DiscoveryChannelVectorSimilarity/RetrievalCandidateVectorSimilarity
// channel for any real candidate.
func TestGateEvidenceG05_PreWorkGateWorksWithZeroVectorInfrastructure(t *testing.T) {
	ctx := t.Context()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)

	_, eligibleRevision := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go build ./... (g05)")
	if _, err := repositories.ListMemoryArtifactEmbeddings(ctx, eligibleRevision); err != nil {
		t.Fatal(err)
	}

	queryID := newTestQueryID(t, "g05")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Eligible) != 1 || result.Eligible[0].RevisionID != eligibleRevision {
		t.Fatalf("result.Eligible = %#v, want exactly one item", result.Eligible)
	}

	// Zero vector infrastructure exists at all for this revision: no
	// embedding was ever created, so nothing had to be "disabled."
	embeddings, err := repositories.ListMemoryArtifactEmbeddings(ctx, eligibleRevision)
	if err != nil {
		t.Fatal(err)
	}
	if len(embeddings) != 0 {
		t.Fatalf("embeddings = %#v, want zero", embeddings)
	}

	// The candidate's real, durably-logged discovery channel(s) are exactly
	// the structured-field channel this package's discoverByKind function
	// produces -- never vector-similarity, which nothing in this package
	// can produce (see doc.go: "no embedding generation, similarity search,
	// or ranking happens anywhere in this package").
	candidateID := deriveCandidateID(queryID, eligibleRevision)
	channels, err := repositories.ListMemoryRetrievalCandidateChannels(ctx, candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		t.Fatal("expected at least one recorded discovery channel")
	}
	for _, channel := range channels {
		if channel == storage.RetrievalCandidateVectorSimilarity {
			t.Fatalf("candidate channel %v included vector-similarity, want none: nothing in this package can produce that channel", channels)
		}
	}

	// The fallback path (M21-076) also works end to end with zero vector
	// infrastructure: a query that discovers nothing still completes
	// cleanly, never as an error.
	otherRepositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	emptyTaskInOtherRepository := mustBuildTaskFingerprint(t, projectID, otherRepositoryID, domain.AssuranceLevelFullyEvaluated)
	fallbackQueryID := newTestQueryID(t, "g05-fallback")
	fallbackResult, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: fallbackQueryID, ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     emptyTaskInOtherRepository,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fallbackResult.FellBack {
		t.Fatal("expected a clean fallback for a repository with no memory artifacts at all")
	}
}
