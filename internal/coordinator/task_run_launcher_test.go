package coordinator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// recordingRunLauncher records what it was asked to start.
//
// It is the double every lifecycle test uses, and its recording is what lets a
// test assert that starting a task actually asked for a worker rather than
// only writing a row.
type recordingRunLauncher struct {
	mu       sync.Mutex
	launches []recordedLaunch
	err      error
}

type recordedLaunch struct {
	TaskID         domain.TaskID
	RunID          domain.RunID
	PolicyRevision uint64
	ProviderKey    string
}

func (launcher *recordingRunLauncher) Launch(
	_ context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	policyRevision uint64,
	providerKey string,
) error {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	launcher.launches = append(launcher.launches, recordedLaunch{
		TaskID: taskID, RunID: runID,
		PolicyRevision: policyRevision, ProviderKey: providerKey,
	})
	return launcher.err
}

func (launcher *recordingRunLauncher) recorded() []recordedLaunch {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	return append([]recordedLaunch(nil), launcher.launches...)
}

func TestTaskRunLauncherRequiresEveryCollaborator(t *testing.T) {
	// Each of these, missing, produces a launcher that cannot start a worker.
	// A nil check that returns a usable value is how the original defect
	// survived: everything looked constructed.
	for name, build := range map[string]func() (*TaskRunLauncher, error){
		"no store": func() (*TaskRunLauncher, error) {
			return NewTaskRunLauncher(nil, nil, nil, nil, "worker", func() string { return "x" }, nil, nil)
		},
		"no worktree service": func() (*TaskRunLauncher, error) {
			return NewTaskRunLauncher(
				stubLauncherStore{}, nil, &WorkerRuntime{}, fixedSequences{}, "worker",
				func() string { return "x" }, nil, nil)
		},
		"no executable": func() (*TaskRunLauncher, error) {
			return NewTaskRunLauncher(
				stubLauncherStore{}, nil, &WorkerRuntime{}, fixedSequences{}, "  ",
				func() string { return "x" }, nil, nil)
		},
		"no endpoint": func() (*TaskRunLauncher, error) {
			return NewTaskRunLauncher(
				stubLauncherStore{}, nil, &WorkerRuntime{}, fixedSequences{}, "worker", nil, nil, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := build(); err == nil {
				t.Fatalf("a launcher with %s was accepted", name)
			}
		})
	}
}

func TestLaunchRefusesWhenTheWorkerExecutableIsAbsent(t *testing.T) {
	// A development checkout hits this constantly, so the message has to name
	// the build rather than read as a transient failure worth retrying.
	launcher := &TaskRunLauncher{
		store:      stubLauncherStore{},
		executable: filepath.Join(t.TempDir(), "codeflux-worker"),
		endpoint:   func() string { return "127.0.0.1:1" },
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	err = launcher.Launch(t.Context(), taskID, runID, 1, "openai")
	if !errors.Is(err, ErrWorkerExecutableMissing) {
		t.Fatalf("a missing worker executable was not reported as such: %v", err)
	}
	if !strings.Contains(err.Error(), "build") {
		t.Errorf("the error does not say how to fix it: %v", err)
	}
}

func TestLaunchRefusesIncompleteIdentity(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "codeflux-worker")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := &TaskRunLauncher{
		store: stubLauncherStore{}, executable: executable,
		endpoint: func() string { return "127.0.0.1:1" },
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.Launch(t.Context(), domain.TaskID{}, runID, 1, "openai"); err == nil {
		t.Error("a launch with no task identity was accepted")
	}
	if err := launcher.Launch(t.Context(), taskID, domain.RunID{}, 1, "openai"); err == nil {
		t.Error("a launch with no run identity was accepted")
	}
	// The provider key becomes the queue's scheduling dimension. An empty one
	// would put every run into a single unnamed provider bucket.
	if err := launcher.Launch(t.Context(), taskID, runID, 1, "  "); err == nil {
		t.Error("a launch with no provider key was accepted")
	}
}

func TestLaunchRefusesWhenTheCoordinatorHasNoAddress(t *testing.T) {
	// The endpoint is what the worker calls back on. Starting a subprocess
	// that cannot reach its coordinator produces a process nobody can stop.
	executable := filepath.Join(t.TempDir(), "codeflux-worker")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := &TaskRunLauncher{
		store: stubLauncherStore{}, executable: executable,
		endpoint: func() string { return "" },
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	err = launcher.Launch(t.Context(), taskID, runID, 1, "openai")
	if err == nil || !strings.Contains(err.Error(), "address") {
		t.Fatalf("a launch with no coordinator address was accepted: %v", err)
	}
}

func TestLeaseIdentifiersAreNotDerivedFromTheRun(t *testing.T) {
	// The lease is what a worker presents to prove it is the process the
	// coordinator started. A value derived from a visible identifier would
	// prove nothing.
	launcher := &TaskRunLauncher{random: func(buffer []byte) (int, error) {
		for index := range buffer {
			buffer[index] = byte(index)
		}
		return len(buffer), nil
	}}
	first, err := launcher.newLeaseID()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "lease-") || len(first) != len("lease-")+32 {
		t.Fatalf("lease %q is not the expected shape", first)
	}

	failing := &TaskRunLauncher{random: func([]byte) (int, error) {
		return 0, errors.New("no entropy")
	}}
	if _, err := failing.newLeaseID(); err == nil {
		t.Error("a lease was minted without entropy")
	}
	short := &TaskRunLauncher{random: func(buffer []byte) (int, error) {
		return len(buffer) - 1, nil
	}}
	if _, err := short.newLeaseID(); err == nil {
		t.Error("a partially filled lease was accepted")
	}
}

func TestAnApplicationWithoutItsOwnWorkersRefusesToStartByName(t *testing.T) {
	// This is the honest alternative to having no launcher at all: pressing
	// start fails and says why, instead of reporting success and doing
	// nothing.
	launcher := unavailableRunLauncher{reason: "this coordinator does not own worker processes"}
	err := launcher.Launch(t.Context(), domain.TaskID{}, domain.RunID{}, 1, "openai")
	if err == nil {
		t.Fatal("an unavailable launcher reported success")
	}
	if !strings.Contains(err.Error(), "does not own worker processes") {
		t.Errorf("the refusal does not explain itself: %v", err)
	}
}

func TestDefaultWorkerExecutableSitsBesideTheCoordinator(t *testing.T) {
	// Never a PATH lookup: the resolved process receives the worktree and the
	// coordinator endpoint, so it must be the one shipped alongside.
	path, err := DefaultWorkerExecutable()
	if err != nil {
		t.Fatalf("resolve the default worker executable: %v", err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Dir(self) {
		t.Errorf("worker executable %q does not sit beside %q", path, self)
	}
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "codeflux-worker") {
		t.Errorf("resolved worker name = %q", name)
	}
	if filepath.Ext(self) == ".exe" && filepath.Ext(path) != ".exe" {
		t.Errorf("the worker path %q lost its executable extension", path)
	}
}

// stubLauncherStore satisfies the launcher's read surface without a database.
//
// Every method fails: these tests stop before any of them is reached, and a
// stub that returned plausible values would let a test pass through a path it
// never meant to exercise.
type stubLauncherStore struct{}

func (stubLauncherStore) GetTask(
	context.Context, domain.TaskID,
) (storage.Task, error) {
	return storage.Task{}, errors.New("the launcher tests do not read tasks")
}

func (stubLauncherStore) GetRepository(
	context.Context, domain.RepositoryID,
) (storage.Repository, error) {
	return storage.Repository{}, errors.New("the launcher tests do not read repositories")
}

func (stubLauncherStore) GetWorktreeBinding(
	context.Context, domain.TaskID,
) (storage.WorktreeBinding, error) {
	return storage.WorktreeBinding{}, errors.New("the launcher tests do not read bindings")
}

// fixedSequences is a predictable enqueue ordering source.
type fixedSequences struct{}

func (fixedSequences) NextEnqueueSequence() uint64 { return 1 }
