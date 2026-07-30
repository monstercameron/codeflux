package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

const goWebComponentsModule = "github.com/monstercameron/GoWebComponents/v5"

func runBuildSpike(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: %v\n", err)
		return exitFailure
	}
	assets, err := resolveCommandRoot(root, "m06-gwc-shell", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: root: %v\n", err)
		return exitUsage
	}
	if !isRepositoryArtifactChild(root, assets) {
		fmt.Fprintln(stderr, "codeflux-dev build-spike: generated shell must remain beneath .artifacts")
		return exitUsage
	}
	if err := removeArtifactChild(root, assets); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: clear generated shell: %v\n", err)
		return exitFailure
	}
	parent := filepath.Dir(assets)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: create artifact parent: %v\n", err)
		return exitFailure
	}

	gwcDirectory, err := resolveModuleDirectory(ctx, root, goWebComponentsModule)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: resolve GoWebComponents: %v\n", err)
		return exitFailure
	}
	if code := runCommandIn(
		ctx,
		gwcDirectory,
		stdout,
		stderr,
		"go",
		"run",
		"./tools/gwc",
		"scaffold",
		"-kind",
		"app",
		"-root",
		parent,
		"-dir",
		filepath.Base(assets),
		"-name",
		"CodefluxSpike",
		"-module",
		"codeflux.dev/codeflux/spike-shell",
		"-no-input",
		"-json",
	); code != exitSuccess {
		return code
	}

	goRootOutput, err := commandOutput(ctx, root, safeToolEnvironment(), "go", "env", "GOROOT")
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: resolve GOROOT: %v\n", err)
		return exitFailure
	}
	shim := filepath.Join(goRootOutput, "lib", "wasm", "wasm_exec.js")
	if _, err := os.Stat(shim); err != nil {
		shim = filepath.Join(goRootOutput, "misc", "wasm", "wasm_exec.js")
	}
	shimBytes, err := os.ReadFile(shim)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: read wasm_exec.js: %v\n", err)
		return exitFailure
	}
	if err := os.WriteFile(filepath.Join(assets, "wasm_exec.js"), shimBytes, 0o644); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: write wasm_exec.js: %v\n", err)
		return exitFailure
	}
	binaryDirectory := filepath.Join(assets, "bin")
	if err := os.MkdirAll(binaryDirectory, 0o755); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: create WASM output directory: %v\n", err)
		return exitFailure
	}
	command := exec.CommandContext(
		ctx,
		"go",
		"build",
		"-trimpath",
		"-o",
		filepath.Join(binaryDirectory, "main.wasm"),
		"./web/client",
	)
	command.Dir = root
	command.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev build-spike: build WASM: %v\n", err)
		return exitFailure
	}
	fmt.Fprintf(stdout, "codeflux-dev build-spike: generated GWC assets at %s\n", assets)
	return exitSuccess
}

func runSpike(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	if code := runBuildSpike(ctx, stdout, stderr, invocation); code != exitSuccess {
		return code
	}
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run-spike: %v\n", err)
		return exitFailure
	}
	assets, err := resolveCommandRoot(root, "m06-gwc-shell", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run-spike: root: %v\n", err)
		return exitUsage
	}
	return runCommandIn(
		ctx,
		root,
		stdout,
		stderr,
		"go",
		"run",
		"./cmd/codeflux-spike",
		"-assets",
		assets,
	)
}

func resolveModuleDirectory(ctx context.Context, root string, module string) (string, error) {
	return commandOutput(
		ctx,
		root,
		safeToolEnvironment(),
		"go",
		"list",
		"-m",
		"-f={{.Dir}}",
		module,
	)
}
