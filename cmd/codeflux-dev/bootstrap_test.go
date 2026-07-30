package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyGeneratorPins(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "buf.gen.yaml"), "remote: buf.build/protocolbuffers/go:v1.36.11\n")
	writeTestFile(t, filepath.Join(root, "cmd", "codeflux-dev", "main.go"), `const tool = "`+pinnedBufModule+`"`)
	if err := verifyGeneratorPins(root); err != nil {
		t.Fatalf("valid pins: %v", err)
	}

	writeTestFile(t, filepath.Join(root, "buf.gen.yaml"), "remote: unpinned\n")
	if err := verifyGeneratorPins(root); err == nil {
		t.Fatal("missing protoc-gen-go pin was accepted")
	}
}

func TestVerifyGoWebComponentsBoundary(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module fixture\n\ngo 1.26.0\n")
	writeTestFile(t, filepath.Join(root, "TODOS.md"), "- [ ] `M06-001 BLOCKER SPIKE` Locate and pin the exact GoWebComponents v5 module and release.\n")
	if err := verifyGoWebComponentsBoundary(root); err != nil {
		t.Fatalf("valid deferred boundary: %v", err)
	}

	writeTestFile(t, filepath.Join(root, "go.mod"), "module fixture\n\nrequire example.com/GoWebComponents v5.0.0\n")
	if err := verifyGoWebComponentsBoundary(root); err == nil {
		t.Fatal("premature GoWebComponents dependency was accepted")
	}
}

func TestSafeToolEnvironmentRemovesCredentialShapedNames(t *testing.T) {
	t.Setenv("CODEFLUX_BOOTSTRAP_API_KEY", "not-a-real-secret")
	t.Setenv("CODEFLUX_BOOTSTRAP_TOKEN", "not-a-real-token")
	t.Setenv("CODEFLUX_BOOTSTRAP_SAFE_VALUE", "retained")

	environment := safeToolEnvironment()
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "CODEFLUX_BOOTSTRAP_API_KEY=") ||
		strings.Contains(joined, "CODEFLUX_BOOTSTRAP_TOKEN=") {
		t.Fatal("credential-shaped environment names were retained")
	}
	if !strings.Contains(joined, "CODEFLUX_BOOTSTRAP_SAFE_VALUE=retained") {
		t.Fatal("ordinary environment value was removed")
	}
}

func TestWithEnvironmentReplacesExistingValue(t *testing.T) {
	environment := withEnvironment(
		[]string{"PATH=fixture", "GOTOOLCHAIN=old"},
		"GOTOOLCHAIN="+pinnedGoToolchain,
	)
	if got := strings.Join(environment, "\n"); strings.Count(got, "GOTOOLCHAIN=") != 1 ||
		!strings.Contains(got, "GOTOOLCHAIN="+pinnedGoToolchain) {
		t.Fatalf("environment replacement = %q", got)
	}
}

func TestCommandOutputReportsCapturedFailure(t *testing.T) {
	if os.Getenv("GO_WANT_BOOTSTRAP_HELPER") == "1" {
		os.Stderr.WriteString("synthetic bootstrap failure")
		os.Exit(7)
	}
	t.Setenv("GO_WANT_BOOTSTRAP_HELPER", "1")
	_, err := commandOutput(
		t.Context(),
		t.TempDir(),
		os.Environ(),
		os.Args[0],
		"-test.run=TestCommandOutputReportsCapturedFailure",
	)
	if err == nil || !strings.Contains(err.Error(), "synthetic bootstrap failure") {
		t.Fatalf("captured failure = %v", err)
	}
}
