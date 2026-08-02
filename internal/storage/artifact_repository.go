package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// ArtifactStorageClass is how long one artifact is worth keeping.
//
// The classes are a closed set because retention is a policy decision rather
// than a per-caller preference: evidence for a review outlives scratch output
// from a failed attempt, and something has to be able to sweep the difference.
type ArtifactStorageClass string

const (
	// ArtifactPermanentSemantic is content that carries meaning about the
	// project — the source a run produced, kept because it is the answer to
	// "what did it actually write".
	ArtifactPermanentSemantic ArtifactStorageClass = "permanent-semantic"
	// ArtifactPermanentEvidence supports a review decision.
	ArtifactPermanentEvidence ArtifactStorageClass = "permanent-evidence"
	// ArtifactBoundedFailure is failure output, kept briefly to diagnose.
	ArtifactBoundedFailure ArtifactStorageClass = "bounded-failure"
)

func (value ArtifactStorageClass) valid() bool {
	switch value {
	case ArtifactPermanentSemantic, ArtifactPermanentEvidence,
		ArtifactBoundedFailure:
		return true
	default:
		return false
	}
}

// RecordArtifact stores one piece of content a run produced.
type RecordArtifact struct {
	ProjectID    domain.ProjectID
	RepositoryID *domain.RepositoryID
	TaskID       *domain.TaskID
	// Type says what this content is, in the product's own words rather than a
	// file extension: "generated-source" is a different thing from "diff" even
	// when both are text.
	Type string
	// Path is where the content lives in the repository, kept so an artifact
	// can be matched back to the file it is a version of.
	Path string
	// Content is already redacted. The column is named sanitized_content
	// because storing raw model or repository output would put whatever
	// happened to be in the worktree into the database unfiltered.
	Content      []byte
	MediaType    string
	StorageClass ArtifactStorageClass
}

// Artifact is one stored piece of produced content.
type Artifact struct {
	ID          domain.ArtifactID
	ProjectID   domain.ProjectID
	Type        string
	ContentHash string
	Version     uint64
	MediaType   string
	Content     []byte
}

// RecordProducedArtifact stores what a run wrote, so the work survives the
// worktree.
//
// A run's output lived only as files in a temporary worktree: correct while
// the run was happening and gone afterwards, which left the database able to
// say a file was written and unable to say what was in it. Content is stored
// once per project and hash — the same file written twice is one artifact, not
// two — and the row is immutable, so a later version never overwrites the
// evidence of an earlier one.
func (repositories *Repositories) RecordProducedArtifact(
	ctx context.Context,
	input RecordArtifact,
) (Artifact, error) {
	if repositories == nil || repositories.database == nil {
		return Artifact{}, errors.New("repositories are unavailable")
	}
	if err := validateRecordArtifact(input); err != nil {
		return Artifact{}, err
	}
	digest := sha256.Sum256(input.Content)
	contentHash := hex.EncodeToString(digest[:])

	var stored Artifact
	err := repositories.database.RunInTransaction(
		ctx,
		func(transaction *Transaction) error {
			// Content addressing makes this idempotent for free: recording the
			// same bytes for the same project twice returns the first artifact
			// rather than accumulating a row per attempt.
			existing, found, err := findArtifactByContent(
				ctx, transaction, input.ProjectID, contentHash)
			if err != nil {
				return err
			}
			if found {
				stored = existing
				return nil
			}
			id, err := domain.NewArtifactID()
			if err != nil {
				return err
			}
			_, micros := repositories.timestamp()
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO artifacts (
					id, project_id, repository_id, task_id, artifact_type,
					immutable_version, content_hash, media_type, storage_class,
					sanitized_content, created_at_unix_micros
				) VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)`,
				id, input.ProjectID, nullableRepositoryID(input.RepositoryID),
				nullableTaskID(input.TaskID), input.Type,
				contentHash, input.MediaType, string(input.StorageClass),
				input.Content, micros,
			); err != nil {
				return repositoryWriteError("record artifact", err)
			}
			stored = Artifact{
				ID: id, ProjectID: input.ProjectID, Type: input.Type,
				ContentHash: contentHash, Version: 1,
				MediaType: input.MediaType, Content: input.Content,
			}
			return nil
		},
	)
	if err != nil {
		return Artifact{}, err
	}
	return stored, nil
}

// GetArtifactContent reads back what a run produced.
func (repositories *Repositories) GetArtifactContent(
	ctx context.Context,
	id domain.ArtifactID,
) (Artifact, error) {
	var (
		artifact Artifact
		content  []byte
	)
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, project_id, artifact_type, content_hash, immutable_version,
			media_type, sanitized_content
		 FROM artifacts WHERE id = ? AND deleted_at_unix_micros IS NULL`,
		id,
	).Scan(&artifact.ID, &artifact.ProjectID, &artifact.Type,
		&artifact.ContentHash, &artifact.Version, &artifact.MediaType, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, typedError(ErrNotFound, "read artifact", err)
	}
	if err != nil {
		return Artifact{}, classify("read artifact", err)
	}
	artifact.Content = content
	return artifact, nil
}

// ListTaskArtifacts returns everything one task produced, newest first.
func (repositories *Repositories) ListTaskArtifacts(
	ctx context.Context,
	taskID domain.TaskID,
) ([]Artifact, error) {
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, project_id, artifact_type, content_hash, immutable_version,
			media_type, sanitized_content
		 FROM artifacts
		 WHERE task_id = ? AND deleted_at_unix_micros IS NULL
		 ORDER BY created_at_unix_micros DESC, id`,
		taskID,
	)
	if err != nil {
		return nil, classify("list task artifacts", err)
	}
	defer func() { _ = rows.Close() }()
	var artifacts []Artifact
	for rows.Next() {
		var (
			artifact Artifact
			content  []byte
		)
		if err := rows.Scan(&artifact.ID, &artifact.ProjectID, &artifact.Type,
			&artifact.ContentHash, &artifact.Version, &artifact.MediaType,
			&content); err != nil {
			return nil, classify("scan task artifact", err)
		}
		artifact.Content = content
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

// findArtifactByContent looks for content this project already stored.
func findArtifactByContent(
	ctx context.Context,
	transaction *Transaction,
	projectID domain.ProjectID,
	contentHash string,
) (Artifact, bool, error) {
	var (
		artifact Artifact
		content  []byte
	)
	err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT id, project_id, artifact_type, content_hash, immutable_version,
			media_type, sanitized_content
		 FROM artifacts
		 WHERE project_id = ? AND content_hash = ?
		 ORDER BY immutable_version DESC LIMIT 1`,
		projectID, contentHash,
	).Scan(&artifact.ID, &artifact.ProjectID, &artifact.Type,
		&artifact.ContentHash, &artifact.Version, &artifact.MediaType, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, classify("find artifact by content", err)
	}
	artifact.Content = content
	return artifact, true, nil
}

// validateRecordArtifact refuses content nobody could interpret later.
func validateRecordArtifact(input RecordArtifact) error {
	switch {
	case input.ProjectID.IsZero():
		return errors.New("an artifact must belong to a project")
	case len(input.Content) == 0:
		return errors.New("an empty artifact records nothing")
	case !input.StorageClass.valid():
		return fmt.Errorf("artifact storage class %q is not one this store keeps",
			input.StorageClass)
	}
	if err := validateBounded("artifact type", input.Type, 255); err != nil {
		return err
	}
	return validateBounded("artifact media type", input.MediaType, 255)
}

// ArtifactMediaType names what one produced file holds.
//
// It is derived from the path rather than sniffed, because the path is what
// the run declared it was writing and a wrong guess here would mislabel
// evidence rather than merely displaying it oddly.
func ArtifactMediaType(path string) string {
	switch {
	case strings.HasSuffix(path, ".go"):
		return "text/x-go"
	case strings.HasSuffix(path, ".md"):
		return "text/markdown"
	case strings.HasSuffix(path, ".json"):
		return "application/json"
	case strings.HasSuffix(path, ".sql"):
		return "application/sql"
	case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"):
		return "application/yaml"
	default:
		return "text/plain"
	}
}

// ListProjectSourceArtifacts returns the source a project has produced before.
//
// It is the registry a later run searches before rebuilding something the
// project already owns. The bound is a caller's, because a search over every
// artifact a long-lived project ever produced would cost more than the rebuild
// it is trying to avoid.
func (repositories *Repositories) ListProjectSourceArtifactsExcludingTask(
	ctx context.Context,
	projectID domain.ProjectID,
	exclude domain.TaskID,
	limit int,
) ([]Artifact, error) {
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, project_id, artifact_type, content_hash, immutable_version,
			media_type, sanitized_content
		 FROM artifacts
		 WHERE project_id = ? AND media_type = 'text/x-go'
		   AND deleted_at_unix_micros IS NULL
		   AND (task_id IS NULL OR task_id != ?)
		 ORDER BY created_at_unix_micros DESC, id
		 LIMIT ?`,
		projectID, exclude, limit,
	)
	if err != nil {
		return nil, classify("list project source artifacts", err)
	}
	defer func() { _ = rows.Close() }()
	var artifacts []Artifact
	for rows.Next() {
		var (
			artifact Artifact
			content  []byte
		)
		if err := rows.Scan(&artifact.ID, &artifact.ProjectID, &artifact.Type,
			&artifact.ContentHash, &artifact.Version, &artifact.MediaType,
			&content); err != nil {
			return nil, classify("scan project source artifact", err)
		}
		artifact.Content = content
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}
