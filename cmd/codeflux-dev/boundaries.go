package main

import (
	"bytes"
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const codefluxModulePath = "codeflux.dev/codeflux"

func checkPackageDependencies(root string, tracked []string) error {
	for _, relative := range tracked {
		slashed := filepath.ToSlash(relative)
		if filepath.Ext(relative) != ".go" || strings.HasSuffix(relative, "_test.go") ||
			strings.Contains(slashed, "/testdata/") || strings.HasPrefix(slashed, "api/gen/") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), relative, source, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse imports for %s: %w", slashed, err)
		}
		sourcePackage := packageImportPath(slashed)
		for _, imported := range file.Imports {
			target, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return fmt.Errorf("parse import in %s: %w", slashed, err)
			}
			if err := validatePackageDependency(sourcePackage, target); err != nil {
				return fmt.Errorf("%s: %w", slashed, err)
			}
		}
	}
	return nil
}

func packageImportPath(relative string) string {
	directory := filepath.ToSlash(filepath.Dir(relative))
	if directory == "." {
		return codefluxModulePath
	}
	return codefluxModulePath + "/" + directory
}

func validatePackageDependency(source, target string) error {
	domain := codefluxModulePath + "/internal/domain"
	if source == domain || strings.HasPrefix(source, domain+"/") {
		if strings.HasPrefix(target, codefluxModulePath+"/") &&
			target != domain && !strings.HasPrefix(target, domain+"/") {
			return fmt.Errorf("domain package must not depend outward on %s", target)
		}
	}
	adapterRoots := []string{
		codefluxModulePath + "/internal/storage",
		codefluxModulePath + "/internal/providers",
		codefluxModulePath + "/internal/transport",
	}
	sourceAdapter := ""
	for _, adapter := range adapterRoots {
		if source == adapter || strings.HasPrefix(source, adapter+"/") {
			sourceAdapter = adapter
			break
		}
	}
	if sourceAdapter == "" {
		return nil
	}
	if target == codefluxModulePath+"/web" ||
		strings.HasPrefix(target, codefluxModulePath+"/web/") {
		return fmt.Errorf("adapter package %s must not own frontend dependency %s", sourceAdapter, target)
	}
	for _, adapter := range adapterRoots {
		if adapter != sourceAdapter && (target == adapter || strings.HasPrefix(target, adapter+"/")) {
			return fmt.Errorf("adapter package %s must depend through inward ports, not sibling adapter %s", sourceAdapter, target)
		}
	}
	return nil
}

func checkArtifactPolicy(ctx context.Context, root string, tracked []string) error {
	for _, relative := range tracked {
		slashed := filepath.ToSlash(relative)
		if slashed == ".artifacts" || strings.HasPrefix(slashed, ".artifacts/") {
			return fmt.Errorf("tracked artifact path is forbidden: %s", slashed)
		}
	}
	ignore := exec.CommandContext(ctx, "git", "check-ignore", "--quiet", ".artifacts/policy-probe")
	ignore.Dir = root
	if err := ignore.Run(); err != nil {
		return fmt.Errorf(".artifacts descendants must remain ignored: %w", err)
	}
	untracked := exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard", "-z")
	untracked.Dir = root
	output, err := untracked.Output()
	if err != nil {
		return fmt.Errorf("list untracked artifact candidates: %w", err)
	}
	for _, path := range bytes.Split(output, []byte{0}) {
		if len(path) == 0 {
			continue
		}
		slashed := filepath.ToSlash(string(path))
		if isKnownDisposableArtifact(slashed) {
			return fmt.Errorf("disposable artifact escaped .artifacts: %s", slashed)
		}
	}
	return nil
}

func isKnownDisposableArtifact(path string) bool {
	lower := strings.ToLower(path)
	base := filepath.Base(lower)
	for _, suffix := range []string{
		".db", ".db-shm", ".db-wal", ".dll", ".dylib", ".exe", ".log", ".out",
		".pprof", ".prof", ".so", ".sqlite", ".sqlite3", ".test", ".wasm",
	} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	switch base {
	case ".ds_store", "coverage", "coverage.out":
		return true
	default:
		return false
	}
}

func runArtifactCheck(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	if code := validateCommandRoot("artifact-check", invocation, stderr); code != exitSuccess {
		return code
	}
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev artifact-check: repository: %v\n", err)
		return exitFailure
	}
	tracked, err := trackedFiles(ctx, root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev artifact-check: %v\n", err)
		return exitFailure
	}
	if err := checkArtifactPolicy(ctx, root, tracked); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev artifact-check: %v\n", err)
		return exitFailure
	}
	// M22-122: retained artifacts are what a developer attaches to a bug
	// report, so a seeded credential surviving into one is a leak with a
	// delivery mechanism.
	if err := checkArtifactSecrets(root); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev artifact-check: %v\n", err)
		return exitFailure
	}
	fmt.Fprintln(stdout, "codeflux-dev artifact-check: no repository-local artifact escaped .artifacts")
	fmt.Fprintln(stdout, "codeflux-dev artifact-check: no seeded credential reached a retained artifact")
	return exitSuccess
}
