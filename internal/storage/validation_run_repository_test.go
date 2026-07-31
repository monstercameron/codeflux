package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/validation"
)

func TestValidationRunRecordsAreImmutableAndIdempotent(t *testing.T) {
	t.Parallel()
	repositories, task := createTaskFixture(t, 22100)
	runID := createToolTestRun(t, repositories, task.ID, 22101)
	intent := validationRunIntentFixture(t, task.ID, runID, 22102, strings.Repeat("a", 64), "validation-intent-1")

	created, err := repositories.CreateValidationRunIntent(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.CreateValidationRunIntent(context.Background(), intent)
	if err != nil || retried.IntentDigest != created.IntentDigest || retried.CreatedAt != created.CreatedAt {
		t.Fatalf("idempotent intent = %#v, %v; want %#v", retried, err, created)
	}
	if _, err := repositories.database.sql.ExecContext(context.Background(), `UPDATE validation_run_intents SET check_id = 'forged' WHERE id = ?`, intent.ID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("intent mutation error = %v, want immutable rejection", err)
	}

	result := validationRunResultFixture(t, intent.ID, intent.DiffIdentity)
	committed, err := repositories.CommitValidationRunResult(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	retriedResult, err := repositories.CommitValidationRunResult(context.Background(), result)
	if err != nil || retriedResult.ResultDigest != committed.ResultDigest || retriedResult.CompletedAt != committed.CompletedAt {
		t.Fatalf("idempotent result = %#v, %v; want %#v", retriedResult, err, committed)
	}
	if _, err := repositories.database.sql.ExecContext(context.Background(), `UPDATE validation_run_results SET state = 'failed' WHERE validation_run_id = ?`, intent.ID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("result mutation error = %v, want immutable rejection", err)
	}
}

func TestValidationInvalidatesAfterOneLinePostTestEditAndRequiresRerun(t *testing.T) {
	t.Parallel()
	repositories, task := createTaskFixture(t, 22200)
	runID := createToolTestRun(t, repositories, task.ID, 22201)
	worktree := t.TempDir()
	sourcePath := filepath.Join(worktree, "widget.go")
	if err := os.WriteFile(sourcePath, []byte("package widget\n\nconst enabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	passedDiff := fileIdentity(t, sourcePath)
	intent := validationRunIntentFixture(t, task.ID, runID, 22202, passedDiff, "post-test-before-edit")
	if _, err := repositories.CreateValidationRunIntent(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CommitValidationRunResult(context.Background(), validationRunResultFixture(t, intent.ID, passedDiff)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("package widget\n\nconst enabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	editedDiff := fileIdentity(t, sourcePath)
	if editedDiff == passedDiff {
		t.Fatal("one-line post-test edit did not change the diff identity fixture")
	}

	invalidations, err := repositories.InvalidateValidationRuns(context.Background(), runID, editedDiff)
	if err != nil {
		t.Fatal(err)
	}
	if len(invalidations) != 1 || invalidations[0].ValidationRunID != intent.ID ||
		invalidations[0].PreviousDiffIdentity != passedDiff || invalidations[0].CurrentDiffIdentity != editedDiff {
		t.Fatalf("invalidations = %#v", invalidations)
	}
	retriedInvalidations, err := repositories.InvalidateValidationRuns(context.Background(), runID, editedDiff)
	if err != nil || len(retriedInvalidations) != 0 {
		t.Fatalf("idempotent invalidations = %#v, %v; want no duplicate event", retriedInvalidations, err)
	}
	reruns, err := repositories.ListRequiredValidationReruns(context.Background(), runID, editedDiff)
	if err != nil {
		t.Fatal(err)
	}
	if len(reruns) != 1 || reruns[0].CheckID != intent.CheckID || reruns[0].PreviousRunID != intent.ID {
		t.Fatalf("required reruns = %#v", reruns)
	}

	rerun := validationRunIntentFixture(t, task.ID, runID, 22203, editedDiff, "post-test-after-edit")
	rerun.CheckID = intent.CheckID
	rerun, err = validation.SealRunIntent(rerun)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateValidationRunIntent(context.Background(), rerun); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CommitValidationRunResult(context.Background(), validationRunResultFixture(t, rerun.ID, editedDiff)); err != nil {
		t.Fatal(err)
	}
	reruns, err = repositories.ListRequiredValidationReruns(context.Background(), runID, editedDiff)
	if err != nil {
		t.Fatal(err)
	}
	if len(reruns) != 0 {
		t.Fatalf("required reruns after current-diff pass = %#v", reruns)
	}
}

func fileIdentity(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func validationRunIntentFixture(t *testing.T, taskID domain.TaskID, runID domain.RunID, number int, diff, key string) validation.RunIntent {
	t.Helper()
	id := testValidationID(t, number)
	intent, err := validation.SealRunIntent(validation.RunIntent{
		ID: id, TaskID: taskID, RunID: runID,
		ProfileName: validation.ProfileProtected, ProfileVersion: validation.ProfileVersionV1,
		ProfileDigest: strings.Repeat("d", 64), CheckID: "targeted-test-fixture",
		CheckClass: validation.CheckTargetedTest, Required: true,
		WorktreeRevision: strings.Repeat("e", 40), DiffIdentity: diff,
		CommandDefinitionJSON: `{"arguments":["go","test","./..."]}`,
		CommandFingerprint:    strings.Repeat("f", 64),
		Executable:            validation.ExecutableIdentity{Path: "C:/tool/go.exe", SHA256: strings.Repeat("1", 64)},
		Timeout:               5 * time.Minute, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func validationRunResultFixture(t *testing.T, id domain.ValidationID, diff string) validation.RunResult {
	t.Helper()
	result, err := validation.SealRunResult(validation.RunResult{
		ValidationRunID: id, State: domain.ValidationStatePassed, ExitCode: 0,
		Duration: 1200 * time.Millisecond, ParserName: "go-test-v1", ParseSucceeded: true,
		ParsedResultJSON:   `{"packages":["codeflux.dev/fixture"],"tests":["TestFixture"]}`,
		RawRedactedSummary: "ok codeflux.dev/fixture", ObservedDiffIdentity: diff,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
