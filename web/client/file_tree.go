package main

import (
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/filetree"
)

// fileTreeAnswer is one coordinator answer about a repository's files.
type fileTreeAnswer struct {
	Files     []filetree.File
	Revision  string
	Dirty     bool
	Total     uint32
	Truncated bool
}

// projectCodeFiles reads a file listing into the tree's shape.
func projectCodeFiles(response *codefluxv1.ListCodeFilesResponse) fileTreeAnswer {
	answer := fileTreeAnswer{}
	revision := response.GetRevision()
	answer.Revision = revision.GetRevision()
	answer.Dirty = revision.GetDirty()
	answer.Total = response.GetTotalFiles()
	answer.Truncated = response.GetPage().GetHasMore()
	for _, file := range response.GetFiles() {
		if file.GetWorkspaceRelativePath() == "" {
			// A file with no path cannot be placed in a tree or opened, and
			// drawing it would put a row on screen that does nothing.
			continue
		}
		answer.Files = append(answer.Files, projectCodeFile(file))
	}
	return answer
}

func projectCodeFile(file *codefluxv1.CodeFileView) filetree.File {
	return filetree.File{
		Path:        file.GetWorkspaceRelativePath(),
		Kind:        file.GetKind(),
		Generated:   file.GetGenerated(),
		ImportPath:  file.GetImportPath(),
		SymbolCount: file.GetSymbolCount(),
		AtomCount:   file.GetAtomCount(),
	}
}

// projectCodeFileContent reads one file into what the viewer draws.
func projectCodeFileContent(response *codefluxv1.ReadCodeFileResponse) filetree.Content {
	content := filetree.Content{
		File:      projectCodeFile(response.GetFile()),
		Text:      response.GetText().GetValue(),
		Lines:     response.GetLineCount(),
		Truncated: response.GetTruncated(),
	}
	for _, declaration := range response.GetDeclarations() {
		content.Declarations = append(content.Declarations, filetree.Declaration{
			Key:      declaration.GetKey(),
			Name:     declaration.GetName(),
			Kind:     declaration.GetKind(),
			Receiver: declaration.GetReceiver(),
			Line:     declaration.GetLine(),
			Atom:     declaration.GetAtom(),
			Exported: declaration.GetExported(),
		})
	}
	return content
}

// narrowedTreeFiles applies a term locally without ever emptying a tree the
// coordinator is still narrowing.
//
// The local pass only knows the files it already has. When it can answer, its
// answer is the better one because it is instant; when it cannot, the rows on
// screen stay until the coordinator's answer replaces them, because a tree
// that blanks mid-keystroke reads as a repository with nothing in it.
func narrowedTreeFiles(files []filetree.File, search string) []filetree.File {
	narrowed := filterTreeFiles(files, search)
	if len(narrowed) == 0 && strings.TrimSpace(search) != "" {
		return files
	}
	return narrowed
}

// filterTreeFiles narrows the tree to paths containing a term.
//
// Matching the whole path rather than the file name is what makes typing a
// directory work: somebody looking for the inventory package types
// "inventory", and every file under it should stay.
func filterTreeFiles(files []filetree.File, search string) []filetree.File {
	needle := strings.ToLower(strings.TrimSpace(search))
	if needle == "" {
		return files
	}
	result := make([]filetree.File, 0, len(files))
	for _, file := range files {
		if strings.Contains(strings.ToLower(file.Path), needle) {
			result = append(result, file)
		}
	}
	return result
}
