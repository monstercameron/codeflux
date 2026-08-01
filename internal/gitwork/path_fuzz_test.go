package gitwork

import (
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/storage"
)

// FuzzResolveTaskPathNeverEscapesTheWorktree is M22-026's safe-path fuzz.
//
// Path resolution is a security boundary: AGENTS.md requires filesystem
// targets be resolved against the task worktree server-side, with traversal
// and symlink escape prevented. A single accepted escape is a full read/write
// primitive outside the task's isolation, so this asserts the property
// directly rather than enumerating known-bad shapes.
func FuzzResolveTaskPathNeverEscapesTheWorktree(f *testing.F) {
	seeds := []string{
		"", ".", "..", "/", "/etc/passwd",
		"internal/server/server.go",
		"../outside.txt", "../../../../etc/shadow",
		"a/../../b", "a/./b", "a//b",
		`internal\server\server.go`,
		"C:/Windows/System32/config/SAM",
		`C:\Windows\System32`,
		`\\server\share\file`,
		"nul", "con", "aux",
		" leading-space", "trailing-space ",
		"file\x00.go", "\u202eexe.txt",
		strings.Repeat("a/", 512) + "deep.go",
		"./internal/../../escape",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	root := filepath.Join(f.TempDir(), "worktree")
	binding := storage.WorktreeBinding{
		State:        storage.WorktreeBindingActive,
		WorktreePath: root,
	}

	f.Fuzz(func(t *testing.T, relative string) {
		resolved, err := ResolveTaskPath(binding, relative)
		if err != nil {
			return // Refusing is always acceptable.
		}
		// An accepted path must resolve strictly inside the worktree.
		absolute := resolved.Absolute
		if absolute == "" {
			t.Fatalf("accepted %q but produced no absolute path", relative)
		}
		cleanRoot := filepath.Clean(root)
		cleanTarget := filepath.Clean(absolute)
		if cleanTarget == cleanRoot {
			t.Fatalf("accepted %q resolving to the worktree root itself", relative)
		}
		if !strings.HasPrefix(cleanTarget, cleanRoot+string(filepath.Separator)) {
			t.Fatalf("accepted %q escaped the worktree: %q is outside %q", relative, cleanTarget, cleanRoot)
		}
		if strings.Contains(resolved.Relative, "..") {
			t.Fatalf("accepted %q retained a traversal segment: %q", relative, resolved.Relative)
		}
		if filepath.IsAbs(resolved.Relative) {
			t.Fatalf("accepted %q produced an absolute relative path: %q", relative, resolved.Relative)
		}
	})
}
