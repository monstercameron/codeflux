package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// frontendAssetDirectory is where build-frontend writes, relative to the
// repository root. `codeflux start` looks here when nothing is embedded, so
// the two must agree; cmd/codeflux names the same path in its own constant and
// a test binds them together.
const frontendAssetDirectory = "frontend"

// frontendAssetBuild describes one browser asset set to generate.
type frontendAssetBuild struct {
	// Directory is the absolute output directory, which must live beneath
	// .artifacts.
	Directory string
	// ApplicationName and ModulePath are what the GWC scaffold is told to
	// call the shell it generates.
	ApplicationName string
	ModulePath      string
	// ClientPackage is the Go package built to WebAssembly.
	ClientPackage string
}

// buildFrontendAssets generates the browser asset set.
//
// The steps are: scaffold the framework-owned document, copy the Go runtime
// shim out of GOROOT, and compile the client to WebAssembly. They are in one
// function because a partial asset set is worse than none — the server would
// start, serve a page, and fail in the browser — so nothing here leaves a
// directory half-built without reporting it.
func buildFrontendAssets(
	ctx context.Context,
	root string,
	build frontendAssetBuild,
	stdout, stderr io.Writer,
	label string,
) int {
	if !isRepositoryArtifactChild(root, build.Directory) {
		fmt.Fprintf(stderr, "%s: generated assets must remain beneath .artifacts\n", label)
		return exitUsage
	}
	if err := removeArtifactChild(root, build.Directory); err != nil {
		fmt.Fprintf(stderr, "%s: clear generated assets: %v\n", label, err)
		return exitFailure
	}
	parent := filepath.Dir(build.Directory)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		fmt.Fprintf(stderr, "%s: create artifact parent: %v\n", label, err)
		return exitFailure
	}

	gwcDirectory, err := resolveModuleDirectory(ctx, root, goWebComponentsModule)
	if err != nil {
		fmt.Fprintf(stderr, "%s: resolve GoWebComponents: %v\n", label, err)
		return exitFailure
	}
	if code := runCommandIn(
		ctx, gwcDirectory, stdout, stderr,
		"go", "run", "./tools/gwc", "scaffold",
		"-kind", "app",
		"-root", parent,
		"-dir", filepath.Base(build.Directory),
		"-name", build.ApplicationName,
		"-module", build.ModulePath,
		"-no-input", "-json",
	); code != exitSuccess {
		return code
	}

	// The scaffold leaves a runnable starter module behind. It is removed
	// rather than shipped: `go build ./...` from the repository root would
	// otherwise try to build a second module living inside this one.
	for _, leftover := range []string{"main.go", "starter_test.go", "go.mod", "go.sum"} {
		if err := os.Remove(filepath.Join(build.Directory, leftover)); err != nil &&
			!os.IsNotExist(err) {
			fmt.Fprintf(stderr, "%s: remove scaffold %s: %v\n", label, leftover, err)
			return exitFailure
		}
	}

	goRootOutput, err := commandOutput(ctx, root, safeToolEnvironment(), "go", "env", "GOROOT")
	if err != nil {
		fmt.Fprintf(stderr, "%s: resolve GOROOT: %v\n", label, err)
		return exitFailure
	}
	shim := filepath.Join(goRootOutput, "lib", "wasm", "wasm_exec.js")
	if _, err := os.Stat(shim); err != nil {
		shim = filepath.Join(goRootOutput, "misc", "wasm", "wasm_exec.js")
	}
	shimBytes, err := os.ReadFile(shim)
	if err != nil {
		fmt.Fprintf(stderr, "%s: read wasm_exec.js: %v\n", label, err)
		return exitFailure
	}
	if err := os.WriteFile(
		filepath.Join(build.Directory, "wasm_exec.js"), shimBytes, 0o644,
	); err != nil {
		fmt.Fprintf(stderr, "%s: write wasm_exec.js: %v\n", label, err)
		return exitFailure
	}

	binaryDirectory := filepath.Join(build.Directory, "bin")
	if err := os.MkdirAll(binaryDirectory, 0o755); err != nil {
		fmt.Fprintf(stderr, "%s: create WebAssembly output directory: %v\n", label, err)
		return exitFailure
	}
	command := exec.CommandContext(
		ctx, "go", "build", "-trimpath",
		"-o", filepath.Join(binaryDirectory, "main.wasm"),
		build.ClientPackage,
	)
	command.Dir = root
	command.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		fmt.Fprintf(stderr, "%s: build WebAssembly client: %v\n", label, err)
		return exitFailure
	}

	// The set is verified before it is announced. Reporting success on a
	// directory the server will later reject moves the failure to a place
	// where it is harder to read.
	for _, relative := range []string{"index.html", "wasm_exec.js", "bin/main.wasm"} {
		info, err := os.Stat(filepath.Join(build.Directory, filepath.FromSlash(relative)))
		if err != nil || info.IsDir() || info.Size() == 0 {
			fmt.Fprintf(stderr, "%s: %s was not produced\n", label, relative)
			return exitFailure
		}
	}
	fmt.Fprintf(stdout, "%s: browser assets ready at %s\n", label, build.Directory)
	return exitSuccess
}

// runBuildFrontend implements `codeflux-dev build-frontend`.
func runBuildFrontend(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	const label = "codeflux-dev build-frontend"
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return exitFailure
	}
	directory, err := resolveCommandRoot(root, frontendAssetDirectory, invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "%s: root: %v\n", label, err)
		return exitUsage
	}
	return buildFrontendAssets(ctx, root, frontendAssetBuild{
		Directory:       directory,
		ApplicationName: "CodeFlux",
		ModulePath:      "codeflux.dev/codeflux/frontend-assets",
		ClientPackage:   "./web/client",
	}, stdout, stderr, label)
}

// runServe implements `codeflux-dev serve`: build the assets, then start the
// product's own command against them.
//
// It exists so the development loop is one command rather than two that can
// drift out of step.
func runServe(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	if code := runBuildFrontend(ctx, stdout, stderr, invocation); code != exitSuccess {
		return code
	}
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev serve: %v\n", err)
		return exitFailure
	}
	directory, err := resolveCommandRoot(root, frontendAssetDirectory, invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev serve: root: %v\n", err)
		return exitUsage
	}
	return runCommandIn(ctx, root, stdout, stderr,
		"go", "run", "./cmd/codeflux", "start",
		"--assets", directory,
		"--database", filepath.Join(root, ".artifacts", "frontend-data", "codeflux.sqlite3"),
		"--no-browser",
	)
}
