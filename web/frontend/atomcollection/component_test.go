package atomcollection_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/atomcollection"
	"codeflux.dev/codeflux/web/frontend/design"
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

func rows() []atomcollection.AtomRow {
	return []atomcollection.AtomRow{
		{
			Key: "atom-1", Name: "ReserveStockUntilCheckoutExpires", Kind: "function",
			ImportPath: "example.com/demo/internal/inventory",
			Path:       "internal/inventory/reserve.go", Line: 55, Admitted: true,
		},
		{
			Key: "atom-2", Name: "ReleaseHold", Kind: "function",
			ImportPath: "example.com/demo/internal/inventory",
			Path:       "internal/inventory/reserve.go", Line: 100,
			Problem: "the atom documentation comment does not parse",
		},
	}
}

// TestAtomsShowRefusedDirectivesAsRefused keeps a declaration that asked to be
// an atom and was turned down visible and named. Hiding it would leave an
// author wondering why their atom never appeared.
func TestAtomsShowRefusedDirectivesAsRefused(t *testing.T) {
	markup := render(t, ui.CreateElement(atomcollection.Component, atomcollection.Props{
		Tokens: tokens(t), State: atomcollection.LoadReady, Rows: rows(),
		TotalAtoms: 1, TotalRefused: 1, ShowRefused: true, Revision: "31c62bbdeadbeef",
		OnSelect: func(string) {}, OnShowRefused: func(bool) {},
	}))
	for _, want := range []string{
		`data-component="atom-collection-shell"`,
		`data-component="atom-row"`,
		`data-admitted="false"`,
		"1 documented atom",
		"1 refused directive",
		"at 31c62bb",
		"Refused",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("atom markup missing %q", want)
		}
	}
}

// TestAtomDetailShowsWhatTheAtomPromises checks the inspector renders the
// documented fields in the author's own order, with the signature and the
// place it lives.
func TestAtomDetailShowsWhatTheAtomPromises(t *testing.T) {
	all := rows()
	detail := atomcollection.AtomDetail{
		Row:             all[0],
		Signature:       "func ReserveStockUntilCheckoutExpires(session string, count int) (Hold, error)",
		OpeningSentence: "Holds stock for one checkout session without committing the sale.",
		SchemaVersion:   1,
		Fields: []atomcollection.AtomField{
			{Label: "Purpose", Text: "Hold scarce stock for one checkout session."},
			{Label: "Failure modes", Text: "Returns ErrInsufficientStock when the count exceeds availability."},
			{Label: "Inputs", Items: []string{"session identifies the checkout", "count must be positive"}},
		},
		Callers: []atomcollection.Reference{{Name: "Checkout", Path: "internal/checkout/flow.go", Line: 12}},
		Tests: []atomcollection.Reference{
			{Name: "TestReserveStockHoldsExactlyWhatWasAsked", Path: "internal/inventory/reserve_test.go", Line: 15},
		},
		Source:          "func ReserveStockUntilCheckoutExpires() {\n\treturn nil\n}",
		SourceStartLine: 55,
		NamingChecked:   true,
		Structure: atomcollection.Structure{
			Measured: true, TimeBound: "O(n)", SpaceClaim: "bounded by the input it is given",
			LoopDepth: 1, Branches: 2, Pure: true,
		},
		DocumentedFields: []string{"Purpose", "Failure semantics"},
		MissingFields:    []string{"Preconditions", "Examples"},
		AccessQuery:      "SELECT name FROM atoms WHERE key = 'atom-1';",
		IndexSchema:      "CREATE VIRTUAL TABLE atoms USING fts5(key UNINDEXED, name);",
	}
	markup := render(t, ui.CreateElement(atomcollection.Component, atomcollection.Props{
		Tokens: tokens(t), State: atomcollection.LoadReady, Rows: all,
		TotalAtoms: 1, TotalRefused: 1,
		SelectedKey: all[0].Key, DetailState: atomcollection.LoadReady, Detail: &detail,
		OnSelect: func(string) {},
	}))
	for _, want := range []string{
		`data-component="atom-detail"`,
		"Documentation (schema v1)",
		"Purpose",
		"Failure modes",
		"count must be positive",
		"func ReserveStockUntilCheckoutExpires",
		"internal/checkout/flow.go",
		// The code is here, with the file's own line numbers, and the tests
		// that exercise it are named beside it.
		`data-component="atom-source"`,
		">55<",
		"TestReserveStockHoldsExactlyWhatWasAsked",
		"Follows the atom naming grammar",
		"Code (lines 55-57)",
		// The structural measurement the run's own atom stages make, and the
		// schema's field list split by what this atom actually answers.
		"O(n) by structure, from one loop",
		"bounded by the input it is given",
		"2 decision points",
		"Nothing outside its arguments",
		"2 of 4 declared fields answered.",
		"Not answered: Preconditions, Examples",
		// The statement that reads this atom back, and where it runs.
		`data-component="atom-query"`,
		"SELECT name FROM atoms WHERE key = &#39;atom-1&#39;;",
		"CREATE VIRTUAL TABLE atoms",
		"in-memory atom index",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("detail markup missing %q", want)
		}
	}
}

// TestAtomsSeparateAnEmptyCollectionFromAnEmptySearch keeps "this repository
// documents no atoms" distinct from "your search matched none of them": the
// two call for opposite next actions.
func TestAtomsSeparateAnEmptyCollectionFromAnEmptySearch(t *testing.T) {
	empty := render(t, ui.CreateElement(atomcollection.Component, atomcollection.Props{
		Tokens: tokens(t), State: atomcollection.LoadReady,
	}))
	if !strings.Contains(empty, "No atom is documented yet") ||
		!strings.Contains(empty, "codeflux:atom") {
		t.Errorf("empty markup = %s", empty)
	}
	searched := render(t, ui.CreateElement(atomcollection.Component, atomcollection.Props{
		Tokens: tokens(t), State: atomcollection.LoadReady, Search: "nothing",
		TotalAtoms: 2, OnSelect: func(string) {},
	}))
	if !strings.Contains(searched, "Nothing matched") {
		t.Errorf("searched markup = %s", searched)
	}
}

// TestTheSearchOffersAWayOutOfItself checks the clear control appears only
// when there is something to clear. A permanently dead button beside a search
// field teaches a person to ignore that corner of the surface.
func TestTheSearchOffersAWayOutOfItself(t *testing.T) {
	empty := render(t, ui.CreateElement(atomcollection.Component, atomcollection.Props{
		Tokens: tokens(t), State: atomcollection.LoadReady, Rows: rows(),
		TotalAtoms: 1, OnSelect: func(string) {}, OnSearch: func(string) {},
	}))
	if strings.Contains(empty, "atom-search-clear") {
		t.Errorf("an empty search offers a clear control: %s", empty)
	}
	typed := render(t, ui.CreateElement(atomcollection.Component, atomcollection.Props{
		Tokens: tokens(t), State: atomcollection.LoadReady, Rows: rows(),
		TotalAtoms: 1, Search: "retry", OnSelect: func(string) {}, OnSearch: func(string) {},
	}))
	if !strings.Contains(typed, "atom-search-clear") ||
		!strings.Contains(typed, "Clear the search") {
		t.Errorf("a typed search offers no way to clear it: %s", typed)
	}
}

// TestAMatchExplainsItself keeps a correct result from reading as a broken
// search: a row found by what it promises quotes the promise that matched.
func TestAMatchExplainsItself(t *testing.T) {
	matched := rows()
	matched[0].MatchedPromise = "Retry: Safe to retry with the same session and count."
	markup := render(t, ui.CreateElement(atomcollection.Component, atomcollection.Props{
		Tokens: tokens(t), State: atomcollection.LoadReady, Rows: matched[:1],
		Search: "retry", Searched: true, CollectionAtoms: 2, TotalAtoms: 1,
		OnSelect: func(string) {}, OnSearch: func(string) {},
	}))
	for _, want := range []string{
		`data-component="atom-match"`,
		"promises",
		"Safe to retry with the same session",
		"1 matching of 2 documented atoms",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("match markup missing %q", want)
		}
	}
}

// TestTheQueryShownIsTheQueryThatRuns keeps the SQL on screen tied to the
// statement the coordinator executed, and offers it for copying.
//
// A query rewritten for display drifts from the one that ran, and a console
// that teaches a statement nobody executes is worse than one that shows none.
func TestTheQueryShownIsTheQueryThatRuns(t *testing.T) {
	all := rows()
	copied := ""
	detail := atomcollection.AtomDetail{
		Row:         all[0],
		AccessQuery: "SELECT name FROM atoms WHERE key = 'atom-1';",
		IndexSchema: "CREATE VIRTUAL TABLE atoms USING fts5(key UNINDEXED, name);",
	}
	markup := render(t, ui.CreateElement(atomcollection.Component, atomcollection.Props{
		Tokens: tokens(t), State: atomcollection.LoadReady, Rows: all,
		SelectedKey: all[0].Key, DetailState: atomcollection.LoadReady, Detail: &detail,
		SearchQuery: "SELECT key FROM atoms WHERE atoms MATCH '\"retry\"*';",
		TotalAtoms:  1,
		OnSelect:    func(string) {},
		OnCopy:      func(query string) { copied = query },
	}))
	for _, want := range []string{
		"Read this atom",
		"This search",
		"The table they run against",
		"MATCH",
		"Copy",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("query markup missing %q", want)
		}
	}
	if copied != "" {
		t.Fatalf("rendering copied something on its own: %q", copied)
	}
	// Without a copy seam the control is not offered at all, rather than drawn
	// as a button that does nothing.
	silent := render(t, ui.CreateElement(atomcollection.Component, atomcollection.Props{
		Tokens: tokens(t), State: atomcollection.LoadReady, Rows: all,
		SelectedKey: all[0].Key, DetailState: atomcollection.LoadReady, Detail: &detail,
		TotalAtoms: 1, OnSelect: func(string) {},
	}))
	if strings.Contains(silent, "Copy the read this atom query") {
		t.Errorf("a copy control was offered with nowhere to copy to")
	}
}
