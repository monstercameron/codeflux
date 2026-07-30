package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCIFailureArtifactUsesOnlyAllowListedFields(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-appear")
	root := t.TempDir()
	outputDir := filepath.Join(root, ".artifacts", "test-failures")
	artifact := ciFailureArtifact{
		SchemaVersion: ciFailureArtifactSchemaVersion,
		Commit:        "0123456789abcdef",
		GoVersion:     "go-test",
		OperatingSys:  "test-os",
		Architecture:  "test-arch",
	}

	if err := writeCIFailureArtifact(outputDir, artifact); err != nil {
		t.Fatalf("writeCIFailureArtifact() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(root, ".artifacts", "test-failures", "context.json"))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		`"schema_version": 1`,
		`"commit": "0123456789abcdef"`,
		`"go_version": "go-test"`,
		`"operating_system": "test-os"`,
		`"architecture": "test-arch"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("artifact does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "must-not-appear") {
		t.Fatal("artifact copied an environment secret")
	}
}
