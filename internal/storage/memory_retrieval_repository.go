// Package storage: retrieval-candidate and retrieval-decision logs
// (M21-026). Schema-backed logging only; the M21-064..077 retrieval gate
// decides what to write here. Per AGENTS.md, vector similarity never
// establishes eligibility by itself.
package storage

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

// MemoryRetrievalQueryKind classifies one retrieval query.
type MemoryRetrievalQueryKind string

const (
	RetrievalQueryExactIdentity       MemoryRetrievalQueryKind = "exact-identity"
	RetrievalQueryApplicabilityFilter MemoryRetrievalQueryKind = "applicability-filter"
	RetrievalQueryVectorSimilarity    MemoryRetrievalQueryKind = "vector-similarity"
)

// MemoryRetrievalCandidateSource classifies how a candidate entered a
// retrieval result set.
type MemoryRetrievalCandidateSource string

const (
	RetrievalCandidateExactMatch        MemoryRetrievalCandidateSource = "exact-match"
	RetrievalCandidateApplicabilityPass MemoryRetrievalCandidateSource = "applicability-pass"
	RetrievalCandidateVectorSimilarity  MemoryRetrievalCandidateSource = "vector-similarity"
)

// MemoryRetrievalDecisionReason classifies why one candidate was accepted
// or rejected.
type MemoryRetrievalDecisionReason string

const (
	RetrievalReasonProjectBoundaryMismatch      MemoryRetrievalDecisionReason = "project-boundary-mismatch"
	RetrievalReasonToolchainMismatch            MemoryRetrievalDecisionReason = "toolchain-mismatch"
	RetrievalReasonDependencyMismatch           MemoryRetrievalDecisionReason = "dependency-mismatch"
	RetrievalReasonInvalidatedEvidence          MemoryRetrievalDecisionReason = "invalidated-evidence"
	RetrievalReasonAssuranceBelowRequirement    MemoryRetrievalDecisionReason = "assurance-below-requirement"
	RetrievalReasonApplicabilityPredicateFailed MemoryRetrievalDecisionReason = "applicability-predicate-failed"
	RetrievalReasonEligibleAndUsed              MemoryRetrievalDecisionReason = "eligible-and-used"
	RetrievalReasonEligibleAndAdapted           MemoryRetrievalDecisionReason = "eligible-and-adapted"
	RetrievalReasonEligibleAndRejectedByAgent   MemoryRetrievalDecisionReason = "eligible-and-rejected-by-agent"
	RetrievalReasonNoEligibleItem               MemoryRetrievalDecisionReason = "no-eligible-item"
)

// CreateMemoryRetrievalQuery declares one executed retrieval query.
type CreateMemoryRetrievalQuery struct {
	ID                       string
	ProjectID                domain.ProjectID
	TaskID                   *domain.TaskID
	FingerprintSchemaVersion int
	QueryKind                MemoryRetrievalQueryKind
}

// CreateMemoryRetrievalCandidate declares one logged candidate.
type CreateMemoryRetrievalCandidate struct {
	ID              string
	QueryID         string
	RevisionID      domain.MemoryArtifactRevisionID
	Rank            int
	CandidateSource MemoryRetrievalCandidateSource
	SimilarityScore *float64
}

// CreateMemoryRetrievalDecision declares one logged accept/reject
// decision for a previously logged candidate.
type CreateMemoryRetrievalDecision struct {
	ID                   string
	CandidateID          string
	Decision             string
	ReasonKind           MemoryRetrievalDecisionReason
	ReasonDetailRedacted *string
}

// CreateMemoryRetrievalQuery persists one retrieval query.
func (repositories *Repositories) CreateMemoryRetrievalQuery(
	ctx context.Context,
	input CreateMemoryRetrievalQuery,
) error {
	switch {
	case input.ID == "":
		return errors.New("memory retrieval query ID must not be empty")
	case input.ProjectID.IsZero():
		return errors.New("memory retrieval query project ID must not be empty")
	case input.FingerprintSchemaVersion < 1:
		return errors.New("memory retrieval query fingerprint schema version must be at least 1")
	}
	_, micros := repositories.timestamp()
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_retrieval_queries (
				id, project_id, task_id, fingerprint_schema_version, query_kind, requested_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?)`,
			input.ID, input.ProjectID, nullableTaskID(input.TaskID), input.FingerprintSchemaVersion, input.QueryKind, micros,
		)
		return repositoryWriteError("create memory retrieval query", err)
	})
}

// CreateMemoryRetrievalCandidate persists one logged candidate. Per
// AGENTS.md, a vector-similarity candidate must always carry a similarity
// score and never implies eligibility by itself; a subsequent
// CreateMemoryRetrievalDecision row still governs whether it was used.
func (repositories *Repositories) CreateMemoryRetrievalCandidate(
	ctx context.Context,
	input CreateMemoryRetrievalCandidate,
) error {
	switch {
	case input.ID == "":
		return errors.New("memory retrieval candidate ID must not be empty")
	case input.QueryID == "":
		return errors.New("memory retrieval candidate query ID must not be empty")
	case input.RevisionID.IsZero():
		return errors.New("memory retrieval candidate revision ID must not be empty")
	case input.Rank < 1:
		return errors.New("memory retrieval candidate rank must be at least 1")
	}
	_, micros := repositories.timestamp()
	var score any
	if input.SimilarityScore != nil {
		score = *input.SimilarityScore
	}
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_retrieval_candidates (
				id, query_id, revision_id, rank, candidate_source, similarity_score, created_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.QueryID, input.RevisionID, input.Rank, input.CandidateSource, score, micros,
		)
		return repositoryWriteError("create memory retrieval candidate", err)
	})
}

// CreateMemoryRetrievalDecision persists one logged accept/reject decision
// for a candidate.
func (repositories *Repositories) CreateMemoryRetrievalDecision(
	ctx context.Context,
	input CreateMemoryRetrievalDecision,
) error {
	switch {
	case input.ID == "":
		return errors.New("memory retrieval decision ID must not be empty")
	case input.CandidateID == "":
		return errors.New("memory retrieval decision candidate ID must not be empty")
	case input.Decision != "accepted" && input.Decision != "rejected":
		return errors.New("memory retrieval decision must be accepted or rejected")
	}
	_, micros := repositories.timestamp()
	var detail any
	if input.ReasonDetailRedacted != nil {
		detail = *input.ReasonDetailRedacted
	}
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_retrieval_decisions (
				id, candidate_id, decision, reason_kind, reason_detail_redacted, decided_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?)`,
			input.ID, input.CandidateID, input.Decision, input.ReasonKind, detail, micros,
		)
		return repositoryWriteError("create memory retrieval decision", err)
	})
}

func nullableTaskID(id *domain.TaskID) any {
	if id == nil {
		return nil
	}
	return *id
}
