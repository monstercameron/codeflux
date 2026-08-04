package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAFailedPatchIsAnsweredWithTheFileItDidNotMatch is the answer the refusal
// was already telling the run to go and find.
//
// The refusal says to read the file and copy the lines from it. The run cannot:
// the file is put in front of it once, when the attempt begins, and every patch
// that lands after that turns the copy it holds into a description of a file
// that no longer exists. So it retypes the lines from memory, misses again, and
// spends another round on the same mistake.
//
// Ladder rung 3 on 2026-08-03: thirty of forty patch calls failed, which at
// roughly five seconds of model latency each was most of a 371-second run.
func TestAFailedPatchIsAnsweredWithTheFileItDidNotMatch(t *testing.T) {
	worktree := t.TempDir()
	source := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	writeEchoFixture(t, worktree, "cmd/generated/main.go", source)
	narrator := &narratingExecutor{worktree: worktree}

	answer := narrator.currentContentOf("cmd/generated/main.go")
	if !strings.Contains(answer, source) {
		t.Fatalf("the file's current text was not offered back:\n%s", answer)
	}
	// It has to say why this text differs from the one the run was shown, or a
	// run holding two copies has no reason to prefer this one.
	if !strings.Contains(answer, "as it stands now") ||
		!strings.Contains(answer, "not the copy you were shown") {
		t.Errorf("the answer does not say which copy is authoritative:\n%s",
			answer)
	}
}

// TestTheFailedPatchAnswerStaysInsideTheWorktree is the security control.
//
// The path comes from a model tool argument, so it is untrusted. This reads on
// the failure path of a call that may have been refused for any reason,
// including for escaping, so it confines the path itself rather than assuming
// the executor already did.
func TestTheFailedPatchAnswerStaysInsideTheWorktree(t *testing.T) {
	worktree := t.TempDir()
	outside := filepath.Join(filepath.Dir(worktree), "outside-secret.go")
	if err := os.WriteFile(outside, []byte("package secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	narrator := &narratingExecutor{worktree: worktree}

	for _, path := range []string{
		"../outside-secret.go",
		"cmd/../../outside-secret.go",
	} {
		if answer := narrator.currentContentOf(path); answer != "" {
			t.Errorf("a path escaping the worktree was read back: %q -> %q",
				path, answer)
		}
	}
}

// TestAnUnreadableFileIsNotAnsweredWithSomethingElse is the second control.
//
// A patch failure that also cannot show the file is still a patch failure.
// Saying nothing is right; saying something that is not the file would send the
// run to write hunks against text that does not exist, which is the defect this
// change exists to remove.
func TestAnUnreadableFileIsNotAnsweredWithSomethingElse(t *testing.T) {
	narrator := &narratingExecutor{worktree: t.TempDir()}
	if answer := narrator.currentContentOf("cmd/generated/absent.go"); answer != "" {
		t.Errorf("a file that is not there produced an answer: %q", answer)
	}
	if answer := narrator.currentContentOf(""); answer != "" {
		t.Errorf("an empty path produced an answer: %q", answer)
	}
	// A file past the echo bound is left out rather than truncated: half a file
	// is worse than none, because a hunk copied from it matches nothing.
	big := t.TempDir()
	writeEchoFixture(t, big, "big.go",
		strings.Repeat("// filler\n", maximumPatchEchoBytes/10+64))
	oversized := &narratingExecutor{worktree: big}
	if answer := oversized.currentContentOf("big.go"); answer != "" {
		t.Errorf("a file past the echo bound was sent anyway (%d bytes)",
			len(answer))
	}
}

// writeEchoFixture writes one file into a fixture worktree.
func writeEchoFixture(t *testing.T, worktree, path, content string) {
	t.Helper()
	full := filepath.Join(worktree, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
