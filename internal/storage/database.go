package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBusyTimeout       = 5 * time.Second
	defaultMaximumConnection = 4
)

// OpenOptions controls the bounded local SQLite connection policy.
type OpenOptions struct {
	Path               string
	BusyTimeout        time.Duration
	MaximumConnections int
}

// Health reports the connection-local invariants checked against SQLite.
type Health struct {
	Readable           bool
	Writable           bool
	ForeignKeysEnabled bool
	JournalMode        string
	Synchronous        int
	BusyTimeout        time.Duration
}

// Database owns the authoritative SQLite connection pool.
type Database struct {
	sql         *sql.DB
	path        string
	busyTimeout time.Duration
	closeOnce   sync.Once
	closeErr    error
	// faults is the SQLite storage boundary's fault injector (AUDIT-027). Nil
	// in production; set only by SetFaultInjector.
	faults FaultInjector
}

// OpenDefault opens the database at DefaultDatabasePath.
func OpenDefault(ctx context.Context) (*Database, error) {
	path, err := DefaultDatabasePath()
	if err != nil {
		return nil, err
	}
	return Open(ctx, OpenOptions{Path: path})
}

// Open creates or opens one configured SQLite database.
func Open(ctx context.Context, options OpenOptions) (*Database, error) {
	if strings.TrimSpace(options.Path) == "" {
		return nil, errors.New("database path must not be empty")
	}
	path, err := filepath.Abs(options.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if options.BusyTimeout == 0 {
		options.BusyTimeout = defaultBusyTimeout
	}
	if options.BusyTimeout < 0 || options.BusyTimeout%time.Millisecond != 0 {
		return nil, errors.New("busy timeout must be a non-negative whole number of milliseconds")
	}
	if options.MaximumConnections == 0 {
		options.MaximumConnections = defaultMaximumConnection
	}
	if options.MaximumConnections < 1 {
		return nil, errors.New("maximum connections must be positive")
	}
	if err := ensureDatabaseFile(path); err != nil {
		return nil, err
	}

	sqlDB, err := sql.Open("sqlite", sqliteDSN(path, options.BusyTimeout))
	if err != nil {
		return nil, classify("open SQLite", err)
	}
	sqlDB.SetMaxOpenConns(options.MaximumConnections)
	sqlDB.SetMaxIdleConns(options.MaximumConnections)
	database := &Database{
		sql:         sqlDB,
		path:        path,
		busyTimeout: options.BusyTimeout,
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, classify("ping SQLite", err)
	}
	if err := database.verifyConnectionPolicy(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	// WAL mode is set by the DSN above, so the write-ahead log and the shared
	// memory file exist by now. They carry committed rows, so leaving them at
	// the inherited permissions would expose the database through its sidecars
	// after the database itself was restricted.
	if err := restrictDatabaseArtifacts(path); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return database, nil
}

func sqliteDSN(path string, busyTimeout time.Duration) string {
	urlPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	location := &url.URL{Scheme: "file", Path: urlPath}
	query := location.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout("+strconv.FormatInt(busyTimeout.Milliseconds(), 10)+")")
	query.Add("_pragma", "synchronous(FULL)")
	query.Add("_pragma", "trusted_schema(OFF)")
	query.Set("_txlock", "immediate")
	query.Set("_dqs", "false")
	query.Set("_error_rc", "true")
	location.RawQuery = query.Encode()
	return location.String()
}

func (database *Database) verifyConnectionPolicy(ctx context.Context) error {
	connection, err := database.sql.Conn(ctx)
	if err != nil {
		return classify("acquire policy-check connection", err)
	}
	defer connection.Close()

	var foreignKeys int
	if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return classify("read foreign-key policy", err)
	}
	if foreignKeys != 1 {
		return errors.New("SQLite foreign-key enforcement is unavailable")
	}
	var journalMode string
	if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return classify("read journal policy", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("SQLite journal mode is %q, want WAL", journalMode)
	}
	var synchronous int
	if err := connection.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		return classify("read synchronous policy", err)
	}
	if synchronous != 2 {
		return fmt.Errorf("SQLite synchronous policy is %d, want FULL (2)", synchronous)
	}
	var busyMillis int64
	if err := connection.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyMillis); err != nil {
		return classify("read busy-timeout policy", err)
	}
	if busyMillis != database.busyTimeout.Milliseconds() {
		return fmt.Errorf(
			"SQLite busy timeout is %d ms, want %d ms",
			busyMillis,
			database.busyTimeout.Milliseconds(),
		)
	}
	return nil
}

// CheckHealth verifies reads, durable writes, foreign-key enforcement, and
// connection policy without retaining probe schema or rows.
func (database *Database) CheckHealth(ctx context.Context) (Health, error) {
	report := Health{BusyTimeout: database.busyTimeout}
	connection, err := database.sql.Conn(ctx)
	if err != nil {
		return report, classify("acquire health-check connection", err)
	}
	defer connection.Close()

	var foreignKeys int
	if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return report, classify("health check foreign keys", err)
	}
	report.ForeignKeysEnabled = foreignKeys == 1
	if !report.ForeignKeysEnabled {
		return report, errors.New("health check found foreign keys disabled")
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&report.JournalMode); err != nil {
		return report, classify("health check journal mode", err)
	}
	if !strings.EqualFold(report.JournalMode, "wal") {
		return report, fmt.Errorf("health check journal mode is %q, want WAL", report.JournalMode)
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&report.Synchronous); err != nil {
		return report, classify("health check synchronous policy", err)
	}

	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return report, classify("begin health-check transaction", err)
	}
	defer transaction.Rollback()
	statements := []string{
		`CREATE TABLE __codeflux_health_parent (id INTEGER PRIMARY KEY) STRICT`,
		`CREATE TABLE __codeflux_health_child (
			id INTEGER PRIMARY KEY,
			parent_id INTEGER NOT NULL REFERENCES __codeflux_health_parent(id)
		) STRICT`,
		`INSERT INTO __codeflux_health_parent (id) VALUES (1)`,
		`INSERT INTO __codeflux_health_child (id, parent_id) VALUES (1, 1)`,
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return report, classify("write health-check transaction", err)
		}
	}
	report.Writable = true
	var count int
	if err := transaction.QueryRowContext(
		ctx,
		`SELECT count(*) FROM __codeflux_health_child WHERE parent_id = 1`,
	).Scan(&count); err != nil {
		return report, classify("read health-check transaction", err)
	}
	if count != 1 {
		return report, fmt.Errorf("health-check read returned %d rows, want 1", count)
	}
	report.Readable = true
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO __codeflux_health_child (id, parent_id) VALUES (2, 99)`,
	); err == nil {
		return report, errors.New("health check foreign-key violation was accepted")
	} else if !errors.Is(classify("health check foreign-key rejection", err), ErrConstraint) {
		return report, classify("health check foreign-key rejection", err)
	}
	return report, nil
}

// Close checkpoints the WAL and closes the pool. It is safe to call repeatedly.
func (database *Database) Close(ctx context.Context) error {
	database.closeOnce.Do(func() {
		var checkpointErr error
		checkpoint, err := database.CheckpointWAL(ctx, true)
		if err != nil {
			checkpointErr = err
		} else if checkpoint.Busy {
			checkpointErr = fmt.Errorf(
				"checkpoint SQLite WAL: %w (%d log frames, %d checkpointed)",
				ErrBusy,
				checkpoint.LogFrames,
				checkpoint.CheckpointedFrames,
			)
		}
		database.closeErr = errors.Join(checkpointErr, classify("close SQLite", database.sql.Close()))
	})
	return database.closeErr
}

// Path returns the resolved authoritative database path for internal lifecycle
// coordination. It must not be emitted in user diagnostics without redaction.
func (database *Database) Path() string {
	return database.path
}
