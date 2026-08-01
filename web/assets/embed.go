package assets

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// embedded holds the browser assets compiled into the executable (M23-053).
//
// The directory is populated by `codeflux-dev generate`; in a development
// checkout it is empty, and Resolve falls back to reading the build directory.
// A release build embeds the real files, so a released executable needs
// nothing beside it on disk.
//
//go:embed all:static
var embedded embed.FS

// ErrAssetsUnavailable reports that neither embedded nor directory assets
// could be found.
var ErrAssetsUnavailable = errors.New("no browser assets are available")

// RequiredAssets are the files a working frontend cannot start without.
func RequiredAssets() []string {
	return []string{"index.html", "wasm_exec.js", "bin/main.wasm"}
}

// Source describes where a resolved asset set came from, so a diagnostic can
// say "this is a release build" or "this is reading your build directory"
// rather than leaving a user to guess.
type Source string

const (
	// SourceEmbedded means the assets are compiled into the executable.
	SourceEmbedded Source = "embedded"
	// SourceDirectory means they were read from disk.
	SourceDirectory Source = "directory"
)

// Resolved is one usable asset set.
type Resolved struct {
	Source Source
	// Files maps a slash-separated relative path to its contents.
	Files map[string][]byte
}

// Get returns one asset.
func (resolved Resolved) Get(relative string) ([]byte, error) {
	content, ok := resolved.Files[relative]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAssetsUnavailable, relative)
	}
	return content, nil
}

// Paths returns every resolved path, sorted.
func (resolved Resolved) Paths() []string {
	paths := make([]string, 0, len(resolved.Files))
	for relative := range resolved.Files {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	return paths
}

// Embedded reports whether anything was compiled in.
//
// It is exported because a doctor and a release check both need to distinguish
// a self-contained executable from one that depends on a directory.
func Embedded() bool {
	// The check is for a REQUIRED asset, not for any file. The directory
	// always holds a placeholder so Go can embed it at all, and counting
	// entries would make every development build claim to be self-contained.
	for _, relative := range RequiredAssets() {
		if _, err := embedded.ReadFile(path.Join("static", relative)); err != nil {
			return false
		}
	}
	return true
}

// Resolve returns the browser assets, preferring embedded ones (M23-053).
//
// directory is the development fallback. It is consulted only when nothing was
// embedded, so a release build can never be silently served stale files from
// somebody's working tree.
func Resolve(directory string) (Resolved, error) {
	if Embedded() {
		files, err := readEmbedded()
		if err != nil {
			return Resolved{}, err
		}
		if err := requireAll(files); err != nil {
			return Resolved{}, err
		}
		return Resolved{Source: SourceEmbedded, Files: files}, nil
	}
	if strings.TrimSpace(directory) == "" {
		return Resolved{}, fmt.Errorf(
			"%w: nothing is embedded and no build directory was supplied", ErrAssetsUnavailable)
	}
	files, err := readDirectory(directory)
	if err != nil {
		return Resolved{}, err
	}
	if err := requireAll(files); err != nil {
		return Resolved{}, err
	}
	return Resolved{Source: SourceDirectory, Files: files}, nil
}

func readEmbedded() (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(embedded, "static", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, readErr := embedded.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		files[strings.TrimPrefix(filepath.ToSlash(name), "static/")] = content
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read embedded assets: %w", err)
	}
	return files, nil
}

func readDirectory(directory string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, relative := range RequiredAssets() {
		full := filepath.Join(directory, filepath.FromSlash(relative))
		content, err := os.ReadFile(full)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: %s is not in the build directory",
					ErrAssetsUnavailable, relative)
			}
			return nil, fmt.Errorf("read %s: %w", relative, err)
		}
		files[relative] = content
	}
	return files, nil
}

// requireAll refuses an asset set missing anything the frontend needs.
//
// A partial set is worse than none: the server would start, serve a page, and
// fail in the browser with an error the user cannot act on.
func requireAll(files map[string][]byte) error {
	var missing []string
	for _, relative := range RequiredAssets() {
		content, ok := files[relative]
		if !ok || len(content) == 0 {
			missing = append(missing, relative)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: missing %s", ErrAssetsUnavailable, strings.Join(missing, ", "))
	}
	return nil
}
