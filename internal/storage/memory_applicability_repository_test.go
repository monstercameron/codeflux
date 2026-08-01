package storage

import "testing"

func TestCreateMemoryArtifactApplicabilityPredicatePersistsStructuredPredicate(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1200)
	repositoryID := testRepositoryID(t, 1201)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1202)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}

	predicate, err := repositories.CreateMemoryArtifactApplicabilityPredicate(ctx, CreateMemoryArtifactApplicabilityPredicate{
		RevisionID: revision.RevisionID, FingerprintSchemaVersion: 1,
		PredicateKind:  ApplicabilityPredicateRepositoryMatch,
		Predicate:      map[string]any{"repository": repositoryID.String()},
		RequiredFields: []string{"repository"}, UnknownFieldBehavior: ApplicabilityUnknownFieldReject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if predicate.ID == "" || predicate.PredicateJSON == "" {
		t.Fatalf("created predicate = %#v", predicate)
	}

	predicates, err := repositories.ListMemoryArtifactApplicabilityPredicates(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(predicates) != 1 || predicates[0].ID != predicate.ID ||
		len(predicates[0].RequiredFields) != 1 || predicates[0].RequiredFields[0] != "repository" {
		t.Fatalf("listed predicates = %#v", predicates)
	}

	if _, err := repositories.CreateMemoryArtifactApplicabilityPredicate(ctx, CreateMemoryArtifactApplicabilityPredicate{
		RevisionID: revision.RevisionID, FingerprintSchemaVersion: 999,
		PredicateKind: ApplicabilityPredicateCustom, Predicate: map[string]any{},
		UnknownFieldBehavior: ApplicabilityUnknownFieldWarn,
	}); err == nil {
		t.Fatal("expected an unregistered fingerprint schema version to be rejected")
	}
}
