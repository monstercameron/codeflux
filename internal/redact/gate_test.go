package redact_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
)

func TestMockTaskLeavesKnownCredentialOutOfEveryBoundary(t *testing.T) {
	ctx := context.Background()
	knownMaterial := "mock-task-provider-material-never-persist"
	reference, err := credentials.NewReference("mock-provider", "gate")
	if err != nil {
		t.Fatal(err)
	}
	store, err := credentials.NewEnvironmentStore(
		map[credentials.Reference]string{reference: "MOCK_PROVIDER_API_KEY"},
		func(name string) (string, bool) {
			return knownMaterial, name == "MOCK_PROVIDER_API_KEY"
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Test(ctx, reference); err != nil {
		t.Fatal(err)
	}
	secret, err := store.Retrieve(ctx, reference)
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Destroy()
	pipeline, err := redact.NewPipeline([]credentials.Secret{secret}, redact.Limits{
		MaximumInputBytes:  32 * 1024,
		MaximumOutputBytes: 16 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()

	prompt, err := pipeline.Redact(
		redact.BoundaryPromptPersistence,
		"provider prompt accidentally included "+knownMaterial,
	)
	if err != nil {
		t.Fatal(err)
	}
	logged, err := pipeline.Redact(
		redact.BoundaryLogPersistence,
		`{"message":"provider failed with `+knownMaterial+`"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	ui, err := pipeline.Redact(
		redact.BoundaryUIDelivery,
		"visible failure "+knownMaterial,
	)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, _, err := pipeline.RedactFields(
		redact.BoundaryDiagnosticExport,
		map[string]any{
			"provider_error": "failure included " + knownMaterial,
			"api_key":        knownMaterial,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	databasePath := filepath.Join(root, "codeflux.sqlite3")
	database, err := storage.Open(ctx, storage.OpenOptions{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Migrate(ctx, storage.MigrationOptions{
		ApplicationVersion: "redaction-gate-test",
		BackupDirectory:    filepath.Join(root, "backups"),
		AvailableBytes: func(string) (uint64, error) {
			return ^uint64(0), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	projectID := mustProjectID(t)
	repositoryID := mustRepositoryID(t)
	threadID := mustThreadID(t)
	taskID := mustTaskID(t)
	if _, err := repositories.CreateProject(ctx, storage.CreateProject{
		ID:   projectID,
		Name: "Redaction gate",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(ctx, storage.CreateRepository{
		ID:            repositoryID,
		ProjectID:     projectID,
		CanonicalPath: "/redaction-gate",
		GitIdentity:   "redaction-gate",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID:           threadID,
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		Title:        "Redaction gate",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.AppendMessage(ctx, storage.AppendMessage{
		ID:             mustMessageID(t),
		ThreadID:       threadID,
		Role:           storage.MessageRoleUser,
		BodyRedacted:   prompt.Text,
		IdempotencyKey: "redacted-prompt",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateTask(ctx, storage.CreateTask{
		ID:                taskID,
		ThreadID:          threadID,
		RepositoryID:      repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		SettingsRevision:  0,
		IdempotencyKey:    "redaction-task",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.AppendTaskEvent(ctx, storage.AppendTaskEvent{
		ID:             mustEventID(t),
		TaskID:         taskID,
		EventType:      "provider.failure",
		PayloadJSON:    logged.Text,
		IdempotencyKey: "redacted-provider-failure",
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(ctx); err != nil {
		t.Fatal(err)
	}

	for name, output := range map[string][]byte{
		"prompt":      []byte(prompt.Text),
		"log":         []byte(logged.Text),
		"ui":          []byte(ui.Text),
		"diagnostics": diagnostics,
	} {
		if bytes.Contains(output, []byte(knownMaterial)) {
			t.Fatalf("%s retained known credential material", name)
		}
	}
	for _, candidate := range []string{
		databasePath,
		databasePath + "-wal",
		databasePath + "-shm",
	} {
		content, err := os.ReadFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(content, []byte(knownMaterial)) {
			t.Fatalf("SQLite artifact %q retained known credential material", candidate)
		}
	}
}

func mustProjectID(t *testing.T) domain.ProjectID {
	t.Helper()
	id, err := domain.NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRepositoryID(t *testing.T) domain.RepositoryID {
	t.Helper()
	id, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustThreadID(t *testing.T) domain.ThreadID {
	t.Helper()
	id, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustTaskID(t *testing.T) domain.TaskID {
	t.Helper()
	id, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustMessageID(t *testing.T) domain.MessageID {
	t.Helper()
	id, err := domain.NewMessageID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustEventID(t *testing.T) domain.EventID {
	t.Helper()
	id, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
