// Package storage: memory artifact influence lineage. derived_from
// (M21-019) is semantic dependency; influenced_by (M21-020) is contextual
// exposure. See docs/plan.md §31 "Influence Lineage" and
// "Descendant Contamination", and internal/domain/memory.go's
// MemoryArtifactLineage, EvidenceFamily, and PreviewMemoryArtifactDeletion.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
)

// memoryArtifactLineageOriginUnknownReason and
// memoryArtifactLineageRootsUnknownReason explain why this storage lane
// never populates domain.MemoryArtifactLineage's OriginArtifactID/
// LineageRootIDs today: M21-019/M21-020 persist only direct
// derived_from/influenced_by edges, and computing a global origin or
// lineage-root set would mean walking (and therefore loading) an
// unbounded ancestor closure on every lineage read, which AGENTS.md's
// "Avoid unbounded ... graph queries" forbids doing implicitly on a plain
// read path. Per the package-wide Known/UnknownReason rule
// (internal/domain/memory.go's MemoryArtifactLineage doc comment), this is
// recorded as an honest "not known" rather than a bare empty value that a
// caller could mistake for "known: no origin/roots".
const (
	memoryArtifactLineageOriginUnknownReason = "origin is not computed by this storage lane; only direct derived_from/influenced_by edges are persisted (M21-019/M21-020)"
	memoryArtifactLineageRootsUnknownReason  = "lineage roots are not computed by this storage lane; only direct derived_from/influenced_by edges are persisted (M21-019/M21-020)"
)

// memoryLineageQueryer is satisfied by both *sql.DB and *sql.Tx.
type memoryLineageQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// newMemoryArtifactLineageStub returns a fresh, otherwise-empty
// domain.MemoryArtifactLineage for id with Origin/LineageRoots explicitly
// marked unknown, so every lineage-index builder in this file produces a
// value domain.MemoryArtifactLineage.Validate() accepts (an unset
// Known/UnknownReason pair, rather than a bare zero value, is ambiguous
// between "known: no origin" and "not determined yet").
func newMemoryArtifactLineageStub(id domain.MemoryArtifactID) domain.MemoryArtifactLineage {
	return domain.MemoryArtifactLineage{
		ArtifactID:                id,
		OriginKnown:               false,
		OriginUnknownReason:       memoryArtifactLineageOriginUnknownReason,
		LineageRootsKnown:         false,
		LineageRootsUnknownReason: memoryArtifactLineageRootsUnknownReason,
	}
}

// RecordMemoryArtifactDerivedFrom declares one semantic-dependency lineage
// edge: artifactID mechanically depends on ancestorID's claim.
func (repositories *Repositories) RecordMemoryArtifactDerivedFrom(
	ctx context.Context,
	artifactID domain.MemoryArtifactID,
	ancestorID domain.MemoryArtifactID,
) error {
	return repositories.recordMemoryArtifactLineageEdge(ctx, "memory_artifact_derived_from", artifactID, ancestorID)
}

// RecordMemoryArtifactInfluencedBy declares one contextual-exposure
// lineage edge: ancestorID entered the context that produced artifactID
// without artifactID mechanically depending on it.
func (repositories *Repositories) RecordMemoryArtifactInfluencedBy(
	ctx context.Context,
	artifactID domain.MemoryArtifactID,
	ancestorID domain.MemoryArtifactID,
) error {
	return repositories.recordMemoryArtifactLineageEdge(ctx, "memory_artifact_influenced_by", artifactID, ancestorID)
}

func (repositories *Repositories) recordMemoryArtifactLineageEdge(
	ctx context.Context,
	table string,
	artifactID domain.MemoryArtifactID,
	ancestorID domain.MemoryArtifactID,
) error {
	switch {
	case artifactID.IsZero():
		return errors.New("memory artifact ID must not be empty")
	case ancestorID.IsZero():
		return errors.New("memory artifact ancestor ID must not be empty")
	case artifactID == ancestorID:
		return errors.New("memory artifact cannot be its own lineage ancestor")
	}
	_, micros := repositories.timestamp()
	return repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT OR IGNORE INTO `+table+` (artifact_id, ancestor_artifact_id, recorded_at_unix_micros)
			 VALUES (?, ?, ?)`,
			artifactID, ancestorID, micros,
		)
		return repositoryWriteError("record memory artifact lineage edge", err)
	})
}

// GetMemoryArtifactLineage reads one artifact's direct derived_from and
// influenced_by ancestors, matching domain.MemoryArtifactLineage's shape.
// SupportingEpisodes and LineageRootIDs are out of this lane's scope: the
// former needs the M21-029..039 episodes table, the latter is derivable at
// query time from the DerivedFrom/InfluencedBy closure.
func (repositories *Repositories) GetMemoryArtifactLineage(
	ctx context.Context,
	artifactID domain.MemoryArtifactID,
) (domain.MemoryArtifactLineage, error) {
	if artifactID.IsZero() {
		return domain.MemoryArtifactLineage{}, errors.New("memory artifact ID must not be empty")
	}
	derivedFrom, err := listMemoryArtifactLineageAncestors(ctx, repositories.database.sql, "memory_artifact_derived_from", artifactID)
	if err != nil {
		return domain.MemoryArtifactLineage{}, err
	}
	influencedBy, err := listMemoryArtifactLineageAncestors(ctx, repositories.database.sql, "memory_artifact_influenced_by", artifactID)
	if err != nil {
		return domain.MemoryArtifactLineage{}, err
	}
	lineage := newMemoryArtifactLineageStub(artifactID)
	lineage.DerivedFrom = derivedFrom
	lineage.InfluencedBy = influencedBy
	return lineage, nil
}

func listMemoryArtifactLineageAncestors(
	ctx context.Context,
	queries memoryLineageQueryer,
	table string,
	artifactID domain.MemoryArtifactID,
) ([]domain.MemoryArtifactID, error) {
	rows, err := queries.QueryContext(
		ctx,
		`SELECT ancestor_artifact_id FROM `+table+` WHERE artifact_id = ? ORDER BY ancestor_artifact_id`,
		artifactID,
	)
	if err != nil {
		return nil, classify("list memory artifact lineage ancestors", err)
	}
	defer rows.Close()
	var ancestors []domain.MemoryArtifactID
	for rows.Next() {
		var ancestor domain.MemoryArtifactID
		if err := rows.Scan(&ancestor); err != nil {
			return nil, classify("scan memory artifact lineage ancestor", err)
		}
		ancestors = append(ancestors, ancestor)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list memory artifact lineage ancestors", err)
	}
	return ancestors, nil
}

// loadMemoryArtifactLineageIndex loads every derived_from/influenced_by
// edge whose descendant belongs to project, keyed by descendant artifact
// ID, in the shape domain.PreviewMemoryArtifactDeletion and
// domain.ConfirmsMemoryArtifactIndependently expect. Edges never cross a
// project boundary (enforced by migration triggers), so restricting to one
// project's descendants is sufficient for any traversal rooted in that
// project.
func loadMemoryArtifactLineageIndex(
	ctx context.Context,
	queries memoryLineageQueryer,
	projectID domain.ProjectID,
) (map[domain.MemoryArtifactID]domain.MemoryArtifactLineage, error) {
	index := map[domain.MemoryArtifactID]domain.MemoryArtifactLineage{}
	if err := populateMemoryArtifactLineageIndex(ctx, queries, projectID, "memory_artifact_derived_from", index, true); err != nil {
		return nil, err
	}
	if err := populateMemoryArtifactLineageIndex(ctx, queries, projectID, "memory_artifact_influenced_by", index, false); err != nil {
		return nil, err
	}
	return index, nil
}

// maximumMemoryArtifactLineageDeletionTraversal bounds how many artifacts
// one deletion's reachable-subgraph lineage load may visit walking
// target's transitive derived_from descendants, per AGENTS.md "Avoid
// unbounded ... goroutines, queues, output buffers, retries, graph
// queries, and database reads." Reproduced defect: DeleteMemoryArtifact
// used to call loadMemoryArtifactLineageIndex(ctx, tx, target.ProjectID),
// which loads every derived_from/influenced_by edge for the ENTIRE
// project on every call, with no LIMIT and no relationship to whether
// those artifacts were reachable from target at all.
const maximumMemoryArtifactLineageDeletionTraversal = 2000

// ErrMemoryArtifactLineageTooLarge classifies a deletion whose target's
// reachable derived_from/influenced_by subgraph exceeds
// maximumMemoryArtifactLineageDeletionTraversal artifacts. Deletion fails
// loudly with this typed error rather than silently truncating the
// preview or cascade.
var ErrMemoryArtifactLineageTooLarge = errors.New("memory artifact lineage traversal exceeds the bounded read limit")

// memoryArtifactDerivedFromDescendantsCTE computes target and every
// artifact transitively reachable by repeatedly following "X derived_from
// Y" edges from Y=target outward to X (i.e. target's descendants),
// matching what cascadeMemoryArtifactDerivedFrom walks and what
// domain.PreviewMemoryArtifactDeletion's transitive dependent computation
// needs. SQLite's recursive CTE uses UNION (not UNION ALL), so duplicate
// rows are dropped as they are produced; this is what keeps the query
// cycle-safe (a derived_from cycle stops growing once every reachable
// distinct artifact has been found, the same guarantee
// domain.PreviewMemoryArtifactDeletion documents for its own visited-set
// walk).
const memoryArtifactDerivedFromDescendantsCTE = `WITH RECURSIVE descendants(id) AS (
	SELECT ?
	UNION
	SELECT d.artifact_id
	FROM memory_artifact_derived_from AS d
	JOIN descendants ON d.ancestor_artifact_id = descendants.id
)`

// loadMemoryArtifactLineageIndexForDeletion loads only the lineage
// DeleteMemoryArtifact actually needs to preview and cascade a deletion
// rooted at target: target's full transitive derived_from descendant
// closure, plus every artifact directly influenced_by target (one hop,
// matching MemoryArtifactDeletionPreview.ContextuallyInfluenced's
// direct-only semantics). Unlike loadMemoryArtifactLineageIndex, it never
// reads an edge belonging to an artifact outside that reachable subgraph
// merely because the artifact shares target's project_id, so the read
// stays proportional to what the deletion actually touches rather than to
// total project size; loadMemoryArtifactLineageIndex remains the correct
// choice for callers (such as domain.ConfirmsMemoryArtifactIndependently
// via EvidenceFamily) that genuinely need the whole project's lineage
// graph.
//
// The returned index is a narrower view than domain.MemoryArtifactLineage's
// full contract: entries added only because they are influenced_by
// target carry just that one InfluencedBy edge, not their complete
// ancestry. That is exactly what
// domain.PreviewMemoryArtifactDeletion/ValidateMemoryArtifactDeletion read
// from this index (dependentsOf construction over DerivedFrom, plus a
// direct InfluencedBy-contains-target check), so it is safe for this
// caller; it is not safe to reuse for a caller needing full lineage.
func loadMemoryArtifactLineageIndexForDeletion(
	ctx context.Context,
	queries memoryLineageQueryer,
	target domain.MemoryArtifactID,
) (map[domain.MemoryArtifactID]domain.MemoryArtifactLineage, error) {
	return loadMemoryArtifactLineageIndexForDeletionBounded(ctx, queries, target, maximumMemoryArtifactLineageDeletionTraversal)
}

// loadMemoryArtifactLineageIndexForDeletionBounded is
// loadMemoryArtifactLineageIndexForDeletion with an explicit cap, split
// out so tests can exercise the exceeded-cap error path with a small,
// fast fixture instead of constructing thousands of artifacts.
func loadMemoryArtifactLineageIndexForDeletionBounded(
	ctx context.Context,
	queries memoryLineageQueryer,
	target domain.MemoryArtifactID,
	cap int,
) (map[domain.MemoryArtifactID]domain.MemoryArtifactLineage, error) {
	if target.IsZero() {
		return nil, errors.New("memory artifact ID must not be empty")
	}
	descendantRows, err := queries.QueryContext(
		ctx,
		memoryArtifactDerivedFromDescendantsCTE+` SELECT id FROM descendants LIMIT ?`,
		target, cap+1,
	)
	if err != nil {
		return nil, classify("load memory artifact derived_from descendant closure", err)
	}
	var descendants []domain.MemoryArtifactID
	for descendantRows.Next() {
		var id domain.MemoryArtifactID
		if err := descendantRows.Scan(&id); err != nil {
			descendantRows.Close()
			return nil, classify("scan memory artifact derived_from descendant closure", err)
		}
		descendants = append(descendants, id)
	}
	rowsErr := descendantRows.Err()
	closeErr := descendantRows.Close()
	if rowsErr != nil {
		return nil, classify("load memory artifact derived_from descendant closure", rowsErr)
	}
	if closeErr != nil {
		return nil, classify("load memory artifact derived_from descendant closure", closeErr)
	}
	if len(descendants) > cap {
		return nil, fmt.Errorf(
			"%w: target %s's derived_from descendant closure reaches at least %d artifacts (bound %d)",
			ErrMemoryArtifactLineageTooLarge, target, len(descendants), cap,
		)
	}

	index := map[domain.MemoryArtifactID]domain.MemoryArtifactLineage{}
	for _, id := range descendants {
		index[id] = newMemoryArtifactLineageStub(id)
	}

	derivedRows, err := queries.QueryContext(
		ctx,
		memoryArtifactDerivedFromDescendantsCTE+`
		 SELECT memory_artifact_derived_from.artifact_id, memory_artifact_derived_from.ancestor_artifact_id
		 FROM memory_artifact_derived_from
		 JOIN descendants ON memory_artifact_derived_from.artifact_id = descendants.id`,
		target,
	)
	if err != nil {
		return nil, classify("load memory artifact derived_from edges for deletion", err)
	}
	if err := scanMemoryArtifactLineageEdgesInto(derivedRows, index, true); err != nil {
		return nil, err
	}

	influencedRows, err := queries.QueryContext(
		ctx,
		memoryArtifactDerivedFromDescendantsCTE+`
		 SELECT memory_artifact_influenced_by.artifact_id, memory_artifact_influenced_by.ancestor_artifact_id
		 FROM memory_artifact_influenced_by
		 JOIN descendants ON memory_artifact_influenced_by.artifact_id = descendants.id`,
		target,
	)
	if err != nil {
		return nil, classify("load memory artifact influenced_by edges for deletion", err)
	}
	if err := scanMemoryArtifactLineageEdgesInto(influencedRows, index, false); err != nil {
		return nil, err
	}

	// Artifacts directly influenced_by target are not necessarily
	// derived_from descendants, so they need their own bounded lookup to
	// appear in the index at all (PreviewMemoryArtifactDeletion's
	// ContextuallyInfluenced scan only inspects artifacts already present
	// in index).
	influencedChildRows, err := queries.QueryContext(
		ctx,
		`SELECT artifact_id, ancestor_artifact_id FROM memory_artifact_influenced_by
		 WHERE ancestor_artifact_id = ? LIMIT ?`,
		target, cap+1,
	)
	if err != nil {
		return nil, classify("load memory artifact influenced_by children for deletion", err)
	}
	if err := scanMemoryArtifactLineageEdgesInto(influencedChildRows, index, false); err != nil {
		return nil, err
	}

	if len(index) > cap {
		return nil, fmt.Errorf(
			"%w: target %s's reachable lineage subgraph reaches at least %d artifacts (bound %d)",
			ErrMemoryArtifactLineageTooLarge, target, len(index), cap,
		)
	}
	return index, nil
}

// scanMemoryArtifactLineageEdgesInto scans (artifact_id, ancestor_artifact_id)
// rows and folds them into index as either a DerivedFrom or InfluencedBy
// edge, creating the descendant's entry if it is not already present. It
// always closes rows.
func scanMemoryArtifactLineageEdgesInto(
	rows *sql.Rows,
	index map[domain.MemoryArtifactID]domain.MemoryArtifactLineage,
	derived bool,
) error {
	defer rows.Close()
	for rows.Next() {
		var artifactID, ancestorID domain.MemoryArtifactID
		if err := rows.Scan(&artifactID, &ancestorID); err != nil {
			return classify("scan memory artifact lineage edge for deletion", err)
		}
		entry, exists := index[artifactID]
		if !exists {
			entry = newMemoryArtifactLineageStub(artifactID)
		}
		if derived {
			entry.DerivedFrom = append(entry.DerivedFrom, ancestorID)
		} else {
			entry.InfluencedBy = append(entry.InfluencedBy, ancestorID)
		}
		index[artifactID] = entry
	}
	return classify("load memory artifact lineage edges for deletion", rows.Err())
}

func populateMemoryArtifactLineageIndex(
	ctx context.Context,
	queries memoryLineageQueryer,
	projectID domain.ProjectID,
	table string,
	index map[domain.MemoryArtifactID]domain.MemoryArtifactLineage,
	derived bool,
) error {
	rows, err := queries.QueryContext(
		ctx,
		`SELECT artifact_id, ancestor_artifact_id FROM `+table+`
		 WHERE artifact_id IN (SELECT id FROM memory_artifacts WHERE project_id = ?)`,
		projectID,
	)
	if err != nil {
		return classify("load memory artifact lineage index", err)
	}
	defer rows.Close()
	for rows.Next() {
		var artifactID, ancestorID domain.MemoryArtifactID
		if err := rows.Scan(&artifactID, &ancestorID); err != nil {
			return classify("scan memory artifact lineage index", err)
		}
		entry, exists := index[artifactID]
		if !exists {
			entry = newMemoryArtifactLineageStub(artifactID)
		}
		if derived {
			entry.DerivedFrom = append(entry.DerivedFrom, ancestorID)
		} else {
			entry.InfluencedBy = append(entry.InfluencedBy, ancestorID)
		}
		index[artifactID] = entry
	}
	return classify("load memory artifact lineage index", rows.Err())
}
