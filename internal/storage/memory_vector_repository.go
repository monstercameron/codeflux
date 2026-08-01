// Package storage: vector-model/version metadata (M21-024) and vector
// rows linked to one artifact revision (M21-025). Schema and metadata
// storage only: no embedding generation, similarity search, ranking, or
// vector runtime, which is M21-078..089 and sits behind the docs/plan.md
// §0 branch gate. AGENTS.md: "Vector similarity may discover candidates;
// it never establishes compatibility, validity, assurance, or permission."
package storage

import (
	"context"
	"database/sql"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

// MemoryEmbeddingModel identifies one embedding provider/model/version.
type MemoryEmbeddingModel struct {
	ID              string
	Provider        string
	ModelName       string
	ModelVersion    string
	Dimensions      int
	NumericEncoding string
	Normalization   string
	CreatedAtMicros int64
}

// CreateMemoryEmbeddingModel declares one embedding model registry entry.
type CreateMemoryEmbeddingModel struct {
	ID              string
	Provider        string
	ModelName       string
	ModelVersion    string
	Dimensions      int
	NumericEncoding string
	Normalization   string
}

// MemoryEmbeddingSpace binds one embedding model, input-schema version,
// and project/security scope.
type MemoryEmbeddingSpace struct {
	ID                 string
	EmbeddingModelID   string
	InputSchemaVersion int
	ProjectID          domain.ProjectID
	SecurityScope      string
	CreatedAtMicros    int64
}

// CreateMemoryEmbeddingSpace declares one embedding space.
type CreateMemoryEmbeddingSpace struct {
	ID                 string
	EmbeddingModelID   string
	InputSchemaVersion int
	ProjectID          domain.ProjectID
	SecurityScope      string
}

// MemoryArtifactEmbedding is one vector row bound to an exact revision.
type MemoryArtifactEmbedding struct {
	ID                  string
	RevisionID          domain.MemoryArtifactRevisionID
	EmbeddingSpaceID    string
	SourceContentSHA256 string
	Vector              []byte
	Valid               bool
	CreatedAtMicros     int64
	InvalidatedAtMicros *int64
}

// CreateMemoryArtifactEmbedding declares one vector row.
type CreateMemoryArtifactEmbedding struct {
	ID                  string
	RevisionID          domain.MemoryArtifactRevisionID
	EmbeddingSpaceID    string
	SourceContentSHA256 string
	Vector              []byte
}

// CreateMemoryEmbeddingModel persists one embedding model registry entry.
func (repositories *Repositories) CreateMemoryEmbeddingModel(
	ctx context.Context,
	input CreateMemoryEmbeddingModel,
) (MemoryEmbeddingModel, error) {
	if err := validateBounded("memory embedding model ID", input.ID, 255); err != nil {
		return MemoryEmbeddingModel{}, err
	}
	if input.Dimensions <= 0 {
		return MemoryEmbeddingModel{}, errors.New("memory embedding model dimensions must be positive")
	}
	_, micros := repositories.timestamp()
	model := MemoryEmbeddingModel{
		ID: input.ID, Provider: input.Provider, ModelName: input.ModelName, ModelVersion: input.ModelVersion,
		Dimensions: input.Dimensions, NumericEncoding: input.NumericEncoding, Normalization: input.Normalization,
		CreatedAtMicros: micros,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_embedding_models (
				id, provider, model_name, model_version, dimensions,
				numeric_encoding, normalization, created_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.Provider, input.ModelName, input.ModelVersion, input.Dimensions,
			input.NumericEncoding, input.Normalization, micros,
		)
		return repositoryWriteError("create memory embedding model", err)
	})
	if err != nil {
		return MemoryEmbeddingModel{}, err
	}
	return model, nil
}

// CreateMemoryEmbeddingSpace persists one embedding space.
func (repositories *Repositories) CreateMemoryEmbeddingSpace(
	ctx context.Context,
	input CreateMemoryEmbeddingSpace,
) (MemoryEmbeddingSpace, error) {
	if err := validateBounded("memory embedding space ID", input.ID, 255); err != nil {
		return MemoryEmbeddingSpace{}, err
	}
	if input.ProjectID.IsZero() {
		return MemoryEmbeddingSpace{}, errors.New("memory embedding space project ID must not be empty")
	}
	_, micros := repositories.timestamp()
	space := MemoryEmbeddingSpace{
		ID: input.ID, EmbeddingModelID: input.EmbeddingModelID, InputSchemaVersion: input.InputSchemaVersion,
		ProjectID: input.ProjectID, SecurityScope: input.SecurityScope, CreatedAtMicros: micros,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_embedding_spaces (
				id, embedding_model_id, input_schema_version, project_id,
				security_scope, created_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?)`,
			input.ID, input.EmbeddingModelID, input.InputSchemaVersion, input.ProjectID, input.SecurityScope, micros,
		)
		return repositoryWriteError("create memory embedding space", err)
	})
	if err != nil {
		return MemoryEmbeddingSpace{}, err
	}
	return space, nil
}

// CreateMemoryArtifactEmbedding persists one revision-bound vector row.
// The project-boundary trigger on memory_artifact_embeddings rejects a
// space whose project differs from the revision's owning artifact.
func (repositories *Repositories) CreateMemoryArtifactEmbedding(
	ctx context.Context,
	input CreateMemoryArtifactEmbedding,
) (MemoryArtifactEmbedding, error) {
	switch {
	case input.ID == "":
		return MemoryArtifactEmbedding{}, errors.New("memory artifact embedding ID must not be empty")
	case input.RevisionID.IsZero():
		return MemoryArtifactEmbedding{}, errors.New("memory artifact revision ID must not be empty")
	case len(input.SourceContentSHA256) != 64:
		return MemoryArtifactEmbedding{}, errors.New("memory artifact embedding source content hash must contain 64 characters")
	case len(input.Vector) == 0:
		return MemoryArtifactEmbedding{}, errors.New("memory artifact embedding vector must not be empty")
	}
	_, micros := repositories.timestamp()
	embedding := MemoryArtifactEmbedding{
		ID: input.ID, RevisionID: input.RevisionID, EmbeddingSpaceID: input.EmbeddingSpaceID,
		SourceContentSHA256: input.SourceContentSHA256, Vector: input.Vector, Valid: true, CreatedAtMicros: micros,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_artifact_embeddings (
				id, revision_id, embedding_space_id, source_content_sha256,
				vector, valid, created_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, 1, ?)`,
			input.ID, input.RevisionID, input.EmbeddingSpaceID, input.SourceContentSHA256, input.Vector, micros,
		)
		return repositoryWriteError("create memory artifact embedding", err)
	})
	if err != nil {
		return MemoryArtifactEmbedding{}, err
	}
	return embedding, nil
}

// InvalidateMemoryArtifactEmbedding marks one vector row invalid, e.g.
// after its source content changes. Embeddings are never hard-deleted.
func (repositories *Repositories) InvalidateMemoryArtifactEmbedding(
	ctx context.Context,
	id string,
) error {
	if id == "" {
		return errors.New("memory artifact embedding ID must not be empty")
	}
	_, micros := repositories.timestamp()
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE memory_artifact_embeddings SET valid = 0, invalidated_at_unix_micros = ? WHERE id = ? AND valid = 1`,
			micros, id,
		)
		if err != nil {
			return repositoryWriteError("invalidate memory artifact embedding", err)
		}
		return requireOneAffected(result, "invalidate memory artifact embedding")
	})
}

// ListMemoryArtifactEmbeddings reads every vector row stored for one
// revision.
func (repositories *Repositories) ListMemoryArtifactEmbeddings(
	ctx context.Context,
	revisionID domain.MemoryArtifactRevisionID,
) ([]MemoryArtifactEmbedding, error) {
	if revisionID.IsZero() {
		return nil, errors.New("memory artifact revision ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, revision_id, embedding_space_id, source_content_sha256, vector, valid,
		        created_at_unix_micros, invalidated_at_unix_micros
		 FROM memory_artifact_embeddings WHERE revision_id = ? ORDER BY created_at_unix_micros`,
		revisionID,
	)
	if err != nil {
		return nil, classify("list memory artifact embeddings", err)
	}
	defer rows.Close()
	var embeddings []MemoryArtifactEmbedding
	for rows.Next() {
		var (
			embedding   MemoryArtifactEmbedding
			invalidated sql.NullInt64
		)
		if err := rows.Scan(
			&embedding.ID, &embedding.RevisionID, &embedding.EmbeddingSpaceID, &embedding.SourceContentSHA256,
			&embedding.Vector, &embedding.Valid, &embedding.CreatedAtMicros, &invalidated,
		); err != nil {
			return nil, classify("scan memory artifact embedding", err)
		}
		if invalidated.Valid {
			embedding.InvalidatedAtMicros = &invalidated.Int64
		}
		embeddings = append(embeddings, embedding)
	}
	return embeddings, classify("list memory artifact embeddings", rows.Err())
}
