package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnrichedBytesAreWhatAReaderFetches is the defect rung 5 failed on.
//
// Enrichment edited the verified worktree and stopped, so the artifact record
// held the version before enrichment. The ladder read the stored bytes, found
// the earlier program, and failed a run whose code was correct on disk. Nothing
// about that was visible from any single record: each was accurate about its
// own subject.
func TestEnrichedBytesAreWhatAReaderFetches(t *testing.T) {
	execution, scope := recallFixture(t)
	execution.redactor = registrationTestRedactionPipeline(t)
	worktree := recallWorktree(t, undocumentedAtomSource)
	scope.worktree = worktree

	produced, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deriveAtomDocumentation(worktree, produced); err != nil {
		t.Fatalf("deriving the schema failed: %v", err)
	}
	if stored := execution.recordEnrichedSource(
		context.Background(), scope,
	); stored == 0 {
		t.Fatal("the enriched source was not recorded, so a reader fetching " +
			"this task's work would get the version before enrichment")
	}

	onDisk, err := os.ReadFile(filepath.Join(worktree, "reserve", "funds.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "//codeflux:atom") {
		t.Fatal("the worktree does not hold the enriched source")
	}
	artifacts, err := execution.repositories.ListProjectSourceArtifactsExcludingTask(
		context.Background(), scope.projectID, scope.taskID, 50)
	if err != nil {
		t.Fatalf("the artifact record could not be read: %v", err)
	}
	found := false
	for _, artifact := range artifacts {
		if !strings.Contains(string(artifact.Content), "//codeflux:atom") {
			continue
		}
		found = true
		if string(artifact.Content) != string(onDisk) {
			t.Errorf("the stored bytes differ from the worktree:\n"+
				"stored %d bytes, on disk %d",
				len(artifact.Content), len(onDisk))
		}
	}
	if !found {
		t.Error("no stored artifact carries the derived documentation, so the " +
			"record lags the worktree")
	}
}

// TestAnEnrichedRevisionGetsItsOwnDigest covers the second half: the terminal
// record must name the revision that exists.
//
// Rung 5 reported "the worktree is NOT verified revision 8bef148ac7cd" after
// enriching it, which was true and useless — the enriched tree was verified, it
// had just never been checkpointed.
func TestAnEnrichedRevisionGetsItsOwnDigest(t *testing.T) {
	worktree := recallWorktree(t, undocumentedAtomSource)
	checkpoint := newVerifiedCheckpoint(worktree)
	if err := checkpoint.capture(worktree, "tests passed"); err != nil {
		t.Fatal(err)
	}
	before := checkpoint.digest

	produced, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deriveAtomDocumentation(worktree, produced); err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.capture(worktree, "documented"); err != nil {
		t.Fatal(err)
	}
	if checkpoint.digest == before {
		t.Error("the enriched revision carries the pre-enrichment digest, so " +
			"the record names a tree that no longer exists")
	}
	if producedTreeDigest(worktree) != checkpoint.digest {
		t.Error("the checkpoint digest does not describe the worktree")
	}
}

// TestBrokenEnrichmentLeavesTheVerifiedRevision is the revert half. A registry
// row is worth having and is not worth a working program.
func TestBrokenEnrichmentLeavesTheVerifiedRevision(t *testing.T) {
	worktree := recallWorktree(t, undocumentedAtomSource)
	checkpoint := newVerifiedCheckpoint(worktree)
	if err := checkpoint.capture(worktree, "tests passed"); err != nil {
		t.Fatal(err)
	}
	good := producedTreeDigest(worktree)

	// Whatever broke it — a bad insertion point, a failing suite afterwards —
	// the run must end on the revision it had.
	if err := os.WriteFile(filepath.Join(worktree, "reserve", "funds.go"),
		[]byte("package reserve\nthis does not parse\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !checkpoint.restore(worktree) {
		t.Fatal("the verified revision was not restored")
	}
	if producedTreeDigest(worktree) != good {
		t.Error("the worktree is not the revision that was verified")
	}
	body, err := os.ReadFile(filepath.Join(worktree, "reserve", "funds.go"))
	if err != nil || strings.Contains(string(body), "does not parse") {
		t.Errorf("the broken enrichment survived the revert: %q", body)
	}
}
