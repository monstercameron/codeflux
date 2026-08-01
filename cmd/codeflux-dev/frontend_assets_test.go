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
