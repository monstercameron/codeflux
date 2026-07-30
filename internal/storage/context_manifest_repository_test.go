package storage

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestContextManifestRepositoryRoundTripIsOrderedAndImmutable(t *testing.T) {
	t.Parallel()

	repositories := openTestRepositories(t)
	repositoryID := testRepositoryID(t, 301)
	mustCreateProjectRepository(t, repositories, testProjectID(t, 300), repositoryID)
	input := validContextManifestInput(repositoryID)

	recorded, err := repositories.RecordContextManifest(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.CreatedAt.IsZero() {
		t.Fatal("recorded manifest lacks creation time")
	}
	loaded, err := repositories.GetContextManifest(t.Context(), input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, recorded) {
		t.Fatalf("loaded manifest = %#v, want %#v", loaded, recorded)
	}
	if _, err := repositories.database.sql.ExecContext(
		t.Context(),
		`UPDATE context_manifest_items
		 SET content_redacted = 'changed'
		 WHERE manifest_id = ? AND ordinal = 0`,
		input.ID,
	); !errors.Is(classify("mutate context manifest item", err), ErrConstraint) {
		t.Fatalf("immutable update error = %v", err)
	}
}

func TestContextManifestRepositoryRejectsInvalidOrConflictingRecords(t *testing.T) {
	t.Parallel()

	repositories := openTestRepositories(t)
	repositoryID := testRepositoryID(t, 311)
	mustCreateProjectRepository(t, repositories, testProjectID(t, 310), repositoryID)

	invalid := validContextManifestInput(repositoryID)
	invalid.Items[0].Path = "../outside.go"
	if _, err := repositories.RecordContextManifest(t.Context(), invalid); err == nil {
		t.Fatal("unsafe item path was accepted")
	}
	var count int
	if err := repositories.database.sql.QueryRowContext(
		t.Context(),
		`SELECT count(*) FROM context_manifests WHERE id = ?`,
		invalid.ID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid manifest left %d rows", count)
	}

	valid := validContextManifestInput(repositoryID)
	if _, err := repositories.RecordContextManifest(t.Context(), valid); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.RecordContextManifest(t.Context(), valid); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate manifest error = %v", err)
	}

	missingRepository := validContextManifestInput(testRepositoryID(t, 399))
	missingRepository.ID = strings.Repeat("9", 64)
	if _, err := repositories.RecordContextManifest(t.Context(), missingRepository); !errors.Is(err, ErrConstraint) {
		t.Fatalf("missing repository error = %v", err)
	}
}

func TestContextManifestRepositoryValidatesAccountingAndLookup(t *testing.T) {
	t.Parallel()

	repositories := openTestRepositories(t)
	repositoryID := testRepositoryID(t, 321)
	mustCreateProjectRepository(t, repositories, testProjectID(t, 320), repositoryID)

	invalid := validContextManifestInput(repositoryID)
	invalid.UsedBytes++
	if _, err := repositories.RecordContextManifest(t.Context(), invalid); err == nil {
		t.Fatal("inconsistent byte accounting was accepted")
	}
	invalid = validContextManifestInput(repositoryID)
	invalid.Items[0].Trust = "trusted"
	if _, err := repositories.RecordContextManifest(t.Context(), invalid); err == nil {
		t.Fatal("trusted repository content was accepted")
	}
	invalid = validContextManifestInput(repositoryID)
	invalid.Items[0].Reasons = nil
	if _, err := repositories.RecordContextManifest(t.Context(), invalid); err == nil {
		t.Fatal("unexplained context item was accepted")
	}
	if _, err := repositories.GetContextManifest(t.Context(), strings.Repeat("8", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing manifest error = %v", err)
	}
}

func validContextManifestInput(repositoryID domain.RepositoryID) RecordContextManifest {
	content := "package service\n\nfunc Greet() string { return \"hi\" }"
	history := strings.Repeat("a", 40)
	return RecordContextManifest{
		ID:                  strings.Repeat("1", 64),
		RepositoryID:        repositoryID,
		RepositoryRevision:  strings.Repeat("2", 40),
		MapRevision:         strings.Repeat("3", 64),
		RequirementSHA256:   strings.Repeat("4", 64),
		SelectionPolicy:     1,
		MaxFiles:            8,
		MaxBytes:            16 << 10,
		MaxEstimatedTokens:  4 << 10,
		UsedFiles:           2,
		UsedBytes:           len(content) + len(history),
		UsedEstimatedTokens: 22,
		Items: []ContextManifestItem{
			{
				Path:            "service/service.go",
				Kind:            "source",
				StartLine:       1,
				EndLine:         3,
				ContentRedacted: content,
				ContentSHA256:   strings.Repeat("5", 64),
				Reasons:         []string{"explicit-path", "exact-symbol-term:Greet"},
				Trust:           contextManifestTrust,
				EstimatedTokens: 12,
			},
			{
				Path:            "service/service.go",
				Kind:            "history",
				ContentRedacted: history,
				ContentSHA256:   strings.Repeat("6", 64),
				Reasons:         []string{"recent-history-for-selected-path"},
				Trust:           contextManifestTrust,
				EstimatedTokens: 10,
			},
		},
		Exclusions: []ContextManifestExclusion{
			{Path: ".env", Reason: "likely-secret-path"},
		},
	}
}
