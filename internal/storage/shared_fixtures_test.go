package storage

// This file holds test fixture helpers that are shared between one or more
// files tagged `integration` (moved out of test-fast because they are
// measurably heavy) and one or more files that stay untagged (fast) and
// still call them. It carries no build tag on purpose: both the untagged
// (`go test ./...`) and the tagged (`go test -tags=integration ./...`)
// builds must see these symbols. See REPO-030a.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/validation"
)

// testWorkspaceID was defined in worktree_repository_test.go (now tagged
// integration) but is also needed by checkpoint_repository_test.go,
// frontend_route_access_test.go, and task_service_repository_test.go, all of
// which stay untagged.
func testWorkspaceID(t *testing.T, number int) domain.WorkspaceID {
	t.Helper()
	id, err := domain.ParseWorkspaceID("wsp_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// setTaskRunStates was defined in startup_recovery_test.go (now tagged
// integration) but is also needed by queue_repository_test.go, which stays
// untagged.
func setTaskRunStates(
	t *testing.T,
	repositories *Repositories,
	taskID domain.TaskID,
	runID domain.RunID,
	taskState domain.TaskState,
	runState domain.RunState,
) {
	t.Helper()
	if _, err := repositories.database.sql.ExecContext(
		context.Background(),
		`UPDATE tasks SET state = ? WHERE id = ?`,
		taskState,
		taskID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		context.Background(),
		`UPDATE runs SET state = ? WHERE id = ? AND task_id = ?`,
		runState,
		runID,
		taskID,
	); err != nil {
		t.Fatal(err)
	}
}

// openMigrationTestDatabase was defined in migrate_test.go (now tagged
// integration) but is also needed by atom_documentation_naming_migration_test.go,
// episode_chronological_migration_test.go, schema_test.go, and
// thread_session_migration_test.go, all of which stay untagged.
func openMigrationTestDatabase(t *testing.T) *Database {
	t.Helper()
	database, err := Open(context.Background(), OpenOptions{
		Path: filepath.Join(t.TempDir(), "codeflux.sqlite3"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close migration database: %v", err)
		}
	})
	return database
}

// validContextManifestInput was defined in context_manifest_repository_test.go
// (now tagged integration) but is also needed by agent_plan_repository_test.go,
// which stays untagged.
func validContextManifestInput(repositoryID domain.RepositoryID) RecordContextManifest {
	content := "package service\n\nfunc Greet() string { return \"hi\" }"
	history := strings.Repeat("a", 40)
	return RecordContextManifest{
		ID:                  strings.Repeat("1", 64),
		RepositoryID:        repositoryID,
		RepositoryRevision:  strings.Repeat("2", 40),
		MapRevision:         strings.Repeat("3", 64),
		RequirementSHA256:   strings.Repeat("4", 64),
		SelectionPolicy:     1,
		MaxFiles:            8,
		MaxBytes:            16 << 10,
		MaxEstimatedTokens:  4 << 10,
		UsedFiles:           2,
		UsedBytes:           len(content) + len(history),
		UsedEstimatedTokens: 22,
		Items: []ContextManifestItem{
			{
				Path:            "service/service.go",
				Kind:            "source",
				StartLine:       1,
				EndLine:         3,
				ContentRedacted: content,
				ContentSHA256:   strings.Repeat("5", 64),
				Reasons:         []string{"explicit-path", "exact-symbol-term:Greet"},
				Trust:           contextManifestTrust,
				EstimatedTokens: 12,
			},
			{
				Path:            "service/service.go",
				Kind:            "history",
				ContentRedacted: history,
				ContentSHA256:   strings.Repeat("6", 64),
				Reasons:         []string{"recent-history-for-selected-path"},
				Trust:           contextManifestTrust,
				EstimatedTokens: 10,
			},
		},
		Exclusions: []ContextManifestExclusion{
			{Path: ".env", Reason: "likely-secret-path"},
		},
	}
}

// validationRunIntentFixture and validationRunResultFixture were defined in
// validation_run_repository_test.go (now tagged integration) but are also
// needed by evidence_report_repository_test.go and
// memory_fact_extraction_repository_test.go, both of which stay untagged.
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

// mustGraphSQL and insertTaskGraphFixture were defined in
// graph_schema_migration_test.go (now tagged integration) but are also
// needed by graph_query_repository_test.go, which stays untagged.
func mustGraphSQL(t *testing.T, database *Database, statement string, arguments ...any) {
	t.Helper()
	if _, err := database.sql.ExecContext(t.Context(), statement, arguments...); err != nil {
		t.Fatalf("graph SQL failed: %v\n%s", err, strings.TrimSpace(statement))
	}
}

func insertTaskGraphFixture(
	t *testing.T,
	fixture agentPlanFixture,
	projectID domain.ProjectID,
	graphID domain.GraphID,
	revisionID domain.GraphRevisionID,
	fromNodeID, toNodeID domain.NodeID,
	edgeID domain.EdgeID,
	eventID domain.EventID,
) {
	t.Helper()
	database := fixture.repositories.database
	mustGraphSQL(t, database, `
		INSERT INTO graphs (id, project_id, repository_id, created_at_unix_micros)
		VALUES (?, ?, ?, 1)`, graphID, projectID, fixture.task.RepositoryID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_task_bindings (
			graph_id, project_id, repository_id, thread_id, task_id,
			created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, 1)`, graphID, projectID,
		fixture.task.RepositoryID, fixture.task.ThreadID, fixture.task.ID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_revisions (
			id, graph_id, revision, parent_revision_id, graph_schema_version,
			metadata_policy, created_at_unix_micros
		) VALUES (?, ?, 1, NULL, 1, 'typed-fields-only', 1)`, revisionID, graphID)
	for _, nodeID := range []domain.NodeID{fromNodeID, toNodeID} {
		mustGraphSQL(t, database, `
			INSERT INTO graph_nodes (id, graph_id, created_at_unix_micros)
			VALUES (?, ?, 1)`, nodeID, graphID)
	}
	mustGraphSQL(t, database, `
		INSERT INTO graph_node_revisions (
			graph_id, graph_revision_id, node_id, node_class, status,
			display_name_redacted, contract_purpose_redacted, tombstoned,
			created_at_unix_micros
		) VALUES (?, ?, ?, 'requirement', 'active', 'Accepted requirement',
			'Explain the accepted user requirement.', 0, 1)`, graphID, revisionID, fromNodeID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_node_revisions (
			graph_id, graph_revision_id, node_id, node_class, status,
			display_name_redacted, contract_purpose_redacted, tombstoned,
			created_at_unix_micros
		) VALUES (?, ?, ?, 'obligation', 'pending', 'Run targeted tests',
			'Record whether the targeted checks pass.', 0, 1)`, graphID, revisionID, toNodeID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_node_contract_items (
			graph_revision_id, node_id, item_kind, ordinal, value_redacted
		) VALUES (?, ?, 'output', 0, 'A bounded validation result.')`, revisionID, toNodeID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_edges (id, graph_id, created_at_unix_micros)
		VALUES (?, ?, 1)`, edgeID, graphID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_edge_revisions (
			graph_id, graph_revision_id, edge_id, edge_class,
			source_node_id, target_node_id, tombstoned,
			created_at_unix_micros
		) VALUES (?, ?, ?, 'evidence-dependency', ?, ?, 0, 1)`,
		graphID, revisionID, edgeID, fromNodeID, toNodeID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_plan_bindings (
			graph_id, graph_revision_id, task_id, plan_revision,
			created_at_unix_micros
		) VALUES (?, ?, ?, ?, 1)`, graphID, revisionID, fixture.task.ID, fixture.plan.Revision)
	stepID := fixture.plan.Plan.Steps[0].ID
	mustGraphSQL(t, database, `
		INSERT INTO graph_revision_plan_step_links (
			graph_id, graph_revision_id, task_id, plan_revision, step_id, ordinal
		) VALUES (?, ?, ?, ?, ?, 0)`, graphID, revisionID, fixture.task.ID, fixture.plan.Revision, stepID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_node_plan_step_links (
			graph_id, graph_revision_id, node_id, task_id, plan_revision,
			step_id, ordinal
		) VALUES (?, ?, ?, ?, ?, ?, 0)`, graphID, revisionID, fromNodeID,
		fixture.task.ID, fixture.plan.Revision, stepID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_edge_plan_step_links (
			graph_id, graph_revision_id, edge_id, task_id, plan_revision,
			step_id, ordinal
		) VALUES (?, ?, ?, ?, ?, ?, 0)`, graphID, revisionID, edgeID,
		fixture.task.ID, fixture.plan.Revision, stepID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_revision_event_links (
			graph_id, graph_revision_id, task_id, event_id, ordinal
		) VALUES (?, ?, ?, ?, 0)`, graphID, revisionID, fixture.task.ID, eventID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_node_event_links (
			graph_id, graph_revision_id, node_id, task_id, event_id, ordinal
		) VALUES (?, ?, ?, ?, ?, 0)`, graphID, revisionID, fromNodeID, fixture.task.ID, eventID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_edge_event_links (
			graph_id, graph_revision_id, edge_id, task_id, event_id, ordinal
		) VALUES (?, ?, ?, ?, ?, 0)`, graphID, revisionID, edgeID, fixture.task.ID, eventID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_message_links (
			graph_id, graph_revision_id, node_id, task_id, thread_id,
			message_id, ordinal
		) VALUES (?, ?, ?, ?, ?, ?, 0)`, graphID, revisionID, fromNodeID,
		fixture.task.ID, fixture.task.ThreadID, fixture.message.ID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_source_links (
			graph_id, graph_revision_id, node_id, repository_id,
			repository_revision, repository_relative_path,
			start_line, start_column, end_line, end_column, ordinal
		) VALUES (?, ?, ?, ?, ?, 'internal/widget.go', 1, 1, 12, 2, 0)`,
		graphID, revisionID, fromNodeID, fixture.task.RepositoryID, fixture.plan.RepositoryRevision)
	mustGraphSQL(t, database, `
		INSERT INTO graph_layout_hints (
			graph_id, graph_revision_id, node_id, algorithm, algorithm_version,
			x_milli, y_milli, width_milli, height_milli, rank, sibling_order,
			created_at_unix_micros
		) VALUES (?, ?, ?, 'layered-ltr', 1, 1000, 2000, 240000, 80000, 0, 0, 1)`,
		graphID, revisionID, fromNodeID)
}

// testSessionID and createSessionEventFixture were defined in
// session_event_repository_test.go (now tagged integration) but are also
// needed by frontend_telemetry_repository_test.go,
// session_projection_notification_repository_test.go, and
// task_service_repository_test.go, all of which stay untagged.
func testSessionID(t *testing.T, number int) domain.SessionID {
	t.Helper()
	id, err := domain.ParseSessionID("ses_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func createSessionEventFixture(
	t *testing.T,
	base int,
) (*Repositories, Task, domain.SessionID) {
	t.Helper()
	repositories, task := createTaskFixture(t, base)
	sessionID := testSessionID(t, base+4)
	session, err := repositories.CreateSession(context.Background(), CreateSession{
		ID:       sessionID,
		ThreadID: task.ThreadID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != sessionID || session.ThreadID != task.ThreadID {
		t.Fatalf("session = %#v", session)
	}
	return repositories, task, sessionID
}
