package main

import (
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
)

func atomSymbol(name, importPath string, admitted bool, problem string) *codefluxv1.CodeSymbolView {
	return &codefluxv1.CodeSymbolView{
		Key: "key-" + name, Name: name, Kind: "function",
		ImportPath: importPath, WorkspaceRelativePath: "internal/inventory/reserve.go",
		Line: 12, Atom: admitted, AtomProblem: problem,
	}
}

// TestAtomProjectionKeepsOnlyDeclarationsThatAskedToBeAtoms holds the surface
// to its subject: an ordinary declaration belongs to the code collection, and
// a directive that failed to admit belongs here because that refusal is what
// an author needs to see.
func TestAtomProjectionKeepsOnlyDeclarationsThatAskedToBeAtoms(t *testing.T) {
	answer := projectAtomSymbols(&codefluxv1.ListCodeSymbolsResponse{
		Revision: &codefluxv1.CodeCollectionRevisionView{Revision: "31c62bb", Dirty: true},
		Symbols: []*codefluxv1.CodeSymbolView{
			atomSymbol("ReleaseHold", "example.com/demo/internal/inventory", false, "comment does not parse"),
			atomSymbol("Helper", "example.com/demo/internal/inventory", false, ""),
			atomSymbol("ReserveStock", "example.com/demo/internal/inventory", true, ""),
		},
	})
	if len(answer.Rows) != 2 {
		t.Fatalf("rows = %#v, want the atom and the refused directive only", answer.Rows)
	}
	if answer.Admitted != 1 || answer.Refused != 1 {
		t.Fatalf("admitted = %d refused = %d", answer.Admitted, answer.Refused)
	}
	// Admitted atoms sort first so the refusals gather where they can be
	// worked through.
	if !answer.Rows[0].Admitted || answer.Rows[1].Admitted {
		t.Fatalf("order = %#v", answer.Rows)
	}
	if answer.Revision != "31c62bb" || !answer.Dirty {
		t.Fatalf("revision = %q dirty = %v", answer.Revision, answer.Dirty)
	}
	if len(answer.Packages) != 1 {
		t.Fatalf("packages = %#v", answer.Packages)
	}
}

// TestAtomFilterMatchesTheQuestionsPeopleArriveWith checks the search reaches
// the receiver, the package, and the file, because "every atom on this type"
// and "every atom in this file" are what people actually look for.
func TestAtomFilterMatchesTheQuestionsPeopleArriveWith(t *testing.T) {
	answer := projectAtomSymbols(&codefluxv1.ListCodeSymbolsResponse{
		Symbols: []*codefluxv1.CodeSymbolView{
			atomSymbol("ReserveStock", "example.com/demo/internal/inventory", true, ""),
			atomSymbol("SendReceipt", "example.com/demo/internal/billing", true, ""),
			atomSymbol("ReleaseHold", "example.com/demo/internal/inventory", false, "does not parse"),
		},
	})
	if hidden := filterAtomRows(answer.Rows, "", "", false); len(hidden) != 2 {
		t.Fatalf("refused directive shown while hidden: %#v", hidden)
	}
	if shown := filterAtomRows(answer.Rows, "", "", true); len(shown) != 3 {
		t.Fatalf("refused directive missing while shown: %#v", shown)
	}
	byPackage := filterAtomRows(answer.Rows, "", "example.com/demo/internal/billing", true)
	if len(byPackage) != 1 || byPackage[0].Name != "SendReceipt" {
		t.Fatalf("package filter = %#v", byPackage)
	}
	bySearch := filterAtomRows(answer.Rows, "billing", "", true)
	if len(bySearch) != 1 || bySearch[0].Name != "SendReceipt" {
		t.Fatalf("search = %#v", bySearch)
	}
}

// TestAtomProseIsRejoinedAtTheAuthorsParagraphs keeps a comment wrapped at
// column 72 from rendering as a ragged column: a blank line is a paragraph the
// author meant, and a single line break is not.
func TestAtomProseIsRejoinedAtTheAuthorsParagraphs(t *testing.T) {
	detail := projectAtomDetail(&codefluxv1.InspectCodeSymbolResponse{
		Symbol: atomSymbol("ReserveStock", "example.com/demo/internal/inventory", true, ""),
		AtomFields: []*codefluxv1.CodeAtomFieldView{
			{
				Label: "Purpose",
				Text: &codefluxv1.RedactedText{
					Value: "Hold scarce stock for one checkout session so two\nshoppers cannot both complete a sale.\n\nA second paragraph.",
				},
			},
		},
	})
	if len(detail.Fields) != 1 {
		t.Fatalf("fields = %#v", detail.Fields)
	}
	text := detail.Fields[0].Text
	if strings.Contains(text, "two\nshoppers") {
		t.Fatalf("wrapped line was not rejoined: %q", text)
	}
	if !strings.Contains(text, "\n\nA second paragraph.") {
		t.Fatalf("paragraph break was lost: %q", text)
	}
}
