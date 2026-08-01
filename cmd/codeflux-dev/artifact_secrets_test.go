package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// TestM22_122_ArtifactScanFindsSeededCredentials covers M22-122.
func TestM22_122_ArtifactScanFindsSeededCredentials(t *testing.T) {
	root := t.TempDir()
	artifacts := filepath.Join(root, ".artifacts", "browser-run")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatalf("create artifacts: %v", err)
	}

	// A clean artifact tree passes.
	if err := os.WriteFile(filepath.Join(artifacts, "run.log"),
		[]byte("scenario passed with no findings\n"), 0o600); err != nil {
		t.Fatalf("write clean artifact: %v", err)
	}
	if err := checkArtifactSecrets(root); err != nil {
		t.Fatalf("a clean artifact tree failed the scan: %v", err)
	}

	// Every scanned kind must be caught, not just the first one.
	for _, name := range []string{
		"replay.json", "diagnostics.txt", "session.jsonl",
		"report.md", "trace.log", "export.csv", "config.yaml",
	} {
		path := filepath.Join(artifacts, name)
		if err := os.WriteFile(path, []byte(
			"provider error: auth failed for "+testfixtures.FixtureCredentialMaterial+"\n"),
			0o600); err != nil {
			t.Fatalf("write leaky artifact: %v", err)
		}
		err := checkArtifactSecrets(root)
		if err == nil {
			t.Fatalf("a credential in %s was not detected", name)
		}
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("the finding does not name %s: %v", name, err)
		}
		// The report must identify the leak without reproducing it.
		if strings.Contains(err.Error(), testfixtures.FixtureCredentialMaterial) {
			t.Fatalf("the leak report reproduced the credential: %v", err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove leaky artifact: %v", err)
		}
	}

	// Every declared fixture credential shape must be scanned for, or a
	// fixture could seed material nothing looks at.
	for _, shape := range testfixtures.FixtureCredentialShapes() {
		path := filepath.Join(artifacts, "shape.log")
		if err := os.WriteFile(path, []byte("value: "+shape+"\n"), 0o600); err != nil {
			t.Fatalf("write shape artifact: %v", err)
		}
		if err := checkArtifactSecrets(root); err == nil {
			t.Fatalf("fixture shape %q is not scanned for", shape)
		}
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove shape artifact: %v", err)
		}
	}
}

// TestM22_122_ArtifactScanHandlesAbsentAndUnscannableTrees proves the check is
// neither vacuous nor brittle.
func TestM22_122_ArtifactScanHandlesAbsentAndUnscannableTrees(t *testing.T) {
	// No artifact directory is a clean result, not an error: a fresh checkout
	// has not run anything yet.
	if err := checkArtifactSecrets(t.TempDir()); err != nil {
		t.Fatalf("an absent artifact tree failed the scan: %v", err)
	}

	// A binary artifact is deliberately not scanned. It must not fail the
	// check, and it must not be mistaken for a clean text file either.
	root := t.TempDir()
	artifacts := filepath.Join(root, ".artifacts")
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatalf("create artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "heap.pprof"),
		[]byte(testfixtures.FixtureCredentialMaterial), 0o600); err != nil {
		t.Fatalf("write binary artifact: %v", err)
	}
	if err := checkArtifactSecrets(root); err != nil {
		t.Fatalf("an unscanned binary artifact failed the scan: %v", err)
	}
	if contains(scannedArtifactExtensions(), ".pprof") {
		t.Fatal("profiles are listed as scannable but cannot be meaningfully scanned")
	}

	// The forbidden set must be non-empty, or the whole check passes for the
	// wrong reason.
	if len(forbiddenArtifactMaterial()) == 0 {
		t.Fatal("no forbidden credential material is declared")
	}
}

// TestM22_122_ShapeDescriptionNeverReproducesTheSecret guards the reporting
// helper directly.
func TestM22_122_ShapeDescriptionNeverReproducesTheSecret(t *testing.T) {
	for _, secret := range append(testfixtures.FixtureCredentialShapes(), "short") {
		description := describeSecretShape(secret)
		if strings.Contains(description, secret) && len(secret) > 8 {
			t.Fatalf("description reproduced the secret: %q", description)
		}
		if !strings.Contains(description, "fixture credential material") {
			t.Fatalf("description does not identify the material: %q", description)
		}
	}
}
