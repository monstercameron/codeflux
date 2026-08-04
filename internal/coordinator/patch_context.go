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
	var revisions []string
	for _, path := range paths {
		content, err := os.ReadFile(
			filepath.Join(worktree, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		// The content field holds the file and nothing else.
		//
		// It used to carry the guidance below appended to the text, which was
		// read exactly as written: a field called content, describing a file,
		// containing the sentence, means the sentence is in the file. Rung 1
		// on 2026-08-03 spent most of two runs' patch rounds writing hunks
		// that deleted "This is cmd/generated/main.go exactly as it stands
		// now" from a file that never contained it, and every one of them
		// failed to match, because the sentence really was not there.
		//
		// The instruction to match character for character is what makes the
		// appended prose so expensive rather than merely untidy: a run that
		// follows it is being told the sentence is part of what it must match.
		items = append(items, agentContextItem(path, string(content)))
		revisions = append(revisions, fmt.Sprintf("%s at revision %s",
			path, executor.HashOfContent(string(content))))
	}
	if len(items) == 0 {
		return nil
	}
	// The guidance and the digests, in an item of their own.
	//
	// A pseudo-path rather than a file path, the way planning-request already
	// is, so there is no file for it to be mistaken for the contents of. The
	// digests still travel with the text, which is what lets a stale hunk be
	// told from a wrong one: a hunk that does not match because the file moved
	// is a different problem from one that never matched.
	items = append(items, agentContextItem("how-to-patch", fmt.Sprintf(
		"The files above are shown exactly as they stand now: %s. Patch hunks "+
			"are matched against that text character for character, so copy "+
			"the lines you are changing from it rather than retyping them. "+
			"This note is not part of any file.",
		strings.Join(revisions, ", "))))
	return items
}

// withoutSlicesOfPatchedFiles removes context-selection excerpts of files that
// are being shown in full.
//
// Selection offers a file as a numbered slice — "cmd/generated/main.go:1-4"
// with four lines in it — which is right for reading and wrong for patching: a
// hunk anchored on a four-line excerpt of a sixty-line file matches nothing,
// and rung 8 failed twenty-odd patches that way while the whole file sat
// further down the same prompt.
//
// Two copies of one file, one of them a fragment, is worse than either alone.
// The model has no way to know which is authoritative and every reason to
// anchor on the first thing it reads.
func withoutSlicesOfPatchedFiles(
	items []agentloop.RepositoryContextItem, steps []agentloop.PlanStep,
) []agentloop.RepositoryContextItem {
	patched := map[string]bool{}
	for _, step := range steps {
		if step.Kind != agentloop.StepKindPatch {
			continue
		}
		for _, file := range step.ExpectedFiles {
			patched[file] = true
		}
	}
	if len(patched) == 0 {
		return items
	}
	kept := items[:0:0]
	for _, item := range items {
		// A slice is the path with a line range appended.
		path := item.Path
		if colon := strings.LastIndex(path, ":"); colon > 0 {
			path = path[:colon]
		}
		if patched[path] {
			continue
		}
		kept = append(kept, item)
	}
	return kept
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
	var shown []string
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
		// The file and nothing else, for the same reason patchContextItems
		// shows one that way: a sentence appended to a field called content,
		// describing a named file, is read as part of that file. These items
		// are shown in the same prompt as files being patched, so a run
		// deciding to patch a test would anchor a hunk on the sentence.
		items = append(items, agentContextItem(file, string(content)))
		shown = append(shown, file)
	}
	if len(items) == 0 {
		return nil
	}
	items = append(items, agentContextItem("tests-that-must-keep-passing",
		strings.Join(shown, ", ")+" are shown above as they stand now, and "+
			"must still pass after your change. This note is not part of any "+
			"file."))
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

// anyStepWritesAFile reports whether this attempt's plan contains a step that
// changes a file, and so whether the suite could tell the run something it
// does not already know by the time the attempt ends.
//
// Every refinement attempt has one, which is the point: an attempt that was
// sent back to fix something is an attempt that is about to change the tree,
// however unchanged the tree looks at the moment the tools are chosen.
func anyStepWritesAFile(steps []agentloop.PlanStep) bool {
	for _, step := range steps {
		if step.Kind == agentloop.StepKindEdit ||
			step.Kind == agentloop.StepKindPatch {
			return true
		}
	}
	return false
}
