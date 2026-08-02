package main

import (
	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/codecollection"
)

// codeCollectionAnswer is one coordinator answer about a repository's code.
type codeCollectionAnswer struct {
	Revision      string
	Dirty         bool
	Warnings      []string
	Packages      []codecollection.PackageRow
	TotalPackages uint32
	TotalSymbols  uint32
	TotalAtoms    uint32
	Truncated     bool
}

// projectCodePackages turns the coordinator's package listing into rows.
func projectCodePackages(
	response *codefluxv1.ListCodePackagesResponse,
) codeCollectionAnswer {
	answer := codeCollectionAnswer{
		Revision:      response.GetRevision().GetRevision(),
		Dirty:         response.GetRevision().GetDirty(),
		Warnings:      collectionTexts(response.GetRevision().GetWarnings()),
		TotalPackages: response.GetTotalPackages(),
		TotalSymbols:  response.GetTotalSymbols(),
		TotalAtoms:    response.GetTotalAtoms(),
		Truncated:     response.GetPage().GetHasMore(),
	}
	for _, view := range response.GetPackages() {
		if view.GetImportPath() == "" {
			// A package with no import path cannot be opened, and drawing it
			// would offer a control that does nothing.
			continue
		}
		answer.Packages = append(answer.Packages, codecollection.PackageRow{
			ImportPath:  view.GetImportPath(),
			Name:        view.GetName(),
			FileCount:   view.GetFileCount(),
			TestCount:   view.GetTestFileCount(),
			SymbolCount: view.GetSymbolCount(),
			AtomCount:   view.GetAtomCount(),
		})
	}
	return answer
}

// codeSymbolAnswer is one coordinator answer about declarations.
type codeSymbolAnswer struct {
	Symbols   []codecollection.SymbolRow
	Matched   uint32
	Truncated bool
}

// projectCodeSymbols turns the coordinator's declaration listing into rows.
func projectCodeSymbols(
	response *codefluxv1.ListCodeSymbolsResponse,
) codeSymbolAnswer {
	answer := codeSymbolAnswer{
		Matched:   response.GetTotalMatched(),
		Truncated: response.GetPage().GetHasMore(),
	}
	for _, view := range response.GetSymbols() {
		if row, ok := projectCodeSymbol(view); ok {
			answer.Symbols = append(answer.Symbols, row)
		}
	}
	return answer
}

// projectCodeSymbol turns one declaration view into a row.
//
// A declaration the coordinator did not give a key cannot be inspected, so it
// is dropped rather than drawn as a control that leads nowhere.
func projectCodeSymbol(view *codefluxv1.CodeSymbolView) (codecollection.SymbolRow, bool) {
	if view.GetKey() == "" {
		return codecollection.SymbolRow{}, false
	}
	return codecollection.SymbolRow{
		Key:         view.GetKey(),
		Name:        view.GetName(),
		Kind:        view.GetKind(),
		Receiver:    view.GetReceiver(),
		ImportPath:  view.GetImportPath(),
		Path:        view.GetWorkspaceRelativePath(),
		Line:        view.GetLine(),
		Exported:    view.GetExported(),
		Atom:        view.GetAtom(),
		AtomProblem: view.GetAtomProblem(),
	}, true
}

// projectCodeSymbolDetail turns one inspected declaration into its detail.
func projectCodeSymbolDetail(
	response *codefluxv1.InspectCodeSymbolResponse,
) codecollection.Detail {
	detail := codecollection.Detail{
		Signature:           response.GetSignature().GetValue(),
		Documentation:       collectionTexts(response.GetDocumentation()),
		AtomOpeningSentence: response.GetAtomOpeningSentence(),
		AtomSchemaVersion:   response.GetAtomSchemaVersion(),
		Implements:          response.GetImplements(),
		ImplementedBy:       response.GetImplementedBy(),
	}
	if symbol, ok := projectCodeSymbol(response.GetSymbol()); ok {
		detail.Symbol = symbol
	}
	for _, field := range response.GetAtomFields() {
		detail.AtomFields = append(detail.AtomFields, codecollection.AtomField{
			Label: field.GetLabel(),
			Text:  field.GetText().GetValue(),
			Items: collectionTexts(field.GetItems()),
		})
	}
	detail.Callers = projectCodeReferences(response.GetCallers())
	detail.Callees = projectCodeReferences(response.GetCallees())
	return detail
}

// projectCodeReferences turns a bounded call list into rows.
func projectCodeReferences(
	views []*codefluxv1.CodeSymbolReferenceView,
) []codecollection.Reference {
	references := make([]codecollection.Reference, 0, len(views))
	for _, view := range views {
		references = append(references, codecollection.Reference{
			Name: view.GetName(),
			Path: view.GetWorkspaceRelativePath(),
			Line: view.GetLine(),
		})
	}
	return references
}

// collectionTexts reads the display text out of a redacted list.
//
// Empty lines are preserved: a documentation comment's blank line is part of
// what the author wrote, and dropping it would rewrite the comment.
func collectionTexts(values []*codefluxv1.RedactedText) []string {
	texts := make([]string, 0, len(values))
	for _, value := range values {
		texts = append(texts, value.GetValue())
	}
	return texts
}
