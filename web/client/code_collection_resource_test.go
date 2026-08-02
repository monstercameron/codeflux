package main

import (
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
)

func TestThePackageProjectionCarriesTheRevisionAndDropsWhatCannotBeOpened(t *testing.T) {
	answer := projectCodePackages(&codefluxv1.ListCodePackagesResponse{
		Revision: &codefluxv1.CodeCollectionRevisionView{
			Revision: "2994deb", Dirty: true,
			Warnings: []*codefluxv1.RedactedText{{Value: "go-list: one package would not load"}},
		},
		Packages: []*codefluxv1.CodePackageView{
			{ImportPath: "codeflux.dev/codeflux/internal/policy", Name: "policy", SymbolCount: 44, AtomCount: 2},
			// A package with no import path cannot be opened, so it must not
			// become a row offering a control that does nothing.
			{Name: "nameless"},
		},
		Page:          &codefluxv1.PageInfo{HasMore: true},
		TotalPackages: 94, TotalSymbols: 20261, TotalAtoms: 2,
	})
	if answer.Revision != "2994deb" || !answer.Dirty || len(answer.Warnings) != 1 {
		t.Fatalf("revision lost a field: %+v", answer)
	}
	if len(answer.Packages) != 1 || answer.Packages[0].SymbolCount != 44 {
		t.Fatalf("packages lost a field: %+v", answer.Packages)
	}
	if !answer.Truncated || answer.TotalPackages != 94 || answer.TotalAtoms != 2 {
		t.Fatalf("totals lost a field: %+v", answer)
	}
}

func TestTheDeclarationProjectionDropsRowsThatCannotBeInspected(t *testing.T) {
	answer := projectCodeSymbols(&codefluxv1.ListCodeSymbolsResponse{
		Symbols: []*codefluxv1.CodeSymbolView{
			{Key: "a", Name: "Select", Kind: "function", Exported: true, Atom: true, Line: 151},
			// A declaration with no key cannot be inspected.
			{Name: "keyless"},
		},
		TotalMatched: 2,
		Page:         &codefluxv1.PageInfo{HasMore: true},
	})
	if len(answer.Symbols) != 1 || answer.Symbols[0].Key != "a" ||
		!answer.Symbols[0].Atom || answer.Symbols[0].Line != 151 {
		t.Fatalf("declarations lost a field: %+v", answer.Symbols)
	}
	if answer.Matched != 2 || !answer.Truncated {
		t.Fatalf("declaration totals lost a field: %+v", answer)
	}
}

func TestTheDetailProjectionKeepsDocumentationLineForLine(t *testing.T) {
	detail := projectCodeSymbolDetail(&codefluxv1.InspectCodeSymbolResponse{
		Symbol:    &codefluxv1.CodeSymbolView{Key: "a", Name: "Select"},
		Signature: &codefluxv1.RedactedText{Value: "func Select() error"},
		Documentation: []*codefluxv1.RedactedText{
			{Value: "Select returns the frozen baseline."},
			{Value: ""},
			{Value: "It performs no I/O."},
		},
		AtomSchemaVersion: 1,
		AtomFields: []*codefluxv1.CodeAtomFieldView{{
			Label: "Inputs",
			Items: []*codefluxv1.RedactedText{{Value: "input: the selection inputs"}},
		}},
		Callers: []*codefluxv1.CodeSymbolReferenceView{
			{Name: "TestSelect", WorkspaceRelativePath: "internal/policy/fixed_test.go", Line: 15},
		},
		Implements: []string{"policy.Selector"},
	})
	if detail.Symbol.Key != "a" || detail.Signature != "func Select() error" {
		t.Fatalf("detail lost a field: %+v", detail)
	}
	// A blank line in a comment is part of what the author wrote.
	if len(detail.Documentation) != 3 || detail.Documentation[1] != "" {
		t.Fatalf("documentation was rewritten: %+v", detail.Documentation)
	}
	if len(detail.AtomFields) != 1 || len(detail.AtomFields[0].Items) != 1 {
		t.Fatalf("atom fields lost a field: %+v", detail.AtomFields)
	}
	if len(detail.Callers) != 1 || detail.Callers[0].Line != 15 {
		t.Fatalf("callers lost a field: %+v", detail.Callers)
	}
	if len(detail.Implements) != 1 {
		t.Fatalf("implements = %+v", detail.Implements)
	}
}
