package coordinator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// mustOpenRunEpisodeFixture builds a real project/repository/task and an
// AgentExecution with nothing but real repositories wired in, so
// openRunEpisode/closeRunEpisode/recordAttemptTransition can be exercised
// directly without constructing the rest of AgentExecution.Run's
// dependencies (a provider, a worktree, a graph service) that these three
// functions never touch.
func mustOpenRunEpisodeFixture(t *testing.T) (*AgentExecution, agentScope, domain.TaskID) {
	t.Helper()
	repositories, _ := mustOpenRetrievalRepositories(t)
	projectID, repositoryID := mustCreateForecastFixtureProjectAndRepository(t, repositories)
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	mustCreateForecastFixtureTask(t, repositories, projectID, repositoryID, taskID)
	execution := &AgentExecution{repositories: repositories}
	scope := agentScope{
		projectID: projectID, repositoryID: repositoryID, taskID: taskID,
		revision: "deadbeefcafefeed",
	}
	return execution, scope, taskID
}

// TestMEM001_OpenRunEpisodeBindsFingerprintAndCloseFreezesItUnresolved
// proves the MEM-001 run-boundary wiring end to end: a task whose exact
// fingerprint was bound at forecast time (RecordTaskExactFingerprintBinding,
// the same call ForecastTask now makes) gets a real, open episode carrying
// that exact fingerprint when its run starts, and a real, closed episode
// with Unresolved outcome when its run ends -- the append-only record §31
// assumes exists, actually existing.
func TestMEM001_OpenRunEpisodeBindsFingerprintAndCloseFreezesItUnresolved(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID := mustOpenRunEpisodeFixture(t)

	hash := strings.Repeat("c", 64)
	if _, err := execution.repositories.RecordTaskExactFingerprintBinding(ctx, storage.RecordTaskExactFingerprintBinding{
		TaskID: taskID, FingerprintSchemaVersion: 1, FingerprintHash: hash,
	}); err != nil {
		t.Fatal(err)
	}

	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	episode := execution.openRunEpisode(ctx, scope, taskID, runID, 1)
	if episode.ID.IsZero() {
		t.Fatal("openRunEpisode returned a zero episode for a task with a real fingerprint binding")
	}
	if episode.Attempt != 1 {
		t.Fatalf("episode attempt = %d, want 1", episode.Attempt)
	}
	if episode.FingerprintHash != hash {
		t.Fatalf("episode fingerprint hash = %s, want the bound hash %s", episode.FingerprintHash, hash)
	}
	if episode.Status != domain.EpisodeStatusOpen {
		t.Fatalf("episode status = %s, want open", episode.Status)
	}

	execution.closeRunEpisode(ctx, episode, scope)

	closed, err := execution.repositories.GetEpisode(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != domain.EpisodeStatusClosed {
		t.Fatalf("episode status after closeRunEpisode = %s, want closed", closed.Status)
	}
	if closed.Outcome == nil || *closed.Outcome != domain.EpisodeOutcomeUnresolved {
		t.Fatalf("episode outcome = %#v, want Unresolved (no user decision is made inside a run)", closed.Outcome)
	}
	if closed.EndingRevision == nil || *closed.EndingRevision != scope.revision {
		t.Fatalf("episode ending revision = %#v, want %s", closed.EndingRevision, scope.revision)
	}
}

// TestMEM001_OpenRunEpisodeIsBestEffortWithoutAFingerprintBinding proves the
// documented best-effort contract: a task whose exact fingerprint was never
// bound (this migration's carry has nothing to read) gets no episode at
// all, rather than a run failure or a fabricated fingerprint that would
// never be found by ListEpisodesByFingerprintHash. If openRunEpisode
// invented a fingerprint instead of refusing, this test's second assertion
// -- that no episode row exists for the task at all -- would fail.
func TestMEM001_OpenRunEpisodeIsBestEffortWithoutAFingerprintBinding(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID := mustOpenRunEpisodeFixture(t)

	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	episode := execution.openRunEpisode(ctx, scope, taskID, runID, 1)
	if !episode.ID.IsZero() {
		t.Fatalf("openRunEpisode = %#v, want a zero episode when no fingerprint binding exists", episode)
	}
	if _, err := execution.repositories.GetEpisodeByTask(ctx, taskID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetEpisodeByTask error = %v, want ErrNotFound (no episode should have been created)", err)
	}

	// closeRunEpisode on the zero episode must be a safe no-op, not a panic
	// or a write against an empty episode ID.
	execution.closeRunEpisode(ctx, episode, scope)
}

// TestMEM003_OpenRunEpisodeBindsThePipelineAttemptNumberItIsGiven proves the
// coordinator-level half of MEM-003: openRunEpisode forwards whatever
// attempt number it is called with (in production, pipelineLedger.
// currentAttempt(), which is itself storage.NextPipelineAttempt) rather
// than hardcoding attempt 1 regardless of caller. Two calls naming
// different attempts for the same task must produce two independent,
// separately reachable episodes.
func TestMEM003_OpenRunEpisodeBindsThePipelineAttemptNumberItIsGiven(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID := mustOpenRunEpisodeFixture(t)
	hash := strings.Repeat("d", 64)
	if _, err := execution.repositories.RecordTaskExactFingerprintBinding(ctx, storage.RecordTaskExactFingerprintBinding{
		TaskID: taskID, FingerprintSchemaVersion: 1, FingerprintHash: hash,
	}); err != nil {
		t.Fatal(err)
	}

	firstRun, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	first := execution.openRunEpisode(ctx, scope, taskID, firstRun, 1)
	if first.ID.IsZero() || first.Attempt != 1 {
		t.Fatalf("first episode = %#v, want a real attempt-1 episode", first)
	}
	execution.closeRunEpisode(ctx, first, scope)

	secondRun, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	second := execution.openRunEpisode(ctx, scope, taskID, secondRun, 2)
	if second.ID.IsZero() || second.Attempt != 2 {
		t.Fatalf("second episode = %#v, want a real attempt-2 episode", second)
	}
	if second.ID == first.ID {
		t.Fatal("the second attempt reused the first attempt's episode identity")
	}

	byAttempt1, err := execution.repositories.GetEpisodeByTaskAttempt(ctx, taskID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if byAttempt1.ID != first.ID {
		t.Fatalf("GetEpisodeByTaskAttempt(1) = %#v, want %#v", byAttempt1, first)
	}
	byAttempt2, err := execution.repositories.GetEpisodeByTaskAttempt(ctx, taskID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if byAttempt2.ID != second.ID {
		t.Fatalf("GetEpisodeByTaskAttempt(2) = %#v, want %#v", byAttempt2, second)
	}
}

// TestMEM002_RecordAttemptTransitionPersistsGateFailureRungAndOutcome
// proves the coordinator-level half of MEM-002: recordAttemptTransition
// turns its arguments into a durable, readable-back
// episode_attempt_transitions row through the real repository -- the exact
// fact agent_execution.go's sendBack closure and converged break now call
// this with -- for both a sent-back attempt (failure present) and a
// converged one (failure absent).
func TestMEM002_RecordAttemptTransitionPersistsGateFailureRungAndOutcome(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID := mustOpenRunEpisodeFixture(t)
	hash := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if _, err := execution.repositories.RecordTaskExactFingerprintBinding(ctx, storage.RecordTaskExactFingerprintBinding{
		TaskID: taskID, FingerprintSchemaVersion: 1, FingerprintHash: hash,
	}); err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	episode := execution.openRunEpisode(ctx, scope, taskID, runID, 1)
	if episode.ID.IsZero() {
		t.Fatal("expected a real episode for this fixture")
	}

	execution.recordAttemptTransition(ctx, episode, 1, "assembly",
		"the code does not compile: undefined: Foo", "gpt-5.6-luna/standard",
		storage.EpisodeAttemptOutcomeSentBack)
	execution.recordAttemptTransition(ctx, episode, 2, "convergence", "",
		"gpt-5.6-luna/standard", storage.EpisodeAttemptOutcomeConverged)

	transitions, err := execution.repositories.ListEpisodeAttemptTransitions(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 2 {
		t.Fatalf("transitions = %#v, want 2", transitions)
	}
	first := transitions[0]
	if first.Attempt != 1 || first.Gate != "assembly" || first.Rung != "gpt-5.6-luna/standard" ||
		first.Outcome != storage.EpisodeAttemptOutcomeSentBack {
		t.Fatalf("first transition = %#v", first)
	}
	if first.NormalizedFailure == nil || *first.NormalizedFailure != "the code does not compile: undefined: Foo" {
		t.Fatalf("first transition normalized failure = %#v", first.NormalizedFailure)
	}
	second := transitions[1]
	if second.Attempt != 2 || second.Outcome != storage.EpisodeAttemptOutcomeConverged {
		t.Fatalf("second transition = %#v", second)
	}
	if second.NormalizedFailure != nil {
		t.Fatalf("converged transition normalized failure = %#v, want nil", second.NormalizedFailure)
	}
}

// TestMEM002_RecordAttemptTransitionIsBestEffortForAZeroEpisode proves the
// same best-effort contract recordAttemptTransition documents: called
// against a zero-value episode (the shape openRunEpisode returns when it
// could not open one), it must not panic and must not attempt a write.
func TestMEM002_RecordAttemptTransitionIsBestEffortForAZeroEpisode(t *testing.T) {
	execution, _, _ := mustOpenRunEpisodeFixture(t)
	execution.recordAttemptTransition(context.Background(), storage.Episode{}, 1,
		"assembly", "it did not compile", "gpt-5.6-luna/standard", storage.EpisodeAttemptOutcomeSentBack)
}
