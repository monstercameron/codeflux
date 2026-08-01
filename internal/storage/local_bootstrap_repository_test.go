package storage

import (
	"path/filepath"
	"strings"
	"testing"
)

// bootstrapFixture is one absolute path and the identity a repository at it
// would be bound to.
func bootstrapFixture(t *testing.T) (string, string) {
	t.Helper()
	return filepath.Join(t.TempDir(), "work", "orders-service"), strings.Repeat("a", 40)
}

func TestOpeningARepositoryLeavesTheApplicationUsable(t *testing.T) {
	// A database with no repository row reports first-run, and the browser then
	// has no workspace to attach a session to: the whole interface stays in
	// local preview with sample content and a composer that refuses to send.
	// Nothing inside the interface could create that first row, so a fresh
	// install had no way out of preview at all.
	repositories := openTestRepositories(t)
	path, identity := bootstrapFixture(t)

	scope, err := repositories.EnsureLocalBootstrap(
		t.Context(), path, identity, "orders-service")
	if err != nil {
		t.Fatalf("opening a repository failed: %v", err)
	}
	if !scope.Created {
		t.Error("the first open did not report creating anything")
	}
	for name, zero := range map[string]bool{
		"project":    scope.ProjectID.IsZero(),
		"repository": scope.RepositoryID.IsZero(),
		"workspace":  scope.WorkspaceID.IsZero(),
		"thread":     scope.ThreadID.IsZero(),
		"session":    scope.SessionID.IsZero(),
	} {
		if zero {
			t.Errorf("opening a repository produced no %s", name)
		}
	}

	access, err := repositories.ReadFrontendRouteAccess(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !access.FirstRunComplete {
		t.Error("the coordinator still reports first-run after a repository was opened")
	}
	// Without a published session the browser starts no stream at all and
	// reports itself disconnected forever, with no error anywhere to say why.
	if access.SelectedSessionID != scope.SessionID {
		t.Errorf("published session = %s, want the opened one %s",
			access.SelectedSessionID, scope.SessionID)
	}
	if access.SelectedThreadID != scope.ThreadID {
		t.Errorf("published thread = %s, want %s",
			access.SelectedThreadID, scope.ThreadID)
	}
	// The repository is published alongside, because a session is only
	// addressable together with the repository its conversation is about.
	// Guessing it from the repository list is wrong the moment there are two.
	if access.SelectedRepositoryID != scope.RepositoryID {
		t.Errorf("published repository = %s, want %s",
			access.SelectedRepositoryID, scope.RepositoryID)
	}
	if access.SelectedWorkspaceID != scope.WorkspaceID {
		t.Errorf("published workspace = %s, want %s",
			access.SelectedWorkspaceID, scope.WorkspaceID)
	}
}

func TestOpeningTheSameRepositoryTwiceReopensIt(t *testing.T) {
	// Every start runs this. A second start that made a second project,
	// repository, workspace and thread would multiply the person's workspace
	// by the number of times they had launched the application.
	repositories := openTestRepositories(t)
	path, identity := bootstrapFixture(t)

	first, err := repositories.EnsureLocalBootstrap(t.Context(), path, identity, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repositories.EnsureLocalBootstrap(t.Context(), path, identity, "")
	if err != nil {
		t.Fatalf("reopening failed: %v", err)
	}
	if second.Created {
		t.Error("reopening reported creating a repository")
	}
	if second.RepositoryID != first.RepositoryID ||
		second.WorkspaceID != first.WorkspaceID ||
		second.ThreadID != first.ThreadID ||
		second.SessionID != first.SessionID {
		t.Errorf("reopening landed somewhere else:\nfirst  %+v\nsecond %+v", first, second)
	}

	access, err := repositories.ReadFrontendRouteAccess(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(access.AccessibleRepositories) != 1 {
		t.Errorf("two starts produced %d repositories, want 1",
			len(access.AccessibleRepositories))
	}
	if len(access.AccessibleThreads) != 1 {
		t.Errorf("two starts produced %d threads, want 1", len(access.AccessibleThreads))
	}
}

func TestOpeningRefusesWhatItCannotBindAChangeTo(t *testing.T) {
	repositories := openTestRepositories(t)
	path, identity := bootstrapFixture(t)
	for name, attempt := range map[string]struct{ path, identity string }{
		"a relative path":  {"work/orders", identity},
		"no path at all":   {"   ", identity},
		"no Git identity":  {path, "  "},
		"an empty request": {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			// A recorded path that is not a repository, or one with no base
			// revision, produces a workspace whose every task fails at its
			// first command. Refusing here is refusing at the only moment a
			// person can still do something about it.
			if _, err := repositories.EnsureLocalBootstrap(
				t.Context(), attempt.path, attempt.identity, "",
			); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

func TestAnInterruptedOpenIsCompletedByTheNextOne(t *testing.T) {
	// A start killed between writing the repository and writing its thread
	// leaves a workspace with nowhere to talk. Handing that back unchanged
	// would give the browser a workspace and no conversation, which is the
	// disconnected interface this whole path exists to prevent.
	repositories := openTestRepositories(t)
	path, identity := bootstrapFixture(t)
	scope, err := repositories.EnsureLocalBootstrap(t.Context(), path, identity, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		t.Context(), "DELETE FROM threads WHERE id = ?", scope.ThreadID,
	); err != nil {
		t.Fatal(err)
	}

	repaired, err := repositories.EnsureLocalBootstrap(t.Context(), path, identity, "")
	if err != nil {
		t.Fatalf("reopening an interrupted scope failed: %v", err)
	}
	if repaired.ThreadID.IsZero() || repaired.SessionID.IsZero() {
		t.Fatal("reopening left the scope without a conversation")
	}
	if repaired.RepositoryID != scope.RepositoryID {
		t.Error("reopening abandoned the existing repository")
	}
	access, err := repositories.ReadFrontendRouteAccess(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if access.SelectedSessionID != repaired.SessionID {
		t.Error("the repaired session is not the one published to the browser")
	}
}
