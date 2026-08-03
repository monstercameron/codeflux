package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
)

// TestAFileBeingPatchedIsShownInFull is what makes the patch tool usable.
//
// A patch cannot be written from memory: its hunks are matched against the
// file's exact text, indentation included. Offering the tool without the file
// was offering a tool that could not be used, and the model did the only thing
// available — rewrote the file from what it could remember.
func TestAFileBeingPatchedIsShownInFull(t *testing.T) {
	worktree := t.TempDir()
	initializeCoordinatorGitRepository(t, worktree)
	source := "package main\n\nfunc run() error {\n\treturn nil\n}\n"
	writeContextFile(t, worktree, "cmd/generated/main.go", source)
	writeContextFile(t, worktree, "cmd/generated/main_test.go",
		"package main\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {}\n")

	steps := []agentloop.PlanStep{{
		ID: "edit-1", Kind: agentloop.StepKindPatch,
		ExpectedFiles: []string{"cmd/generated/main.go"},
	}}
	items := patchContextItems(worktree, steps)
	if len(items) != 1 {
		t.Fatalf("%d file(s) offered for a one-file patch step", len(items))
	}
	if !strings.Contains(items[0].ContentRedacted, "func run() error {") {
		t.Error("the file's actual text was not included, so no hunk can be " +
			"matched against it")
	}
	// The revision travels with it: a hunk that fails because the file moved is
	// a different problem from one that never matched.
	if !strings.Contains(items[0].ContentRedacted, "revision ") {
		t.Errorf("no revision was named:\n%s", items[0].ContentRedacted)
	}

	tests := producedTestFilesFor(worktree, steps)
	if len(tests) != 1 {
		t.Fatalf("%d test file(s) offered beside the file being changed",
			len(tests))
	}
	if !strings.Contains(tests[0].ContentRedacted, "func TestRun") {
		t.Error("the tests that must keep passing were not shown")
	}
}

// TestNothingIsAddedForACreateStep keeps the context to what is needed. A file
// that does not exist has nothing to show, and a create step is not patching.
func TestNothingIsAddedForACreateStep(t *testing.T) {
	worktree := t.TempDir()
	writeContextFile(t, worktree, "cmd/generated/main.go", "package main\n")
	steps := []agentloop.PlanStep{{
		ID: "edit-1", Kind: agentloop.StepKindEdit,
		ExpectedFiles: []string{"cmd/generated/main.go"},
	}}
	if items := patchContextItems(worktree, steps); len(items) != 0 {
		t.Errorf("%d file(s) offered for a create step", len(items))
	}
	if tests := producedTestFilesFor(worktree, steps); len(tests) != 0 {
		t.Errorf("%d test file(s) offered for a create step", len(tests))
	}
}

// TestTestsFromElsewhereAreNotOffered: a test in another directory is testing
// something else, and context spent on it is context the instruction loses.
func TestTestsFromElsewhereAreNotOffered(t *testing.T) {
	worktree := t.TempDir()
	initializeCoordinatorGitRepository(t, worktree)
	writeContextFile(t, worktree, "cmd/generated/main.go", "package main\n")
	writeContextFile(t, worktree, "internal/other/other_test.go",
		"package other\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n")
	steps := []agentloop.PlanStep{{
		ID: "edit-1", Kind: agentloop.StepKindPatch,
		ExpectedFiles: []string{"cmd/generated/main.go"},
	}}
	for _, item := range producedTestFilesFor(worktree, steps) {
		if strings.Contains(item.Path, "internal/other") {
			t.Errorf("a test from an unrelated package was offered: %s",
				item.Path)
		}
	}
}

func writeContextFile(t *testing.T, worktree, relative, body string) {
	t.Helper()
	path := filepath.Join(worktree, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
