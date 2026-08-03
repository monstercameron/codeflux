package coordinator

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAVerifiedRevisionSurvivesALaterAttemptBreakingIt is the point of the
// copy: a run that reached a good state and refined its way out of it should
// not report a failure it had already solved.
func TestAVerifiedRevisionSurvivesALaterAttemptBreakingIt(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "worktree")
	writeCheckpointFile(t, worktree, "cmd/generated/main.go", "package main\n")
	writeCheckpointFile(t, worktree, "go.mod", "module generated\n")

	checkpoint := newVerifiedCheckpoint(worktree)
	if err := checkpoint.capture(worktree, "tests passed"); err != nil {
		t.Fatalf("capturing the verified revision failed: %v", err)
	}
	if checkpoint.digest == "" {
		t.Error("a captured revision carries no identity, so nothing can name it")
	}

	// A later attempt breaks it and adds a file that was never verified.
	writeCheckpointFile(t, worktree, "cmd/generated/main.go", "this is not Go")
	writeCheckpointFile(t, worktree, "cmd/generated/extra.go", "package main\n")

	if !checkpoint.restore(worktree) {
		t.Fatal("the verified revision was not restored")
	}
	body, err := os.ReadFile(filepath.Join(worktree, "cmd/generated/main.go"))
	if err != nil || string(body) != "package main\n" {
		t.Errorf("the restored file reads %q, wanted the verified content", body)
	}
	if _, err := os.Stat(filepath.Join(worktree, "cmd/generated/extra.go")); err == nil {
		t.Error("a file the later attempt added survived the restore, so the " +
			"worktree is a state nobody verified wearing the label of one that was")
	}
}

// TestACheckpointNeverTakenIsNeverRestored guards the empty-directory case: a
// restore from nothing would read as a run that deleted its own work.
func TestACheckpointNeverTakenIsNeverRestored(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "worktree")
	writeCheckpointFile(t, worktree, "main.go", "package main\n")
	if newVerifiedCheckpoint(worktree).restore(worktree) {
		t.Error("a checkpoint that was never captured reported a restore")
	}
	if _, err := os.Stat(filepath.Join(worktree, "main.go")); err != nil {
		t.Error("the worktree was cleared by a checkpoint that held nothing")
	}
}

// TestVersionControlIsNotCheckpointed keeps .git out of the copy: restoring one
// over a live repository is actively harmful, not merely wasteful.
func TestVersionControlIsNotCheckpointed(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "worktree")
	writeCheckpointFile(t, worktree, "main.go", "package main\n")
	writeCheckpointFile(t, worktree, ".git/HEAD", "ref: refs/heads/main\n")
	writeCheckpointFile(t, worktree, "generated.exe", "binary")

	checkpoint := newVerifiedCheckpoint(worktree)
	if err := checkpoint.capture(worktree, "tests passed"); err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	for _, unwanted := range []string{".git/HEAD", "generated.exe"} {
		if _, err := os.Stat(filepath.Join(checkpoint.store, unwanted)); err == nil {
			t.Errorf("%s was copied into the checkpoint", unwanted)
		}
	}
	if _, err := os.Stat(filepath.Join(worktree, ".git/HEAD")); err != nil {
		t.Error("restoring cleared version control out of the worktree")
	}
}

func writeCheckpointFile(t *testing.T, worktree, relative, body string) {
	t.Helper()
	path := filepath.Join(worktree, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("preparing %s failed: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s failed: %v", relative, err)
	}
}
