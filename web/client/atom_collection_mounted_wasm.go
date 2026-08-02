//go:build js && wasm

package main

import (
	"context"
	"errors"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/atomcollection"
	"codeflux.dev/codeflux/web/frontend/design"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// mountedAtomTimeout bounds a collection read. Parsing a repository's source
// is slower than a database query, and a person who asked for the atoms of a
// large module is waiting on a compiler-shaped amount of work.
const mountedAtomTimeout = 20 * time.Second

// atomSearchDebounce is how long typing settles before the coordinator is
// asked. It is long enough that a typed phrase is one query rather than one
// per word, and short enough that a person who has stopped typing is not left
// watching a list that has not caught up.
const atomSearchDebounce = 450 * time.Millisecond

var errAtomScopeUnavailable = errors.New("no repository is open to read atoms from")

// heldAtomAnswer is the last answer the coordinator gave and the term it was
// for. Keeping the term is what lets the surface tell "these rows already
// answer what is typed" from "these rows are the previous question's".
type heldAtomAnswer struct {
	Answer  atomCollectionAnswer
	Term    string
	Present bool
}

// useMountedAtoms reads every atom in the open repository and keeps one open.
//
// The listing is a single read of the whole collection rather than a page per
// filter, because filtering is what a person does repeatedly here and
// re-parsing the repository on every keystroke would make the surface unusable.
// The declaration read is separate and happens only for the selected row.
func useMountedAtoms(repositoryID string, tokens design.Tokens) *atomcollection.Props {
	search := ui.UseState("")
	packageFilter := ui.UseState("")
	showRefused := ui.UseState(false)
	notesExpanded := ui.UseState(false)
	selected := ui.UseState("")
	lastAnswer := ui.UseState(heldAtomAnswer{})
	copied := ui.UseState("")

	// The coordinator's search reads the documented promises through a
	// full-text index; the local filter below reads only what is already
	// loaded. Typing narrows instantly against what is on screen, and the
	// wider answer arrives a moment later, so neither the responsiveness nor
	// the reach is given up.
	submitted := useDebouncedValue(search.Get(), atomSearchDebounce)
	dependency := "unavailable"
	if repositoryID != "" {
		dependency = repositoryID + "|" + submitted
		if showRefused.Get() {
			// Refused directives are a second, wider read: the coordinator's
			// atoms-only listing returns admitted atoms alone, so asking for
			// the refusals means asking for every declaration and keeping the
			// ones that carry a directive. That is not a cost the ordinary
			// view should pay.
			dependency += "|with-refused"
		}
	}
	listing := fetch.UseResource(func(parent context.Context) (atomCollectionAnswer, error) {
		if repositoryID == "" {
			return atomCollectionAnswer{}, errAtomScopeUnavailable
		}
		ctx, cancel := context.WithTimeout(parent, mountedAtomTimeout)
		defer cancel()
		connection, err := openBrowserCollectionConnection(ctx)
		if err != nil {
			return atomCollectionAnswer{}, err
		}
		defer func() { _ = connection.Close() }()
		response, err := codefluxv1.NewCodeCollectionServiceClient(connection).ListCodeSymbols(
			ctx,
			&codefluxv1.ListCodeSymbolsRequest{
				RepositoryId: repositoryIdentityFor(repositoryID),
				AtomsOnly:    !showRefused.Get(),
				Search:       submitted,
			},
		)
		if err != nil {
			return atomCollectionAnswer{}, err
		}
		return projectAtomSymbols(response), nil
	}, dependency)

	detailDependency := "none"
	if selected.Get() != "" && repositoryID != "" {
		detailDependency = repositoryID + "|" + selected.Get()
	}
	detail := fetch.UseResource(func(parent context.Context) (atomcollection.AtomDetail, error) {
		if detailDependency == "none" {
			return atomcollection.AtomDetail{}, errAtomScopeUnavailable
		}
		ctx, cancel := context.WithTimeout(parent, mountedAtomTimeout)
		defer cancel()
		connection, err := openBrowserCollectionConnection(ctx)
		if err != nil {
			return atomcollection.AtomDetail{}, err
		}
		defer func() { _ = connection.Close() }()
		response, err := codefluxv1.NewCodeCollectionServiceClient(connection).InspectCodeSymbol(
			ctx,
			&codefluxv1.InspectCodeSymbolRequest{
				RepositoryId: repositoryIdentityFor(repositoryID), Key: selected.Get(),
			},
		)
		if err != nil {
			return atomcollection.AtomDetail{}, err
		}
		return projectAtomDetail(response), nil
	}, detailDependency)

	props := &atomcollection.Props{
		Tokens: tokens,
		Search: search.Get(), PackageFilter: packageFilter.Get(),
		ShowRefused: showRefused.Get(), SelectedKey: selected.Get(),
		OnSearch: search.Set,
		OnPackage: func(importPath string) {
			packageFilter.Set(importPath)
		},
		OnShowRefused: showRefused.Set,
		NotesExpanded: notesExpanded.Get(),
		OnNotesToggle: notesExpanded.Set,
		OnSelect:      selected.Set,
		CopiedQuery:   copied.Get(),
		OnCopy: func(query string) {
			copyToClipboard(query)
			copied.Set(query)
		},
		OnRetry: listing.Reload,
	}
	current := listing.Get()
	// The last answer is held so a reload has something to draw. Without it the
	// list empties the instant a new query starts and fills again when it
	// finishes, which under a person typing is a list that blinks once per
	// word and is never the thing they are reading.
	held := lastAnswer.Get()
	if current.Ready && current.Error == nil {
		held = heldAtomAnswer{Answer: current.Value, Term: submitted, Present: true}
		// The held answer is replaced only when the term it answers changes.
		// Writing it on every render would set state during a render and ask
		// for another one, every render, forever.
		previous := lastAnswer.Get()
		if !previous.Present || previous.Term != held.Term ||
			len(previous.Answer.Rows) != len(held.Answer.Rows) {
			lastAnswer.Set(held)
		}
	}
	switch {
	case repositoryID == "":
		props.State = atomcollection.LoadUnavailable
	case current.Error != nil && !held.Present:
		props.State = atomcollection.LoadFailed
		props.ErrorMessage = current.Error.Error()
	case current.Ready && current.Error == nil:
		props.State = atomcollection.LoadReady
	case held.Present:
		props.State = atomcollection.LoadLoading
	default:
		props.State = atomcollection.LoadLoading
	}
	if held.Present {
		answer := held.Answer
		// The typed term narrows what is on screen whenever it is ahead of the
		// term the shown answer was for. Once the coordinator has answered the
		// term in the box, the narrowing stops: its matches are name matches,
		// and applying it to a settled answer would throw away every atom found
		// by something it promises rather than by what it is called.
		local := ""
		if search.Get() != held.Term {
			local = search.Get()
		}
		props.Rows = filterAtomRows(
			answer.Rows, local, packageFilter.Get(), showRefused.Get(),
		)
		// The local narrowing matches names only. When the typed term is a word
		// from a documented promise it matches no name, and narrowing would
		// empty the list a moment before the coordinator answers with the atoms
		// that promise it. An answer this layer cannot give is left to the one
		// that can: the rows stay, and the surface says it is still searching.
		if local != "" && len(props.Rows) == 0 {
			props.Rows = filterAtomRows(
				answer.Rows, "", packageFilter.Get(), showRefused.Get(),
			)
		}
		props.SearchPending = search.Get() != submitted || !current.Ready
		props.Packages = answer.Packages
		props.Revision = answer.Revision
		props.Dirty = answer.Dirty
		props.Warnings = answer.Warnings
		props.TotalAtoms = answer.Admitted
		props.TotalRefused = answer.Refused
		props.SearchQuery = answer.SearchQuery
		// The collection's size comes from the coordinator on every answer,
		// searched or not, so a filtered list can say what it is a part of
		// without the browser holding a number across renders.
		props.CollectionAtoms = answer.Collection
		props.Searched = strings.TrimSpace(held.Term) != ""
	}
	if props.State == atomcollection.LoadUnavailable || props.State == atomcollection.LoadFailed {
		return props
	}
	currentDetail := detail.Get()
	switch {
	case selected.Get() == "":
		props.DetailState = atomcollection.LoadReady
	case currentDetail.Loading:
		props.DetailState = atomcollection.LoadLoading
	case currentDetail.Error != nil:
		props.DetailState = atomcollection.LoadFailed
		props.DetailError = currentDetail.Error.Error()
	case currentDetail.Ready:
		props.DetailState = atomcollection.LoadReady
		value := currentDetail.Value
		props.Detail = &value
	default:
		props.DetailState = atomcollection.LoadLoading
	}
	return props
}
