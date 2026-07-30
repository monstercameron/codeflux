package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const contextManifestTrust = "untrusted-repository-data"

func (repositories *Repositories) RecordContextManifest(
	ctx context.Context,
	input RecordContextManifest,
) (ContextManifest, error) {
	if err := validateContextManifest(input); err != nil {
		return ContextManifest{}, err
	}
	reasonsJSON := make([]string, len(input.Items))
	for index, item := range input.Items {
		encoded, err := json.Marshal(item.Reasons)
		if err != nil {
			return ContextManifest{}, fmt.Errorf("encode context item reasons: %w", err)
		}
		reasonsJSON[index] = string(encoded)
	}
	now, micros := repositories.timestamp()
	recorded := ContextManifest{
		ID:                  input.ID,
		RepositoryID:        input.RepositoryID,
		RepositoryRevision:  input.RepositoryRevision,
		MapRevision:         input.MapRevision,
		RequirementSHA256:   input.RequirementSHA256,
		SelectionPolicy:     input.SelectionPolicy,
		MaxFiles:            input.MaxFiles,
		MaxBytes:            input.MaxBytes,
		MaxEstimatedTokens:  input.MaxEstimatedTokens,
		UsedFiles:           input.UsedFiles,
		UsedBytes:           input.UsedBytes,
		UsedEstimatedTokens: input.UsedEstimatedTokens,
		Items:               cloneContextManifestItems(input.Items),
		Exclusions:          append([]ContextManifestExclusion(nil), input.Exclusions...),
		CreatedAt:           now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO context_manifests (
				id, repository_id, repository_revision, map_revision,
				requirement_sha256, selection_policy_version,
				max_files, max_bytes, max_estimated_tokens,
				used_files, used_bytes, used_estimated_tokens,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID,
			input.RepositoryID,
			input.RepositoryRevision,
			input.MapRevision,
			input.RequirementSHA256,
			input.SelectionPolicy,
			input.MaxFiles,
			input.MaxBytes,
			input.MaxEstimatedTokens,
			input.UsedFiles,
			input.UsedBytes,
			input.UsedEstimatedTokens,
			micros,
		); err != nil {
			return repositoryWriteError("record context manifest", err)
		}
		for index, item := range input.Items {
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO context_manifest_items (
					manifest_id, ordinal, repository_relative_path, kind,
					start_line, end_line, content_redacted, content_sha256,
					reasons_json, trust, generated, binary, minified, vendor,
					dependency, estimated_tokens
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				input.ID,
				index,
				item.Path,
				item.Kind,
				item.StartLine,
				item.EndLine,
				item.ContentRedacted,
				item.ContentSHA256,
				reasonsJSON[index],
				item.Trust,
				item.Generated,
				item.Binary,
				item.Minified,
				item.Vendor,
				item.Dependency,
				item.EstimatedTokens,
			); err != nil {
				return repositoryWriteError("record context manifest item", err)
			}
		}
		for index, exclusion := range input.Exclusions {
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO context_manifest_exclusions (
					manifest_id, ordinal, repository_relative_path, reason
				) VALUES (?, ?, ?, ?)`,
				input.ID,
				index,
				exclusion.Path,
				exclusion.Reason,
			); err != nil {
				return repositoryWriteError("record context manifest exclusion", err)
			}
		}
		return nil
	})
	if err != nil {
		return ContextManifest{}, err
	}
	return recorded, nil
}

func (repositories *Repositories) GetContextManifest(
	ctx context.Context,
	id string,
) (ContextManifest, error) {
	if err := validateSHA256("context manifest ID", id); err != nil {
		return ContextManifest{}, err
	}
	var (
		manifest      ContextManifest
		createdMicros int64
	)
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, repository_id, repository_revision, map_revision,
		        requirement_sha256, selection_policy_version,
		        max_files, max_bytes, max_estimated_tokens,
		        used_files, used_bytes, used_estimated_tokens,
		        created_at_unix_micros
		 FROM context_manifests
		 WHERE id = ?`,
		id,
	).Scan(
		&manifest.ID,
		&manifest.RepositoryID,
		&manifest.RepositoryRevision,
		&manifest.MapRevision,
		&manifest.RequirementSHA256,
		&manifest.SelectionPolicy,
		&manifest.MaxFiles,
		&manifest.MaxBytes,
		&manifest.MaxEstimatedTokens,
		&manifest.UsedFiles,
		&manifest.UsedBytes,
		&manifest.UsedEstimatedTokens,
		&createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ContextManifest{}, typedError(ErrNotFound, "get context manifest", err)
	}
	if err != nil {
		return ContextManifest{}, classify("get context manifest", err)
	}
	manifest.CreatedAt = repositoryTime(createdMicros)
	manifest.Items, err = repositories.getContextManifestItems(ctx, id)
	if err != nil {
		return ContextManifest{}, err
	}
	manifest.Exclusions, err = repositories.getContextManifestExclusions(ctx, id)
	if err != nil {
		return ContextManifest{}, err
	}
	return manifest, nil
}

func (repositories *Repositories) getContextManifestItems(
	ctx context.Context,
	id string,
) ([]ContextManifestItem, error) {
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT repository_relative_path, kind, start_line, end_line,
		        content_redacted, content_sha256, reasons_json, trust,
		        generated, binary, minified, vendor, dependency,
		        estimated_tokens
		 FROM context_manifest_items
		 WHERE manifest_id = ?
		 ORDER BY ordinal`,
		id,
	)
	if err != nil {
		return nil, classify("list context manifest items", err)
	}
	defer rows.Close()
	var items []ContextManifestItem
	for rows.Next() {
		var (
			item        ContextManifestItem
			reasonsJSON string
		)
		if err := rows.Scan(
			&item.Path,
			&item.Kind,
			&item.StartLine,
			&item.EndLine,
			&item.ContentRedacted,
			&item.ContentSHA256,
			&reasonsJSON,
			&item.Trust,
			&item.Generated,
			&item.Binary,
			&item.Minified,
			&item.Vendor,
			&item.Dependency,
			&item.EstimatedTokens,
		); err != nil {
			return nil, classify("scan context manifest item", err)
		}
		if err := json.Unmarshal([]byte(reasonsJSON), &item.Reasons); err != nil {
			return nil, classify("decode context manifest item reasons", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate context manifest items", err)
	}
	return items, nil
}

func (repositories *Repositories) getContextManifestExclusions(
	ctx context.Context,
	id string,
) ([]ContextManifestExclusion, error) {
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT repository_relative_path, reason
		 FROM context_manifest_exclusions
		 WHERE manifest_id = ?
		 ORDER BY ordinal`,
		id,
	)
	if err != nil {
		return nil, classify("list context manifest exclusions", err)
	}
	defer rows.Close()
	var exclusions []ContextManifestExclusion
	for rows.Next() {
		var exclusion ContextManifestExclusion
		if err := rows.Scan(&exclusion.Path, &exclusion.Reason); err != nil {
			return nil, classify("scan context manifest exclusion", err)
		}
		exclusions = append(exclusions, exclusion)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate context manifest exclusions", err)
	}
	return exclusions, nil
}

func validateContextManifest(input RecordContextManifest) error {
	switch {
	case input.RepositoryID.IsZero():
		return errors.New("repository ID must not be empty")
	case input.SelectionPolicy < 1:
		return errors.New("context selection policy must be positive")
	case input.MaxFiles < 1 || input.MaxFiles > 200:
		return errors.New("context file budget is outside supported bounds")
	case input.MaxBytes < 1024 || input.MaxBytes > 8<<20:
		return errors.New("context byte budget is outside supported bounds")
	case input.MaxEstimatedTokens < 256 || input.MaxEstimatedTokens > 2_000_000:
		return errors.New("context token budget is outside supported bounds")
	case input.UsedFiles != len(input.Items) || input.UsedFiles > input.MaxFiles:
		return errors.New("context used file count is inconsistent")
	case input.UsedBytes < 0 || input.UsedBytes > input.MaxBytes:
		return errors.New("context used byte count is outside its budget")
	case input.UsedEstimatedTokens < 0 ||
		input.UsedEstimatedTokens > input.MaxEstimatedTokens:
		return errors.New("context used token count is outside its budget")
	}
	if err := validateSHA256("context manifest ID", input.ID); err != nil {
		return err
	}
	if err := validateRevision("repository revision", input.RepositoryRevision); err != nil {
		return err
	}
	if err := validateSHA256("repository map revision", input.MapRevision); err != nil {
		return err
	}
	if err := validateSHA256("requirement hash", input.RequirementSHA256); err != nil {
		return err
	}
	usedBytes := 0
	usedTokens := 0
	for index, item := range input.Items {
		if err := validateContextManifestItem(item); err != nil {
			return fmt.Errorf("context item %d: %w", index, err)
		}
		usedBytes += len(item.ContentRedacted)
		usedTokens += item.EstimatedTokens
	}
	if usedBytes != input.UsedBytes {
		return errors.New("context used byte count does not match items")
	}
	if usedTokens != input.UsedEstimatedTokens {
		return errors.New("context used token count does not match items")
	}
	for index, exclusion := range input.Exclusions {
		if !safeRepositoryRelativePath(exclusion.Path) {
			return fmt.Errorf("context exclusion %d path is unsafe", index)
		}
		if err := validateBounded("context exclusion reason", exclusion.Reason, 512); err != nil {
			return fmt.Errorf("context exclusion %d: %w", index, err)
		}
	}
	return nil
}

func validateContextManifestItem(item ContextManifestItem) error {
	if !safeRepositoryRelativePath(item.Path) {
		return errors.New("path is unsafe")
	}
	switch item.Kind {
	case "source", "test", "module", "instruction", "configuration", "history":
	default:
		return errors.New("kind is invalid")
	}
	switch {
	case item.StartLine < 0 || item.EndLine < item.StartLine:
		return errors.New("line range is invalid")
	case item.Kind != "history" && item.StartLine < 1:
		return errors.New("source excerpt must start at a positive line")
	case item.Trust != contextManifestTrust:
		return errors.New("trust label is invalid")
	case item.EstimatedTokens < 0:
		return errors.New("estimated token count must not be negative")
	case len(item.Reasons) == 0:
		return errors.New("selection reasons must not be empty")
	}
	if err := validateSHA256("content hash", item.ContentSHA256); err != nil {
		return err
	}
	for _, reason := range item.Reasons {
		if err := validateBounded("selection reason", reason, 512); err != nil {
			return err
		}
	}
	return nil
}

func validateSHA256(label, value string) error {
	if len(value) != 64 || strings.ToLower(value) != value {
		return errors.New(label + " must be a lowercase SHA-256")
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return errors.New(label + " must be a lowercase SHA-256")
		}
	}
	return nil
}

func validateRevision(label, value string) error {
	if len(value) != 40 && len(value) != 64 {
		return errors.New(label + " must be a Git object ID")
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return errors.New(label + " must be a lowercase Git object ID")
		}
	}
	return nil
}

func safeRepositoryRelativePath(value string) bool {
	if value == "" || len(value) > 4096 ||
		strings.Contains(value, "\\") ||
		filepath.IsAbs(value) ||
		(len(value) >= 2 && value[1] == ':') {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	return cleaned == value && cleaned != "." && cleaned != ".." &&
		!strings.HasPrefix(cleaned, "../")
}

func cloneContextManifestItems(items []ContextManifestItem) []ContextManifestItem {
	result := make([]ContextManifestItem, len(items))
	copy(result, items)
	for index := range result {
		result[index].Reasons = append([]string(nil), items[index].Reasons...)
	}
	return result
}
