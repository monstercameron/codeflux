package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"codeflux.dev/codeflux/migrations"
)

const migrationSpaceReserve = 16 * 1024 * 1024

// MigrationOptions binds schema changes to one application and catalog.
type MigrationOptions struct {
	ApplicationVersion string
	BackupDirectory    string
	Sources            []migrations.Source
	Now                func() time.Time
	AvailableBytes     func(string) (uint64, error)
}

// MigrationResult reports one completed or unnecessary migration run.
type MigrationResult struct {
	FromVersion int
	ToVersion   int
	BackupPath  string
	Applied     int
}

// Migrate applies ordered embedded migrations under cross-process authority.
func (database *Database) Migrate(
	ctx context.Context,
	options MigrationOptions,
) (MigrationResult, error) {
	options, err := normalizedMigrationOptions(database, options)
	if err != nil {
		return MigrationResult{}, err
	}
	lock := flock.New(
		database.path+".migration.lock",
		flock.SetPermissions(0o600),
	)
	locked, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if locked {
		// flock's SetPermissions is a POSIX mode and is inert on Windows, so
		// the lock file is narrowed explicitly once it exists. It gates who
		// may apply schema changes, which is why it is on the protected list
		// rather than treated as a scratch file.
		if restrictErr := restrictToCurrentUser(database.path + ".migration.lock"); restrictErr != nil {
			_ = lock.Close()
			return MigrationResult{}, restrictErr
		}
	}
	if err != nil {
		return MigrationResult{}, &Error{
			Kind:      ErrMigrationLocked,
			Operation: "acquire migration authority",
			Cause:     err,
		}
	}
	if !locked {
		return MigrationResult{}, &Error{
			Kind:      ErrMigrationLocked,
			Operation: "acquire migration authority",
			Cause:     context.Canceled,
		}
	}
	defer lock.Close()

	if err := database.ensureMigrationControl(ctx, options); err != nil {
		return MigrationResult{}, err
	}
	current, err := database.currentSchemaVersion(ctx)
	if err != nil {
		return MigrationResult{}, err
	}
	latest := len(options.Sources) - 1
	result := MigrationResult{FromVersion: current, ToVersion: current}
	if current > latest {
		return result, &Error{
			Kind:      ErrSchemaNewer,
			Operation: "compare database schema with binary",
			Cause: fmt.Errorf(
				"database version %d exceeds supported version %d",
				current,
				latest,
			),
		}
	}
	if err := database.recoverOrRefusePreviousFailure(ctx, options); err != nil {
		return result, err
	}
	if err := database.verifyAppliedMigrations(ctx, options.Sources, current); err != nil {
		return result, err
	}
	if current == latest {
		return result, nil
	}

	backupPath, err := database.prepareMigrationBackup(ctx, options, current, latest)
	if err != nil {
		return result, err
	}
	result.BackupPath = backupPath
	for _, source := range options.Sources[current+1:] {
		if err := verifyMigrationSource(source); err != nil {
			return result, err
		}
		started := options.Now().UTC().UnixMicro()
		historyID, err := database.recordMigrationStarted(
			ctx,
			source,
			options.ApplicationVersion,
			started,
			backupPath,
		)
		if err != nil {
			return result, err
		}
		if err := database.applyMigration(
			ctx,
			source,
			historyID,
			options.ApplicationVersion,
			options.Now().UTC().UnixMicro(),
		); err != nil {
			recoveryErr := database.restoreAndRecordFailure(
				ctx,
				options,
				source,
				started,
				backupPath,
				err,
			)
			return result, &Error{
				Kind:      ErrMigrationFailed,
				Operation: "apply database migration",
				Cause:     errors.Join(err, recoveryErr),
			}
		}
		result.Applied++
		result.ToVersion = source.Descriptor.Number
	}
	if err := database.integrityCheck(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func normalizedMigrationOptions(
	database *Database,
	options MigrationOptions,
) (MigrationOptions, error) {
	options.ApplicationVersion = strings.TrimSpace(options.ApplicationVersion)
	if options.ApplicationVersion == "" {
		return MigrationOptions{}, errors.New("application version must not be empty")
	}
	if len(options.ApplicationVersion) > 128 {
		return MigrationOptions{}, errors.New("application version must not exceed 128 bytes")
	}
	if options.Sources == nil {
		sources, err := migrations.Sources()
		if err != nil {
			return MigrationOptions{}, &Error{
				Kind:      ErrMigrationChecksum,
				Operation: "load embedded migrations",
				Cause:     err,
			}
		}
		options.Sources = sources
	}
	for index, source := range options.Sources {
		if source.Descriptor.Number != index {
			return MigrationOptions{}, fmt.Errorf(
				"migration source position %d has number %d",
				index,
				source.Descriptor.Number,
			)
		}
		if err := verifyMigrationSource(source); err != nil {
			return MigrationOptions{}, err
		}
	}
	if options.BackupDirectory == "" {
		options.BackupDirectory = filepath.Join(filepath.Dir(database.path), "backups")
	}
	resolved, err := filepath.Abs(options.BackupDirectory)
	if err != nil {
		return MigrationOptions{}, fmt.Errorf("resolve migration backup directory: %w", err)
	}
	options.BackupDirectory = resolved
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.AvailableBytes == nil {
		options.AvailableBytes = operatingSystemAvailableBytes
	}
	return options, nil
}

func verifyMigrationSource(source migrations.Source) error {
	sum := sha256.Sum256([]byte(source.SQL))
	checksum := hex.EncodeToString(sum[:])
	if source.Descriptor.SHA256 != checksum {
		return &Error{
			Kind:      ErrMigrationChecksum,
			Operation: "verify migration source",
			Cause: fmt.Errorf(
				"migration %d checksum is %s, want %s",
				source.Descriptor.Number,
				checksum,
				source.Descriptor.SHA256,
			),
		}
	}
	return nil
}

func (database *Database) ensureMigrationControl(
	ctx context.Context,
	options MigrationOptions,
) error {
	transaction, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return classify("begin migration-control transaction", err)
	}
	defer transaction.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS codeflux_schema_version (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			version INTEGER NOT NULL CHECK (version >= -1),
			application_version TEXT NOT NULL CHECK (length(application_version) BETWEEN 1 AND 128),
			updated_at_unix_micros INTEGER NOT NULL CHECK (updated_at_unix_micros >= 0)
		) STRICT`,
		`CREATE TABLE IF NOT EXISTS codeflux_migration_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			migration_number INTEGER NOT NULL CHECK (migration_number >= 0),
			name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
			checksum TEXT NOT NULL CHECK (length(checksum) = 64),
			application_version TEXT NOT NULL CHECK (length(application_version) BETWEEN 1 AND 128),
			started_at_unix_micros INTEGER NOT NULL CHECK (started_at_unix_micros >= 0),
			completed_at_unix_micros INTEGER,
			result TEXT NOT NULL CHECK (result IN ('started', 'succeeded', 'failed')),
			error_text TEXT,
			backup_path TEXT NOT NULL CHECK (length(backup_path) BETWEEN 1 AND 4096),
			CHECK (
				(result = 'started' AND completed_at_unix_micros IS NULL AND error_text IS NULL)
				OR
				(result = 'succeeded' AND completed_at_unix_micros IS NOT NULL AND error_text IS NULL)
				OR
				(result = 'failed' AND completed_at_unix_micros IS NOT NULL AND error_text IS NOT NULL)
			)
		) STRICT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS codeflux_migration_succeeded_once
			ON codeflux_migration_history(migration_number)
			WHERE result = 'succeeded'`,
		`CREATE INDEX IF NOT EXISTS codeflux_migration_result
			ON codeflux_migration_history(result, id DESC)`,
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return classify("create migration-control schema", err)
		}
	}
	_, err = transaction.ExecContext(
		ctx,
		`INSERT INTO codeflux_schema_version (
			singleton, version, application_version, updated_at_unix_micros
		) VALUES (1, -1, ?, ?)
		ON CONFLICT(singleton) DO NOTHING`,
		options.ApplicationVersion,
		options.Now().UTC().UnixMicro(),
	)
	if err != nil {
		return classify("initialize schema version", err)
	}
	if err := transaction.Commit(); err != nil {
		return classify("commit migration-control schema", err)
	}
	return nil
}

// SchemaVersion reports the applied application schema version, or zero when
// the database has none yet.
//
// An unmigrated database is an ordinary state — it is what an operator has
// immediately after the file is created — so it is reported as a version of
// zero rather than as a missing-table error. currentSchemaVersion keeps the
// stricter behaviour, because inside a migration a missing version table is a
// genuine fault.
func (database *Database) SchemaVersion(ctx context.Context) (int, error) {
	var present string
	if err := database.sql.QueryRowContext(
		ctx,
		`SELECT name FROM sqlite_master
		 WHERE type = 'table' AND name = 'codeflux_schema_version'`,
	).Scan(&present); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, classify("look for the schema version table", err)
	}
	return database.currentSchemaVersion(ctx)
}

func (database *Database) currentSchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := database.sql.QueryRowContext(
		ctx,
		`SELECT version FROM codeflux_schema_version WHERE singleton = 1`,
	).Scan(&version); err != nil {
		return 0, classify("read schema version", err)
	}
	return version, nil
}

func (database *Database) verifyAppliedMigrations(
	ctx context.Context,
	sources []migrations.Source,
	current int,
) error {
	for number := 0; number <= current; number++ {
		var checksum string
		err := database.sql.QueryRowContext(
			ctx,
			`SELECT checksum
			 FROM codeflux_migration_history
			 WHERE migration_number = ? AND result = 'succeeded'`,
			number,
		).Scan(&checksum)
		if err != nil {
			return &Error{
				Kind:      ErrMigrationChecksum,
				Operation: "verify applied migration history",
				Cause:     err,
			}
		}
		if checksum != sources[number].Descriptor.SHA256 {
			return &Error{
				Kind:      ErrMigrationChecksum,
				Operation: "verify applied migration history",
				Cause: fmt.Errorf(
					"migration %d recorded checksum %s, want %s",
					number,
					checksum,
					sources[number].Descriptor.SHA256,
				),
			}
		}
	}
	return nil
}

func (database *Database) prepareMigrationBackup(
	ctx context.Context,
	options MigrationOptions,
	current int,
	latest int,
) (string, error) {
	info, err := os.Stat(database.path)
	if err != nil {
		return "", classify("inspect database before migration", err)
	}
	size := uint64(info.Size())
	required := size*2 + migrationSpaceReserve
	if required < size {
		return "", &Error{
			Kind:      ErrDiskSpace,
			Operation: "calculate migration disk requirement",
			Cause:     errors.New("database size overflow"),
		}
	}
	available, err := options.AvailableBytes(filepath.Dir(database.path))
	if err != nil {
		return "", fmt.Errorf("query migration disk space: %w", err)
	}
	if available < required {
		return "", &Error{
			Kind:      ErrDiskSpace,
			Operation: "validate migration disk space",
			Cause: fmt.Errorf(
				"%d bytes available, %d required",
				available,
				required,
			),
		}
	}
	name := "codeflux-v" + strconv.Itoa(current) +
		"-to-v" + strconv.Itoa(latest) +
		"-" + options.Now().UTC().Format("20060102T150405.000000000Z") +
		".sqlite3"
	destination := filepath.Join(options.BackupDirectory, name)
	if err := database.Backup(ctx, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func (database *Database) recordMigrationStarted(
	ctx context.Context,
	source migrations.Source,
	applicationVersion string,
	started int64,
	backupPath string,
) (int64, error) {
	result, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO codeflux_migration_history (
			migration_number, name, checksum, application_version,
			started_at_unix_micros, result, backup_path
		) VALUES (?, ?, ?, ?, ?, 'started', ?)`,
		source.Descriptor.Number,
		source.Descriptor.Name,
		source.Descriptor.SHA256,
		applicationVersion,
		started,
		backupPath,
	)
	if err != nil {
		return 0, classify("record migration start", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, classify("read migration-history identity", err)
	}
	return id, nil
}

func (database *Database) applyMigration(
	ctx context.Context,
	source migrations.Source,
	historyID int64,
	applicationVersion string,
	completed int64,
) error {
	transaction, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return classify("begin schema migration", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, source.SQL); err != nil {
		return classify("execute schema migration", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE codeflux_schema_version
		 SET version = ?, application_version = ?, updated_at_unix_micros = ?
		 WHERE singleton = 1`,
		source.Descriptor.Number,
		applicationVersion,
		completed,
	); err != nil {
		return classify("advance schema version", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE codeflux_migration_history
		 SET completed_at_unix_micros = ?, result = 'succeeded'
		 WHERE id = ? AND result = 'started'`,
		completed,
		historyID,
	); err != nil {
		return classify("complete migration history", err)
	}
	if err := transaction.Commit(); err != nil {
		return classify("commit schema migration", err)
	}
	return nil
}

func (database *Database) recoverOrRefusePreviousFailure(
	ctx context.Context,
	options MigrationOptions,
) error {
	var (
		number     int
		name       string
		checksum   string
		started    int64
		result     string
		errorText  *string
		backupPath string
	)
	err := database.sql.QueryRowContext(
		ctx,
		`SELECT migration_number, name, checksum, started_at_unix_micros,
		        result, error_text, backup_path
		 FROM codeflux_migration_history
		 WHERE result IN ('started', 'failed')
		 ORDER BY id DESC
		 LIMIT 1`,
	).Scan(&number, &name, &checksum, &started, &result, &errorText, &backupPath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return classify("read incomplete migration history", err)
	}
	if result == "failed" {
		reason := "previous migration failed"
		if errorText != nil {
			reason = *errorText
		}
		return &Error{
			Kind:      ErrMigrationFailed,
			Operation: "refuse repeated failed migration",
			Cause:     errors.New(reason),
		}
	}
	source := migrations.Source{
		Descriptor: migrations.Descriptor{
			Number: number,
			Name:   name,
			SHA256: checksum,
		},
	}
	interrupted := errors.New("migration was interrupted before completion")
	if err := database.Restore(ctx, backupPath); err != nil {
		return &Error{
			Kind:      ErrMigrationFailed,
			Operation: "restore interrupted migration backup",
			Cause:     err,
		}
	}
	if err := database.ensureMigrationControl(ctx, options); err != nil {
		return err
	}
	if err := database.recordMigrationFailure(
		ctx,
		source,
		options.ApplicationVersion,
		started,
		options.Now().UTC().UnixMicro(),
		backupPath,
		interrupted,
	); err != nil {
		return err
	}
	return &Error{
		Kind:      ErrMigrationFailed,
		Operation: "recover interrupted migration",
		Cause:     interrupted,
	}
}

func (database *Database) restoreAndRecordFailure(
	ctx context.Context,
	options MigrationOptions,
	source migrations.Source,
	started int64,
	backupPath string,
	migrationErr error,
) error {
	if err := database.Restore(ctx, backupPath); err != nil {
		return err
	}
	if err := database.ensureMigrationControl(ctx, options); err != nil {
		return err
	}
	return database.recordMigrationFailure(
		ctx,
		source,
		options.ApplicationVersion,
		started,
		options.Now().UTC().UnixMicro(),
		backupPath,
		migrationErr,
	)
}

func (database *Database) recordMigrationFailure(
	ctx context.Context,
	source migrations.Source,
	applicationVersion string,
	started int64,
	completed int64,
	backupPath string,
	failure error,
) error {
	errorText := failure.Error()
	if len(errorText) > 1024 {
		errorText = errorText[:1024]
	}
	_, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO codeflux_migration_history (
			migration_number, name, checksum, application_version,
			started_at_unix_micros, completed_at_unix_micros,
			result, error_text, backup_path
		) VALUES (?, ?, ?, ?, ?, ?, 'failed', ?, ?)`,
		source.Descriptor.Number,
		source.Descriptor.Name,
		source.Descriptor.SHA256,
		applicationVersion,
		started,
		completed,
		errorText,
		backupPath,
	)
	if err != nil {
		return classify("record migration failure", err)
	}
	return nil
}

func (database *Database) integrityCheck(ctx context.Context) error {
	var result string
	if err := database.sql.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return classify("integrity-check migrated database", err)
	}
	if result != "ok" {
		return &Error{
			Kind:      ErrCorrupt,
			Operation: "integrity-check migrated database",
			Cause:     errors.New("SQLite integrity check did not return ok"),
		}
	}
	return nil
}
