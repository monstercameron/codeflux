package coordinator

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/worker"
)

type WorkerLeaseStore interface {
	AcquireWorkerLease(
		context.Context,
		storage.AcquireWorkerLease,
	) (storage.WorkerLease, error)
	FinishWorkerLease(
		context.Context,
		storage.FinishWorkerLease,
	) (storage.WorkerLease, error)
}

type StartWorker struct {
	LeaseID             string
	TaskID              domain.TaskID
	RunID               domain.RunID
	WorktreePath        string
	PolicyRevision      uint64
	ToolSchemaVersion   int
	CoordinatorEndpoint string
	Executable          string
	ExecutableArguments []string
	ContainerCommand    []string
	AdditionalAllowed   []string
	AdditionalSensitive []string
}

type supervisedWorker struct {
	taskID  domain.TaskID
	runID   domain.RunID
	process *worker.Process
	done    chan struct{}
}

// Supervisor owns one subprocess per active run.
type Supervisor struct {
	mu           sync.Mutex
	store        WorkerLeaseStore
	gateway      *WorkerGateway
	random       func([]byte) (int, error)
	active       map[domain.RunID]*supervisedWorker
	grace        time.Duration
	closed       bool
	complete     func(domain.TaskID, domain.RunID) error
	completeErr  error
	lifecycleErr error
}

func (supervisor *Supervisor) SetCompletionObserver(
	observer func(domain.TaskID, domain.RunID) error,
) error {
	if observer == nil {
		return errors.New("worker completion observer is required")
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.closed || len(supervisor.active) != 0 ||
		supervisor.complete != nil {
		return errors.New("worker completion observer cannot be changed")
	}
	supervisor.complete = observer
	return nil
}

func NewSupervisor(
	store WorkerLeaseStore,
	gateway *WorkerGateway,
) (*Supervisor, error) {
	if store == nil || gateway == nil {
		return nil, errors.New("worker lease store and gateway are required")
	}
	return &Supervisor{
		store: store, gateway: gateway, random: rand.Read,
		active: make(map[domain.RunID]*supervisedWorker),
		grace:  worker.GracePeriod,
	}, nil
}

func (supervisor *Supervisor) Start(
	ctx context.Context,
	input StartWorker,
) (storage.WorkerLease, error) {
	if input.LeaseID == "" || input.Executable == "" {
		return storage.WorkerLease{}, errors.New("worker lease and executable are required")
	}
	supervisor.mu.Lock()
	if supervisor.completeErr != nil || supervisor.lifecycleErr != nil {
		err := errors.Join(supervisor.completeErr, supervisor.lifecycleErr)
		supervisor.mu.Unlock()
		return storage.WorkerLease{}, errors.Join(
			errors.New("worker completion bookkeeping failed"), err,
		)
	}
	if supervisor.closed {
		supervisor.mu.Unlock()
		return storage.WorkerLease{}, errors.New("worker supervisor is shutting down")
	}
	if _, exists := supervisor.active[input.RunID]; exists {
		supervisor.mu.Unlock()
		return storage.WorkerLease{}, errors.New("run already has an active worker process")
	}
	supervisor.mu.Unlock()
	tokenBytes := make([]byte, 32)
	count, err := supervisor.random(tokenBytes)
	if err != nil || count != len(tokenBytes) {
		return storage.WorkerLease{}, errors.New("generate worker session token")
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	digest := sha256.Sum256([]byte(token))
	lease, err := supervisor.store.AcquireWorkerLease(
		ctx,
		storage.AcquireWorkerLease{
			ID: input.LeaseID, TaskID: input.TaskID, RunID: input.RunID,
			ProtocolVersion:    worker.ProtocolVersion,
			ToolSchemaVersion:  input.ToolSchemaVersion,
			PolicyRevision:     input.PolicyRevision,
			WorktreePath:       input.WorktreePath,
			Endpoint:           input.CoordinatorEndpoint,
			SessionTokenSHA256: hex.EncodeToString(digest[:]),
		},
	)
	if err != nil {
		return storage.WorkerLease{}, err
	}
	if err := supervisor.gateway.Register(lease, token); err != nil {
		finishErr := supervisor.finishFailedStart(lease)
		return storage.WorkerLease{}, errors.Join(err, finishErr)
	}
	process, err := worker.Launch(context.Background(), worker.LaunchOptions{
		Executable:          input.Executable,
		ExecutableArguments: input.ExecutableArguments,
		Startup: worker.StartupParameters{
			ProtocolVersion: worker.ProtocolVersion,
			TaskID:          input.TaskID, RunID: input.RunID,
			WorktreePath:        input.WorktreePath,
			PolicyRevision:      input.PolicyRevision,
			ToolSchemaVersion:   input.ToolSchemaVersion,
			CoordinatorEndpoint: input.CoordinatorEndpoint,
			SessionToken:        token, ContainerCommand: input.ContainerCommand,
		},
		ParentEnvironment:   os.Environ(),
		AdditionalAllowed:   input.AdditionalAllowed,
		AdditionalSensitive: input.AdditionalSensitive,
		ContainerCommand:    input.ContainerCommand,
	})
	if err != nil {
		supervisor.gateway.Unregister(input.RunID)
		finishErr := supervisor.finishFailedStart(lease)
		return storage.WorkerLease{}, errors.Join(err, finishErr)
	}
	lease, err = supervisor.gateway.RecordProcessStarted(
		ctx, input.RunID, process.PID(),
	)
	if err != nil {
		_ = process.Stop()
		_ = process.Wait(context.Background())
		current := lease
		if snapshot, exists := supervisor.gateway.SnapshotLease(input.RunID); exists {
			current = snapshot
		}
		supervisor.gateway.Unregister(input.RunID)
		finishErr := supervisor.finishFailedStart(current)
		return storage.WorkerLease{}, errors.Join(err, finishErr)
	}
	owned := &supervisedWorker{
		taskID: input.TaskID, runID: input.RunID,
		process: process, done: make(chan struct{}),
	}
	supervisor.mu.Lock()
	if _, exists := supervisor.active[input.RunID]; exists {
		supervisor.mu.Unlock()
		_ = process.Stop()
		_ = process.Wait(context.Background())
		supervisor.gateway.Unregister(input.RunID)
		finishErr := supervisor.finishFailedStart(lease)
		return storage.WorkerLease{}, errors.Join(
			errors.New("run acquired duplicate in-process ownership"),
			finishErr,
		)
	}
	supervisor.active[input.RunID] = owned
	supervisor.mu.Unlock()
	go supervisor.reap(owned, lease)
	return lease, nil
}

func (supervisor *Supervisor) finishFailedStart(
	lease storage.WorkerLease,
) error {
	_, err := supervisor.store.FinishWorkerLease(
		context.Background(),
		storage.FinishWorkerLease{
			ID: lease.ID, ExpectedRevision: lease.Revision,
			State: storage.WorkerLeaseCrashed,
		},
	)
	return err
}

func (supervisor *Supervisor) reap(
	owned *supervisedWorker,
	initial storage.WorkerLease,
) {
	err := owned.process.Wait(context.Background())
	state := storage.WorkerLeaseExited
	exitCode := 0
	if err != nil {
		state = storage.WorkerLeaseCrashed
		exitCode = -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
	}
	lease := initial
	if current, exists := supervisor.gateway.SnapshotLease(owned.runID); exists {
		lease = current
	}
	var finishErr error
	for attempt := 0; attempt < 4; attempt++ {
		if current, exists := supervisor.gateway.SnapshotLease(owned.runID); exists {
			lease = current
		}
		_, finishErr = supervisor.store.FinishWorkerLease(
			context.Background(),
			storage.FinishWorkerLease{
				ID: lease.ID, ExpectedRevision: lease.Revision,
				State: state, ExitCode: &exitCode,
			},
		)
		if finishErr == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if errors.Is(finishErr, storage.ErrConflict) {
		finishErr = nil
	}
	supervisor.gateway.Unregister(owned.runID)
	supervisor.mu.Lock()
	delete(supervisor.active, owned.runID)
	complete := supervisor.complete
	if finishErr != nil {
		supervisor.lifecycleErr = errors.Join(supervisor.lifecycleErr, finishErr)
	}
	supervisor.mu.Unlock()
	if complete != nil {
		if err := complete(owned.taskID, owned.runID); err != nil {
			supervisor.mu.Lock()
			supervisor.completeErr = errors.Join(supervisor.completeErr, err)
			supervisor.mu.Unlock()
		}
	}
	close(owned.done)
}

func (supervisor *Supervisor) QueueControl(
	runID domain.RunID,
	control worker.Control,
) error {
	supervisor.mu.Lock()
	_, exists := supervisor.active[runID]
	supervisor.mu.Unlock()
	if !exists {
		return errors.New("worker process is not active")
	}
	return supervisor.gateway.QueueControl(runID, control)
}

func (supervisor *Supervisor) CheckpointAndStopAll(ctx context.Context) error {
	supervisor.mu.Lock()
	supervisor.closed = true
	active := make([]*supervisedWorker, 0, len(supervisor.active))
	for _, owned := range supervisor.active {
		active = append(active, owned)
	}
	supervisor.mu.Unlock()
	for _, owned := range active {
		_ = supervisor.gateway.QueueControl(owned.runID, worker.Control{
			Kind: worker.ControlCheckpoint, Reason: "coordinator shutdown",
		})
		_ = supervisor.gateway.QueueControl(owned.runID, worker.Control{
			Kind: worker.ControlShutdown, Reason: "coordinator shutdown",
		})
	}
	grace := supervisor.grace
	if grace <= 0 {
		grace = worker.GracePeriod
	}
	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	var stopErr error
	for _, owned := range active {
		select {
		case <-owned.done:
		case <-ctx.Done():
			for _, remaining := range active {
				stopErr = errors.Join(stopErr, remaining.process.Stop())
			}
			return errors.Join(ctx.Err(), stopErr)
		case <-graceTimer.C:
			for _, remaining := range active {
				stopErr = errors.Join(stopErr, remaining.process.Stop())
			}
			forcedTimer := time.NewTimer(grace)
			for _, remaining := range active {
				select {
				case <-remaining.done:
				case <-ctx.Done():
					forcedTimer.Stop()
					return errors.Join(ctx.Err(), stopErr)
				case <-forcedTimer.C:
					return errors.Join(
						stopErr,
						errors.New("worker process did not exit after forced termination"),
					)
				}
			}
			forcedTimer.Stop()
			return stopErr
		}
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return errors.Join(supervisor.completeErr, supervisor.lifecycleErr)
}

func (supervisor *Supervisor) ActiveCount() int {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	return len(supervisor.active)
}
