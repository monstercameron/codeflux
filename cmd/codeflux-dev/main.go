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
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"codeflux.dev/codeflux/internal/buildinfo"
)

const (
	exitSuccess     = 0
	exitFailure     = 1
	exitUsage       = 2
	exitUnavailable = 3
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Stdout, os.Stderr, os.Args[1:]))
}

func run(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		_ = printRegistry(stderr, false)
		return exitUsage
	}

	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		jsonOutput := len(args) == 2 && args[1] == "--json"
		if len(args) > 1 && !jsonOutput {
			fmt.Fprintf(stderr, "codeflux-dev help: unknown arguments %q\n", args[1:])
			return exitUsage
		}
		if err := printRegistry(stdout, jsonOutput); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev help: %v\n", err)
			return exitFailure
		}
		return exitSuccess
	}
	spec, ok := findCommandSpec(args[0])
	if !ok {
		fmt.Fprintf(stderr, "codeflux-dev: unknown command %q\n", args[0])
		_ = printRegistry(stderr, false)
		return exitUsage
	}
	invocation, err := parseCommandInvocation(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev %s: %v\n", spec.Name, err)
		return exitUsage
	}
	if invocation.Help {
		if err := printCommandHelp(stdout, spec, invocation.JSON); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev %s help: %v\n", spec.Name, err)
			return exitFailure
		}
		return exitSuccess
	}
	if invocation.JSON && !spec.MachineReadable {
		fmt.Fprintf(stderr, "codeflux-dev %s: --json is not supported by this command\n", spec.Name)
		return exitUsage
	}
	if invocation.Once && spec.Name != "run" {
		fmt.Fprintf(stderr, "codeflux-dev %s: --once is only valid for run\n", spec.Name)
		return exitUsage
	}
	if spec.Name != "run-live" &&
		(invocation.Provider != "" || invocation.Model != "" ||
			invocation.ModelRevision != "" || invocation.CredentialRef != "" ||
			invocation.Database != "") {
		fmt.Fprintf(stderr, "codeflux-dev %s: live-provider options are only valid for run-live\n", spec.Name)
		return exitUsage
	}
	if len(invocation.Positional) > maximumPositionals(spec.Name) {
		fmt.Fprintf(stderr, "codeflux-dev %s: unexpected positional arguments %q\n", spec.Name, invocation.Positional)
		return exitUsage
	}

	switch spec.Name {
	case "artifact-check":
		return runArtifactCheck(ctx, stdout, stderr, invocation)
	case "benchmark":
		return runBenchmark(ctx, stdout, stderr, invocation)
	case "bootstrap":
		return runBootstrap(ctx, stdout, stderr, invocation)
	case "build":
		return runBuild(ctx, stdout, stderr, invocation)
	case "build-frontend":
		return runBuildFrontend(ctx, stdout, stderr, invocation)
	case "serve":
		return runServe(ctx, stdout, stderr, invocation)
	case "build-spike":
		return runBuildSpike(ctx, stdout, stderr, invocation)
	case "generate":
		return runGenerate(ctx, stdout, stderr, invocation)
	case "generate-check":
		return runGenerateCheck(ctx, stdout, stderr, invocation)
	case "ci-failure-artifact":
		return runCIFailureArtifact(ctx, stderr, invocation)
	case "test-fast":
		if code := validateCommandRoot(spec.Name, invocation, stderr); code != exitSuccess {
			return code
		}
		return runGo(ctx, stdout, stderr, "test", "./...")
	case "test-race":
		return runRace(ctx, stdout, stderr, invocation)
	case "test-coverage":
		return runCoverage(ctx, stdout, stderr, invocation)
	case "lint":
		return runLint(ctx, stdout, stderr, invocation)
	case "migration-check":
		return runMigrationCheck(ctx, stdout, stderr, invocation)
	case "run":
		return runDeterministicProfile(ctx, stdout, stderr, invocation)
	case "run-spike":
		return runSpike(ctx, stdout, stderr, invocation)
	case "run-live":
		return runLiveGate(ctx, stdout, stderr, invocation)
	case "test-all":
		return runAllTests(ctx, stdout, stderr, invocation)
	case "test-browser":
		return runBrowserTests(ctx, stdout, stderr, invocation)
	case "test-integration":
		return runIntegrationTests(ctx, stdout, stderr, invocation)
	case "test-security":
		return runSecurityTests(ctx, stdout, stderr, invocation)
	default:
		if code := validateCommandRoot(spec.Name, invocation, stderr); code != exitSuccess {
			return code
		}
		return runUnavailable(stderr, spec, invocation)
	}
}

func maximumPositionals(command string) int {
	switch command {
	case "benchmark", "inspect-db", "replay", "seed":
		return 1
	default:
		return 0
	}
}

func runGenerate(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev generate: %v\n", err)
		return exitFailure
	}
	outputRoot := root
	var bufArgs []string
	if invocation.Root != "" {
		outputRoot, err = resolveCommandRoot(root, "generate", invocation.Root)
		if err != nil {
			fmt.Fprintf(stderr, "codeflux-dev generate: root: %v\n", err)
			return exitUsage
		}
		bufArgs = []string{"--output", outputRoot}
	}
	bufArgs = append([]string{"generate"}, bufArgs...)
	if code := runPinnedBuf(
		ctx,
		root,
		stdout,
		stderr,
		bufArgs...,
	); code != exitSuccess {
		return code
	}
	if err := generateRepositorySource(root, outputRoot); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev generate: repository outputs: %v\n", err)
		return exitFailure
	}
	return exitSuccess
}

func runGenerateCheck(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev generate-check: %v\n", err)
		return exitFailure
	}
	var staging string
	cleanup := true
	if invocation.Root == "" {
		staging, err = createArtifactTemp(root, "generate-check-")
	} else {
		stagingRoot, rootErr := resolveCommandRoot(root, "tmp", invocation.Root)
		if rootErr != nil {
			fmt.Fprintf(stderr, "codeflux-dev generate-check: root: %v\n", rootErr)
			return exitUsage
		}
		if rootErr = os.MkdirAll(stagingRoot, 0o755); rootErr != nil {
			fmt.Fprintf(stderr, "codeflux-dev generate-check: create staging root: %v\n", rootErr)
			return exitFailure
		}
		staging, err = os.MkdirTemp(stagingRoot, "generate-check-")
		cleanup = isRepositoryArtifactChild(root, staging)
	}
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev generate-check: %v\n", err)
		return exitFailure
	}
	defer func() {
		if !cleanup {
			return
		}
		if removeErr := removeArtifactChild(root, staging); removeErr != nil {
			fmt.Fprintf(stderr, "codeflux-dev generate-check: cleanup: %v\n", removeErr)
		}
	}()

	if code := runPinnedBuf(
		ctx,
		root,
		stdout,
		stderr,
		"generate",
		"--output",
		staging,
	); code != exitSuccess {
		return code
	}
	if err := generateRepositorySource(root, staging); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev generate-check: repository outputs: %v\n", err)
		return exitFailure
	}
	for _, relative := range generatedRepositoryPaths {
		if err := compareGeneratedPath(root, staging, relative); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev generate-check: %v\n", err)
			return exitFailure
		}
	}
	return exitSuccess
}

func compareGeneratedPath(root, staging, relative string) error {
	expected := filepath.Join(root, filepath.FromSlash(relative))
	actual := filepath.Join(staging, filepath.FromSlash(relative))
	info, err := os.Stat(expected)
	if err != nil {
		return fmt.Errorf("inspect committed generated path %s: %w", relative, err)
	}
	if info.IsDir() {
		return compareDirectoryTrees(expected, actual)
	}
	expectedContent, err := os.ReadFile(expected)
	if err != nil {
		return err
	}
	actualContent, err := os.ReadFile(actual)
	if err != nil {
		return fmt.Errorf("read regenerated %s: %w", relative, err)
	}
	if !bytes.Equal(expectedContent, actualContent) {
		return fmt.Errorf("generated content differs for %s", relative)
	}
	return nil
}

func runPinnedBuf(
	ctx context.Context,
	root string,
	stdout io.Writer,
	stderr io.Writer,
	args ...string,
) int {
	commandArgs := []string{
		"run",
		"github.com/bufbuild/buf/cmd/buf@v1.72.0",
	}
	commandArgs = append(commandArgs, args...)
	return runCommandIn(ctx, root, stdout, stderr, "go", commandArgs...)
}

func runBuild(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build: %v\n", err)
		return exitFailure
	}

	binDir, err := resolveCommandRoot(root, "bin", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build: root: %v\n", err)
		return exitUsage
	}
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
		ldflags string
	}{
		{name: "codeflux" + suffix, pkgPath: "./cmd/codeflux"},
		{name: "codeflux-worker" + suffix, pkgPath: "./cmd/codeflux-worker"},
	}
	ldflags, err := buildLinkerFlags(ctx, root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build: version metadata: %v\n", err)
		return exitFailure
	}
	for _, target := range targets {
		outputPath := filepath.Join(binDir, target.name)
		target.ldflags = ldflags
		if code := runGoIn(
			ctx,
			root,
			stdout,
			stderr,
			"build",
			"-trimpath",
			"-buildvcs=false",
			"-ldflags",
			target.ldflags,
			"-o",
			outputPath,
			target.pkgPath,
		); code != exitSuccess {
			return code
		}
	}
	return exitSuccess
}

func buildLinkerFlags(ctx context.Context, root string) (string, error) {
	commit, err := gitOutput(ctx, root, "rev-parse", "--short=12", "HEAD")
	if err != nil {
		return "", err
	}
	buildDate, err := gitOutput(ctx, root, "show", "-s", "--format=%cI", "HEAD")
	if err != nil {
		return "", err
	}
	const prefix = "codeflux.dev/codeflux/internal/buildinfo."
	return strings.Join([]string{
		"-X", prefix + "version=0.0.0-dev",
		"-X", prefix + "commit=" + commit,
		"-X", prefix + "buildDate=" + buildDate,
		"-X", prefix + "frontendVersion=" + buildinfo.Current().FrontendVersion,
	}, " "), nil
}

func gitOutput(ctx context.Context, root string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", fmt.Errorf("git %s returned empty output", args[0])
	}
	return value, nil
}

func runRace(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	if code := validateCommandRoot("test-race", invocation, stderr); code != exitSuccess {
		return code
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
		fmt.Fprintln(stderr, "codeflux-dev test-race: unsupported on windows/arm64; run the declared Linux amd64 CI race job")
		return exitFailure
	}
	// The storage property suite deliberately explores thousands of concurrent
	// schedules. Race instrumentation can push that package beyond Go's default
	// ten-minute package timeout on shared CI runners.
	return runGo(ctx, stdout, stderr, "test", "-race", "-timeout=20m", "./...")
}

func runCoverage(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-coverage: %v\n", err)
		return exitFailure
	}
	coverageDir, err := resolveCommandRoot(root, "coverage", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev test-coverage: root: %v\n", err)
		return exitUsage
	}
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

func runLint(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev lint: %v\n", err)
		return exitFailure
	}
	if _, err := resolveCommandRoot(root, "lint", invocation.Root); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev lint: root: %v\n", err)
		return exitUsage
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
	if err := runRepositoryChecks(ctx, root); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev lint: repository check: %v\n", err)
		return exitFailure
	}
	if code := runPinnedBuf(ctx, root, stdout, stderr, "lint"); code != exitSuccess {
		fmt.Fprintln(stderr, "codeflux-dev lint: sub-step protobuf-lint failed")
		return code
	}
	if code := runPinnedBuf(
		ctx,
		root,
		stdout,
		stderr,
		"breaking",
		"--against",
		".git#ref=HEAD",
	); code != exitSuccess {
		fmt.Fprintln(stderr, "codeflux-dev lint: sub-step protobuf-compatibility failed")
		return code
	}
	if code := runGoIn(ctx, root, stdout, stderr, "vet", "./..."); code != exitSuccess {
		return code
	}
	staticcheck := staticcheckExecutable(root)
	if code := requireStaticcheckVersion(ctx, root, stderr, staticcheck); code != exitSuccess {
		return code
	}
	return runCommandIn(ctx, root, stdout, stderr, staticcheck, "./...")
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

func staticcheckExecutable(root string) string {
	executable := filepath.Join(root, ".artifacts", "tools", "bin", "staticcheck")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if info, err := os.Stat(executable); err == nil && !info.IsDir() {
		return executable
	}
	return "staticcheck"
}

func requireStaticcheckVersion(
	ctx context.Context,
	root string,
	stderr io.Writer,
	staticcheck string,
) int {
	command := exec.CommandContext(ctx, staticcheck, "-version")
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
