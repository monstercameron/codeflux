package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// NonFunctionalBaseline is one repository's recorded suite duration (PIPE-013).
type NonFunctionalBaseline struct {
	RepositoryID       domain.RepositoryID
	Elapsed            time.Duration
	RepositoryRevision string
	HostPlatform       string
	RecordedAt         time.Time
}

// RecordNonFunctionalBaseline is one measurement to hold as the comparison
// point for later runs.
type RecordNonFunctionalBaseline struct {
	ProjectID          domain.ProjectID
	RepositoryID       domain.RepositoryID
	Elapsed            time.Duration
	RepositoryRevision string
	HostPlatform       string
}

// RecordNonFunctionalBaseline stores or replaces a repository's baseline.
//
// A later measurement supersedes an earlier one rather than accumulating: the
// baseline is a rolling answer to "how long does this suite take", not a
// history of every run.
func (repositories *Repositories) RecordNonFunctionalBaseline(
	ctx context.Context,
	input RecordNonFunctionalBaseline,
) (NonFunctionalBaseline, error) {
	switch {
	case input.ProjectID.IsZero() || input.RepositoryID.IsZero():
		return NonFunctionalBaseline{}, errors.New("project and repository are required")
	case input.Elapsed < 0:
		return NonFunctionalBaseline{}, errors.New("a baseline duration must not be negative")
	case !gitRevision.MatchString(input.RepositoryRevision):
		return NonFunctionalBaseline{}, errors.New(
			"a baseline must name the forty-character revision it was measured at")
	case strings.TrimSpace(input.HostPlatform) == "":
		return NonFunctionalBaseline{}, errors.New(
			"a baseline must name the host that measured it")
	}

	moment, micros := repositories.timestamp()
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO non_functional_baselines
		   (repository_id, project_id, elapsed_millis, repository_revision,
		    host_platform, recorded_at_unix_micros)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (repository_id) DO UPDATE SET
		   elapsed_millis = excluded.elapsed_millis,
		   repository_revision = excluded.repository_revision,
		   host_platform = excluded.host_platform,
		   recorded_at_unix_micros = excluded.recorded_at_unix_micros`,
		input.RepositoryID.String(), input.ProjectID.String(),
		input.Elapsed.Milliseconds(), input.RepositoryRevision,
		input.HostPlatform, micros,
	); err != nil {
		return NonFunctionalBaseline{}, classify("record non-functional baseline", err)
	}
	return NonFunctionalBaseline{
		RepositoryID:       input.RepositoryID,
		Elapsed:            input.Elapsed,
		RepositoryRevision: input.RepositoryRevision,
		HostPlatform:       input.HostPlatform,
		RecordedAt:         moment,
	}, nil
}

// NonFunctionalBaselineFor returns a repository's baseline, or reports that
// none has been recorded.
func (repositories *Repositories) NonFunctionalBaselineFor(
	ctx context.Context,
	repositoryID domain.RepositoryID,
) (NonFunctionalBaseline, bool, error) {
	if repositoryID.IsZero() {
		return NonFunctionalBaseline{}, false, errors.New("repository ID must not be empty")
	}
	var (
		baseline     NonFunctionalBaseline
		elapsedMilli int64
		micros       int64
	)
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT elapsed_millis, repository_revision, host_platform,
		        recorded_at_unix_micros
		   FROM non_functional_baselines WHERE repository_id = ?`,
		repositoryID.String(),
	).Scan(&elapsedMilli, &baseline.RepositoryRevision,
		&baseline.HostPlatform, &micros)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NonFunctionalBaseline{}, false, nil
		}
		return NonFunctionalBaseline{}, false,
			classify("read non-functional baseline", err)
	}
	baseline.RepositoryID = repositoryID
	baseline.Elapsed = time.Duration(elapsedMilli) * time.Millisecond
	baseline.RecordedAt = time.UnixMicro(micros).UTC()
	return baseline, true, nil
}
