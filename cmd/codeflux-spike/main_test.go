package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLaunchSecretCreatesAndReusesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "launch-secret")

	first, err := resolveLaunchSecret(path)
	if err != nil {
		t.Fatalf("resolve first launch secret: %v", err)
	}
	second, err := resolveLaunchSecret(path)
	if err != nil {
		t.Fatalf("resolve second launch secret: %v", err)
	}
	if first != second {
		t.Fatal("expected the persisted launch secret to be reused")
	}
	if err := validateLaunchSecret(first); err != nil {
		t.Fatalf("validate generated launch secret: %v", err)
	}
}

func TestResolveLaunchSecretRejectsInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launch-secret")
	if err := os.WriteFile(path, []byte("not-a-secret\n"), 0o600); err != nil {
		t.Fatalf("write invalid launch secret: %v", err)
	}

	if _, err := resolveLaunchSecret(path); err == nil {
		t.Fatal("expected invalid launch secret file to be rejected")
	}
}
