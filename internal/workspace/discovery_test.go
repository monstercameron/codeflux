package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDiscoverRepositoryCanonicalCleanSnapshot(t *testing.T) {
	t.Parallel()

	root := newTestRepository(t)
	writeTestFile(t, root, "nested/package.go", "package nested\n")
	testGit(t, root, "add", "nested/package.go")
	testGit(t, root, "commit", "-m", "add nested package")
	testGit(t, root, "remote", "add", "origin", "https://secret:token@example.com/team/repository.git")
	head := testGit(t, root, "rev-parse", "HEAD")

	snapshot, err := DiscoverRepository(t.Context(), filepath.Join(root, "nested"), ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err = filepath.Abs(canonicalRoot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CanonicalRoot != canonicalRoot {
		t.Fatalf("root = %q; want %q", snapshot.CanonicalRoot, canonicalRoot)
	}
	if snapshot.HeadRevision != head || !validRevision(snapshot.HeadRevision) {
		t.Fatalf("HEAD = %q; want %q", snapshot.HeadRevision, head)
	}
	if snapshot.Branch == "" || snapshot.Detached || snapshot.Dirty || snapshot.Conflicted {
		t.Fatalf("unexpected clean state: %+v", snapshot)
	}
	if !strings.HasPrefix(snapshot.GitIdentity, "git-root-sha256:") {
		t.Fatalf("Git identity = %q", snapshot.GitIdentity)
	}
	if len(snapshot.Remotes) != 1 ||
		snapshot.Remotes[0].URL != "https://redacted@example.com/team/repository.git" {
		t.Fatalf("sanitized remotes = %+v", snapshot.Remotes)
	}
	if len(snapshot.Warnings) != 0 {
		t.Fatalf("clean warnings = %v", snapshot.Warnings)
	}
}

func TestRepositoryIdentitySurvivesPathMove(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	root := filepath.Join(parent, "original")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	initializeTestRepository(t, root)
	before, err := DiscoverRepository(t.Context(), root, ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(parent, "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	after, err := DiscoverRepository(t.Context(), moved, ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if before.GitIdentity != after.GitIdentity {
		t.Fatalf("identity changed after move: %q -> %q", before.GitIdentity, after.GitIdentity)
	}
	if before.CanonicalRoot == after.CanonicalRoot {
		t.Fatal("test did not move the canonical path")
	}
}

func TestDiscoverRepositoryDirtyIgnoredDetachedAndContentMarkers(t *testing.T) {
	t.Parallel()

	root := newTestRepository(t)
	writeTestFile(t, root, ".gitignore", "ignored.txt\n")
	writeTestFile(
		t,
		root,
		".gitmodules",
		"[submodule \"library\"]\n\tpath = deps/library\n\turl = https://example.com/library.git\n",
	)
	writeTestFile(
		t,
		root,
		"large.bin",
		"version https://git-lfs.github.com/spec/v1\noid sha256:0123456789abcdef\nsize 1024\n",
	)
	testGit(t, root, "add", ".gitignore", ".gitmodules", "large.bin")
	testGit(t, root, "commit", "-m", "add repository metadata")
	writeTestFile(t, root, "untracked.txt", "untracked\n")
	writeTestFile(t, root, "ignored.txt", "ignored\n")
	writeTestFile(t, root, "nested/.git/HEAD", "ref: refs/heads/main\n")
	testGit(t, root, "checkout", "--detach")

	snapshot, err := DiscoverRepository(t.Context(), root, ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Detached || !snapshot.Dirty {
		t.Fatalf("detached/dirty = %v/%v", snapshot.Detached, snapshot.Dirty)
	}
	if !slices.Contains(snapshot.UntrackedPaths, "untracked.txt") {
		t.Fatalf("untracked = %v", snapshot.UntrackedPaths)
	}
	if !slices.Contains(snapshot.IgnoredPaths, "ignored.txt") {
		t.Fatalf("ignored = %v", snapshot.IgnoredPaths)
	}
	if !slices.Contains(snapshot.Submodules, "deps/library") || snapshot.SubmodulesSupported {
		t.Fatalf("submodule decision = %v supported=%v", snapshot.Submodules, snapshot.SubmodulesSupported)
	}
	if !slices.Contains(snapshot.NestedRepositories, "nested") {
		t.Fatalf("nested repositories = %v", snapshot.NestedRepositories)
	}
	if !slices.Contains(snapshot.LFSPointers, "large.bin") {
		t.Fatalf("LFS pointers = %v", snapshot.LFSPointers)
	}
	for _, warning := range []string{
		"dirty-worktree",
		"detached-head",
		"submodules-unsupported",
		"nested-repositories",
		"git-lfs-content-not-fetched",
		"ignored-files-observed",
	} {
		if !slices.Contains(snapshot.Warnings, warning) {
			t.Errorf("warnings %v lack %q", snapshot.Warnings, warning)
		}
	}
}

func TestDiscoverRepositoryChangedAndConflictedStatus(t *testing.T) {
	t.Parallel()

	root := newTestRepository(t)
	writeTestFile(t, root, "README.txt", "changed\n")
	snapshot, err := DiscoverRepository(t.Context(), root, ExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Dirty || len(snapshot.ChangedPaths) != 1 ||
		snapshot.ChangedPaths[0].Path != "README.txt" {
		t.Fatalf("changed paths = %+v", snapshot.ChangedPaths)
	}

	conflicted := RepositorySnapshot{}
	parseStatus([]byte("u UU N... 100644 100644 100644 100644 a b c path/with space.go\x00"), &conflicted)
	if !conflicted.Dirty || !conflicted.Conflicted ||
		len(conflicted.ChangedPaths) != 1 ||
		conflicted.ChangedPaths[0].Path != "path/with space.go" {
		t.Fatalf("conflict parse = %+v", conflicted)
	}
}

func TestOperationStates(t *testing.T) {
	t.Parallel()

	root := newTestRepository(t)
	gitDirectory := testGit(t, root, "rev-parse", "--path-format=absolute", "--git-dir")
	head := testGit(t, root, "rev-parse", "HEAD")
	writeAbsoluteTestFile(t, filepath.Join(gitDirectory, "MERGE_HEAD"), head+"\n")
	if err := os.Mkdir(filepath.Join(gitDirectory, "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeAbsoluteTestFile(t, filepath.Join(gitDirectory, "CHERRY_PICK_HEAD"), head+"\n")
	writeAbsoluteTestFile(t, filepath.Join(gitDirectory, "BISECT_LOG"), "git bisect start\n")

	states, err := operationStates(t.Context(), ExecRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(states, []string{"merge", "rebase", "cherry-pick", "bisect"}) {
		t.Fatalf("operation states = %v", states)
	}
}

func TestDiscoverRepositoryRejectsInvalidSelections(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := DiscoverRepository(t.Context(), missing, ExecRunner{}); !errors.Is(err, ErrPathNotFound) {
		t.Fatalf("missing error = %v", err)
	}
	file := filepath.Join(t.TempDir(), "file")
	writeAbsoluteTestFile(t, file, "not a directory")
	if _, err := DiscoverRepository(t.Context(), file, ExecRunner{}); !errors.Is(err, ErrPathNotDir) {
		t.Fatalf("file error = %v", err)
	}
	directory := t.TempDir()
	if _, err := DiscoverRepository(t.Context(), directory, ExecRunner{}); !errors.Is(err, ErrNotGit) {
		t.Fatalf("non-Git error = %v", err)
	}

	emptyRepository := t.TempDir()
	testGit(t, emptyRepository, "init", "--initial-branch=main")
	if _, err := DiscoverRepository(t.Context(), emptyRepository, ExecRunner{}); !errors.Is(err, ErrNoRevision) {
		t.Fatalf("empty-repository error = %v", err)
	}
}

func TestResolveGitRootRejectsRunnerEscape(t *testing.T) {
	t.Parallel()

	selected := t.TempDir()
	outside := t.TempDir()
	runner := commandRunnerFunc(func(
		context.Context,
		string,
		string,
		...string,
	) (CommandResult, error) {
		return CommandResult{Stdout: []byte(outside + "\n")}, nil
	})
	if _, err := resolveGitRoot(t.Context(), selected, runner); !errors.Is(err, ErrInvalidGitRoot) {
		t.Fatalf("unsafe root error = %v", err)
	}
}

func TestExecRunnerBoundsOutputAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	if _, err := (ExecRunner{MaxOutputBytes: 1}).Run(t.Context(), t.TempDir(), "git", "--version"); !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("output limit error = %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (ExecRunner{}).Run(cancelled, t.TempDir(), "git", "--version"); err == nil {
		t.Fatal("cancelled command unexpectedly succeeded")
	}
}

func newTestRepository(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	initializeTestRepository(t, root)
	return root
}

func initializeTestRepository(t *testing.T, root string) {
	t.Helper()

	testGit(t, root, "init", "--initial-branch=main")
	testGit(t, root, "config", "user.name", "Codeflux Test")
	testGit(t, root, "config", "user.email", "codeflux@example.invalid")
	writeTestFile(t, root, "README.txt", "initial\n")
	testGit(t, root, "add", "README.txt")
	testGit(t, root, "commit", "-m", "initial")
}

func testGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()

	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()

	writeAbsoluteTestFile(t, filepath.Join(root, filepath.FromSlash(relative)), content)
}

func writeAbsoluteTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type commandRunnerFunc func(context.Context, string, string, ...string) (CommandResult, error)

func (function commandRunnerFunc) Run(
	ctx context.Context,
	directory string,
	executable string,
	arguments ...string,
) (CommandResult, error) {
	return function(ctx, directory, executable, arguments...)
}
