package coordinator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
)

// ToolSchemaVersion is the tool contract a worker is started against.
//
// It is passed to the subprocess rather than read there, so a coordinator and
// a worker built from different revisions refuse each other at startup instead
// of disagreeing about a tool's shape halfway through a run.
const ToolSchemaVersion = 1

// TaskRunLauncher turns a started run into a running worker subprocess.
//
// Before this existed, starting a task wrote a run row and returned. The task
// showed as running, no subprocess was ever spawned, and no provider request
// was made: the parts of the agent loop were all present and none of them were
// connected. This is the connection.
type TaskRunLauncher struct {
	store      taskRunLauncherStore
	worktrees  *gitwork.Service
	runtime    *WorkerRuntime
	sequences  enqueueSequenceSource
	executable string
	endpoint   func() string
	random     func([]byte) (int, error)
	// containerCommand is the operator-authorized command every worker this
	// launcher starts is launched inside, or nil for the native path
	// (M11-033, AUDIT-017). It was validated once, at construction, by
	// validateContainerCommand.
	containerCommand []string
}

// taskRunLauncherStore is the narrow read surface launching a run needs.
type taskRunLauncherStore interface {
	GetTask(context.Context, domain.TaskID) (storage.Task, error)
	GetRepository(context.Context, domain.RepositoryID) (storage.Repository, error)
	GetWorktreeBinding(context.Context, domain.TaskID) (storage.WorktreeBinding, error)
}

// NewTaskRunLauncher builds the launcher.
//
// endpoint is a function rather than a value because the coordinator binds its
// listener after this is constructed; capturing the address early would hand
// every worker an empty endpoint.
func NewTaskRunLauncher(
	store taskRunLauncherStore,
	worktrees *gitwork.Service,
	runtime *WorkerRuntime,
	sequences enqueueSequenceSource,
	executable string,
	endpoint func() string,
	random func([]byte) (int, error),
	containerCommand []string,
) (*TaskRunLauncher, error) {
	switch {
	case store == nil:
		return nil, errors.New("task run launcher store is required")
	case worktrees == nil:
		return nil, errors.New("worktree service is required")
	case runtime == nil:
		return nil, errors.New("worker runtime is required")
	case sequences == nil:
		return nil, errors.New("an enqueue sequence source is required")
	case strings.TrimSpace(executable) == "":
		return nil, errors.New("worker executable is required")
	case endpoint == nil:
		return nil, errors.New("coordinator endpoint accessor is required")
	}
	if random == nil {
		random = rand.Read
	}
	if err := validateContainerCommand(containerCommand); err != nil {
		return nil, fmt.Errorf("container command: %w", err)
	}
	return &TaskRunLauncher{
		store: store, worktrees: worktrees, runtime: runtime, sequences: sequences,
		executable: executable, endpoint: endpoint, random: random,
		containerCommand: containerCommand,
	}, nil
}

// enqueueSequenceSource supplies the queue ordering number for a run.
//
// It is an interface so the launcher cannot reach into scheduler state for
// anything else, and so a test can supply a predictable sequence.
type enqueueSequenceSource interface {
	NextEnqueueSequence() uint64
}

// ErrWorkerExecutableMissing reports that the subprocess entry point is not
// present beside the coordinator.
//
// It is distinguished from an ordinary failure because the remedy is a build,
// not a retry, and because a development checkout hits it constantly.
var ErrWorkerExecutableMissing = errors.New("the worker executable is not available")

// Launch starts the worker that will execute one prepared run.
//
// The order matters. The worktree is created before the run is queued, so a
// queued run always has a workspace; the run is queued before the subprocess
// starts, so a crash between the two leaves a durable row the recovery path
// can see rather than an orphan process nobody recorded.
func (launcher *TaskRunLauncher) Launch(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	policyRevision uint64,
	providerKey string,
) error {
	if strings.TrimSpace(providerKey) == "" {
		return errors.New("a provider key is required to queue a run")
	}
	if taskID.IsZero() || runID.IsZero() {
		return errors.New("task and run IDs are required to launch a worker")
	}
	if _, err := os.Stat(launcher.executable); err != nil {
		return fmt.Errorf("%w at %s: build it with `codeflux-dev build`",
			ErrWorkerExecutableMissing, launcher.executable)
	}
	// The endpoint is checked before the worktree is created. It is the
	// address the worker calls back on, so without it the subprocess could not
	// be stopped, and creating a worktree first would leave a directory and a
	// branch behind for a launch that was never going to proceed.
	endpoint, err := launcher.coordinatorEndpoint()
	if err != nil {
		return err
	}
	worktreePath, resolveErr := launcher.resolveWorktree(ctx, taskID)
	if resolveErr != nil {
		return resolveErr
	}
	leaseID, err := launcher.newLeaseID()
	if err != nil {
		return err
	}
	start := StartWorker{
		LeaseID:             leaseID,
		TaskID:              taskID,
		RunID:               runID,
		WorktreePath:        worktreePath,
		PolicyRevision:      policyRevision,
		ToolSchemaVersion:   ToolSchemaVersion,
		CoordinatorEndpoint: endpoint,
		Executable:          launcher.executable,
		ContainerCommand:    launcher.containerCommand,
	}
	// The queue row is keyed by the run rather than the task: a retried start
	// mints a new run, and keying by task would make the second attempt look
	// like a duplicate of the first.
	if _, err := launcher.runtime.Submit(ctx, storage.EnqueueTask{
		ID:              "run-" + runID.String(),
		TaskID:          taskID,
		ProviderKey:     providerKey,
		Reason:          "waiting for worker capacity",
		Priority:        1,
		EnqueueSequence: launcher.sequences.NextEnqueueSequence(),
	}, start); err != nil {
		return fmt.Errorf("queue the run for execution: %w", err)
	}
	// StartNext respects the durable concurrency limit, so a run beyond the
	// limit stays queued rather than being refused. Reporting "not started"
	// as an error here would turn a working queue into a visible failure.
	if _, _, err := launcher.runtime.StartNext(ctx); err != nil {
		return fmt.Errorf("start the queued run: %w", err)
	}
	return nil
}

// coordinatorEndpoint renders the address the worker calls back on.
//
// The worker's own validation requires a bare http loopback origin: no path,
// no query, no credentials. Building it here rather than passing the raw
// host:port means a mismatch fails at the coordinator, where the message can
// name the address, instead of inside a subprocess that exits with a code.
func (launcher *TaskRunLauncher) coordinatorEndpoint() (string, error) {
	address := strings.TrimSpace(launcher.endpoint())
	if address == "" {
		return "", errors.New("the coordinator has no address to give the worker")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("the coordinator address %q is not host:port", address)
	}
	parsed := net.ParseIP(strings.Trim(host, "[]"))
	if parsed == nil || !parsed.IsLoopback() {
		return "", fmt.Errorf(
			"the coordinator address %q is not loopback; a worker must not be handed a "+
				"routable coordinator", address)
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// resolveWorktree returns the workspace this run will edit, creating it when
// the task does not have one yet.
//
// An existing binding is reused rather than replaced. A second worktree for
// the same task would split its edits across two directories, and only one of
// them would ever be reviewed.
func (launcher *TaskRunLauncher) resolveWorktree(
	ctx context.Context,
	taskID domain.TaskID,
) (string, error) {
	binding, err := launcher.store.GetWorktreeBinding(ctx, taskID)
	if err == nil && strings.TrimSpace(binding.WorktreePath) != "" {
		return binding.WorktreePath, nil
	}
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return "", fmt.Errorf("read the task worktree binding: %w", err)
	}

	task, err := launcher.store.GetTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("read the task: %w", err)
	}
	repository, err := launcher.store.GetRepository(ctx, task.RepositoryID)
	if err != nil {
		return "", fmt.Errorf("read the task repository: %w", err)
	}
	base, err := launcher.worktrees.ResolveRevision(ctx, repository.CanonicalPath, "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve the repository head: %w", err)
	}
	created, err := launcher.worktrees.CreateTaskWorktree(ctx, gitwork.CreateWorktreeInput{
		TaskID:         taskID,
		RepositoryID:   repository.ID,
		RepositoryPath: repository.CanonicalPath,
		BaseRevision:   base,
	})
	if err != nil {
		return "", fmt.Errorf("create the task worktree: %w", err)
	}
	return created.Binding.WorktreePath, nil
}

// newLeaseID mints the lease that binds this worker to this run.
//
// It is random rather than derived from the run, because the lease is what a
// worker presents to prove it is the process the coordinator started. A value
// anybody could compute from a visible identifier would not prove that.
func (launcher *TaskRunLauncher) newLeaseID() (string, error) {
	raw := make([]byte, 16)
	count, err := launcher.random(raw)
	if err != nil || count != len(raw) {
		return "", errors.New("generate a worker lease identifier")
	}
	return "lease-" + hex.EncodeToString(raw), nil
}

// DefaultWorkerExecutable is the path a coordinator uses for the worker.
//
// It is resolved beside the running executable rather than through PATH: a
// coordinator must not start whatever `codeflux-worker` a user happens to have
// earlier in their PATH, because that process receives the worktree and the
// coordinator endpoint.
func DefaultWorkerExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the running executable: %w", err)
	}
	directory := filepath.Dir(self)
	name := "codeflux-worker"
	if filepath.Ext(self) == ".exe" {
		name += ".exe"
	}
	return filepath.Join(directory, name), nil
}
