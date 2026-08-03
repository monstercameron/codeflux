package storage

import (
	"errors"
	"testing"
)

// TestRecordEpisodeAttemptTransitionAllocatesOrdinalsPerAttempt proves
// MEM-002: an attempt's gate, normalised failure, rung, and outcome become
// durable rows an extractor can read, ordinals allocate independently per
// (episode, attempt), and a retried idempotency key returns the existing
// row rather than minting a duplicate.
func TestMEM002_RecordEpisodeAttemptTransitionAllocatesOrdinalsPerAttempt(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5400)
	projectID := testProjectID(t, 5400)
	repositoryID := testRepositoryID(t, 5401)
	episode := mustOpenEpisode(t, repositories, 5403, projectID, repositoryID, task)

	failure := "the code does not compile: undefined: Foo"
	first, err := repositories.RecordEpisodeAttemptTransition(ctx, RecordEpisodeAttemptTransition{
		ID: "transition-1", EpisodeID: episode.ID, Attempt: 1, Gate: "assembly",
		NormalizedFailure: &failure, Rung: "gpt-5.6-luna/standard",
		Outcome: EpisodeAttemptOutcomeSentBack, IdempotencyKey: "transition-1-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Ordinal != 0 {
		t.Fatalf("first transition ordinal = %d, want 0", first.Ordinal)
	}
	if first.NormalizedFailure == nil || *first.NormalizedFailure != failure {
		t.Fatalf("first transition normalized failure = %#v, want %q", first.NormalizedFailure, failure)
	}

	// A second transition for the SAME attempt gets the next ordinal.
	second, err := repositories.RecordEpisodeAttemptTransition(ctx, RecordEpisodeAttemptTransition{
		ID: "transition-2", EpisodeID: episode.ID, Attempt: 1, Gate: "assembly",
		NormalizedFailure: &failure, Rung: "gpt-5.6-luna/standard",
		Outcome: EpisodeAttemptOutcomeEscalated, IdempotencyKey: "transition-2-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Ordinal != 1 {
		t.Fatalf("second transition ordinal = %d, want 1", second.Ordinal)
	}

	// Attempt 2 gets its own independent ordinal sequence starting at 0.
	converged, err := repositories.RecordEpisodeAttemptTransition(ctx, RecordEpisodeAttemptTransition{
		ID: "transition-3", EpisodeID: episode.ID, Attempt: 2, Gate: "assembly",
		NormalizedFailure: nil, Rung: "gpt-5.6-sol/standard",
		Outcome: EpisodeAttemptOutcomeConverged, IdempotencyKey: "transition-3-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if converged.Ordinal != 0 {
		t.Fatalf("attempt-2 first transition ordinal = %d, want 0", converged.Ordinal)
	}
	if converged.NormalizedFailure != nil {
		t.Fatalf("converged transition normalized failure = %#v, want nil", converged.NormalizedFailure)
	}

	// Idempotent retry returns the existing row rather than a new one.
	retried, err := repositories.RecordEpisodeAttemptTransition(ctx, RecordEpisodeAttemptTransition{
		ID: "transition-1", EpisodeID: episode.ID, Attempt: 1, Gate: "assembly",
		NormalizedFailure: &failure, Rung: "gpt-5.6-luna/standard",
		Outcome: EpisodeAttemptOutcomeSentBack, IdempotencyKey: "transition-1-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if retried.Ordinal != first.Ordinal || retried.ID != first.ID {
		t.Fatalf("retried transition = %#v, want %#v", retried, first)
	}

	all, err := repositories.ListEpisodeAttemptTransitions(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("transitions = %#v, want 3", all)
	}
	if all[0].Attempt != 1 || all[1].Attempt != 1 || all[2].Attempt != 2 {
		t.Fatalf("transitions not ordered by attempt: %#v", all)
	}
}

// TestEpisodeAttemptTransitionsRequireFailurePresentExceptWhenConverged
// proves the storage-layer CHECK constraint discriminates correctly in both
// directions: a converged transition must carry no normalized failure, and
// every other outcome must carry one. Both are proven through the real
// repository call, and the second is additionally proven with a raw-SQL
// attack bypassing the repository entirely, matching this package's
// established pattern for schema-level rules.
func TestMEM002_EpisodeAttemptTransitionsRequireFailurePresentExceptWhenConverged(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5410)
	projectID := testProjectID(t, 5410)
	repositoryID := testRepositoryID(t, 5411)
	episode := mustOpenEpisode(t, repositories, 5413, projectID, repositoryID, task)

	failure := "tests failed"
	// A converged outcome carrying a failure is refused.
	if _, err := repositories.RecordEpisodeAttemptTransition(ctx, RecordEpisodeAttemptTransition{
		ID: "bad-1", EpisodeID: episode.ID, Attempt: 1, Gate: "assembly",
		NormalizedFailure: &failure, Rung: "gpt-5.6-luna/standard",
		Outcome: EpisodeAttemptOutcomeConverged, IdempotencyKey: "bad-1-key",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("converged transition with a failure error = %v, want ErrConstraint", err)
	}

	// A non-converged outcome carrying no failure is refused.
	if _, err := repositories.RecordEpisodeAttemptTransition(ctx, RecordEpisodeAttemptTransition{
		ID: "bad-2", EpisodeID: episode.ID, Attempt: 1, Gate: "assembly",
		NormalizedFailure: nil, Rung: "gpt-5.6-luna/standard",
		Outcome: EpisodeAttemptOutcomeSentBack, IdempotencyKey: "bad-2-key",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("sent-back transition with no failure error = %v, want ErrConstraint", err)
	}

	// Raw-SQL attack: bypass the repository entirely and attempt the same
	// invalid combination directly.
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO episode_attempt_transitions (
			id, episode_id, attempt, ordinal, gate, normalized_failure, rung, outcome,
			idempotency_key, recorded_at_unix_micros
		) VALUES ('bad-raw', ?, 1, 0, 'assembly', NULL, 'gpt-5.6-luna/standard', 'sent-back', 'bad-raw-key', 0)`,
		episode.ID,
	); !errors.Is(classify("raw invalid transition insert", err), ErrConstraint) {
		t.Fatalf("raw invalid transition insert error = %v, want ErrConstraint", err)
	}

	// The valid combination succeeds, proving the constraint discriminates
	// rather than rejecting everything.
	if _, err := repositories.RecordEpisodeAttemptTransition(ctx, RecordEpisodeAttemptTransition{
		ID: "good-1", EpisodeID: episode.ID, Attempt: 1, Gate: "assembly",
		NormalizedFailure: &failure, Rung: "gpt-5.6-luna/standard",
		Outcome: EpisodeAttemptOutcomeSentBack, IdempotencyKey: "good-1-key",
	}); err != nil {
		t.Fatal(err)
	}
}

// TestEpisodeAttemptTransitionsAreImmutable is the raw-SQL attack proving a
// recorded transition can never be edited or removed after the fact.
func TestMEM002_EpisodeAttemptTransitionsAreImmutable(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5420)
	projectID := testProjectID(t, 5420)
	repositoryID := testRepositoryID(t, 5421)
	episode := mustOpenEpisode(t, repositories, 5423, projectID, repositoryID, task)

	failure := "tests failed"
	transition, err := repositories.RecordEpisodeAttemptTransition(ctx, RecordEpisodeAttemptTransition{
		ID: "immutable-1", EpisodeID: episode.ID, Attempt: 1, Gate: "assembly",
		NormalizedFailure: &failure, Rung: "gpt-5.6-luna/standard",
		Outcome: EpisodeAttemptOutcomeSentBack, IdempotencyKey: "immutable-1-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE episode_attempt_transitions SET outcome = 'converged' WHERE id = ?`, transition.ID,
	); !errors.Is(classify("raw mutate transition", err), ErrConstraint) {
		t.Fatalf("raw UPDATE of a transition error = %v, want ErrConstraint", err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx, `DELETE FROM episode_attempt_transitions WHERE id = ?`, transition.ID,
	); !errors.Is(classify("raw delete transition", err), ErrConstraint) {
		t.Fatalf("raw DELETE of a transition error = %v, want ErrConstraint", err)
	}
}
