package transport

import (
	"context"
	"errors"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stubWorkspaceApplication answers the narrow surface the service reads.
type stubWorkspaceApplication struct {
	repositories []RepositoryRecord
	scope        WorkspaceRecord
	err          error
	requested    int
	// gitState is what the working tree says, and gitErr is Git declining to
	// answer. They are separate from err because an inspection must still
	// return the repository when only the live read fails.
	gitState RepositoryGitState
	gitErr   error
}

func (stub *stubWorkspaceApplication) ReadRepositoryState(
	_ context.Context, _ RepositoryRecord,
) (RepositoryGitState, error) {
	return stub.gitState, stub.gitErr
}

func (stub *stubWorkspaceApplication) ListRepositories(
	_ context.Context, limit int,
) ([]RepositoryRecord, error) {
	stub.requested = limit
	return stub.repositories, stub.err
}

func (stub *stubWorkspaceApplication) GetRepository(
	_ context.Context, _ domain.RepositoryID,
) (RepositoryRecord, error) {
	if stub.err != nil {
		return RepositoryRecord{}, stub.err
	}
	if len(stub.repositories) == 0 {
		return RepositoryRecord{}, ErrWorkspaceTargetNotFound
	}
	return stub.repositories[0], nil
}

func (stub *stubWorkspaceApplication) GetWorkspaceScope(
	_ context.Context, _ domain.WorkspaceID,
) (WorkspaceRecord, error) {
	if stub.err != nil {
		return WorkspaceRecord{}, stub.err
	}
	return stub.scope, nil
}

func fixtureRepository(t *testing.T, path, identity string) RepositoryRecord {
	t.Helper()
	id, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	return RepositoryRecord{ID: id, CanonicalPath: path, GitIdentity: identity, Revision: 3}
}

func TestTheWorkspaceServiceRequiresAnApplication(t *testing.T) {
	if _, err := NewWorkspaceService(nil); err == nil {
		t.Fatal("a workspace service with no application was built")
	}
}

func TestARepositoryIsNamedByItsDirectoryNotItsWholePath(t *testing.T) {
	// A picker showing four absolute paths that differ only in their last
	// segment is a picker nobody can read.
	service, err := NewWorkspaceService(&stubWorkspaceApplication{
		repositories: []RepositoryRecord{
			fixtureRepository(t, "/home/person/work/orders-service", strings.Repeat("a", 40)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.ListRepositories(
		context.Background(), &codefluxv1.ListRepositoriesRequest{})
	if err != nil {
		t.Fatalf("listing repositories failed: %v", err)
	}
	if len(response.GetRepositories()) != 1 {
		t.Fatalf("listed %d repositories, want 1", len(response.GetRepositories()))
	}
	summary := response.GetRepositories()[0]
	if summary.GetDisplayName().GetValue() != "orders-service" {
		t.Errorf("display name = %q, want the directory name",
			summary.GetDisplayName().GetValue())
	}
	if summary.GetRepositoryId().GetKind() !=
		codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY {
		t.Error("the summary does not carry a repository identity")
	}
}

func TestNoSummaryCarriesAPathAClientCouldActOn(t *testing.T) {
	// An absolute path in a view is a path a client could act on, and nothing
	// outside the coordinator is allowed to.
	service, _ := NewWorkspaceService(&stubWorkspaceApplication{
		repositories: []RepositoryRecord{
			fixtureRepository(t, "/home/person/secret-project", strings.Repeat("b", 40)),
		},
	})
	response, err := service.ListRepositories(
		context.Background(), &codefluxv1.ListRepositoriesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	root := response.GetRepositories()[0].GetRoot().GetWorkspaceRelativeSlashPath()
	if strings.Contains(root, "/home/") || strings.HasPrefix(root, "/") {
		t.Errorf("a summary carries an absolute path: %q", root)
	}
}

func TestTheListIsBounded(t *testing.T) {
	// A list with no bound eventually returns everything at once, which for a
	// picker means a page that takes longer to render the longer somebody has
	// used the product.
	stub := &stubWorkspaceApplication{}
	service, _ := NewWorkspaceService(stub)
	if _, err := service.ListRepositories(context.Background(),
		&codefluxv1.ListRepositoriesRequest{
			Page: &codefluxv1.PageRequest{Limit: 5},
		}); err != nil {
		t.Fatal(err)
	}
	if stub.requested != 5 {
		t.Errorf("the requested limit was %d, want the caller's 5", stub.requested)
	}
}

func TestInspectionWarnsAboutWhatWouldBiteLater(t *testing.T) {
	// A repository with no Git identity cannot bind a change to a base
	// revision. Finding that out after starting a task is finding it out too
	// late.
	service, _ := NewWorkspaceService(&stubWorkspaceApplication{
		repositories: []RepositoryRecord{fixtureRepository(t, "relative/path", "  ")},
	})
	id, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.InspectRepository(context.Background(),
		&codefluxv1.InspectRepositoryRequest{
			RepositoryId: &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
				Value: id.String(),
			},
		})
	if err != nil {
		t.Fatalf("inspection failed: %v", err)
	}
	warnings := response.GetWarnings()
	if len(warnings) != 2 {
		t.Fatalf("inspection produced %d warning(s), want one per problem", len(warnings))
	}
	joined := ""
	for _, warning := range warnings {
		joined += warning.GetValue() + "\n"
	}
	for _, expected := range []string{"Git identity", "not absolute"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("inspection did not warn about %q:\n%s", expected, joined)
		}
	}
}

func TestAMissingRepositoryIsNotFoundRatherThanInternal(t *testing.T) {
	// The two mean different things to a caller: one is a bad identity, the
	// other is a broken coordinator.
	service, _ := NewWorkspaceService(&stubWorkspaceApplication{})
	id, _ := domain.NewRepositoryID()
	_, err := service.InspectRepository(context.Background(),
		&codefluxv1.InspectRepositoryRequest{
			RepositoryId: &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
				Value: id.String(),
			},
		})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("a missing repository reported %s", status.Code(err))
	}

	broken, _ := NewWorkspaceService(&stubWorkspaceApplication{err: errors.New("disk gone")})
	_, err = broken.InspectRepository(context.Background(),
		&codefluxv1.InspectRepositoryRequest{
			RepositoryId: &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
				Value: id.String(),
			},
		})
	if status.Code(err) != codes.Internal {
		t.Fatalf("a broken read reported %s", status.Code(err))
	}
	// The underlying error must not reach the caller: a storage message can
	// carry a path.
	if strings.Contains(status.Convert(err).Message(), "disk gone") {
		t.Error("the internal failure leaked its own message")
	}
}

func TestOpeningAWorkspaceIsRefusedRatherThanFaked(t *testing.T) {
	// Opening a workspace grants a coordinator authority over a directory.
	// Returning one here would be the coordinator claiming an authority
	// nobody granted.
	service, _ := NewWorkspaceService(&stubWorkspaceApplication{})
	_, err := service.OpenWorkspace(context.Background(),
		&codefluxv1.OpenWorkspaceRequest{SelectedPath: "/anything"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("opening a workspace reported %s, want FailedPrecondition", status.Code(err))
	}
	if !strings.Contains(status.Convert(err).Message(), "approval") {
		t.Errorf("the refusal does not say what is missing: %s", status.Convert(err).Message())
	}
}

func TestAnInvalidIdentityIsRejectedBeforeAnyRead(t *testing.T) {
	stub := &stubWorkspaceApplication{}
	service, _ := NewWorkspaceService(stub)
	if _, err := service.InspectRepository(context.Background(),
		&codefluxv1.InspectRepositoryRequest{}); err == nil {
		t.Error("an inspection with no repository identity was accepted")
	}
	if _, err := service.GetWorkspaceState(context.Background(),
		&codefluxv1.GetWorkspaceStateRequest{}); err == nil {
		t.Error("a workspace read with no identity was accepted")
	}
}

// The compile-time proof that lived here named *storage.Repositories, which is
// the dependency this adapter is not allowed to have. The composition that does
// satisfy this port is asserted where it is built, in the coordinator.

func TestInspectionReportsTheWorkingTreeGitReportsNow(t *testing.T) {
	// The stored row records what was true when a repository was opened. A top
	// bar drawn from it says "main / uncommitted changes" about a tree that has
	// since been committed and moved to another branch.
	stub := &stubWorkspaceApplication{
		repositories: []RepositoryRecord{
			fixtureRepository(t, "/home/person/work/orders", strings.Repeat("a", 40)),
		},
		gitState: RepositoryGitState{
			Branch: "release/4.2", HeadRevision: strings.Repeat("c", 40),
			Dirty: true, ChangedPathCount: 7,
		},
	}
	service, _ := NewWorkspaceService(stub)
	id, _ := domain.NewRepositoryID()
	response, err := service.InspectRepository(context.Background(),
		&codefluxv1.InspectRepositoryRequest{
			RepositoryId: &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
				Value: id.String(),
			},
		})
	if err != nil {
		t.Fatalf("inspection failed: %v", err)
	}
	git := response.GetRepository().GetGit()
	if git.GetBranch() != "release/4.2" {
		t.Errorf("branch = %q, want the branch Git reports", git.GetBranch())
	}
	if !git.GetDirty() || git.GetChangedPathCount() != 7 {
		t.Errorf("working tree reported dirty=%v count=%d, want true and 7",
			git.GetDirty(), git.GetChangedPathCount())
	}
	if git.GetHeadRevision() == strings.Repeat("a", 40) {
		t.Error("the head revision came from the stored row rather than from Git")
	}
}

func TestAnUnreadableWorkingTreeStillReturnsTheRepository(t *testing.T) {
	// A working-tree read fails for ordinary reasons — the directory moved, Git
	// is mid-rebase — and none of them are a reason to refuse the whole
	// inspection. Only the live fields go unanswered.
	service, _ := NewWorkspaceService(&stubWorkspaceApplication{
		repositories: []RepositoryRecord{
			fixtureRepository(t, "/home/person/work/orders", strings.Repeat("a", 40)),
		},
		gitErr: errors.New("git is unavailable"),
	})
	id, _ := domain.NewRepositoryID()
	response, err := service.InspectRepository(context.Background(),
		&codefluxv1.InspectRepositoryRequest{
			RepositoryId: &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
				Value: id.String(),
			},
		})
	if err != nil {
		t.Fatalf("an unreadable working tree refused the whole inspection: %v", err)
	}
	if response.GetRepository().GetDisplayName().GetValue() != "orders" {
		t.Error("the repository identity was lost with the live read")
	}
	if branch := response.GetRepository().GetGit().GetBranch(); branch != "" {
		t.Errorf("branch = %q; an unanswered field must stay unanswered", branch)
	}
}
