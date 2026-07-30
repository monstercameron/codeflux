package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePackageDependencyEnforcesInwardAndAdapterBoundaries(t *testing.T) {
	domain := codefluxModulePath + "/internal/domain"
	if err := validatePackageDependency(domain, "context"); err != nil {
		t.Fatalf("domain standard-library dependency: %v", err)
	}
	if err := validatePackageDependency(domain, codefluxModulePath+"/internal/storage"); err == nil {
		t.Fatal("outward domain dependency was accepted")
	}
	storage := codefluxModulePath + "/internal/storage"
	if err := validatePackageDependency(storage, domain); err != nil {
		t.Fatalf("storage-to-domain dependency: %v", err)
	}
	if err := validatePackageDependency(storage, codefluxModulePath+"/web/assets"); err == nil {
		t.Fatal("storage frontend dependency was accepted")
	}
	if err := validatePackageDependency(storage, codefluxModulePath+"/internal/providers"); err == nil {
		t.Fatal("storage sibling-adapter dependency was accepted")
	}
}

func TestCheckPackageDependenciesParsesTrackedProductionSource(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("internal", "domain", "bad.go")
	writeTestFile(t, filepath.Join(root, relative), "package domain\n\nimport _ \"codeflux.dev/codeflux/internal/storage\"\n")
	err := checkPackageDependencies(root, []string{relative})
	if err == nil || !strings.Contains(err.Error(), "must not depend outward") {
		t.Fatalf("dependency error = %v", err)
	}
}

func TestCheckArtifactPolicyRequiresIgnoreAndRejectsEscape(t *testing.T) {
	root := t.TempDir()
	runGitForTest(t, root, "init")
	writeTestFile(t, filepath.Join(root, ".gitignore"), "/.artifacts/\n")
	runGitForTest(t, root, "add", "--", ".gitignore")

	if err := checkArtifactPolicy(context.Background(), root, []string{".gitignore"}); err != nil {
		t.Fatalf("valid artifact policy: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "escaped.exe"), "synthetic disposable output")
	err := checkArtifactPolicy(context.Background(), root, []string{".gitignore"})
	if err == nil || !strings.Contains(err.Error(), "escaped .artifacts") {
		t.Fatalf("escape error = %v", err)
	}
}

func TestCheckArtifactPolicyRejectsTrackedArtifactDescendant(t *testing.T) {
	root := t.TempDir()
	runGitForTest(t, root, "init")
	writeTestFile(t, filepath.Join(root, ".gitignore"), "/.artifacts/\n")
	writeTestFile(t, filepath.Join(root, ".artifacts", "forced.txt"), "fixture")
	runGitForTest(t, root, "add", "--", ".gitignore")
	runGitForTest(t, root, "add", "-f", "--", ".artifacts/forced.txt")

	tracked, err := trackedFiles(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkArtifactPolicy(context.Background(), root, tracked); err == nil ||
		!strings.Contains(err.Error(), "tracked artifact") {
		t.Fatalf("tracked artifact error = %v", err)
	}
}

func TestIsKnownDisposableArtifact(t *testing.T) {
	for _, path := range []string{"codeflux.exe", "coverage.out", "tmp/session.sqlite", "profile.pprof", ".DS_Store"} {
		if !isKnownDisposableArtifact(path) {
			t.Errorf("%q was not classified as disposable", path)
		}
	}
	for _, path := range []string{"cmd/codeflux/main.go", "design/reference.png", "testdata/POLICY.txt"} {
		if isKnownDisposableArtifact(path) {
			t.Errorf("%q was classified as disposable", path)
		}
	}
}

func runGitForTest(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = safeToolEnvironment()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func TestResolveCommandRootAllowsExplicitExternalWithoutTouchingIt(t *testing.T) {
	repository := t.TempDir()
	externalParent := t.TempDir()
	external := filepath.Join(externalParent, "codeflux-output")
	got, err := resolveCommandRoot(repository, "build", external)
	if err != nil {
		t.Fatal(err)
	}
	if got != external {
		t.Fatalf("external root = %q, want %q", got, external)
	}
	if _, err := os.Stat(external); !os.IsNotExist(err) {
		t.Fatalf("root resolution wrote external path: %v", err)
	}
}
