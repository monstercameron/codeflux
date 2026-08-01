package testfixtures

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/migrations"
	_ "modernc.org/sqlite" // fixture databases use the same driver as production
)

// DatabaseFixture is a real temporary SQLite database with the real migration
// catalog applied (M22-105).
//
// It deliberately does not depend on internal/storage. A fixture built on the
// package under test would pass whenever that package's own opener agreed with
// itself; building it from the migration catalog and the driver directly means
// a schema the production opener cannot read is a failure a test can see.
type DatabaseFixture struct {
	Path          string
	Directory     string
	DB            *sql.DB
	AppliedCount  int
	LatestVersion int
	preserve      bool
}

// DatabaseFixtureOptions controls how a fixture database is built.
type DatabaseFixtureOptions struct {
	// Directory is where the database file is created. It must be a temporary
	// directory: cleanup deletes it recursively and refuses anything else.
	Directory string
	// BusyTimeout matches the production default when zero.
	BusyTimeout time.Duration
	// PreserveOnCleanup keeps the directory after Close, for inspecting a
	// failure. It is explicit because leaving databases behind by default
	// fills a developer's disk with files nobody reads.
	PreserveOnCleanup bool
	// StopAtVersion applies only migrations up to and including this version,
	// so a test can build the previous schema and exercise an upgrade. Zero
	// applies the whole catalog.
	StopAtVersion int
}

// NewDatabaseFixture creates and migrates a temporary database (M22-105).
func NewDatabaseFixture(
	ctx context.Context,
	options DatabaseFixtureOptions,
) (*DatabaseFixture, error) {
	if strings.TrimSpace(options.Directory) == "" {
		return nil, errors.New("a database fixture requires a directory")
	}
	if err := ValidateCleanupTarget(options.Directory); err != nil {
		return nil, fmt.Errorf("fixture directory is not safely cleanable: %w", err)
	}
	if options.BusyTimeout <= 0 {
		options.BusyTimeout = 5 * time.Second
	}
	if err := os.MkdirAll(options.Directory, 0o755); err != nil {
		return nil, fmt.Errorf("create fixture directory: %w", err)
	}

	path := filepath.Join(options.Directory, "fixture.sqlite3")
	database, err := sql.Open("sqlite", fixtureDSN(path, options.BusyTimeout))
	if err != nil {
		return nil, fmt.Errorf("open fixture database: %w", err)
	}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("reach fixture database: %w", err)
	}

	sources, err := migrations.Sources()
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("load migration catalog: %w", err)
	}
	applied := 0
	latest := 0
	for _, source := range sources {
		if options.StopAtVersion > 0 && source.Descriptor.Number > options.StopAtVersion {
			break
		}
		if _, err := database.ExecContext(ctx, source.SQL); err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("apply migration %d (%s): %w",
				source.Descriptor.Number, source.Descriptor.Name, err)
		}
		applied++
		latest = source.Descriptor.Number
	}
	if applied == 0 {
		_ = database.Close()
		return nil, errors.New("no migration was applied, so the fixture has no schema")
	}

	fixture := &DatabaseFixture{
		Path: path, Directory: options.Directory, DB: database,
		AppliedCount: applied, LatestVersion: latest,
		preserve: options.PreserveOnCleanup,
	}
	// Integrity is asserted at construction rather than left to the caller: a
	// fixture that silently produced a corrupt database would turn every test
	// built on it into a test of corruption handling.
	if err := fixture.AssertIntegrity(ctx); err != nil {
		_ = fixture.Close()
		return nil, err
	}
	return fixture, nil
}

// AssertIntegrity runs SQLite's own integrity and foreign-key checks.
func (fixture *DatabaseFixture) AssertIntegrity(ctx context.Context) error {
	var result string
	if err := fixture.DB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("run integrity check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("fixture database failed its integrity check: %s", result)
	}

	rows, err := fixture.DB.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("run foreign key check: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		return errors.New("fixture database has foreign key violations")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read foreign key check: %w", err)
	}

	// Foreign keys must actually be ON. A database that passed the check only
	// because enforcement was disabled proves nothing.
	var enabled int
	if err := fixture.DB.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled); err != nil {
		return fmt.Errorf("read foreign key setting: %w", err)
	}
	if enabled != 1 {
		return errors.New("fixture database has foreign key enforcement disabled")
	}
	return nil
}

// TableNames returns every table in the fixture schema, so a test can assert
// the migration catalog produced what it expected.
func (fixture *DatabaseFixture) TableNames(ctx context.Context) ([]string, error) {
	rows, err := fixture.DB.QueryContext(ctx,
		`SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list fixture tables: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// Close closes the database and removes the fixture directory unless
// preservation was requested (M22-112 governs the deletion).
func (fixture *DatabaseFixture) Close() error {
	var closeErr error
	if fixture.DB != nil {
		closeErr = fixture.DB.Close()
		fixture.DB = nil
	}
	if err := RemoveFixtureDirectory(fixture.Directory, fixture.preserve); err != nil {
		if closeErr != nil {
			return fmt.Errorf("%w; and cleanup: %v", closeErr, err)
		}
		return err
	}
	return closeErr
}

func fixtureDSN(path string, busyTimeout time.Duration) string {
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
	location.RawQuery = query.Encode()
	return location.String()
}
