package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

type editorWorkspaceResolverStub struct {
	workspace EditorWorkspace
	err       error
}

func (stub editorWorkspaceResolverStub) ResolveEditorWorkspace(
	context.Context,
	domain.WorkspaceID,
) (EditorWorkspace, error) {
	return stub.workspace, stub.err
}

type editorLauncherStub struct {
	targets []EditorTarget
	err     error
}

type editorCommandRunnerStub struct {
	executable string
	arguments  []string
	err        error
}

func (stub *editorCommandRunnerStub) RunEditorCommand(
	_ context.Context,
	executable string,
	arguments ...string,
) error {
	stub.executable = executable
	stub.arguments = append([]string(nil), arguments...)
	return stub.err
}

func (stub *editorLauncherStub) OpenEditor(_ context.Context, target EditorTarget) error {
	stub.targets = append(stub.targets, target)
	return stub.err
}

func TestEditorOpenServiceLaunchesOnlyResolvedSourceLocation(t *testing.T) {
	root := t.TempDir()
	writeEditorFixture(t, root, "source folder/main;safe.go", "package main\nfunc main() {}\n")
	workspaceID, err := domain.NewWorkspaceID()
	if err != nil {
		t.Fatal(err)
	}
	launcher := &editorLauncherStub{}
	service, err := NewEditorOpenService(editorWorkspaceResolverStub{workspace: EditorWorkspace{
		WorkspaceID: workspaceID, RepositoryRoot: root,
	}}, launcher)
	if err != nil {
		t.Fatal(err)
	}
	target, err := service.OpenInEditor(context.Background(), OpenEditorCommand{
		WorkspaceID: workspaceID, RelativePath: "source folder/main;safe.go", Line: 2, Column: 6,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.targets) != 1 || launcher.targets[0] != target {
		t.Fatalf("launch targets = %+v, result = %+v", launcher.targets, target)
	}
	if target.RelativePath() != "source folder/main;safe.go" || target.Line() != 2 || target.Column() != 6 {
		t.Fatalf("validated target = %q:%d:%d", target.RelativePath(), target.Line(), target.Column())
	}
	if target.AbsolutePath() != filepath.Join(root, "source folder", "main;safe.go") {
		t.Fatalf("absolute target = %q", target.AbsolutePath())
	}
}

func TestEditorOpenServiceRejectsUnboundAndEscapingLocationsWithoutLaunch(t *testing.T) {
	root := t.TempDir()
	writeEditorFixture(t, root, "inside.go", "package inside\n")
	workspaceID, _ := domain.NewWorkspaceID()
	otherWorkspaceID, _ := domain.NewWorkspaceID()

	for _, test := range []struct {
		name      string
		workspace EditorWorkspace
		path      string
		line      uint32
		column    uint32
		want      error
	}{
		{name: "workspace mismatch", workspace: EditorWorkspace{WorkspaceID: otherWorkspaceID, RepositoryRoot: root}, path: "inside.go", line: 1, column: 1, want: ErrEditorSourceOutsideRepository},
		{name: "parent traversal", workspace: EditorWorkspace{WorkspaceID: workspaceID, RepositoryRoot: root}, path: "../outside.go", line: 1, column: 1, want: ErrEditorSourceOutsideRepository},
		{name: "absolute", workspace: EditorWorkspace{WorkspaceID: workspaceID, RepositoryRoot: root}, path: filepath.Join(root, "inside.go"), line: 1, column: 1, want: ErrEditorSourceOutsideRepository},
		{name: "windows drive", workspace: EditorWorkspace{WorkspaceID: workspaceID, RepositoryRoot: root}, path: `C:\outside.go`, line: 1, column: 1, want: ErrEditorSourceOutsideRepository},
		{name: "newline", workspace: EditorWorkspace{WorkspaceID: workspaceID, RepositoryRoot: root}, path: "inside.go\n--wait", line: 1, column: 1, want: ErrInvalidEditorSource},
		{name: "zero line", workspace: EditorWorkspace{WorkspaceID: workspaceID, RepositoryRoot: root}, path: "inside.go", line: 0, column: 1, want: ErrInvalidEditorSource},
		{name: "line outside file", workspace: EditorWorkspace{WorkspaceID: workspaceID, RepositoryRoot: root}, path: "inside.go", line: 3, column: 1, want: ErrInvalidEditorSource},
		{name: "column outside line", workspace: EditorWorkspace{WorkspaceID: workspaceID, RepositoryRoot: root}, path: "inside.go", line: 1, column: 16, want: ErrInvalidEditorSource},
		{name: "missing", workspace: EditorWorkspace{WorkspaceID: workspaceID, RepositoryRoot: root}, path: "missing.go", line: 1, column: 1, want: ErrEditorSourceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			launcher := &editorLauncherStub{}
			service, err := NewEditorOpenService(editorWorkspaceResolverStub{workspace: test.workspace}, launcher)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.OpenInEditor(context.Background(), OpenEditorCommand{
				WorkspaceID: workspaceID, RelativePath: test.path, Line: test.line, Column: test.column,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if len(launcher.targets) != 0 {
				t.Fatalf("invalid request launched %+v", launcher.targets)
			}
		})
	}
}

func TestResolveEditorTargetRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ordinary Windows developer accounts may not create symlinks")
	}
	root := t.TempDir()
	outside := t.TempDir()
	writeEditorFixture(t, outside, "outside.go", "package outside\n")
	if err := os.Symlink(filepath.Join(outside, "outside.go"), filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveEditorTarget(root, "linked.go", 1, 1)
	if !errors.Is(err, ErrEditorSourceOutsideRepository) {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestEditorOpenServicePropagatesLauncherFailure(t *testing.T) {
	root := t.TempDir()
	writeEditorFixture(t, root, "main.go", "package main\n")
	workspaceID, _ := domain.NewWorkspaceID()
	launchErr := errors.New("editor unavailable")
	launcher := &editorLauncherStub{err: launchErr}
	service, err := NewEditorOpenService(editorWorkspaceResolverStub{workspace: EditorWorkspace{
		WorkspaceID: workspaceID, RepositoryRoot: root,
	}}, launcher)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.OpenInEditor(context.Background(), OpenEditorCommand{
		WorkspaceID: workspaceID, RelativePath: "main.go", Line: 1, Column: 1,
	})
	if !errors.Is(err, launchErr) || len(launcher.targets) != 1 {
		t.Fatalf("launch error = %v, targets = %+v", err, launcher.targets)
	}
}

func TestCLIEditorLauncherUsesInjectionSafeArgumentVector(t *testing.T) {
	root := t.TempDir()
	writeEditorFixture(t, root, "source folder/main;safe.go", "package main\n")
	target, err := ResolveEditorTarget(root, "source folder/main;safe.go", 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	runner := &editorCommandRunnerStub{}
	launcher, err := newCLIEditorLauncher("code", runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.OpenEditor(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	wantLocation := target.AbsolutePath() + ":1:8"
	if runner.executable != "code" || len(runner.arguments) != 2 ||
		runner.arguments[0] != "--goto" || runner.arguments[1] != wantLocation {
		t.Fatalf("editor invocation = %q %#v", runner.executable, runner.arguments)
	}
}

func TestCLIEditorLauncherRejectsExecutableControlCharacters(t *testing.T) {
	if _, err := newCLIEditorLauncher("code\n--wait", &editorCommandRunnerStub{}); err == nil {
		t.Fatal("control-bearing editor executable was accepted")
	}
}

func writeEditorFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
