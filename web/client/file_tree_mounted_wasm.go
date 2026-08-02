//go:build js && wasm

package main

import (
	"context"
	"errors"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/filetree"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const mountedFileTimeout = 20 * time.Second

// fileSearchDebounce is how long typing settles before the coordinator is
// asked again. It matches the atom search: long enough that a burst of
// keystrokes is one question, short enough that a pause answers.
const fileSearchDebounce = 450 * time.Millisecond

var errFileScopeUnavailable = errors.New("no repository is open to read files from")

// useMountedFileTree reads a repository's files and keeps one open.
//
// The listing is one read of the whole tree: filtering and expanding are what a
// person does repeatedly here, and asking the coordinator again for each would
// make the tree feel like a network resource rather than a directory. The file
// read is separate and happens only for the file that was opened.
func useMountedFileTree(active bool, repositoryID string, tokens design.Tokens) *filetree.Props {
	search := ui.UseState("")
	held := ui.UseState(fileTreeAnswer{})
	expanded := ui.UseState(map[string]bool{})
	selected := ui.UseState("")
	selectedLine := ui.UseState(uint32(0))

	// The typed term is sent to the coordinator as well as applied locally.
	// Local narrowing answers instantly and covers what is already here; the
	// coordinator's answer covers the whole repository, which matters as soon
	// as a repository has more files than one page carries.
	submitted := useDebouncedValue(search.Get(), fileSearchDebounce)
	dependency := "inactive"
	if active && repositoryID != "" {
		dependency = repositoryID + "|" + submitted
	}
	listing := fetch.UseResource(func(parent context.Context) (fileTreeAnswer, error) {
		if !active || repositoryID == "" {
			return fileTreeAnswer{}, errFileScopeUnavailable
		}
		ctx, cancel := context.WithTimeout(parent, mountedFileTimeout)
		defer cancel()
		connection, err := openBrowserCollectionConnection(ctx)
		if err != nil {
			return fileTreeAnswer{}, err
		}
		defer func() { _ = connection.Close() }()
		response, err := codefluxv1.NewCodeCollectionServiceClient(connection).ListCodeFiles(
			ctx, &codefluxv1.ListCodeFilesRequest{
				RepositoryId: repositoryIdentityFor(repositoryID),
				Search:       submitted,
			},
		)
		if err != nil {
			return fileTreeAnswer{}, err
		}
		return projectCodeFiles(response), nil
	}, dependency)

	contentDependency := "none"
	if active && repositoryID != "" && selected.Get() != "" {
		contentDependency = repositoryID + "|" + selected.Get()
	}
	content := fetch.UseResource(func(parent context.Context) (filetree.Content, error) {
		if contentDependency == "none" {
			return filetree.Content{}, errFileScopeUnavailable
		}
		ctx, cancel := context.WithTimeout(parent, mountedFileTimeout)
		defer cancel()
		connection, err := openBrowserCollectionConnection(ctx)
		if err != nil {
			return filetree.Content{}, err
		}
		defer func() { _ = connection.Close() }()
		response, err := codefluxv1.NewCodeCollectionServiceClient(connection).ReadCodeFile(
			ctx, &codefluxv1.ReadCodeFileRequest{
				RepositoryId:          repositoryIdentityFor(repositoryID),
				WorkspaceRelativePath: selected.Get(),
			},
		)
		if err != nil {
			return filetree.Content{}, err
		}
		return projectCodeFileContent(response), nil
	}, contentDependency)

	props := &filetree.Props{
		Tokens: tokens, Search: search.Get(), Expanded: expanded.Get(),
		SelectedPath: selected.Get(), SelectedLine: selectedLine.Get(),
		OnSearch: search.Set,
		OnToggleDir: func(path string) {
			next := map[string]bool{}
			for key, open := range expanded.Get() {
				next[key] = open
			}
			next[path] = !next[path]
			expanded.Set(next)
		},
		OnSelectFile: func(path string) {
			selected.Set(path)
			// The line marker belongs to the file it was chosen in; carrying it
			// across would highlight an unrelated line in the next file.
			selectedLine.Set(0)
		},
		OnSelectLine: selectedLine.Set,
		OnRetry:      listing.Reload,
	}
	current := listing.Get()
	if current.Ready {
		// The last answer is kept so a re-read for a new term redraws the tree
		// it already had instead of blanking it while the next one arrives.
		held.Set(current.Value)
	}
	answer := held.Get()
	switch {
	case !active || repositoryID == "":
		props.State = filetree.LoadUnavailable
	case current.Error != nil:
		props.State = filetree.LoadFailed
		props.ErrorMessage = current.Error.Error()
	case current.Ready || len(answer.Files) > 0:
		props.State = filetree.LoadReady
		props.Files = narrowedTreeFiles(answer.Files, search.Get())
		props.Revision = answer.Revision
		props.Dirty = answer.Dirty
		props.TotalFiles = answer.Total
	default:
		props.State = filetree.LoadLoading
	}
	if props.State != filetree.LoadReady || selected.Get() == "" {
		return props
	}
	currentContent := content.Get()
	switch {
	case currentContent.Error != nil:
		props.ContentState = filetree.LoadFailed
		props.ContentError = currentContent.Error.Error()
	case currentContent.Ready:
		props.ContentState = filetree.LoadReady
		value := currentContent.Value
		props.Content = &value
	default:
		props.ContentState = filetree.LoadLoading
	}
	return props
}
