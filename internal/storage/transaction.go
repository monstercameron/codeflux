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
	wrapped := &Transaction{sql: transaction}
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
