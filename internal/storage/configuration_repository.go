package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	maximumConfigurationBytes = 64 * 1024
	maximumSourcesBytes       = 8 * 1024
)

func (repositories *Repositories) CreateSettingsRevision(
	ctx context.Context,
	input CreateSettingsRevision,
) (SettingsRevision, error) {
	if err := validateSettingsRevisionInput(input); err != nil {
		return SettingsRevision{}, err
	}
	if err := validateNonSecretJSONObject(
		"configuration",
		input.ConfigurationJSON,
		maximumConfigurationBytes,
	); err != nil {
		return SettingsRevision{}, err
	}
	if err := validateBounded("settings idempotency key", input.IdempotencyKey, 255); err != nil {
		return SettingsRevision{}, err
	}
	contentHash := hashJSON(input.ConfigurationJSON)
	now, micros := repositories.timestamp()
	var revision SettingsRevision
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findSettingsRevisionByIdempotency(
			ctx,
			transaction,
			input.Scope,
			input.RepositoryID,
			input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ConfigurationJSON != input.ConfigurationJSON ||
				existing.ContentSHA256 != contentHash ||
				!sameRepositoryID(existing.RepositoryID, input.RepositoryID) ||
				!sameOptionalString(existing.ApprovalReference, input.ApprovalReference) {
				return typedError(
					ErrConflict,
					"create idempotent settings revision",
					errors.New("idempotency key belongs to different settings"),
				)
			}
			revision = existing
			return nil
		}
		var next uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(revision), 0) + 1 FROM settings_revisions`,
		).Scan(&next); err != nil {
			return classify("allocate settings revision", err)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO settings_revisions (
				revision, scope, repository_id, configuration_json,
				content_sha256, approved, approval_reference,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)`,
			next,
			input.Scope,
			nullableRepositoryID(input.RepositoryID),
			input.ConfigurationJSON,
			contentHash,
			nullableString(input.ApprovalReference),
			input.IdempotencyKey,
			micros,
		); err != nil {
			return repositoryWriteError("create settings revision", err)
		}
		revision = SettingsRevision{
			Revision:          next,
			Scope:             input.Scope,
			RepositoryID:      input.RepositoryID,
			ConfigurationJSON: input.ConfigurationJSON,
			ContentSHA256:     contentHash,
			ApprovalReference: input.ApprovalReference,
			IdempotencyKey:    input.IdempotencyKey,
			CreatedAt:         now,
		}
		return nil
	})
	return revision, err
}

func (repositories *Repositories) GetSettingsRevision(
	ctx context.Context,
	revision uint64,
) (SettingsRevision, error) {
	return scanSettingsRevision(repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT revision, scope, repository_id, configuration_json,
		        content_sha256, approval_reference, idempotency_key,
		        created_at_unix_micros
		 FROM settings_revisions
		 WHERE revision = ?`,
		revision,
	), "get settings revision")
}

func (repositories *Repositories) RecordRunConfiguration(
	ctx context.Context,
	input RecordRunConfiguration,
) (RunConfiguration, error) {
	if input.RunID.IsZero() {
		return RunConfiguration{}, errors.New("run ID must not be empty")
	}
	if err := validateNonSecretJSONObject(
		"effective configuration",
		input.EffectiveJSON,
		maximumConfigurationBytes,
	); err != nil {
		return RunConfiguration{}, err
	}
	if err := validateNonSecretJSONObject(
		"configuration sources",
		input.SourcesJSON,
		maximumSourcesBytes,
	); err != nil {
		return RunConfiguration{}, err
	}
	contentHash := hashJSON(input.EffectiveJSON)
	now, micros := repositories.timestamp()
	var recorded RunConfiguration
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findRunConfiguration(ctx, transaction.sql, input.RunID)
		if err != nil {
			return err
		}
		if found {
			if existing.SettingsRevision != input.SettingsRevision ||
				existing.EffectiveJSON != input.EffectiveJSON ||
				existing.SourcesJSON != input.SourcesJSON {
				return typedError(
					ErrConflict,
					"record idempotent run configuration",
					errors.New("run already has different effective configuration"),
				)
			}
			recorded = existing
			return nil
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO run_configurations (
				run_id, settings_revision, effective_configuration_json,
				sources_json, content_sha256, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?)`,
			input.RunID,
			input.SettingsRevision,
			input.EffectiveJSON,
			input.SourcesJSON,
			contentHash,
			micros,
		); err != nil {
			return repositoryWriteError("record run configuration", err)
		}
		recorded = RunConfiguration{
			RunID:            input.RunID,
			SettingsRevision: input.SettingsRevision,
			EffectiveJSON:    input.EffectiveJSON,
			SourcesJSON:      input.SourcesJSON,
			ContentSHA256:    contentHash,
			CreatedAt:        now,
		}
		return nil
	})
	return recorded, err
}

func (repositories *Repositories) GetRunConfiguration(
	ctx context.Context,
	runID domain.RunID,
) (RunConfiguration, error) {
	if runID.IsZero() {
		return RunConfiguration{}, errors.New("run ID must not be empty")
	}
	configuration, found, err := findRunConfiguration(ctx, repositories.database.sql, runID)
	if err != nil {
		return RunConfiguration{}, err
	}
	if !found {
		return RunConfiguration{}, typedError(
			ErrNotFound,
			"get run configuration",
			sql.ErrNoRows,
		)
	}
	return configuration, nil
}

func validateSettingsRevisionInput(input CreateSettingsRevision) error {
	switch input.Scope {
	case SettingsScopeUser:
		if input.RepositoryID != nil || input.ApprovalReference != nil {
			return errors.New("user settings must not carry repository approval")
		}
	case SettingsScopeRepository:
		if input.RepositoryID == nil || input.RepositoryID.IsZero() {
			return errors.New("repository settings require a repository ID")
		}
		if input.ApprovalReference == nil ||
			strings.TrimSpace(*input.ApprovalReference) == "" ||
			len(*input.ApprovalReference) > 255 {
			return errors.New("repository settings require a bounded approval reference")
		}
	default:
		return errors.New("settings scope is invalid")
	}
	return nil
}

func validateNonSecretJSONObject(name, raw string, maximumBytes int) error {
	if len(raw) == 0 || len(raw) > maximumBytes {
		return fmt.Errorf("%s must be a non-empty bounded JSON object", name)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return fmt.Errorf("%s must be valid JSON: %w", name, err)
	}
	if value == nil {
		return fmt.Errorf("%s must be a JSON object", name)
	}
	if key := forbiddenConfigurationKey(value); key != "" {
		return fmt.Errorf("%s contains forbidden secret-bearing field %q", name, key)
	}
	return nil
}

func forbiddenConfigurationKey(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			for _, forbidden := range []string{
				"credential",
				"secret",
				"api_key",
				"token",
				"password",
				"private_key",
				"authorization",
			} {
				if strings.Contains(normalized, forbidden) {
					return key
				}
			}
			if nested := forbiddenConfigurationKey(child); nested != "" {
				return nested
			}
		}
	case []any:
		for _, child := range typed {
			if nested := forbiddenConfigurationKey(child); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func hashJSON(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum)
}

func findSettingsRevisionByIdempotency(
	ctx context.Context,
	transaction *Transaction,
	scope SettingsScope,
	repositoryID *domain.RepositoryID,
	idempotencyKey string,
) (SettingsRevision, bool, error) {
	revision, err := scanSettingsRevision(transaction.sql.QueryRowContext(
		ctx,
		`SELECT revision, scope, repository_id, configuration_json,
		        content_sha256, approval_reference, idempotency_key,
		        created_at_unix_micros
		 FROM settings_revisions
		 WHERE scope = ?
		   AND repository_id IS ?
		   AND idempotency_key = ?`,
		scope,
		nullableRepositoryID(repositoryID),
		idempotencyKey,
	), "find settings revision by idempotency")
	if errors.Is(err, ErrNotFound) {
		return SettingsRevision{}, false, nil
	}
	return revision, err == nil, err
}

func scanSettingsRevision(row rowScanner, operation string) (SettingsRevision, error) {
	var (
		revision        SettingsRevision
		repositoryRaw   sql.NullString
		approvalRaw     sql.NullString
		createdAtMicros int64
	)
	if err := row.Scan(
		&revision.Revision,
		&revision.Scope,
		&repositoryRaw,
		&revision.ConfigurationJSON,
		&revision.ContentSHA256,
		&approvalRaw,
		&revision.IdempotencyKey,
		&createdAtMicros,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SettingsRevision{}, typedError(ErrNotFound, operation, err)
		}
		return SettingsRevision{}, classify(operation, err)
	}
	if repositoryRaw.Valid {
		parsed, err := domain.ParseRepositoryID(repositoryRaw.String)
		if err != nil {
			return SettingsRevision{}, replayCorruption("settings repository identity is invalid")
		}
		revision.RepositoryID = &parsed
	}
	if approvalRaw.Valid {
		revision.ApprovalReference = &approvalRaw.String
	}
	revision.CreatedAt = time.UnixMicro(createdAtMicros).UTC()
	return revision, nil
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func findRunConfiguration(
	ctx context.Context,
	queries queryRower,
	runID domain.RunID,
) (RunConfiguration, bool, error) {
	var (
		configuration RunConfiguration
		createdMicros int64
	)
	if err := queries.QueryRowContext(
		ctx,
		`SELECT run_id, settings_revision, effective_configuration_json,
		        sources_json, content_sha256, created_at_unix_micros
		 FROM run_configurations
		 WHERE run_id = ?`,
		runID,
	).Scan(
		&configuration.RunID,
		&configuration.SettingsRevision,
		&configuration.EffectiveJSON,
		&configuration.SourcesJSON,
		&configuration.ContentSHA256,
		&createdMicros,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunConfiguration{}, false, nil
		}
		return RunConfiguration{}, false, classify("find run configuration", err)
	}
	configuration.CreatedAt = time.UnixMicro(createdMicros).UTC()
	return configuration, true, nil
}

func nullableRepositoryID(value *domain.RepositoryID) any {
	if value == nil {
		return nil
	}
	return *value
}

func sameRepositoryID(left, right *domain.RepositoryID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
