package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildFrontendWritesWhereStartLooks binds the two commands together.
//
// They are in different packages and nothing at compile time connects them, so
// a rename on either side would produce a build that succeeds and a server
// that serves nothing — the exact failure this pipeline was added to fix. The
// binding is checked by reading cmd/codeflux's own source rather than by
// importing it, because cmd packages cannot import each other.
func TestBuildFrontendWritesWhereStartLooks(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	source, err := os.ReadFile(filepath.Join(root, "cmd", "codeflux", "assets.go"))
	if err != nil {
		t.Fatalf("read the start command's asset resolution: %v", err)
	}
	const declaration = `DevelopmentAssetDirectory = ".artifacts/frontend"`
	if !strings.Contains(string(source), declaration) {
		t.Fatalf("cmd/codeflux no longer declares %s; build-frontend writes to "+
			".artifacts/%s and start would not find it", declaration, frontendAssetDirectory)
	}
	if frontendAssetDirectory != "frontend" {
		t.Fatalf("build-frontend writes to .artifacts/%s, which start does not read",
			frontendAssetDirectory)
	}
}

func TestBuildFrontendRefusesToWriteOutsideArtifacts(t *testing.T) {
	// The output directory is cleared before it is rebuilt. A path outside
	// .artifacts would make that a destructive operation on somebody's work.
	root := repositoryRootForCommandGraph(t)
	var stdout, stderr strings.Builder
	code := buildFrontendAssets(t.Context(), root, frontendAssetBuild{
		Directory:       filepath.Join(root, "web"),
		ApplicationName: "Fixture",
		ModulePath:      "codeflux.dev/codeflux/fixture",
		ClientPackage:   "./web/client",
	}, &stdout, &stderr, "test")
	if code == exitSuccess {
		t.Fatal("a build targeting a source directory succeeded")
	}
	if !strings.Contains(stderr.String(), ".artifacts") {
		t.Errorf("the refusal does not say where output must live: %q", stderr.String())
	}
}

func TestBuildFrontendAndServeAreRegisteredCommands(t *testing.T) {
	// A command that exists as a function but not in the registry cannot be
	// run, and the registry is what `codeflux-dev help` prints.
	declared := map[string]bool{}
	for _, spec := range developmentCommandRegistry() {
		declared[spec.Name] = true
	}
	for _, name := range []string{"build-frontend", "serve"} {
		if !declared[name] {
			t.Errorf("%q is implemented but not declared in the command registry", name)
		}
	}
}

// TestStagingRefusesToRemoveAnythingOutsideTheEmbedDirectory guards the one
// destructive step a release build performs.
//
// It copies a compiled client into a source directory and then deletes it. A
// path-handling mistake there would delete source, so the removal is bounded
// by construction rather than by the caller passing the right thing.
func TestStagingRefusesToRemoveAnythingOutsideTheEmbedDirectory(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	outside := filepath.Join(root, "web", "assets", "embed.go")
	if err := unstageEmbeddedAssets(root, []string{outside}); err == nil {
		t.Fatal("unstaging removed a path outside the embed directory")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("a source file was removed: %v", err)
	}
}

// TestTheSourceTreeCarriesNoCompiledClient keeps a build output out of source.
//
// A staged client left behind would make every later ordinary build silently
// self-contained, and would put a tens-of-megabytes binary into the next
// commit.
func TestTheSourceTreeCarriesNoCompiledClient(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	present, err := embeddedAssetsPresent(root)
	if err != nil {
		t.Fatalf("inspect the embed directory: %v", err)
	}
	if present {
		t.Fatal("the embed directory carries a staged build output; " +
			"`codeflux-dev release` should have removed it")
	}
}

// TestStagingRequiresARealBuild refuses to embed an empty file.
//
// An empty main.wasm passes an existence check and produces an executable that
// starts, serves a page, and fails in the browser.
func TestStagingRequiresARealBuild(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(filepath.Join(assets, "bin"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"index.html", "wasm_exec.js"} {
		if err := os.WriteFile(
			filepath.Join(assets, relative), []byte("content"), 0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(assets, "bin", "main.wasm"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err := stageEmbeddedAssets(root, assets)
	t.Cleanup(func() { _ = unstageEmbeddedAssets(root, staged) })
	if err == nil {
		t.Fatal("an empty client was staged for embedding")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("the refusal does not say what was wrong: %v", err)
	}
}
