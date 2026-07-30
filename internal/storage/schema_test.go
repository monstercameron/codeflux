package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitialOperationalSchemaContainsEveryRequiredTable(t *testing.T) {
	database := openMigratedSchema(t)
	expected := []string{
		"projects",
		"repositories",
		"workspaces",
		"worktree_bindings",
		"threads",
		"messages",
		"tasks",
		"runs",
		"task_events",
		"approvals",
		"permission_decisions",
		"providers",
		"model_catalog",
		"pricing_snapshots",
		"model_requests",
		"forecasts",
		"budgets",
		"usage_records",
		"command_executions",
		"redacted_outputs",
		"checkpoints",
		"recovery_attempts",
		"artifacts",
		"diff_summaries",
		"validations",
		"evidence",
		"sessions",
		"session_events",
		"session_snapshots",
		"session_commands",
		"context_manifests",
		"context_manifest_items",
		"context_manifest_exclusions",
		"run_tool_schemas",
		"permission_grant_uses",
		"custom_commands",
		"worker_leases",
		"worker_reports",
		"task_queue_entries",
		"provider_configuration_revisions",
		"provider_pricing_revisions",
		"provider_price_components",
		"provider_pricing_revision_seals",
		"provider_logical_requests",
		"provider_request_attempts",
		"provider_attempt_accounting",
		"provider_attempt_evidence",
		"provider_live_smoke_fixtures",
	}
	for _, table := range expected {
		var count int
		if err := database.sql.QueryRow(
			`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`,
			table,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s count = %d, want 1", table, count)
		}
	}
}

func TestInitialSchemaEnforcesLineageStateIdempotencyAndExactValues(t *testing.T) {
	ctx := context.Background()
	database := openMigratedSchema(t)
	seedOperationalParents(t, database)

	if _, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO repositories (
			id, project_id, canonical_path, git_identity,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES ('repo_missing', 'missing', '/missing', 'git-missing', 1, 1)`,
	); !errors.Is(classify("insert invalid repository", err), ErrConstraint) {
		t.Fatalf("foreign-key error = %v", err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO tasks (
			id, thread_id, repository_id, state, policy_preset,
			reasoning_effort, risk_level, required_assurance,
			idempotency_key, created_at_unix_micros, updated_at_unix_micros
		) VALUES (
			'task_bad', 'thread_1', 'repo_1', 'invented', 'balanced',
			'standard', 'routine', 'runtime-only', 'bad-state', 1, 1
		)`,
	); !errors.Is(classify("insert invalid task state", err), ErrConstraint) {
		t.Fatalf("task-state error = %v", err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO messages (
			id, thread_id, sequence, role, body_redacted,
			idempotency_key, created_at_unix_micros
		) VALUES ('message_duplicate', 'thread_1', 1, 'user', 'duplicate', 'other', 2)`,
	); !errors.Is(classify("insert duplicate message sequence", err), ErrConstraint) {
		t.Fatalf("message-sequence error = %v", err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO budgets (
			id, task_id, currency, warning_cost_minor, hard_stop_cost_minor,
			warning_tokens, hard_stop_tokens, warning_wall_clock_millis,
			hard_stop_wall_clock_millis, maximum_provider_calls,
			maximum_repair_rounds, maximum_tool_executions,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (
			'budget_bad', 'task_1', 'USD', -1, 100,
			0, 1, 0, 1, 1, 1, 1, 1, 1
		)`,
	); !errors.Is(classify("insert negative budget", err), ErrConstraint) {
		t.Fatalf("budget-value error = %v", err)
	}
}

func TestInitialSchemaProtectsImmutableRowsAndEventSequence(t *testing.T) {
	ctx := context.Background()
	database := openMigratedSchema(t)
	seedOperationalParents(t, database)
	if _, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO task_events (
			id, task_id, run_id, sequence, event_type, payload_json,
			idempotency_key, created_at_unix_micros
		) VALUES ('event_1', 'task_1', 'run_1', 1, 'task.created', '{}', 'event-key', 1)`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`UPDATE task_events SET event_type = 'rewritten' WHERE id = 'event_1'`,
	); !errors.Is(classify("rewrite event", err), ErrConstraint) {
		t.Fatalf("event immutability error = %v", err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`UPDATE messages SET body_redacted = 'rewritten' WHERE id = 'message_1'`,
	); !errors.Is(classify("rewrite message", err), ErrConstraint) {
		t.Fatalf("message immutability error = %v", err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO task_events (
			id, task_id, run_id, sequence, event_type, payload_json,
			idempotency_key, created_at_unix_micros
		) VALUES ('event_2', 'task_1', 'run_1', 1, 'duplicate', '{}', 'event-key-2', 2)`,
	); !errors.Is(classify("duplicate event sequence", err), ErrConstraint) {
		t.Fatalf("event-sequence error = %v", err)
	}
}

func TestInitialSchemaHasRequiredQueryIndexes(t *testing.T) {
	database := openMigratedSchema(t)
	expected := []string{
		"worktree_bindings_active",
		"worktree_bindings_by_task",
		"threads_for_pagination",
		"messages_for_pagination",
		"tasks_active",
		"task_events_for_replay",
		"approvals_pending",
		"model_requests_for_cost",
		"session_events_by_thread",
		"session_events_by_task",
		"session_snapshots_latest",
		"permission_decisions_by_task_action",
		"worker_leases_one_active_run",
		"worker_leases_heartbeat",
		"worker_reports_by_run",
		"task_queue_one_live_entry",
		"task_queue_dispatch_order",
		"provider_price_components_standard_unique",
		"provider_price_components_specific_unique",
		"provider_logical_requests_for_task",
		"provider_request_attempts_for_request",
		"provider_attempt_accounting_for_attempt",
		"provider_attempt_evidence_for_attempt",
	}
	for _, index := range expected {
		var count int
		if err := database.sql.QueryRow(
			`SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND name = ?`,
			index,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("index %s count = %d, want 1", index, count)
		}
	}
}

func TestInitialSchemaContainsNoCredentialColumns(t *testing.T) {
	database := openMigratedSchema(t)
	rows, err := database.sql.Query(
		`SELECT name FROM sqlite_schema WHERE type = 'table' ORDER BY name`,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"credential",
		"secret",
		"api_key",
		"access_token",
		"refresh_token",
		"password",
		"private_key",
		"authorization",
	}
	for _, table := range tables {
		columns, err := database.sql.Query(`PRAGMA table_info("` + table + `")`)
		if err != nil {
			t.Fatal(err)
		}
		for columns.Next() {
			var (
				position   int
				name       string
				columnType string
				notNull    int
				defaultSQL any
				primaryKey int
			)
			if err := columns.Scan(
				&position,
				&name,
				&columnType,
				&notNull,
				&defaultSQL,
				&primaryKey,
			); err != nil {
				columns.Close()
				t.Fatal(err)
			}
			lower := strings.ToLower(name)
			for _, fragment := range forbidden {
				if strings.Contains(lower, fragment) {
					columns.Close()
					t.Errorf("forbidden credential column %s.%s", table, name)
				}
			}
		}
		if err := columns.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func openMigratedSchema(t *testing.T) *Database {
	t.Helper()
	database := openMigrationTestDatabase(t)
	_, err := database.Migrate(context.Background(), MigrationOptions{
		ApplicationVersion: "schema-test",
		BackupDirectory:    filepath.Join(t.TempDir(), "backups"),
		AvailableBytes: func(string) (uint64, error) {
			return ^uint64(0), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func seedOperationalParents(t *testing.T, database *Database) {
	t.Helper()
	statements := []string{
		`INSERT INTO projects (
			id, name, created_at_unix_micros, updated_at_unix_micros
		) VALUES ('project_1', 'Project', 1, 1)`,
		`INSERT INTO repositories (
			id, project_id, canonical_path, git_identity,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES ('repo_1', 'project_1', '/repository', 'git-identity', 1, 1)`,
		`INSERT INTO threads (
			id, project_id, repository_id, title,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES ('thread_1', 'project_1', 'repo_1', 'Thread', 1, 1)`,
		`INSERT INTO messages (
			id, thread_id, sequence, role, body_redacted,
			idempotency_key, created_at_unix_micros
		) VALUES ('message_1', 'thread_1', 1, 'user', 'message', 'message-key', 1)`,
		`INSERT INTO tasks (
			id, thread_id, repository_id, request_message_id, state,
			policy_preset, reasoning_effort, risk_level, required_assurance,
			idempotency_key, created_at_unix_micros, updated_at_unix_micros
		) VALUES (
			'task_1', 'thread_1', 'repo_1', 'message_1', 'draft',
			'balanced', 'standard', 'routine', 'runtime-only',
			'task-key', 1, 1
		)`,
		`INSERT INTO runs (
			id, task_id, state, attempt, task_revision, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES ('run_1', 'task_1', 'pending', 1, 0, 'run-key', 1, 1)`,
	}
	for index, statement := range statements {
		if _, err := database.sql.ExecContext(
			context.Background(),
			statement,
		); err != nil {
			t.Fatalf("seed statement %d: %v", index, err)
		}
	}
}

func TestInitialSchemaPassesForeignKeyAndIntegrityChecks(t *testing.T) {
	database := openMigratedSchema(t)
	rows, err := database.sql.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign_key_check returned a violation")
	}
	if err := database.integrityCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	var foreignKeyCount int
	if err := database.sql.QueryRow(
		`SELECT count(*) FROM pragma_foreign_key_list('evidence')`,
	).Scan(&foreignKeyCount); err != nil {
		t.Fatal(err)
	}
	if foreignKeyCount < 2 {
		t.Fatalf("evidence foreign keys = %d, want at least 2", foreignKeyCount)
	}
}
