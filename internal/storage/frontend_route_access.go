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
	FirstRunComplete    bool
	SelectedWorkspaceID domain.WorkspaceID
	// SelectedSessionID and SelectedThreadID name the conversation a browser
	// attaches to on load. Without them the client starts no session stream at
	// all and reports itself disconnected forever, with no error anywhere to
	// say why: it simply has nothing to connect to.
	SelectedSessionID      domain.SessionID
	SelectedThreadID       domain.ThreadID
	SelectedRepositoryID   domain.RepositoryID
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
		SELECT identity_kind, identity_value, lifecycle, owner
		FROM (
			SELECT 'repository' AS identity_kind, id AS identity_value,
				'active' AS lifecycle, '' AS owner
			FROM repositories
			WHERE deleted_at_unix_micros IS NULL
			UNION ALL
			SELECT 'workspace', workspaces.id, 'active', ''
			FROM workspaces
			JOIN repositories ON repositories.id = workspaces.repository_id
			WHERE workspaces.state = 'active'
			  AND repositories.deleted_at_unix_micros IS NULL
			UNION ALL
			SELECT 'thread', threads.id,
				CASE WHEN threads.archived_at_unix_micros IS NULL THEN 'active' ELSE 'archived' END,
				''
			FROM threads
			JOIN repositories ON repositories.id = threads.repository_id
			WHERE repositories.deleted_at_unix_micros IS NULL
			  AND threads.deleted_at_unix_micros IS NULL
			UNION ALL
			-- A session row carries its thread and repository in the trailing
			-- columns, because a session is only addressable together with the
			-- conversation it streams and the repository that conversation is
			-- about. Guessing either from the other rows would be wrong the
			-- moment a second repository exists.
			SELECT 'session', sessions.id, threads.id, threads.repository_id
			FROM sessions
			JOIN threads ON threads.id = sessions.thread_id
			JOIN repositories ON repositories.id = threads.repository_id
			WHERE repositories.deleted_at_unix_micros IS NULL
			  AND threads.deleted_at_unix_micros IS NULL
			  AND threads.archived_at_unix_micros IS NULL
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
		var kind, value, lifecycle, owner string
		if err := rows.Scan(&kind, &value, &lifecycle, &owner); err != nil {
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
		case "session":
			// The first session by identity wins. Identities are time-ordered,
			// so this is the oldest live conversation, which is the one a
			// returning browser expects to be put back into.
			if result.SelectedSessionID.IsZero() {
				identity, parseErr := domain.ParseSessionID(value)
				if parseErr != nil {
					return FrontendRouteAccess{}, classify("parse frontend session identity", parseErr)
				}
				threadIdentity, parseErr := domain.ParseThreadID(lifecycle)
				if parseErr != nil {
					return FrontendRouteAccess{}, classify("parse frontend session thread", parseErr)
				}
				repositoryIdentity, parseErr := domain.ParseRepositoryID(owner)
				if parseErr != nil {
					return FrontendRouteAccess{}, classify("parse frontend session repository", parseErr)
				}
				result.SelectedSessionID = identity
				result.SelectedThreadID = threadIdentity
				result.SelectedRepositoryID = repositoryIdentity
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
