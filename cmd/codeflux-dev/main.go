// Command codeflux-dev provides the cross-platform repository development
// entry point. Repository-local disposable output is confined to .artifacts.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(context.Background(), os.Stdout, os.Stderr, os.Args[1:]))
}

func run(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		printHelp(stderr)
		return exitUsage
	}

	switch args[0] {
	case "help", "--help", "-h":
		printHelp(stdout)
		return exitSuccess
	case "build":
		return runBuild(ctx, stdout, stderr)
	case "test-fast":
		return runGo(ctx, stdout, stderr, "test", "./...")
	default:
		fmt.Fprintf(stderr, "codeflux-dev: unknown command %q\n", args[0])
		printHelp(stderr)
		return exitUsage
	}
}

func printHelp(output io.Writer) {
	fmt.Fprintln(output, "Usage: go run ./cmd/codeflux-dev <command>")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  build      Compile all packages and write command binaries to .artifacts/bin")
	fmt.Fprintln(output, "  test-fast  Run the complete fast Go test suite")
}

func runBuild(ctx context.Context, stdout, stderr io.Writer) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build: %v\n", err)
		return exitFailure
	}

	binDir := filepath.Join(root, ".artifacts", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build: create artifact directory: %v\n", err)
		return exitFailure
	}

	if code := runGoIn(ctx, root, stdout, stderr, "build", "./..."); code != exitSuccess {
		return code
	}

	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	targets := []struct {
		name    string
		pkgPath string
	}{
		{name: "codeflux" + suffix, pkgPath: "./cmd/codeflux"},
		{name: "codeflux-worker" + suffix, pkgPath: "./cmd/codeflux-worker"},
	}
	for _, target := range targets {
		outputPath := filepath.Join(binDir, target.name)
		if code := runGoIn(
			ctx,
			root,
			stdout,
			stderr,
			"build",
			"-trimpath",
			"-o",
			outputPath,
			target.pkgPath,
		); code != exitSuccess {
			return code
		}
	}
	return exitSuccess
}

func runGo(ctx context.Context, stdout, stderr io.Writer, args ...string) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev: %v\n", err)
		return exitFailure
	}
	return runGoIn(ctx, root, stdout, stderr, args...)
}

func runGoIn(
	ctx context.Context,
	root string,
	stdout io.Writer,
	stderr io.Writer,
	args ...string,
) int {
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir = root
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			fmt.Fprintf(stderr, "codeflux-dev: go %s failed with exit code %d\n", args[0], exitError.ExitCode())
		} else {
			fmt.Fprintf(stderr, "codeflux-dev: run go %s: %v\n", args[0], err)
		}
		return exitFailure
	}
	return exitSuccess
}

func repositoryRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current directory: %w", err)
	}

	for {
		if info, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil && !info.IsDir() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("go.mod not found in current directory or any parent")
		}
		current = parent
	}
}
