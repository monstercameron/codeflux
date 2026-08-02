package codecollection_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/codecollection"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func render(t *testing.T, props codecollection.Props) string {
	t.Helper()
	markup, err := ui.RenderToString(codecollection.Component(props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTheCollectionNamesTheRevisionItDescribes(t *testing.T) {
	markup := render(t, codecollection.Props{
		Revision: "2994deb59e08aaaa", Dirty: true,
		TotalPackages: 94, TotalSymbols: 20261, TotalAtoms: 2,
		Packages: []codecollection.PackageRow{{
			ImportPath: "codeflux.dev/codeflux/internal/policy", SymbolCount: 44, AtomCount: 2,
		}},
	})
	for _, want := range []string{
		"94 packages", "20261 declarations", "2 documented atoms",
		"2994deb59e08", "Uncommitted changes",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup lacks %q: %s", want, markup)
		}
	}
	// The revision is shortened for reading, not truncated into a different
	// identity, so the long form must not survive as a claim.
	if strings.Contains(markup, "2994deb59e08aaaa") {
		t.Fatal("the full revision was rendered where a short one belongs")
	}
}

func TestCountsAgreeWithTheirNouns(t *testing.T) {
	markup := render(t, codecollection.Props{
		Revision: "abc", TotalPackages: 1, TotalSymbols: 1, TotalAtoms: 1,
		Packages: []codecollection.PackageRow{{ImportPath: "one", SymbolCount: 1}},
	})
	for _, want := range []string{"1 package ", "1 declaration ", "1 documented atom "} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup lacks %q: %s", want, markup)
		}
	}
}

func TestAnUnadmittedAtomIsNotShownAsDocumented(t *testing.T) {
	markup := render(t, codecollection.Props{
		Revision:        "abc",
		Packages:        []codecollection.PackageRow{{ImportPath: "one"}},
		SelectedPackage: "one",
		Symbols: []codecollection.SymbolRow{
			{Key: "a", Name: "Admitted", Kind: "function", Exported: true, Atom: true},
			{
				Key: "b", Name: "Broken", Kind: "function", Exported: true,
				AtomProblem: "the atom documentation comment does not parse",
			},
		},
		OnSelectSymbol: func(string) {},
	})
	if !strings.Contains(markup, "Documented atom") {
		t.Fatalf("an admitted atom must be labelled: %s", markup)
	}
	if !strings.Contains(markup, "Atom directive, unparsed documentation") {
		t.Fatalf("a directive without documentation must say so: %s", markup)
	}
}

func TestADeclarationIsReadWithoutItsBody(t *testing.T) {
	markup := render(t, codecollection.Props{
		Revision:       "abc",
		Packages:       []codecollection.PackageRow{{ImportPath: "one"}},
		SelectedSymbol: "a",
		Detail: codecollection.Detail{
			Symbol:    codecollection.SymbolRow{Key: "a", Name: "Select", Path: "internal/policy/fixed.go", Line: 151},
			Signature: "func Select(input SelectionInput) (Snapshot, error)",
			Documentation: []string{
				"Select returns the frozen baseline.", "", "It performs no I/O.",
			},
			AtomSchemaVersion:   1,
			AtomOpeningSentence: "Select chooses the policy a run uses.",
			AtomFields: []codecollection.AtomField{
				{Label: "Purpose", Text: "Choose the model policy a run uses."},
				{Label: "Inputs", Items: []string{"input: the selection inputs"}},
			},
			Callers: []codecollection.Reference{
				{Name: "TestSelect", Path: "internal/policy/fixed_test.go", Line: 15},
			},
		},
	})
	for _, want := range []string{
		"internal/policy/fixed.go:151",
		"func Select(input SelectionInput) (Snapshot, error)",
		"It performs no I/O.",
		"Documented atom, schema v1",
		"Choose the model policy a run uses.",
		"input: the selection inputs",
		"TestSelect · internal/policy/fixed_test.go:15",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("detail markup lacks %q: %s", want, markup)
		}
	}
}

func TestAnEmptyCallListSaysSoRatherThanVanishing(t *testing.T) {
	markup := render(t, codecollection.Props{
		Revision:       "abc",
		Packages:       []codecollection.PackageRow{{ImportPath: "one"}},
		SelectedSymbol: "a",
		Detail: codecollection.Detail{
			Symbol: codecollection.SymbolRow{Key: "a", Name: "Orphan"},
		},
	})
	if !strings.Contains(markup, "Called by: none recorded at this revision.") {
		t.Fatalf("an empty call list must say so: %s", markup)
	}
}

func TestTheCollectionSaysWhichStateItIsIn(t *testing.T) {
	loading := render(t, codecollection.Props{Loading: true})
	if !strings.Contains(loading, "Mapping the repository with the Go toolchain") {
		t.Errorf("loading markup lacks its state: %s", loading)
	}
	failed := render(t, codecollection.Props{Failed: true, OnReload: func() {}})
	for _, want := range []string{"could not be read", "mapping only reads", "Reload the collection"} {
		if !strings.Contains(failed, want) {
			t.Errorf("failure markup lacks %q: %s", want, failed)
		}
	}
	unavailable := render(t, codecollection.Props{
		Unavailable: true, UnavailableReason: "Choose a repository first.",
	})
	if !strings.Contains(unavailable, "Choose a repository first.") {
		t.Errorf("unavailable markup lacks its reason: %s", unavailable)
	}
	empty := render(t, codecollection.Props{Revision: "abc", OnReload: func() {}})
	if !strings.Contains(empty, "No package is mapped") {
		t.Errorf("an empty collection must say so: %s", empty)
	}
}
