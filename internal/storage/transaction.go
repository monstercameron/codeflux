package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Transaction is the storage-owned mutation boundary. Domain repositories add
// operations to this type without exposing generic SQL to application code.
type Transaction struct {
	sql *sql.Tx
	// faults carries the database's injector into the transaction, so a
	// repository function holding only a *Transaction — which is most of
	// them — can still reach it (AUDIT-027).
	faults FaultInjector
}

// FaultInjector reports an injected fault at one named boundary.
//
// A nil injector never faults, which is what makes consulting it inside
// RunInTransaction and appendTaskEventTransaction safe in production: the
// check is a nil comparison unless a test armed one. The interface is
// declared here, in storage, rather than imported from a test package, so
// production imports nothing test-only (mirrors
// internal/executor.FaultInjector).
type FaultInjector interface {
	// Check returns a non-nil error when a fault is armed at the point.
	Check(point string) error
}

// Named injection points inside the SQLite storage boundary.
//
// The strings match internal/testfixtures.FaultPoint values exactly, so the
// declared vocabulary and the wired boundaries cannot drift apart silently
// (see TestAUDIT027_TheWiredPointsMatchTheDeclaredVocabulary).
const (
	// FaultPointDatabaseBusyTimeout fires once a transaction's connection is
	// acquired and its busy timeout is set, immediately before the
	// transaction begins — the point every one of this package's mutations
	// passes through, since RunInTransaction is their sole entry.
	FaultPointDatabaseBusyTimeout = "database-busy-timeout"
	// FaultPointDiskFullOnEventAppend fires immediately before a durable
	// task event is inserted, which is every caller of
	// appendTaskEventTransaction — the single choke point every durable
	// task-scoped event in this database passes through.
	FaultPointDiskFullOnEventAppend = "disk-exhausted-on-event-append"
)

// checkFault consults the injector when one is present.
func checkFault(faults FaultInjector, point string) error {
	if faults == nil {
		return nil
	}
	return faults.Check(point)
}

// SetFaultInjector arms this database's storage-boundary fault points
// (AUDIT-027). It is nil by default, which is the production path; only a
// test wires a real injector.
func (database *Database) SetFaultInjector(injector FaultInjector) {
	database.faults = injector
}

// RunInTransaction executes one mutation under immediate SQLite write intent.
// It commits only after the callback succeeds and always rolls back otherwise.
func (database *Database) RunInTransaction(
	ctx context.Context,
	operation func(*Transaction) error,
) error {
	if operation == nil {
		return errors.New("transaction operation must not be nil")
	}
	connection, err := database.sql.Conn(ctx)
	if err != nil {
		return classify("acquire storage transaction connection", err)
	}
	defer connection.Close()
	effectiveTimeout, deadlineBound, err := database.boundBusyTimeoutToContext(ctx)
	if err != nil {
		return err
	}
	if err := setConnectionBusyTimeout(ctx, connection, effectiveTimeout); err != nil {
		return err
	}
	restoreBusyTimeout := func() error {
		return setConnectionBusyTimeout(context.Background(), connection, database.busyTimeout)
	}
	if err := checkFault(database.faults, FaultPointDatabaseBusyTimeout); err != nil {
		return errors.Join(err, restoreBusyTimeout())
	}

	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		restoreErr := restoreBusyTimeout()
		classified := classify("begin storage transaction", err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return errors.Join(ctxErr, restoreErr)
		}
		if deadlineBound && errors.Is(classified, ErrBusy) {
			return errors.Join(context.DeadlineExceeded, restoreErr)
		}
		return errors.Join(classified, restoreErr)
	}
	wrapped := &Transaction{sql: transaction, faults: database.faults}
	if err := operation(wrapped); err != nil {
		rollbackErr := transaction.Rollback()
		return errors.Join(
			err,
			classify("rollback storage transaction", rollbackErr),
			restoreBusyTimeout(),
		)
	}
	if err := transaction.Commit(); err != nil {
		commitErr := classify("commit storage transaction", err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			commitErr = ctxErr
		}
		return errors.Join(commitErr, restoreBusyTimeout())
	}
	return restoreBusyTimeout()
}

func (database *Database) boundBusyTimeoutToContext(
	ctx context.Context,
) (time.Duration, bool, error) {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		return database.busyTimeout, false, nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, false, context.DeadlineExceeded
	}
	remaining = remaining.Truncate(time.Millisecond)
	if remaining < time.Millisecond {
		remaining = time.Millisecond
	}
	if remaining < database.busyTimeout {
		return remaining, true, nil
	}
	return database.busyTimeout, false, nil
}

func setConnectionBusyTimeout(
	ctx context.Context,
	connection *sql.Conn,
	timeout time.Duration,
) error {
	statement := "PRAGMA busy_timeout=" + strconv.FormatInt(timeout.Milliseconds(), 10)
	if _, err := connection.ExecContext(ctx, statement); err != nil {
		return classify("configure transaction busy timeout", err)
	}
	var observed int64
	if err := connection.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&observed); err != nil {
		return classify("verify transaction busy timeout", err)
	}
	if observed != timeout.Milliseconds() {
		return fmt.Errorf(
			"transaction busy timeout is %d ms, want %d ms",
			observed,
			timeout.Milliseconds(),
		)
	}
	return nil
}
