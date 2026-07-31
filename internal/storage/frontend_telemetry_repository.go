package storage

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/frontendtelemetry"
)

var _ frontendtelemetry.Store = (*Repositories)(nil)

// RecordFrontendTelemetry appends one structurally content-free UX event and
// atomically trims the journal to its fixed local retention ceiling.
func (repositories *Repositories) RecordFrontendTelemetry(
	ctx context.Context,
	event frontendtelemetry.Event,
) (frontendtelemetry.Event, error) {
	if repositories == nil || repositories.database == nil {
		return frontendtelemetry.Event{}, errors.New("repositories are unavailable")
	}
	if err := event.ValidateForRecord(); err != nil {
		return frontendtelemetry.Event{}, err
	}
	if event.FailureClass == "" {
		event.FailureClass = frontendtelemetry.FailureNone
	}
	_, occurredAtMicros := repositories.timestamp()
	durationMicros := int64(0)
	if event.Duration > 0 {
		durationMicros = int64(event.Duration / time.Microsecond)
		if event.Duration%time.Microsecond != 0 {
			durationMicros++
		}
	}

	var insertedID int64
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		result, err := transaction.sql.ExecContext(ctx, `
			INSERT INTO frontend_telemetry_events (
				kind, occurred_at_unix_micros, duration_micros, outcome, component,
				graph_mode, failure_class, task_id, thread_id, session_id,
				event_sequence, entity_revision
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.Kind, occurredAtMicros, durationMicros, event.Outcome, event.Component,
			nullableTelemetryString(string(event.GraphMode)), event.FailureClass,
			nullableTelemetryString(event.TaskID.String()),
			nullableTelemetryString(event.ThreadID.String()),
			nullableTelemetryString(event.SessionID.String()),
			event.Sequence, event.Revision,
		)
		if err != nil {
			return classify("record frontend telemetry", err)
		}
		insertedID, err = result.LastInsertId()
		if err != nil {
			return classify("read frontend telemetry identity", err)
		}
		if _, err := transaction.sql.ExecContext(ctx, `
			DELETE FROM frontend_telemetry_events
			WHERE id NOT IN (
				SELECT id FROM frontend_telemetry_events
				ORDER BY id DESC
				LIMIT ?
			)`, frontendtelemetry.MaxStoredEvents); err != nil {
			return classify("enforce frontend telemetry retention", err)
		}
		return nil
	})
	if err != nil {
		return frontendtelemetry.Event{}, err
	}
	event.ID = uint64(insertedID)
	event.OccurredAt = time.UnixMicro(occurredAtMicros).UTC()
	event.Duration = time.Duration(durationMicros) * time.Microsecond
	return event, nil
}

// ListFrontendTelemetry returns newest-first local telemetry with an exclusive
// stable cursor. The query is capped regardless of caller input.
func (repositories *Repositories) ListFrontendTelemetry(
	ctx context.Context,
	query frontendtelemetry.Query,
) (frontendtelemetry.Page, error) {
	if repositories == nil || repositories.database == nil {
		return frontendtelemetry.Page{}, errors.New("repositories are unavailable")
	}
	if err := query.Validate(); err != nil {
		return frontendtelemetry.Page{}, err
	}
	if query.BeforeID > math.MaxInt64 {
		return frontendtelemetry.Page{}, errors.New("frontend telemetry cursor is invalid")
	}
	limit := query.Limit
	if limit == 0 {
		limit = frontendtelemetry.DefaultLimit
	}

	statement := strings.Builder{}
	statement.WriteString(`
		SELECT id, kind, occurred_at_unix_micros, duration_micros, outcome,
			component, graph_mode, failure_class, task_id, thread_id, session_id,
			event_sequence, entity_revision
		FROM frontend_telemetry_events
		WHERE 1 = 1`)
	arguments := make([]any, 0, len(query.Kinds)+4)
	if query.BeforeID != 0 {
		statement.WriteString(" AND id < ?")
		arguments = append(arguments, query.BeforeID)
	}
	if !query.Since.IsZero() {
		statement.WriteString(" AND occurred_at_unix_micros >= ?")
		arguments = append(arguments, query.Since.UTC().UnixMicro())
	}
	if !query.Until.IsZero() {
		statement.WriteString(" AND occurred_at_unix_micros <= ?")
		arguments = append(arguments, query.Until.UTC().UnixMicro())
	}
	if len(query.Kinds) > 0 {
		statement.WriteString(" AND kind IN (")
		for index, kind := range query.Kinds {
			if index > 0 {
				statement.WriteString(",")
			}
			statement.WriteString("?")
			arguments = append(arguments, kind)
		}
		statement.WriteString(")")
	}
	statement.WriteString(" ORDER BY id DESC LIMIT ?")
	arguments = append(arguments, limit+1)

	rows, err := repositories.database.sql.QueryContext(ctx, statement.String(), arguments...)
	if err != nil {
		return frontendtelemetry.Page{}, classify("list frontend telemetry", err)
	}
	defer rows.Close()
	page := frontendtelemetry.Page{Events: make([]frontendtelemetry.Event, 0, limit)}
	for rows.Next() {
		event, scanErr := scanFrontendTelemetry(rows)
		if scanErr != nil {
			return frontendtelemetry.Page{}, scanErr
		}
		page.Events = append(page.Events, event)
	}
	if err := rows.Err(); err != nil {
		return frontendtelemetry.Page{}, classify("iterate frontend telemetry", err)
	}
	if len(page.Events) > limit {
		page.Events = page.Events[:limit]
		page.NextBeforeID = page.Events[len(page.Events)-1].ID
	}
	return page, nil
}

// DeleteFrontendTelemetry performs the user's explicit local deletion request.
func (repositories *Repositories) DeleteFrontendTelemetry(
	ctx context.Context,
	request frontendtelemetry.DeleteRequest,
) (frontendtelemetry.DeleteResult, error) {
	if repositories == nil || repositories.database == nil {
		return frontendtelemetry.DeleteResult{}, errors.New("repositories are unavailable")
	}
	if err := request.Validate(); err != nil {
		return frontendtelemetry.DeleteResult{}, err
	}
	result := frontendtelemetry.DeleteResult{}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		statement := "DELETE FROM frontend_telemetry_events"
		var arguments []any
		if request.Scope == frontendtelemetry.DeleteBefore {
			statement += " WHERE occurred_at_unix_micros < ?"
			arguments = append(arguments, request.Before.UTC().UnixMicro())
		}
		deleted, err := transaction.sql.ExecContext(ctx, statement, arguments...)
		if err != nil {
			return classify("delete frontend telemetry", err)
		}
		deletedCount, err := deleted.RowsAffected()
		if err != nil {
			return classify("count deleted frontend telemetry", err)
		}
		var remaining int64
		if err := transaction.sql.QueryRowContext(ctx,
			"SELECT count(*) FROM frontend_telemetry_events",
		).Scan(&remaining); err != nil {
			return classify("count remaining frontend telemetry", err)
		}
		result.Deleted = uint64(deletedCount)
		result.Remaining = uint64(remaining)
		return nil
	})
	return result, err
}

type telemetryScanner interface {
	Scan(...any) error
}

func scanFrontendTelemetry(scanner telemetryScanner) (frontendtelemetry.Event, error) {
	var event frontendtelemetry.Event
	var id, occurredAtMicros, durationMicros int64
	var graphMode, taskID, threadID, sessionID sql.NullString
	if err := scanner.Scan(
		&id, &event.Kind, &occurredAtMicros, &durationMicros, &event.Outcome,
		&event.Component, &graphMode, &event.FailureClass, &taskID, &threadID,
		&sessionID, &event.Sequence, &event.Revision,
	); err != nil {
		return frontendtelemetry.Event{}, classify("scan frontend telemetry", err)
	}
	event.ID = uint64(id)
	event.OccurredAt = time.UnixMicro(occurredAtMicros).UTC()
	event.Duration = time.Duration(durationMicros) * time.Microsecond
	event.GraphMode = frontendtelemetry.GraphMode(graphMode.String)
	var err error
	if taskID.Valid {
		event.TaskID, err = domain.ParseTaskID(taskID.String)
		if err != nil {
			return frontendtelemetry.Event{}, classify("parse frontend telemetry task identity", err)
		}
	}
	if threadID.Valid {
		event.ThreadID, err = domain.ParseThreadID(threadID.String)
		if err != nil {
			return frontendtelemetry.Event{}, classify("parse frontend telemetry thread identity", err)
		}
	}
	if sessionID.Valid {
		event.SessionID, err = domain.ParseSessionID(sessionID.String)
		if err != nil {
			return frontendtelemetry.Event{}, classify("parse frontend telemetry session identity", err)
		}
	}
	return event, nil
}

func nullableTelemetryString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
