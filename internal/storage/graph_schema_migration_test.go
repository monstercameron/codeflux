package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/migrations"
)

func TestTaskGraphStorageMigrationAppliesForwardFromPreviousSchema(t *testing.T) {
	database := openMigrationTestDatabase(t)
	sources, err := migrations.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) < 21 || sources[20].Descriptor.Name != "000020_task_graph_storage.sql" {
		t.Fatalf("migration sources = %#v", sources)
	}
	for _, source := range sources[:20] {
		if _, err := database.sql.ExecContext(t.Context(), source.SQL); err != nil {
			t.Fatalf("apply %s: %v", source.Descriptor.Name, err)
		}
	}
	var before int
	if err := database.sql.QueryRowContext(t.Context(), `
		SELECT count(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'graphs'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 0 {
		t.Fatal("graph schema existed before its forward migration")
	}
	if _, err := database.sql.ExecContext(t.Context(), sources[20].SQL); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"graphs", "graph_task_bindings", "graph_revisions", "graph_nodes",
		"graph_node_revisions", "graph_node_contract_items", "graph_edges",
		"graph_edge_revisions", "graph_plan_bindings",
		"graph_revision_plan_step_links", "graph_node_plan_step_links",
		"graph_edge_plan_step_links", "graph_revision_event_links",
		"graph_node_event_links", "graph_edge_event_links",
		"graph_message_links", "graph_source_links", "graph_revision_seals",
		"graph_layout_hints",
	} {
		var count int
		if err := database.sql.QueryRowContext(t.Context(), `
			SELECT count(*) FROM sqlite_master
			WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s count = %d", table, count)
		}
	}
	assertGraphDatabaseIntegrity(t, database)
}

func TestTaskGraphStorageConstraintsIndexesAndBackupRestore(t *testing.T) {
	fixture := createAgentPlanFixture(t, 19_200)
	database := fixture.repositories.database
	eventID, _ := domain.NewEventID()
	event, err := fixture.repositories.AppendTaskEvent(t.Context(), AppendTaskEvent{
		ID: eventID, TaskID: fixture.task.ID, EventType: "graph-projection-source",
		PayloadJSON: `{}`, IdempotencyKey: "graph-projection-source",
	})
	if err != nil {
		t.Fatal(err)
	}
	var projectID domain.ProjectID
	if err := database.sql.QueryRowContext(t.Context(),
		"SELECT project_id FROM threads WHERE id = ?", fixture.task.ThreadID,
	).Scan(&projectID); err != nil {
		t.Fatal(err)
	}

	graphID, _ := domain.NewGraphID()
	revisionID, _ := domain.NewGraphRevisionID()
	fromNodeID, _ := domain.NewNodeID()
	toNodeID, _ := domain.NewNodeID()
	edgeID, _ := domain.NewEdgeID()
	insertTaskGraphFixture(t, fixture, projectID, graphID, revisionID, fromNodeID, toNodeID, edgeID, event.ID)

	for _, index := range []string{
		"graph_task_bindings_task_slice", "graph_node_revisions_lookup",
		"graph_edge_revisions_from_neighbor", "graph_edge_revisions_to_neighbor",
		"graph_edge_revisions_evidence_cone", "graph_message_links_message",
	} {
		var count int
		if err := database.sql.QueryRowContext(t.Context(), `
			SELECT count(*) FROM sqlite_master
			WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("index %s count = %d", index, count)
		}
	}

	assertGraphSQLFails(t, database, `
		UPDATE graph_node_revisions SET status = 'passed'
		WHERE graph_revision_id = ? AND node_id = ?`, revisionID, fromNodeID)
	assertGraphSQLFails(t, database, `
		DELETE FROM graph_revisions WHERE id = ?`, revisionID)
	badRevisionID, _ := domain.NewGraphRevisionID()
	assertGraphSQLFails(t, database, `
		INSERT INTO graph_revisions (
			id, graph_id, revision, parent_revision_id, graph_schema_version,
			metadata_policy, created_at_unix_micros
		) VALUES (?, ?, 3, ?, 1, 'typed-fields-only', 3)`,
		badRevisionID, graphID, revisionID)
	missingNodeID, _ := domain.NewNodeID()
	badEdgeID, _ := domain.NewEdgeID()
	mustGraphSQL(t, database, `
		INSERT INTO graph_edges (id, graph_id, created_at_unix_micros)
		VALUES (?, ?, 2)`, badEdgeID, graphID)
	assertGraphSQLFails(t, database, `
		INSERT INTO graph_edge_revisions (
			graph_id, graph_revision_id, edge_id, edge_class,
			source_node_id, target_node_id, tombstoned,
			created_at_unix_micros
		) VALUES (?, ?, ?, 'control', ?, ?, 0, 2)`,
		graphID, revisionID, badEdgeID, fromNodeID, missingNodeID)
	assertGraphSQLFails(t, database, `
		INSERT INTO graph_source_links (
			graph_id, graph_revision_id, node_id, repository_id,
			repository_revision, repository_relative_path,
			start_line, start_column, end_line, end_column, ordinal
		) VALUES (?, ?, ?, ?, ?, '../escape.go', 1, 1, 1, 2, 9)`,
		graphID, revisionID, fromNodeID, fixture.task.RepositoryID,
		fixture.plan.RepositoryRevision)
	mustGraphSQL(t, database, `
		INSERT INTO graph_nodes (id, graph_id, created_at_unix_micros)
		VALUES (?, ?, 2)`, missingNodeID, graphID)
	assertGraphSQLFails(t, database, `
		INSERT INTO graph_node_revisions (
			graph_id, graph_revision_id, node_id, node_class, status,
			display_name_redacted, contract_purpose_redacted, tombstoned,
			created_at_unix_micros
		) VALUES (?, ?, ?, 'effect', 'active', 'Removed', '', 1, 2)`,
		graphID, revisionID, missingNodeID)
	mustGraphSQL(t, database, `
		INSERT INTO graph_node_revisions (
			graph_id, graph_revision_id, node_id, node_class, status,
			display_name_redacted, contract_purpose_redacted, tombstoned,
			created_at_unix_micros
		) VALUES (?, ?, ?, 'effect', 'invalidated', 'Removed', '', 1, 2)`,
		graphID, revisionID, missingNodeID)
	assertGraphSQLFails(t, database, `
		INSERT INTO graph_revision_seals (
			graph_id, graph_revision_id, node_count, edge_count,
			content_sha256, sealed_at_unix_micros
		) VALUES (?, ?, 2, 1, ?, 2)`, graphID, revisionID, strings.Repeat("e", 64))
	mustGraphSQL(t, database, `
		INSERT INTO graph_revision_seals (
			graph_id, graph_revision_id, node_count, edge_count,
			content_sha256, sealed_at_unix_micros
		) VALUES (?, ?, 3, 1, ?, 2)`, graphID, revisionID, strings.Repeat("f", 64))
	assertGraphSQLFails(t, database, `
		INSERT INTO graph_node_contract_items (
			graph_revision_id, node_id, item_kind, ordinal, value_redacted
		) VALUES (?, ?, 'output', 1, 'Late mutation')`, revisionID, toNodeID)
	assertGraphSQLFails(t, database, `
		UPDATE graph_revision_seals SET node_count = 4
		WHERE graph_revision_id = ?`, revisionID)

	backupPath := filepath.Join(t.TempDir(), "graph-snapshot.sqlite3")
	if err := database.Backup(t.Context(), backupPath); err != nil {
		t.Fatal(err)
	}
	secondRevisionID, _ := domain.NewGraphRevisionID()
	mustGraphSQL(t, database, `
		INSERT INTO graph_revisions (
			id, graph_id, revision, parent_revision_id, graph_schema_version,
			metadata_policy, created_at_unix_micros
		) VALUES (?, ?, 2, ?, 1, 'typed-fields-only', 3)`,
		secondRevisionID, graphID, revisionID)
	if err := database.Restore(t.Context(), backupPath); err != nil {
		t.Fatal(err)
	}
	var restoredFirst, restoredSecond, restoredLinks int
	if err := database.sql.QueryRowContext(t.Context(),
		"SELECT count(*) FROM graph_revisions WHERE id = ?", revisionID,
	).Scan(&restoredFirst); err != nil {
		t.Fatal(err)
	}
	if err := database.sql.QueryRowContext(t.Context(),
		"SELECT count(*) FROM graph_revisions WHERE id = ?", secondRevisionID,
	).Scan(&restoredSecond); err != nil {
		t.Fatal(err)
	}
	if err := database.sql.QueryRowContext(t.Context(),
		"SELECT count(*) FROM graph_message_links WHERE message_id = ?", fixture.message.ID,
	).Scan(&restoredLinks); err != nil {
		t.Fatal(err)
	}
	if restoredFirst != 1 || restoredSecond != 0 || restoredLinks != 1 {
		t.Fatalf("restored graph facts = first:%d second:%d links:%d", restoredFirst, restoredSecond, restoredLinks)
	}
	assertGraphDatabaseIntegrity(t, database)
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

func mustGraphSQL(t *testing.T, database *Database, statement string, arguments ...any) {
	t.Helper()
	if _, err := database.sql.ExecContext(t.Context(), statement, arguments...); err != nil {
		t.Fatalf("graph SQL failed: %v\n%s", err, strings.TrimSpace(statement))
	}
}

func assertGraphSQLFails(t *testing.T, database *Database, statement string, arguments ...any) {
	t.Helper()
	if _, err := database.sql.ExecContext(t.Context(), statement, arguments...); err == nil {
		t.Fatalf("graph SQL unexpectedly succeeded:\n%s", strings.TrimSpace(statement))
	}
}

func assertGraphDatabaseIntegrity(t *testing.T, database *Database) {
	t.Helper()
	rows, err := database.sql.QueryContext(context.Background(), "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("graph schema has a foreign-key violation")
	}
	var integrity string
	if err := database.sql.QueryRowContext(context.Background(), "PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("graph database integrity = %q", integrity)
	}
}
