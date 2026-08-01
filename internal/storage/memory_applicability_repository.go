// Package storage: structured, machine-checkable applicability-predicate
// records (M21-022), distinct from the free-text ApplicabilityStatement
// carried inside domain.ExecutionRecipeContent. Deterministic predicate
// evaluation is the M21-064..077 retrieval gate's concern; this file only
// stores the predicate records.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"codeflux.dev/codeflux/internal/domain"
)

// MemoryArtifactApplicabilityPredicateKind classifies the shape a stored
// applicability predicate checks.
type MemoryArtifactApplicabilityPredicateKind string

const (
	ApplicabilityPredicateRepositoryMatch       MemoryArtifactApplicabilityPredicateKind = "repository-match"
	ApplicabilityPredicateDependencyBinding     MemoryArtifactApplicabilityPredicateKind = "dependency-binding"
	ApplicabilityPredicateCapabilityRequirement MemoryArtifactApplicabilityPredicateKind = "capability-requirement"
	ApplicabilityPredicateEffectRequirement     MemoryArtifactApplicabilityPredicateKind = "effect-requirement"
	ApplicabilityPredicatePathScope             MemoryArtifactApplicabilityPredicateKind = "path-scope"
	ApplicabilityPredicateCustom                MemoryArtifactApplicabilityPredicateKind = "custom"
)

// MemoryArtifactApplicabilityUnknownFieldBehavior declares how a predicate
// behaves when a required fingerprint field is absent, per docs/plan.md
// §31 "Every predicate declares... behavior when fields are absent."
type MemoryArtifactApplicabilityUnknownFieldBehavior string

const (
	ApplicabilityUnknownFieldReject MemoryArtifactApplicabilityUnknownFieldBehavior = "reject"
	ApplicabilityUnknownFieldSkip   MemoryArtifactApplicabilityUnknownFieldBehavior = "skip"
	ApplicabilityUnknownFieldWarn   MemoryArtifactApplicabilityUnknownFieldBehavior = "warn"
)

// CreateMemoryArtifactApplicabilityPredicate declares one structured
// applicability predicate bound to one revision.
type CreateMemoryArtifactApplicabilityPredicate struct {
	RevisionID               domain.MemoryArtifactRevisionID
	FingerprintSchemaVersion int
	PredicateKind            MemoryArtifactApplicabilityPredicateKind
	Predicate                any
	RequiredFields           []string
	UnknownFieldBehavior     MemoryArtifactApplicabilityUnknownFieldBehavior
}

// MemoryArtifactApplicabilityPredicate is one durable stored predicate.
type MemoryArtifactApplicabilityPredicate struct {
	ID                       string
	RevisionID               domain.MemoryArtifactRevisionID
	FingerprintSchemaVersion int
	PredicateKind            MemoryArtifactApplicabilityPredicateKind
	PredicateJSON            string
	RequiredFields           []string
	UnknownFieldBehavior     MemoryArtifactApplicabilityUnknownFieldBehavior
	CreatedAtMicros          int64
}

// CreateMemoryArtifactApplicabilityPredicate persists one predicate
// record.
func (repositories *Repositories) CreateMemoryArtifactApplicabilityPredicate(
	ctx context.Context,
	input CreateMemoryArtifactApplicabilityPredicate,
) (MemoryArtifactApplicabilityPredicate, error) {
	switch {
	case input.RevisionID.IsZero():
		return MemoryArtifactApplicabilityPredicate{}, errors.New("memory artifact revision ID must not be empty")
	case input.FingerprintSchemaVersion < 1:
		return MemoryArtifactApplicabilityPredicate{}, errors.New("fingerprint schema version must be at least 1")
	case !isValidApplicabilityPredicateKind(input.PredicateKind):
		return MemoryArtifactApplicabilityPredicate{}, errors.New("applicability predicate kind is not declared")
	case !isValidApplicabilityUnknownFieldBehavior(input.UnknownFieldBehavior):
		return MemoryArtifactApplicabilityPredicate{}, errors.New("applicability predicate unknown-field behavior is not declared")
	}
	predicateJSON, err := json.Marshal(input.Predicate)
	if err != nil {
		return MemoryArtifactApplicabilityPredicate{}, err
	}
	requiredFieldsJSON, err := json.Marshal(input.RequiredFields)
	if err != nil {
		return MemoryArtifactApplicabilityPredicate{}, err
	}
	_, micros := repositories.timestamp()
	id := memoryApplicabilityPredicateID(input.RevisionID, string(predicateJSON), micros)
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_artifact_applicability_predicates (
				id, revision_id, fingerprint_schema_version, predicate_kind,
				predicate_json, required_fields_json, unknown_field_behavior,
				created_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, input.RevisionID, input.FingerprintSchemaVersion, input.PredicateKind,
			string(predicateJSON), string(requiredFieldsJSON), input.UnknownFieldBehavior, micros,
		)
		return repositoryWriteError("create memory artifact applicability predicate", err)
	})
	if err != nil {
		return MemoryArtifactApplicabilityPredicate{}, err
	}
	return MemoryArtifactApplicabilityPredicate{
		ID: id, RevisionID: input.RevisionID, FingerprintSchemaVersion: input.FingerprintSchemaVersion,
		PredicateKind: input.PredicateKind, PredicateJSON: string(predicateJSON),
		RequiredFields: input.RequiredFields, UnknownFieldBehavior: input.UnknownFieldBehavior,
		CreatedAtMicros: micros,
	}, nil
}

// ListMemoryArtifactApplicabilityPredicates reads every predicate stored
// for one revision.
func (repositories *Repositories) ListMemoryArtifactApplicabilityPredicates(
	ctx context.Context,
	revisionID domain.MemoryArtifactRevisionID,
) ([]MemoryArtifactApplicabilityPredicate, error) {
	if revisionID.IsZero() {
		return nil, errors.New("memory artifact revision ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT id, revision_id, fingerprint_schema_version, predicate_kind,
		        predicate_json, required_fields_json, unknown_field_behavior, created_at_unix_micros
		 FROM memory_artifact_applicability_predicates WHERE revision_id = ? ORDER BY created_at_unix_micros`,
		revisionID,
	)
	if err != nil {
		return nil, classify("list memory artifact applicability predicates", err)
	}
	defer rows.Close()
	var records []MemoryArtifactApplicabilityPredicate
	for rows.Next() {
		var (
			record             MemoryArtifactApplicabilityPredicate
			requiredFieldsJSON string
		)
		if err := rows.Scan(
			&record.ID, &record.RevisionID, &record.FingerprintSchemaVersion, &record.PredicateKind,
			&record.PredicateJSON, &requiredFieldsJSON, &record.UnknownFieldBehavior, &record.CreatedAtMicros,
		); err != nil {
			return nil, classify("scan memory artifact applicability predicate", err)
		}
		if err := json.Unmarshal([]byte(requiredFieldsJSON), &record.RequiredFields); err != nil {
			return nil, typedError(ErrCorrupt, "scan memory artifact applicability predicate", err)
		}
		records = append(records, record)
	}
	return records, classify("list memory artifact applicability predicates", rows.Err())
}

func isValidApplicabilityPredicateKind(kind MemoryArtifactApplicabilityPredicateKind) bool {
	switch kind {
	case ApplicabilityPredicateRepositoryMatch, ApplicabilityPredicateDependencyBinding,
		ApplicabilityPredicateCapabilityRequirement, ApplicabilityPredicateEffectRequirement,
		ApplicabilityPredicatePathScope, ApplicabilityPredicateCustom:
		return true
	default:
		return false
	}
}

func isValidApplicabilityUnknownFieldBehavior(behavior MemoryArtifactApplicabilityUnknownFieldBehavior) bool {
	switch behavior {
	case ApplicabilityUnknownFieldReject, ApplicabilityUnknownFieldSkip, ApplicabilityUnknownFieldWarn:
		return true
	default:
		return false
	}
}

func memoryApplicabilityPredicateID(revisionID domain.MemoryArtifactRevisionID, predicateJSON string, micros int64) string {
	sum := sha256.Sum256([]byte(revisionID.String() + "|" + predicateJSON + "|" + strconv.FormatInt(micros, 10)))
	return hex.EncodeToString(sum[:])
}
