//go:build js && wasm

package main

import (
	"context"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/codecollection"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// mountedCollectionTimeout bounds one collection read.
//
// Mapping runs the Go toolchain over a repository, which is slower than any
// other read this console makes, so the bound is generous and the surface says
// what it is waiting for rather than spinning without explanation.
const mountedCollectionTimeout = 90 * time.Second

// mountedSymbolTimeout bounds a listing or an inspection against an already
// mapped repository.
const mountedSymbolTimeout = 15 * time.Second

// useMountedCodeCollection asks the coordinator what code a repository
// contains.
func useMountedCodeCollection(
	active bool,
	repositoryID domain.RepositoryID,
) codecollection.Props {
	selectedPackage := ui.UseState("")
	selectedSymbol := ui.UseState("")
	search := ui.UseState("")
	exportedOnly := ui.UseState(false)
	atomsOnly := ui.UseState(false)

	identity := ""
	if !repositoryID.IsZero() {
		identity = repositoryID.String()
	}
	packagesDependency := "inactive"
	if active && identity != "" {
		packagesDependency = "packages|" + identity
	}
	packages := fetch.UseResource(func(parent context.Context) (codeCollectionAnswer, error) {
		if !active || identity == "" {
			return codeCollectionAnswer{}, nil
		}
		ctx, cancel := context.WithTimeout(parent, mountedCollectionTimeout)
		defer cancel()
		return listMountedCodePackages(ctx, identity)
	}, packagesDependency)

	symbolsDependency := "inactive"
	if active && identity != "" {
		symbolsDependency = "symbols|" + identity + "|" + selectedPackage.Get() + "|" +
			search.Get() + "|" + boolKey(exportedOnly.Get()) + "|" + boolKey(atomsOnly.Get())
	}
	symbols := fetch.UseResource(func(parent context.Context) (codeSymbolAnswer, error) {
		if !active || identity == "" {
			return codeSymbolAnswer{}, nil
		}
		if selectedPackage.Get() == "" && search.Get() == "" && !atomsOnly.Get() {
			// Listing every declaration in a repository before somebody has
			// asked for one would spend the whole page's time on an answer
			// nobody is reading.
			return codeSymbolAnswer{}, nil
		}
		ctx, cancel := context.WithTimeout(parent, mountedSymbolTimeout)
		defer cancel()
		return listMountedCodeSymbols(ctx, identity, mountedSymbolFilter{
			ImportPath:   selectedPackage.Get(),
			Search:       search.Get(),
			ExportedOnly: exportedOnly.Get(),
			AtomsOnly:    atomsOnly.Get(),
		})
	}, symbolsDependency)

	detailDependency := "inactive"
	if active && identity != "" && selectedSymbol.Get() != "" {
		detailDependency = "detail|" + identity + "|" + selectedSymbol.Get()
	}
	detail := fetch.UseResource(func(parent context.Context) (codecollection.Detail, error) {
		if !active || identity == "" || selectedSymbol.Get() == "" {
			return codecollection.Detail{}, nil
		}
		ctx, cancel := context.WithTimeout(parent, mountedSymbolTimeout)
		defer cancel()
		return inspectMountedCodeSymbol(ctx, identity, selectedSymbol.Get())
	}, detailDependency)

	currentPackages := packages.Get()
	currentSymbols := symbols.Get()
	currentDetail := detail.Get()
	props := codecollection.Props{
		SelectedPackage: selectedPackage.Get(),
		SelectedSymbol:  selectedSymbol.Get(),
		Search:          search.Get(),
		ExportedOnly:    exportedOnly.Get(),
		AtomsOnly:       atomsOnly.Get(),
	}
	if identity == "" {
		props.Unavailable = true
		props.UnavailableReason = "A collection is read from an open repository. " +
			"Choose a repository first."
		return props
	}
	if !active {
		return props
	}
	props.Loading = currentPackages.Loading || !currentPackages.Ready
	props.Failed = currentPackages.Error != nil
	if currentPackages.Ready && currentPackages.Error == nil {
		answer := currentPackages.Value
		props.Revision = answer.Revision
		props.Dirty = answer.Dirty
		props.Warnings = answer.Warnings
		props.Packages = answer.Packages
		props.TotalPackages = answer.TotalPackages
		props.TotalSymbols = answer.TotalSymbols
		props.TotalAtoms = answer.TotalAtoms
		props.Truncated = answer.Truncated
	}
	props.SymbolsLoading = currentSymbols.Loading
	if currentSymbols.Ready && currentSymbols.Error == nil {
		props.Symbols = currentSymbols.Value.Symbols
		props.SymbolsMatched = currentSymbols.Value.Matched
		props.SymbolsTruncated = currentSymbols.Value.Truncated
	}
	props.Detail = currentDetail.Value
	props.Detail.Loading = currentDetail.Loading || (selectedSymbol.Get() != "" && !currentDetail.Ready)
	props.Detail.Failed = currentDetail.Error != nil

	props.OnReload = func() {
		packages.Reload()
		symbols.Reload()
	}
	props.OnSelectPackage = func(importPath string) {
		if selectedPackage.Get() == importPath {
			selectedPackage.Set("")
			return
		}
		selectedPackage.Set(importPath)
		selectedSymbol.Set("")
	}
	props.OnSelectSymbol = func(key string) { selectedSymbol.Set(key) }
	props.OnSearch = func(value string) {
		search.Set(value)
		selectedSymbol.Set("")
	}
	props.OnToggleExported = func() { exportedOnly.Set(!exportedOnly.Get()) }
	props.OnToggleAtoms = func() {
		atomsOnly.Set(!atomsOnly.Get())
		selectedSymbol.Set("")
	}
	return props
}

// mountedSymbolFilter narrows one declaration listing.
type mountedSymbolFilter struct {
	ImportPath   string
	Search       string
	ExportedOnly bool
	AtomsOnly    bool
}

// boolKey renders a flag for a resource dependency key.
func boolKey(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// repositoryIdentityFor builds the typed identity the collection calls carry.
func repositoryIdentityFor(value string) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
		Value: value,
	}
}
