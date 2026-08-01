package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/assets"
)

// writeAssetSet creates a usable browser asset set on disk.
func writeAssetSet(t *testing.T, directory string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(directory, "bin"), 0o750); err != nil {
		t.Fatalf("create asset directory: %v", err)
	}
	for relative, content := range map[string]string{
		"index.html":    "<!DOCTYPE html><html><body>fixture</body></html>",
		"wasm_exec.js":  "// fixture runtime shim",
		"bin/main.wasm": "\x00asm fixture",
	} {
		path := filepath.Join(directory, filepath.FromSlash(relative))
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", relative, err)
		}
	}
}

func TestStartRefusesToRunWithoutAnInterfaceToServe(t *testing.T) {
	// The failure this replaces: the coordinator came up, printed a URL, and
	// served 404 there. A process that looks healthy while the product is
	// absent is worse than one that refuses to start.
	_, _, err := resolveFrontendAssets("", func(string) (string, bool) { return "", false }, t.TempDir())
	if err == nil {
		t.Fatal("assets resolved in a tree that has none")
	}
	if !errors.Is(err, ErrNoFrontendAssets) {
		t.Fatalf("error does not identify the missing assets: %v", err)
	}
	// The message has to name the command that fixes it. "no assets" sends
	// somebody to read source; the command sends them to a working server.
	if !strings.Contains(err.Error(), "build-frontend") {
		t.Errorf("the error does not name the build command: %v", err)
	}
}

func TestAnExplicitAssetsPathIsUsed(t *testing.T) {
	directory := t.TempDir()
	writeAssetSet(t, directory)

	resolved, origin, err := resolveFrontendAssets(directory, nil, t.TempDir())
	if err != nil {
		t.Fatalf("an explicit assets directory was refused: %v", err)
	}
	if origin != originFlag {
		t.Errorf("origin = %q, want %q", origin, originFlag)
	}
	index, err := resolved.Get("index.html")
	if err != nil || !strings.Contains(string(index), "fixture") {
		t.Errorf("the resolved set did not come from the named directory: %v", err)
	}
}

func TestTheEnvironmentNamesAssetsWhenNoFlagDoes(t *testing.T) {
	directory := t.TempDir()
	writeAssetSet(t, directory)

	resolved, origin, err := resolveFrontendAssets("", func(name string) (string, bool) {
		if name == "CODEFLUX_ASSETS" {
			return directory, true
		}
		return "", false
	}, t.TempDir())
	if err != nil {
		t.Fatalf("CODEFLUX_ASSETS was refused: %v", err)
	}
	if origin != originEnvironment {
		t.Errorf("origin = %q, want %q", origin, originEnvironment)
	}
	if _, err := resolved.Get("wasm_exec.js"); err != nil {
		t.Errorf("the environment-named set is incomplete: %v", err)
	}
}

func TestAnEmptyEnvironmentValueDoesNotCountAsAnAnswer(t *testing.T) {
	// An exported-but-blank variable is a common shell accident. Treating it
	// as an answer would produce "assets directory \"\"" instead of the
	// message that names the build command.
	_, _, err := resolveFrontendAssets("", func(string) (string, bool) {
		return "   ", true
	}, t.TempDir())
	if !errors.Is(err, ErrNoFrontendAssets) {
		t.Fatalf("a blank CODEFLUX_ASSETS was treated as an answer: %v", err)
	}
}

func TestADevelopmentCheckoutNeedsNoFlag(t *testing.T) {
	// A checkout with built assets must start with no arguments, because a
	// development loop that requires remembering a path is a loop people stop
	// running.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module fixture\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	build := filepath.Join(root, filepath.FromSlash(DevelopmentAssetDirectory))
	writeAssetSet(t, build)

	nested := filepath.Join(root, "internal", "somewhere", "deep")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}
	// Started from a subdirectory, which is where people actually are.
	_, origin, err := resolveFrontendAssets("", nil, nested)
	if err != nil {
		t.Fatalf("a built checkout was refused from a subdirectory: %v", err)
	}
	if origin != originDevelopment {
		t.Errorf("origin = %q, want %q", origin, originDevelopment)
	}
}

func TestAHalfBuiltDirectoryIsReportedAsMissingNotAsBroken(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module fixture\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	build := filepath.Join(root, filepath.FromSlash(DevelopmentAssetDirectory))
	writeAssetSet(t, build)

	for _, removed := range assets.RequiredAssets() {
		t.Run(removed, func(t *testing.T) {
			partial := t.TempDir()
			if err := os.WriteFile(filepath.Join(partial, "go.mod"),
				[]byte("module fixture\n"), 0o600); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}
			directory := filepath.Join(partial, filepath.FromSlash(DevelopmentAssetDirectory))
			writeAssetSet(t, directory)
			if err := os.Remove(
				filepath.Join(directory, filepath.FromSlash(removed)),
			); err != nil {
				t.Fatalf("remove %s: %v", removed, err)
			}
			// An interrupted build should send somebody back to the build
			// command, not report a missing file from a path they never typed.
			_, _, err := resolveFrontendAssets("", nil, partial)
			if !errors.Is(err, ErrNoFrontendAssets) {
				t.Fatalf("a partial build was not reported as missing: %v", err)
			}
			if !strings.Contains(err.Error(), "build-frontend") {
				t.Errorf("the error does not name the build command: %v", err)
			}
		})
	}
}

func TestAnEmptyAssetIsNotAUsableAsset(t *testing.T) {
	// A zero-byte main.wasm is what an interrupted build leaves behind. It
	// exists, so a bare existence check would accept it and the browser would
	// fail with an error nobody can act on.
	directory := t.TempDir()
	writeAssetSet(t, directory)
	if err := os.WriteFile(
		filepath.Join(directory, "bin", "main.wasm"), nil, 0o600,
	); err != nil {
		t.Fatalf("truncate main.wasm: %v", err)
	}
	if directoryHasRequiredAssets(directory) {
		t.Fatal("a directory with a zero-byte client was reported usable")
	}
}

func TestAMissingDirectoryIsRefusedByName(t *testing.T) {
	_, _, err := resolveFrontendAssets(
		filepath.Join(t.TempDir(), "nowhere"), nil, t.TempDir())
	if err == nil {
		t.Fatal("a nonexistent --assets directory was accepted")
	}
	if !strings.Contains(err.Error(), "index.html") {
		t.Errorf("the error does not name what was missing: %v", err)
	}
}

func TestStartAcceptsTheAssetsFlag(t *testing.T) {
	arguments, err := parseStartArguments([]string{"--assets", "somewhere"})
	if err != nil {
		t.Fatalf("--assets was refused: %v", err)
	}
	if arguments.assets != "somewhere" {
		t.Errorf("assets = %q, want %q", arguments.assets, "somewhere")
	}
	if _, err := parseStartArguments([]string{"--assets"}); err == nil {
		t.Error("--assets with no value was accepted")
	}
	if _, err := parseStartArguments([]string{"--assets", "  "}); err == nil {
		t.Error("--assets with a blank value was accepted")
	}
	// The flags must stay independent: a switch that fell through would put
	// the assets path into the listen address, which fails much later.
	arguments, err = parseStartArguments([]string{
		"--assets", "a", "--database", "b", "--address", "127.0.0.1:1234",
	})
	if err != nil {
		t.Fatalf("combined flags were refused: %v", err)
	}
	if arguments.assets != "a" || arguments.database != "b" ||
		arguments.address != "127.0.0.1:1234" {
		t.Errorf("flags were crossed: %+v", arguments)
	}
}
