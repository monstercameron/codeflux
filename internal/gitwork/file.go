package gitwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/storage"
)

const maximumEditableFileBytes = 2 << 20

var (
	ErrUnsafeTaskPath    = errors.New("unsafe task path")
	ErrUnsupportedBinary = errors.New("unsupported binary edit")
	ErrEditConflict      = errors.New("file changed since it was read")
	ErrApprovalRequired  = errors.New("edit requires higher-risk approval")
)

// ResolvedTaskPath is a verified worktree-confined path.
type ResolvedTaskPath struct {
	Relative string
	Absolute string
}

// FileSnapshot is the expected-content precondition for one path.
type FileSnapshot struct {
	Path         string
	Exists       bool
	Content      []byte
	SHA256       string
	Mode         fs.FileMode
	NewlineStyle string
}

// ResolveTaskPath rejects absolute, traversal, alternate-separator, and
// symlink/reparse-like redirection outside the exact task worktree.
func ResolveTaskPath(
	binding storage.WorktreeBinding,
	relativePath string,
) (ResolvedTaskPath, error) {
	if binding.State != storage.WorktreeBindingActive {
		return ResolvedTaskPath{}, errors.New("task worktree is not active")
	}
	if relativePath == "" || relativePath != strings.TrimSpace(relativePath) ||
		strings.Contains(relativePath, "\\") ||
		path.IsAbs(relativePath) ||
		(len(relativePath) >= 2 && relativePath[1] == ':') {
		return ResolvedTaskPath{}, ErrUnsafeTaskPath
	}
	cleaned := path.Clean(relativePath)
	if cleaned != relativePath || cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") {
		return ResolvedTaskPath{}, ErrUnsafeTaskPath
	}
	root, err := filepath.Abs(binding.WorktreePath)
	if err != nil {
		return ResolvedTaskPath{}, fmt.Errorf("resolve task worktree: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil || !samePath(resolvedRoot, root) {
		return ResolvedTaskPath{}, ErrUnsafeTaskPath
	}
	absolute := filepath.Join(root, filepath.FromSlash(cleaned))
	if !pathWithin(root, absolute) {
		return ResolvedTaskPath{}, ErrUnsafeTaskPath
	}
	current := root
	parts := strings.Split(cleaned, "/")
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if index != len(parts)-1 {
				return ResolvedTaskPath{}, errors.New("task path parent does not exist")
			}
			break
		}
		if statErr != nil {
			return ResolvedTaskPath{}, fmt.Errorf("inspect task path: %w", statErr)
		}
		// ModeIrregular is included because Windows reports a directory
		// junction that way rather than as a symlink, and filepath.EvalSymlinks
		// returns a junction unchanged. Neither the symlink bit nor the
		// resolution check below sees one, so an unprivileged attacker (a
		// junction needs no privilege, unlike a symlink) could otherwise plant
		// a reparse point in the worktree that redirects outside it.
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return ResolvedTaskPath{}, ErrUnsafeTaskPath
		}
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr != nil || !samePath(resolved, current) ||
			!pathWithin(root, resolved) {
			return ResolvedTaskPath{}, ErrUnsafeTaskPath
		}
		if index != len(parts)-1 && !info.IsDir() {
			return ResolvedTaskPath{}, errors.New("task path parent is not a directory")
		}
	}
	return ResolvedTaskPath{Relative: cleaned, Absolute: absolute}, nil
}

// ReadFileAtRevision reads one bounded UTF-8 file snapshot.
func ReadFileAtRevision(
	_ context.Context,
	binding storage.WorktreeBinding,
	relativePath string,
) (FileSnapshot, error) {
	resolved, err := ResolveTaskPath(binding, relativePath)
	if err != nil {
		return FileSnapshot{}, err
	}
	info, err := os.Lstat(resolved.Absolute)
	if errors.Is(err, os.ErrNotExist) {
		return FileSnapshot{Path: resolved.Relative}, nil
	}
	if err != nil {
		return FileSnapshot{}, fmt.Errorf("inspect task file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maximumEditableFileBytes {
		return FileSnapshot{}, ErrUnsupportedBinary
	}
	content, err := os.ReadFile(resolved.Absolute)
	if err != nil {
		return FileSnapshot{}, fmt.Errorf("read task file: %w", err)
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return FileSnapshot{}, ErrUnsupportedBinary
	}
	digest := sha256.Sum256(content)
	return FileSnapshot{
		Path:         resolved.Relative,
		Exists:       true,
		Content:      content,
		SHA256:       hex.EncodeToString(digest[:]),
		Mode:         info.Mode().Perm(),
		NewlineStyle: newlineStyle(content),
	}, nil
}

// DetectConcurrentUserChanges compares exact existence and content identity.
func DetectConcurrentUserChanges(before, current FileSnapshot) bool {
	return before.Path != current.Path ||
		before.Exists != current.Exists ||
		before.SHA256 != current.SHA256
}

func newlineStyle(content []byte) string {
	if bytes.Contains(content, []byte("\r\n")) {
		return "crlf"
	}
	return "lf"
}
