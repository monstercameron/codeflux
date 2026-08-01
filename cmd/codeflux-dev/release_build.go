package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// stagedAssetNames are the files a release build copies into web/assets/static
// so //go:embed compiles them into the executable.
//
// They are staged and then removed rather than committed. The client is tens
// of megabytes and changes with every frontend edit; keeping it in history
// would make every visual change a binary commit, and a checkout would carry a
// build nobody asked for.
func stagedAssetNames() []string {
	return []string{"index.html", "wasm_exec.js", filepath.Join("bin", "main.wasm")}
}

// runRelease implements `codeflux-dev release`.
//
// It produces an executable that needs nothing beside it: the browser assets
// are compiled in, so `codeflux start` serves the interface from inside
// itself. That is the difference between a development checkout and something
// a person can be handed.
func runRelease(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	const label = "codeflux-dev release"
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return exitFailure
	}
	assets, err := resolveCommandRoot(root, frontendAssetDirectory, invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "%s: root: %v\n", label, err)
		return exitUsage
	}
	binaries, err := resolveCommandRoot(root, "release", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "%s: root: %v\n", label, err)
		return exitUsage
	}

	// The generated protobuf output is verified before anything is built, so a
	// release can never carry a contract that disagrees with its source.
	if code := runGenerateCheck(ctx, stdout, stderr, invocation); code != exitSuccess {
		fmt.Fprintf(stderr, "%s: generated output does not match its source\n", label)
		return code
	}
	if code := buildFrontendAssets(ctx, root, frontendAssetBuild{
		Directory:       assets,
		ApplicationName: "CodeFlux",
		ModulePath:      "codeflux.dev/codeflux/frontend-assets",
		ClientPackage:   "./web/client",
	}, stdout, stderr, label); code != exitSuccess {
		return code
	}

	staged, err := stageEmbeddedAssets(root, assets)
	// The staged files are removed whatever happens next. Leaving a compiled
	// client in a source directory would make the next ordinary build produce
	// a silently self-contained executable, which is exactly the confusion
	// this command exists to remove.
	defer func() {
		if err := unstageEmbeddedAssets(root, staged); err != nil {
			fmt.Fprintf(stderr, "%s: remove staged assets: %v\n", label, err)
		}
	}()
	if err != nil {
		fmt.Fprintf(stderr, "%s: stage assets for embedding: %v\n", label, err)
		return exitFailure
	}

	if err := os.MkdirAll(binaries, 0o755); err != nil {
		fmt.Fprintf(stderr, "%s: create release directory: %v\n", label, err)
		return exitFailure
	}
	ldflags, err := buildLinkerFlags(ctx, root)
	if err != nil {
		fmt.Fprintf(stderr, "%s: version metadata: %v\n", label, err)
		return exitFailure
	}
	suffix := executableSuffix()
	for name, pkg := range map[string]string{
		"codeflux" + suffix:        "./cmd/codeflux",
		"codeflux-worker" + suffix: "./cmd/codeflux-worker",
	} {
		if code := runGoIn(ctx, root, stdout, stderr,
			"build", "-trimpath", "-buildvcs=false", "-ldflags", ldflags,
			"-o", filepath.Join(binaries, name), pkg,
		); code != exitSuccess {
			return code
		}
	}
	fmt.Fprintf(stdout, "%s: self-contained executables at %s\n", label, binaries)
	return exitSuccess
}

// stageEmbeddedAssets copies the built assets into the embed directory.
//
// It returns what it wrote so the caller can remove exactly that, rather than
// clearing a source directory by pattern.
func stageEmbeddedAssets(root, assets string) ([]string, error) {
	target := filepath.Join(root, "web", "assets", "static")
	staged := make([]string, 0, len(stagedAssetNames()))
	for _, relative := range stagedAssetNames() {
		source := filepath.Join(assets, relative)
		destination := filepath.Join(target, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return staged, fmt.Errorf("create %s: %w", filepath.Dir(relative), err)
		}
		content, err := os.ReadFile(source)
		if err != nil {
			return staged, fmt.Errorf("read %s: %w", relative, err)
		}
		if len(content) == 0 {
			return staged, fmt.Errorf("%s is empty; the build did not produce it", relative)
		}
		if err := os.WriteFile(destination, content, 0o644); err != nil {
			return staged, fmt.Errorf("write %s: %w", relative, err)
		}
		staged = append(staged, destination)
	}
	return staged, nil
}

// unstageEmbeddedAssets removes exactly what was staged.
func unstageEmbeddedAssets(root string, staged []string) error {
	target := filepath.Join(root, "web", "assets", "static")
	for _, path := range staged {
		// Nothing outside the embed directory is ever removed, whatever the
		// caller passed.
		relative, err := filepath.Rel(target, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			return fmt.Errorf("refusing to remove %s from outside the embed directory", path)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	// The bin directory the client was staged into is removed too, so the
	// source tree returns to exactly what it was.
	binary := filepath.Join(target, "bin")
	entries, err := os.ReadDir(binary)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		return os.Remove(binary)
	}
	return nil
}

// executableSuffix returns the platform's executable extension.
func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// embeddedAssetsPresent reports whether a source tree currently carries staged
// assets, so a check can refuse to commit one.
func embeddedAssetsPresent(root string) (bool, error) {
	target := filepath.Join(root, "web", "assets", "static")
	present := false
	err := filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.EqualFold(entry.Name(), "README.md") {
			return nil
		}
		present = true
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return present, nil
}
