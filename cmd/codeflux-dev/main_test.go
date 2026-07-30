package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	for _, command := range []string{
		"benchmark",
		"bootstrap",
		"build",
		"doctor",
		"generate",
		"inspect-db",
		"lint",
		"replay",
		"run",
		"run-live",
		"seed",
		"test-all",
		"test-browser",
		"test-coverage",
		"test-fast",
		"test-integration",
		"test-race",
		"test-security",
	} {
		if !strings.Contains(stdout.String(), command) {
			t.Errorf("help omits command %q: %s", command, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("help stderr = %q, want empty", stderr.String())
	}
}

func TestEveryRegisteredCommandSupportsHelp(t *testing.T) {
	for _, spec := range developmentCommandRegistry() {
		t.Run(spec.Name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := run(context.Background(), &stdout, &stderr, []string{spec.Name, "--help"})
			if code != exitSuccess {
				t.Fatalf("help exit = %d, stderr=%q", code, stderr.String())
			}
			for _, required := range []string{"Usage:", "Prerequisites:", "Exit codes:"} {
				if !strings.Contains(stdout.String(), required) {
					t.Errorf("help omits %q:\n%s", required, stdout.String())
				}
			}
		})
	}
}

func TestRegistryJSONIsVersionedAndStable(t *testing.T) {
	var first bytes.Buffer
	var second bytes.Buffer
	if err := printRegistry(&first, true); err != nil {
		t.Fatal(err)
	}
	if err := printRegistry(&second, true); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("registry JSON is not deterministic")
	}
	for _, required := range []string{
		`"schema_version": 1`,
		`"name": "bootstrap"`,
		`"purpose":`,
		`"prerequisites":`,
		`"arguments":`,
		`"exit_codes":`,
		`"machine_readable":`,
	} {
		if !strings.Contains(first.String(), required) {
			t.Errorf("registry JSON omits %q", required)
		}
	}
}

func TestUnavailableCommandHasStableExitAndJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"seed", "--json"})
	if code != exitUnavailable {
		t.Fatalf("seed exit = %d, want %d", code, exitUnavailable)
	}
	if !strings.Contains(stderr.String(), `"status":"unavailable"`) {
		t.Fatalf("seed JSON = %q", stderr.String())
	}
}

func TestCurrentSkeletonCommandsAreHonestlyUnavailable(t *testing.T) {
	for _, command := range []string{"doctor", "inspect-db", "package", "replay", "seed", "test-browser"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := run(context.Background(), &stdout, &stderr, []string{command, "--json"})
			if code != exitUnavailable {
				t.Fatalf("%s exit = %d, stderr=%q", command, code, stderr.String())
			}
			if !strings.Contains(stderr.String(), `"status":"unavailable"`) ||
				!strings.Contains(stderr.String(), `"command":"`+command+`"`) {
				t.Fatalf("%s result = %q", command, stderr.String())
			}
		})
	}
}

func TestStaticcheckExecutablePrefersBootstrappedTool(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, ".artifacts", "tools", "bin", "staticcheck")
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	writeTestFile(t, want, "fixture")
	if got := staticcheckExecutable(root); got != want {
		t.Fatalf("staticcheck executable = %q, want %q", got, want)
	}
}

func TestCommandRejectsUndeclaredMachineOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"build", "--json"})
	if code != exitUsage {
		t.Fatalf("build --json exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "--json is not supported") {
		t.Fatalf("build --json error = %q", stderr.String())
	}
}

func TestCommandRejectsOnceOutsideRun(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"build", "--once"})
	if code != exitUsage {
		t.Fatalf("build --once exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "--once is only valid for run") {
		t.Fatalf("build --once error = %q", stderr.String())
	}
}

func TestResolveCommandRootRejectsRepositoryAndLocalEscape(t *testing.T) {
	root := t.TempDir()
	valid, err := resolveCommandRoot(root, "build", "")
	if err != nil {
		t.Fatalf("default root: %v", err)
	}
	if want := filepath.Join(root, ".artifacts", "build"); valid != want {
		t.Fatalf("default root = %q, want %q", valid, want)
	}
	if _, err := resolveCommandRoot(root, "build", root); err == nil {
		t.Fatal("repository root was accepted")
	}
	if _, err := resolveCommandRoot(root, "build", filepath.Join(root, "output")); err == nil {
		t.Fatal("repository-local root outside .artifacts was accepted")
	}
	explicit := filepath.Join(root, ".artifacts", "custom")
	if got, err := resolveCommandRoot(root, "build", explicit); err != nil || got != explicit {
		t.Fatalf("explicit artifact child = %q, %v", got, err)
	}
}

func TestEveryCommandRejectsRepositoryLocalRootOutsideArtifactsBeforeWriting(t *testing.T) {
	repository, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	unsafeName := fmt.Sprintf(".codeflux-synthetic-unsafe-%d", os.Getpid())
	unsafePath := filepath.Join(repository, unsafeName)
	if _, err := os.Stat(unsafePath); !os.IsNotExist(err) {
		t.Fatalf("unsafe-path fixture unexpectedly exists: %v", err)
	}
	for _, spec := range developmentCommandRegistry() {
		t.Run(spec.Name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := run(context.Background(), &stdout, &stderr, []string{
				spec.Name,
				"--root",
				unsafeName,
			})
			if code != exitUsage {
				t.Fatalf("exit = %d, want %d; stdout=%q stderr=%q", code, exitUsage, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "child of .artifacts") {
				t.Fatalf("root error = %q", stderr.String())
			}
			if _, err := os.Stat(unsafePath); !os.IsNotExist(err) {
				t.Fatalf("unsafe path was written: %v", err)
			}
		})
	}
}

func TestImplementedCommandRejectsUnexpectedPositionalArgument(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), &stdout, &stderr, []string{"build", "unexpected"})
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "unexpected positional arguments") {
		t.Fatalf("argument error = %q", stderr.String())
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
