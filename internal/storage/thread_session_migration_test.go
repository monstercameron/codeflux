package storage

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/migrations"
)

func TestThreadSessionLifecycleMigrationBackfillsLegacyThread(t *testing.T) {
	t.Parallel()

	database := openMigrationTestDatabase(t)
	sources, err := migrations.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) < 18 || sources[17].Descriptor.Name != "000017_thread_session_lifecycle.sql" {
		t.Fatalf("migration sources = %#v", sources)
	}
	for _, source := range sources[:17] {
		if _, err := database.sql.ExecContext(context.Background(), source.SQL); err != nil {
			t.Fatalf("apply %s: %v", source.Descriptor.Name, err)
		}
	}
	projectID := testProjectID(t, 660)
	repositoryID := testRepositoryID(t, 661)
	threadID := testThreadID(t, 662)
	if _, err := database.sql.ExecContext(t.Context(), `INSERT INTO projects (
		id, name, created_at_unix_micros, updated_at_unix_micros
	) VALUES (?, 'Legacy', 1, 1)`, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(t.Context(), `INSERT INTO repositories (
		id, project_id, canonical_path, git_identity,
		created_at_unix_micros, updated_at_unix_micros
	) VALUES (?, ?, '/legacy', 'legacy', 1, 1)`, repositoryID, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(t.Context(), `INSERT INTO threads (
		id, project_id, repository_id, title,
		created_at_unix_micros, updated_at_unix_micros, revision
	) VALUES (?, ?, ?, 'Legacy thread', 1, 1, 0)`, threadID, projectID, repositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(t.Context(), sources[17].SQL); err != nil {
		t.Fatal(err)
	}
	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repositories.GetThreadSession(t.Context(), threadID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repositories.ReplaySessionEvents(t.Context(), ReplaySessionEvents{SessionID: session.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if session.CurrentSequence != 1 || len(replayed) != 1 || replayed[0].Kind != events.KindThreadCreated ||
		replayed[0].Payload.ThreadCreated.Title != "Legacy thread" || replayed[0].Payload.ThreadCreated.WorkspaceID != nil {
		t.Fatalf("backfilled session=%#v events=%#v", session, replayed)
	}
}
