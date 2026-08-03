package coordinator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestValidateContainerCommandRejectsMalformedShapes covers the guard
// AUDIT-017 adds so a broken configuration fails at startup rather than being
// silently dropped.
func TestValidateContainerCommandRejectsMalformedShapes(t *testing.T) {
	if err := validateContainerCommand(nil); err != nil {
		t.Errorf("the native default was refused: %v", err)
	}
	if err := validateContainerCommand([]string{"podman", "run", "--rm", "-i"}); err != nil {
		t.Errorf("a well-formed container command was refused: %v", err)
	}
	for name, command := range map[string][]string{
		"a blank argument":    {"podman", ""},
		"a NUL byte argument": {"podman", "run\x00"},
		"too many arguments":  make([]string, 65),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateContainerCommand(command); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// TestAUDIT017_AConfiguredContainerCommandWrapsTheRealWorkerLaunch reconciles
// M11-033: it is not enough for internal/worker to know how to launch a
// worker inside a container command — nothing between ApplicationOptions and
// the launched subprocess ever carried one. StartWorker.ContainerCommand and
// Supervisor.StartWorker already threaded it end to end into worker.Launch;
// only the coordinator's own configuration surface never populated it, so
// every task ran natively regardless of what an operator configured, because
// there never was anywhere to configure it.
//
// The test proves the real path — ApplicationOptions.ContainerCommand,
// through StartApplication, through a real StartTask, to a real subprocess —
// rather than only that TaskRunLauncher.Launch builds the right struct.
// The container command here is a tiny compiled Go program that writes a
// marker file naming its own argv[1] and exits immediately; it never forwards
// to the worker executable that follows it on argv, which is deliberate: a
// worker that never reports back is exactly the failure this test does not
// need to avoid; the only thing it needs to observe is which executable the
// coordinator actually started as the subprocess.
func TestAUDIT017_AConfiguredContainerCommandWrapsTheRealWorkerLaunch(t *testing.T) {
	markerDir := t.TempDir()
	markerPath := filepath.Join(markerDir, "container-invoked")
	wrapper := buildContainerMarkerExecutable(t)

	root := t.TempDir()
	repositoryPath := filepath.Join(root, "repo")
	initializeCoordinatorGitRepository(t, repositoryPath)

	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		WorktreeRoot:     filepath.Join(root, "worktrees"),
		WorkerExecutable: buildCoordinatorWorkerExecutable(t),
		TaskControls:     &applicationTaskControlStub{},
		AgentModel: &scriptedEngineModel{
			turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
				writeFile("service/version.go", "package service\n\nconst Version = \"1\"\n"),
			},
		},
		// The command under test: argv[1] is the marker path, so a wrapper
		// invoked as configured writes exactly this file before doing
		// anything with the worker executable and arguments appended after
		// it (see internal/worker.buildWorkerCommand).
		ContainerCommand: []string{wrapper, markerPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	scope, err := application.repos.EnsureLocalBootstrap(
		context.Background(), repositoryPath, strings.Repeat("f", 40), "Engine")
	if err != nil {
		t.Fatalf("opening the repository failed: %v", err)
	}

	const requirement = "Update service/service.go so the handler reports its version."
	messageID, err := domain.NewMessageID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.AppendMessage(context.Background(), storage.AppendMessage{
		ID: messageID, ThreadID: scope.ThreadID, Role: storage.MessageRoleUser,
		BodyRedacted:   requirement,
		IdempotencyKey: "audit017-request",
	}); err != nil {
		t.Fatalf("recording the request failed: %v", err)
	}

	worktreeSeed := filepath.Join(repositoryPath, "service")
	if err := os.MkdirAll(worktreeSeed, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeSeed, "service.go"),
		[]byte("package service\n\nfunc Handle() string { return \"ok\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitEverythingForTest(t, repositoryPath)

	lifecycle := application.TaskLifecycleApplication()
	created, err := lifecycle.CreateTaskFromRequirement(context.Background(), transport.CreateTaskCommand{
		ThreadID:                 scope.ThreadID,
		RequestMessageID:         &messageID,
		Requirement:              requirement,
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("1", 40),
		BaselineModelRevision:    "scripted-provider-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"service"},
		IdempotencyKey:           "audit017-requirement",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}
	readyRevision := driveTaskToReady(t, application.repos, created.TaskID, created.Revision)
	preflight, err := application.TaskPreflightService().BindExecution(
		context.Background(), created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"audit017-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := lifecycle.StartPreparedTask(context.Background(), transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "audit017-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	// The marker is written by the container command itself, at process
	// start, before it does anything with the worker executable appended
	// after it on argv. If ApplicationOptions.ContainerCommand never reached
	// the launched subprocess, the coordinator would have executed the real
	// worker binary directly and this file would never appear.
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, statErr := os.Stat(markerPath); statErr == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the configured container command was never invoked as the worker's launch process")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// buildContainerMarkerExecutable compiles a standalone program that writes a
// marker file named by its own argv[1] and exits, standing in for a real
// container runtime without requiring one to be installed on the machine
// running the test.
func buildContainerMarkerExecutable(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := `package main

import "os"

func main() {
	if len(os.Args) > 1 {
		_ = os.WriteFile(os.Args[1], []byte("invoked"), 0o600)
	}
}
`
	sourcePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "container-marker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	output := filepath.Join(dir, name)
	command := exec.CommandContext(t.Context(), "go", "build", "-o", output, sourcePath)
	if built, err := command.CombinedOutput(); err != nil {
		t.Skipf("cannot build the container marker executable in this environment: %v\n%s", err, built)
	}
	return output
}
