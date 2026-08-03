package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commitMsgHookFixtureChangelog and commitMsgHookFixtureDevlog are minimal
// ledgers, anchored the same way the real CHANGELOG/DEVLOG are, declaring
// exactly one identifier each: CL-20260802-001 and DL-20260802-001.
const (
	commitMsgHookFixtureChangelog = "TITLE\n\nEntries\n-------\nChange-ID: CL-20260802-001\nCommit: pending\n"
	commitMsgHookFixtureDevlog    = "TITLE\n\nEntries\n-------\nDev-Log: DL-20260802-001\nStatus: ok\n"
)

// setUpCommitMsgHookRepository creates an isolated Git repository, points
// its hooksPath at this repository's real .githooks directory -- the actual
// production hook under test, not a copy -- and stages a fixture CHANGELOG
// and DEVLOG declaring exactly one identifier each.
func setUpCommitMsgHookRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	hooksPath := filepath.Join(repositoryRootForCommandGraph(t), ".githooks")

	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-q")
	runGitCommand(t, repo, "config", "user.email", "commit-msg-hook-test@example.com")
	runGitCommand(t, repo, "config", "user.name", "commit-msg-hook-test")
	// core.hooksPath accepts forward slashes on Windows and needs no further
	// escaping; filepath.ToSlash keeps this portable.
	runGitCommand(t, repo, "config", "core.hooksPath", filepath.ToSlash(hooksPath))

	writeTestFile(t, filepath.Join(repo, "CHANGELOG"), commitMsgHookFixtureChangelog)
	writeTestFile(t, filepath.Join(repo, "DEVLOG"), commitMsgHookFixtureDevlog)
	runGitCommand(t, repo, "add", "CHANGELOG", "DEVLOG")
	return repo
}

func runGitCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output.String())
	}
	return output.String()
}

// attemptCommitMsgHookCommit runs `git commit -m <message>` in repo and
// returns the combined output and whether the commit succeeded, without
// failing the test -- the caller decides whether success or failure is
// expected.
func attemptCommitMsgHookCommit(t *testing.T, repo, message string) (string, bool) {
	t.Helper()
	command := exec.Command("git", "commit", "-m", message)
	command.Dir = repo
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.String(), err == nil
}

// TestCommitMsgHookFiresOnMissingTrailers is the discriminating regression
// for REPO-020: a commit message that carries neither ledger trailer must
// be refused by the real .githooks/commit-msg hook, driven end to end
// through an actual `git commit`.
func TestCommitMsgHookFiresOnMissingTrailers(t *testing.T) {
	repo := setUpCommitMsgHookRepository(t)
	output, ok := attemptCommitMsgHookCommit(t, repo, "a change with no trailers at all")
	if ok {
		t.Fatalf("commit with no ledger trailers was accepted; output:\n%s", output)
	}
	if !strings.Contains(output, "Change-Log") {
		t.Fatalf("missing-trailer output = %q, want it to name the missing Change-Log trailer", output)
	}
}

func TestCommitMsgHookFiresOnMalformedTrailer(t *testing.T) {
	repo := setUpCommitMsgHookRepository(t)
	message := "a change\n\nChange-Log: CL-2026080-001\nDev-Log: DL-20260802-001\n"
	output, ok := attemptCommitMsgHookCommit(t, repo, message)
	if ok {
		t.Fatalf("commit with a malformed Change-Log trailer was accepted; output:\n%s", output)
	}
	if !strings.Contains(output, "does not match") {
		t.Fatalf("malformed-trailer output = %q, want a shape-mismatch finding", output)
	}
}

func TestCommitMsgHookFiresOnIdentifierWithNoLedgerEntry(t *testing.T) {
	repo := setUpCommitMsgHookRepository(t)
	message := "a change\n\nChange-Log: CL-20260802-999\nDev-Log: DL-20260802-001\n"
	output, ok := attemptCommitMsgHookCommit(t, repo, message)
	if ok {
		t.Fatalf("commit referencing a nonexistent CHANGELOG entry was accepted; output:\n%s", output)
	}
	if !strings.Contains(output, "CL-20260802-999") || !strings.Contains(output, "CHANGELOG") {
		t.Fatalf("no-entry output = %q, want it to name the missing CHANGELOG identifier", output)
	}
}

// TestCommitMsgHookPassesOnValidTrailers is the required non-vacuous
// positive case: a message carrying both trailers, each matching an
// identifier actually declared in the staged ledgers, must be accepted and
// produce a real commit.
func TestCommitMsgHookPassesOnValidTrailers(t *testing.T) {
	repo := setUpCommitMsgHookRepository(t)
	message := "a change\n\nChange-Log: CL-20260802-001\nDev-Log: DL-20260802-001\n"
	output, ok := attemptCommitMsgHookCommit(t, repo, message)
	if !ok {
		t.Fatalf("commit with valid, resolvable trailers was refused; output:\n%s", output)
	}
	log := runGitCommand(t, repo, "log", "-1", "--format=%s%n%b")
	if !strings.Contains(log, "Change-Log: CL-20260802-001") || !strings.Contains(log, "Dev-Log: DL-20260802-001") {
		t.Fatalf("committed message = %q, want both trailers preserved", log)
	}
}

// TestCommitMsgHookReproducesTheHistoricalUnfixableViolation documents the
// worked example that motivated REPO-020: CL-20260802-018 is a released
// CHANGELOG entry whose commit (92f1eed) was swept into a checkpoint commit
// carrying no Change-Log trailer at all -- `git log
// --grep="Change-Log: CL-20260802-018"` in the real repository returns
// nothing. That history cannot be repaired (the commit would have to be
// rewritten), which is exactly why this rule is forward-looking only: it
// gates the commit being made, in commit-msg, and never rescans history.
// This test reproduces the same shape -- a checkpoint-style message with no
// trailer -- and confirms the hook would have refused it.
func TestCommitMsgHookReproducesTheHistoricalUnfixableViolation(t *testing.T) {
	repo := setUpCommitMsgHookRepository(t)
	message := "CHECKPOINT: commit the working tree in full at explicit user request\n\n" +
		"This is a checkpoint commit, not a feature commit.\n"
	output, ok := attemptCommitMsgHookCommit(t, repo, message)
	if ok {
		t.Fatalf("a trailer-less checkpoint message was accepted; output:\n%s", output)
	}
	if !strings.Contains(output, "Change-Log") {
		t.Fatalf("checkpoint-shaped output = %q, want it to name the missing Change-Log trailer", output)
	}
}
