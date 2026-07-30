package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateRepositorySourceIsDeterministicAndComplete(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "migrations", "000000_bootstrap.sql"), "SELECT 1;\n")
	writeTestFile(t, filepath.Join(source, "web", "assets", "static", "shell.css"), "body {}\n")
	writeTestFile(t, filepath.Join(source, "internal", "events", "kinds.go"), "package events\n\n//codeflux:event task.started\n")
	first := t.TempDir()
	second := t.TempDir()

	if err := generateRepositorySource(source, first); err != nil {
		t.Fatalf("first generation: %v", err)
	}
	if err := generateRepositorySource(source, second); err != nil {
		t.Fatalf("second generation: %v", err)
	}
	if err := compareDirectoryTrees(first, second); err != nil {
		t.Fatalf("deterministic generation: %v", err)
	}
	for _, fixture := range []struct {
		path string
		want string
	}{
		{path: "migrations/catalog_gen.go", want: "000000_bootstrap.sql"},
		{path: "web/assets/manifest_gen.go", want: "static/shell.css"},
		{path: "internal/events/registry_gen.go", want: "task.started"},
		{path: "internal/buildinfo/versions_gen.go", want: "generatedFrontendVersion"},
	} {
		content, err := os.ReadFile(filepath.Join(first, filepath.FromSlash(fixture.path)))
		if err != nil {
			t.Fatalf("read %s: %v", fixture.path, err)
		}
		if !strings.HasPrefix(string(content), "// Code generated ") ||
			!strings.Contains(string(content), fixture.want) {
			t.Errorf("generated %s lacks header or %q:\n%s", fixture.path, fixture.want, content)
		}
	}
}

func TestEventKindNamesRejectsDuplicateDirective(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "internal", "events", "first.go"), "package events\n\n//codeflux:event task.started\n")
	writeTestFile(t, filepath.Join(root, "internal", "events", "second.go"), "package events\n\n//codeflux:event task.started\n")

	if _, err := eventKindNames(root); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate event error = %v", err)
	}
}
