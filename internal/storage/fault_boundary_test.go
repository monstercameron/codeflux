package storage

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/testfixtures"
)

// TestAUDIT027_AnInjectedFaultReachesTheRealDatabaseBoundary covers the
// SQLite half of AUDIT-027, reconciling part of M22-036 through M22-050 and
// M22-G03.
//
// Fifteen fault points were declared and no production boundary consulted
// any of them: RunWithFault appeared only inside its own package's tests.
// This arms the real vocabulary against the real transaction wrapper and the
// real durable event journal — the two SQLite-owned points every mutation in
// this package passes through — rather than a parallel test-only stand-in.
func TestAUDIT027_AnInjectedFaultReachesTheRealDatabaseBoundary(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		point          testfixtures.FaultPoint
		idempotencyKey string
		run            func(*Repositories, domain.TaskID, string) error
	}{
		{
			name:  "acquiring a transaction",
			point: testfixtures.FaultDatabaseBusyTimeout,
			run: func(repositories *Repositories, taskID domain.TaskID, _ string) error {
				return repositories.database.RunInTransaction(
					context.Background(), func(*Transaction) error { return nil })
			},
		},
		{
			name:           "appending a durable task event",
			point:          testfixtures.FaultDiskFullOnEventAppend,
			idempotencyKey: "audit027-event-append",
			run: func(repositories *Repositories, taskID domain.TaskID, idempotencyKey string) error {
				eventID, err := domain.NewEventID()
				if err != nil {
					t.Fatal(err)
				}
				_, err = repositories.AppendTaskEvent(context.Background(), AppendTaskEvent{
					ID: eventID, TaskID: taskID, EventType: "audit027.fault.probe",
					PayloadJSON: "{}", IdempotencyKey: idempotencyKey,
				})
				return err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repositories, task := createTaskFixture(t, 6200)
			injector := testfixtures.NewFaultInjector()
			injector.Arm(testCase.point, 1, "audit-027 storage boundary check")
			repositories.database.SetFaultInjector(
				testfixtures.StringPointInjector{Injector: injector})

			err := testCase.run(repositories, task.ID, testCase.idempotencyKey)
			if !errors.Is(err, testfixtures.ErrInjectedFault) {
				t.Fatalf("operation returned %v, want the injected fault", err)
			}
			if injector.Fired(testCase.point) != 1 {
				t.Fatalf("the point fired %d times; the boundary was not reached",
					injector.Fired(testCase.point))
			}
			if injector.Remaining(testCase.point) != 0 {
				t.Error("the arm was not consumed, so the boundary did not check it")
			}
		})
	}
}

// TestAUDIT027_AnUnarmedDatabaseInjectorChangesNothing proves wiring the
// check into RunInTransaction and the event journal costs nothing when no
// fault is armed.
func TestAUDIT027_AnUnarmedDatabaseInjectorChangesNothing(t *testing.T) {
	repositories, task := createTaskFixture(t, 6210)
	injector := testfixtures.NewFaultInjector()
	repositories.database.SetFaultInjector(testfixtures.StringPointInjector{Injector: injector})

	eventID, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.AppendTaskEvent(context.Background(), AppendTaskEvent{
		ID: eventID, TaskID: task.ID, EventType: "audit027.fault.probe",
		PayloadJSON: "{}", IdempotencyKey: "audit027-unarmed",
	}); err != nil {
		t.Fatalf("an unarmed injector broke a real append: %v", err)
	}
}

// TestAUDIT027_ANilDatabaseInjectorIsTheProductionPath proves the default: a
// database that never calls SetFaultInjector — every production database —
// consults nothing and behaves exactly as it did before this ticket.
func TestAUDIT027_ANilDatabaseInjectorIsTheProductionPath(t *testing.T) {
	repositories, task := createTaskFixture(t, 6220)
	eventID, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.AppendTaskEvent(context.Background(), AppendTaskEvent{
		ID: eventID, TaskID: task.ID, EventType: "audit027.fault.probe",
		PayloadJSON: "{}", IdempotencyKey: "audit027-nil",
	}); err != nil {
		t.Fatalf("the production path (no injector) failed: %v", err)
	}
}

// TestAUDIT027_TheWiredDatabasePointsMatchTheDeclaredVocabulary keeps the
// boundary and the fault catalogue from drifting apart, mirroring
// internal/executor's equivalent check.
func TestAUDIT027_TheWiredDatabasePointsMatchTheDeclaredVocabulary(t *testing.T) {
	declared := make(map[string]struct{})
	for _, point := range testfixtures.AllFaultPoints() {
		declared[string(point)] = struct{}{}
	}
	for _, wired := range []string{FaultPointDatabaseBusyTimeout, FaultPointDiskFullOnEventAppend} {
		if _, ok := declared[wired]; !ok {
			t.Errorf("%q is consulted by storage but is not a declared fault point", wired)
		}
	}
}
