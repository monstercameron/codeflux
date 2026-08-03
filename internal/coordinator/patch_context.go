package coordinator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
)

// patchContextItems put the files a run is about to change in front of it, in
// full, with the digest a patch is written against.
//
// Context selection reads the repository at its base revision, which on a
// generated project is empty: the files the run is refining are ones it wrote
// itself, and they were in no revision when the selection ran. So a refinement
// round was asking a model to change a file it had last seen several attempts
// ago, from a summary of a summary, and the model did the only thing available
// — rewrote the whole file from memory, which is how a working program loses a
// function nobody meant to touch.
//
// A patch cannot be written from memory at all. Its hunks are matched against
// the file's exact text, indentation included, so a run that cannot see the
// file cannot produce one. Offering the patch tool without the file was
// offering a tool that could not be used.
//
// Only the files a step is actually going to modify. Everything else is the
// selection's business, and adding files nobody asked about spends the context
// the instruction needs.
func patchContextItems(
	worktree string, steps []agentloop.PlanStep,
) []agentloop.RepositoryContextItem {
	wanted := map[string]bool{}
	for _, step := range steps {
		if step.Kind != agentloop.StepKindPatch {
			continue
		}
		for _, file := range step.ExpectedFiles {
			wanted[file] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	paths := make([]string, 0, len(wanted))
	for path := range wanted {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var items []agentloop.RepositoryContextItem
	for _, path := range paths {
		content, err := os.ReadFile(
			filepath.Join(worktree, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		// The digest travels with the text so a patch can be written against a
		// named revision, and so a stale one can be told from a wrong one: a
		// hunk that does not match because the file moved is a different
		// problem from a hunk that never matched, and they need different
		// answers.
		items = append(items, agentContextItem(path, fmt.Sprintf(
			"%s\n\nThis is %s exactly as it stands now, revision %s. Patch "+
				"hunks are matched against this text character for character, "+
				"so copy the lines you are changing from here rather than "+
				"retyping them.",
			string(content), path, executor.HashOfContent(string(content)))))
	}
	return items
}

// producedTestFilesFor names the tests that exercise a file being changed.
//
// A patch to a function whose tests the model cannot see is a patch written
// without knowing what must keep passing, and the first thing it breaks is a
// test it never read. Included whole, because a test is short and the part that
// matters is whichever assertion the change is about to falsify.
func producedTestFilesFor(
	worktree string, steps []agentloop.PlanStep,
) []agentloop.RepositoryContextItem {
	changing := map[string]bool{}
	for _, step := range steps {
		if step.Kind != agentloop.StepKindPatch {
			continue
		}
		for _, file := range step.ExpectedFiles {
			changing[file] = true
		}
	}
	if len(changing) == 0 {
		return nil
	}
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil
	}
	var items []agentloop.RepositoryContextItem
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") || changing[file] {
			continue
		}
		// Only the tests beside a file being changed. A test in another
		// directory is testing something else.
		if !changing[strings.TrimSuffix(file, "_test.go")+".go"] &&
			!sameDirectoryAsAnyOf(file, changing) {
			continue
		}
		content, readErr := os.ReadFile(
			filepath.Join(worktree, filepath.FromSlash(file)))
		if readErr != nil {
			continue
		}
		items = append(items, agentContextItem(file, string(content)+
			"\n\nThese tests must still pass after your change."))
	}
	return items
}

// sameDirectoryAsAnyOf reports whether a file sits beside one being changed.
func sameDirectoryAsAnyOf(file string, changing map[string]bool) bool {
	directory := filepath.ToSlash(filepath.Dir(file))
	for candidate := range changing {
		if filepath.ToSlash(filepath.Dir(candidate)) == directory {
			return true
		}
	}
	return false
}
