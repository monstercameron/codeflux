package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAFailedRefinementDoesNotBecomeTheNextBaseline is the transactional rule.
//
// Ladder rung 5 had a correct, tested, accepted program at 33.8 seconds,
// changed its output while adding two blind-spot tests, and then spent three
// more attempts repairing a descendant of the broken version — losing a
// function its tests named, drifting a signature, and referencing an undefined
// identifier, none of which existed in the program it started from.
func TestAFailedRefinementDoesNotBecomeTheNextBaseline(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "worktree")
	writeCheckpointFile(t, worktree, "main.go", "package main // good\n")

	checkpoint := newVerifiedCheckpoint(worktree)
	if err := checkpoint.capture(worktree, "tests passed"); err != nil {
		t.Fatalf("capture failed: %v", err)
	}

	// A refinement breaks it.
	writeCheckpointFile(t, worktree, "main.go", "package main // broken\n")
	writeCheckpointFile(t, worktree, "stray.go", "package main // added\n")

	note := discardRefinement(checkpoint, worktree, nil)
	if note == "" {
		t.Fatal("nothing was discarded, so the next attempt inherits the break")
	}
	body, err := os.ReadFile(filepath.Join(worktree, "main.go"))
	if err != nil || !strings.Contains(string(body), "good") {
		t.Errorf("the worktree still holds the broken revision: %q", body)
	}
	if _, err := os.Stat(filepath.Join(worktree, "stray.go")); err == nil {
		t.Error("a file the failed refinement added survived the discard")
	}
	// The run must be told, or it reads its own files and finds work it does
	// not remember doing.
	if !strings.Contains(note, "put back") || !strings.Contains(note, "gone") {
		t.Errorf("the instruction does not say the edits were discarded:\n%s", note)
	}

	// What it tried is kept as evidence rather than deleted outright.
	discarded := filepath.Join(
		filepath.Dir(worktree), filepath.Base(worktree)+".discarded")
	kept, err := os.ReadFile(filepath.Join(discarded, "main.go"))
	if err != nil || !strings.Contains(string(kept), "broken") {
		t.Errorf("the failed candidate was not preserved as evidence: %v", err)
	}
}

// TestNothingIsDiscardedWithoutSomewhereBetterToGo keeps the rule from
// destroying the only work a run has.
func TestNothingIsDiscardedWithoutSomewhereBetterToGo(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "worktree")
	writeCheckpointFile(t, worktree, "main.go", "package main // the only copy\n")

	if note := discardRefinement(newVerifiedCheckpoint(worktree), worktree, nil); note != "" {
		t.Error("a run with no verified revision discarded its work anyway")
	}
	body, err := os.ReadFile(filepath.Join(worktree, "main.go"))
	if err != nil || !strings.Contains(string(body), "only copy") {
		t.Errorf("the worktree was cleared with nothing to restore: %q", body)
	}
}
