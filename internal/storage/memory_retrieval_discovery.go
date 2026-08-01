// Package storage: read-side discovery helpers for the M21-064..076
// pre-work retrieval gate (internal/retrieval). Every function here is a
// pure structured-field lookup -- exact repository/kind/project filters or
// an exact fingerprint-hash match -- never a similarity search, per
// AGENTS.md "Vector similarity may discover candidates; it never
// establishes compatibility, validity, assurance, or permission." Nothing
// here decides eligibility: internal/retrievalgate.EvaluateEligibility is
// the only place that happens.
//
// This file is deliberately separate from memory_artifact_repository.go,
// evidence_repository.go, and episode_repository.go: it only reads,
// through each of those tables' already-declared schema, without touching
// (or needing to touch) any of those three files.
package storage

import (
	"context"
	"database/sql"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

// ListMemoryArtifactRevisionsByRepositoryAndKind reads the latest (highest
// revision_number) non-deleted revision of every memory artifact of kind
// bound to repositoryID within projectID (M21-065, M21-066, M21-067's
// repository/command/recipe/atom discovery: "retrieve reviewed project
// facts relevant to selected context," "retrieve compatible commands and
// file-to-test mappings," "retrieve exact reusable atoms or recipes").
// This is a deterministic structured-field filter, never a similarity
// search: callers should log it under
// storage.RetrievalCandidateApplicabilityPass (or ExactMatch when the
// candidate independently also matched an exact-identity channel; see
// internal/retrieval), never RetrievalCandidateVectorSimilarity.
func (repositories *Repositories) ListMemoryArtifactRevisionsByRepositoryAndKind(
	ctx context.Context,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	kind domain.MemoryArtifactKind,
) ([]MemoryArtifactRevisionRecord, error) {
	switch {
	case projectID.IsZero():
		return nil, errors.New("project ID must not be empty")
	case repositoryID.IsZero():
		return nil, errors.New("repository ID must not be empty")
	case !kind.IsValid():
		return nil, errors.New("memory artifact kind is not declared")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		memoryArtifactRevisionSelect+`
		 WHERE kind = ?
		   AND content_repository_id = ?
		   AND revision_number = (
		       SELECT MAX(revision_number) FROM memory_artifact_revisions AS latest
		       WHERE latest.artifact_id = memory_artifact_revisions.artifact_id
		   )
		   AND artifact_id IN (
		       SELECT id FROM memory_artifacts
		       WHERE project_id = ? AND deleted_at_unix_micros IS NULL
		   )
		 ORDER BY artifact_id`,
		kind, repositoryID, projectID,
	)
	if err != nil {
		return nil, classify("list memory artifact revisions by repository and kind", err)
	}
	defer rows.Close()
	var records []MemoryArtifactRevisionRecord
	for rows.Next() {
		record, err := scanMemoryArtifactRevision(rows, "scan memory artifact revision by repository and kind")
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, classify("list memory artifact revisions by repository and kind", rows.Err())
}

// GetEvidenceAssuranceLevel reads the currently recorded assurance level for
// one evidence row (the "evidence" table's own assurance_level column, set
// at CreateEvidence time), for building
// internal/retrievalgate.EligibilityCandidate.SupportingEvidenceAssurance.
// This is a distinct axis from domain.EvidenceStrength (which governs
// memory-artifact maturity promotion, see memory_evidence_repository.go):
// AssuranceLevel describes how rigorously the claim behind the evidence was
// verified, not how many independent sources corroborate it.
func (repositories *Repositories) GetEvidenceAssuranceLevel(
	ctx context.Context,
	evidenceID domain.EvidenceID,
) (domain.AssuranceLevel, error) {
	if evidenceID.IsZero() {
		return "", errors.New("evidence ID must not be empty")
	}
	var level domain.AssuranceLevel
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT assurance_level FROM evidence WHERE id = ?`,
		evidenceID,
	).Scan(&level)
	if errors.Is(err, sql.ErrNoRows) {
		return "", typedError(ErrNotFound, "get evidence assurance level", err)
	}
	if err != nil {
		return "", classify("get evidence assurance level", err)
	}
	return level, nil
}

// ListEpisodesByFingerprintHash reads every closed episode within
// projectID whose exact task fingerprint hash matches fingerprintHash,
// newest first (M21-064 BLOCKER: "Run exact identity/fingerprint lookup
// before planning from scratch. This is the entry point."). A match here
// is the strongest possible discovery signal -- the literal same task,
// byte-for-byte, was already attempted -- so callers should log resulting
// candidates under storage.RetrievalCandidateExactMatch. Open episodes are
// deliberately excluded: an unfinished episode has no terminal outcome yet
// to learn anything durable from.
func (repositories *Repositories) ListEpisodesByFingerprintHash(
	ctx context.Context,
	projectID domain.ProjectID,
	fingerprintHash string,
) ([]Episode, error) {
	switch {
	case projectID.IsZero():
		return nil, errors.New("project ID must not be empty")
	case len(fingerprintHash) != 64:
		return nil, errors.New("fingerprint hash must contain 64 characters")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		episodeSelect+`
		 WHERE project_id = ? AND fingerprint_hash = ? AND status = 'closed'
		 ORDER BY closed_at_unix_micros DESC`,
		projectID, fingerprintHash,
	)
	if err != nil {
		return nil, classify("list episodes by fingerprint hash", err)
	}
	defer rows.Close()
	var episodes []Episode
	for rows.Next() {
		episode, err := scanEpisode(rows, "scan episode by fingerprint hash")
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, episode)
	}
	return episodes, classify("list episodes by fingerprint hash", rows.Err())
}

// ListMemoryArtifactsSupportedByEpisode reads every memory-artifact
// identity that episodeID exposed or supported (the
// memory_artifact_supporting_episode link table from migration 000027),
// for M21-064's exact-identity discovery: a prior episode matching the
// task's exact fingerprint hash names which memory artifacts it produced or
// relied on, and those are exact-identity candidates for the current task.
func (repositories *Repositories) ListMemoryArtifactsSupportedByEpisode(
	ctx context.Context,
	episodeID domain.EpisodeID,
) ([]domain.MemoryArtifactID, error) {
	if episodeID.IsZero() {
		return nil, errors.New("episode ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT artifact_id FROM memory_artifact_supporting_episode
		 WHERE episode_id = ? ORDER BY recorded_at_unix_micros`,
		episodeID,
	)
	if err != nil {
		return nil, classify("list memory artifacts supported by episode", err)
	}
	defer rows.Close()
	var ids []domain.MemoryArtifactID
	for rows.Next() {
		var id domain.MemoryArtifactID
		if err := rows.Scan(&id); err != nil {
			return nil, classify("scan memory artifact supported by episode", err)
		}
		ids = append(ids, id)
	}
	return ids, classify("list memory artifacts supported by episode", rows.Err())
}

// MemoryArtifactIsDeleted reports whether one memory artifact has already
// been logically deleted (memory_artifacts.deleted_at_unix_micros is the
// only deletion mechanism; see memory_artifact_repository.go's
// DeleteMemoryArtifact). M21-064's exact-identity discovery reaches memory
// artifacts indirectly, through a prior episode's supported-artifact links
// rather than through GetLatestMemoryArtifactRevision's own project/kind
// filters; this check is what keeps that path from resurfacing a deleted
// artifact as a live candidate.
func (repositories *Repositories) MemoryArtifactIsDeleted(
	ctx context.Context,
	id domain.MemoryArtifactID,
) (bool, error) {
	if id.IsZero() {
		return false, errors.New("memory artifact ID must not be empty")
	}
	var deleted sql.NullInt64
	err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT deleted_at_unix_micros FROM memory_artifacts WHERE id = ?`, id,
	).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, typedError(ErrNotFound, "check memory artifact deletion", err)
	}
	if err != nil {
		return false, classify("check memory artifact deletion", err)
	}
	return deleted.Valid, nil
}
