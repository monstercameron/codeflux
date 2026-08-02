package filetree_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/filetree"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func tokens(t *testing.T) design.Tokens {
	t.Helper()
	value, err := design.TokensFor(design.Options{
		Theme: design.ThemeDark, Density: design.DensityComfortable,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func files() []filetree.File {
	return []filetree.File{
		{
			Path: "internal/inventory/reserve.go", Kind: "go",
			ImportPath:  "example.com/demo/internal/inventory",
			SymbolCount: 5, AtomCount: 2,
		},
		{
			Path: "internal/inventory/reserve_test.go", Kind: "go_test",
			ImportPath: "example.com/demo/internal/inventory", SymbolCount: 3,
		},
		{Path: "go.mod", Kind: "module"},
	}
}

func ready(t *testing.T) filetree.Props {
	t.Helper()
	return filetree.Props{
		Tokens: tokens(t), State: filetree.LoadReady, Files: files(),
		TotalFiles: 3, Revision: "e7699ae0deadbeef",
		OnSelectFile: func(string) {}, OnSelectLine: func(uint32) {},
		OnToggleDir: func(string) {}, OnSearch: func(string) {},
	}
}

// TestTheTreeNestsFilesUnderTheirDirectories keeps the repository's own shape,
// because a person who knows the tree can find things in it without learning a
// second vocabulary first.
func TestTheTreeNestsFilesUnderTheirDirectories(t *testing.T) {
	props := ready(t)
	props.Expanded = map[string]bool{"internal": true, "internal/inventory": true}
	markup := render(t, ui.CreateElement(filetree.Component, props))
	for _, want := range []string{
		`data-component="file-tree-shell"`,
		`data-component="tree-directory"`,
		`data-path="internal"`,
		`data-path="internal/inventory"`,
		`data-path="internal/inventory/reserve.go"`,
		`data-path="go.mod"`,
		"2 atoms",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("the tree is missing %q", want)
		}
	}
}

// TestAClosedDirectoryHidesItsFiles keeps the tree readable in a repository
// with more files than fit on a screen.
func TestAClosedDirectoryHidesItsFiles(t *testing.T) {
	props := ready(t)
	props.Expanded = map[string]bool{}
	markup := render(t, ui.CreateElement(filetree.Component, props))
	if strings.Contains(markup, "internal/inventory/reserve.go") {
		t.Fatal("a file under a closed directory was drawn")
	}
	if !strings.Contains(markup, `data-path="internal"`) {
		t.Fatal("the closed directory itself is missing")
	}
	if !strings.Contains(markup, `data-path="go.mod"`) {
		t.Fatal("a file at the root should not be hidden by a closed directory")
	}
}

// TestAFilteredTreeOpensItselfSoTheMatchesAreVisible: a filter that leaves
// every match behind a closed directory has done nothing a person can see.
func TestAFilteredTreeOpensItselfSoTheMatchesAreVisible(t *testing.T) {
	props := ready(t)
	props.Expanded = map[string]bool{}
	props.Search = "reserve"
	props.Files = []filetree.File{files()[0]}
	markup := render(t, ui.CreateElement(filetree.Component, props))
	if !strings.Contains(markup, `data-path="internal/inventory/reserve.go"`) {
		t.Fatal("a filtered tree did not open itself to its match")
	}
}

// TestOpeningAFileShowsItsSourceWithRealLineNumbers keeps the numbers in the
// viewer the same numbers an editor and a stack trace use.
func TestOpeningAFileShowsItsSourceWithRealLineNumbers(t *testing.T) {
	props := ready(t)
	props.SelectedPath = "internal/inventory/reserve.go"
	props.ContentState = filetree.LoadReady
	props.SelectedLine = 2
	props.Content = &filetree.Content{
		File: files()[0], Lines: 3,
		Text: "package inventory\n\nfunc ReserveStockUntilCheckoutExpires() {}",
		Declarations: []filetree.Declaration{
			{Key: "d1", Name: "ReserveStockUntilCheckoutExpires", Kind: "function", Line: 3, Atom: true},
			{Key: "d2", Name: "ReleaseHold", Kind: "function", Line: 9},
		},
	}
	markup := render(t, ui.CreateElement(filetree.Component, props))
	for _, want := range []string{
		`data-component="file-content"`,
		`data-path="internal/inventory/reserve.go"`,
		"internal/inventory/reserve.go",
		"3 lines",
		`data-atom="true" data-component="file-declaration" data-declaration="ReserveStockUntilCheckoutExpires"`,
		`data-atom="false" data-component="file-declaration" data-declaration="ReleaseHold"`,
		`data-line="1"`,
		`data-line="3"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("the open file is missing %q", want)
		}
	}
}

// TestATruncatedReadSaysSoRatherThanEndingQuietly: a file that stops halfway
// with no explanation reads as a file that is that short.
func TestATruncatedReadSaysSoRatherThanEndingQuietly(t *testing.T) {
	props := ready(t)
	props.SelectedPath = "internal/inventory/reserve.go"
	props.ContentState = filetree.LoadReady
	props.Content = &filetree.Content{File: files()[0], Lines: 4000, Text: "package inventory", Truncated: true}
	markup := render(t, ui.CreateElement(filetree.Component, props))
	if !strings.Contains(markup, "continues past what was read") {
		t.Fatal("a truncated read did not say so")
	}
}

// TestAFileThatCannotBeReadSaysWhy keeps the failure on the file rather than
// blanking the tree that is still perfectly good.
func TestAFileThatCannotBeReadSaysWhy(t *testing.T) {
	props := ready(t)
	props.Expanded = map[string]bool{"internal": true, "internal/inventory": true}
	props.SelectedPath = "internal/inventory/reserve.go"
	props.ContentState = filetree.LoadFailed
	props.ContentError = "the file is not in the repository map at this revision"
	markup := render(t, ui.CreateElement(filetree.Component, props))
	if !strings.Contains(markup, "not in the repository map") {
		t.Fatal("the read failure was not reported")
	}
	if !strings.Contains(markup, `data-component="tree-file"`) {
		t.Fatal("a failed file read emptied the tree beside it")
	}
}

// TestNoRepositorySaysSoInsteadOfDrawingAnEmptyTree.
func TestNoRepositorySaysSoInsteadOfDrawingAnEmptyTree(t *testing.T) {
	props := ready(t)
	props.State = filetree.LoadUnavailable
	props.Files = nil
	markup := render(t, ui.CreateElement(filetree.Component, props))
	if !strings.Contains(markup, "No repository is open") {
		t.Fatal("an unavailable tree did not say why it is empty")
	}
}
