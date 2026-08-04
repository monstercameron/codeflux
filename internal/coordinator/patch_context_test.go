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
	// One item for the file, one note carrying the guidance and the revisions.
	if len(items) != 2 {
		t.Fatalf("%d item(s) offered for a one-file patch step, want the file "+
			"and the note", len(items))
	}
	if items[0].Path != "cmd/generated/main.go" {
		t.Fatalf("the first item is %q, not the file being patched",
			items[0].Path)
	}
	if !strings.Contains(items[0].ContentRedacted, "func run() error {") {
		t.Error("the file's actual text was not included, so no hunk can be " +
			"matched against it")
	}
	// The content field holds the file and nothing else. Anything appended to
	// it is read as part of the file, because that is what the field means:
	// rung 1 on 2026-08-03 spent most of two runs' patch rounds writing hunks
	// that deleted the explanatory sentence from a file that never contained
	// it, and every one failed to match.
	if items[0].ContentRedacted != source {
		t.Errorf("the content offered for a file is not exactly that file, so "+
			"a hunk copied from it cannot match what is on disk:\n%q",
			items[0].ContentRedacted)
	}
	// The revision still travels with it, in the note: a hunk that fails
	// because the file moved is a different problem from one that never
	// matched.
	if items[1].Path != "how-to-patch" {
		t.Fatalf("the note is at %q, which could be mistaken for a file",
			items[1].Path)
	}
	if !strings.Contains(items[1].ContentRedacted, "revision ") {
		t.Errorf("no revision was named:\n%s", items[1].ContentRedacted)
	}
	if !strings.Contains(items[1].ContentRedacted, "cmd/generated/main.go") {
		t.Errorf("the note does not say which file it is about:\n%s",
			items[1].ContentRedacted)
	}

	// One test file, one note. The note is separate for the same reason the
	// patch guidance is: these items sit in the same prompt as files being
	// patched, and a sentence inside a field called content, on an item named
	// after a file, is read as part of that file.
	tests := producedTestFilesFor(worktree, steps)
	if len(tests) != 2 {
		t.Fatalf("%d item(s) offered beside the file being changed, want the "+
			"test file and the note", len(tests))
	}
	if tests[0].Path != "cmd/generated/main_test.go" {
		t.Fatalf("the first item is %q, not the test file", tests[0].Path)
	}
	if !strings.Contains(tests[0].ContentRedacted, "func TestRun") {
		t.Error("the tests that must keep passing were not shown")
	}
	if strings.Contains(tests[0].ContentRedacted, "must still pass") {
		t.Errorf("prose was appended to a test file's content, so a hunk "+
			"anchored on it can never match:\n%q", tests[0].ContentRedacted)
	}
	if tests[1].Path != "tests-that-must-keep-passing" {
		t.Fatalf("the note is at %q, which could be mistaken for a file",
			tests[1].Path)
	}
	if !strings.Contains(tests[1].ContentRedacted, "must still pass") {
		t.Errorf("the note does not say the tests must keep passing:\n%s",
			tests[1].ContentRedacted)
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
