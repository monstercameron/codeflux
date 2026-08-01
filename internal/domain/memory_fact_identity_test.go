package domain

import (
	"testing"
)

// -----------------------------------------------------------------------
// M21-046: deterministic normalized-fact identity.
// -----------------------------------------------------------------------

func mustReviewedCommandContent(t *testing.T, repository RepositoryID, purpose CommandPurpose, argv []string) MemoryArtifactContent {
	t.Helper()
	content, err := NewReviewedCommandMemoryContent(ReviewedCommandContent{
		Repository: repository, Purpose: purpose, Argv: argv,
		Binding: RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestNormalizedMemoryFactIdentitySameForDifferingArgvOfSameCommandSlot(t *testing.T) {
	repository := mustRepositoryID(t)
	first := mustReviewedCommandContent(t, repository, CommandPurposeTest, []string{"go", "test", "./..."})
	second := mustReviewedCommandContent(t, repository, CommandPurposeTest, []string{"gotestsum", "--", "./..."})

	firstIdentity, err := NormalizedMemoryFactIdentity(first)
	if err != nil {
		t.Fatal(err)
	}
	secondIdentity, err := NormalizedMemoryFactIdentity(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstIdentity != secondIdentity {
		t.Fatalf("identity changed with argv alone: %q vs %q, want equal -- argv is payload, not identity, per M21-046's rule", firstIdentity, secondIdentity)
	}
}

func TestNormalizedMemoryFactIdentityDiffersByPurposeAndRepository(t *testing.T) {
	repository := mustRepositoryID(t)
	otherRepository := mustRepositoryID(t)
	test := mustReviewedCommandContent(t, repository, CommandPurposeTest, []string{"go", "test", "./..."})
	build := mustReviewedCommandContent(t, repository, CommandPurposeBuild, []string{"go", "test", "./..."})
	otherRepoTest := mustReviewedCommandContent(t, otherRepository, CommandPurposeTest, []string{"go", "test", "./..."})

	testIdentity, err := NormalizedMemoryFactIdentity(test)
	if err != nil {
		t.Fatal(err)
	}
	buildIdentity, err := NormalizedMemoryFactIdentity(build)
	if err != nil {
		t.Fatal(err)
	}
	otherRepoIdentity, err := NormalizedMemoryFactIdentity(otherRepoTest)
	if err != nil {
		t.Fatal(err)
	}
	if testIdentity == buildIdentity {
		t.Fatalf("build and test purposes collided: %q", testIdentity)
	}
	if testIdentity == otherRepoIdentity {
		t.Fatalf("distinct repositories collided: %q", testIdentity)
	}
}

func TestNormalizedMemoryFactIdentityIgnoresRevisionBinding(t *testing.T) {
	repository := mustRepositoryID(t)
	first, err := NewReviewedCommandMemoryContent(ReviewedCommandContent{
		Repository: repository, Purpose: CommandPurposeBuild, Argv: []string{"go", "build", "./..."},
		Binding: RevisionBinding{Known: true, ExactRevision: "1111111111111111111111111111111111111111"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewReviewedCommandMemoryContent(ReviewedCommandContent{
		Repository: repository, Purpose: CommandPurposeBuild, Argv: []string{"go", "build", "./..."},
		Binding: RevisionBinding{Known: true, ExactRevision: "2222222222222222222222222222222222222222"},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstIdentity, err := NormalizedMemoryFactIdentity(first)
	if err != nil {
		t.Fatal(err)
	}
	secondIdentity, err := NormalizedMemoryFactIdentity(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstIdentity != secondIdentity {
		t.Fatalf("identity forked on revision alone: %q vs %q, want equal so reconfirmation at a newer revision is recognized as the same fact", firstIdentity, secondIdentity)
	}
}

func TestNormalizedMemoryFactIdentityFileToTestMappingKeyedBySourcePathOnly(t *testing.T) {
	repository := mustRepositoryID(t)
	binding := RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"}
	first, err := NewFileToTestMappingMemoryContent(FileToTestMappingContent{
		Repository: repository, SourcePath: "internal/widget/widget.go",
		TestPaths: []string{"internal/widget/widget_test.go"}, Binding: binding,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewFileToTestMappingMemoryContent(FileToTestMappingContent{
		Repository: repository, SourcePath: "internal/widget/widget.go",
		TestPaths: []string{"internal/widget/widget_test.go", "internal/widget/widget_integration_test.go"}, Binding: binding,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstIdentity, err := NormalizedMemoryFactIdentity(first)
	if err != nil {
		t.Fatal(err)
	}
	secondIdentity, err := NormalizedMemoryFactIdentity(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstIdentity != secondIdentity {
		t.Fatalf("identity forked on TestPaths alone: %q vs %q, want equal -- TestPaths is payload", firstIdentity, secondIdentity)
	}

	other, err := NewFileToTestMappingMemoryContent(FileToTestMappingContent{
		Repository: repository, SourcePath: "internal/other/other.go",
		TestPaths: []string{"internal/widget/widget_test.go"}, Binding: binding,
	})
	if err != nil {
		t.Fatal(err)
	}
	otherIdentity, err := NormalizedMemoryFactIdentity(other)
	if err != nil {
		t.Fatal(err)
	}
	if otherIdentity == firstIdentity {
		t.Fatalf("distinct source paths collided: %q", firstIdentity)
	}
}

func TestNormalizedMemoryFactIdentityWhitespaceInsensitiveButCaseSensitive(t *testing.T) {
	repository := mustRepositoryID(t)
	binding := RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"}
	spaced, err := NewRepositoryConventionMemoryContent(RepositoryConventionContent{
		Repository: repository, Scope: "  static-analysis  ", Statement: "run golangci-lint", Binding: binding,
	})
	if err != nil {
		t.Fatal(err)
	}
	tight, err := NewRepositoryConventionMemoryContent(RepositoryConventionContent{
		Repository: repository, Scope: "static-analysis", Statement: "run golangci-lint", Binding: binding,
	})
	if err != nil {
		t.Fatal(err)
	}
	upper, err := NewRepositoryConventionMemoryContent(RepositoryConventionContent{
		Repository: repository, Scope: "Static-Analysis", Statement: "run golangci-lint", Binding: binding,
	})
	if err != nil {
		t.Fatal(err)
	}

	spacedIdentity, err := NormalizedMemoryFactIdentity(spaced)
	if err != nil {
		t.Fatal(err)
	}
	tightIdentity, err := NormalizedMemoryFactIdentity(tight)
	if err != nil {
		t.Fatal(err)
	}
	upperIdentity, err := NormalizedMemoryFactIdentity(upper)
	if err != nil {
		t.Fatal(err)
	}
	if spacedIdentity != tightIdentity {
		t.Fatalf("whitespace changed identity: %q vs %q", spacedIdentity, tightIdentity)
	}
	if upperIdentity == tightIdentity {
		t.Fatalf("case difference collapsed identity: %q", tightIdentity)
	}
}

func TestNormalizedMemoryFactIdentityRejectsInvalidContent(t *testing.T) {
	if _, err := NormalizedMemoryFactIdentity(MemoryArtifactContent{}); err == nil {
		t.Fatal("expected an error for an empty, unpopulated content value")
	}
}

func TestNormalizedMemoryFactIdentityFieldBoundariesDoNotCollideAcrossFields(t *testing.T) {
	// joinMemoryFactIdentityFields length-prefixes every field; without that,
	// ("ab", "c") and ("a", "bc") would both naively join to "abc".
	repository := mustRepositoryID(t)
	ab := joinMemoryFactIdentityFields(string(MemoryArtifactKindRepositoryConvention), repository.String(), "ab", "c")
	abc := joinMemoryFactIdentityFields(string(MemoryArtifactKindRepositoryConvention), repository.String(), "a", "bc")
	if ab == abc {
		t.Fatalf("field boundary collision: %q", ab)
	}
}

func TestNormalizedMemoryFactIdentityRejectsUndeclaredKind(t *testing.T) {
	content := MemoryArtifactContent{Kind: "not-a-real-kind"}
	if _, err := NormalizedMemoryFactIdentity(content); err == nil {
		t.Fatal("expected an error for an undeclared kind with no populated payload")
	}
}
