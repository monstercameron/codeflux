package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildCompletenessEnvironment opts into the real end-to-end build. It is
// gated for the reason M22-G01 states: a WebAssembly client plus two command
// binaries is minutes of work, and the default suite must stay runnable on
// every change. CI runs `codeflux-dev build` itself on every supported
// platform, so the expensive path is exercised there rather than never.
const buildCompletenessEnvironment = "CODEFLUX_BUILD_COMPLETENESS"

// TestAUDIT003_OneBuildInvocationProducesEveryShippedArtifact covers AUDIT-003
// (reconciling M01-026).
//
// M01-026 was recorded complete against a `build` that compiled ./... and two
// commands. It never verified generated protobuf output and never built the
// browser client, so a tree with a stale contract or an unbuildable frontend
// passed the build and failed at the first page load.
func TestAUDIT003_OneBuildInvocationProducesEveryShippedArtifact(t *testing.T) {
	if os.Getenv(buildCompletenessEnvironment) != "1" {
		t.Skip("set " + buildCompletenessEnvironment + "=1 to run the real end-to-end build")
	}

	root := repositoryRootForCommandGraph(t)
	artifactRoot := filepath.Join(root, ".artifacts", "tmp", "build-completeness")
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		t.Fatalf("create artifact root: %v", err)
	}
	t.Cleanup(func() { _ = removeArtifactChild(root, artifactRoot) })

	var stdout, stderr bytes.Buffer
	code := runBuild(t.Context(), &stdout, &stderr, commandInvocation{Root: artifactRoot})
	if code != exitSuccess {
		t.Fatalf("build exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	suffix := ""
	if isWindows() {
		suffix = ".exe"
	}
	// One invocation, one root, every artifact the product ships.
	for _, relative := range []string{
		filepath.Join("bin", "codeflux"+suffix),
		filepath.Join("bin", "codeflux-worker"+suffix),
		filepath.Join(frontendAssetDirectory, "index.html"),
		filepath.Join(frontendAssetDirectory, "wasm_exec.js"),
		filepath.Join(frontendAssetDirectory, "bin", "main.wasm"),
	} {
		info, err := os.Stat(filepath.Join(artifactRoot, relative))
		if err != nil {
			t.Errorf("build did not produce %s: %v", relative, err)
			continue
		}
		if info.IsDir() || info.Size() == 0 {
			t.Errorf("build produced an empty %s", relative)
		}
	}
}

// TestAUDIT003_BuildVerifiesGeneratedOutputAndTheFrontend states the same
// claim cheaply enough for the default suite.
//
// It reads the dispatch rather than running it. That is weaker evidence than
// the gated test above and is not pretending otherwise: its job is to fail
// fast when someone removes a step, not to prove the step works.
func TestAUDIT003_BuildVerifiesGeneratedOutputAndTheFrontend(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	source, err := os.ReadFile(filepath.Join(root, "cmd", "codeflux-dev", "main.go"))
	if err != nil {
		t.Fatalf("read dispatch: %v", err)
	}
	body := string(source)
	start := strings.Index(body, "func runBuild(")
	if start < 0 {
		t.Fatal("there is no build command")
	}
	end := strings.Index(body[start:], "\nfunc buildLinkerFlags(")
	if end < 0 {
		t.Fatal("runBuild has no recognisable end")
	}
	window := body[start : start+end]

	for _, required := range []struct {
		fragment string
		claim    string
	}{
		{"runGenerateCheck(", "build does not verify generated protobuf output"},
		{"./cmd/codeflux", "build does not build the server command"},
		{"./cmd/codeflux-worker", "build does not build the worker command"},
		{"buildFrontendAssets(", "build does not build the GWC/WASM frontend"},
		{"./web/client", "build does not name the browser client package"},
	} {
		if !strings.Contains(window, required.fragment) {
			t.Errorf("%s (looked for %q)", required.claim, required.fragment)
		}
	}

	// Every output resolves against the one invocation root, so a caller
	// cannot end up with binaries in one place and the client in another.
	if strings.Count(window, "invocation.Root") < 2 {
		t.Error("build resolves its outputs against more than one root")
	}
}

// isWindows reports the host family without importing runtime into a test that
// only needs the executable suffix.
func isWindows() bool {
	return filepath.Separator == '\\'
}
