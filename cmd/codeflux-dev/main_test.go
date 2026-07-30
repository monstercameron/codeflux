package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, []string{"--help"})

	if code != exitSuccess {
		t.Fatalf("run help exit = %d, want %d; stderr=%q", code, exitSuccess, stderr.String())
	}
	for _, command := range []string{"build", "test-fast", "test-race", "test-coverage", "lint"} {
		if !strings.Contains(stdout.String(), command) {
			t.Errorf("help omits command %q: %s", command, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("help stderr = %q, want empty", stderr.String())
	}
}

func TestUnformattedGoFilesReportsOnlySourceOutsideArtifacts(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "formatted.go"), "package fixture\n")
	writeTestFile(t, filepath.Join(root, "unformatted.go"), "package fixture\n\nfunc value( )int{return 1}\n")
	writeTestFile(t, filepath.Join(root, ".artifacts", "ignored.go"), "not go source")
	writeTestFile(t, filepath.Join(root, ".git", "ignored.go"), "not go source")

	got, err := unformattedGoFiles(root)
	if err != nil {
		t.Fatalf("unformattedGoFiles: %v", err)
	}
	if len(got) != 1 || got[0] != "unformatted.go" {
		t.Fatalf("unformattedGoFiles = %v, want [unformatted.go]", got)
	}
}

func TestUnformattedGoFilesRejectsInvalidSource(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "invalid.go"), "package")

	_, err := unformattedGoFiles(root)
	if err == nil {
		t.Fatal("unformattedGoFiles accepted invalid Go source")
	}
	if !strings.Contains(err.Error(), "invalid.go") {
		t.Fatalf("unformattedGoFiles error = %q, want source path", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create test directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

func TestRunWithoutCommandIsUsageError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, nil)

	if code != exitUsage {
		t.Fatalf("run without command exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("usage error omits help: %q", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run(context.Background(), &stdout, &stderr, []string{"unknown"})

	if code != exitUsage {
		t.Fatalf("unknown command exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Fatalf("unknown command error = %q", stderr.String())
	}
}
