package main

import (
	"errors"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
)

func identityOf(t *testing.T, kind codefluxv1.StableIdentityKind, value string) *codefluxv1.StableIdentity {
	t.Helper()
	return &codefluxv1.StableIdentity{Kind: kind, Value: value}
}

func repositorySummaryFixture(t *testing.T, name, revision string) (
	*codefluxv1.RepositorySummary, domain.RepositoryID,
) {
	t.Helper()
	id, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	return &codefluxv1.RepositorySummary{
		RepositoryId: identityOf(t,
			codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY, id.String()),
		DisplayName: &codefluxv1.RedactedText{Value: name},
		Git:         &codefluxv1.GitStateView{HeadRevision: revision},
	}, id
}

func TestEveryOfferedRepositoryCanBeEntered(t *testing.T) {
	// The chooser was built from a hardcoded count of zero, so a person with a
	// repository already open was told they had none, beside a browse control
	// that could not create one either. A row that cannot be entered is not a
	// choice, so every row must carry a path.
	summary, repositoryID := repositorySummaryFixture(t, "orders-service", strings.Repeat("c", 40))
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	choices, err := projectRepositoryChoices(
		&codefluxv1.ListRepositoriesResponse{
			Repositories: []*codefluxv1.RepositorySummary{summary},
		},
		map[string]routes.Route{
			repositoryID.String(): {
				Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID,
			},
		},
	)
	if err != nil {
		t.Fatalf("projecting choices failed: %v", err)
	}
	if len(choices) != 1 {
		t.Fatalf("projected %d choices, want 1", len(choices))
	}
	choice := choices[0]
	if choice.Name != "orders-service" {
		t.Errorf("name = %q, want the coordinator's display name", choice.Name)
	}
	if choice.Path == "" {
		t.Fatal("the choice carries no path, so nothing can be opened from it")
	}
	if !strings.Contains(choice.Path, threadID.String()) {
		t.Errorf("path = %q, want the open thread", choice.Path)
	}
	if choice.Revision != strings.Repeat("c", 7) {
		t.Errorf("revision = %q, want it shortened to what people read", choice.Revision)
	}
}

func TestARepositoryWithNoOpenThreadStillGoesSomewhere(t *testing.T) {
	summary, _ := repositorySummaryFixture(t, "orders-service", "")
	choices, err := projectRepositoryChoices(
		&codefluxv1.ListRepositoriesResponse{
			Repositories: []*codefluxv1.RepositorySummary{summary},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if choices[0].Path == "" {
		t.Error("a repository with no thread became an inert row")
	}
	if choices[0].Detail != "No open thread" {
		t.Errorf("detail = %q, want it to say there is no thread", choices[0].Detail)
	}
}

func TestAnEmptyListingIsSaidRatherThanReportedAsAFailure(t *testing.T) {
	// The two mean opposite things to a person: an empty list is a state to act
	// on, and a failure is a state to retry.
	_, err := projectRepositoryChoices(&codefluxv1.ListRepositoriesResponse{}, nil)
	if !errors.Is(err, errNoRepositoryChoices) {
		t.Fatalf("an empty listing reported %v, want the empty answer", err)
	}
}

func TestOnlyTheNamedRepositoryGetsTheSelectedThread(t *testing.T) {
	// Assuming the selected thread belongs to every listed repository sends a
	// person into the wrong conversation the moment a second repository exists.
	// The coordinator is the only thing that knows which one is right.
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	other, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	selected := selectedThreadRoutes(bootstrapEnvelope{
		SelectedThreadID: identityOf(t,
			codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, threadID.String()),
		SelectedRepositoryID: identityOf(t,
			codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY, repositoryID.String()),
	})
	if _, present := selected[repositoryID.String()]; !present {
		t.Error("the named repository was not given its thread")
	}
	if _, present := selected[other.String()]; present {
		t.Error("an unrelated repository was given the selected thread")
	}
}

func TestNoThreadIsGuessedWithoutARepositoryToPinItTo(t *testing.T) {
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	selected := selectedThreadRoutes(bootstrapEnvelope{
		SelectedThreadID: identityOf(t,
			codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, threadID.String()),
	})
	if len(selected) != 0 {
		t.Errorf("a thread with no repository produced %d route(s)", len(selected))
	}
}
