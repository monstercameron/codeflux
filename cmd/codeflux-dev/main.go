// Command codeflux-dev provides the cross-platform repository development
// entry point. Repository-local disposable output is confined to .artifacts.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	case "test-race":
		return runRace(ctx, stdout, stderr)
	case "test-coverage":
		return runCoverage(ctx, stdout, stderr)
	case "lint":
		return runLint(ctx, stdout, stderr)
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
	fmt.Fprintln(output, "  test-race  Run Go race tests on a supported host")
	fmt.Fprintln(output, "  test-coverage  Write unit coverage to .artifacts/coverage")
	fmt.Fprintln(output, "  lint       Verify formatting, vet, and staticcheck 2026.1")
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

func runRace(ctx context.Context, stdout, stderr io.Writer) int {
	if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
		fmt.Fprintln(stderr, "codeflux-dev test-race: unsupported on windows/arm64; run the declared Linux amd64 CI race job")
		return exitFailure
	}
	return runGo(ctx, stdout, stderr, "test", "-race", "./...")
}

func runCoverage(ctx context.Context, stdout, stderr io.Writer) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-coverage: %v\n", err)
		return exitFailure
	}
	coverageDir := filepath.Join(root, ".artifacts", "coverage")
	if err := os.MkdirAll(coverageDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-coverage: create artifact directory: %v\n", err)
		return exitFailure
	}
	return runGoIn(
		ctx,
		root,
		stdout,
		stderr,
		"test",
		"-coverprofile="+filepath.Join(coverageDir, "unit.out"),
		"./...",
	)
}

func runLint(ctx context.Context, stdout, stderr io.Writer) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev lint: %v\n", err)
		return exitFailure
	}
	unformatted, err := unformattedGoFiles(root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev lint: formatting check: %v\n", err)
		return exitFailure
	}
	if len(unformatted) != 0 {
		fmt.Fprintln(stderr, "codeflux-dev lint: gofmt required:")
		for _, path := range unformatted {
			fmt.Fprintf(stderr, "  %s\n", path)
		}
		return exitFailure
	}
	if code := runGoIn(ctx, root, stdout, stderr, "vet", "./..."); code != exitSuccess {
		return code
	}
	if code := requireStaticcheckVersion(ctx, root, stderr); code != exitSuccess {
		return code
	}
	return runCommandIn(ctx, root, stdout, stderr, "staticcheck", "./...")
}

func unformattedGoFiles(root string) ([]string, error) {
	var unformatted []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".artifacts":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if filepath.Ext(entry.Name()) != ".go" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(source)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !bytes.Equal(source, formatted) {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			unformatted = append(unformatted, filepath.ToSlash(relative))
		}
		return nil
	})
	return unformatted, err
}

func requireStaticcheckVersion(ctx context.Context, root string, stderr io.Writer) int {
	command := exec.CommandContext(ctx, "staticcheck", "-version")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev lint: staticcheck 2026.1 is required: %v\n", err)
		return exitFailure
	}
	if !strings.Contains(string(output), "2026.1") {
		fmt.Fprintf(stderr, "codeflux-dev lint: staticcheck version mismatch: got %q, want 2026.1\n", strings.TrimSpace(string(output)))
		return exitFailure
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
	return runCommandIn(ctx, root, stdout, stderr, "go", args...)
}

func runCommandIn(
	ctx context.Context,
	root string,
	stdout io.Writer,
	stderr io.Writer,
	name string,
	args ...string,
) int {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = root
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			fmt.Fprintf(stderr, "codeflux-dev: %s %s failed with exit code %d\n", name, args[0], exitError.ExitCode())
		} else {
			fmt.Fprintf(stderr, "codeflux-dev: run %s %s: %v\n", name, args[0], err)
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
