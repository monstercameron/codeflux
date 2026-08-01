package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// InspectionEntity names one domain entity a developer may inspect (M22-118).
//
// The set is closed and each entry maps to a fixed, parameterised query. There
// is no free-text SQL door: an inspection tool that accepted arbitrary SQL
// would be a way to mutate the durable store by accident, and the plan
// forbids manual database mutation as a way of making a flow work.
type InspectionEntity string

const (
	InspectTask       InspectionEntity = "task"
	InspectRun        InspectionEntity = "run"
	InspectEvent      InspectionEntity = "event"
	InspectApproval   InspectionEntity = "approval"
	InspectCheckpoint InspectionEntity = "checkpoint"
	InspectPlan       InspectionEntity = "plan"
	InspectMemory     InspectionEntity = "memory-artifact"
	InspectGraph      InspectionEntity = "graph-revision"
)

// AllInspectionEntities returns every inspectable entity.
func AllInspectionEntities() []InspectionEntity {
	return []InspectionEntity{
		InspectTask, InspectRun, InspectEvent, InspectApproval,
		InspectCheckpoint, InspectPlan, InspectMemory, InspectGraph,
	}
}

// Valid reports whether an entity is one of the declared set.
func (entity InspectionEntity) Valid() bool {
	for _, candidate := range AllInspectionEntities() {
		if candidate == entity {
			return true
		}
	}
	return false
}

// InspectionQuery is one read-only request (M22-118).
type InspectionQuery struct {
	Entity InspectionEntity
	// ID scopes to one entity. Empty lists within the other filters.
	ID string
	// TaskID scopes event, approval, checkpoint, and plan queries.
	TaskID string
	// FromSequence and ToSequence bound an event query.
	FromSequence uint64
	ToSequence   uint64
	// AtRevision selects one revision of a versioned entity. Zero means the
	// current one.
	AtRevision uint64
	// IncludeLineage follows derived-from relationships for a memory artifact.
	IncludeLineage bool
	// IncludeInvalidated includes entities that have been invalidated. They
	// are excluded by default, because an invalidated item presented beside
	// live ones is how a stale fact gets believed.
	IncludeInvalidated bool
	// Limit bounds the result. It is required: an unbounded inspection of a
	// long-running project's events will not fit on a screen or in memory.
	Limit int
}

// MaximumInspectionLimit bounds any single inspection.
const MaximumInspectionLimit = 500

// Validate rejects an unusable query.
func (query InspectionQuery) Validate() error {
	if !query.Entity.Valid() {
		return fmt.Errorf("unknown inspection entity %q", query.Entity)
	}
	if query.Limit <= 0 {
		return errors.New("an inspection requires an explicit limit")
	}
	if query.Limit > MaximumInspectionLimit {
		return fmt.Errorf("inspection limit %d exceeds the maximum of %d",
			query.Limit, MaximumInspectionLimit)
	}
	if query.ToSequence != 0 && query.FromSequence > query.ToSequence {
		return errors.New("inspection sequence range ends before it starts")
	}
	if query.Entity != InspectEvent && (query.FromSequence != 0 || query.ToSequence != 0) {
		return fmt.Errorf("a sequence range is only meaningful for events, not %q", query.Entity)
	}
	if query.IncludeLineage && query.Entity != InspectMemory {
		return fmt.Errorf("lineage is only meaningful for memory artifacts, not %q", query.Entity)
	}
	return nil
}

// InspectionRow is one returned record.
//
// Values are strings because an inspection is for reading, not for computing:
// returning typed values would invite a caller to build logic on them, and the
// point of this surface is that nothing depends on it.
type InspectionRow struct {
	Fields map[string]string
}

// InspectionResult is a bounded read-only answer.
type InspectionResult struct {
	Entity InspectionEntity
	Rows   []InspectionRow
	// Truncated reports the limit was reached. Without it a caller cannot tell
	// a complete answer from a clipped one.
	Truncated bool
}

// Inspect runs one read-only inspection (M22-118).
//
// Every query is a fixed statement with bound parameters, executed on a
// read-only path. The method never writes, and there is no code path here that
// could.
func (repositories *Repositories) Inspect(
	ctx context.Context,
	query InspectionQuery,
) (InspectionResult, error) {
	if err := query.Validate(); err != nil {
		return InspectionResult{}, err
	}
	statement, arguments, columns, err := inspectionStatement(query)
	if err != nil {
		return InspectionResult{}, err
	}
	// One extra row is requested so truncation is detectable rather than
	// guessed from a full page.
	arguments = append(arguments, query.Limit+1)

	rows, err := repositories.database.sql.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return InspectionResult{}, classify("run inspection", err)
	}
	defer func() { _ = rows.Close() }()

	result := InspectionResult{Entity: query.Entity}
	for rows.Next() {
		if len(result.Rows) == query.Limit {
			result.Truncated = true
			break
		}
		values := make([]any, len(columns))
		holders := make([]sql.NullString, len(columns))
		for index := range columns {
			values[index] = &holders[index]
		}
		if err := rows.Scan(values...); err != nil {
			return InspectionResult{}, classify("scan inspection row", err)
		}
		fields := make(map[string]string, len(columns))
		for index, column := range columns {
			if holders[index].Valid {
				fields[column] = holders[index].String
				continue
			}
			// A null is reported as an explicit unknown rather than an empty
			// string, so a reader never mistakes "not recorded" for "empty".
			fields[column] = "(null)"
		}
		result.Rows = append(result.Rows, InspectionRow{Fields: fields})
	}
	if err := rows.Err(); err != nil {
		return InspectionResult{}, classify("iterate inspection rows", err)
	}
	return result, nil
}

func inspectionStatement(query InspectionQuery) (string, []any, []string, error) {
	switch query.Entity {
	case InspectTask:
		columns := []string{"id", "thread_id", "state", "risk_level", "revision", "created_at_unix_micros"}
		statement := `SELECT id, thread_id, state, risk_level, revision, created_at_unix_micros
			FROM tasks WHERE (? = '' OR id = ?) ORDER BY created_at_unix_micros DESC LIMIT ?`
		return statement, []any{query.ID, query.ID}, columns, nil

	case InspectRun:
		columns := []string{"id", "task_id", "state", "created_at_unix_micros"}
		statement := `SELECT id, task_id, state, created_at_unix_micros
			FROM runs WHERE (? = '' OR id = ?) AND (? = '' OR task_id = ?)
			ORDER BY created_at_unix_micros DESC LIMIT ?`
		return statement, []any{query.ID, query.ID, query.TaskID, query.TaskID}, columns, nil

	case InspectEvent:
		columns := []string{"id", "task_id", "sequence", "event_type", "created_at_unix_micros"}
		statement := `SELECT id, task_id, sequence, event_type, created_at_unix_micros
			FROM task_events
			WHERE (? = '' OR task_id = ?)
			  AND (? = 0 OR sequence >= ?)
			  AND (? = 0 OR sequence <= ?)
			ORDER BY task_id, sequence LIMIT ?`
		return statement, []any{
			query.TaskID, query.TaskID,
			query.FromSequence, query.FromSequence,
			query.ToSequence, query.ToSequence,
		}, columns, nil

	case InspectApproval:
		columns := []string{"id", "task_id", "state", "scope", "revision", "requested_at_unix_micros"}
		statement := `SELECT id, task_id, state, scope, revision, requested_at_unix_micros
			FROM approvals WHERE (? = '' OR id = ?) AND (? = '' OR task_id = ?)
			ORDER BY requested_at_unix_micros DESC LIMIT ?`
		return statement, []any{query.ID, query.ID, query.TaskID, query.TaskID}, columns, nil

	case InspectCheckpoint:
		columns := []string{"id", "task_id", "created_at_unix_micros"}
		statement := `SELECT id, task_id, created_at_unix_micros
			FROM checkpoints WHERE (? = '' OR id = ?) AND (? = '' OR task_id = ?)
			ORDER BY created_at_unix_micros DESC LIMIT ?`
		return statement, []any{query.ID, query.ID, query.TaskID, query.TaskID}, columns, nil

	case InspectPlan:
		columns := []string{"task_id", "revision", "created_at_unix_micros"}
		statement := `SELECT task_id, revision, created_at_unix_micros
			FROM agent_plan_revisions
			WHERE (? = '' OR task_id = ?) AND (? = 0 OR revision = ?)
			ORDER BY task_id, revision DESC LIMIT ?`
		return statement, []any{
			query.TaskID, query.TaskID, query.AtRevision, query.AtRevision,
		}, columns, nil

	case InspectMemory:
		columns := []string{"id", "artifact_id", "revision_number", "maturity"}
		if query.IncludeLineage {
			// Lineage follows derived_from, the SEMANTIC dependency edge, not
			// influenced_by: an invalidated ancestor quarantines everything
			// derived from it, so those descendants are what a reader
			// inspecting one artifact actually needs to see. The edge is keyed
			// by artifact, so ID is read as an artifact identity here.
			statement := `WITH RECURSIVE lineage(artifact_id) AS (
					SELECT id FROM memory_artifacts WHERE id = ?
					UNION
					SELECT edge.artifact_id
					FROM memory_artifact_derived_from AS edge
					JOIN lineage ON lineage.artifact_id = edge.ancestor_artifact_id
				)
				SELECT revisions.id, revisions.artifact_id,
				       revisions.revision_number, revisions.maturity
				FROM memory_artifact_revisions AS revisions
				JOIN lineage ON lineage.artifact_id = revisions.artifact_id
				WHERE (? = 1 OR revisions.maturity != 'invalidated')
				ORDER BY revisions.artifact_id, revisions.revision_number LIMIT ?`
			return statement, []any{query.ID, boolToInt(query.IncludeInvalidated)}, columns, nil
		}
		statement := `SELECT id, artifact_id, revision_number, maturity
			FROM memory_artifact_revisions
			WHERE (? = '' OR id = ?)
			  AND (? = 0 OR revision_number = ?)
			  AND (? = 1 OR maturity != 'invalidated')
			ORDER BY artifact_id, revision_number DESC LIMIT ?`
		return statement, []any{
			query.ID, query.ID, query.AtRevision, query.AtRevision,
			boolToInt(query.IncludeInvalidated),
		}, columns, nil

	case InspectGraph:
		columns := []string{"id", "graph_id", "revision", "created_at_unix_micros"}
		statement := `SELECT id, graph_id, revision, created_at_unix_micros
			FROM graph_revisions
			WHERE (? = '' OR id = ?) AND (? = 0 OR revision = ?)
			ORDER BY graph_id, revision DESC LIMIT ?`
		return statement, []any{
			query.ID, query.ID, query.AtRevision, query.AtRevision,
		}, columns, nil
	}
	return "", nil, nil, fmt.Errorf("entity %q has no inspection statement", query.Entity)
}

// InspectionEntitySummary lists the entities and the filters each accepts, so
// a developer-facing command can print help without duplicating this list.
func InspectionEntitySummary() string {
	entities := AllInspectionEntities()
	names := make([]string, 0, len(entities))
	for _, entity := range entities {
		names = append(names, string(entity))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
