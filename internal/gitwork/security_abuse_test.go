package gitwork

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/storage"
)

// traversalPayloads are the shapes an attacker actually sends when trying to
// leave a confined root. They are asserted against EVERY path-taking file API
// rather than against the resolver alone, because a resolver that is correct
// but bypassed protects nothing.
func traversalPayloads() []string {
	return []string{
		"../outside.txt",
		"../../outside.txt",
		"a/../../outside.txt",
		"./../outside.txt",
		"..",
		".",
		"/etc/passwd",
		"/absolute.txt",
		`\\server\share\file.txt`,
		`..\outside.txt`,
		`C:\Windows\System32\drivers\etc\hosts`,
		"C:/Windows/System32/config/SAM",
		"a//b",
		"a/./b",
		"a/",
		" leading-space.txt",
		"trailing-space.txt ",
		"",
	}
}

func newAbuseWorktree(t *testing.T) (storage.WorktreeBinding, string) {
	t.Helper()
	parent := t.TempDir()
	// EvalSymlinks matters here: on macOS and on some Windows setups TempDir
	// hands back a path containing a link, and the resolver correctly refuses
	// an unresolved root. Resolving up front keeps the test about traversal.
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatalf("resolve temp parent: %v", err)
	}
	root := filepath.Join(resolvedParent, "worktree")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatalf("create worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "inside.txt"), []byte("inside\n"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}
	secret := filepath.Join(resolvedParent, "outside.txt")
	if err := os.WriteFile(secret, []byte("secret-outside-the-worktree\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	return storage.WorktreeBinding{
		State:        storage.WorktreeBindingActive,
		WorktreePath: root,
	}, resolvedParent
}

// TestM22_051_PathTraversalIsRefusedByEveryFileAPI is M22-051.
func TestM22_051_PathTraversalIsRefusedByEveryFileAPI(t *testing.T) {
	binding, parent := newAbuseWorktree(t)
	ctx := context.Background()

	for _, payload := range traversalPayloads() {
		t.Run("resolve/"+strings.ReplaceAll(payload, "/", "_"), func(t *testing.T) {
			resolved, err := ResolveTaskPath(binding, payload)
			if err == nil {
				t.Fatalf("payload %q resolved to %q instead of being refused",
					payload, resolved.Absolute)
			}
		})
		t.Run("read/"+strings.ReplaceAll(payload, "/", "_"), func(t *testing.T) {
			snapshot, err := ReadFileAtRevision(ctx, binding, payload)
			if err == nil {
				t.Fatalf("payload %q was read (exists=%v, %d bytes) instead of being refused",
					payload, snapshot.Exists, len(snapshot.Content))
			}
			if strings.Contains(string(snapshot.Content), "secret-outside-the-worktree") {
				t.Fatalf("payload %q leaked content from outside the worktree", payload)
			}
		})
	}

	// The refusals must not be vacuous: the same API on a legitimate path
	// inside the worktree has to succeed, or this test would pass against a
	// function that rejects everything.
	snapshot, err := ReadFileAtRevision(ctx, binding, "pkg/inside.txt")
	if err != nil {
		t.Fatalf("legitimate in-worktree read failed: %v", err)
	}
	if string(snapshot.Content) != "inside\n" {
		t.Fatalf("in-worktree read returned %q", snapshot.Content)
	}
	// And nothing above may have created a file outside the worktree.
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "worktree" && entry.Name() != "outside.txt" {
			t.Fatalf("a refused payload created %q outside the worktree", entry.Name())
		}
	}
}

// TestM22_052_SymlinkEscapeIsRefused is M22-052.
//
// A path can be free of ".." and still leave the root, if a component of it is
// a link. This is the escape a purely lexical check misses, so it is proven
// against real links on disk rather than against string inputs.
func TestM22_052_SymlinkEscapeIsRefused(t *testing.T) {
	binding, parent := newAbuseWorktree(t)
	root := binding.WorktreePath
	ctx := context.Background()

	type link struct {
		name   string
		target string
	}
	links := []link{
		{"escape-file.txt", filepath.Join(parent, "outside.txt")},
		{"escape-dir", parent},
	}
	created := map[string]bool{}
	for _, candidate := range links {
		if err := os.Symlink(candidate.target, filepath.Join(root, candidate.name)); err != nil {
			// Unprivileged Windows accounts cannot create links. Skipping is
			// honest; silently passing would not be.
			if runtime.GOOS == "windows" {
				t.Skipf("symlink creation unavailable on this account: %v", err)
			}
			t.Fatalf("create symlink %q: %v", candidate.name, err)
		}
		created[candidate.name] = true
	}
	if len(created) != len(links) {
		t.Fatal("symlink fixture incomplete")
	}

	escapes := []string{
		"escape-file.txt",
		"escape-dir/outside.txt",
		"escape-dir/worktree/pkg/inside.txt",
	}
	for _, payload := range escapes {
		if _, err := ResolveTaskPath(binding, payload); !errors.Is(err, ErrUnsafeTaskPath) {
			t.Fatalf("ResolveTaskPath(%q) = %v, want ErrUnsafeTaskPath", payload, err)
		}
		snapshot, err := ReadFileAtRevision(ctx, binding, payload)
		if err == nil {
			t.Fatalf("ReadFileAtRevision(%q) succeeded through a symlink", payload)
		}
		if strings.Contains(string(snapshot.Content), "secret-outside-the-worktree") {
			t.Fatalf("payload %q leaked outside content through a symlink", payload)
		}
	}
}

// TestM22_052_DirectoryJunctionEscapeIsRefused is M22-052 on Windows.
//
// Creating a symlink on Windows needs a privilege an ordinary account does not
// hold, but creating a DIRECTORY JUNCTION does not. A junction is therefore the
// reparse point an unprivileged attacker can actually plant inside a worktree,
// and it is the one this platform must be proven against.
func TestM22_052_DirectoryJunctionEscapeIsRefused(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows reparse point")
	}
	binding, parent := newAbuseWorktree(t)
	junction := filepath.Join(binding.WorktreePath, "escape-junction")
	output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, parent).CombinedOutput()
	if err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	if _, statErr := os.Lstat(junction); statErr != nil {
		t.Fatalf("junction fixture not created: %v", statErr)
	}

	ctx := context.Background()
	for _, payload := range []string{
		"escape-junction/outside.txt",
		"escape-junction/worktree/pkg/inside.txt",
		// The junction as the FINAL component. Refusing the two paths above
		// only requires noticing the component is not a directory; refusing
		// this one requires actually recognising the reparse point, which is
		// the property being claimed.
		"escape-junction",
	} {
		if _, err := ResolveTaskPath(binding, payload); !errors.Is(err, ErrUnsafeTaskPath) {
			t.Fatalf("ResolveTaskPath(%q) = %v, want ErrUnsafeTaskPath", payload, err)
		}
		snapshot, err := ReadFileAtRevision(ctx, binding, payload)
		if err == nil {
			t.Fatalf("ReadFileAtRevision(%q) succeeded through a junction", payload)
		}
		if strings.Contains(string(snapshot.Content), "secret-outside-the-worktree") {
			t.Fatalf("payload %q leaked outside content through a junction", payload)
		}
	}
}

// TestM22_052_InactiveBindingRefusesEveryPath proves confinement does not
// depend on the path alone: a worktree that is not active grants no reads at
// all, including of paths that would otherwise be legitimate.
func TestM22_052_InactiveBindingRefusesEveryPath(t *testing.T) {
	binding, _ := newAbuseWorktree(t)
	binding.State = storage.WorktreeBindingReleased

	if _, err := ResolveTaskPath(binding, "pkg/inside.txt"); err == nil {
		t.Fatal("a released worktree binding still resolved a path")
	}
	if _, err := ReadFileAtRevision(context.Background(), binding, "pkg/inside.txt"); err == nil {
		t.Fatal("a released worktree binding still served a read")
	}
}
