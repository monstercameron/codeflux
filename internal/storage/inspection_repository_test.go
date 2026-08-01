package storage

import (
	"strings"
	"testing"
)

// TestM22_118_EveryInspectionQueryRunsAgainstTheRealSchema covers M22-118.
//
// Each entity's fixed statement is executed. An inspection referencing a
// renamed column would otherwise fail only when a developer reached for it
// mid-debugging, which is the worst possible moment to discover it.
func TestM22_118_EveryInspectionQueryRunsAgainstTheRealSchema(t *testing.T) {
	repositories := openTestRepositories(t)
	for _, entity := range AllInspectionEntities() {
		t.Run(string(entity), func(t *testing.T) {
			result, err := repositories.Inspect(t.Context(), InspectionQuery{
				Entity: entity, Limit: 10,
			})
			if err != nil {
				t.Fatalf("inspect %s: %v", entity, err)
			}
			if result.Entity != entity {
				t.Fatalf("result entity = %q", result.Entity)
			}
			if len(result.Rows) != 0 {
				t.Fatalf("empty database returned %d rows", len(result.Rows))
			}
			if result.Truncated {
				t.Fatal("empty result was reported truncated")
			}
		})
	}

	// Memory lineage takes a different statement and must also run.
	if _, err := repositories.Inspect(t.Context(), InspectionQuery{
		Entity: InspectMemory, ID: "rev_absent", IncludeLineage: true, Limit: 10,
	}); err != nil {
		t.Fatalf("inspect memory lineage: %v", err)
	}
}

// TestM22_118_InspectionReturnsRealRowsAndReportsTruncation proves the
// inspection is not merely returning empty results.
func TestM22_118_InspectionReturnsRealRowsAndReportsTruncation(t *testing.T) {
	repositories, task := createTaskFixture(t, 9500)

	found, err := repositories.Inspect(t.Context(), InspectionQuery{
		Entity: InspectTask, Limit: 10,
	})
	if err != nil {
		t.Fatalf("inspect tasks: %v", err)
	}
	if len(found.Rows) != 1 {
		t.Fatalf("inspected %d tasks, want 1", len(found.Rows))
	}
	if found.Rows[0].Fields["id"] != task.ID.String() {
		t.Fatalf("inspected task id = %q, want %q",
			found.Rows[0].Fields["id"], task.ID.String())
	}
	for _, column := range []string{"thread_id", "state", "risk_level", "revision"} {
		if found.Rows[0].Fields[column] == "" {
			t.Fatalf("column %q is missing from the inspection row", column)
		}
	}

	// Scoping by ID must actually scope.
	scoped, err := repositories.Inspect(t.Context(), InspectionQuery{
		Entity: InspectTask, ID: "tsk_does_not_exist", Limit: 10,
	})
	if err != nil {
		t.Fatalf("inspect scoped: %v", err)
	}
	if len(scoped.Rows) != 0 {
		t.Fatalf("scoping to an absent id returned %d rows", len(scoped.Rows))
	}

	// Insert enough events to exceed a small limit, so truncation is real.
	created, err := repositories.scalar(t.Context(),
		`SELECT created_at_unix_micros FROM tasks WHERE id = ?`, task.ID.String())
	if err != nil {
		t.Fatalf("read task time: %v", err)
	}
	for index := 1; index <= 5; index++ {
		if _, err := repositories.database.sql.ExecContext(t.Context(),
			`INSERT INTO task_events (
				id, task_id, sequence, event_type, payload_json,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, 'tool-started', '{}', ?, ?)`,
			"evt_inspect_"+itoaFixture(index), task.ID.String(), index,
			"inspect-"+itoaFixture(index), created+int64(index),
		); err != nil {
			t.Fatalf("insert event %d: %v", index, err)
		}
	}

	limited, err := repositories.Inspect(t.Context(), InspectionQuery{
		Entity: InspectEvent, TaskID: task.ID.String(), Limit: 3,
	})
	if err != nil {
		t.Fatalf("inspect events: %v", err)
	}
	if len(limited.Rows) != 3 {
		t.Fatalf("limited inspection returned %d rows, want 3", len(limited.Rows))
	}
	if !limited.Truncated {
		t.Fatal("a clipped inspection was not reported as truncated")
	}

	// A sequence range must actually filter.
	ranged, err := repositories.Inspect(t.Context(), InspectionQuery{
		Entity: InspectEvent, TaskID: task.ID.String(),
		FromSequence: 2, ToSequence: 4, Limit: 10,
	})
	if err != nil {
		t.Fatalf("inspect ranged events: %v", err)
	}
	if len(ranged.Rows) != 3 {
		t.Fatalf("range 2..4 returned %d rows, want 3", len(ranged.Rows))
	}
	if ranged.Truncated {
		t.Fatal("a complete range was reported truncated")
	}
	for _, row := range ranged.Rows {
		if row.Fields["sequence"] == "1" || row.Fields["sequence"] == "5" {
			t.Fatalf("range 2..4 returned sequence %s", row.Fields["sequence"])
		}
	}
}

// TestM22_118_InspectionRefusesUnusableQueries proves the surface cannot be
// used as an unbounded or nonsensical door into the store.
func TestM22_118_InspectionRefusesUnusableQueries(t *testing.T) {
	repositories := openTestRepositories(t)
	bad := map[string]InspectionQuery{
		"unknown entity":        {Entity: InspectionEntity("invented"), Limit: 10},
		"no limit":              {Entity: InspectTask},
		"negative limit":        {Entity: InspectTask, Limit: -1},
		"limit above maximum":   {Entity: InspectTask, Limit: MaximumInspectionLimit + 1},
		"inverted range":        {Entity: InspectEvent, FromSequence: 9, ToSequence: 2, Limit: 10},
		"range on non-event":    {Entity: InspectTask, FromSequence: 1, Limit: 10},
		"lineage on non-memory": {Entity: InspectTask, IncludeLineage: true, Limit: 10},
	}
	for name, query := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := repositories.Inspect(t.Context(), query); err == nil {
				t.Fatalf("an unusable inspection was accepted: %s", name)
			}
		})
	}
}

// TestM22_118_InspectionIsReadOnly proves an inspection cannot mutate the
// store, which is the property that makes it safe to hand a developer.
func TestM22_118_InspectionIsReadOnly(t *testing.T) {
	repositories, task := createTaskFixture(t, 9550)

	before, err := repositories.scalar(t.Context(), `SELECT revision FROM tasks WHERE id = ?`,
		task.ID.String())
	if err != nil {
		t.Fatalf("read revision: %v", err)
	}

	for _, entity := range AllInspectionEntities() {
		if _, err := repositories.Inspect(t.Context(), InspectionQuery{
			Entity: entity, Limit: 50,
		}); err != nil {
			t.Fatalf("inspect %s: %v", entity, err)
		}
	}

	after, err := repositories.scalar(t.Context(), `SELECT revision FROM tasks WHERE id = ?`,
		task.ID.String())
	if err != nil {
		t.Fatalf("re-read revision: %v", err)
	}
	if before != after {
		t.Fatalf("inspection changed the task revision from %d to %d", before, after)
	}

	count, err := repositories.scalar(t.Context(), `SELECT count(*) FROM tasks`)
	if err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("inspection changed the task count to %d", count)
	}
}

// TestM22_118_InspectionExcludesInvalidatedByDefault proves stale facts are not
// presented beside live ones unless explicitly asked for.
func TestM22_118_InspectionExcludesInvalidatedByDefault(t *testing.T) {
	repositories := openTestRepositories(t)

	// The statement must differ between the two modes, or the flag is
	// decorative.
	withDefault, _, _, err := inspectionStatement(InspectionQuery{
		Entity: InspectMemory, Limit: 10,
	})
	if err != nil {
		t.Fatalf("build default statement: %v", err)
	}
	if !strings.Contains(withDefault, "maturity != 'invalidated'") {
		t.Fatalf("the default memory inspection does not exclude invalidated revisions:\n%s",
			withDefault)
	}

	// Both modes must execute against the real schema.
	for _, include := range []bool{false, true} {
		if _, err := repositories.Inspect(t.Context(), InspectionQuery{
			Entity: InspectMemory, IncludeInvalidated: include, Limit: 10,
		}); err != nil {
			t.Fatalf("inspect memory (include invalidated=%v): %v", include, err)
		}
	}
}

// TestM22_118_EntitySummaryNamesEverySupportedEntity guards the help text.
func TestM22_118_EntitySummaryNamesEverySupportedEntity(t *testing.T) {
	summary := InspectionEntitySummary()
	for _, entity := range AllInspectionEntities() {
		if !strings.Contains(summary, string(entity)) {
			t.Fatalf("entity summary omits %q: %s", entity, summary)
		}
	}
	if InspectionEntity("invented").Valid() {
		t.Fatal("an unknown entity validated")
	}
}

func itoaFixture(value int) string {
	return string(rune('0' + value))
}
