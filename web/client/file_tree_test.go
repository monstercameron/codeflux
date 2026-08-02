package main

import (
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/filetree"
)

func fileViews() []*codefluxv1.CodeFileView {
	return []*codefluxv1.CodeFileView{
		{
			WorkspaceRelativePath: "internal/inventory/reserve.go", Kind: "go",
			ImportPath: "example.com/demo/internal/inventory", SymbolCount: 5, AtomCount: 2,
		},
		{
			WorkspaceRelativePath: "internal/inventory/reserve_test.go",
			Kind:                  "go_test", SymbolCount: 3,
		},
		{WorkspaceRelativePath: "", Kind: "go"},
	}
}

// TestAFileWithNoPathIsNotDrawn: a row with no path cannot be placed in a tree
// or opened, so drawing it would put something on screen that does nothing.
func TestAFileWithNoPathIsNotDrawn(t *testing.T) {
	answer := projectCodeFiles(&codefluxv1.ListCodeFilesResponse{
		Files:      fileViews(),
		Revision:   &codefluxv1.CodeCollectionRevisionView{Revision: "e7699ae", Dirty: true},
		TotalFiles: 3,
	})
	if len(answer.Files) != 2 {
		t.Fatalf("wanted the two placeable files, got %d", len(answer.Files))
	}
	if answer.Files[0].AtomCount != 2 || answer.Files[0].SymbolCount != 5 {
		t.Fatalf("the counts did not survive the projection: %+v", answer.Files[0])
	}
	if answer.Revision != "e7699ae" || !answer.Dirty {
		t.Fatalf("the revision did not survive the projection: %+v", answer)
	}
	if answer.Total != 3 {
		t.Fatalf("wanted the repository's own total, got %d", answer.Total)
	}
}

// TestFilteringMatchesTheWholePathSoADirectoryWorks: somebody looking for the
// inventory package types "inventory", and every file under it should stay.
func TestFilteringMatchesTheWholePathSoADirectoryWorks(t *testing.T) {
	files := []filetree.File{
		{Path: "internal/inventory/reserve.go"},
		{Path: "internal/inventory/reserve_test.go"},
		{Path: "cmd/hello/main.go"},
	}
	if got := len(filterTreeFiles(files, "inventory")); got != 2 {
		t.Fatalf("a directory term kept %d files, wanted 2", got)
	}
	if got := len(filterTreeFiles(files, "MAIN")); got != 1 {
		t.Fatalf("filtering should ignore case, kept %d", got)
	}
	if got := len(filterTreeFiles(files, "   ")); got != 3 {
		t.Fatalf("blank filter kept %d files, wanted all of them", got)
	}
}

// TestAReadCarriesTheDeclarationsTheJumpChipsNeed keeps the line numbers and
// the atom marks, which are what makes a chip worth clicking.
func TestAReadCarriesTheDeclarationsTheJumpChipsNeed(t *testing.T) {
	content := projectCodeFileContent(&codefluxv1.ReadCodeFileResponse{
		File:      fileViews()[0],
		Text:      &codefluxv1.RedactedText{Value: "package inventory\n"},
		LineCount: 113,
		Truncated: true,
		Declarations: []*codefluxv1.CodeSymbolView{
			{Key: "d1", Name: "ReserveStockUntilCheckoutExpires", Kind: "function", Line: 60, Atom: true, Exported: true},
			{Key: "d2", Name: "ReleaseHold", Kind: "function", Line: 100},
		},
	})
	if content.Lines != 113 || !content.Truncated {
		t.Fatalf("the read's shape did not survive: %+v", content)
	}
	if len(content.Declarations) != 2 {
		t.Fatalf("wanted both declarations, got %d", len(content.Declarations))
	}
	first := content.Declarations[0]
	if first.Line != 60 || !first.Atom || first.Name != "ReserveStockUntilCheckoutExpires" {
		t.Fatalf("the declaration lost what a jump chip needs: %+v", first)
	}
	if content.File.Path != "internal/inventory/reserve.go" {
		t.Fatalf("the read lost its own file: %+v", content.File)
	}
}
