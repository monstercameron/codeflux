package storage

import "testing"

// TestListLatestMemoryArtifactRevisionsForProjectStaysInsideItsProject proves
// the read the memory page depends on: newest revision per artifact, newest
// first, and never an artifact belonging to another project.
func TestListLatestMemoryArtifactRevisionsForProjectStaysInsideItsProject(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 940)
	repositoryID := testRepositoryID(t, 941)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	otherProjectID := testProjectID(t, 942)
	otherRepositoryID := testRepositoryID(t, 943)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)

	firstArtifact := testMemoryArtifactID(t, 944)
	firstRevision := testMemoryArtifactRevisionID(t, 945)
	if _, err := repositories.CreateMemoryArtifact(ctx, CreateMemoryArtifact{
		ArtifactID: firstArtifact, RevisionID: firstRevision, ProjectID: projectID,
		Content: testRepositoryFactContent(t, repositoryID, "go test ./..."), IdempotencyKey: "listed-one",
	}); err != nil {
		t.Fatal(err)
	}
	correctedRevision := testMemoryArtifactRevisionID(t, 946)
	if _, err := repositories.CreateMemoryArtifactCorrection(ctx, CreateMemoryArtifactCorrection{
		PriorRevisionID: firstRevision, NewRevisionID: correctedRevision,
		CorrectedContent: testRepositoryFactContent(t, repositoryID, "go test -race ./..."),
		IdempotencyKey:   "listed-one-correction",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryArtifact(ctx, CreateMemoryArtifact{
		ArtifactID: testMemoryArtifactID(t, 947), RevisionID: testMemoryArtifactRevisionID(t, 948),
		ProjectID:      otherProjectID,
		Content:        testRepositoryFactContent(t, otherRepositoryID, "make check"),
		IdempotencyKey: "other-project",
	}); err != nil {
		t.Fatal(err)
	}

	records, err := repositories.ListLatestMemoryArtifactRevisionsForProject(ctx, projectID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want exactly this project's one artifact: %#v", len(records), records)
	}
	if records[0].ArtifactID != firstArtifact || records[0].RevisionID != correctedRevision {
		t.Fatalf("record = %#v, want the corrected revision of %v", records[0], firstArtifact)
	}
	if records[0].RevisionNumber != 2 || !records[0].CreatedFromCorrection {
		t.Fatalf("record = %#v, want revision 2 written by a correction", records[0])
	}

	owner, found, err := repositories.GetMemoryArtifactProject(ctx, firstArtifact)
	if err != nil || !found || owner != projectID {
		t.Fatalf("owner = %v found = %v err = %v", owner, found, err)
	}
	missing, found, err := repositories.GetMemoryArtifactProject(ctx, testMemoryArtifactID(t, 949))
	if err != nil || found || !missing.IsZero() {
		t.Fatalf("unknown artifact owner = %v found = %v err = %v", missing, found, err)
	}
}
