package main

import (
	"sort"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/atomcollection"
)

// atomCollectionAnswer is one coordinator answer about a repository's atoms.
type atomCollectionAnswer struct {
	Rows     []atomcollection.AtomRow
	Packages []string
	Revision string
	Dirty    bool
	Warnings []string
	Admitted uint32
	Refused  uint32
	// Collection is how many atoms the repository holds, whatever this answer
	// matched, so a searched answer can still say what it is a part of.
	Collection uint32
	// SearchQuery is the SQL this answer's search performed.
	SearchQuery string
	Truncated   bool
}

// projectAtomSymbols turns a symbol listing into atom rows.
//
// The listing is asked for with atoms_only, which returns both admitted atoms
// and declarations that carry the directive without admitting. Both are kept:
// a refused directive is the most useful thing this surface can show an
// author, and dropping it would leave them wondering why their atom never
// appeared.
func projectAtomSymbols(response *codefluxv1.ListCodeSymbolsResponse) atomCollectionAnswer {
	answer := atomCollectionAnswer{}
	revision := response.GetRevision()
	answer.Revision = revision.GetRevision()
	answer.Dirty = revision.GetDirty()
	for _, warning := range revision.GetWarnings() {
		if value := warning.GetValue(); value != "" {
			answer.Warnings = append(answer.Warnings, value)
		}
	}
	packages := map[string]struct{}{}
	for _, symbol := range response.GetSymbols() {
		row := atomcollection.AtomRow{
			Key:            symbol.GetKey(),
			Name:           symbol.GetName(),
			Kind:           symbol.GetKind(),
			Receiver:       symbol.GetReceiver(),
			ImportPath:     symbol.GetImportPath(),
			Path:           symbol.GetWorkspaceRelativePath(),
			Line:           symbol.GetLine(),
			Admitted:       symbol.GetAtom(),
			Problem:        symbol.GetAtomProblem(),
			MatchedName:    symbol.GetMatchedName(),
			MatchedPromise: symbol.GetMatchedPromise().GetValue(),
		}
		if row.Key == "" || row.Name == "" {
			// A row the inspector cannot ask about again is not drawn: every
			// action on this surface is keyed by that identity.
			continue
		}
		if !row.Admitted && strings.TrimSpace(row.Problem) == "" {
			// An ordinary declaration that never asked to be an atom belongs to
			// the code collection, not here. Only admitted atoms and directives
			// that failed to admit are this surface's subject.
			continue
		}
		if row.Admitted {
			answer.Admitted++
		} else {
			answer.Refused++
		}
		if row.ImportPath != "" {
			packages[row.ImportPath] = struct{}{}
		}
		answer.Rows = append(answer.Rows, row)
	}
	answer.Packages = make([]string, 0, len(packages))
	for importPath := range packages {
		answer.Packages = append(answer.Packages, importPath)
	}
	sort.Strings(answer.Packages)
	// Admitted atoms first, then by package and name, so the collection reads
	// the same way twice and the refused directives gather at the end where
	// they can be worked through.
	sort.SliceStable(answer.Rows, func(first, second int) bool {
		left, right := answer.Rows[first], answer.Rows[second]
		if left.Admitted != right.Admitted {
			return left.Admitted
		}
		if left.ImportPath != right.ImportPath {
			return left.ImportPath < right.ImportPath
		}
		return left.Name < right.Name
	})
	answer.Collection = response.GetTotalAtoms()
	answer.SearchQuery = response.GetSearchQuery()
	answer.Truncated = response.GetPage().GetHasMore()
	return answer
}

// filterAtomRows applies the search, the package filter, and the refused
// toggle in the browser.
//
// The coordinator's own search matches declaration names; this one also
// matches the package and the receiver, because "every atom on this type" and
// "every atom in this package" are the two questions people actually arrive
// with.
func filterAtomRows(
	rows []atomcollection.AtomRow,
	search, importPath string,
	showRefused bool,
) []atomcollection.AtomRow {
	needle := strings.ToLower(strings.TrimSpace(search))
	result := make([]atomcollection.AtomRow, 0, len(rows))
	for _, row := range rows {
		if !row.Admitted && !showRefused {
			continue
		}
		if importPath != "" && row.ImportPath != importPath {
			continue
		}
		if needle != "" && !matchesAtomSearch(row, needle) {
			continue
		}
		result = append(result, row)
	}
	return result
}

func matchesAtomSearch(row atomcollection.AtomRow, needle string) bool {
	for _, field := range []string{row.Name, row.Receiver, row.ImportPath, row.Path} {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}

// projectAtomDetail reads one declaration into the inspector's shape.
func projectAtomDetail(response *codefluxv1.InspectCodeSymbolResponse) atomcollection.AtomDetail {
	symbol := response.GetSymbol()
	detail := atomcollection.AtomDetail{
		Row: atomcollection.AtomRow{
			Key:        symbol.GetKey(),
			Name:       symbol.GetName(),
			Kind:       symbol.GetKind(),
			Receiver:   symbol.GetReceiver(),
			ImportPath: symbol.GetImportPath(),
			Path:       symbol.GetWorkspaceRelativePath(),
			Line:       symbol.GetLine(),
			Admitted:   symbol.GetAtom(),
			Problem:    symbol.GetAtomProblem(),
		},
		Signature:        response.GetSignature().GetValue(),
		OpeningSentence:  response.GetAtomOpeningSentence(),
		SchemaVersion:    response.GetAtomSchemaVersion(),
		Implements:       response.GetImplements(),
		ImplementedBy:    response.GetImplementedBy(),
		Source:           response.GetSource().GetValue(),
		SourceStartLine:  response.GetSourceStartLine(),
		SourceTruncated:  response.GetSourceTruncated(),
		NamingChecked:    response.GetNamingChecked(),
		NamingFinding:    response.GetNamingFinding().GetValue(),
		DocumentedFields: response.GetDocumentedFields(),
		MissingFields:    response.GetMissingFields(),
		AccessQuery:      response.GetAccessQuery(),
		IndexSchema:      response.GetIndexSchema(),
	}
	if structure := response.GetStructure(); structure.GetMeasured() {
		detail.Structure = atomcollection.Structure{
			Measured:   true,
			TimeBound:  structure.GetTimeBound(),
			SpaceClaim: structure.GetSpaceClaim(),
			LoopDepth:  structure.GetLoopDepth(),
			Branches:   structure.GetBranches(),
			Pure:       structure.GetPure(),
		}
	}
	for _, line := range response.GetDocumentation() {
		detail.Documentation = append(detail.Documentation, line.GetValue())
	}
	for _, field := range response.GetAtomFields() {
		entry := atomcollection.AtomField{
			Label: field.GetLabel(), Text: unwrapDocumentationProse(field.GetText().GetValue()),
		}
		for _, item := range field.GetItems() {
			entry.Items = append(entry.Items, item.GetValue())
		}
		detail.Fields = append(detail.Fields, entry)
	}
	detail.Callers = projectAtomReferences(response.GetCallers())
	detail.Callees = projectAtomReferences(response.GetCallees())
	detail.Tests = projectAtomReferences(response.GetTests())
	return detail
}

func projectAtomReferences(references []*codefluxv1.CodeSymbolReferenceView) []atomcollection.Reference {
	if len(references) == 0 {
		return nil
	}
	result := make([]atomcollection.Reference, 0, len(references))
	for _, reference := range references {
		result = append(result, atomcollection.Reference{
			Name: reference.GetName(),
			Path: reference.GetWorkspaceRelativePath(),
			Line: reference.GetLine(),
		})
	}
	return result
}

// unwrapDocumentationProse rejoins lines the author wrapped in the source.
//
// Atom documentation is written inside a Go comment wrapped near column 72, so
// rendering it with the source line breaks intact produced a ragged column
// that broke mid-sentence at whatever width the author's editor used. A blank
// line is a paragraph the author meant; a single line break is not.
func unwrapDocumentationProse(text string) string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	paragraphs := strings.Split(normalized, "\n\n")
	rejoined := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		fields := strings.Fields(paragraph)
		if len(fields) == 0 {
			continue
		}
		rejoined = append(rejoined, strings.Join(fields, " "))
	}
	return strings.Join(rejoined, "\n\n")
}
