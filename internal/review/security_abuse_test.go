package review

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// recordingLauncher fails the test if it is ever asked to launch. An editor
// launch is an external effect on the user's machine, so the property under
// test is not "the call returned an error" but "no process was ever started".
type recordingLauncher struct {
	launched []EditorTarget
}

func (launcher *recordingLauncher) OpenEditor(_ context.Context, target EditorTarget) error {
	launcher.launched = append(launcher.launched, target)
	return nil
}

func newEditorRepository(t *testing.T) (root string, outside string) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp parent: %v", err)
	}
	root = filepath.Join(parent, "repository")
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	source := "package internal\n\nfunc Example() {}\n"
	if err := os.WriteFile(filepath.Join(root, "internal", "example.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	outside = filepath.Join(parent, "private.txt")
	if err := os.WriteFile(outside, []byte("private-outside-the-repository\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	return root, outside
}

// TestM22_053_UnsafeEditorTargetsAreRefused is M22-053.
func TestM22_053_UnsafeEditorTargetsAreRefused(t *testing.T) {
	root, _ := newEditorRepository(t)

	unsafe := []struct {
		name         string
		relativePath string
		line, column uint32
	}{
		{"parent traversal", "../private.txt", 1, 1},
		{"deep traversal", "../../private.txt", 1, 1},
		{"embedded traversal", "internal/../../private.txt", 1, 1},
		{"posix absolute", "/etc/passwd", 1, 1},
		{"windows absolute", `C:\Windows\win.ini`, 1, 1},
		{"unc path", `\\server\share\file.go`, 1, 1},
		{"drive relative", "C:example.go", 1, 1},
		{"backslash separator", `internal\example.go`, 1, 1},
		{"dot", ".", 1, 1},
		{"dotdot", "..", 1, 1},
		{"empty", "", 1, 1},
		{"leading space", " internal/example.go", 1, 1},
		{"trailing space", "internal/example.go ", 1, 1},
		{"control character", "internal/exa\x00mple.go", 1, 1},
		{"newline injection", "internal/example.go\n--wait", 1, 1},
		{"unclean double slash", "internal//example.go", 1, 1},
		{"directory not file", "internal", 1, 1},
		{"missing file", "internal/absent.go", 1, 1},
		{"zero line", "internal/example.go", 0, 1},
		{"zero column", "internal/example.go", 1, 0},
		{"line past end", "internal/example.go", 99, 1},
		{"column past end", "internal/example.go", 1, 99},
	}

	for _, testCase := range unsafe {
		t.Run(testCase.name, func(t *testing.T) {
			target, err := ResolveEditorTarget(root, testCase.relativePath, testCase.line, testCase.column)
			if err == nil {
				t.Fatalf("target %q resolved to %q instead of being refused",
					testCase.relativePath, target.AbsolutePath())
			}
		})
	}

	// Not vacuous: a legitimate coordinate inside the repository resolves.
	target, err := ResolveEditorTarget(root, "internal/example.go", 3, 1)
	if err != nil {
		t.Fatalf("legitimate editor target was refused: %v", err)
	}
	if !strings.HasPrefix(target.AbsolutePath(), root) {
		t.Fatalf("resolved target %q escaped root %q", target.AbsolutePath(), root)
	}
	if target.RelativePath() != "internal/example.go" {
		t.Fatalf("resolved relative path = %q", target.RelativePath())
	}
}

// TestM22_053_UnsafeEditorTargetNeverLaunchesAProcess proves the refusal
// happens before the external effect, not after it.
func TestM22_053_UnsafeEditorTargetNeverLaunchesAProcess(t *testing.T) {
	root, _ := newEditorRepository(t)
	workspaceID, err := domain.NewWorkspaceID()
	if err != nil {
		t.Fatalf("new workspace ID: %v", err)
	}
	launcher := &recordingLauncher{}
	service, err := NewEditorOpenService(editorWorkspaceResolverStub{
		workspace: EditorWorkspace{WorkspaceID: workspaceID, RepositoryRoot: root},
	}, launcher)
	if err != nil {
		t.Fatalf("construct editor service: %v", err)
	}

	for _, relativePath := range []string{
		"../private.txt", "/etc/passwd", `C:\Windows\win.ini`, "internal/absent.go", "",
	} {
		_, err := service.OpenInEditor(context.Background(), OpenEditorCommand{
			WorkspaceID:    workspaceID,
			RelativePath:   relativePath,
			Line:           1,
			Column:         1,
			IdempotencyKey: "abuse-" + relativePath,
		})
		if err == nil {
			t.Fatalf("OpenInEditor(%q) succeeded", relativePath)
		}
	}
	if len(launcher.launched) != 0 {
		t.Fatalf("an unsafe target reached the launcher: %+v", launcher.launched)
	}
}

// TestM22_053_EditorJunctionTargetIsRefused covers the unprivileged Windows
// reparse point, matching the worktree file API.
func TestM22_053_EditorJunctionTargetIsRefused(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are a Windows reparse point")
	}
	root, outside := newEditorRepository(t)
	junction := filepath.Join(root, "escape")
	output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, filepath.Dir(outside)).CombinedOutput()
	if err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}

	for _, relativePath := range []string{"escape/private.txt", "escape"} {
		if _, err := ResolveEditorTarget(root, relativePath, 1, 1); err == nil {
			t.Fatalf("editor target %q resolved through a junction", relativePath)
		} else if !errors.Is(err, ErrEditorSourceOutsideRepository) &&
			!errors.Is(err, ErrEditorSourceUnavailable) {
			t.Fatalf("editor target %q refused with unexpected error: %v", relativePath, err)
		}
	}
}
