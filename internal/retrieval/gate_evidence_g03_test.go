package retrieval

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/retrievalgate"
	"codeflux.dev/codeflux/internal/storage"
)

// TestGateEvidenceG03_EveryMemoryItemForATaskIsReconstructableByTaskID is the
// M21-G03 gate-evidence test: "The user can identify every memory item that
// influenced a completed task." It proves what is provable TODAY at the
// storage/service layer: given only a TaskID (never a QueryID or CandidateID
// the caller must already know), storage.ListMemoryRetrievalQueriesByTask +
// storage.ListMemoryRetrievalCandidatesForQuery + the existing
// GetMemoryRetrievalDecision durably and completely reconstruct every
// candidate RunPreWorkGate considered for that task, its discovery
// channel(s), and its final decision -- including which one item actually
// influenced the outcome (an "eligible-and-used" decision) versus an
// eligible-but-never-acted-on item (no decision row at all: eligibility and
// influence are different facts, per docs/plan.md §31).
//
// What this test does NOT prove, and what remains for end-to-end proof: no
// coordinator or UI wiring calls these two read queries today (a concurrent
// lane is wiring RunPreWorkGate into the coordinator separately); an actual
// user has no in-product surface yet through which to ask "what influenced
// task X." This test establishes that the durable data those two queries
// need already exists and is queryable by TaskID -- the missing piece is
// presentation, not durability.
func TestGateEvidenceG03_EveryMemoryItemForATaskIsReconstructableByTaskID(t *testing.T) {
	ctx := t.Context()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)

	// A real task record: RunPreWorkGate's TaskID linkage is what this
	// gate's read path keys off.
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "G03 fixture thread",
	}); err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := repositories.CreateTask(ctx, storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "g03-fixture-task",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, usedRevision := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go build ./... (g03 used)")
	_, untouchedRevision := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go build ./... (g03 untouched)")

	queryID := newTestQueryID(t, "g03")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID, TaskID: createdTask.ID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Eligible) != 2 {
		t.Fatalf("result.Eligible = %#v, want 2 (both candidates satisfy the runtime-only requirement)", result.Eligible)
	}
	var usedItem InfluentialMemoryItem
	for _, item := range result.Eligible {
		if item.RevisionID == usedRevision {
			usedItem = item
		}
	}
	if usedItem.RevisionID.IsZero() {
		t.Fatal("expected the used-revision candidate to be present in result.Eligible")
	}
	if err := service.RecordInfluence(ctx, usedItem, retrievalgate.AgentInfluenceActionUsed, "g03: this is the one item the agent actually used"); err != nil {
		t.Fatal(err)
	}
	// untouchedRevision is deliberately left eligible but never acted on:
	// the agent considered it and simply moved on, recording no decision at
	// all for it -- a legitimate, real outcome this gate must also be able
	// to represent (an eligible item is not automatically "influence").

	// Now reconstruct everything using ONLY taskID -- simulating a user (or
	// a future UI backed by these same two queries) who does not already
	// know queryID, candidate IDs, or which revision was used.
	queries, err := repositories.ListMemoryRetrievalQueriesByTask(ctx, createdTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 || queries[0].ID != queryID {
		t.Fatalf("queries for task = %#v, want exactly %s", queries, queryID)
	}
	if queries[0].TaskID == nil || *queries[0].TaskID != createdTask.ID {
		t.Fatalf("queries[0].TaskID = %v, want %s", queries[0].TaskID, createdTask.ID)
	}

	candidates, err := repositories.ListMemoryRetrievalCandidatesForQuery(ctx, queries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates for query = %#v, want 2", candidates)
	}

	sawUsed, sawUntouched := false, false
	for _, candidate := range candidates {
		decision, found, err := repositories.GetMemoryRetrievalDecision(ctx, candidate.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch candidate.RevisionID {
		case usedRevision:
			sawUsed = true
			if !found || decision.Decision != "accepted" || string(decision.ReasonKind) != string(retrievalgate.RejectionReasonEligibleAndUsed) {
				t.Fatalf("reconstructed decision for the used revision = %#v (found=%v), want accepted/eligible-and-used", decision, found)
			}
		case untouchedRevision:
			sawUntouched = true
			if found {
				t.Fatalf("reconstructed decision for the untouched-but-eligible revision = %#v, want no decision row at all", decision)
			}
		default:
			t.Fatalf("unexpected reconstructed candidate revision %s", candidate.RevisionID)
		}
	}
	if !sawUsed || !sawUntouched {
		t.Fatalf("reconstruction by TaskID alone did not recover both real candidates: sawUsed=%v sawUntouched=%v", sawUsed, sawUntouched)
	}
}
