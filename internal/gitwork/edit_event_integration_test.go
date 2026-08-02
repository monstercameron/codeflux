package gitwork

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	_ "modernc.org/sqlite"
)

// TestAUDIT012_AMediatedEditBatchCommitsARedactedEventToTheRealJournal covers
// AUDIT-012, reconciling M09-026.
//
// M09-026 recorded that mediated edits are routed through ApplyEditBatch and
// that a redacted summary reaches the ordered task journal. Every test that
// supported it bound memoryEditRecorder, and SetEditEventRecorder had no
// production caller at all, so the production service applied edit batches
// with no recorder attached and wrote nothing. This exercises the real
// StorageEditEventRecorder against a real migrated SQLite database.
func TestAUDIT012_AMediatedEditBatchCommitsARedactedEventToTheRealJournal(t *testing.T) {
	repositories, databasePath, taskID := seedRealTaskForEditEvents(t)

	base := t.TempDir()
	repositoryPath := filepath.Join(base, "repository")
	head := initializeGitRepository(t, repositoryPath)
	service := newTestService(
		t, filepath.Join(base, "worktrees"),
		newMemoryBindingStore(), bytes.Repeat([]byte{91}, 64),
	)

	recorder, err := NewStorageEditEventRecorder(repositories)
	if err != nil {
		t.Fatal(err)
	}
	service.SetEditEventRecorder(recorder)

	created, err := service.CreateTaskWorktree(t.Context(), CreateWorktreeInput{
		TaskID: taskID, RepositoryID: fixtureRepositoryID(t, 92),
		RepositoryPath: repositoryPath, BaseRevision: head,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding := created.Binding

	// A secret in the edited content. The summary must record that a file
	// changed without recording what it now says, so this is the value that
	// proves the event is redacted rather than merely short.
	const secretInContent = "not-a-real-key-audit012-must-not-be-journalled"
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{
			{Operation: MutationCreate, Path: "config.go", ExpectAbsent: true,
				Content: []byte("package main\n\nconst key = \"" + secretInContent + "\"\n")},
			{Operation: MutationCreate, Path: "notes.txt", ExpectAbsent: true,
				Content: []byte("second file\n")},
		},
	}); err != nil {
		t.Fatalf("apply edit batch: %v", err)
	}

	// The edits landed on disk.
	for _, relative := range []string{"config.go", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(binding.WorktreePath, relative)); err != nil {
			t.Fatalf("%s was not written: %v", relative, err)
		}
	}

	payload := findEditSummaryPayload(t, repositories, databasePath, taskID)
	if payload == "" {
		t.Fatalf("no %s event reached the journal", editBatchAppliedEventType)
	}

	// Redaction: the payload carries counts and a batch digest, never content.
	if strings.Contains(payload, secretInContent) {
		t.Error("the journalled edit summary contains the edited file's content")
	}
	var summary struct {
		BatchSHA256 string `json:"batch_sha256"`
		Created     int    `json:"created"`
		FileCount   int    `json:"file_count"`
	}
	if err := json.Unmarshal([]byte(payload), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Created != 2 || summary.FileCount != 2 {
		t.Errorf("summary counts do not describe the batch: %+v", summary)
	}
	if len(summary.BatchSHA256) != 64 {
		t.Errorf("summary carries no batch digest: %q", summary.BatchSHA256)
	}
}

// TestAUDIT012_AFailedEditBatchLeavesNeitherFilesNorAnEvent proves the
// atomicity half.
//
// A partially applied batch that still journalled a summary would be worse
// than no record: the journal would assert an edit the worktree does not have.
func TestAUDIT012_AFailedEditBatchLeavesNeitherFilesNorAnEvent(t *testing.T) {
	repositories, databasePath, taskID := seedRealTaskForEditEvents(t)

	base := t.TempDir()
	repositoryPath := filepath.Join(base, "repository")
	head := initializeGitRepository(t, repositoryPath)
	service := newTestService(
		t, filepath.Join(base, "worktrees"),
		newMemoryBindingStore(), bytes.Repeat([]byte{93}, 64),
	)
	recorder, err := NewStorageEditEventRecorder(repositories)
	if err != nil {
		t.Fatal(err)
	}
	service.SetEditEventRecorder(recorder)

	created, err := service.CreateTaskWorktree(t.Context(), CreateWorktreeInput{
		TaskID: taskID, RepositoryID: fixtureRepositoryID(t, 94),
		RepositoryPath: repositoryPath, BaseRevision: head,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The second edit escapes the worktree, so the batch must be refused
	// whole. The first edit is valid precisely so a non-atomic implementation
	// would leave it behind.
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{
			{Operation: MutationCreate, Path: "good.go", ExpectAbsent: true,
				Content: []byte("package main\n")},
			{Operation: MutationCreate, Path: "../escaped.go", ExpectAbsent: true,
				Content: []byte("package main\n")},
		},
	}); err == nil {
		t.Fatal("a batch containing a path escape was accepted")
	}

	if _, err := os.Stat(filepath.Join(created.Binding.WorktreePath, "good.go")); err == nil {
		t.Error("the valid half of a refused batch was left on disk")
	}
	if findEditSummaryPayload(t, repositories, databasePath, taskID) != "" {
		t.Fatal("a refused batch still journalled an applied-edit summary")
	}
}

// findEditSummaryPayload returns the payload of the applied-edit summary in the
// ordered task journal, or an empty string when none was recorded. It reads
// through the supported read-only inspection surface rather than raw SQL.
func findEditSummaryPayload(
	t *testing.T,
	repositories *storage.Repositories,
	databasePath string,
	taskID domain.TaskID,
) string {
	t.Helper()
	// Presence is asserted through the supported inspection surface.
	result, err := repositories.Inspect(t.Context(), storage.InspectionQuery{
		Entity: storage.InspectEvent,
		TaskID: taskID.String(),
		Limit:  200,
	})
	if err != nil {
		t.Fatalf("inspect events: %v", err)
	}
	present := false
	for _, row := range result.Rows {
		if row.Fields["event_type"] == editBatchAppliedEventType {
			present = true
		}
	}
	if !present {
		return ""
	}

	// The inspection projection deliberately omits payload_json, so the
	// redaction assertion reads the column directly. That is the right
	// direction for this test: it checks what was actually stored rather than
	// what a projection chose to show.
	connection, err := sql.Open("sqlite", "file:"+filepath.ToSlash(databasePath)+"?mode=ro")
	if err != nil {
		t.Fatalf("open database for payload read: %v", err)
	}
	defer func() { _ = connection.Close() }()
	var payload string
	if err := connection.QueryRowContext(t.Context(),
		`SELECT payload_json FROM task_events
		 WHERE task_id = ? AND event_type = ?
		 ORDER BY sequence DESC LIMIT 1`,
		taskID.String(), editBatchAppliedEventType,
	).Scan(&payload); err != nil {
		t.Fatalf("read edit summary payload: %v", err)
	}
	return payload
}

// seedRealTaskForEditEvents opens a migrated SQLite database and creates the
// parent rows a task event needs, so the journal write is exercised against
// real constraints rather than a map.
func seedRealTaskForEditEvents(t *testing.T) (*storage.Repositories, string, domain.TaskID) {
	t.Helper()
	root := t.TempDir()
	databasePath := filepath.Join(root, "codeflux.sqlite3")
	database, err := storage.Open(t.Context(), storage.OpenOptions{
		Path: databasePath,
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(t.Context()) })
	if _, err := database.Migrate(t.Context(), storage.MigrationOptions{
		ApplicationVersion: "audit-012-test",
		BackupDirectory:    filepath.Join(root, "backups"),
		AvailableBytes:     func(string) (uint64, error) { return ^uint64(0), nil },
	}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repositories, err := storage.NewRepositories(database, time.Now)
	if err != nil {
		t.Fatalf("build repositories: %v", err)
	}

	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	taskID, _ := domain.NewTaskID()
	if _, err := repositories.CreateProject(t.Context(), storage.CreateProject{
		ID: projectID, Name: "Audit 012",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(t.Context(), storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: filepath.Join(root, "repository"), GitIdentity: "audit-012-repository",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(t.Context(), storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Audit 012",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateTask(t.Context(), storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset:    domain.PolicyPresetBalanced,
		ReasoningEffort: domain.ReasoningEffortStandard, RiskLevel: domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly, SettingsRevision: 0,
		IdempotencyKey: "audit-012-task",
	}); err != nil {
		t.Fatal(err)
	}
	return repositories, databasePath, taskID
}
