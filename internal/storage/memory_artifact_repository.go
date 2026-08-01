// Package storage: memory artifact identity and immutable revisions
// (M21-014, M21-015), the type/maturity fields and repository-revision
// binding carried on each revision (M21-016, M21-017), and cascading
// logical deletion (M21-028). See docs/plan.md §31 "Learning Artifact
// Types" and internal/domain/memory.go for the governed authority model
// this table set persists.
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// MemoryArtifact is one stable project-memory artifact identity.
type MemoryArtifact struct {
	ID             domain.MemoryArtifactID
	Kind           domain.MemoryArtifactKind
	ProjectID      domain.ProjectID
	CreatedAt      time.Time
	DeletedAt      *time.Time
	DeletionReason *string
}

// MemoryRevisionBinding mirrors domain.RevisionBinding for one persisted
// revision. Nil on a MemoryArtifactRevisionRecord when the artifact kind
// carries no binding (execution-recipe, observation-hypothesis).
type MemoryRevisionBinding struct {
	Known              bool
	UnknownReason      string
	ExactRevision      string
	ValidFromRevision  string
	ValidUntilRevision string
}

// MemoryArtifactRevisionRecord is one immutable memory-artifact revision
// plus its mutable current maturity overlay.
type MemoryArtifactRevisionRecord struct {
	ArtifactID            domain.MemoryArtifactID
	RevisionID            domain.MemoryArtifactRevisionID
	RevisionNumber        uint64
	Kind                  domain.MemoryArtifactKind
	ScopeRepositoryID     *domain.RepositoryID
	ContentRepositoryID   domain.RepositoryID
	Content               domain.MemoryArtifactContent
	ContentSHA256         string
	Binding               *MemoryRevisionBinding
	Maturity              domain.MaturityState
	SupersedesRevisionID  *domain.MemoryArtifactRevisionID
	CreatedFromCorrection bool
	IdempotencyKey        string
	CreatedAt             time.Time
}

// CreateMemoryArtifact declares one new memory artifact and its first
// (candidate) revision together, idempotently.
type CreateMemoryArtifact struct {
	ArtifactID        domain.MemoryArtifactID
	RevisionID        domain.MemoryArtifactRevisionID
	ProjectID         domain.ProjectID
	ScopeRepositoryID *domain.RepositoryID
	Content           domain.MemoryArtifactContent
	IdempotencyKey    string
}

// CreateMemoryArtifactCorrection declares one new revision that supersedes
// prior without mutating it (M21-013/M21-015).
type CreateMemoryArtifactCorrection struct {
	PriorRevisionID  domain.MemoryArtifactRevisionID
	NewRevisionID    domain.MemoryArtifactRevisionID
	CorrectedContent domain.MemoryArtifactContent
	IdempotencyKey   string
}

// DeleteMemoryArtifact declares one user-authorized logical deletion,
// carrying the same acknowledgement gate as
// domain.MemoryArtifactDeletionRequest. AcknowledgedDependents must name
// exactly the identities the caller was shown and acknowledged (not a bare
// bool): domain.ValidateMemoryArtifactDeletion rejects any set that does
// not equal the freshly recomputed transitive DirectDependents, so a
// caller can never satisfy the acknowledgement gate with a stale or
// forged smaller set.
type DeleteMemoryArtifact struct {
	Target                 domain.MemoryArtifactID
	AcknowledgedDependents []domain.MemoryArtifactID
	ReasonRedacted         string
}

// MemoryArtifactDeletionOutcome reports every artifact actually deleted by
// one cascading logical deletion, plus the domain-computed preview it was
// validated against.
type MemoryArtifactDeletionOutcome struct {
	Preview          domain.MemoryArtifactDeletionPreview
	DeletedArtifacts []domain.MemoryArtifactID
	// InvalidatedEmbeddings counts derived vectors invalidated alongside the
	// deleted artifacts (M21-088). Rows are retained for lineage per
	// M21-135, so this counts invalidations, never deletions.
	InvalidatedEmbeddings int
}

// CreateMemoryArtifact persists a new artifact identity and its first
// revision, or returns the existing revision on an idempotent retry.
func (repositories *Repositories) CreateMemoryArtifact(
	ctx context.Context,
	input CreateMemoryArtifact,
) (MemoryArtifactRevisionRecord, error) {
	if err := validateBounded("memory artifact idempotency key", input.IdempotencyKey, 255); err != nil {
		return MemoryArtifactRevisionRecord{}, err
	}
	scope := domain.MemoryProjectScope{Project: input.ProjectID}
	if input.ScopeRepositoryID != nil {
		scope.Repository = *input.ScopeRepositoryID
	}
	if _, err := domain.NewMemoryArtifactRevision(input.ArtifactID, input.RevisionID, scope, input.Content); err != nil {
		return MemoryArtifactRevisionRecord{}, err
	}
	contentRepositoryID, binding, err := memoryArtifactContentBinding(input.Content)
	if err != nil {
		return MemoryArtifactRevisionRecord{}, err
	}
	contentJSON, contentSHA256, err := canonicalMemoryArtifactContent(input.Content)
	if err != nil {
		return MemoryArtifactRevisionRecord{}, err
	}
	now, micros := repositories.timestamp()
	var record MemoryArtifactRevisionRecord
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existingArtifact, found, err := findMemoryArtifact(ctx, transaction.sql, input.ArtifactID)
		if err != nil {
			return err
		}
		if found {
			if existingArtifact.ProjectID != input.ProjectID || existingArtifact.Kind != input.Content.Kind {
				return typedError(ErrConflict, "create memory artifact", errors.New("artifact identity retried with different project or kind"))
			}
		} else {
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO memory_artifacts (id, kind, project_id, created_at_unix_micros)
				 VALUES (?, ?, ?, ?)`,
				input.ArtifactID, input.Content.Kind, input.ProjectID, micros,
			); err != nil {
				return repositoryWriteError("create memory artifact identity", err)
			}
		}
		existingRevision, found, err := findMemoryArtifactRevisionByIdempotency(ctx, transaction.sql, input.ArtifactID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existingRevision.RevisionID != input.RevisionID || existingRevision.ContentSHA256 != contentSHA256 {
				return typedError(ErrConflict, "create memory artifact revision", errors.New("idempotency key belongs to a different revision"))
			}
			record = existingRevision
			return nil
		}
		if err := insertMemoryArtifactRevision(ctx, transaction, memoryArtifactRevisionInsert{
			RevisionID:            input.RevisionID,
			ArtifactID:            input.ArtifactID,
			RevisionNumber:        1,
			Kind:                  input.Content.Kind,
			ScopeRepositoryID:     input.ScopeRepositoryID,
			ContentRepositoryID:   contentRepositoryID,
			ContentJSON:           contentJSON,
			ContentSHA256:         contentSHA256,
			Binding:               binding,
			SupersedesRevisionID:  nil,
			CreatedFromCorrection: false,
			IdempotencyKey:        input.IdempotencyKey,
			CreatedAtMicros:       micros,
		}); err != nil {
			return err
		}
		record = MemoryArtifactRevisionRecord{
			ArtifactID: input.ArtifactID, RevisionID: input.RevisionID, RevisionNumber: 1,
			Kind: input.Content.Kind, ScopeRepositoryID: input.ScopeRepositoryID,
			ContentRepositoryID: contentRepositoryID, Content: input.Content, ContentSHA256: contentSHA256,
			Binding: memoryRevisionBindingFromDomain(binding), Maturity: domain.MaturityStateCandidate,
			IdempotencyKey: input.IdempotencyKey, CreatedAt: now,
		}
		return nil
	})
	if err != nil {
		return MemoryArtifactRevisionRecord{}, err
	}
	return record, nil
}

// CreateMemoryArtifactCorrection persists a new revision superseding prior
// without mutating it, per domain.ReviseMemoryArtifactWithUserCorrection.
func (repositories *Repositories) CreateMemoryArtifactCorrection(
	ctx context.Context,
	input CreateMemoryArtifactCorrection,
) (MemoryArtifactRevisionRecord, error) {
	if err := validateBounded("memory artifact correction idempotency key", input.IdempotencyKey, 255); err != nil {
		return MemoryArtifactRevisionRecord{}, err
	}
	now, micros := repositories.timestamp()
	var record MemoryArtifactRevisionRecord
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		prior, err := scanMemoryArtifactRevision(transaction.sql.QueryRowContext(ctx, memoryArtifactRevisionSelect+` WHERE id = ?`, input.PriorRevisionID), "read prior memory artifact revision")
		if err != nil {
			return err
		}
		revised, err := domain.ReviseMemoryArtifactWithUserCorrection(
			domain.MemoryArtifactRevision{ArtifactID: prior.ArtifactID, RevisionID: prior.RevisionID, Maturity: prior.Maturity},
			input.NewRevisionID,
			input.CorrectedContent,
		)
		if err != nil {
			return err
		}
		if revised.Content.Kind != prior.Kind {
			return typedError(ErrConstraint, "create memory artifact correction", errors.New("correction cannot change the artifact kind"))
		}
		existingRevision, found, err := findMemoryArtifactRevisionByIdempotency(ctx, transaction.sql, prior.ArtifactID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		contentRepositoryID, binding, err := memoryArtifactContentBinding(input.CorrectedContent)
		if err != nil {
			return err
		}
		contentJSON, contentSHA256, err := canonicalMemoryArtifactContent(input.CorrectedContent)
		if err != nil {
			return err
		}
		if found {
			if existingRevision.RevisionID != input.NewRevisionID || existingRevision.ContentSHA256 != contentSHA256 {
				return typedError(ErrConflict, "create memory artifact correction", errors.New("idempotency key belongs to a different revision"))
			}
			record = existingRevision
			return nil
		}
		if err := insertMemoryArtifactRevision(ctx, transaction, memoryArtifactRevisionInsert{
			RevisionID: input.NewRevisionID, ArtifactID: prior.ArtifactID, RevisionNumber: prior.RevisionNumber + 1,
			Kind: revised.Content.Kind, ScopeRepositoryID: prior.ScopeRepositoryID,
			ContentRepositoryID: contentRepositoryID, ContentJSON: contentJSON, ContentSHA256: contentSHA256,
			Binding: binding, SupersedesRevisionID: &prior.RevisionID, CreatedFromCorrection: true,
			IdempotencyKey: input.IdempotencyKey, CreatedAtMicros: micros,
		}); err != nil {
			return err
		}
		supersedes := prior.RevisionID
		record = MemoryArtifactRevisionRecord{
			ArtifactID: prior.ArtifactID, RevisionID: input.NewRevisionID, RevisionNumber: prior.RevisionNumber + 1,
			Kind: revised.Content.Kind, ScopeRepositoryID: prior.ScopeRepositoryID,
			ContentRepositoryID: contentRepositoryID, Content: input.CorrectedContent, ContentSHA256: contentSHA256,
			Binding: memoryRevisionBindingFromDomain(binding), Maturity: domain.MaturityStateCandidate,
			SupersedesRevisionID: &supersedes, CreatedFromCorrection: true,
			IdempotencyKey: input.IdempotencyKey, CreatedAt: now,
		}
		return nil
	})
	if err != nil {
		return MemoryArtifactRevisionRecord{}, err
	}
	return record, nil
}

// GetMemoryArtifactRevision reads one revision by its exact identity.
func (repositories *Repositories) GetMemoryArtifactRevision(
	ctx context.Context,
	id domain.MemoryArtifactRevisionID,
) (MemoryArtifactRevisionRecord, error) {
	if id.IsZero() {
		return MemoryArtifactRevisionRecord{}, errors.New("memory artifact revision ID must not be empty")
	}
	return scanMemoryArtifactRevision(
		repositories.database.sql.QueryRowContext(ctx, memoryArtifactRevisionSelect+` WHERE id = ?`, id),
		"get memory artifact revision",
	)
}

// GetLatestMemoryArtifactRevision reads the highest-numbered revision for
// one artifact regardless of maturity.
func (repositories *Repositories) GetLatestMemoryArtifactRevision(
	ctx context.Context,
	artifactID domain.MemoryArtifactID,
) (MemoryArtifactRevisionRecord, error) {
	if artifactID.IsZero() {
		return MemoryArtifactRevisionRecord{}, errors.New("memory artifact ID must not be empty")
	}
	return scanMemoryArtifactRevision(
		repositories.database.sql.QueryRowContext(
			ctx,
			memoryArtifactRevisionSelect+` WHERE artifact_id = ? ORDER BY revision_number DESC LIMIT 1`,
			artifactID,
		),
		"get latest memory artifact revision",
	)
}

// DeleteMemoryArtifact logically deletes target and cascades the deletion
// to its transitive derived_from (semantic dependent) descendants, never
// its influenced_by (contextually exposed) descendants, per docs/plan.md
// §31 "Descendant Contamination". It composes with
// domain.PreviewMemoryArtifactDeletion and domain.ValidateMemoryArtifactDeletion:
// direct semantic dependents must be previewed and acknowledged first.
func (repositories *Repositories) DeleteMemoryArtifact(
	ctx context.Context,
	input DeleteMemoryArtifact,
) (MemoryArtifactDeletionOutcome, error) {
	if input.Target.IsZero() {
		return MemoryArtifactDeletionOutcome{}, errors.New("memory artifact target must not be empty")
	}
	if err := validateBounded("memory artifact deletion reason", input.ReasonRedacted, 2048); err != nil {
		return MemoryArtifactDeletionOutcome{}, err
	}
	_, micros := repositories.timestamp()
	var outcome MemoryArtifactDeletionOutcome
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		target, found, err := findMemoryArtifact(ctx, transaction.sql, input.Target)
		if err != nil {
			return err
		}
		if !found {
			return typedError(ErrNotFound, "delete memory artifact", errors.New("memory artifact does not exist"))
		}
		if target.DeletedAt != nil {
			outcome = MemoryArtifactDeletionOutcome{
				Preview:          domain.MemoryArtifactDeletionPreview{Target: input.Target},
				DeletedArtifacts: nil,
			}
			return nil
		}
		index, err := loadMemoryArtifactLineageIndexForDeletion(ctx, transaction.sql, input.Target)
		if err != nil {
			return err
		}
		preview, err := domain.PreviewMemoryArtifactDeletion(input.Target, index)
		if err != nil {
			return err
		}
		if err := domain.ValidateMemoryArtifactDeletion(domain.MemoryArtifactDeletionRequest{
			Target: input.Target, Preview: preview, AcknowledgedDependents: input.AcknowledgedDependents,
		}, index); err != nil {
			return err
		}
		cascade := cascadeMemoryArtifactDerivedFrom(input.Target, index)
		for _, id := range cascade {
			reason := input.ReasonRedacted
			if id != input.Target {
				reason = "cascaded from deletion of " + input.Target.String() + ": " + input.ReasonRedacted
			}
			result, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE memory_artifacts
				 SET deleted_at_unix_micros = ?, deletion_reason_redacted = ?
				 WHERE id = ? AND deleted_at_unix_micros IS NULL`,
				micros, reason, id,
			)
			if err != nil {
				return repositoryWriteError("delete memory artifact", err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return classify("delete memory artifact", err)
			}
			if affected == 1 {
				outcome.DeletedArtifacts = append(outcome.DeletedArtifacts, id)
			}
			// M21-088: a deleted artifact's derived vectors must not survive
			// it. Deletion here is logical, so the embeddings are
			// invalidated in the same transaction rather than dropped: the
			// row stays for lineage (M21-135) but can never be an active
			// retrieval candidate again.
			invalidated, err := invalidateMemoryArtifactEmbeddingsForArtifact(ctx, transaction, id, micros)
			if err != nil {
				return err
			}
			outcome.InvalidatedEmbeddings += invalidated
		}
		outcome.Preview = preview
		return nil
	})
	if err != nil {
		return MemoryArtifactDeletionOutcome{}, err
	}
	return outcome, nil
}

// cascadeMemoryArtifactDerivedFrom returns target and every descendant
// transitively reachable through derived_from edges (semantic dependency),
// in breadth-first, deterministic order. influenced_by descendants are
// deliberately excluded: they are only ever flagged, never cascaded.
func cascadeMemoryArtifactDerivedFrom(
	target domain.MemoryArtifactID,
	index map[domain.MemoryArtifactID]domain.MemoryArtifactLineage,
) []domain.MemoryArtifactID {
	visited := map[domain.MemoryArtifactID]struct{}{target: {}}
	order := []domain.MemoryArtifactID{target}
	queue := []domain.MemoryArtifactID{target}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for id, lineage := range index {
			if _, seen := visited[id]; seen {
				continue
			}
			for _, ancestor := range lineage.DerivedFrom {
				if ancestor == current {
					visited[id] = struct{}{}
					order = append(order, id)
					queue = append(queue, id)
					break
				}
			}
		}
	}
	return order
}

func memoryArtifactContentBinding(content domain.MemoryArtifactContent) (domain.RepositoryID, *domain.RevisionBinding, error) {
	switch content.Kind {
	case domain.MemoryArtifactKindRepositoryFact:
		return content.RepositoryFact.Repository, &content.RepositoryFact.Binding, nil
	case domain.MemoryArtifactKindReviewedCommand:
		return content.ReviewedCommand.Repository, &content.ReviewedCommand.Binding, nil
	case domain.MemoryArtifactKindFileToTestMapping:
		return content.FileToTestMapping.Repository, &content.FileToTestMapping.Binding, nil
	case domain.MemoryArtifactKindRepositoryConvention:
		return content.RepositoryConvention.Repository, &content.RepositoryConvention.Binding, nil
	case domain.MemoryArtifactKindAcceptedRegressionCase:
		return content.AcceptedRegressionCase.Repository, &content.AcceptedRegressionCase.Binding, nil
	case domain.MemoryArtifactKindExecutionRecipe:
		return content.ExecutionRecipe.Repository, nil, nil
	case domain.MemoryArtifactKindExecutableAtomReference:
		return content.AtomReference.Repository, &content.AtomReference.Binding, nil
	case domain.MemoryArtifactKindObservationHypothesis:
		return content.ObservationHypothesis.Repository, nil, nil
	default:
		return domain.RepositoryID{}, nil, fmt.Errorf("memory artifact content kind %q is not declared", content.Kind)
	}
}

func canonicalMemoryArtifactContent(content domain.MemoryArtifactContent) (string, string, error) {
	encoded, err := json.Marshal(content)
	if err != nil {
		return "", "", fmt.Errorf("marshal memory artifact content: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return string(encoded), hex.EncodeToString(sum[:]), nil
}

type memoryArtifactRevisionInsert struct {
	RevisionID            domain.MemoryArtifactRevisionID
	ArtifactID            domain.MemoryArtifactID
	RevisionNumber        uint64
	Kind                  domain.MemoryArtifactKind
	ScopeRepositoryID     *domain.RepositoryID
	ContentRepositoryID   domain.RepositoryID
	ContentJSON           string
	ContentSHA256         string
	Binding               *domain.RevisionBinding
	SupersedesRevisionID  *domain.MemoryArtifactRevisionID
	CreatedFromCorrection bool
	IdempotencyKey        string
	CreatedAtMicros       int64
}

func insertMemoryArtifactRevision(ctx context.Context, transaction *Transaction, input memoryArtifactRevisionInsert) error {
	var bindingKnown any
	var exact, validFrom, validUntil, unknownReason any
	if input.Binding != nil {
		bindingKnown = input.Binding.Known
		if input.Binding.Known {
			exact = nullableNonEmpty(input.Binding.ExactRevision)
			validFrom = nullableNonEmpty(input.Binding.ValidFromRevision)
			validUntil = nullableNonEmpty(input.Binding.ValidUntilRevision)
		} else {
			unknownReason = input.Binding.UnknownReason
		}
	}
	_, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO memory_artifact_revisions (
			id, artifact_id, revision_number, kind, scope_repository_id,
			content_repository_id, content_json, content_sha256,
			binding_known, binding_exact_revision, binding_valid_from_revision,
			binding_valid_until_revision, binding_unknown_reason,
			supersedes_revision_id, created_from_correction, idempotency_key,
			created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.RevisionID, input.ArtifactID, input.RevisionNumber, input.Kind,
		nullableRepositoryID(input.ScopeRepositoryID), input.ContentRepositoryID,
		input.ContentJSON, input.ContentSHA256, bindingKnown, exact, validFrom, validUntil, unknownReason,
		nullableRevisionID(input.SupersedesRevisionID), input.CreatedFromCorrection,
		input.IdempotencyKey, input.CreatedAtMicros,
	)
	return repositoryWriteError("insert memory artifact revision", err)
}

func nullableNonEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableRevisionID(id *domain.MemoryArtifactRevisionID) any {
	if id == nil {
		return nil
	}
	return *id
}

func memoryRevisionBindingFromDomain(binding *domain.RevisionBinding) *MemoryRevisionBinding {
	if binding == nil {
		return nil
	}
	return &MemoryRevisionBinding{
		Known:              binding.Known,
		UnknownReason:      binding.UnknownReason,
		ExactRevision:      binding.ExactRevision,
		ValidFromRevision:  binding.ValidFromRevision,
		ValidUntilRevision: binding.ValidUntilRevision,
	}
}

func findMemoryArtifact(ctx context.Context, queries queryRower, id domain.MemoryArtifactID) (MemoryArtifact, bool, error) {
	var (
		artifact       MemoryArtifact
		createdMicros  int64
		deletedMicros  sql.NullInt64
		deletionReason sql.NullString
	)
	err := queries.QueryRowContext(
		ctx,
		`SELECT id, kind, project_id, created_at_unix_micros, deleted_at_unix_micros, deletion_reason_redacted
		 FROM memory_artifacts WHERE id = ?`,
		id,
	).Scan(&artifact.ID, &artifact.Kind, &artifact.ProjectID, &createdMicros, &deletedMicros, &deletionReason)
	if errors.Is(err, sql.ErrNoRows) {
		return MemoryArtifact{}, false, nil
	}
	if err != nil {
		return MemoryArtifact{}, false, classify("find memory artifact", err)
	}
	artifact.CreatedAt = repositoryTime(createdMicros)
	if deletedMicros.Valid {
		deletedAt := repositoryTime(deletedMicros.Int64)
		artifact.DeletedAt = &deletedAt
	}
	if deletionReason.Valid {
		artifact.DeletionReason = &deletionReason.String
	}
	return artifact, true, nil
}

const memoryArtifactRevisionSelect = `SELECT
	id, artifact_id, revision_number, kind, scope_repository_id, content_repository_id,
	content_json, content_sha256, binding_known, binding_exact_revision,
	binding_valid_from_revision, binding_valid_until_revision, binding_unknown_reason,
	maturity, supersedes_revision_id, created_from_correction, idempotency_key,
	created_at_unix_micros
 FROM memory_artifact_revisions`

func findMemoryArtifactRevisionByIdempotency(
	ctx context.Context,
	queries queryRower,
	artifactID domain.MemoryArtifactID,
	key string,
) (MemoryArtifactRevisionRecord, bool, error) {
	record, err := scanMemoryArtifactRevision(
		queries.QueryRowContext(ctx, memoryArtifactRevisionSelect+` WHERE artifact_id = ? AND idempotency_key = ?`, artifactID, key),
		"find memory artifact revision by idempotency",
	)
	if errors.Is(err, ErrNotFound) {
		return MemoryArtifactRevisionRecord{}, false, nil
	}
	return record, err == nil, err
}

func scanMemoryArtifactRevision(row rowScanner, operation string) (MemoryArtifactRevisionRecord, error) {
	var (
		record                                      MemoryArtifactRevisionRecord
		scopeRepositoryRaw                          sql.NullString
		contentJSON                                 string
		bindingKnown                                sql.NullBool
		exact, validFrom, validUntil, unknownReason sql.NullString
		supersedesRaw                               sql.NullString
		createdMicros                               int64
	)
	err := row.Scan(
		&record.RevisionID, &record.ArtifactID, &record.RevisionNumber, &record.Kind,
		&scopeRepositoryRaw, &record.ContentRepositoryID, &contentJSON, &record.ContentSHA256,
		&bindingKnown, &exact, &validFrom, &validUntil, &unknownReason,
		&record.Maturity, &supersedesRaw, &record.CreatedFromCorrection, &record.IdempotencyKey,
		&createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MemoryArtifactRevisionRecord{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return MemoryArtifactRevisionRecord{}, classify(operation, err)
	}
	if err := json.Unmarshal([]byte(contentJSON), &record.Content); err != nil {
		return MemoryArtifactRevisionRecord{}, typedError(ErrCorrupt, operation, err)
	}
	if scopeRepositoryRaw.Valid {
		id, err := domain.ParseRepositoryID(scopeRepositoryRaw.String)
		if err != nil {
			return MemoryArtifactRevisionRecord{}, typedError(ErrCorrupt, operation, err)
		}
		record.ScopeRepositoryID = &id
	}
	if bindingKnown.Valid {
		record.Binding = &MemoryRevisionBinding{
			Known:              bindingKnown.Bool,
			UnknownReason:      unknownReason.String,
			ExactRevision:      exact.String,
			ValidFromRevision:  validFrom.String,
			ValidUntilRevision: validUntil.String,
		}
	}
	if supersedesRaw.Valid {
		id, err := domain.ParseMemoryArtifactRevisionID(supersedesRaw.String)
		if err != nil {
			return MemoryArtifactRevisionRecord{}, typedError(ErrCorrupt, operation, err)
		}
		record.SupersedesRevisionID = &id
	}
	record.CreatedAt = repositoryTime(createdMicros)
	return record, nil
}

// invalidateMemoryArtifactEmbeddingsForArtifact invalidates every still-valid
// embedding derived from any revision of artifactID (M21-088). It returns how
// many rows it invalidated.
//
// The rows are retained rather than removed: M21-135 keeps prior vectors for
// historical lineage while excluding them from active retrieval, and
// retention and eligibility are separate questions.
func invalidateMemoryArtifactEmbeddingsForArtifact(
	ctx context.Context,
	transaction *Transaction,
	artifactID domain.MemoryArtifactID,
	micros int64,
) (int, error) {
	result, err := transaction.sql.ExecContext(
		ctx,
		`UPDATE memory_artifact_embeddings
		 SET valid = 0, invalidated_at_unix_micros = ?
		 WHERE valid = 1 AND revision_id IN (
		     SELECT id FROM memory_artifact_revisions WHERE artifact_id = ?
		 )`,
		micros, artifactID,
	)
	if err != nil {
		return 0, repositoryWriteError("invalidate memory artifact embeddings on deletion", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, classify("invalidate memory artifact embeddings on deletion", err)
	}
	return int(affected), nil
}
