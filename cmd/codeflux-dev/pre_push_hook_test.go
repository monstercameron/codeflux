package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setUpPrePushHookRepositories creates a bare "remote" and a local repo whose
// core.hooksPath points at this repository's real .githooks directory -- the
// actual production hook under test, not a copy -- and seeds one pushed
// commit so subsequent pushes have a remote ref to diff against.
func setUpPrePushHookRepositories(t *testing.T) (local string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	hooksPath := filepath.Join(repositoryRootForCommandGraph(t), ".githooks")

	remote := t.TempDir()
	runGitCommand(t, remote, "init", "-q", "--bare")

	local = t.TempDir()
	runGitCommand(t, local, "init", "-q")
	runGitCommand(t, local, "config", "user.email", "pre-push-hook-test@example.com")
	runGitCommand(t, local, "config", "user.name", "pre-push-hook-test")
	// core.hooksPath accepts forward slashes on Windows and needs no further
	// escaping; filepath.ToSlash keeps this portable.
	runGitCommand(t, local, "config", "core.hooksPath", filepath.ToSlash(hooksPath))
	runGitCommand(t, local, "remote", "add", "origin", filepath.ToSlash(remote))

	writeTestFile(t, filepath.Join(local, "README"), "seed\n")
	runGitCommand(t, local, "add", "README")
	// --no-verify skips pre-commit and commit-msg for this setup commit: this
	// fixture repository has no cmd/codeflux-dev for pre-commit's lint step to
	// run, and no CHANGELOG/DEVLOG entries for commit-msg to resolve. Only
	// pre-push -- invoked at push time regardless of a commit's --no-verify --
	// is under test here.
	runGitCommand(t, local, "commit", "-q", "-m", "seed", "--no-verify")
	runGitCommand(t, local, "push", "-q", "origin", "HEAD:refs/heads/main")
	return local
}

// attemptPrePushHookPush runs `git push` in local and returns the combined
// output and whether it succeeded, without failing the test -- the caller
// decides whether success or failure is expected.
func attemptPrePushHookPush(t *testing.T, local string) (string, bool) {
	t.Helper()
	command := exec.Command("git", "push", "origin", "HEAD:refs/heads/main")
	command.Dir = local
	command.Env = os.Environ()
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.String(), err == nil
}

// TestPrePushHookSkipsWhenNoGoFilesChanged is the discriminating regression
// for AGENTS.md's "a push carrying no Go change skips the measurement":
// REPO-027 moved measurement itself behind `go run ./cmd/codeflux-dev
// test-coverage --min-coverage`, but the decision of whether this push even
// has a Go change to measure stays in the hook, computed from the
// local/remote ref pair git hands it on stdin -- test-coverage has no notion
// of a push and cannot make this call. A push touching only a non-Go file
// must succeed without ever invoking that command: if it were invoked here,
// it would fail outright, because this temporary repository is not a Go
// module and has no cmd/codeflux-dev to run.
func TestPrePushHookSkipsWhenNoGoFilesChanged(t *testing.T) {
	local := setUpPrePushHookRepositories(t)
	writeTestFile(t, filepath.Join(local, "NOTES.txt"), "not go\n")
	runGitCommand(t, local, "add", "NOTES.txt")
	runGitCommand(t, local, "commit", "-q", "-m", "notes only", "--no-verify")

	output, ok := attemptPrePushHookPush(t, local)
	if !ok {
		t.Fatalf("push touching no Go files was refused; output:\n%s", output)
	}
	if !strings.Contains(output, "no Go changes in this push") {
		t.Fatalf("output = %q, want the skip message", output)
	}
}

// TestPrePushHookMeasuresWhenGoFilesChanged is the paired non-vacuous case:
// a push that does touch a .go file must not take the skip branch. This
// repository is not a Go module, so codeflux-dev cannot actually run; the
// hook is expected to attempt it and fail for that reason, which is exactly
// what distinguishes "skipped" from "attempted measurement" here.
func TestPrePushHookMeasuresWhenGoFilesChanged(t *testing.T) {
	local := setUpPrePushHookRepositories(t)
	writeTestFile(t, filepath.Join(local, "main.go"), "package main\n\nfunc main() {}\n")
	runGitCommand(t, local, "add", "main.go")
	runGitCommand(t, local, "commit", "-q", "-m", "add a go file", "--no-verify")

	output, ok := attemptPrePushHookPush(t, local)
	if ok {
		t.Fatalf("push touching a Go file in a non-module fixture repo unexpectedly succeeded; output:\n%s", output)
	}
	if strings.Contains(output, "no Go changes in this push") {
		t.Fatalf("output = %q, a Go-file push must not take the skip branch", output)
	}
	if !strings.Contains(output, "measuring statement coverage") {
		t.Fatalf("output = %q, want the hook to have announced measurement before failing to run it", output)
	}
}
