package storage

import (
	"path/filepath"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/migrations"
)

// TestChronologicalEpisodesMigrationAppliesForwardFromPreviousSchema mirrors
// TestAtomDocumentationAndNamingMigrationAppliesForwardFromPreviousSchema: it
// proves migration 000027 is a genuine forward step from 000026's committed
// schema (the new tables do not exist before it applies and do exist,
// exactly once, after it applies).
func TestChronologicalEpisodesMigrationAppliesForwardFromPreviousSchema(t *testing.T) {
	database := openMigrationTestDatabase(t)
	sources, err := migrations.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) < 28 || sources[27].Descriptor.Name != "000027_chronological_episodes.sql" {
		t.Fatalf("migration sources = %#v", sources)
	}
	for _, source := range sources[:27] {
		if _, err := database.sql.ExecContext(t.Context(), source.SQL); err != nil {
			t.Fatalf("apply %s: %v", source.Descriptor.Name, err)
		}
	}
	newTables := []string{
		"episodes", "episode_fact_references", "episode_invalidation_overlays",
		"memory_artifact_supporting_episode",
	}
	for _, table := range newTables {
		var before int
		if err := database.sql.QueryRowContext(t.Context(), `
			SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if before != 0 {
			t.Fatalf("table %s existed before migration 000027", table)
		}
	}

	if _, err := database.sql.ExecContext(t.Context(), sources[27].SQL); err != nil {
		t.Fatal(err)
	}

	for _, table := range newTables {
		var count int
		if err := database.sql.QueryRowContext(t.Context(), `
			SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s count after migration = %d, want 1", table, count)
		}
	}
	for _, index := range []string{
		"episodes_by_project", "episodes_by_repository",
		"episode_fact_references_by_episode",
		"episode_invalidation_overlays_by_episode",
		"memory_artifact_supporting_episode_by_episode",
	} {
		var count int
		if err := database.sql.QueryRowContext(t.Context(), `
			SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("index %s count after migration = %d, want 1", index, count)
		}
	}
	for _, trigger := range []string{
		"episodes_task_boundary", "episodes_immutable_identity", "episodes_immutable_after_close",
		"episodes_reject_delete",
		"episode_fact_references_task_boundary", "episode_fact_references_repair_requires_prior_failure",
		"episode_fact_references_immutable_update", "episode_fact_references_immutable_delete",
		"episode_invalidation_overlays_requires_closed_episode",
		"episode_invalidation_overlays_sequence_contiguous",
		"episode_invalidation_overlays_replacement_boundary",
		"episode_invalidation_overlays_immutable_update", "episode_invalidation_overlays_immutable_delete",
		"memory_artifact_supporting_episode_project_boundary",
		"memory_artifact_supporting_episode_immutable_update",
		"memory_artifact_supporting_episode_immutable_delete",
	} {
		var count int
		if err := database.sql.QueryRowContext(t.Context(), `
			SELECT count(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trigger).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("trigger %s count after migration = %d, want 1", trigger, count)
		}
	}
}

// TestChronologicalEpisodesBackupAndRestore proves the online backup API
// round-trips real episode, fact-reference, invalidation-overlay, and
// supporting-episode rows written through this lane's repository API: a
// snapshot taken before a later mutation restores the pre-mutation state
// exactly, including the immutable episode row and the append-only overlay
// history.
func TestChronologicalEpisodesBackupAndRestore(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5200)
	database := repositories.database
	projectID := testProjectID(t, 5200)
	repositoryID := testRepositoryID(t, 5201)

	schemaVersion, hash := mustEpisodeFingerprint(t, projectID, repositoryID)
	episode, err := repositories.OpenEpisode(ctx, OpenEpisode{
		ID: testEpisodeID(t, 5203), ProjectID: projectID, RepositoryID: repositoryID, TaskID: task.ID,
		FingerprintSchemaVersion: schemaVersion, FingerprintHash: hash,
		StartingRevision: "deadbeefcafefeed", IdempotencyKey: "backup-episode",
	})
	if err != nil {
		t.Fatal(err)
	}
	one := uint64(1)
	if _, err := repositories.RecordEpisodeFactReference(ctx, RecordEpisodeFactReference{
		ID: "backup-fact-1", EpisodeID: episode.ID, FactTaskID: task.ID,
		Kind: domain.EpisodeFactKindRequirementRevision, Revision: &one, IdempotencyKey: "backup-fact-1-key",
	}); err != nil {
		t.Fatal(err)
	}
	closed, err := repositories.CloseEpisode(ctx, CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: "feedfacecafebeef", Outcome: domain.EpisodeOutcomeAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 5210)
	if err := repositories.RecordMemoryArtifactSupportingEpisode(ctx, artifact, episode.ID); err != nil {
		t.Fatal(err)
	}
	firstOverlay, err := repositories.RecordEpisodeInvalidationOverlay(ctx, RecordEpisodeInvalidationOverlay{
		ID: "backup-overlay-1", EpisodeID: episode.ID, Kind: domain.EpisodeInvalidationOverlayKindOther,
		Assessment: domain.EpisodeCurrentAssessmentUnreliable, ReasonRedacted: "pre-backup",
		IdempotencyKey: "backup-overlay-1-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(t.TempDir(), "chronological-episodes-snapshot.sqlite3")
	if err := database.Backup(ctx, backupPath); err != nil {
		t.Fatal(err)
	}

	// Mutate after the snapshot: a second overlay layered on top.
	if _, err := repositories.RecordEpisodeInvalidationOverlay(ctx, RecordEpisodeInvalidationOverlay{
		ID: "backup-overlay-2", EpisodeID: episode.ID, Kind: domain.EpisodeInvalidationOverlayKindEvidenceInvalidated,
		Assessment: domain.EpisodeCurrentAssessmentSuperseded, ReasonRedacted: "post-backup",
		IdempotencyKey: "backup-overlay-2-key",
	}); err != nil {
		t.Fatal(err)
	}

	if err := database.Restore(ctx, backupPath); err != nil {
		t.Fatal(err)
	}

	restoredEpisode, err := repositories.GetEpisode(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !episodesEqual(restoredEpisode, closed) {
		t.Fatalf("restored episode = %#v, want %#v", restoredEpisode, closed)
	}

	restoredFacts, err := repositories.ListEpisodeFactReferences(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restoredFacts) != 1 || restoredFacts[0].ID != "backup-fact-1" {
		t.Fatalf("restored fact references = %#v", restoredFacts)
	}

	restoredOverlays, err := repositories.ListEpisodeInvalidationOverlays(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restoredOverlays) != 1 || restoredOverlays[0].ID != firstOverlay.ID {
		t.Fatalf("restored overlays = %#v, want only the pre-backup overlay", restoredOverlays)
	}

	restoredLineage, err := repositories.GetMemoryArtifactLineage(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if !restoredLineage.SupportingEpisodesKnown || len(restoredLineage.SupportingEpisodes) != 1 || restoredLineage.SupportingEpisodes[0] != episode.ID {
		t.Fatalf("restored supporting episodes = %#v", restoredLineage)
	}
}
