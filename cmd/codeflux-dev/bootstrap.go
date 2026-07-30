package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const (
	pinnedGoToolchain       = "go1.26.5"
	pinnedBufModule         = "github.com/bufbuild/buf/cmd/buf@v1.72.0"
	pinnedProtocGenGoModule = "google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11"
	pinnedStaticcheckModule = "honnef.co/go/tools/cmd/staticcheck@v0.7.0"
)

type bootstrapCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Detail  string `json:"detail"`
}

type bootstrapResult struct {
	SchemaVersion int              `json:"schema_version"`
	Status        string           `json:"status"`
	ToolRoot      string           `json:"tool_root"`
	Checks        []bootstrapCheck `json:"checks"`
}

func runBootstrap(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev bootstrap: repository: %v\n", err)
		return exitFailure
	}
	toolRoot, err := resolveCommandRoot(repository, "tools", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev bootstrap: root: %v\n", err)
		return exitUsage
	}
	result, err := bootstrapDevelopmentTools(ctx, repository, toolRoot)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev bootstrap: %v\n", err)
		return exitFailure
	}
	if invocation.JSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev bootstrap: encode result: %v\n", err)
			return exitFailure
		}
		return exitSuccess
	}
	fmt.Fprintf(stdout, "Codeflux bootstrap: %s\n", result.Status)
	fmt.Fprintf(stdout, "Tool root: %s\n", result.ToolRoot)
	for _, check := range result.Checks {
		version := ""
		if check.Version != "" {
			version = " " + check.Version
		}
		fmt.Fprintf(stdout, "  %-18s %-8s%s - %s\n", check.Name, check.Status, version, check.Detail)
	}
	return exitSuccess
}

func bootstrapDevelopmentTools(
	ctx context.Context,
	repository string,
	toolRoot string,
) (bootstrapResult, error) {
	goMod, err := os.ReadFile(filepath.Join(repository, "go.mod"))
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("read go.mod: %w", err)
	}
	if !regexp.MustCompile(`(?m)^go 1\.26\.0$`).Match(goMod) {
		return bootstrapResult{}, fmt.Errorf("verify Go language version: go.mod must declare go 1.26.0")
	}
	goVersion, err := commandOutput(
		ctx,
		repository,
		withEnvironment(safeToolEnvironment(), "GOTOOLCHAIN="+pinnedGoToolchain),
		"go",
		"version",
	)
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("verify patched Go toolchain: %w", err)
	}
	if !strings.Contains(goVersion, pinnedGoToolchain) {
		return bootstrapResult{}, fmt.Errorf("verify patched Go toolchain: got %q, want %s", goVersion, pinnedGoToolchain)
	}
	gitVersion, err := commandOutput(ctx, repository, safeToolEnvironment(), "git", "--version")
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("verify Git: %w", err)
	}

	if err := verifyGeneratorPins(repository); err != nil {
		return bootstrapResult{}, fmt.Errorf("verify generator configuration: %w", err)
	}
	binRoot := filepath.Join(toolRoot, "bin")
	if err := os.MkdirAll(binRoot, 0o755); err != nil {
		return bootstrapResult{}, fmt.Errorf("create tool root: %w", err)
	}
	installEnvironment := withEnvironment(
		safeToolEnvironment(),
		"GOBIN="+binRoot,
		"GOTOOLCHAIN="+pinnedGoToolchain,
	)
	tools := []struct {
		name       string
		module     string
		versionArg []string
		want       string
	}{
		{name: "buf", module: pinnedBufModule, versionArg: []string{"--version"}, want: "1.72.0"},
		{name: "protoc-gen-go", module: pinnedProtocGenGoModule, versionArg: []string{"--version"}, want: "v1.36.11"},
		{name: "staticcheck", module: pinnedStaticcheckModule, versionArg: []string{"-version"}, want: "2026.1"},
	}
	checks := []bootstrapCheck{
		{Name: "Go", Status: "ok", Version: pinnedGoToolchain, Detail: "patched Go 1.26 toolchain selected"},
		{Name: "Git", Status: "ok", Version: strings.TrimPrefix(gitVersion, "git version "), Detail: "source identity and worktree tooling available"},
	}
	for _, tool := range tools {
		if _, err := commandOutput(
			ctx,
			repository,
			installEnvironment,
			"go",
			"install",
			tool.module,
		); err != nil {
			return bootstrapResult{}, fmt.Errorf("install %s: %w", tool.name, err)
		}
		executable := filepath.Join(binRoot, tool.name)
		if runtime.GOOS == "windows" {
			executable += ".exe"
		}
		version, err := commandOutput(
			ctx,
			repository,
			safeToolEnvironment(),
			executable,
			tool.versionArg...,
		)
		if err != nil {
			return bootstrapResult{}, fmt.Errorf("verify %s: %w", tool.name, err)
		}
		if !strings.Contains(version, tool.want) {
			return bootstrapResult{}, fmt.Errorf("verify %s: got %q, want %s", tool.name, version, tool.want)
		}
		checks = append(checks, bootstrapCheck{
			Name:    tool.name,
			Status:  "ok",
			Version: tool.want,
			Detail:  "pinned repository-local tool installed",
		})
	}
	if err := verifyGoWebComponentsBoundary(repository); err != nil {
		return bootstrapResult{}, fmt.Errorf("verify GoWebComponents boundary: %w", err)
	}
	checks = append(checks, bootstrapCheck{
		Name:    "GoWebComponents",
		Status:  "deferred",
		Version: "M06-001",
		Detail:  "exact v5 release intentionally awaits the bounded transport spike",
	})
	return bootstrapResult{
		SchemaVersion: 1,
		Status:        "ok",
		ToolRoot:      toolRoot,
		Checks:        checks,
	}, nil
}

func verifyGeneratorPins(repository string) error {
	bufConfig, err := os.ReadFile(filepath.Join(repository, "buf.gen.yaml"))
	if err != nil {
		return err
	}
	if !bytes.Contains(bufConfig, []byte("buf.build/protocolbuffers/go:v1.36.11")) {
		return fmt.Errorf("buf.gen.yaml lacks protoc-gen-go v1.36.11")
	}
	mainSource, err := os.ReadFile(filepath.Join(repository, "cmd", "codeflux-dev", "main.go"))
	if err != nil {
		return err
	}
	if !bytes.Contains(mainSource, []byte(pinnedBufModule)) {
		return fmt.Errorf("generation entry point lacks pinned Buf 1.72.0")
	}
	return nil
}

func verifyGoWebComponentsBoundary(repository string) error {
	goMod, err := os.ReadFile(filepath.Join(repository, "go.mod"))
	if err != nil {
		return err
	}
	if bytes.Contains(bytes.ToLower(goMod), []byte("gowebcomponents")) {
		return fmt.Errorf("GoWebComponents dependency exists before M06-001 selects the exact v5 release")
	}
	todos, err := os.ReadFile(filepath.Join(repository, "TODOS.md"))
	if err != nil {
		return err
	}
	if !bytes.Contains(todos, []byte("`M06-001 BLOCKER SPIKE` Locate and pin the exact GoWebComponents v5 module and release.")) {
		return fmt.Errorf("M06-001 GoWebComponents selection gate is missing")
	}
	return nil
}

func commandOutput(
	ctx context.Context,
	directory string,
	environment []string,
	name string,
	args ...string,
) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = environment
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, text)
	}
	return text, nil
}

func safeToolEnvironment() []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "SECRET") ||
			strings.Contains(upper, "PASSWORD") ||
			strings.Contains(upper, "CREDENTIAL") ||
			strings.Contains(upper, "API_KEY") {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}

func withEnvironment(environment []string, values ...string) []string {
	result := append([]string(nil), environment...)
	for _, value := range values {
		name, _, _ := strings.Cut(value, "=")
		prefix := strings.ToUpper(name) + "="
		filtered := result[:0]
		for _, existing := range result {
			if !strings.HasPrefix(strings.ToUpper(existing), prefix) {
				filtered = append(filtered, existing)
			}
		}
		result = append(filtered, value)
	}
	return result
}
