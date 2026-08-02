package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/transport"
)

// TestTheFileListingIsTheRepositoryRatherThanItsPackages: a listing built from
// packages drops everything the Go loader ignores — the module file, generated
// output, testdata — and a tree missing those is not the repository a person
// is looking at in their editor.
func TestTheFileListingIsTheRepositoryRatherThanItsPackages(t *testing.T) {
	application, repositoryID := newCollectedFixture(t)

	page, err := application.ListCodeFiles(t.Context(), transport.CodeCollectionQuery{
		RepositoryID: repositoryID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Revision.Revision == "" {
		t.Fatal("a listing that does not name its revision describes nothing in particular")
	}
	byPath := map[string]transport.CodeFileRecord{}
	for _, file := range page.Files {
		byPath[file.Path] = file
	}
	for _, want := range []string{"go.mod", "main.go", "reserve/funds.go"} {
		if _, present := byPath[want]; !present {
			t.Fatalf("%s is missing from the listing: %+v", want, page.Files)
		}
	}
	source := byPath["reserve/funds.go"]
	if source.AtomCount != 1 {
		t.Fatalf("the file's atom count = %d, want 1", source.AtomCount)
	}
	if source.SymbolCount < 3 {
		t.Fatalf("the file's declaration count = %d, want its three", source.SymbolCount)
	}
	if !strings.HasSuffix(source.ImportPath, "/reserve") {
		t.Fatalf("the file lost the package it belongs to: %+v", source)
	}
	if page.TotalFiles < uint32(len(page.Files)) {
		t.Fatalf("total %d is under the %d listed", page.TotalFiles, len(page.Files))
	}
}

// TestReadingAFileReturnsTheSourceAndItsDeclarationsInOrder: the declarations
// are what the viewer turns into jump targets, and a target at the wrong line
// sends a reader somewhere else in the file.
func TestReadingAFileReturnsTheSourceAndItsDeclarationsInOrder(t *testing.T) {
	application, repositoryID := newCollectedFixture(t)

	read, err := application.ReadCodeFile(t.Context(), transport.CodeFileRead{
		RepositoryID: repositoryID, Path: "reserve/funds.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read.Text, "package reserve") {
		t.Fatalf("the read did not return the file: %q", firstLine(read.Text))
	}
	if read.Lines == 0 || read.Truncated {
		t.Fatalf("the read lost its own shape: lines=%d truncated=%v",
			read.Lines, read.Truncated)
	}
	if len(read.Declarations) < 3 {
		t.Fatalf("want the file's three declarations, got %+v", read.Declarations)
	}
	previous := uint32(0)
	for _, declaration := range read.Declarations {
		if declaration.Line <= previous {
			t.Fatalf("declarations are not in source order: %+v", read.Declarations)
		}
		previous = declaration.Line
	}
	lines := strings.Split(read.Text, "\n")
	first := read.Declarations[0]
	if int(first.Line) > len(lines) {
		t.Fatalf("declaration line %d is past the file's %d lines", first.Line, len(lines))
	}
	if !strings.Contains(lines[first.Line-1], first.Name) {
		t.Fatalf("line %d does not declare %s: %q", first.Line, first.Name, lines[first.Line-1])
	}
	atoms := 0
	for _, declaration := range read.Declarations {
		if declaration.Atom {
			atoms++
		}
	}
	if atoms != 1 {
		t.Fatalf("the read marked %d atoms, want the file's one", atoms)
	}
}

// TestAPathTheMapNeverRecordedIsRefused keeps the reader inside the repository
// it named. The listing is the whole set of readable paths, so a request for
// anything else is not a file this surface can be asked for.
func TestAPathTheMapNeverRecordedIsRefused(t *testing.T) {
	application, repositoryID := newCollectedFixture(t)

	for _, path := range []string{
		"../../../etc/passwd",
		"reserve/../../secret.go",
		"does/not/exist.go",
		"",
	} {
		if _, err := application.ReadCodeFile(t.Context(), transport.CodeFileRead{
			RepositoryID: repositoryID, Path: path,
		}); err == nil {
			t.Fatalf("reading %q was allowed", path)
		}
	}
}

// TestAFileListingIsNotBoundedLikeAPackageListing: a repository with a
// thousand files is ordinary, and cutting its tree at a package-sized page
// would show an arbitrary slice of the alphabet and call it the directory
// structure.
func TestAFileListingIsNotBoundedLikeAPackageListing(t *testing.T) {
	if boundedCodeFileLimit(0) != transport.MaximumCodeFilePage {
		t.Fatalf("an unbounded request bounds to %d", boundedCodeFileLimit(0))
	}
	if transport.MaximumCodeFilePage <= transport.MaximumCodePage {
		t.Fatalf("the file page (%d) is no larger than the package page (%d)",
			transport.MaximumCodeFilePage, transport.MaximumCodePage)
	}
	if boundedCodeFileLimit(25) != 25 {
		t.Fatal("a caller's own smaller limit was ignored")
	}
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}
