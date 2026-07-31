package review

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// EditorCommandRunner starts one editor CLI invocation without a shell.
type EditorCommandRunner interface {
	RunEditorCommand(context.Context, string, ...string) error
}

type processEditorCommandRunner struct{}

func (processEditorCommandRunner) RunEditorCommand(
	ctx context.Context,
	executable string,
	arguments ...string,
) error {
	return exec.CommandContext(ctx, executable, arguments...).Run()
}

// CLIEditorLauncher opens validated locations through an editor implementing
// the widely supported `--goto path:line:column` command contract. The path and
// coordinates remain one argument and never pass through a command shell.
type CLIEditorLauncher struct {
	executable string
	runner     EditorCommandRunner
}

// NewCLIEditorLauncher creates a production external-editor adapter. Executable
// is one program path or lookup name; embedded arguments are deliberately not
// parsed or supported.
func NewCLIEditorLauncher(executable string) (*CLIEditorLauncher, error) {
	return newCLIEditorLauncher(executable, processEditorCommandRunner{})
}

func newCLIEditorLauncher(
	executable string,
	runner EditorCommandRunner,
) (*CLIEditorLauncher, error) {
	if executable == "" || executable != strings.TrimSpace(executable) {
		return nil, errors.New("editor executable is required")
	}
	for _, character := range executable {
		if character < ' ' || character == 0x7f {
			return nil, errors.New("editor executable contains a control character")
		}
	}
	if runner == nil {
		return nil, errors.New("editor command runner is required")
	}
	return &CLIEditorLauncher{executable: executable, runner: runner}, nil
}

// OpenEditor invokes the configured editor with an injection-safe argument
// vector. A Unix path containing a colon is rejected because the editor's
// location grammar uses colons as coordinate delimiters.
func (launcher *CLIEditorLauncher) OpenEditor(
	ctx context.Context,
	target EditorTarget,
) error {
	if launcher == nil || launcher.runner == nil || launcher.executable == "" {
		return errors.New("editor launcher is not configured")
	}
	if target.absolutePath == "" || target.line == 0 || target.column == 0 {
		return ErrInvalidEditorSource
	}
	if runtime.GOOS != "windows" && strings.Contains(target.absolutePath, ":") {
		return fmt.Errorf("%w: source path conflicts with editor coordinate grammar", ErrInvalidEditorSource)
	}
	location := fmt.Sprintf("%s:%d:%d", target.absolutePath, target.line, target.column)
	if err := launcher.runner.RunEditorCommand(ctx, launcher.executable, "--goto", location); err != nil {
		return fmt.Errorf("run editor command: %w", err)
	}
	return nil
}
