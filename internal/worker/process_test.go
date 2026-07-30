package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const workerProcessHelper = "CODEFLUX_WORKER_PROCESS_HELPER"

func TestWorkerProcessHelper(t *testing.T) {
	if os.Getenv(workerProcessHelper) == "" {
		return
	}
	startup, err := DecodeStartup(os.Stdin)
	if err != nil {
		os.Exit(11)
	}
	currentDirectory, err := os.Stat(".")
	if err != nil {
		os.Exit(12)
	}
	startupDirectory, err := os.Stat(startup.WorktreePath)
	if err != nil || !os.SameFile(currentDirectory, startupDirectory) {
		os.Exit(12)
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		os.Exit(13)
	}
	if os.Getenv(workerProcessHelper) == "sleep" {
		time.Sleep(30 * time.Second)
	}
}

func TestLaunchWorkerPassesStartupByPipeAndSanitizesEnvironment(t *testing.T) {
	worktree := t.TempDir()
	startup := workerStartupFixture(t, worktree)
	process, err := Launch(t.Context(), LaunchOptions{
		Executable:          os.Args[0],
		ExecutableArguments: []string{"-test.run=^TestWorkerProcessHelper$"},
		Startup:             startup,
		ParentEnvironment: append(os.Environ(),
			workerProcessHelper+"=exit",
			"OPENAI_API_KEY=must-not-reach-worker",
		),
		AdditionalAllowed: []string{workerProcessHelper},
	})
	if err != nil {
		t.Fatal(err)
	}
	if process.PID() <= 0 {
		t.Fatal("worker process ID was not recorded")
	}
	waitCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := process.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchWorkerCancellationTerminatesProcess(t *testing.T) {
	worktree := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	process, err := Launch(ctx, LaunchOptions{
		Executable:          os.Args[0],
		ExecutableArguments: []string{"-test.run=^TestWorkerProcessHelper$"},
		Startup:             workerStartupFixture(t, worktree),
		ParentEnvironment:   append(os.Environ(), workerProcessHelper+"=sleep"),
		AdditionalAllowed:   []string{workerProcessHelper},
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	waitCtx, waitCancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer waitCancel()
	if err := process.Wait(waitCtx); err == nil {
		t.Fatal("cancelled worker exited successfully")
	}
}

func TestDecodeStartupRejectsUnknownAndTrailingData(t *testing.T) {
	startup := workerStartupFixture(t, t.TempDir())
	_ = startup
	for _, value := range []string{
		`{"Unknown":"value"}`,
		`{} {}`,
	} {
		if _, err := DecodeStartup(strings.NewReader(value)); err == nil {
			t.Fatalf("unsafe startup accepted: %s", value)
		}
	}
	encoded, err := json.Marshal(startup)
	if err != nil {
		t.Fatal(err)
	}
	oversized := append(encoded, bytes.Repeat([]byte(" "), maxStartupBytes)...)
	if _, err := DecodeStartup(bytes.NewReader(oversized)); err == nil {
		t.Fatal("oversized startup was accepted")
	}
}

func TestBuildWorkerCommandSupportsDeclaredContainerAndLabelsIsolation(t *testing.T) {
	startup := workerStartupFixture(t, t.TempDir())
	startup.ContainerCommand = []string{"container-runtime", "run", "--"}
	executable, arguments, err := buildWorkerCommand(LaunchOptions{
		Executable:          "codeflux-worker",
		ExecutableArguments: []string{"--fixture"},
		Startup:             startup,
		ContainerCommand:    append([]string(nil), startup.ContainerCommand...),
	})
	if err != nil {
		t.Fatal(err)
	}
	if executable != "container-runtime" ||
		strings.Join(arguments, "|") != "run|--|codeflux-worker|--fixture" {
		t.Fatalf("container command = %s %#v", executable, arguments)
	}
	if !strings.Contains(DefaultIsolationLabel, "mediated workspace confinement") ||
		!strings.Contains(DefaultIsolationLabel, "not a perfect sandbox") {
		t.Fatalf("default isolation label = %q", DefaultIsolationLabel)
	}
	mismatched := startup
	mismatched.ContainerCommand = nil
	if _, _, err := buildWorkerCommand(LaunchOptions{
		Executable: "codeflux-worker", Startup: mismatched,
		ContainerCommand: []string{"container-runtime"},
	}); err == nil {
		t.Fatal("container command absent from startup metadata was accepted")
	}
}

func workerStartupFixture(t *testing.T, worktree string) StartupParameters {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(worktree)
	if err != nil {
		t.Fatal(err)
	}
	return StartupParameters{
		ProtocolVersion: ProtocolVersion, TaskID: taskID, RunID: runID,
		WorktreePath: absolute, ToolSchemaVersion: 1,
		CoordinatorEndpoint: "http://127.0.0.1:43117",
		SessionToken:        "0123456789abcdef0123456789abcdef",
	}
}
