package coordinator

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/worker"
)

const supervisorHelperMode = "CODEFLUX_SUPERVISOR_HELPER"

func TestSupervisorWorkerHelper(t *testing.T) {
	mode := os.Getenv(supervisorHelperMode)
	if mode == "" {
		return
	}
	startup, err := worker.DecodeStartup(os.Stdin)
	if err != nil {
		os.Exit(11)
	}
	if mode == "crash" {
		os.Exit(12)
	}
	if mode == "unresponsive" {
		time.Sleep(time.Hour)
	}
	err = worker.Run(context.Background(), startup, worker.ClientOptions{
		HeartbeatInterval: 100 * time.Millisecond,
		Checkpointer:      supervisorCheckpointer{},
	})
	if err != nil {
		os.Exit(13)
	}
}

func TestSupervisorOwnsOneProcessAndGracefullyStops(t *testing.T) {
	store := newSupervisorStore()
	gateway, err := NewWorkerGateway(store)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	supervisor, err := NewSupervisor(store, gateway)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(supervisorHelperMode, "run")
	input := supervisorStartFixture(t, server.URL)
	if _, err := supervisor.Start(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context(), input); err == nil {
		t.Fatal("duplicate in-process worker ownership was accepted")
	}
	waitFor(t, func() bool { return store.heartbeatCount() > 0 })
	if err := supervisor.QueueControl(input.RunID, worker.Control{
		Kind: worker.ControlPause, Reason: "test pause",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return store.finishedState(input.RunID) == storage.WorkerLeasePaused
	})
	store.failNextFinish()
	if err := supervisor.CheckpointAndStopAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return supervisor.ActiveCount() == 0 })
	if state := store.finishedState(input.RunID); state != storage.WorkerLeaseExited {
		t.Fatalf("finished worker state = %s", state)
	}
	if checkpoint := store.lastCheckpoint(input.RunID); checkpoint != "checkpoint-supervisor" {
		t.Fatalf("last checkpoint = %q", checkpoint)
	}
	if _, err := supervisor.Start(
		t.Context(), supervisorStartFixture(t, server.URL),
	); err == nil {
		t.Fatal("worker start was accepted after supervisor shutdown")
	}
}

func TestSupervisorRecordsWorkerCrash(t *testing.T) {
	store := newSupervisorStore()
	gateway, _ := NewWorkerGateway(store)
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	supervisor, _ := NewSupervisor(store, gateway)
	t.Setenv(supervisorHelperMode, "crash")
	input := supervisorStartFixture(t, server.URL)
	if _, err := supervisor.Start(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return supervisor.ActiveCount() == 0 })
	if state := store.finishedState(input.RunID); state != storage.WorkerLeaseCrashed {
		t.Fatalf("crashed worker state = %s", state)
	}
	if exitCode := store.exitCode(input.RunID); exitCode != 12 {
		t.Fatalf("crashed worker exit code = %d", exitCode)
	}
}

func TestWorkerCrashCannotAffectAnotherTaskWorktree(t *testing.T) {
	store := newSupervisorStore()
	gateway, _ := NewWorkerGateway(store)
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	supervisor, _ := NewSupervisor(store, gateway)

	crashTree := t.TempDir()
	healthyTree := t.TempDir()
	healthySentinel := filepath.Join(healthyTree, "owned-by-healthy-task")
	if err := os.WriteFile(healthySentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(supervisorHelperMode, "crash")
	crashed := supervisorStartFixture(t, server.URL)
	crashed.WorktreePath = crashTree
	if _, err := supervisor.Start(t.Context(), crashed); err != nil {
		t.Fatal(err)
	}
	t.Setenv(supervisorHelperMode, "run")
	healthy := supervisorStartFixture(t, server.URL)
	healthy.WorktreePath = healthyTree
	if _, err := supervisor.Start(t.Context(), healthy); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return store.finishedState(crashed.RunID) == storage.WorkerLeaseCrashed &&
			store.heartbeatCountFor(healthy.RunID) > 0
	})
	content, err := os.ReadFile(healthySentinel)
	if err != nil || string(content) != "unchanged" {
		t.Fatalf("healthy task worktree changed: %q, %v", content, err)
	}
	if err := supervisor.QueueControl(healthy.RunID, worker.Control{
		Kind: worker.ControlShutdown, Reason: "test complete",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return supervisor.ActiveCount() == 0 })
}

func TestSupervisorForcesUnresponsiveWorkerToStopAfterGrace(t *testing.T) {
	store := newSupervisorStore()
	gateway, _ := NewWorkerGateway(store)
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	supervisor, _ := NewSupervisor(store, gateway)
	supervisor.grace = 100 * time.Millisecond
	t.Setenv(supervisorHelperMode, "unresponsive")
	input := supervisorStartFixture(t, server.URL)
	if _, err := supervisor.Start(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := supervisor.CheckpointAndStopAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("forced shutdown took %s", elapsed)
	}
	waitFor(t, func() bool { return supervisor.ActiveCount() == 0 })
}

func supervisorStartFixture(t *testing.T, endpoint string) StartWorker {
	t.Helper()
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	return StartWorker{
		LeaseID: "lease-" + runID.String(), TaskID: taskID, RunID: runID,
		WorktreePath: t.TempDir(), ToolSchemaVersion: 1,
		CoordinatorEndpoint: endpoint, Executable: os.Args[0],
		ExecutableArguments: []string{"-test.run=^TestSupervisorWorkerHelper$"},
		AdditionalAllowed:   []string{supervisorHelperMode},
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type supervisorCheckpointer struct{}

func (supervisorCheckpointer) Checkpoint(context.Context) (string, error) {
	return "checkpoint-supervisor", nil
}

type supervisorStore struct {
	mu             sync.Mutex
	leases         map[domain.RunID]storage.WorkerLease
	heartbeats     int
	byRun          map[domain.RunID]int
	finishFailures int
}

func newSupervisorStore() *supervisorStore {
	return &supervisorStore{
		leases: make(map[domain.RunID]storage.WorkerLease),
		byRun:  make(map[domain.RunID]int),
	}
}

func (store *supervisorStore) AcquireWorkerLease(
	_ context.Context,
	input storage.AcquireWorkerLease,
) (storage.WorkerLease, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.leases[input.RunID]; exists {
		return storage.WorkerLease{}, errors.New("duplicate")
	}
	lease := storage.WorkerLease{
		ID: input.ID, TaskID: input.TaskID, RunID: input.RunID,
		State:             storage.WorkerLeaseStarting,
		ProtocolVersion:   input.ProtocolVersion,
		ToolSchemaVersion: input.ToolSchemaVersion,
		PolicyRevision:    input.PolicyRevision, WorktreePath: input.WorktreePath,
		Endpoint:           input.Endpoint,
		SessionTokenSHA256: input.SessionTokenSHA256,
		StartedAt:          time.Now(), UpdatedAt: time.Now(),
	}
	store.leases[input.RunID] = lease
	return lease, nil
}

func (store *supervisorStore) RecordWorkerHeartbeat(
	_ context.Context,
	input storage.RecordWorkerHeartbeat,
) (storage.WorkerLease, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for runID, lease := range store.leases {
		if lease.ID != input.ID {
			continue
		}
		lease.State = input.State
		lease.ProcessID = &input.ProcessID
		lease.LastSequence = input.Sequence
		lease.LastHeartbeatAt = &input.ObservedAt
		lease.LastCheckpointID = input.CheckpointID
		lease.Revision++
		store.leases[runID] = lease
		store.heartbeats++
		store.byRun[runID]++
		return lease, nil
	}
	return storage.WorkerLease{}, errors.New("missing lease")
}

func (store *supervisorStore) RecordWorkerProcessStarted(
	_ context.Context,
	input storage.RecordWorkerProcessStarted,
) (storage.WorkerLease, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for runID, lease := range store.leases {
		if lease.ID != input.ID {
			continue
		}
		lease.ProcessID = &input.ProcessID
		lease.Revision++
		store.leases[runID] = lease
		return lease, nil
	}
	return storage.WorkerLease{}, errors.New("missing lease")
}

func (store *supervisorStore) heartbeatCountFor(runID domain.RunID) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.byRun[runID]
}

func (store *supervisorStore) RecordWorkerReport(
	_ context.Context,
	input storage.RecordWorkerReport,
) (storage.WorkerLease, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for runID, lease := range store.leases {
		if lease.ID != input.ID {
			continue
		}
		lease.LastSequence = input.Sequence
		lease.Revision++
		store.leases[runID] = lease
		return lease, nil
	}
	return storage.WorkerLease{}, errors.New("missing lease")
}

func (store *supervisorStore) FinishWorkerLease(
	_ context.Context,
	input storage.FinishWorkerLease,
) (storage.WorkerLease, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.finishFailures > 0 {
		store.finishFailures--
		return storage.WorkerLease{}, errors.New("injected stale finish")
	}
	for runID, lease := range store.leases {
		if lease.ID != input.ID {
			continue
		}
		lease.State = input.State
		lease.ExitCode = input.ExitCode
		lease.Revision++
		store.leases[runID] = lease
		return lease, nil
	}
	return storage.WorkerLease{}, errors.New("missing lease")
}

func (store *supervisorStore) failNextFinish() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.finishFailures++
}

func (store *supervisorStore) heartbeatCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.heartbeats
}

func (store *supervisorStore) finishedState(runID domain.RunID) storage.WorkerLeaseState {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.leases[runID].State
}

func (store *supervisorStore) lastCheckpoint(runID domain.RunID) string {
	store.mu.Lock()
	defer store.mu.Unlock()
	checkpoint := store.leases[runID].LastCheckpointID
	if checkpoint == nil {
		return ""
	}
	return *checkpoint
}

func (store *supervisorStore) exitCode(runID domain.RunID) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	exitCode := store.leases[runID].ExitCode
	if exitCode == nil {
		return 0
	}
	return *exitCode
}
