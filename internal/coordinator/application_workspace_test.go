package coordinator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestAUDIT016_TheWorkspaceSurfacesAnswerOverTheGeneratedClient covers
// AUDIT-016, reconciling M11-004.
//
// M11-004 recorded that credential, provider, workspace, event, and transport
// services are initialized. The settings and provider halves had a real
// capability test; the workspace half had none, so "initialized" meant
// registered rather than able to answer. AUDIT-008 proved registration for
// every service; this proves this one does the work behind it.
func TestAUDIT016_TheWorkspaceSurfacesAnswerOverTheGeneratedClient(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	initializeCoordinatorGitRepository(t, repository)

	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls: &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	connection, err := grpc.NewClient(
		application.TaskControlAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := codefluxv1.NewWorkspaceServiceClient(connection)

	// Session authentication is enforced on this surface like every other.
	if _, err := client.ListRepositories(t.Context(),
		&codefluxv1.ListRepositoriesRequest{},
	); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated list = %v, want Unauthenticated", err)
	}
	ctx := metadata.AppendToOutgoingContext(
		t.Context(), transport.SessionMetadataKey, application.BrowserSessionSecret(),
	)

	// A coordinator that has opened nothing says so. An empty list is a state
	// to act on, not a failure.
	empty, err := client.ListRepositories(ctx, &codefluxv1.ListRepositoriesRequest{})
	if err != nil {
		t.Fatalf("list repositories: %v", err)
	}
	if len(empty.GetRepositories()) != 0 {
		t.Fatalf("want no repositories before one is opened, got %d",
			len(empty.GetRepositories()))
	}

	// OpenWorkspace is a deliberate refusal, not an unimplemented stub: it
	// declines rather than granting a coordinator authority over a
	// caller-supplied path that nobody durably approved. Pinning it here means
	// the day it starts answering, this test says so.
	if _, err := client.OpenWorkspace(ctx, &codefluxv1.OpenWorkspaceRequest{
		Control:      &codefluxv1.MutationControl{IdempotencyKey: "audit016-open"},
		SelectedPath: repository,
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("OpenWorkspace = %v, want a FailedPrecondition refusal", err)
	} else if !strings.Contains(err.Error(), "durable repository approval") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}

	// The repository the product can actually open today is opened the way the
	// product opens it, then read back over the generated client. This is the
	// capability M11-004 claimed and nothing verified.
	scope, err := application.repos.EnsureLocalBootstrap(
		ctx, repository, strings.Repeat("a", 40), "Audit 016")
	if err != nil {
		t.Fatalf("opening the repository failed: %v", err)
	}

	listed, err := client.ListRepositories(ctx, &codefluxv1.ListRepositoriesRequest{})
	if err != nil {
		t.Fatalf("list repositories after opening: %v", err)
	}
	if len(listed.GetRepositories()) != 1 {
		t.Fatalf("want one repository after opening, got %d", len(listed.GetRepositories()))
	}
	summary := listed.GetRepositories()[0]
	if summary.GetRepositoryId().GetValue() == "" {
		t.Fatalf("the listed repository has no identity: %+v", summary)
	}
	if summary.GetRepositoryId().GetValue() != scope.RepositoryID.String() {
		t.Fatalf("the listed repository is not the one opened: %+v", summary)
	}

	// Inspection must say whether Git actually answered. A clean tree and a
	// tree nobody could read otherwise arrive identically, and "clean" is the
	// claim somebody starts an agent on.
	inspected, err := client.InspectRepository(ctx, &codefluxv1.InspectRepositoryRequest{
		RepositoryId: summary.GetRepositoryId(),
	})
	if err != nil {
		t.Fatalf("inspect repository: %v", err)
	}
	if !inspected.GetWorkingTreeRead() {
		t.Error("inspection did not report that the working tree was read, " +
			"so a clean tree cannot be told from an unreadable one")
	}
	if inspected.GetRepository().GetRepositoryId().GetValue() !=
		summary.GetRepositoryId().GetValue() {
		t.Fatalf("inspection answered about a different repository: %+v",
			inspected.GetRepository())
	}

	// A workspace identity that was never opened is a NotFound, not an empty
	// view that reads as a workspace with nothing in it.
	if _, err := client.GetWorkspaceState(ctx, &codefluxv1.GetWorkspaceStateRequest{
		WorkspaceId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE,
			Value: "wsp-does-not-exist",
		},
	}); status.Code(err) == codes.OK {
		t.Error("an unknown workspace identity answered as though it existed")
	}
}

// TestAUDIT016_OpeningSomethingThatIsNotARepositoryIsRefused proves the
// surface validates rather than recording whatever it is handed.
func TestAUDIT016_OpeningSomethingThatIsNotARepositoryIsRefused(t *testing.T) {
	root := t.TempDir()
	notARepository := filepath.Join(root, "plain-directory")
	if err := exec.Command("git", "--version").Run(); err != nil {
		t.Skipf("git is unavailable: %v", err)
	}
	if err := os.MkdirAll(notARepository, 0o700); err != nil {
		t.Fatal(err)
	}

	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls: &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	connection, err := grpc.NewClient(
		application.TaskControlAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	ctx := metadata.AppendToOutgoingContext(
		t.Context(), transport.SessionMetadataKey, application.BrowserSessionSecret(),
	)
	_, err = codefluxv1.NewWorkspaceServiceClient(connection).OpenWorkspace(
		ctx, &codefluxv1.OpenWorkspaceRequest{
			Control:      &codefluxv1.MutationControl{IdempotencyKey: "audit016-bad"},
			SelectedPath: notARepository,
		})
	if err == nil {
		t.Fatal("a directory that is not a Git repository was opened as a workspace")
	}
	if code := status.Code(err); code != codes.InvalidArgument && code != codes.FailedPrecondition {
		t.Fatalf("refusal code = %v, want InvalidArgument or FailedPrecondition", code)
	}
	if strings.Contains(strings.ToLower(err.Error()), "panic") {
		t.Fatalf("the refusal leaked an internal failure: %v", err)
	}
}
