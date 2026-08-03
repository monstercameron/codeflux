package storage

import (
	"context"
	"encoding/json"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

// ProjectLesson is one thing an earlier run in this project learned the hard
// way, ready to be put in front of the next one.
type ProjectLesson struct {
	Statement string
	Revision  domain.MemoryArtifactRevisionID
}

// ListProjectLessons reads the live observation-hypothesis facts a project
// holds, newest first.
//
// This is the read half of the loop §31 describes and nothing performed. The
// write half existed — UpsertExtractedMemoryFact has worked since M21 — and
// the only production caller extracted build commands from an accepted review,
// which no run reaches, so every memory table in every run of this session was
// empty. Two different ladder rungs independently invented the same deadlock
// because nothing carried the first one's lesson to the second.
//
// Only live maturities are returned. A retired or invalidated fact is one the
// project decided against, and putting it back in front of a run would undo
// that decision silently.
func (repositories *Repositories) ListProjectLessons(
	ctx context.Context,
	projectID domain.ProjectID,
	limit int,
) ([]ProjectLesson, error) {
	if projectID.IsZero() {
		return nil, errors.New("project ID must not be empty")
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := repositories.database.sql.QueryContext(ctx,
		`SELECT revision.id, revision.content_json
		   FROM memory_artifact_revisions AS revision
		   JOIN memory_artifacts AS artifact ON artifact.id = revision.artifact_id
		  WHERE artifact.project_id = ?
		    AND revision.kind = ?
		    AND revision.maturity IN ('candidate', 'validated', 'preferred-for-experiment')
		    AND NOT EXISTS (
		          SELECT 1 FROM memory_artifact_revisions AS newer
		           WHERE newer.supersedes_revision_id = revision.id
		        )
		  ORDER BY revision.revision_number DESC, revision.id DESC
		  LIMIT ?`,
		projectID.String(),
		string(domain.MemoryArtifactKindObservationHypothesis),
		limit,
	)
	if err != nil {
		return nil, classify("list project lessons", err)
	}
	defer func() { _ = rows.Close() }()

	var lessons []ProjectLesson
	for rows.Next() {
		var revisionID, contentJSON string
		if err := rows.Scan(&revisionID, &contentJSON); err != nil {
			return nil, classify("scan project lesson", err)
		}
		var content domain.MemoryArtifactContent
		if err := json.Unmarshal([]byte(contentJSON), &content); err != nil {
			// A row this cannot read is skipped rather than failing the read.
			// Lessons are advisory: losing one costs the next run a hint, and
			// refusing them all would cost it every hint over one bad row.
			continue
		}
		if content.ObservationHypothesis == nil {
			continue
		}
		parsed, err := domain.ParseMemoryArtifactRevisionID(revisionID)
		if err != nil {
			continue
		}
		lessons = append(lessons, ProjectLesson{
			Statement: content.ObservationHypothesis.Statement,
			Revision:  parsed,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, classify("read project lessons", err)
	}
	return lessons, nil
}
