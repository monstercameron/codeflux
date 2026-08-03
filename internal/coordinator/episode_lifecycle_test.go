package coordinator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/validation"
)

// mustOpenRunEpisodeFixture builds a real project/repository/task and an
// AgentExecution with nothing but real repositories wired in, so
// openRunEpisode/closeRunEpisode/recordAttemptTransition can be exercised
// directly without constructing the rest of AgentExecution.Run's
// dependencies (a provider, a worktree, a graph service) that these three
// functions never touch. The returned path is the fixture's own SQLite
// file, for tests (MEM-001a) that need to move the task to a state no
// storage-layer method reachable from this package can produce directly --
// see mustSetTaskState.
func mustOpenRunEpisodeFixture(t *testing.T) (*AgentExecution, agentScope, domain.TaskID, string) {
	t.Helper()
	repositories, path := mustOpenEpisodeLifecycleRepositories(t)
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
	return execution, scope, taskID, path
}

// mustOpenEpisodeLifecycleRepositories opens a real, temporary, file-backed
// SQLite database with every migration applied, mirroring task_preflight_
// test.go's mustOpenRetrievalRepositories but also returning the database's
// own path, which mustSetTaskState (MEM-001a) needs and the shared helper
// does not expose.
func mustOpenEpisodeLifecycleRepositories(t *testing.T) (*storage.Repositories, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "episode-lifecycle.sqlite")
	database, err := storage.Open(ctx, storage.OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close episode lifecycle fixture database: %v", err)
		}
	})
	if _, err := database.Migrate(ctx, storage.MigrationOptions{
		ApplicationVersion: "episode-lifecycle-test",
		BackupDirectory:    filepath.Join(t.TempDir(), "backups"),
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	return repositories, path
}

// mustSetTaskState pokes a task's state directly through a second raw
// connection to the same on-disk SQLite database mustOpenRunEpisodeFixture
// built (WAL mode, so a second connection to the same file is safe). This
// is the same "raw SQL fixture setup" pattern internal/storage's own tests
// use to reach a state no exported repository method can produce with this
// little scaffolding (e.g. worktree_repository_test.go, startup_recovery_
// test.go), done from outside package storage because *storage.Repositories
// keeps its *sql.DB unexported.
func mustSetTaskState(t *testing.T, path string, taskID domain.TaskID, state domain.TaskState) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := raw.Close(); err != nil {
			t.Errorf("close raw task-state connection: %v", err)
		}
	}()
	if _, err := raw.ExecContext(context.Background(),
		`UPDATE tasks SET state = ? WHERE id = ?`, string(state), taskID.String(),
	); err != nil {
		t.Fatal(err)
	}
}

// mustCreateRunRow inserts a minimal runs row over the same raw connection
// pattern mustSetTaskState uses, so mustCreateAttributableValidationRun's
// validation_run_intents insert satisfies the validation_run_intents_task_
// consistency trigger (migration 000022), which requires runs.id/task_id to
// already agree with the intent it is about to accept. Mirrors internal/
// storage/tool_repository_test.go's createToolTestRun, built from raw SQL
// here because that helper is an unexported test-only symbol in a
// different package.
func mustCreateRunRow(t *testing.T, path string, taskID domain.TaskID, runID domain.RunID) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := raw.Close(); err != nil {
			t.Errorf("close raw run-row connection: %v", err)
		}
	}()
	if _, err := raw.ExecContext(context.Background(),
		`INSERT INTO runs (
			id, task_id, state, attempt, task_revision, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, 'pending', 1, 0, ?, 1, 1)`,
		runID.String(), taskID.String(), "extraction-fixture-run-"+runID.String(),
	); err != nil {
		t.Fatal(err)
	}
}

// TestMEM001a_OpenRunEpisodeBindsFingerprintAndCloseAbandonsItWhenTheTaskNeverReachesReview
// proves the MEM-001 run-boundary wiring end to end, updated for MEM-001a's
// closing rule: a task whose exact fingerprint was bound at forecast time
// (RecordTaskExactFingerprintBinding, the same call ForecastTask now makes)
// gets a real, open episode carrying that exact fingerprint when its run
// starts, and -- because this fixture's task never reaches
// TaskStateAwaitingReview -- a real, closed episode with Abandoned outcome
// when its run ends: nobody ever had the chance to accept, reject, or
// request a repair, so MEM-001a's home for "the run stopped without a
// decision" applies. If closeRunEpisode still closed unconditionally
// (MEM-001's rule), this would still pass with Abandoned; the discriminating
// half of the rule -- that AwaitingReview leaves the episode OPEN instead --
// is TestMEM001a_CloseRunEpisodeLeavesTheEpisodeOpenWhileAwaitingReview.
func TestMEM001a_OpenRunEpisodeBindsFingerprintAndCloseAbandonsItWhenTheTaskNeverReachesReview(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID, _ := mustOpenRunEpisodeFixture(t)

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

	// mustCreateForecastFixtureTask leaves the task in TaskStateDraft; it is
	// never moved to AwaitingReview in this test, so closeRunEpisode must
	// close it rather than leave it open.
	execution.closeRunEpisode(ctx, episode, scope)

	closed, err := execution.repositories.GetEpisode(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != domain.EpisodeStatusClosed {
		t.Fatalf("episode status after closeRunEpisode = %s, want closed", closed.Status)
	}
	if closed.Outcome == nil || *closed.Outcome != domain.EpisodeOutcomeAbandoned {
		t.Fatalf("episode outcome = %#v, want Abandoned (the run ended without ever reaching a review decision)", closed.Outcome)
	}
	if closed.EndingRevision == nil || *closed.EndingRevision != scope.revision {
		t.Fatalf("episode ending revision = %#v, want %s", closed.EndingRevision, scope.revision)
	}
}

// TestMEM001a_CloseRunEpisodeLeavesTheEpisodeOpenWhileAwaitingReview proves
// the half of MEM-001a that actually fixes the "outcome column is always
// the same value" defect: a run whose task reached TaskStateAwaitingReview
// has a human decision still pending, so closeRunEpisode must leave that
// episode open rather than closing it (Unresolved or Abandoned) and putting
// it beyond episodes_immutable_after_close's reach forever. If the
// AwaitingReview guard in closeRunEpisode were removed, this test's episode
// would come back Closed/Abandoned instead of Open, and it would fail.
func TestMEM001a_CloseRunEpisodeLeavesTheEpisodeOpenWhileAwaitingReview(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID, path := mustOpenRunEpisodeFixture(t)
	hash := strings.Repeat("f", 64)
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

	mustSetTaskState(t, path, taskID, domain.TaskStateAwaitingReview)

	execution.closeRunEpisode(ctx, episode, scope)

	stillOpen, err := execution.repositories.GetEpisode(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillOpen.Status != domain.EpisodeStatusOpen {
		t.Fatalf("episode status after closeRunEpisode while AwaitingReview = %s, want open", stillOpen.Status)
	}
	if stillOpen.Outcome != nil {
		t.Fatalf("episode outcome = %#v, want nil (still open)", stillOpen.Outcome)
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
	execution, scope, taskID, _ := mustOpenRunEpisodeFixture(t)

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
	execution, scope, taskID, _ := mustOpenRunEpisodeFixture(t)
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
	execution, scope, taskID, _ := mustOpenRunEpisodeFixture(t)
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
	execution, _, _, _ := mustOpenRunEpisodeFixture(t)
	execution.recordAttemptTransition(context.Background(), storage.Episode{}, 1,
		"assembly", "it did not compile", "gpt-5.6-luna/standard", storage.EpisodeAttemptOutcomeSentBack)
}

// -----------------------------------------------------------------------
// MEM-006/MEM-008/MEM-011a: ExtractDeterministicFactsAtEpisodeClose.
// -----------------------------------------------------------------------

// mustCreateAttributableValidationRun builds and commits one real
// validation_run_intents/validation_run_results pair (argv baked into
// command_definition_json, exactly like internal/storage's own
// memory_fact_extraction_repository_test.go fixture) and records the
// episode fact reference an Extract*FromEpisode function requires to ever
// see it -- built from exported storage/validation API only, since
// internal/storage's equivalent helpers are unexported test-only symbols in
// a different package.
func mustCreateAttributableValidationRun(
	t *testing.T,
	repositories *storage.Repositories,
	episode storage.Episode,
	taskID domain.TaskID,
	runID domain.RunID,
	number int,
	checkClass validation.CheckClass,
	argv []string,
	diffIdentity string,
) validation.RunIntent {
	t.Helper()
	ctx := context.Background()

	validationID, err := domain.NewValidationID()
	if err != nil {
		t.Fatal(err)
	}
	argvJSON := `{"arguments":[`
	for index, argument := range argv {
		if index > 0 {
			argvJSON += ","
		}
		argvJSON += `"` + argument + `"`
	}
	argvJSON += `]}`

	intent, err := validation.SealRunIntent(validation.RunIntent{
		ID: validationID, TaskID: taskID, RunID: runID,
		ProfileName: validation.ProfileProtected, ProfileVersion: validation.ProfileVersionV1,
		ProfileDigest: strings.Repeat("d", 64), CheckID: fmt.Sprintf("extraction-fixture-%d", number),
		CheckClass: checkClass, Required: true,
		WorktreeRevision: strings.Repeat("e", 40), DiffIdentity: diffIdentity,
		CommandDefinitionJSON: argvJSON,
		CommandFingerprint:    strings.Repeat("f", 64),
		Executable:            validation.ExecutableIdentity{Path: "C:/tool/go.exe", SHA256: strings.Repeat("1", 64)},
		Timeout:               5 * time.Minute,
		IdempotencyKey:        fmt.Sprintf("extraction-fixture-intent-%d", number),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateValidationRunIntent(ctx, intent); err != nil {
		t.Fatal(err)
	}

	result, err := validation.SealRunResult(validation.RunResult{
		ValidationRunID: intent.ID, State: domain.ValidationStatePassed, ExitCode: 0,
		Duration: time.Second, ParserName: "go-test-v1", ParseSucceeded: true,
		ParsedResultJSON:     `{}`,
		RawRedactedSummary:   "extraction fixture",
		ObservedDiffIdentity: diffIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CommitValidationRunResult(ctx, result); err != nil {
		t.Fatal(err)
	}

	factID := fmt.Sprintf("fact-%d-%s", number, episode.ID.String())
	if _, err := repositories.RecordEpisodeFactReference(ctx, storage.RecordEpisodeFactReference{
		ID: factID, EpisodeID: episode.ID, FactTaskID: taskID,
		Kind: domain.EpisodeFactKindValidationResult, ReferenceID: intent.ID.String(),
		IdempotencyKey: factID,
	}); err != nil {
		t.Fatal(err)
	}
	return intent
}

// TestMEM006_ExtractDeterministicFactsAtEpisodeCloseExtractsBuildAndTestCommandsWhenAccepted
// proves the positive case end to end against a real SQLite fixture: an
// episode closed Accepted, carrying one attributable passing CheckBuild and
// one attributable passing CheckBroadTest, yields exactly one reviewed-
// command memory fact for each purpose, each promoted to Validated by its
// own corroborated evidence (never inherited).
func TestMEM006_ExtractDeterministicFactsAtEpisodeCloseExtractsBuildAndTestCommandsWhenAccepted(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID, path := mustOpenRunEpisodeFixture(t)
	hash := strings.Repeat("a", 64)
	if _, err := execution.repositories.RecordTaskExactFingerprintBinding(ctx, storage.RecordTaskExactFingerprintBinding{
		TaskID: taskID, FingerprintSchemaVersion: 1, FingerprintHash: hash,
	}); err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	mustCreateRunRow(t, path, taskID, runID)
	episode := execution.openRunEpisode(ctx, scope, taskID, runID, 1)
	if episode.ID.IsZero() {
		t.Fatal("expected a real episode for this fixture")
	}

	diffIdentity := strings.Repeat("1", 64)
	mustCreateAttributableValidationRun(t, execution.repositories, episode, taskID, runID, 1,
		validation.CheckBuild, []string{"go", "build", "./..."}, diffIdentity)
	mustCreateAttributableValidationRun(t, execution.repositories, episode, taskID, runID, 2,
		validation.CheckBroadTest, []string{"go", "test", "./..."}, diffIdentity)

	closed, err := execution.repositories.CloseEpisode(ctx, storage.CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: diffIdentity, Outcome: domain.EpisodeOutcomeAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}

	summary, err := ExtractDeterministicFactsAtEpisodeClose(ctx, execution.repositories, closed)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.BuildCommands) != 1 {
		t.Fatalf("build commands = %#v, want exactly one", summary.BuildCommands)
	}
	if summary.BuildCommands[0].Upsert.Maturity != domain.MaturityStateValidated {
		t.Fatalf("build command maturity = %s, want validated from its own corroborated evidence", summary.BuildCommands[0].Upsert.Maturity)
	}
	if len(summary.TestCommands) != 1 {
		t.Fatalf("test commands = %#v, want exactly one", summary.TestCommands)
	}
	if len(summary.RepositoryConventions) != 0 {
		t.Fatalf("repository conventions = %#v, want none: no formatter/static-analysis check was recorded", summary.RepositoryConventions)
	}
	// MEM-011a: the call ran three real extractors against a real database,
	// so the measured cost must not be negative (a wall-clock bug would show
	// up as exactly that); it is not asserted strictly positive because a
	// fast machine can legitimately complete inside the clock's resolution.
	if summary.Duration < 0 {
		t.Fatalf("duration = %s, want a non-negative measured cost (MEM-011a)", summary.Duration)
	}
}

// TestMEM008_ExtractDeterministicFactsAtEpisodeCloseExtractsRepositoryConventionsWhenAccepted
// proves MEM-008: an attributable passing CheckFormatter check on an
// Accepted episode yields a repository-convention memory fact.
func TestMEM008_ExtractDeterministicFactsAtEpisodeCloseExtractsRepositoryConventionsWhenAccepted(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID, path := mustOpenRunEpisodeFixture(t)
	hash := strings.Repeat("b", 64)
	if _, err := execution.repositories.RecordTaskExactFingerprintBinding(ctx, storage.RecordTaskExactFingerprintBinding{
		TaskID: taskID, FingerprintSchemaVersion: 1, FingerprintHash: hash,
	}); err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	mustCreateRunRow(t, path, taskID, runID)
	episode := execution.openRunEpisode(ctx, scope, taskID, runID, 1)
	if episode.ID.IsZero() {
		t.Fatal("expected a real episode for this fixture")
	}

	diffIdentity := strings.Repeat("2", 64)
	mustCreateAttributableValidationRun(t, execution.repositories, episode, taskID, runID, 1,
		validation.CheckFormatter, []string{"gofmt", "-l", "."}, diffIdentity)

	closed, err := execution.repositories.CloseEpisode(ctx, storage.CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: diffIdentity, Outcome: domain.EpisodeOutcomeAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}

	summary, err := ExtractDeterministicFactsAtEpisodeClose(ctx, execution.repositories, closed)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.RepositoryConventions) != 1 {
		t.Fatalf("repository conventions = %#v, want exactly one", summary.RepositoryConventions)
	}
	if len(summary.BuildCommands) != 0 || len(summary.TestCommands) != 0 {
		t.Fatalf("build/test commands = %#v/%#v, want none: no build/test check was recorded", summary.BuildCommands, summary.TestCommands)
	}
}

// TestMEM006_ExtractDeterministicFactsAtEpisodeCloseRefusesAnUnacceptedEpisode
// is the discriminating half of the Accepted-only gate: the SAME
// attributable, passing CheckBuild check as the positive test above, but
// the episode closes Rejected. The storage-layer extractor itself does not
// filter by outcome (only by domain.EpisodeStatusClosed -- proven by
// TestExtractAttributableBuildCommandsFromEpisodeExtractsPassingCheckBuildOnly
// in internal/storage, which closes its fixture Accepted only incidentally),
// so this failing without the coordinator-level Outcome check, and passing
// with it, is what proves the guard in ExtractDeterministicFactsAtEpisode
// Close -- not the extractor -- is what refuses a rejected episode's work.
func TestMEM006_ExtractDeterministicFactsAtEpisodeCloseRefusesAnUnacceptedEpisode(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID, path := mustOpenRunEpisodeFixture(t)
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
	mustCreateRunRow(t, path, taskID, runID)
	episode := execution.openRunEpisode(ctx, scope, taskID, runID, 1)
	if episode.ID.IsZero() {
		t.Fatal("expected a real episode for this fixture")
	}

	diffIdentity := strings.Repeat("3", 64)
	mustCreateAttributableValidationRun(t, execution.repositories, episode, taskID, runID, 1,
		validation.CheckBuild, []string{"go", "build", "./..."}, diffIdentity)

	closed, err := execution.repositories.CloseEpisode(ctx, storage.CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: diffIdentity, Outcome: domain.EpisodeOutcomeRejected,
	})
	if err != nil {
		t.Fatal(err)
	}

	summary, err := ExtractDeterministicFactsAtEpisodeClose(ctx, execution.repositories, closed)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.BuildCommands) != 0 || len(summary.TestCommands) != 0 || len(summary.RepositoryConventions) != 0 {
		t.Fatalf("summary = %#v, want a no-op: the episode was Rejected, not Accepted", summary)
	}
	if summary.Duration != 0 {
		t.Fatalf("duration = %s, want zero: no extractor ran, so no cost was spent", summary.Duration)
	}
}

// TestMEM006_ExtractDeterministicFactsAtEpisodeCloseIsANoOpForAnOpenEpisode
// proves the second guard: an episode that is still Open (never closed)
// is refused before any extractor runs, rather than relying on the
// extractors' own domain.EpisodeStatusClosed check to surface as an error.
func TestMEM006_ExtractDeterministicFactsAtEpisodeCloseIsANoOpForAnOpenEpisode(t *testing.T) {
	ctx := context.Background()
	execution, scope, taskID, _ := mustOpenRunEpisodeFixture(t)
	hash := strings.Repeat("9", 64)
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
	if episode.Status != domain.EpisodeStatusOpen {
		t.Fatalf("episode status = %s, want open (never closed in this test)", episode.Status)
	}

	summary, err := ExtractDeterministicFactsAtEpisodeClose(ctx, execution.repositories, episode)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.BuildCommands) != 0 || len(summary.TestCommands) != 0 || len(summary.RepositoryConventions) != 0 {
		t.Fatalf("summary = %#v, want a no-op for a still-open episode", summary)
	}
}

// TestMEM006_ExtractDeterministicFactsAtEpisodeCloseIsANoOpForAZeroEpisode
// proves the nil/zero-value safety every other best-effort function in this
// file has: called with a zero storage.Episode (what openRunEpisode returns
// when it could not open one), this must not panic or attempt a write.
func TestMEM006_ExtractDeterministicFactsAtEpisodeCloseIsANoOpForAZeroEpisode(t *testing.T) {
	execution, _, _, _ := mustOpenRunEpisodeFixture(t)
	summary, err := ExtractDeterministicFactsAtEpisodeClose(context.Background(), execution.repositories, storage.Episode{})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.BuildCommands) != 0 {
		t.Fatalf("summary = %#v, want a no-op for a zero episode", summary)
	}
}
