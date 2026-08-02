package coordinator

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/transport"
)

// maximumFileLines bounds one file read.
//
// A directory of code that cannot show a file is a directory nobody can act
// on, but a read with no ceiling is a way to hand a browser a generated file
// the size of a dictionary. The bound is reported rather than hidden, so a
// reader knows the file continues past what they were shown.
const maximumFileLines = 4000

// ListCodeFiles returns every file the repository map recorded.
func (application *codeCollectionApplication) ListCodeFiles(
	ctx context.Context,
	query transport.CodeCollectionQuery,
) (transport.CodeFilePage, error) {
	collected, err := application.collect(ctx, query.RepositoryID)
	if err != nil {
		return transport.CodeFilePage{}, err
	}
	limit := boundedCodeFileLimit(query.Limit)
	search := strings.ToLower(strings.TrimSpace(query.Search))
	page := transport.CodeFilePage{
		Revision:   collected.revisionView(),
		TotalFiles: uint32(len(collected.files)),
	}
	for _, file := range collected.files {
		if search != "" && !strings.Contains(strings.ToLower(file.Path), search) {
			continue
		}
		if len(page.Files) == limit {
			page.Truncated = true
			break
		}
		page.Files = append(page.Files, file)
	}
	return page, nil
}

// boundedCodeFileLimit bounds a file listing.
func boundedCodeFileLimit(limit int) int {
	if limit <= 0 || limit > transport.MaximumCodeFilePage {
		return transport.MaximumCodeFilePage
	}
	return limit
}

// ReadCodeFile returns one file's text and what it declares.
func (application *codeCollectionApplication) ReadCodeFile(
	ctx context.Context,
	read transport.CodeFileRead,
) (transport.CodeFileContent, error) {
	collected, err := application.collect(ctx, read.RepositoryID)
	if err != nil {
		return transport.CodeFileContent{}, err
	}
	relative := strings.TrimSpace(read.Path)
	record, present := collected.fileByPath[relative]
	if !present {
		// Only a file the map recorded can be read. Resolving an arbitrary
		// path against the repository root would turn a directory listing into
		// a way to read anything the coordinator can reach.
		return transport.CodeFileContent{}, transport.ErrCodeSymbolNotFound
	}
	text, err := readBoundedSource(filepath.Join(collected.root, filepath.FromSlash(relative)))
	if err != nil {
		return transport.CodeFileContent{}, err
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	content := transport.CodeFileContent{
		Revision: collected.revisionView(),
		File:     record,
		Lines:    uint32(len(lines)),
	}
	if len(lines) > maximumFileLines {
		lines = lines[:maximumFileLines]
		content.Truncated = true
	}
	content.Text = strings.Join(lines, "\n")
	content.Declarations = collected.declarationsIn(relative)
	return content, nil
}

// declarationsIn returns what one file declares, in the order it declares it.
//
// Source order rather than name order: a reader scrolling a file expects the
// list beside it to run the same way the file does.
func (collected *collectedRepository) declarationsIn(path string) []transport.CodeSymbolRecord {
	var declared []transport.CodeSymbolRecord
	for _, symbol := range collected.symbols {
		if symbol.Path == path {
			declared = append(declared, symbol)
		}
	}
	sort.SliceStable(declared, func(first, second int) bool {
		return declared[first].Line < declared[second].Line
	})
	return declared
}
