// Package storage: atom-documentation revision storage (M21-103..107). See
// docs/plan.md §7 "Atom Documentation as Retrieval Material" and §23 "Atom
// Storage", and internal/atomdoc.DocumentationRevision, whose exact fields
// this file persists. Schema and metadata only: no embedding generation,
// similarity search, or ranking, which is M21-078..089 and sits behind the
// docs/plan.md §0 branch gate.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
)

// queryContexter is satisfied by both *sql.DB and *sql.Tx for read paths
// that stream multiple rows.
type queryContexter interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// AtomDocumentationValidationStatus is the mutable, one-way governance
// overlay on one otherwise-immutable atom_documentation_revisions row:
// admitted -> invalidated only, matching AGENTS.md "Preserve immutable
// historical records; represent later correction through revisions or
// invalidation overlays" and AGENTS.md "On comment, contract, binding, or
// embedding-schema change, invalidate or regenerate the derived vector
// before it can influence retrieval."
type AtomDocumentationValidationStatus string

const (
	AtomDocumentationValidationStatusAdmitted    AtomDocumentationValidationStatus = "admitted"
	AtomDocumentationValidationStatusInvalidated AtomDocumentationValidationStatus = "invalidated"
)

// AtomDocumentationRevision is one persisted, immutable atom-documentation
// revision plus its mutable validation-status overlay.
type AtomDocumentationRevision struct {
	RevisionID               atomdoc.DocumentationRevisionID
	ProjectID                domain.ProjectID
	AtomID                   domain.AtomID
	AtomVersion              atomdoc.AtomVersionID
	SchemaVersion            uint32
	Authoring                atomdoc.AuthoringMode
	SourceRepositoryRevision string
	SourceCommentHash        atomdoc.SourceCommentHash
	NormalizedInputHash      atomdoc.NormalizedInputHash
	ContractHash             atomdoc.ContractHash
	DependencyBindings       []atomdoc.DependencyBinding
	Document                 atomdoc.Document
	ValidationStatus         AtomDocumentationValidationStatus
	InvalidatedReason        string
	CreatedAt                time.Time
	InvalidatedAt            *time.Time
}

// CreateAtomDocumentationRevision declares one storage input persisting an
// admitted atomdoc.DocumentationRevision (M21-103, M21-104, M21-106).
// ProjectID is not carried by atomdoc.DocumentationRevision itself (that
// package has no concept of project scope); the storage caller supplies it,
// exactly as CreateMemoryArtifact.ProjectID sits beside
// domain.MemoryArtifactContent.
type CreateAtomDocumentationRevision struct {
	ProjectID domain.ProjectID
	Revision  atomdoc.DocumentationRevision
}

// CreateAtomDocumentationRevision persists one admitted atomdoc
// documentation revision idempotently: since
// atomdoc.DocumentationRevisionID is content-addressed (derived from atom,
// atom version, schema version, normalized-input hash, and contract hash),
// a retry with the exact same revision ID and content is accepted as a
// no-op; a retry with the same ID but different content is rejected
// (M21-106's "two different normalized documents ... claiming one
// documentation revision").
func (repositories *Repositories) CreateAtomDocumentationRevision(
	ctx context.Context,
	input CreateAtomDocumentationRevision,
) (AtomDocumentationRevision, error) {
	if input.ProjectID.IsZero() {
		return AtomDocumentationRevision{}, errors.New("atom documentation project ID must not be empty")
	}
	revision := input.Revision
	if revision.RevisionID.IsZero() {
		return AtomDocumentationRevision{}, errors.New("atom documentation revision ID must not be empty")
	}
	if revision.AtomID.IsZero() {
		return AtomDocumentationRevision{}, errors.New("atom documentation atom ID must not be empty")
	}
	if revision.AtomVersion.IsZero() {
		return AtomDocumentationRevision{}, errors.New("atom documentation atom version must not be empty")
	}
	if revision.AtomVersion.AtomID() != revision.AtomID {
		return AtomDocumentationRevision{}, errors.New("atom documentation atom version does not belong to the declared atom")
	}
	if revision.SchemaVersion == 0 {
		return AtomDocumentationRevision{}, errors.New("atom documentation schema version must not be zero")
	}
	if !revision.Authoring.IsValid() || revision.Authoring == atomdoc.AuthoringGeneratedProjection {
		return AtomDocumentationRevision{}, fmt.Errorf("atom documentation authoring %q is not a persistable authoring mode", revision.Authoring)
	}
	if err := validateBounded("atom documentation source repository revision", revision.SourceRepositoryRevision, 255); err != nil {
		return AtomDocumentationRevision{}, err
	}
	if revision.SourceCommentHash.IsZero() {
		return AtomDocumentationRevision{}, errors.New("atom documentation source comment hash must not be empty")
	}
	if revision.NormalizedInputHash.IsZero() {
		return AtomDocumentationRevision{}, errors.New("atom documentation normalized input hash must not be empty")
	}
	if revision.ContractHash.IsZero() {
		return AtomDocumentationRevision{}, errors.New("atom documentation contract hash must not be empty")
	}
	if len(revision.DependencyBindings) > atomdoc.MaximumDependencyBindings {
		return AtomDocumentationRevision{}, fmt.Errorf("atom documentation dependency bindings exceed the maximum bound of %d", atomdoc.MaximumDependencyBindings)
	}
	for _, binding := range revision.DependencyBindings {
		if err := binding.Validate(); err != nil {
			return AtomDocumentationRevision{}, err
		}
	}
	fields := atomDocumentationFieldSpecs(revision.Document)
	for _, field := range fields {
		if field.Value.IsEmpty() {
			return AtomDocumentationRevision{}, fmt.Errorf("atom documentation field %q must not be empty", field.Label)
		}
	}

	dependencyBindingsJSON, err := json.Marshal(revision.DependencyBindings)
	if err != nil {
		return AtomDocumentationRevision{}, fmt.Errorf("marshal atom documentation dependency bindings: %w", err)
	}

	now, micros := repositories.timestamp()
	var record AtomDocumentationRevision
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findAtomDocumentationRevision(ctx, transaction.sql, revision.RevisionID)
		if err != nil {
			return err
		}
		if found {
			if existing.ProjectID != input.ProjectID ||
				existing.NormalizedInputHash != revision.NormalizedInputHash ||
				existing.ContractHash != revision.ContractHash {
				return typedError(ErrConflict, "create atom documentation revision", errors.New("revision identity retried with different content"))
			}
			record = existing
			return nil
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO atom_documentation_revisions (
				id, project_id, atom_id, atom_version_number, schema_version, authoring,
				source_repository_revision, source_comment_hash, normalized_input_hash,
				contract_hash, dependency_bindings_json, validation_status, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'admitted', ?)`,
			revision.RevisionID.String(), input.ProjectID, revision.AtomID, revision.AtomVersion.Number(),
			revision.SchemaVersion, string(revision.Authoring), revision.SourceRepositoryRevision,
			revision.SourceCommentHash.String(), revision.NormalizedInputHash.String(), revision.ContractHash.String(),
			string(dependencyBindingsJSON), micros,
		); err != nil {
			return repositoryWriteError("create atom documentation revision", err)
		}
		for _, field := range fields {
			if err := insertAtomDocumentationField(ctx, transaction, revision.RevisionID, field); err != nil {
				return err
			}
		}
		record = AtomDocumentationRevision{
			RevisionID: revision.RevisionID, ProjectID: input.ProjectID, AtomID: revision.AtomID,
			AtomVersion: revision.AtomVersion, SchemaVersion: revision.SchemaVersion, Authoring: revision.Authoring,
			SourceRepositoryRevision: revision.SourceRepositoryRevision, SourceCommentHash: revision.SourceCommentHash,
			NormalizedInputHash: revision.NormalizedInputHash, ContractHash: revision.ContractHash,
			DependencyBindings: append([]atomdoc.DependencyBinding(nil), revision.DependencyBindings...),
			Document:           revision.Document, ValidationStatus: AtomDocumentationValidationStatusAdmitted, CreatedAt: now,
		}
		return nil
	})
	if err != nil {
		return AtomDocumentationRevision{}, err
	}
	return record, nil
}

// GetAtomDocumentationRevision reads one revision by its exact content-
// addressed identity, including its full normalized field set (M21-104).
func (repositories *Repositories) GetAtomDocumentationRevision(
	ctx context.Context,
	id atomdoc.DocumentationRevisionID,
) (AtomDocumentationRevision, error) {
	if id.IsZero() {
		return AtomDocumentationRevision{}, errors.New("atom documentation revision ID must not be empty")
	}
	record, found, err := findAtomDocumentationRevision(ctx, repositories.database.sql, id)
	if err != nil {
		return AtomDocumentationRevision{}, err
	}
	if !found {
		return AtomDocumentationRevision{}, typedError(ErrNotFound, "get atom documentation revision", errors.New("atom documentation revision does not exist"))
	}
	fields, err := listAtomDocumentationFields(ctx, repositories.database.sql, id)
	if err != nil {
		return AtomDocumentationRevision{}, err
	}
	document, err := documentFromFields(fields)
	if err != nil {
		return AtomDocumentationRevision{}, typedError(ErrCorrupt, "get atom documentation revision", err)
	}
	record.Document = document
	return record, nil
}

// InvalidateAtomDocumentationRevision declares one one-way admitted ->
// invalidated governance transition (M21-134's storage-layer mechanism).
type InvalidateAtomDocumentationRevision struct {
	RevisionID atomdoc.DocumentationRevisionID
	Reason     string
}

// InvalidateAtomDocumentationRevision marks one currently-admitted revision
// invalidated. The one-way trigger on atom_documentation_revisions rejects
// any other transition, including re-admission.
func (repositories *Repositories) InvalidateAtomDocumentationRevision(
	ctx context.Context,
	input InvalidateAtomDocumentationRevision,
) error {
	if input.RevisionID.IsZero() {
		return errors.New("atom documentation revision ID must not be empty")
	}
	if err := validateBounded("atom documentation invalidation reason", input.Reason, 2048); err != nil {
		return err
	}
	_, micros := repositories.timestamp()
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE atom_documentation_revisions
			 SET validation_status = 'invalidated', invalidated_reason = ?, invalidated_at_unix_micros = ?
			 WHERE id = ? AND validation_status = 'admitted'`,
			input.Reason, micros, input.RevisionID.String(),
		)
		if err != nil {
			return repositoryWriteError("invalidate atom documentation revision", err)
		}
		return requireOneAffected(result, "invalidate atom documentation revision")
	})
}

// AtomDocumentationEmbedding is one vector row linked to the exact
// documentation revision that produced it (M21-105).
type AtomDocumentationEmbedding struct {
	ID                  string
	RevisionID          atomdoc.DocumentationRevisionID
	EmbeddingSpaceID    string
	SourceContentSHA256 string
	Vector              []byte
	Valid               bool
	CreatedAt           time.Time
	InvalidatedAt       *time.Time
}

// RecordAtomDocumentationEmbedding declares one vector row bound to an exact
// documentation revision.
type RecordAtomDocumentationEmbedding struct {
	ID                  string
	RevisionID          atomdoc.DocumentationRevisionID
	EmbeddingSpaceID    string
	SourceContentSHA256 string
	Vector              []byte
}

// RecordAtomDocumentationEmbedding persists one revision-bound vector row
// (M21-105). The project-boundary trigger on atom_documentation_embeddings
// rejects a space whose project differs from the revision's owning atom.
func (repositories *Repositories) RecordAtomDocumentationEmbedding(
	ctx context.Context,
	input RecordAtomDocumentationEmbedding,
) (AtomDocumentationEmbedding, error) {
	switch {
	case input.ID == "":
		return AtomDocumentationEmbedding{}, errors.New("atom documentation embedding ID must not be empty")
	case input.RevisionID.IsZero():
		return AtomDocumentationEmbedding{}, errors.New("atom documentation embedding revision ID must not be empty")
	case input.EmbeddingSpaceID == "":
		return AtomDocumentationEmbedding{}, errors.New("atom documentation embedding space ID must not be empty")
	case len(input.SourceContentSHA256) != 64:
		return AtomDocumentationEmbedding{}, errors.New("atom documentation embedding source content hash must contain 64 characters")
	case len(input.Vector) == 0:
		return AtomDocumentationEmbedding{}, errors.New("atom documentation embedding vector must not be empty")
	}
	_, micros := repositories.timestamp()
	embedding := AtomDocumentationEmbedding{
		ID: input.ID, RevisionID: input.RevisionID, EmbeddingSpaceID: input.EmbeddingSpaceID,
		SourceContentSHA256: input.SourceContentSHA256, Vector: input.Vector, Valid: true,
		CreatedAt: repositoryTime(micros),
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO atom_documentation_embeddings (
				id, revision_id, embedding_space_id, source_content_sha256, vector, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?)`,
			input.ID, input.RevisionID.String(), input.EmbeddingSpaceID, input.SourceContentSHA256, input.Vector, micros,
		)
		return repositoryWriteError("record atom documentation embedding", err)
	})
	if err != nil {
		return AtomDocumentationEmbedding{}, err
	}
	return embedding, nil
}

// InvalidateAtomDocumentationEmbedding marks one vector row invalid, e.g.
// after its documentation revision is invalidated. Embeddings are never
// hard-deleted.
func (repositories *Repositories) InvalidateAtomDocumentationEmbedding(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("atom documentation embedding ID must not be empty")
	}
	_, micros := repositories.timestamp()
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE atom_documentation_embeddings SET valid = 0, invalidated_at_unix_micros = ? WHERE id = ? AND valid = 1`,
			micros, id,
		)
		if err != nil {
			return repositoryWriteError("invalidate atom documentation embedding", err)
		}
		return requireOneAffected(result, "invalidate atom documentation embedding")
	})
}

// ListAtomDocumentationRevisionsPendingReembedding returns every admitted
// revision with no currently valid embedding in embeddingSpaceID (M21-107's
// "pending re-embedding" index), in creation order. It performs no
// embedding generation itself.
func (repositories *Repositories) ListAtomDocumentationRevisionsPendingReembedding(
	ctx context.Context,
	embeddingSpaceID string,
) ([]atomdoc.DocumentationRevisionID, error) {
	if embeddingSpaceID == "" {
		return nil, errors.New("atom documentation embedding space ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT r.id FROM atom_documentation_revisions AS r
		 WHERE r.validation_status = 'admitted'
		   AND NOT EXISTS (
		       SELECT 1 FROM atom_documentation_embeddings AS e
		       WHERE e.revision_id = r.id AND e.embedding_space_id = ? AND e.valid = 1
		   )
		 ORDER BY r.created_at_unix_micros`,
		embeddingSpaceID,
	)
	if err != nil {
		return nil, classify("list atom documentation revisions pending reembedding", err)
	}
	defer rows.Close()
	var pending []atomdoc.DocumentationRevisionID
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, classify("scan atom documentation revision pending reembedding", err)
		}
		id, err := atomdoc.ParseDocumentationRevisionID(raw)
		if err != nil {
			return nil, typedError(ErrCorrupt, "scan atom documentation revision pending reembedding", err)
		}
		pending = append(pending, id)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list atom documentation revisions pending reembedding", err)
	}
	return pending, nil
}

// ListAtomDocumentationRevisionsByProject returns every currently admitted
// atom-documentation revision in one project, newest first, bounded by limit
// (PIPE-048/PIPE-054: "the registry as it now stands"). Invalidated revisions
// are excluded -- an invalidated revision is not something a later run
// should recall as a live match.
//
// It does not load each revision's full field set (documentFromFields): a
// caller indexing the registry by ContractHash to look for a match, or to
// classify a reuse-regret finding, has no use for the nineteen-field prose
// until it has already decided a particular revision matters, and loading it
// for every row up front would turn a bounded index scan into N+1 queries
// for the common case where most rows are never inspected further.
func (repositories *Repositories) ListAtomDocumentationRevisionsByProject(
	ctx context.Context,
	projectID domain.ProjectID,
	limit int,
) ([]AtomDocumentationRevision, error) {
	if projectID.IsZero() {
		return nil, errors.New("atom documentation project ID must not be empty")
	}
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		atomDocumentationRevisionSelect+`
		 WHERE project_id = ? AND validation_status = 'admitted'
		 ORDER BY created_at_unix_micros DESC, id
		 LIMIT ?`,
		projectID, limit,
	)
	if err != nil {
		return nil, classify("list atom documentation revisions by project", err)
	}
	defer func() { _ = rows.Close() }()
	var revisions []AtomDocumentationRevision
	for rows.Next() {
		record, err := scanAtomDocumentationRevision(rows, "list atom documentation revisions by project")
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, record)
	}
	return revisions, rows.Err()
}

// atomDocumentationFieldSpec pairs one schema-v1 field label and its
// structural kind with its value from a parsed atomdoc.Document. This
// mirrors internal/atomdoc's unexported fieldSpecs table (schema.go): that
// package does not export a generic field-iteration accessor, so this list
// is hand-written here and defended by
// TestAtomDocumentationFieldSpecsMatchCanonicalLabels, which fails loudly if
// atomdoc's schema-v1 field set or order ever drifts from this copy.
type atomDocumentationFieldSpec struct {
	Label string
	Kind  string // "prose" or "list"
	Value atomdoc.Field
}

func atomDocumentationFieldSpecs(doc atomdoc.Document) []atomDocumentationFieldSpec {
	return []atomDocumentationFieldSpec{
		{"Purpose", "prose", doc.Purpose},
		{"Use when", "prose", doc.UseWhen},
		{"Do not use when", "prose", doc.DoNotUseWhen},
		{"Semantics", "prose", doc.Semantics},
		{"Inputs", "list", doc.Inputs},
		{"Outputs", "list", doc.Outputs},
		{"Preconditions", "list", doc.Preconditions},
		{"Postconditions", "list", doc.Postconditions},
		{"Effects", "list", doc.Effects},
		{"Failure semantics", "list", doc.FailureSemantics},
		{"Determinism", "prose", doc.Determinism},
		{"Idempotency and retry", "prose", doc.IdempotencyAndRetry},
		{"Reconciliation and compensation", "prose", doc.ReconciliationAndCompensation},
		{"Security and privacy", "prose", doc.SecurityAndPrivacy},
		{"Dependencies and bindings", "prose", doc.DependenciesAndBindings},
		{"Complexity and limits", "prose", doc.ComplexityAndLimits},
		{"Examples", "list", doc.Examples},
		{"Verification", "prose", doc.Verification},
		{"Retrieval concepts", "prose", doc.RetrievalConcepts},
	}
}

func insertAtomDocumentationField(
	ctx context.Context,
	transaction *Transaction,
	revisionID atomdoc.DocumentationRevisionID,
	field atomDocumentationFieldSpec,
) error {
	var isNone int
	var noneReason, fieldText, fieldItemsJSON any
	switch {
	case field.Value.None:
		isNone = 1
		noneReason = field.Value.Reason
	case field.Kind == "list":
		encoded, err := json.Marshal(field.Value.Items)
		if err != nil {
			return fmt.Errorf("marshal atom documentation field %q items: %w", field.Label, err)
		}
		fieldItemsJSON = string(encoded)
	default:
		fieldText = field.Value.Text
	}
	_, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO atom_documentation_fields (
			revision_id, field_label, field_kind, is_none, none_reason, field_text, field_items_json
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		revisionID.String(), field.Label, field.Kind, isNone, noneReason, fieldText, fieldItemsJSON,
	)
	return repositoryWriteError("insert atom documentation field", err)
}

type atomDocumentationFieldRow struct {
	Label string
	Kind  string
	Field atomdoc.Field
}

func listAtomDocumentationFields(
	ctx context.Context,
	queries queryContexter,
	revisionID atomdoc.DocumentationRevisionID,
) ([]atomDocumentationFieldRow, error) {
	rows, err := queries.QueryContext(
		ctx,
		`SELECT field_label, field_kind, is_none, none_reason, field_text, field_items_json
		 FROM atom_documentation_fields WHERE revision_id = ?`,
		revisionID.String(),
	)
	if err != nil {
		return nil, classify("list atom documentation fields", err)
	}
	defer rows.Close()
	var results []atomDocumentationFieldRow
	for rows.Next() {
		var (
			label, kind                           string
			isNone                                int
			noneReason, fieldText, fieldItemsJSON sql.NullString
		)
		if err := rows.Scan(&label, &kind, &isNone, &noneReason, &fieldText, &fieldItemsJSON); err != nil {
			return nil, classify("scan atom documentation field", err)
		}
		field := atomdoc.Field{}
		switch {
		case isNone == 1:
			field.None = true
			field.Reason = noneReason.String
		case kind == "list":
			var items []string
			if fieldItemsJSON.Valid {
				if err := json.Unmarshal([]byte(fieldItemsJSON.String), &items); err != nil {
					return nil, typedError(ErrCorrupt, "scan atom documentation field", err)
				}
			}
			field.Items = items
		default:
			field.Text = fieldText.String
		}
		results = append(results, atomDocumentationFieldRow{Label: label, Kind: kind, Field: field})
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list atom documentation fields", err)
	}
	return results, nil
}

// documentFromFields reconstructs an atomdoc.Document from the persisted
// per-field rows, requiring every one of the nineteen schema-v1 fields to be
// present exactly once so a truncated or corrupted field set is rejected
// rather than silently returned as a partial document.
func documentFromFields(fields []atomDocumentationFieldRow) (atomdoc.Document, error) {
	var doc atomdoc.Document
	seen := make(map[string]bool, len(fields))
	for _, row := range fields {
		if seen[row.Label] {
			return atomdoc.Document{}, fmt.Errorf("atom documentation field %q is duplicated", row.Label)
		}
		seen[row.Label] = true
		switch row.Label {
		case "Purpose":
			doc.Purpose = row.Field
		case "Use when":
			doc.UseWhen = row.Field
		case "Do not use when":
			doc.DoNotUseWhen = row.Field
		case "Semantics":
			doc.Semantics = row.Field
		case "Inputs":
			doc.Inputs = row.Field
		case "Outputs":
			doc.Outputs = row.Field
		case "Preconditions":
			doc.Preconditions = row.Field
		case "Postconditions":
			doc.Postconditions = row.Field
		case "Effects":
			doc.Effects = row.Field
		case "Failure semantics":
			doc.FailureSemantics = row.Field
		case "Determinism":
			doc.Determinism = row.Field
		case "Idempotency and retry":
			doc.IdempotencyAndRetry = row.Field
		case "Reconciliation and compensation":
			doc.ReconciliationAndCompensation = row.Field
		case "Security and privacy":
			doc.SecurityAndPrivacy = row.Field
		case "Dependencies and bindings":
			doc.DependenciesAndBindings = row.Field
		case "Complexity and limits":
			doc.ComplexityAndLimits = row.Field
		case "Examples":
			doc.Examples = row.Field
		case "Verification":
			doc.Verification = row.Field
		case "Retrieval concepts":
			doc.RetrievalConcepts = row.Field
		default:
			return atomdoc.Document{}, fmt.Errorf("atom documentation field %q is not a recognized schema-v1 field", row.Label)
		}
	}
	for _, label := range atomdoc.CanonicalFieldLabels() {
		if !seen[label] {
			return atomdoc.Document{}, fmt.Errorf("atom documentation revision is missing field %q", label)
		}
	}
	return doc, nil
}

const atomDocumentationRevisionSelect = `SELECT
	id, project_id, atom_id, atom_version_number, schema_version, authoring,
	source_repository_revision, source_comment_hash, normalized_input_hash, contract_hash,
	dependency_bindings_json, validation_status, invalidated_reason,
	created_at_unix_micros, invalidated_at_unix_micros
 FROM atom_documentation_revisions`

func findAtomDocumentationRevision(
	ctx context.Context,
	queries queryRower,
	id atomdoc.DocumentationRevisionID,
) (AtomDocumentationRevision, bool, error) {
	record, err := scanAtomDocumentationRevision(
		queries.QueryRowContext(ctx, atomDocumentationRevisionSelect+` WHERE id = ?`, id.String()),
		"find atom documentation revision",
	)
	if errors.Is(err, ErrNotFound) {
		return AtomDocumentationRevision{}, false, nil
	}
	return record, err == nil, err
}

func scanAtomDocumentationRevision(row rowScanner, operation string) (AtomDocumentationRevision, error) {
	var (
		record                                       AtomDocumentationRevision
		idRaw, projectIDRaw, atomIDRaw               string
		atomVersionNumber                            uint32
		authoringRaw                                 string
		sourceCommentHashRaw, normalizedInputHashRaw string
		contractHashRaw                              string
		dependencyBindingsJSON                       string
		validationStatusRaw                          string
		invalidatedReason                            sql.NullString
		createdMicros                                int64
		invalidatedMicros                            sql.NullInt64
	)
	err := row.Scan(
		&idRaw, &projectIDRaw, &atomIDRaw, &atomVersionNumber, &record.SchemaVersion, &authoringRaw,
		&record.SourceRepositoryRevision, &sourceCommentHashRaw, &normalizedInputHashRaw, &contractHashRaw,
		&dependencyBindingsJSON, &validationStatusRaw, &invalidatedReason, &createdMicros, &invalidatedMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AtomDocumentationRevision{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return AtomDocumentationRevision{}, classify(operation, err)
	}

	revisionID, err := atomdoc.ParseDocumentationRevisionID(idRaw)
	if err != nil {
		return AtomDocumentationRevision{}, typedError(ErrCorrupt, operation, err)
	}
	projectID, err := domain.ParseProjectID(projectIDRaw)
	if err != nil {
		return AtomDocumentationRevision{}, typedError(ErrCorrupt, operation, err)
	}
	atomID, err := domain.ParseAtomID(atomIDRaw)
	if err != nil {
		return AtomDocumentationRevision{}, typedError(ErrCorrupt, operation, err)
	}
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, atomVersionNumber)
	if err != nil {
		return AtomDocumentationRevision{}, typedError(ErrCorrupt, operation, err)
	}
	sourceCommentHash, err := atomdoc.ParseSourceCommentHash(sourceCommentHashRaw)
	if err != nil {
		return AtomDocumentationRevision{}, typedError(ErrCorrupt, operation, err)
	}
	normalizedInputHash, err := atomdoc.ParseNormalizedInputHash(normalizedInputHashRaw)
	if err != nil {
		return AtomDocumentationRevision{}, typedError(ErrCorrupt, operation, err)
	}
	contractHash, err := atomdoc.ParseContractHash(contractHashRaw)
	if err != nil {
		return AtomDocumentationRevision{}, typedError(ErrCorrupt, operation, err)
	}
	var dependencyBindings []atomdoc.DependencyBinding
	if err := json.Unmarshal([]byte(dependencyBindingsJSON), &dependencyBindings); err != nil {
		return AtomDocumentationRevision{}, typedError(ErrCorrupt, operation, err)
	}

	record.RevisionID = revisionID
	record.ProjectID = projectID
	record.AtomID = atomID
	record.AtomVersion = atomVersion
	record.Authoring = atomdoc.AuthoringMode(authoringRaw)
	record.SourceCommentHash = sourceCommentHash
	record.NormalizedInputHash = normalizedInputHash
	record.ContractHash = contractHash
	record.DependencyBindings = dependencyBindings
	record.ValidationStatus = AtomDocumentationValidationStatus(validationStatusRaw)
	if invalidatedReason.Valid {
		record.InvalidatedReason = invalidatedReason.String
	}
	record.CreatedAt = repositoryTime(createdMicros)
	if invalidatedMicros.Valid {
		invalidatedAt := repositoryTime(invalidatedMicros.Int64)
		record.InvalidatedAt = &invalidatedAt
	}
	return record, nil
}
