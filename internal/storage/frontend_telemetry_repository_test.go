package storage

import (
	"reflect"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/frontendtelemetry"
)

func TestFrontendTelemetryRecordQueryInspectAndDelete(t *testing.T) {
	repositories := openTestRepositories(t)
	taskID := testTaskID(t, 801)
	sessionID := testSessionID(t, 802)
	recorded := make([]frontendtelemetry.Event, 0, 3)
	for _, event := range []frontendtelemetry.Event{
		{
			Kind: frontendtelemetry.KindTimeToPlan, TaskID: taskID,
			Component: frontendtelemetry.ComponentPlan,
			Outcome:   frontendtelemetry.OutcomeSucceeded, Duration: 1200 * time.Millisecond,
		},
		{
			Kind: frontendtelemetry.KindReconnect, SessionID: sessionID,
			Component: frontendtelemetry.ComponentSession,
			Outcome:   frontendtelemetry.OutcomeReconnected, Duration: 80 * time.Millisecond,
		},
		{
			Kind: frontendtelemetry.KindSlowRender, TaskID: taskID,
			Component: frontendtelemetry.ComponentGraph,
			Outcome:   frontendtelemetry.OutcomeSucceeded, Duration: 55*time.Millisecond + time.Nanosecond,
		},
	} {
		stored, err := repositories.RecordFrontendTelemetry(t.Context(), event)
		if err != nil {
			t.Fatal(err)
		}
		recorded = append(recorded, stored)
	}
	if recorded[2].Duration != 55*time.Millisecond+time.Microsecond {
		t.Fatalf("sub-microsecond duration was not conservatively rounded up: %s", recorded[2].Duration)
	}

	page, err := repositories.ListFrontendTelemetry(t.Context(), frontendtelemetry.Query{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].ID != recorded[2].ID ||
		page.Events[1].ID != recorded[1].ID || page.NextBeforeID != recorded[1].ID {
		t.Fatalf("first page = %#v", page)
	}
	second, err := repositories.ListFrontendTelemetry(t.Context(), frontendtelemetry.Query{
		BeforeID: page.NextBeforeID, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 1 || second.Events[0].ID != recorded[0].ID || second.NextBeforeID != 0 {
		t.Fatalf("second page = %#v", second)
	}
	filtered, err := repositories.ListFrontendTelemetry(t.Context(), frontendtelemetry.Query{
		Kinds: []frontendtelemetry.Kind{frontendtelemetry.KindReconnect}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Events) != 1 || !reflect.DeepEqual(filtered.Events[0], recorded[1]) {
		t.Fatalf("filtered telemetry = %#v want %#v", filtered.Events, recorded[1])
	}

	if _, err := repositories.DeleteFrontendTelemetry(t.Context(), frontendtelemetry.DeleteRequest{
		Scope: frontendtelemetry.DeleteAll,
	}); err == nil {
		t.Fatal("unconfirmed telemetry deletion succeeded")
	}
	deleted, err := repositories.DeleteFrontendTelemetry(t.Context(), frontendtelemetry.DeleteRequest{
		Scope: frontendtelemetry.DeleteBefore, Before: recorded[1].OccurredAt,
		Confirmation: frontendtelemetry.ConfirmTelemetryDeletion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Deleted != 1 || deleted.Remaining != 2 {
		t.Fatalf("partial deletion = %#v", deleted)
	}
	deleted, err = repositories.DeleteFrontendTelemetry(t.Context(), frontendtelemetry.DeleteRequest{
		Scope:        frontendtelemetry.DeleteAll,
		Confirmation: frontendtelemetry.ConfirmTelemetryDeletion,
	})
	if err != nil || deleted.Deleted != 2 || deleted.Remaining != 0 {
		t.Fatalf("complete deletion = %#v error=%v", deleted, err)
	}
}

func TestFrontendTelemetryEnforcesRetentionCeiling(t *testing.T) {
	repositories := openTestRepositories(t)
	if _, err := repositories.database.sql.ExecContext(t.Context(), `
		WITH RECURSIVE counter(value) AS (
			VALUES(1)
			UNION ALL
			SELECT value + 1 FROM counter WHERE value < ?
		)
		INSERT INTO frontend_telemetry_events (
			kind, occurred_at_unix_micros, duration_micros, outcome, component,
			failure_class, event_sequence, entity_revision
		)
		SELECT 'first-run-step', value, 0, 'succeeded', 'first-run', 'none', 0, 0
		FROM counter`, frontendtelemetry.MaxStoredEvents); err != nil {
		t.Fatal(err)
	}
	stored, err := repositories.RecordFrontendTelemetry(t.Context(), frontendtelemetry.Event{
		Kind: frontendtelemetry.KindFirstRunStep, Component: frontendtelemetry.ComponentFirstRun,
		Outcome: frontendtelemetry.OutcomeSucceeded,
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	var minimumID uint64
	if err := repositories.database.sql.QueryRowContext(t.Context(),
		"SELECT count(*), min(id) FROM frontend_telemetry_events",
	).Scan(&count, &minimumID); err != nil {
		t.Fatal(err)
	}
	if count != frontendtelemetry.MaxStoredEvents || minimumID != 2 || stored.ID != frontendtelemetry.MaxStoredEvents+1 {
		t.Fatalf("retention count=%d minimum=%d stored=%d", count, minimumID, stored.ID)
	}
}

func TestFrontendTelemetrySchemaRejectsInvalidValuesAndUpdates(t *testing.T) {
	repositories := openTestRepositories(t)
	_, err := repositories.database.sql.ExecContext(t.Context(), `
		INSERT INTO frontend_telemetry_events (
			kind, occurred_at_unix_micros, duration_micros, outcome, component,
			failure_class, event_sequence, entity_revision
		) VALUES ('raw-content', 1, 0, 'succeeded', 'thread', 'none', 0, 0)`)
	if err == nil {
		t.Fatalf("invalid telemetry kind error=%v", err)
	}
	stored, err := repositories.RecordFrontendTelemetry(t.Context(), frontendtelemetry.Event{
		Kind: frontendtelemetry.KindFirstRunStep, Component: frontendtelemetry.ComponentFirstRun,
		Outcome: frontendtelemetry.OutcomeSucceeded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(t.Context(),
		"UPDATE frontend_telemetry_events SET outcome = 'failed' WHERE id = ?", stored.ID,
	); err == nil {
		t.Fatal("immutable telemetry row was updated")
	}
}
