package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/retrievalgate"
)

// mustEligibleMemoryCandidateFixture records one durable retrieval query and
// one candidate against taskID, so ListEligibleMemoryForTask and
// RecordEpisodeMemoryInfluence have something real to read and act on
// rather than a hand-built literal.
func mustEligibleMemoryCandidateFixture(
	t *testing.T,
	repositories *Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	taskID domain.TaskID,
	base int,
) (queryID, candidateID string) {
	t.Helper()
	ctx := t.Context()
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, base)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	queryID = "influence-query-" + testUUID(base)
	task := taskID
	if err := repositories.CreateMemoryRetrievalQuery(ctx, CreateMemoryRetrievalQuery{
		ID: queryID, ProjectID: projectID, TaskID: &task, FingerprintSchemaVersion: 1,
		QueryKind: RetrievalQueryApplicabilityFilter,
	}); err != nil {
		t.Fatal(err)
	}
	candidateID = "influence-candidate-" + testUUID(base)
	if err := repositories.CreateMemoryRetrievalCandidate(ctx, CreateMemoryRetrievalCandidate{
		ID: candidateID, QueryID: queryID, RevisionID: revision.RevisionID, Rank: 1,
		Channels: []MemoryRetrievalCandidateSource{RetrievalCandidateApplicabilityPass},
	}); err != nil {
		t.Fatal(err)
	}
	return queryID, candidateID
}

// TestListEligibleMemoryForTaskExcludesAlreadyDecidedCandidates proves
// MEM-004's durable carry discriminates correctly: a candidate the
// eligibility gates rejected outright (already carrying a
// memory_retrieval_decisions row) is excluded from the eligible set, and a
// genuinely undecided candidate is included. If the decided/undecided
// filter were absent, the rejected candidate below would incorrectly appear
// alongside the eligible one.
func TestMEM004_ListEligibleMemoryForTaskExcludesAlreadyDecidedCandidates(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5600)
	projectID := testProjectID(t, 5600)
	repositoryID := testRepositoryID(t, 5601)

	_, eligibleCandidate := mustEligibleMemoryCandidateFixture(t, repositories, projectID, repositoryID, task.ID, 5610)
	_, rejectedCandidate := mustEligibleMemoryCandidateFixture(t, repositories, projectID, repositoryID, task.ID, 5620)

	// Simulate what RunPreWorkGate itself does for an ineligible candidate:
	// a decision recorded at discovery time, before any run ever sees it.
	if err := repositories.CreateMemoryRetrievalDecision(ctx, CreateMemoryRetrievalDecision{
		ID: "pre-rejected-decision", CandidateID: rejectedCandidate, Decision: "rejected",
		ReasonKind: RetrievalReasonToolchainMismatch,
	}); err != nil {
		t.Fatal(err)
	}

	eligible, err := repositories.ListEligibleMemoryForTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 1 {
		t.Fatalf("eligible set = %#v, want exactly the one undecided candidate", eligible)
	}
	if eligible[0].CandidateID != eligibleCandidate {
		t.Fatalf("eligible candidate = %s, want %s", eligible[0].CandidateID, eligibleCandidate)
	}
}

// TestRecordEpisodeMemoryInfluenceWritesDecisionAndRemovesFromEligibleSet
// proves MEM-004a end to end: recording influence writes both the
// memory_retrieval_decisions row and the episode-scoped link atomically,
// the recorded action maps to the correct decision/reason, and the
// candidate leaves the eligible set once it has been decided -- eligibility
// becomes influence exactly once, not something that can be re-offered.
func TestMEM004a_RecordEpisodeMemoryInfluenceWritesDecisionAndRemovesFromEligibleSet(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5630)
	projectID := testProjectID(t, 5630)
	repositoryID := testRepositoryID(t, 5631)
	episode := mustOpenEpisode(t, repositories, 5633, projectID, repositoryID, task)
	_, candidateID := mustEligibleMemoryCandidateFixture(t, repositories, projectID, repositoryID, task.ID, 5640)

	before, err := repositories.ListEligibleMemoryForTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatalf("eligible set before influence = %#v, want 1", before)
	}

	influence, err := repositories.RecordEpisodeMemoryInfluence(ctx, RecordEpisodeMemoryInfluence{
		ID: "influence-1", EpisodeID: episode.ID, CandidateID: candidateID,
		Action: retrievalgate.AgentInfluenceActionUsed, JustificationRedacted: "used as-is for the retry helper",
		IdempotencyKey: "influence-1-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if influence.Action != retrievalgate.AgentInfluenceActionUsed {
		t.Fatalf("influence action = %s, want used", influence.Action)
	}

	decision, found, err := repositories.GetMemoryRetrievalDecision(ctx, candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected a memory retrieval decision to be written")
	}
	if decision.Decision != "accepted" || decision.ReasonKind != RetrievalReasonEligibleAndUsed {
		t.Fatalf("decision = %#v, want accepted/eligible-and-used", decision)
	}

	linked, err := repositories.ListEpisodeMemoryInfluence(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 1 || linked[0].CandidateID != candidateID {
		t.Fatalf("episode memory influence = %#v, want one row for %s", linked, candidateID)
	}

	// The candidate has now been decided, so it no longer appears in the
	// eligible set: eligibility became influence exactly once.
	after, err := repositories.ListEligibleMemoryForTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("eligible set after influence = %#v, want empty", after)
	}

	// Idempotent retry with the same idempotency key returns the same row
	// rather than a conflict or a duplicate.
	retried, err := repositories.RecordEpisodeMemoryInfluence(ctx, RecordEpisodeMemoryInfluence{
		ID: "influence-1", EpisodeID: episode.ID, CandidateID: candidateID,
		Action: retrievalgate.AgentInfluenceActionUsed, JustificationRedacted: "used as-is for the retry helper",
		IdempotencyKey: "influence-1-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != influence.ID {
		t.Fatalf("retried influence = %#v, want %#v", retried, influence)
	}

	// A divergent retry -- same idempotency key, different action -- is a
	// typed conflict rather than a silent second decision for one candidate.
	if _, err := repositories.RecordEpisodeMemoryInfluence(ctx, RecordEpisodeMemoryInfluence{
		ID: "influence-1", EpisodeID: episode.ID, CandidateID: candidateID,
		Action: retrievalgate.AgentInfluenceActionRejected, JustificationRedacted: "changed my mind",
		IdempotencyKey: "influence-1-key",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("divergent influence retry error = %v, want ErrConflict", err)
	}
}

// TestEpisodeMemoryInfluenceRejectsCandidateFromAnotherTask is the raw-SQL
// (and repository-layer) attack proving the candidate boundary trigger: an
// episode may only be recorded as influenced by a candidate that was
// actually queried for THAT episode's own task, never one borrowed from a
// different task's retrieval pass.
func TestMEM004a_EpisodeMemoryInfluenceRejectsCandidateFromAnotherTask(t *testing.T) {
	ctx := t.Context()
	repositories, taskA := createTaskFixture(t, 5650)
	projectID := testProjectID(t, 5650)
	repositoryID := testRepositoryID(t, 5651)
	episodeA := mustOpenEpisode(t, repositories, 5653, projectID, repositoryID, taskA)

	taskB := secondTaskFixture(t, repositories, projectID, repositoryID, 5660)
	_, candidateForB := mustEligibleMemoryCandidateFixture(t, repositories, projectID, repositoryID, taskB.ID, 5670)

	// Repository-layer attempt: episode A, candidate that belongs to task B.
	if _, err := repositories.RecordEpisodeMemoryInfluence(ctx, RecordEpisodeMemoryInfluence{
		ID: "cross-task-influence", EpisodeID: episodeA.ID, CandidateID: candidateForB,
		Action: retrievalgate.AgentInfluenceActionUsed, JustificationRedacted: "borrowed from the wrong task",
		IdempotencyKey: "cross-task-key",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-task influence error = %v, want ErrConstraint", err)
	}

	// Raw-SQL attack: bypass the repository (and so the decision-row write)
	// entirely and attempt the same cross-task link directly.
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO episode_memory_influence_events (
			id, episode_id, candidate_id, action, justification_redacted, idempotency_key, recorded_at_unix_micros
		) VALUES ('cross-task-raw', ?, ?, 'used', 'raw attack', 'cross-task-raw-key', 0)`,
		episodeA.ID, candidateForB,
	); !errors.Is(classify("raw cross-task influence insert", err), ErrConstraint) {
		t.Fatalf("raw cross-task influence insert error = %v, want ErrConstraint", err)
	}
}

// TestEpisodeMemoryInfluenceEventsAreImmutable is the raw-SQL attack proving
// a recorded influence event can never be edited or removed after the fact.
func TestMEM004a_EpisodeMemoryInfluenceEventsAreImmutable(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5680)
	projectID := testProjectID(t, 5680)
	repositoryID := testRepositoryID(t, 5681)
	episode := mustOpenEpisode(t, repositories, 5683, projectID, repositoryID, task)
	_, candidateID := mustEligibleMemoryCandidateFixture(t, repositories, projectID, repositoryID, task.ID, 5690)

	influence, err := repositories.RecordEpisodeMemoryInfluence(ctx, RecordEpisodeMemoryInfluence{
		ID: "immutable-influence", EpisodeID: episode.ID, CandidateID: candidateID,
		Action: retrievalgate.AgentInfluenceActionAdapted, JustificationRedacted: "adapted the parameter names",
		IdempotencyKey: "immutable-influence-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE episode_memory_influence_events SET action = 'rejected' WHERE id = ?`, influence.ID,
	); !errors.Is(classify("raw mutate influence event", err), ErrConstraint) {
		t.Fatalf("raw UPDATE of an influence event error = %v, want ErrConstraint", err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx, `DELETE FROM episode_memory_influence_events WHERE id = ?`, influence.ID,
	); !errors.Is(classify("raw delete influence event", err), ErrConstraint) {
		t.Fatalf("raw DELETE of an influence event error = %v, want ErrConstraint", err)
	}
}
