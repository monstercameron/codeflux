package gitwork

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const maximumGitOutputBytes = 1 << 20

// CommandResult is bounded process output from one fixed-argument Git command.
type CommandResult struct {
	Stdout []byte
	Stderr []byte
}

// CommandRunner executes argument arrays without a shell.
type CommandRunner interface {
	Run(context.Context, string, string, ...string) (CommandResult, error)
}

// ExecRunner executes bounded local commands without shell interpretation.
type ExecRunner struct{}

func (ExecRunner) Run(
	ctx context.Context,
	directory string,
	executable string,
	arguments ...string,
) (CommandResult, error) {
	return runCommand(ctx, directory, executable, nil, nil, arguments...)
}

// RunEnvironment executes with a bounded explicit environment overlay.
func (ExecRunner) RunEnvironment(
	ctx context.Context,
	directory string,
	executable string,
	environment map[string]string,
	arguments ...string,
) (CommandResult, error) {
	return runCommand(ctx, directory, executable, environment, nil, arguments...)
}

// RunInput executes with bounded standard input and output.
func (ExecRunner) RunInput(
	ctx context.Context,
	directory string,
	executable string,
	input []byte,
	arguments ...string,
) (CommandResult, error) {
	return runCommand(ctx, directory, executable, nil, input, arguments...)
}

func runCommand(
	ctx context.Context,
	directory string,
	executable string,
	environment map[string]string,
	input []byte,
	arguments ...string,
) (CommandResult, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = directory
	if len(environment) != 0 {
		command.Env = append([]string(nil), os.Environ()...)
		keys := make([]string, 0, len(environment))
		for key := range environment {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			command.Env = append(command.Env, key+"="+environment[key])
		}
	}
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr boundedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if stdout.overflow || stderr.overflow {
		return result, errors.New("command output exceeded the bounded capture limit")
	}
	if err != nil {
		// Git states why it refused, and that sentence is the whole diagnosis.
		// Reporting only the exit status turned every failure into "exit status
		// 128", which is the same message for a locked worktree, a branch that
		// already exists, and a path that cannot be written — and it left a run
		// stuck in starting with nothing anywhere to say what happened.
		if reason := strings.TrimSpace(string(result.Stderr)); reason != "" {
			return result, fmt.Errorf("%s failed: %w: %s", executable, err, reason)
		}
		return result, fmt.Errorf("%s failed: %w", executable, err)
	}
	return result, nil
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	original := len(content)
	remaining := maximumGitOutputBytes - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.overflow = true
		return original, nil
	}
	if len(content) > remaining {
		buffer.overflow = true
		content = content[:remaining]
	}
	_, _ = buffer.buffer.Write(content)
	return original, nil
}

func (buffer *boundedBuffer) Bytes() []byte {
	return append([]byte(nil), buffer.buffer.Bytes()...)
}
