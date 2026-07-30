package storage

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestSettingsRevisionBindsTaskAndEffectiveRunConfiguration(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 600)
	repositoryID := testRepositoryID(t, 601)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	threadID := testThreadID(t, 602)
	if _, err := repositories.CreateThread(ctx, CreateThread{
		ID:           threadID,
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		Title:        "Configuration fixture",
	}); err != nil {
		t.Fatal(err)
	}
	approval := "repository-settings-review-1"
	revisionInput := CreateSettingsRevision{
		Scope:             SettingsScopeRepository,
		RepositoryID:      &repositoryID,
		ConfigurationJSON: validConfigurationJSON(),
		ApprovalReference: &approval,
		IdempotencyKey:    "repository-settings-1",
	}
	revision, err := repositories.CreateSettingsRevision(ctx, revisionInput)
	if err != nil {
		t.Fatal(err)
	}
	retriedRevision, err := repositories.CreateSettingsRevision(ctx, revisionInput)
	if err != nil {
		t.Fatal(err)
	}
	if !sameSettingsRevisionValues(retriedRevision, revision) {
		t.Fatalf("retried revision = %#v, want %#v", retriedRevision, revision)
	}
	readRevision, err := repositories.GetSettingsRevision(ctx, revision.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !sameSettingsRevisionValues(readRevision, revision) {
		t.Fatalf("read revision = %#v, want %#v", readRevision, revision)
	}

	task, err := repositories.CreateTask(ctx, CreateTask{
		ID:                testTaskID(t, 603),
		ThreadID:          threadID,
		RepositoryID:      repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		SettingsRevision:  revision.Revision,
		IdempotencyKey:    "configured-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	var boundRevision uint64
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT settings_revision FROM task_settings_bindings WHERE task_id = ?`,
		task.ID,
	).Scan(&boundRevision); err != nil {
		t.Fatal(err)
	}
	if boundRevision != revision.Revision {
		t.Fatalf("task settings revision = %d, want %d", boundRevision, revision.Revision)
	}

	runID := newConfigurationTestRunID(t)
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO runs (
			id, task_id, state, attempt, task_revision, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, 'pending', 1, 0, 'configured-run', 1, 1)`,
		runID,
		task.ID,
	); err != nil {
		t.Fatal(err)
	}
	recordInput := RecordRunConfiguration{
		RunID:            runID,
		SettingsRevision: revision.Revision,
		EffectiveJSON:    validConfigurationJSON(),
		SourcesJSON:      `{"provider_endpoint":"repository","policy_preset":"task"}`,
	}
	recorded, err := repositories.RecordRunConfiguration(ctx, recordInput)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.RecordRunConfiguration(ctx, recordInput)
	if err != nil {
		t.Fatal(err)
	}
	if retried != recorded {
		t.Fatalf("retried run configuration = %#v, want %#v", retried, recorded)
	}
	read, err := repositories.GetRunConfiguration(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if read != recorded {
		t.Fatalf("read run configuration = %#v, want %#v", read, recorded)
	}
}

func TestSettingsAndRunConfigurationRejectConflictsSecretsAndWrongBindings(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	userInput := CreateSettingsRevision{
		Scope:             SettingsScopeUser,
		ConfigurationJSON: validConfigurationJSON(),
		IdempotencyKey:    "user-settings-1",
	}
	revision, err := repositories.CreateSettingsRevision(ctx, userInput)
	if err != nil {
		t.Fatal(err)
	}
	changed := userInput
	changed.ConfigurationJSON = `{"policy_preset":"fast"}`
	if _, err := repositories.CreateSettingsRevision(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed settings retry error = %v", err)
	}
	secretBearing := userInput
	secretBearing.IdempotencyKey = "secret-settings"
	secretBearing.ConfigurationJSON = `{"provider":{"api_key":"fixture-value"}}`
	if _, err := repositories.CreateSettingsRevision(ctx, secretBearing); err == nil {
		t.Fatal("secret-bearing settings were persisted")
	}

	projectID := testProjectID(t, 610)
	repositoryID := testRepositoryID(t, 611)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	threadID := testThreadID(t, 612)
	if _, err := repositories.CreateThread(ctx, CreateThread{
		ID:           threadID,
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		Title:        "Binding fixture",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := repositories.CreateTask(ctx, CreateTask{
		ID:                testTaskID(t, 613),
		ThreadID:          threadID,
		RepositoryID:      repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		SettingsRevision:  revision.Revision,
		IdempotencyKey:    "binding-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	runID := newConfigurationTestRunID(t)
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO runs (
			id, task_id, state, attempt, task_revision, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, 'pending', 1, 0, 'binding-run', 1, 1)`,
		runID,
		task.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.RecordRunConfiguration(ctx, RecordRunConfiguration{
		RunID:            runID,
		SettingsRevision: 0,
		EffectiveJSON:    validConfigurationJSON(),
		SourcesJSON:      `{"policy_preset":"default"}`,
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("wrong binding error = %v, want constraint", err)
	}
	if _, err := repositories.RecordRunConfiguration(ctx, RecordRunConfiguration{
		RunID:            runID,
		SettingsRevision: revision.Revision,
		EffectiveJSON:    `{"nested":{"access-token":"fixture-value"}}`,
		SourcesJSON:      `{}`,
	}); err == nil {
		t.Fatal("secret-bearing effective run configuration was persisted")
	}
}

func validConfigurationJSON() string {
	return `{"provider_endpoint":"https://models.example.test/v1","hard_budget":{"currency":"USD","minor_units":10000},"request_timeout_ms":120000,"worktree_root":"/safe/worktrees","policy_preset":"balanced"}`
}

func newConfigurationTestRunID(t *testing.T) domain.RunID {
	t.Helper()
	id, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func sameSettingsRevisionValues(left, right SettingsRevision) bool {
	return left.Revision == right.Revision &&
		left.Scope == right.Scope &&
		sameRepositoryID(left.RepositoryID, right.RepositoryID) &&
		left.ConfigurationJSON == right.ConfigurationJSON &&
		left.ContentSHA256 == right.ContentSHA256 &&
		sameOptionalString(left.ApprovalReference, right.ApprovalReference) &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.CreatedAt.Equal(right.CreatedAt)
}
