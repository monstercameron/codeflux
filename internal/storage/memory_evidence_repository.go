// Package storage: supporting-evidence links for memory artifact revisions
// (M21-018). See internal/domain/memory.go's SupportingEvidenceRecord and
// AuthorizeMemoryArtifactMaturityGrant, and docs/plan.md §31 "Agent
// explanations are stored only as agent_self_report with
// evidence_strength: none."
package storage

import (
	"context"
	"database/sql"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

// MemoryArtifactSupportingEvidence is one immutable evidence link.
type MemoryArtifactSupportingEvidence struct {
	RevisionID domain.MemoryArtifactRevisionID
	EvidenceID domain.EvidenceID
	Source     domain.EvidenceSourceKind
	Strength   domain.EvidenceStrength
	RecordedAt int64
}

// RecordMemoryArtifactSupportingEvidence links one piece of evidence to one
// revision, validated by domain.SupportingEvidenceRecord.Validate before
// any write. Idempotent: recording the same (revision, evidence) link
// again with identical source/strength is a no-op; a conflicting retry is
// rejected.
func (repositories *Repositories) RecordMemoryArtifactSupportingEvidence(
	ctx context.Context,
	revisionID domain.MemoryArtifactRevisionID,
	record domain.SupportingEvidenceRecord,
) error {
	if revisionID.IsZero() {
		return errors.New("memory artifact revision ID must not be empty")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	_, micros := repositories.timestamp()
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var existingSource, existingStrength string
		err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT source, strength FROM memory_artifact_supporting_evidence
			 WHERE revision_id = ? AND evidence_id = ?`,
			revisionID, record.Evidence,
		).Scan(&existingSource, &existingStrength)
		if err == nil {
			if existingSource != string(record.Source) || existingStrength != string(record.Strength) {
				return typedError(ErrConflict, "record memory artifact supporting evidence", errors.New("evidence link already recorded with a different source or strength"))
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return classify("find memory artifact supporting evidence", err)
		}
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_artifact_supporting_evidence (
				revision_id, evidence_id, source, strength, recorded_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?)`,
			revisionID, record.Evidence, record.Source, record.Strength, micros,
		)
		return repositoryWriteError("record memory artifact supporting evidence", err)
	})
}

// ListMemoryArtifactSupportingEvidence reads every evidence link recorded
// for one revision, in insertion order.
func (repositories *Repositories) ListMemoryArtifactSupportingEvidence(
	ctx context.Context,
	revisionID domain.MemoryArtifactRevisionID,
) ([]MemoryArtifactSupportingEvidence, error) {
	if revisionID.IsZero() {
		return nil, errors.New("memory artifact revision ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT revision_id, evidence_id, source, strength, recorded_at_unix_micros
		 FROM memory_artifact_supporting_evidence
		 WHERE revision_id = ? ORDER BY recorded_at_unix_micros`,
		revisionID,
	)
	if err != nil {
		return nil, classify("list memory artifact supporting evidence", err)
	}
	defer rows.Close()
	var links []MemoryArtifactSupportingEvidence
	for rows.Next() {
		var link MemoryArtifactSupportingEvidence
		if err := rows.Scan(&link.RevisionID, &link.EvidenceID, &link.Source, &link.Strength, &link.RecordedAt); err != nil {
			return nil, classify("scan memory artifact supporting evidence", err)
		}
		links = append(links, link)
	}
	return links, classify("list memory artifact supporting evidence", rows.Err())
}

// supportingEvidenceRecords loads every evidence link for revisionID as
// domain.SupportingEvidenceRecord values, for
// domain.AuthorizeMemoryArtifactMaturityGrant.
func supportingEvidenceRecords(
	ctx context.Context,
	queries memoryLineageQueryer,
	revisionID domain.MemoryArtifactRevisionID,
) ([]domain.SupportingEvidenceRecord, error) {
	rows, err := queries.QueryContext(
		ctx,
		`SELECT evidence_id, source, strength FROM memory_artifact_supporting_evidence
		 WHERE revision_id = ? ORDER BY recorded_at_unix_micros`,
		revisionID,
	)
	if err != nil {
		return nil, classify("load memory artifact supporting evidence", err)
	}
	defer rows.Close()
	var records []domain.SupportingEvidenceRecord
	for rows.Next() {
		var record domain.SupportingEvidenceRecord
		if err := rows.Scan(&record.Evidence, &record.Source, &record.Strength); err != nil {
			return nil, classify("scan memory artifact supporting evidence", err)
		}
		records = append(records, record)
	}
	return records, classify("load memory artifact supporting evidence", rows.Err())
}
