// Package storage: governed memory-artifact maturity transitions
// (M21-016), authorized only through domain.ValidateMemoryArtifactMaturityTransition
// and domain.AuthorizeMemoryArtifactMaturityGrant (M21-011), and the
// invalidation/quarantine record every transition into quarantined,
// invalidated, or retired carries (M21-021). See docs/plan.md §31
// "Artifact Failure Protocol": "Quarantine is terminal for the exposed
// lesson version."
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
)

// ErrMemoryArtifactCounterexampleCrossProject classifies an attempt to log
// a maturity transition whose CounterexampleEvidenceID belongs to a
// different project than the transitioning revision's owning artifact.
// Reproduced defect: a memory_artifact_maturity_transitions row for a
// Project-A revision previously accepted a counterexample_evidence_id
// pointing at Project-B's evidence with no error, unlike every other table
// reaching a memory artifact, which enforces its project boundary with a
// migration trigger (AGENTS.md "Add explicit project-boundary predicates
// to memory, graph, vector, and retrieval queries"). This is the
// application-layer half of the fix; the migration's
// memory_artifact_maturity_transitions_counterexample_project_boundary
// trigger is the storage-layer defense-in-depth mirror.
var ErrMemoryArtifactCounterexampleCrossProject = errors.New("memory artifact maturity transition counterexample evidence crosses the owning project boundary")

// MemoryArtifactInvalidationReasonKind classifies why a maturity
// transition moved a revision into quarantined, invalidated, or retired,
// mirroring the §31 Artifact Failure Protocol consequences.
type MemoryArtifactInvalidationReasonKind string

const (
	MemoryArtifactInvalidationReasonLessonArmWorse          MemoryArtifactInvalidationReasonKind = "lesson-arm-worse"
	MemoryArtifactInvalidationReasonBothArmsBad             MemoryArtifactInvalidationReasonKind = "both-arms-bad"
	MemoryArtifactInvalidationReasonAdaptationFailed        MemoryArtifactInvalidationReasonKind = "adaptation-failed"
	MemoryArtifactInvalidationReasonBindingChanged          MemoryArtifactInvalidationReasonKind = "binding-changed"
	MemoryArtifactInvalidationReasonEvidenceAmbiguous       MemoryArtifactInvalidationReasonKind = "evidence-ambiguous"
	MemoryArtifactInvalidationReasonUserCorrection          MemoryArtifactInvalidationReasonKind = "user-correction"
	MemoryArtifactInvalidationReasonDeployedWorkflowFailure MemoryArtifactInvalidationReasonKind = "deployed-workflow-failure"
	MemoryArtifactInvalidationReasonOther                   MemoryArtifactInvalidationReasonKind = "other"
)

// TransitionMemoryArtifactMaturity declares one idempotent, authorized
// governed-maturity change. Reaching Validated or PreferredForExperiment
// requires already-recorded corroborated, non-self-report supporting
// evidence for RevisionID (M21-011); reaching Quarantined, Invalidated, or
// Retired requires ReasonKind and DetailRedacted (M21-021).
type TransitionMemoryArtifactMaturity struct {
	RevisionID               domain.MemoryArtifactRevisionID
	From                     domain.MaturityState
	To                       domain.MaturityState
	ReasonKind               MemoryArtifactInvalidationReasonKind
	DetailRedacted           string
	CounterexampleEvidenceID *domain.EvidenceID
	IdempotencyKey           string
}

// MemoryArtifactMaturityTransitionRecord is one immutable logged
// transition.
type MemoryArtifactMaturityTransitionRecord struct {
	ID                       string
	RevisionID               domain.MemoryArtifactRevisionID
	From                     domain.MaturityState
	To                       domain.MaturityState
	ReasonKind               MemoryArtifactInvalidationReasonKind
	DetailRedacted           string
	CounterexampleEvidenceID *domain.EvidenceID
	TransitionedAtMicros     int64
}

// TransitionMemoryArtifactMaturity validates the transition through
// domain.ValidateMemoryArtifactMaturityTransition (obtaining an
// authorityProof from already-recorded evidence when the destination is
// authority-bearing), then atomically logs the transition and mutates the
// revision's current maturity overlay. A retry with the same
// IdempotencyKey returns the original result; a retry with a different
// requested From/To for the same key is rejected.
func (repositories *Repositories) TransitionMemoryArtifactMaturity(
	ctx context.Context,
	input TransitionMemoryArtifactMaturity,
) (MemoryArtifactMaturityTransitionRecord, error) {
	if input.RevisionID.IsZero() {
		return MemoryArtifactMaturityTransitionRecord{}, errors.New("memory artifact revision ID must not be empty")
	}
	if err := validateBounded("memory artifact maturity transition idempotency key", input.IdempotencyKey, 255); err != nil {
		return MemoryArtifactMaturityTransitionRecord{}, err
	}
	if input.To.IsAuthorityBearing() {
		// Evidence proof is (re)validated below from durable state, inside
		// the transaction, so this pre-check only produces a clean error
		// early for callers outside a transaction.
	} else if input.To == domain.MaturityStateQuarantined || input.To == domain.MaturityStateInvalidated || input.To == domain.MaturityStateRetired {
		if input.ReasonKind == "" || !isValidMemoryArtifactInvalidationReason(input.ReasonKind) {
			return MemoryArtifactMaturityTransitionRecord{}, errors.New("memory artifact invalidation reason kind is not declared")
		}
		if err := validateBounded("memory artifact invalidation detail", input.DetailRedacted, 8192); err != nil {
			return MemoryArtifactMaturityTransitionRecord{}, err
		}
	}
	id := memoryMaturityTransitionID(input.RevisionID, input.IdempotencyKey)
	_, micros := repositories.timestamp()
	var record MemoryArtifactMaturityTransitionRecord
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if existing, found, err := findMemoryMaturityTransitionByIdempotency(ctx, transaction.sql, input.RevisionID, input.IdempotencyKey); err != nil {
			return err
		} else if found {
			if existing.From != input.From || existing.To != input.To {
				return typedError(ErrConflict, "transition memory artifact maturity", errors.New("idempotency key belongs to a different transition"))
			}
			record = existing
			return nil
		}
		if input.CounterexampleEvidenceID != nil {
			revisionProject, err := memoryArtifactRevisionProjectID(ctx, transaction.sql, input.RevisionID)
			if err != nil {
				return err
			}
			evidenceProject, err := memoryEvidenceProjectID(ctx, transaction.sql, *input.CounterexampleEvidenceID)
			if err != nil {
				return err
			}
			if revisionProject != evidenceProject {
				return typedError(ErrConstraint, "transition memory artifact maturity", fmt.Errorf(
					"%w: revision %s belongs to project %s, counterexample evidence %s belongs to project %s",
					ErrMemoryArtifactCounterexampleCrossProject, input.RevisionID, revisionProject,
					*input.CounterexampleEvidenceID, evidenceProject,
				))
			}
		}
		if input.To.IsAuthorityBearing() {
			// domain.AuthorizeMemoryArtifactMaturityGrant now owns the
			// self-report exclusion policy internally:
			// SupportingEvidenceRecord.Validate pins agent-self-report to
			// EvidenceStrengthNone (M21-011), so self-report can never
			// itself satisfy the corroborated-evidence requirement no
			// matter what else is in the recorded set. Pass the full
			// recorded evidence set rather than pre-filtering self-report
			// out here, so exactly one place (the domain function) decides
			// the policy, per §31 "Agent explanations are stored only as
			// agent_self_report with evidence_strength: none. They are not
			// treated as causal accounts" -- present, not poisoning.
			recorded, err := supportingEvidenceRecords(ctx, transaction.sql, input.RevisionID)
			if err != nil {
				return err
			}
			proof, err := domain.AuthorizeMemoryArtifactMaturityGrant(input.RevisionID, recorded)
			if err != nil {
				return err
			}
			if err := domain.ValidateMemoryArtifactMaturityTransition(domain.MaturityTransitionRequest{
				From: input.From, To: input.To, Revision: input.RevisionID, Proof: proof,
			}); err != nil {
				return err
			}
		} else {
			if err := domain.ValidateMemoryArtifactMaturityTransition(domain.MaturityTransitionRequest{
				From: input.From, To: input.To, Revision: input.RevisionID,
			}); err != nil {
				return err
			}
		}
		var reasonKind, detail any
		if input.ReasonKind != "" {
			reasonKind, detail = input.ReasonKind, input.DetailRedacted
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_artifact_maturity_transitions (
				id, revision_id, from_state, to_state, reason_kind, detail_redacted,
				counterexample_evidence_id, idempotency_key, transitioned_at_unix_micros
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, input.RevisionID, input.From, input.To, reasonKind, detail,
			nullableEvidenceID(input.CounterexampleEvidenceID), input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("log memory artifact maturity transition", err)
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE memory_artifact_revisions SET maturity = ? WHERE id = ? AND maturity = ?`,
			input.To, input.RevisionID, input.From,
		)
		if err != nil {
			return repositoryWriteError("apply memory artifact maturity transition", err)
		}
		if err := requireOneAffected(result, "apply memory artifact maturity transition"); err != nil {
			return err
		}
		record = MemoryArtifactMaturityTransitionRecord{
			ID: id, RevisionID: input.RevisionID, From: input.From, To: input.To,
			ReasonKind: input.ReasonKind, DetailRedacted: input.DetailRedacted,
			CounterexampleEvidenceID: input.CounterexampleEvidenceID, TransitionedAtMicros: micros,
		}
		return nil
	})
	if err != nil {
		return MemoryArtifactMaturityTransitionRecord{}, err
	}
	return record, nil
}

// ListMemoryArtifactMaturityTransitions reads the full immutable
// transition history for one revision, oldest first.
func (repositories *Repositories) ListMemoryArtifactMaturityTransitions(
	ctx context.Context,
	revisionID domain.MemoryArtifactRevisionID,
) ([]MemoryArtifactMaturityTransitionRecord, error) {
	if revisionID.IsZero() {
		return nil, errors.New("memory artifact revision ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		memoryMaturityTransitionSelect+` WHERE revision_id = ? ORDER BY transitioned_at_unix_micros`,
		revisionID,
	)
	if err != nil {
		return nil, classify("list memory artifact maturity transitions", err)
	}
	defer rows.Close()
	var records []MemoryArtifactMaturityTransitionRecord
	for rows.Next() {
		record, err := scanMemoryMaturityTransitionRow(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, classify("list memory artifact maturity transitions", rows.Err())
}

const memoryMaturityTransitionSelect = `SELECT
	id, revision_id, from_state, to_state, reason_kind, detail_redacted,
	counterexample_evidence_id, transitioned_at_unix_micros
 FROM memory_artifact_maturity_transitions`

func findMemoryMaturityTransitionByIdempotency(
	ctx context.Context,
	queries memoryLineageQueryer,
	revisionID domain.MemoryArtifactRevisionID,
	key string,
) (MemoryArtifactMaturityTransitionRecord, bool, error) {
	rows, err := queries.QueryContext(
		ctx,
		memoryMaturityTransitionSelect+` WHERE revision_id = ? AND idempotency_key = ?`,
		revisionID, key,
	)
	if err != nil {
		return MemoryArtifactMaturityTransitionRecord{}, false, classify("find memory artifact maturity transition", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return MemoryArtifactMaturityTransitionRecord{}, false, classify("find memory artifact maturity transition", rows.Err())
	}
	record, err := scanMemoryMaturityTransitionRow(rows)
	if err != nil {
		return MemoryArtifactMaturityTransitionRecord{}, false, err
	}
	return record, true, nil
}

func scanMemoryMaturityTransitionRow(row rowScanner) (MemoryArtifactMaturityTransitionRecord, error) {
	var (
		record                    MemoryArtifactMaturityTransitionRecord
		reasonKind, detail        sql.NullString
		counterexampleEvidenceRaw sql.NullString
	)
	if err := row.Scan(
		&record.ID, &record.RevisionID, &record.From, &record.To, &reasonKind, &detail,
		&counterexampleEvidenceRaw, &record.TransitionedAtMicros,
	); err != nil {
		return MemoryArtifactMaturityTransitionRecord{}, classify("scan memory artifact maturity transition", err)
	}
	record.ReasonKind = MemoryArtifactInvalidationReasonKind(reasonKind.String)
	record.DetailRedacted = detail.String
	if counterexampleEvidenceRaw.Valid {
		id, err := domain.ParseEvidenceID(counterexampleEvidenceRaw.String)
		if err != nil {
			return MemoryArtifactMaturityTransitionRecord{}, typedError(ErrCorrupt, "scan memory artifact maturity transition", err)
		}
		record.CounterexampleEvidenceID = &id
	}
	return record, nil
}

func nullableEvidenceID(id *domain.EvidenceID) any {
	if id == nil {
		return nil
	}
	return *id
}

// memoryArtifactRevisionProjectID looks up the owning project of the
// memory artifact that revisionID belongs to, for the Go-side half of the
// counterexample-evidence project-boundary check (defense in depth ahead
// of the migration trigger of the same name).
func memoryArtifactRevisionProjectID(
	ctx context.Context,
	queries queryRower,
	revisionID domain.MemoryArtifactRevisionID,
) (domain.ProjectID, error) {
	var projectID domain.ProjectID
	err := queries.QueryRowContext(
		ctx,
		`SELECT memory_artifacts.project_id
		 FROM memory_artifact_revisions
		 JOIN memory_artifacts ON memory_artifacts.id = memory_artifact_revisions.artifact_id
		 WHERE memory_artifact_revisions.id = ?`,
		revisionID,
	).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectID{}, typedError(ErrNotFound, "find memory artifact revision project", err)
	}
	if err != nil {
		return domain.ProjectID{}, classify("find memory artifact revision project", err)
	}
	return projectID, nil
}

// memoryEvidenceProjectID looks up the owning project of one evidence
// record, following the same evidence -> task -> thread -> project join
// the migration's memory_artifact_supporting_evidence_project_boundary
// trigger uses.
func memoryEvidenceProjectID(
	ctx context.Context,
	queries queryRower,
	evidenceID domain.EvidenceID,
) (domain.ProjectID, error) {
	var projectID domain.ProjectID
	err := queries.QueryRowContext(
		ctx,
		`SELECT thread.project_id
		 FROM evidence
		 JOIN tasks AS task ON task.id = evidence.task_id
		 JOIN threads AS thread ON thread.id = task.thread_id
		 WHERE evidence.id = ?`,
		evidenceID,
	).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectID{}, typedError(ErrNotFound, "find memory artifact counterexample evidence project", err)
	}
	if err != nil {
		return domain.ProjectID{}, classify("find memory artifact counterexample evidence project", err)
	}
	return projectID, nil
}

func isValidMemoryArtifactInvalidationReason(kind MemoryArtifactInvalidationReasonKind) bool {
	switch kind {
	case MemoryArtifactInvalidationReasonLessonArmWorse,
		MemoryArtifactInvalidationReasonBothArmsBad,
		MemoryArtifactInvalidationReasonAdaptationFailed,
		MemoryArtifactInvalidationReasonBindingChanged,
		MemoryArtifactInvalidationReasonEvidenceAmbiguous,
		MemoryArtifactInvalidationReasonUserCorrection,
		MemoryArtifactInvalidationReasonDeployedWorkflowFailure,
		MemoryArtifactInvalidationReasonOther:
		return true
	default:
		return false
	}
}

// memoryMaturityTransitionID derives a stable, content-hash-shaped primary
// key from the (revision, idempotency key) pair the transitions table
// already enforces uniqueness on, matching the id-shape convention used by
// other content-addressed identity columns in this schema (e.g.
// acceptance_decisions.id).
func memoryMaturityTransitionID(revisionID domain.MemoryArtifactRevisionID, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(revisionID.String() + "|" + idempotencyKey))
	return hex.EncodeToString(sum[:])
}
