package storage

import (
	"errors"
	"strconv"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
)

// episodesEqual compares two Episode values by content rather than by Go's
// default struct equality, which for pointer fields (EndingRevision,
// Outcome, ClosedAt) compares pointer identity, not the pointed-to value:
// two independently scanned rows with equal closure facts hold different
// pointer addresses and would otherwise spuriously compare unequal.
func episodesEqual(a, b Episode) bool {
	switch {
	case a.ID != b.ID || a.ProjectID != b.ProjectID || a.RepositoryID != b.RepositoryID || a.TaskID != b.TaskID:
		return false
	case a.FingerprintSchemaVersion != b.FingerprintSchemaVersion || a.FingerprintHash != b.FingerprintHash:
		return false
	case a.StartingRevision != b.StartingRevision || a.Status != b.Status || a.IdempotencyKey != b.IdempotencyKey:
		return false
	case !a.StartedAt.Equal(b.StartedAt):
		return false
	case (a.EndingRevision == nil) != (b.EndingRevision == nil):
		return false
	case a.EndingRevision != nil && *a.EndingRevision != *b.EndingRevision:
		return false
	case (a.Outcome == nil) != (b.Outcome == nil):
		return false
	case a.Outcome != nil && *a.Outcome != *b.Outcome:
		return false
	case (a.ClosedAt == nil) != (b.ClosedAt == nil):
		return false
	case a.ClosedAt != nil && !a.ClosedAt.Equal(*b.ClosedAt):
		return false
	default:
		return true
	}
}

// episodeFactReferencesEqual compares two EpisodeFactReference values by
// content: Revision is a pointer field with the same identity-vs-value
// hazard episodesEqual documents above.
func episodeFactReferencesEqual(a, b EpisodeFactReference) bool {
	switch {
	case a.ID != b.ID || a.EpisodeID != b.EpisodeID || a.FactTaskID != b.FactTaskID || a.Kind != b.Kind:
		return false
	case a.ReferenceID != b.ReferenceID || a.Ordinal != b.Ordinal || a.IdempotencyKey != b.IdempotencyKey:
		return false
	case (a.Revision == nil) != (b.Revision == nil):
		return false
	case a.Revision != nil && *a.Revision != *b.Revision:
		return false
	case !a.RecordedAt.Equal(b.RecordedAt):
		return false
	default:
		return true
	}
}

func testEpisodeID(t *testing.T, number int) domain.EpisodeID {
	t.Helper()
	id, err := domain.ParseEpisodeID("epi_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// mustEpisodeFingerprint builds a real internal/fingerprint.ExactFingerprint
// and hashes it, so episode tests exercise the actual versioned-fingerprint
// machinery the M21-029..039 task direction calls for, rather than a fake
// hex string standing in for it.
func mustEpisodeFingerprint(t *testing.T, projectID domain.ProjectID, repositoryID domain.RepositoryID) (uint32, string) {
	t.Helper()
	exact, err := fingerprint.BuildExactFingerprint(fingerprint.ExactFingerprintInput{
		Project:           projectID,
		Repository:        repositoryID,
		BaseRevision:      domain.RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
		TaskClass:         fingerprint.TaskClassBugFix,
		Risk:              domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := exact.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return exact.SchemaVersion, hash
}

func mustOpenEpisode(t *testing.T, repositories *Repositories, base int, projectID domain.ProjectID, repositoryID domain.RepositoryID, task Task) Episode {
	t.Helper()
	schemaVersion, hash := mustEpisodeFingerprint(t, projectID, repositoryID)
	episode, err := repositories.OpenEpisode(t.Context(), OpenEpisode{
		ID: testEpisodeID(t, base), ProjectID: projectID, RepositoryID: repositoryID, TaskID: task.ID,
		FingerprintSchemaVersion: schemaVersion, FingerprintHash: hash,
		StartingRevision: "deadbeefcafefeed", IdempotencyKey: "episode-open-" + task.ID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return episode
}

// -----------------------------------------------------------------------
// M21-029 boundary: open, idempotent retry, and project/repository binding.
// -----------------------------------------------------------------------

func TestOpenEpisodeIsIdempotentAndConflictsOnDivergentRetry(t *testing.T) {
	repositories, task := createTaskFixture(t, 5000)
	projectID := testProjectID(t, 5000)
	repositoryID := testRepositoryID(t, 5001)

	episode := mustOpenEpisode(t, repositories, 5003, projectID, repositoryID, task)
	if episode.Status != domain.EpisodeStatusOpen {
		t.Fatalf("status = %s, want open", episode.Status)
	}
	if episode.TaskID != task.ID || episode.ProjectID != projectID || episode.RepositoryID != repositoryID {
		t.Fatalf("episode = %#v", episode)
	}

	// Idempotent retry with the same identity and key returns the same row.
	retried := mustOpenEpisode(t, repositories, 5003, projectID, repositoryID, task)
	if !episodesEqual(retried, episode) {
		t.Fatalf("retried episode = %#v, want %#v", retried, episode)
	}

	// A different episode ID for the same task is a conflict: one task has
	// at most one episode.
	schemaVersion, hash := mustEpisodeFingerprint(t, projectID, repositoryID)
	if _, err := repositories.OpenEpisode(t.Context(), OpenEpisode{
		ID: testEpisodeID(t, 5006), ProjectID: projectID, RepositoryID: repositoryID, TaskID: task.ID,
		FingerprintSchemaVersion: schemaVersion, FingerprintHash: hash,
		StartingRevision: "deadbeefcafefeed", IdempotencyKey: "different-key",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("divergent open error = %v, want ErrConflict", err)
	}
}

func TestOpenEpisodeRejectsMismatchedProjectOrRepository(t *testing.T) {
	repositories, task := createTaskFixture(t, 5010)
	projectID := testProjectID(t, 5010)
	repositoryID := testRepositoryID(t, 5011)
	otherProjectID := testProjectID(t, 5020)
	mustCreateProjectRepository(t, repositories, otherProjectID, testRepositoryID(t, 5021))

	schemaVersion, hash := mustEpisodeFingerprint(t, projectID, repositoryID)
	if _, err := repositories.OpenEpisode(t.Context(), OpenEpisode{
		ID: testEpisodeID(t, 5013), ProjectID: otherProjectID, RepositoryID: repositoryID, TaskID: task.ID,
		FingerprintSchemaVersion: schemaVersion, FingerprintHash: hash,
		StartingRevision: "deadbeefcafefeed", IdempotencyKey: "mismatched-project",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("mismatched-project open error = %v, want ErrConstraint", err)
	}
}

// -----------------------------------------------------------------------
// M21-034, M21-037, M21-038: close freezes the episode; raw-SQL attacks
// against the freeze trigger.
// -----------------------------------------------------------------------

func TestCloseEpisodeFreezesAndRejectsFurtherMutation(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5030)
	projectID := testProjectID(t, 5030)
	repositoryID := testRepositoryID(t, 5031)
	episode := mustOpenEpisode(t, repositories, 5033, projectID, repositoryID, task)

	closed, err := repositories.CloseEpisode(ctx, CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: "feedfacecafebeef", Outcome: domain.EpisodeOutcomeAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != domain.EpisodeStatusClosed || closed.Outcome == nil || *closed.Outcome != domain.EpisodeOutcomeAccepted {
		t.Fatalf("closed episode = %#v", closed)
	}
	if closed.EndingRevision == nil || *closed.EndingRevision != "feedfacecafebeef" {
		t.Fatalf("closed episode ending revision = %#v", closed.EndingRevision)
	}

	// Idempotent retry with the identical closure facts succeeds.
	retried, err := repositories.CloseEpisode(ctx, CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: "feedfacecafebeef", Outcome: domain.EpisodeOutcomeAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !episodesEqual(retried, closed) {
		t.Fatalf("retried close = %#v, want %#v", retried, closed)
	}

	// A divergent close (different outcome) is a typed conflict, not a
	// silent overwrite of frozen history.
	if _, err := repositories.CloseEpisode(ctx, CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: "feedfacecafebeef", Outcome: domain.EpisodeOutcomeRejected,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("divergent close error = %v, want ErrConflict", err)
	}

	// Raw-SQL attack 1: attempt to directly UPDATE a closed episode's
	// outcome, bypassing the repository layer entirely. The
	// episodes_immutable_after_close trigger must abort it.
	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE episodes SET outcome = 'rejected' WHERE id = ?`, episode.ID,
	); !errors.Is(classify("raw update closed episode", err), ErrConstraint) {
		t.Fatalf("raw UPDATE on closed episode error = %v, want ErrConstraint", err)
	}

	// Raw-SQL attack 2: attempt to re-open a closed episode.
	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE episodes SET status = 'open' WHERE id = ?`, episode.ID,
	); !errors.Is(classify("raw reopen closed episode", err), ErrConstraint) {
		t.Fatalf("raw re-open of closed episode error = %v, want ErrConstraint", err)
	}

	// Raw-SQL attack 3: attempt to delete the episode outright.
	if _, err := repositories.database.sql.ExecContext(
		ctx, `DELETE FROM episodes WHERE id = ?`, episode.ID,
	); !errors.Is(classify("raw delete episode", err), ErrConstraint) {
		t.Fatalf("raw DELETE of episode error = %v, want ErrConstraint", err)
	}

	// The row is genuinely untouched by every attack above.
	unchanged, err := repositories.GetEpisode(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !episodesEqual(unchanged, closed) {
		t.Fatalf("episode after attacks = %#v, want unchanged %#v", unchanged, closed)
	}
}

func TestCloseEpisodeRejectsUnknownEpisode(t *testing.T) {
	if _, err := (&Repositories{}).CloseEpisode(t.Context(), CloseEpisode{}); err == nil {
		t.Fatal("expected an error for an empty episode ID")
	}
}

// -----------------------------------------------------------------------
// M21-032: ordered actions and results by event reference.
// -----------------------------------------------------------------------

func TestListEpisodeActionEventsOrdersByJournalSequenceWithoutCopyingPayload(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5050)
	projectID := testProjectID(t, 5050)
	repositoryID := testRepositoryID(t, 5051)
	episode := mustOpenEpisode(t, repositories, 5053, projectID, repositoryID, task)

	kinds := []string{"validator-result-observed", "compiler-diagnostic-emitted", "refinement-applied"}
	for index, kind := range kinds {
		if _, err := repositories.AppendTaskEvent(ctx, AppendTaskEvent{
			ID: testEventID(t, 5060+index), TaskID: task.ID, EventType: kind,
			PayloadJSON: `{"ordinal":` + strconv.Itoa(index) + `}`, IdempotencyKey: "event-" + strconv.Itoa(index),
		}); err != nil {
			t.Fatal(err)
		}
	}

	events, err := repositories.ListEpisodeActionEvents(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(kinds) {
		t.Fatalf("action events = %#v, want %d entries", events, len(kinds))
	}
	for index, event := range events {
		if event.EventType != kinds[index] {
			t.Fatalf("event[%d].EventType = %s, want %s (out of order)", index, event.EventType, kinds[index])
		}
		if event.Sequence != uint64(index+1) {
			t.Fatalf("event[%d].Sequence = %d, want %d", index, event.Sequence, index+1)
		}
	}
}

// -----------------------------------------------------------------------
// M21-030..036: episode fact references; M21-036 raw-SQL attack proving a
// repair-attempt fact can never be recorded ahead of its pre-repair-failure
// fact.
// -----------------------------------------------------------------------

func TestRecordEpisodeFactReferenceAllocatesOrdinalsPerKind(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5070)
	projectID := testProjectID(t, 5070)
	repositoryID := testRepositoryID(t, 5071)
	episode := mustOpenEpisode(t, repositories, 5073, projectID, repositoryID, task)

	one := uint64(1)
	first, err := repositories.RecordEpisodeFactReference(ctx, RecordEpisodeFactReference{
		ID: "fact-1", EpisodeID: episode.ID, FactTaskID: task.ID,
		Kind: domain.EpisodeFactKindRequirementRevision, Revision: &one, IdempotencyKey: "fact-1-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Ordinal != 0 {
		t.Fatalf("first fact ordinal = %d, want 0", first.Ordinal)
	}
	two := uint64(2)
	second, err := repositories.RecordEpisodeFactReference(ctx, RecordEpisodeFactReference{
		ID: "fact-2", EpisodeID: episode.ID, FactTaskID: task.ID,
		Kind: domain.EpisodeFactKindRequirementRevision, Revision: &two, IdempotencyKey: "fact-2-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Ordinal != 1 {
		t.Fatalf("second fact ordinal = %d, want 1", second.Ordinal)
	}

	// A different fact kind gets its own independent ordinal sequence.
	approval, err := repositories.RecordEpisodeFactReference(ctx, RecordEpisodeFactReference{
		ID: "fact-3", EpisodeID: episode.ID, FactTaskID: task.ID,
		Kind: domain.EpisodeFactKindUserApproval, ReferenceID: "apr_example", IdempotencyKey: "fact-3-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if approval.Ordinal != 0 {
		t.Fatalf("first approval fact ordinal = %d, want 0", approval.Ordinal)
	}

	// Idempotent retry.
	retried, err := repositories.RecordEpisodeFactReference(ctx, RecordEpisodeFactReference{
		ID: "fact-1", EpisodeID: episode.ID, FactTaskID: task.ID,
		Kind: domain.EpisodeFactKindRequirementRevision, Revision: &one, IdempotencyKey: "fact-1-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !episodeFactReferencesEqual(retried, first) {
		t.Fatalf("retried fact = %#v, want %#v", retried, first)
	}

	facts, err := repositories.ListEpisodeFactReferences(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 3 {
		t.Fatalf("facts = %#v, want 3", facts)
	}
}

func TestEpisodeFactReferencesRejectRepairAttemptWithoutPriorFailure(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5090)
	projectID := testProjectID(t, 5090)
	repositoryID := testRepositoryID(t, 5091)
	episode := mustOpenEpisode(t, repositories, 5093, projectID, repositoryID, task)

	// Repository-layer attempt.
	if _, err := repositories.RecordEpisodeFactReference(ctx, RecordEpisodeFactReference{
		ID: "repair-1", EpisodeID: episode.ID, FactTaskID: task.ID,
		Kind: domain.EpisodeFactKindRepairAttempt, ReferenceID: "run_example", IdempotencyKey: "repair-1-key",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("repair attempt without prior failure error = %v, want ErrConstraint", err)
	}

	// Raw-SQL attack: bypass the repository entirely and attempt the same
	// insert directly. The episode_fact_references_repair_requires_prior_failure
	// trigger must still refuse it.
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO episode_fact_references (
			id, episode_id, fact_task_id, fact_kind, fact_reference_id, ordinal, idempotency_key, recorded_at_unix_micros
		) VALUES (?, ?, ?, 'repair-attempt', 'run_raw_attack', 0, 'raw-attack-key', 0)`,
		"repair-raw", episode.ID, task.ID,
	); !errors.Is(classify("raw repair attempt insert", err), ErrConstraint) {
		t.Fatalf("raw repair-attempt insert error = %v, want ErrConstraint", err)
	}

	// Recording the pre-repair failure first, then the repair attempt,
	// succeeds -- proving the ordering requirement, not merely rejecting
	// everything.
	if _, err := repositories.RecordEpisodeFactReference(ctx, RecordEpisodeFactReference{
		ID: "failure-1", EpisodeID: episode.ID, FactTaskID: task.ID,
		Kind: domain.EpisodeFactKindPreRepairFailure, ReferenceID: "val_example", IdempotencyKey: "failure-1-key",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.RecordEpisodeFactReference(ctx, RecordEpisodeFactReference{
		ID: "repair-2", EpisodeID: episode.ID, FactTaskID: task.ID,
		Kind: domain.EpisodeFactKindRepairAttempt, ReferenceID: "run_example", IdempotencyKey: "repair-2-key",
	}); err != nil {
		t.Fatal(err)
	}

	// M21-036: a later repair must never erase the recorded pre-repair
	// failure. Because episode_fact_references is insert-only, attempting to
	// UPDATE or DELETE the failure fact must be refused.
	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE episode_fact_references SET fact_kind = 'repair-attempt' WHERE id = 'failure-1'`,
	); !errors.Is(classify("raw mutate pre-repair failure", err), ErrConstraint) {
		t.Fatalf("raw UPDATE of pre-repair failure fact error = %v, want ErrConstraint", err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx, `DELETE FROM episode_fact_references WHERE id = 'failure-1'`,
	); !errors.Is(classify("raw delete pre-repair failure", err), ErrConstraint) {
		t.Fatalf("raw DELETE of pre-repair failure fact error = %v, want ErrConstraint", err)
	}
	facts, err := repositories.ListEpisodeFactReferences(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundFailure := false
	for _, fact := range facts {
		if fact.ID == "failure-1" && fact.Kind == domain.EpisodeFactKindPreRepairFailure {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatalf("pre-repair failure fact was erased: %#v", facts)
	}
}

// -----------------------------------------------------------------------
// M21-039: invalidation overlays correct a closed episode without
// mutating it; raw-SQL attacks against the overlay-not-rewrite rule.
// -----------------------------------------------------------------------

func TestEpisodeInvalidationOverlayRequiresClosedEpisode(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5110)
	projectID := testProjectID(t, 5110)
	repositoryID := testRepositoryID(t, 5111)
	episode := mustOpenEpisode(t, repositories, 5113, projectID, repositoryID, task)

	// Repository-layer attempt against an open episode.
	if _, err := repositories.RecordEpisodeInvalidationOverlay(ctx, RecordEpisodeInvalidationOverlay{
		ID: "overlay-1", EpisodeID: episode.ID, Kind: domain.EpisodeInvalidationOverlayKindOther,
		Assessment: domain.EpisodeCurrentAssessmentUnreliable, ReasonRedacted: "test", IdempotencyKey: "overlay-1-key",
	}); err == nil {
		t.Fatal("expected an error overlaying an open episode")
	}

	// Raw-SQL attack: bypass the repository and attempt the same insert
	// directly against the still-open episode.
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO episode_invalidation_overlays (
			id, episode_id, sequence, overlay_kind, current_assessment, reason_redacted, idempotency_key, recorded_at_unix_micros
		) VALUES (?, ?, 1, 'other', 'unreliable', 'raw attack', 'raw-attack-key', 0)`,
		"overlay-raw", episode.ID,
	); !errors.Is(classify("raw overlay open episode", err), ErrConstraint) {
		t.Fatalf("raw overlay of an open episode error = %v, want ErrConstraint", err)
	}

	if _, err := repositories.CloseEpisode(ctx, CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: "feedfacecafebeef", Outcome: domain.EpisodeOutcomeAccepted,
	}); err != nil {
		t.Fatal(err)
	}

	overlay, err := repositories.RecordEpisodeInvalidationOverlay(ctx, RecordEpisodeInvalidationOverlay{
		ID: "overlay-2", EpisodeID: episode.ID, Kind: domain.EpisodeInvalidationOverlayKindOutcomeInvalidated,
		Assessment: domain.EpisodeCurrentAssessmentUnreliable, ReasonRedacted: "counterexample found", IdempotencyKey: "overlay-2-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if overlay.Sequence != 1 {
		t.Fatalf("first overlay sequence = %d, want 1", overlay.Sequence)
	}

	// The episode row itself is never mutated by the overlay: the original
	// terminal outcome remains 'accepted' in history, even though the
	// current assessment now says the outcome is unreliable.
	stillAccepted, err := repositories.GetEpisode(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillAccepted.Outcome == nil || *stillAccepted.Outcome != domain.EpisodeOutcomeAccepted {
		t.Fatalf("episode outcome after overlay = %#v, want unchanged 'accepted'", stillAccepted.Outcome)
	}

	assessment, found, err := repositories.GetEpisodeCurrentAssessment(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || assessment != domain.EpisodeCurrentAssessmentUnreliable {
		t.Fatalf("current assessment = %s (found=%v), want unreliable", assessment, found)
	}

	// A second overlay must use the next contiguous sequence; the storage
	// trigger rejects any other value, including a raw-SQL attempt to skip
	// ahead or replay an old sequence number.
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO episode_invalidation_overlays (
			id, episode_id, sequence, overlay_kind, current_assessment, reason_redacted, idempotency_key, recorded_at_unix_micros
		) VALUES (?, ?, 5, 'other', 'reliable', 'raw attack', 'raw-attack-key-2', 0)`,
		"overlay-raw-2", episode.ID,
	); !errors.Is(classify("raw overlay non-contiguous sequence", err), ErrConstraint) {
		t.Fatalf("raw overlay with a non-contiguous sequence error = %v, want ErrConstraint", err)
	}

	// Raw-SQL attack: an overlay row, once written, is itself immutable.
	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE episode_invalidation_overlays SET current_assessment = 'reliable' WHERE id = 'overlay-2'`,
	); !errors.Is(classify("raw mutate overlay", err), ErrConstraint) {
		t.Fatalf("raw UPDATE of an overlay error = %v, want ErrConstraint", err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx, `DELETE FROM episode_invalidation_overlays WHERE id = 'overlay-2'`,
	); !errors.Is(classify("raw delete overlay", err), ErrConstraint) {
		t.Fatalf("raw DELETE of an overlay error = %v, want ErrConstraint", err)
	}
}

// -----------------------------------------------------------------------
// SupportingEpisodes landmine closure: a descendant exposed to its
// ancestor's episode must be refused as independent confirmation, proved
// through the real repository/database path (not a hand-built domain index).
// -----------------------------------------------------------------------

func TestSupportingEpisodeLandmineRefusesContaminatedIndependentConfirmation(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5130)
	projectID := testProjectID(t, 5130)
	repositoryID := testRepositoryID(t, 5131)
	episode := mustOpenEpisode(t, repositories, 5133, projectID, repositoryID, task)

	ancestor := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 5140)
	descendant := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 5150)
	if err := repositories.RecordMemoryArtifactDerivedFrom(ctx, descendant, ancestor); err != nil {
		t.Fatal(err)
	}

	// The ancestor was exposed to `episode`; the descendant inherited that
	// same exposure (it was derived from the ancestor while that episode was
	// active).
	if err := repositories.RecordMemoryArtifactSupportingEpisode(ctx, ancestor, episode.ID); err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactSupportingEpisode(ctx, descendant, episode.ID); err != nil {
		t.Fatal(err)
	}

	// A second, later episode is a genuinely fresh, unexposed episode.
	freshEpisode := mustOpenEpisode(t, repositories, 5160, projectID, repositoryID, secondTaskFixture(t, repositories, projectID, repositoryID, 5170))

	// Build the lineage index from the real, repository-backed
	// GetMemoryArtifactLineage path -- the API the fact-extraction and
	// retrieval lanes are expected to call -- rather than a hand-built
	// domain literal, so this test proves the actual storage wiring, not
	// just the domain-level algorithm (already covered by
	// TestConfirmsMemoryArtifactIndependentlyRejectsSameEvidenceFamily in
	// internal/domain).
	ancestorLineage, err := repositories.GetMemoryArtifactLineage(ctx, ancestor)
	if err != nil {
		t.Fatal(err)
	}
	descendantLineage, err := repositories.GetMemoryArtifactLineage(ctx, descendant)
	if err != nil {
		t.Fatal(err)
	}
	if !ancestorLineage.SupportingEpisodesKnown || len(ancestorLineage.SupportingEpisodes) != 1 || ancestorLineage.SupportingEpisodes[0] != episode.ID {
		t.Fatalf("ancestor lineage supporting episodes = %#v", ancestorLineage)
	}
	index := map[domain.MemoryArtifactID]domain.MemoryArtifactLineage{
		ancestor:   ancestorLineage,
		descendant: descendantLineage,
	}

	// The landmine, if still open, would report this as independent
	// confirmation because SupportingEpisodes would read back empty. Fixed
	// behavior: the descendant's only exposure is the same episode as its
	// ancestor's, so this must be refused.
	confirmed, err := domain.ConfirmsMemoryArtifactIndependently(ancestor, index, []domain.EpisodeID{episode.ID})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed {
		t.Fatal("descendant's shared exposure to the ancestor's episode was incorrectly accepted as independent confirmation")
	}

	// A genuinely fresh episode, never recorded against either artifact,
	// does count.
	confirmed, err = domain.ConfirmsMemoryArtifactIndependently(ancestor, index, []domain.EpisodeID{freshEpisode.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed {
		t.Fatal("a genuinely unexposed episode should confirm independence")
	}

	// The whole-project bulk loader (loadMemoryArtifactLineageIndex, used by
	// deletion preview and other project-wide callers) also reports the
	// descendant's real, known exposure -- not the pre-fix always-empty
	// placeholder.
	projectIndex, err := loadMemoryArtifactLineageIndex(ctx, repositories.database.sql, projectID)
	if err != nil {
		t.Fatal(err)
	}
	descendantEntry, ok := projectIndex[descendant]
	if !ok {
		t.Fatalf("project-wide lineage index missing descendant %s", descendant)
	}
	if !descendantEntry.SupportingEpisodesKnown || len(descendantEntry.SupportingEpisodes) != 1 || descendantEntry.SupportingEpisodes[0] != episode.ID {
		t.Fatalf("descendant entry from the whole-project loader = %#v", descendantEntry)
	}
}

// TestMemoryArtifactSupportingEpisodeRejectsCrossProjectLink is the
// raw-SQL attack against memory_artifact_supporting_episode's
// project-boundary trigger: an artifact in one project must never be
// recorded as supported by an episode belonging to a different project,
// per AGENTS.md "Add explicit project-boundary predicates to memory,
// graph, vector, and retrieval queries."
func TestMemoryArtifactSupportingEpisodeRejectsCrossProjectLink(t *testing.T) {
	ctx := t.Context()
	repositories, taskA := createTaskFixture(t, 5230)
	projectA := testProjectID(t, 5230)
	repositoryA := testRepositoryID(t, 5231)
	episodeA := mustOpenEpisode(t, repositories, 5233, projectA, repositoryA, taskA)

	projectB := testProjectID(t, 5240)
	repositoryB := testRepositoryID(t, 5241)
	mustCreateProjectRepository(t, repositories, projectB, repositoryB)
	taskB := secondTaskFixture(t, repositories, projectB, repositoryB, 5250)
	episodeB := mustOpenEpisode(t, repositories, 5253, projectB, repositoryB, taskB)

	artifactA := createMemoryArtifactFixture(t, repositories, projectA, repositoryA, 5260)

	// Repository-layer attempt: artifactA (project A) linked to episodeB
	// (project B).
	if err := repositories.RecordMemoryArtifactSupportingEpisode(ctx, artifactA, episodeB.ID); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project supporting episode error = %v, want ErrConstraint", err)
	}

	// Raw-SQL attack: bypass the repository entirely.
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO memory_artifact_supporting_episode (artifact_id, episode_id, recorded_at_unix_micros) VALUES (?, ?, 0)`,
		artifactA, episodeB.ID,
	); !errors.Is(classify("raw cross-project supporting episode insert", err), ErrConstraint) {
		t.Fatalf("raw cross-project supporting episode insert error = %v, want ErrConstraint", err)
	}

	// The same-project link succeeds, proving the trigger discriminates
	// correctly rather than rejecting everything.
	if err := repositories.RecordMemoryArtifactSupportingEpisode(ctx, artifactA, episodeA.ID); err != nil {
		t.Fatal(err)
	}
}

// secondTaskFixture creates a second task (and its own episode-eligible
// project scope) sharing an existing project/repository, for tests that
// need two independent tasks/episodes within the same project.
func secondTaskFixture(t *testing.T, repositories *Repositories, projectID domain.ProjectID, repositoryID domain.RepositoryID, base int) Task {
	t.Helper()
	threadID := testThreadID(t, base)
	if _, err := repositories.CreateThread(t.Context(), CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Second task fixture",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := repositories.CreateTask(t.Context(), CreateTask{
		ID: testTaskID(t, base+1), ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "second-task-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}
