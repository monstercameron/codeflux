package storage

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

const maxFrontendRouteAccessIdentities = 10_000

// FrontendRouteAccess is a bounded identifier-only snapshot used to validate a
// browser-stored route. Canonical paths and thread content are deliberately
// excluded.
type FrontendRouteAccess struct {
	FirstRunComplete       bool
	SelectedWorkspaceID    domain.WorkspaceID
	AccessibleRepositories []domain.RepositoryID
	AccessibleThreads      []domain.ThreadID
	ArchivedThreads        []domain.ThreadID
}

// ReadFrontendRouteAccess returns a single-statement SQLite snapshot so a
// repository deleted during startup cannot be paired with a stale thread row.
func (repositories *Repositories) ReadFrontendRouteAccess(
	ctx context.Context,
) (FrontendRouteAccess, error) {
	if repositories == nil || repositories.database == nil {
		return FrontendRouteAccess{}, errors.New("repositories are unavailable")
	}
	rows, err := repositories.database.sql.QueryContext(ctx, `
		SELECT identity_kind, identity_value, lifecycle
		FROM (
			SELECT 'repository' AS identity_kind, id AS identity_value, 'active' AS lifecycle
			FROM repositories
			WHERE deleted_at_unix_micros IS NULL
			UNION ALL
			SELECT 'workspace', workspaces.id, 'active'
			FROM workspaces
			JOIN repositories ON repositories.id = workspaces.repository_id
			WHERE workspaces.state = 'active'
			  AND repositories.deleted_at_unix_micros IS NULL
			UNION ALL
			SELECT 'thread', threads.id,
				CASE WHEN threads.archived_at_unix_micros IS NULL THEN 'active' ELSE 'archived' END
			FROM threads
			JOIN repositories ON repositories.id = threads.repository_id
			WHERE repositories.deleted_at_unix_micros IS NULL
			  AND threads.deleted_at_unix_micros IS NULL
		)
		ORDER BY identity_kind, identity_value
		LIMIT ?`, maxFrontendRouteAccessIdentities+1)
	if err != nil {
		return FrontendRouteAccess{}, classify("read frontend route access", err)
	}
	defer rows.Close()

	result := FrontendRouteAccess{}
	count := 0
	for rows.Next() {
		count++
		if count > maxFrontendRouteAccessIdentities {
			return FrontendRouteAccess{}, errors.New("frontend route access exceeds bounded identity limit")
		}
		var kind, value, lifecycle string
		if err := rows.Scan(&kind, &value, &lifecycle); err != nil {
			return FrontendRouteAccess{}, classify("scan frontend route access", err)
		}
		switch kind {
		case "repository":
			identity, parseErr := domain.ParseRepositoryID(value)
			if parseErr != nil {
				return FrontendRouteAccess{}, classify("parse frontend repository identity", parseErr)
			}
			result.AccessibleRepositories = append(result.AccessibleRepositories, identity)
			result.FirstRunComplete = true
		case "thread":
			identity, parseErr := domain.ParseThreadID(value)
			if parseErr != nil {
				return FrontendRouteAccess{}, classify("parse frontend thread identity", parseErr)
			}
			if lifecycle == "archived" {
				result.ArchivedThreads = append(result.ArchivedThreads, identity)
			} else {
				result.AccessibleThreads = append(result.AccessibleThreads, identity)
			}
		case "workspace":
			if result.SelectedWorkspaceID.IsZero() {
				identity, parseErr := domain.ParseWorkspaceID(value)
				if parseErr != nil {
					return FrontendRouteAccess{}, classify("parse frontend workspace identity", parseErr)
				}
				result.SelectedWorkspaceID = identity
			}
		}
	}
	if err := rows.Err(); err != nil {
		return FrontendRouteAccess{}, classify("iterate frontend route access", err)
	}
	return result, nil
}
