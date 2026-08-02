package storage

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// TestPIPE013_ABaselineIsRecordedOncePerRepositoryAndSuperseded covers the
// durable half of PIPE-013.
//
// The non-functional stage compared against a fixed sixty-second budget. A
// fixed number measures the machine rather than the change, so the comparison
// point has to be this repository's own recorded duration.
func TestPIPE013_ABaselineIsRecordedOncePerRepositoryAndSuperseded(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 7100)
	repositoryID := testRepositoryID(t, 7101)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	if _, found, err := repositories.NonFunctionalBaselineFor(
		t.Context(), repositoryID,
	); err != nil || found {
		t.Fatalf("a repository with no history reported a baseline: found=%v err=%v",
			found, err)
	}

	first := RecordNonFunctionalBaseline{
		ProjectID: projectID, RepositoryID: repositoryID,
		Elapsed:            4 * time.Second,
		RepositoryRevision: approvalRevision,
		HostPlatform:       "windows/arm64",
	}
	if _, err := repositories.RecordNonFunctionalBaseline(t.Context(), first); err != nil {
		t.Fatalf("recording the first baseline: %v", err)
	}

	stored, found, err := repositories.NonFunctionalBaselineFor(t.Context(), repositoryID)
	if err != nil || !found {
		t.Fatalf("the baseline did not resolve: found=%v err=%v", found, err)
	}
	if stored.Elapsed != 4*time.Second {
		t.Errorf("baseline = %s, want 4s", stored.Elapsed)
	}
	if stored.HostPlatform != "windows/arm64" {
		t.Errorf("the host that measured it was lost: %q", stored.HostPlatform)
	}

	// A later measurement supersedes rather than accumulating: the baseline is
	// a rolling answer, not a history.
	second := first
	second.Elapsed = 6 * time.Second
	if _, err := repositories.RecordNonFunctionalBaseline(t.Context(), second); err != nil {
		t.Fatalf("replacing the baseline: %v", err)
	}
	stored, _, err = repositories.NonFunctionalBaselineFor(t.Context(), repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Elapsed != 6*time.Second {
		t.Errorf("baseline = %s after replacement, want 6s", stored.Elapsed)
	}

	// Another repository keeps its own answer.
	otherProject := testProjectID(t, 7110)
	otherRepository := testRepositoryID(t, 7111)
	mustCreateProjectRepository(t, repositories, otherProject, otherRepository)
	if _, found, err := repositories.NonFunctionalBaselineFor(
		t.Context(), otherRepository,
	); err != nil || found {
		t.Errorf("a baseline crossed a repository boundary: found=%v err=%v", found, err)
	}
}

// TestPIPE013_ABaselineWithoutItsProvenanceIsRefused keeps the number usable.
//
// A duration with no revision and no host is a number nobody can interpret: it
// cannot say whether it predates the change being judged, or whether it was
// measured on the machine now being compared.
func TestPIPE013_ABaselineWithoutItsProvenanceIsRefused(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 7120)
	repositoryID := testRepositoryID(t, 7121)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	base := RecordNonFunctionalBaseline{
		ProjectID: projectID, RepositoryID: repositoryID,
		Elapsed:            time.Second,
		RepositoryRevision: approvalRevision,
		HostPlatform:       "windows/arm64",
	}
	for name, mutate := range map[string]func(*RecordNonFunctionalBaseline){
		"no revision":        func(r *RecordNonFunctionalBaseline) { r.RepositoryRevision = "" },
		"a short revision":   func(r *RecordNonFunctionalBaseline) { r.RepositoryRevision = "abc" },
		"no host":            func(r *RecordNonFunctionalBaseline) { r.HostPlatform = "" },
		"no repository":      func(r *RecordNonFunctionalBaseline) { r.RepositoryID = domain.RepositoryID{} },
		"a negative elapsed": func(r *RecordNonFunctionalBaseline) { r.Elapsed = -time.Second },
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			input := base
			mutate(&input)
			if _, err := repositories.RecordNonFunctionalBaseline(t.Context(), input); err == nil {
				t.Fatal("an uninterpretable baseline was stored")
			}
		})
	}
}
