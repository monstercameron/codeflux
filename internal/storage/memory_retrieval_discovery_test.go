package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// TestListMemoryArtifactRevisionsByRepositoryAndKindFiltersLatestNonDeleted
// proves the M21-065/066/067 discovery query returns only the latest
// revision of matching, non-deleted artifacts, scoped to the requested
// project, repository, and kind.
func TestListMemoryArtifactRevisionsByRepositoryAndKindFiltersLatestNonDeleted(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1450)
	repositoryID := testRepositoryID(t, 1451)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	// One artifact with a correction, so only its LATEST revision should be
	// returned.
	artifactID := testMemoryArtifactID(t, 1452)
	revisionOne := testMemoryArtifactRevisionID(t, 1453)
	if _, err := repositories.CreateMemoryArtifact(ctx, CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: revisionOne, ProjectID: projectID,
		Content:        testRepositoryFactContent(t, repositoryID, "go build ./..."),
		IdempotencyKey: revisionOne.String(),
	}); err != nil {
		t.Fatal(err)
	}
	revisionTwo := testMemoryArtifactRevisionID(t, 1454)
	if _, err := repositories.CreateMemoryArtifactCorrection(ctx, CreateMemoryArtifactCorrection{
		PriorRevisionID:  revisionOne,
		NewRevisionID:    revisionTwo,
		CorrectedContent: testRepositoryFactContent(t, repositoryID, "go build ./... (corrected)"),
		IdempotencyKey:   revisionTwo.String(),
	}); err != nil {
		t.Fatal(err)
	}

	// A second, unrelated artifact of the same kind/repository/project, to
	// prove multiple matches are all returned.
	secondArtifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1460)

	// A deleted artifact must never be discoverable.
	deletedArtifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1465)
	if _, err := repositories.DeleteMemoryArtifact(ctx, DeleteMemoryArtifact{
		Target: deletedArtifact, ReasonRedacted: "no longer relevant",
	}); err != nil {
		t.Fatal(err)
	}

	// A different repository must never be discoverable for this query.
	otherRepositoryID := testRepositoryID(t, 1470)
	if _, err := repositories.CreateRepository(ctx, CreateRepository{
		ID: otherRepositoryID, ProjectID: projectID,
		CanonicalPath: "/fixture/" + otherRepositoryID.String(),
		GitIdentity:   "git-" + otherRepositoryID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	createMemoryArtifactFixture(t, repositories, projectID, otherRepositoryID, 1475)

	records, err := repositories.ListMemoryArtifactRevisionsByRepositoryAndKind(
		ctx, projectID, repositoryID, domain.MemoryArtifactKindRepositoryFact,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v, want exactly the 2 non-deleted, same-repository artifacts", records)
	}
	byArtifact := map[domain.MemoryArtifactID]MemoryArtifactRevisionRecord{}
	for _, record := range records {
		byArtifact[record.ArtifactID] = record
	}
	corrected, ok := byArtifact[artifactID]
	if !ok {
		t.Fatalf("records = %#v, missing the corrected artifact", records)
	}
	if corrected.RevisionID != revisionTwo {
		t.Fatalf("corrected artifact revision = %v, want the latest revision %v", corrected.RevisionID, revisionTwo)
	}
	if _, ok := byArtifact[secondArtifact]; !ok {
		t.Fatalf("records = %#v, missing the second artifact", records)
	}
	if _, ok := byArtifact[deletedArtifact]; ok {
		t.Fatalf("records = %#v, must not include the deleted artifact", records)
	}
}

// TestGetEvidenceAssuranceLevelReadsCurrentAssuranceLevel proves
// GetEvidenceAssuranceLevel reads the evidence table's own assurance_level
// column, the axis internal/retrievalgate.EligibilityCandidate.
// SupportingEvidenceAssurance is built from -- a distinct axis from
// domain.EvidenceStrength.
func TestGetEvidenceAssuranceLevelReadsCurrentAssuranceLevel(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 1480)

	validation, err := repositories.CreateValidation(ctx, CreateValidation{
		ID: testValidationID(t, 1484), TaskID: task.ID,
		State: domain.ValidationStatePassed, Severity: domain.ValidationSeverityAdvisory,
		ProfileName: "fixture-profile",
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := repositories.CreateEvidence(ctx, CreateEvidence{
		ID: testEvidenceID(t, 1485), ValidationID: validation.ID, TaskID: task.ID,
		AssuranceLevel: domain.AssuranceLevelContractChecked,
		EvidenceType:   "fixture", ContentHash: testHexHash(1485),
	})
	if err != nil {
		t.Fatal(err)
	}

	level, err := repositories.GetEvidenceAssuranceLevel(ctx, evidence.ID)
	if err != nil {
		t.Fatal(err)
	}
	if level != domain.AssuranceLevelContractChecked {
		t.Fatalf("assurance level = %q, want %q", level, domain.AssuranceLevelContractChecked)
	}

	if _, err := repositories.GetEvidenceAssuranceLevel(ctx, testEvidenceID(t, 1499)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown evidence error = %v, want not-found", err)
	}
}

// TestListEpisodesByFingerprintHashAndSupportedArtifacts proves the M21-064
// exact-identity discovery path: a closed episode matching the task's exact
// fingerprint hash is found, scoped to its project, and the memory
// artifacts it supported are readable from it.
func TestListEpisodesByFingerprintHashAndSupportedArtifacts(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 1490)
	projectID := testProjectID(t, 1490)
	repositoryID := testRepositoryID(t, 1491)

	episode := mustOpenEpisode(t, repositories, 1494, projectID, repositoryID, task)
	if _, err := repositories.CloseEpisode(ctx, CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: "deadbeefcafefeed", Outcome: domain.EpisodeOutcomeAccepted,
	}); err != nil {
		t.Fatal(err)
	}

	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1497)
	if err := repositories.RecordMemoryArtifactSupportingEpisode(ctx, artifact, episode.ID); err != nil {
		t.Fatal(err)
	}

	otherProjectID := testProjectID(t, 1500)
	otherRepositoryID := testRepositoryID(t, 1501)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)

	episodes, err := repositories.ListEpisodesByFingerprintHash(ctx, projectID, episode.FingerprintHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 || episodes[0].ID != episode.ID {
		t.Fatalf("episodes = %#v, want exactly the one matching closed episode", episodes)
	}

	// The same hash under a DIFFERENT project must never match: cross-
	// project isolation applies to exact-identity discovery too.
	none, err := repositories.ListEpisodesByFingerprintHash(ctx, otherProjectID, episode.FingerprintHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("episodes for other project = %#v, want none", none)
	}

	supported, err := repositories.ListMemoryArtifactsSupportedByEpisode(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(supported) != 1 || supported[0] != artifact {
		t.Fatalf("supported artifacts = %#v, want exactly %v", supported, artifact)
	}
}
