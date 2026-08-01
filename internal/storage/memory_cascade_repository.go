// Package storage: transitive invalidation cascade for memory artifacts
// (M21-G04, "changed support invalidates dependent facts and vectors
// transitively"). Single-artifact invalidation lives in
// memory_maturity_repository.go; this file walks the derived_from closure
// so a failed claim cannot leave authority-bearing descendants standing.
//
// docs/plan.md §31 "Descendant Contamination" fixes the semantics:
//
//	depends_on(artifact)    = semantic dependency on the failed claim
//	influenced_by(artifact) = contextual exposure without a mechanical dependency
//
// "Direct semantic dependents are quarantined automatically. Contextually
// influenced descendants are flagged, excluded from independent
// confirmation, and required to verify their own claims, but are not
// automatically invalidated." The same section caps automatic bulk action:
// past five percent of active artifacts, "permanent bulk invalidation
// requires assurance-owner review".
package storage

import (
	"context"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
)

// memoryArtifactCascadeReviewThresholdPercent is §31's five-percent bound.
// Reaching it does not stop the quarantine — quarantine and assurance
// downgrade "still occur" — it records that permanent bulk invalidation
// needs an assurance owner to review before going further.
const memoryArtifactCascadeReviewThresholdPercent = 5

// CascadeMemoryArtifactInvalidation requests the transitive quarantine of
// every derived_from descendant of an artifact whose support just changed.
type CascadeMemoryArtifactInvalidation struct {
	ProjectID            domain.ProjectID
	OriginArtifactID     domain.MemoryArtifactID
	ReasonKind           MemoryArtifactInvalidationReasonKind
	DetailRedacted       string
	IdempotencyKeyPrefix string
}

// MemoryArtifactCascadeOutcome reports what the cascade actually did.
// FlaggedInfluenced is deliberately separate from QuarantinedRevisions:
// contextual exposure is recorded, never auto-invalidated.
type MemoryArtifactCascadeOutcome struct {
	QuarantinedRevisions         []domain.MemoryArtifactRevisionID
	QuarantinedArtifacts         []domain.MemoryArtifactID
	AlreadyTerminal              []domain.MemoryArtifactID
	FlaggedInfluenced            []domain.MemoryArtifactID
	ActiveArtifactCount          int
	RequiresAssuranceOwnerReview bool
}

// CascadeMemoryArtifactInvalidationTransitively quarantines every
// transitive derived_from descendant of OriginArtifactID that can still
// reach authority, and reports (without touching) the influenced_by
// descendants of the affected set.
//
// The origin artifact itself is not transitioned here; callers invalidate
// it through TransitionMemoryArtifactMaturity and then cascade, so the
// origin's own reason and evidence stay under the caller's control.
//
// Traversal reuses memoryArtifactDerivedFromDescendantsCTE and the same
// bound as deletion, so a runaway lineage fails loudly with
// ErrMemoryArtifactLineageTooLarge rather than silently truncating.
func (repositories *Repositories) CascadeMemoryArtifactInvalidationTransitively(
	ctx context.Context,
	input CascadeMemoryArtifactInvalidation,
) (MemoryArtifactCascadeOutcome, error) {
	return repositories.cascadeMemoryArtifactInvalidationBounded(
		ctx, input, maximumMemoryArtifactLineageDeletionTraversal,
	)
}

// cascadeMemoryArtifactInvalidationBounded is the cascade with an explicit
// cap so tests can exercise the exceeded-bound path without building
// thousands of artifacts.
func (repositories *Repositories) cascadeMemoryArtifactInvalidationBounded(
	ctx context.Context,
	input CascadeMemoryArtifactInvalidation,
	bound int,
) (MemoryArtifactCascadeOutcome, error) {
	if input.ProjectID.IsZero() {
		return MemoryArtifactCascadeOutcome{}, errors.New("project ID must not be empty")
	}
	if input.OriginArtifactID.IsZero() {
		return MemoryArtifactCascadeOutcome{}, errors.New("origin memory artifact ID must not be empty")
	}
	if !isValidMemoryArtifactInvalidationReason(input.ReasonKind) {
		return MemoryArtifactCascadeOutcome{}, errors.New("memory artifact invalidation reason kind is not declared")
	}
	// Both fields are optional: an empty detail takes the derived default
	// below, and an empty prefix takes the "cascade:" default. Only a
	// supplied value is bounds-checked.
	if input.DetailRedacted != "" {
		if err := validateBounded("memory artifact cascade detail", input.DetailRedacted, 8192); err != nil {
			return MemoryArtifactCascadeOutcome{}, err
		}
	}
	if input.IdempotencyKeyPrefix != "" {
		if err := validateBounded("memory artifact cascade idempotency key prefix", input.IdempotencyKeyPrefix, 200); err != nil {
			return MemoryArtifactCascadeOutcome{}, err
		}
	}

	descendants, err := loadMemoryArtifactDerivedFromDescendants(
		ctx, repositories.database.sql, input.OriginArtifactID, bound,
	)
	if err != nil {
		return MemoryArtifactCascadeOutcome{}, err
	}

	outcome := MemoryArtifactCascadeOutcome{}
	outcome.ActiveArtifactCount, err = countActiveMemoryArtifacts(ctx, repositories.database.sql, input.ProjectID)
	if err != nil {
		return MemoryArtifactCascadeOutcome{}, err
	}

	affected := make([]domain.MemoryArtifactID, 0, len(descendants)+1)
	affected = append(affected, input.OriginArtifactID)

	for _, descendant := range descendants {
		if descendant == input.OriginArtifactID {
			continue
		}
		affected = append(affected, descendant)

		deleted, err := repositories.MemoryArtifactIsDeleted(ctx, descendant)
		if err != nil {
			return MemoryArtifactCascadeOutcome{}, err
		}
		if deleted {
			continue
		}

		revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, descendant)
		if err != nil {
			return MemoryArtifactCascadeOutcome{}, err
		}
		if !revision.Maturity.CanReachAuthority() {
			// Already quarantined, invalidated, or retired. §31 makes
			// quarantine terminal, so re-transitioning would be both
			// invalid and misleading.
			outcome.AlreadyTerminal = append(outcome.AlreadyTerminal, descendant)
			continue
		}

		detail := input.DetailRedacted
		if detail == "" {
			detail = fmt.Sprintf(
				"Quarantined by transitive derived_from cascade from origin artifact %s.",
				input.OriginArtifactID,
			)
		}
		if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
			RevisionID:     revision.RevisionID,
			From:           revision.Maturity,
			To:             domain.MaturityStateQuarantined,
			ReasonKind:     input.ReasonKind,
			DetailRedacted: detail,
			IdempotencyKey: cascadeIdempotencyKey(input.IdempotencyKeyPrefix, descendant),
		}); err != nil {
			return MemoryArtifactCascadeOutcome{}, err
		}
		outcome.QuarantinedRevisions = append(outcome.QuarantinedRevisions, revision.RevisionID)
		outcome.QuarantinedArtifacts = append(outcome.QuarantinedArtifacts, descendant)
	}

	outcome.FlaggedInfluenced, err = listMemoryArtifactInfluencedDependents(
		ctx, repositories.database.sql, affected,
	)
	if err != nil {
		return MemoryArtifactCascadeOutcome{}, err
	}

	outcome.RequiresAssuranceOwnerReview = cascadeExceedsReviewThreshold(
		len(outcome.QuarantinedArtifacts), outcome.ActiveArtifactCount,
	)
	return outcome, nil
}

// cascadeExceedsReviewThreshold reports §31's five-percent bulk bound.
func cascadeExceedsReviewThreshold(quarantined int, active int) bool {
	if quarantined == 0 || active <= 0 {
		return false
	}
	return quarantined*100 > active*memoryArtifactCascadeReviewThresholdPercent
}

// cascadeIdempotencyKey derives a per-descendant key so a retried cascade
// re-uses each descendant's original transition instead of failing.
func cascadeIdempotencyKey(prefix string, descendant domain.MemoryArtifactID) string {
	if prefix == "" {
		return "cascade:" + descendant.String()
	}
	return prefix + ":" + descendant.String()
}

// loadMemoryArtifactDerivedFromDescendants returns the bounded transitive
// derived_from closure of target, including target itself.
func loadMemoryArtifactDerivedFromDescendants(
	ctx context.Context,
	queries memoryLineageQueryer,
	target domain.MemoryArtifactID,
	bound int,
) ([]domain.MemoryArtifactID, error) {
	rows, err := queries.QueryContext(
		ctx,
		memoryArtifactDerivedFromDescendantsCTE+` SELECT id FROM descendants LIMIT ?`,
		target, bound+1,
	)
	if err != nil {
		return nil, classify("load memory artifact cascade descendants", err)
	}
	var descendants []domain.MemoryArtifactID
	for rows.Next() {
		var id domain.MemoryArtifactID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, classify("scan memory artifact cascade descendants", err)
		}
		descendants = append(descendants, id)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		return nil, classify("load memory artifact cascade descendants", rowsErr)
	}
	if closeErr != nil {
		return nil, classify("load memory artifact cascade descendants", closeErr)
	}
	if len(descendants) > bound {
		return nil, fmt.Errorf(
			"%w: origin %s's derived_from descendant closure reaches at least %d artifacts (bound %d)",
			ErrMemoryArtifactLineageTooLarge, target, len(descendants), bound,
		)
	}
	return descendants, nil
}

// countActiveMemoryArtifacts counts non-deleted artifacts in one project,
// the denominator for §31's five-percent bulk-invalidation bound.
func countActiveMemoryArtifacts(
	ctx context.Context,
	queries memoryLineageQueryer,
	projectID domain.ProjectID,
) (int, error) {
	rows, err := queries.QueryContext(
		ctx,
		`SELECT COUNT(*) FROM memory_artifacts WHERE project_id = ? AND deleted_at_unix_micros IS NULL`,
		projectID,
	)
	if err != nil {
		return 0, classify("count active memory artifacts", err)
	}
	defer rows.Close()
	count := 0
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, classify("scan active memory artifact count", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, classify("count active memory artifacts", err)
	}
	return count, nil
}

// listMemoryArtifactInfluencedDependents returns artifacts contextually
// exposed to any affected artifact. Per §31 these are flagged and excluded
// from independent confirmation; they are never auto-invalidated here.
func listMemoryArtifactInfluencedDependents(
	ctx context.Context,
	queries memoryLineageQueryer,
	affected []domain.MemoryArtifactID,
) ([]domain.MemoryArtifactID, error) {
	seen := map[domain.MemoryArtifactID]bool{}
	for _, ancestor := range affected {
		seen[ancestor] = true
	}
	var flagged []domain.MemoryArtifactID
	for _, ancestor := range affected {
		rows, err := queries.QueryContext(
			ctx,
			`SELECT artifact_id FROM memory_artifact_influenced_by WHERE ancestor_artifact_id = ?`,
			ancestor,
		)
		if err != nil {
			return nil, classify("list influenced memory artifact dependents", err)
		}
		for rows.Next() {
			var id domain.MemoryArtifactID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, classify("scan influenced memory artifact dependents", err)
			}
			if !seen[id] {
				seen[id] = true
				flagged = append(flagged, id)
			}
		}
		rowsErr := rows.Err()
		closeErr := rows.Close()
		if rowsErr != nil {
			return nil, classify("list influenced memory artifact dependents", rowsErr)
		}
		if closeErr != nil {
			return nil, classify("list influenced memory artifact dependents", closeErr)
		}
	}
	return flagged, nil
}
