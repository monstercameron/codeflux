package coordinator

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofrs/flock"
	"google.golang.org/grpc"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"codeflux.dev/codeflux/internal/worker"
	"codeflux.dev/codeflux/internal/workspace"
	"codeflux.dev/codeflux/migrations"
)

const defaultHeartbeatTimeout = 15 * time.Second

// WorkerController owns checkpoint-and-stop requests during shutdown.
type WorkerController interface {
	CheckpointAndStopAll(context.Context) error
}

type ApplicationOptions struct {
	DatabasePath        string
	BackupDirectory     string
	ListenAddress       string
	WorkerLimit         int
	ProviderLimits      map[string]int
	HeartbeatTimeout    time.Duration
	Now                 func() time.Time
	Random              func([]byte) (int, error)
	Workers             WorkerController
	ShutdownCheckpoints GracefulShutdownCheckpointer
	TaskControls        transport.TaskControlApplication
	TaskListenAddress   string
}

type OrphanedWorkerCandidate struct {
	Recovery           storage.RecoveryCandidate
	Alive              bool
	Unknown            bool
	IdentityUnverified bool
}

// Application is the long-lived local coordinator dependency root.
type Application struct {
	lock                *flock.Flock
	database            *storage.Database
	repos               *storage.Repositories
	credentials         credentials.Store
	providers           ProviderDependencies
	workspace           WorkspaceDependencies
	preflight           *TaskPreflightService
	events              *events.Hub
	transport           *transport.Boundary
	scheduler           *DurableScheduler
	runtime             *WorkerRuntime
	gateway             *WorkerGateway
	supervisor          *Supervisor
	listener            net.Listener
	server              *http.Server
	taskListener        net.Listener
	taskServer          *grpc.Server
	taskServeDone       chan error
	workers             WorkerController
	secret              string
	recovery            []storage.RecoveryCandidate
	recoveryMu          sync.Mutex
	worktrees           []storage.WorktreeRecoveryCandidate
	taskRuns            []storage.UnownedTaskRunRecoveryCandidate
	recoveryAssessments []storage.RecoveryAssessmentRecord
	recoveryDecisions   *RecoveryDecisionService
	orphans             []OrphanedWorkerCandidate
	heartbeat           time.Duration
	now                 func() time.Time
	monitorStop         context.CancelFunc
	monitorDone         chan struct{}
	accepting           atomic.Bool
	serveDone           chan error
	closeOnce           sync.Once
	closeErr            error
	checkpointClose     func() error
}

func StartApplication(
	ctx context.Context,
	options ApplicationOptions,
) (*Application, error) {
	if options.DatabasePath == "" || !filepath.IsAbs(options.DatabasePath) {
		return nil, errors.New("absolute database path is required")
	}
	if options.BackupDirectory == "" || !filepath.IsAbs(options.BackupDirectory) {
		return nil, errors.New("absolute migration backup directory is required")
	}
	if options.ListenAddress == "" {
		options.ListenAddress = "127.0.0.1:0"
	}
	if options.WorkerLimit == 0 {
		options.WorkerLimit = 2
	}
	if options.HeartbeatTimeout == 0 {
		options.HeartbeatTimeout = defaultHeartbeatTimeout
	}
	if options.HeartbeatTimeout < time.Second || options.HeartbeatTimeout > time.Minute {
		return nil, errors.New("heartbeat timeout is outside supported bounds")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Read
	}
	instanceLock := flock.New(
		options.DatabasePath+".instance.lock",
		flock.SetPermissions(0o600),
	)
	locked, err := instanceLock.TryLock()
	if err != nil || !locked {
		_ = instanceLock.Close()
		return nil, errors.New("another Codeflux coordinator owns this database")
	}
	cleanupLock := true
	defer func() {
		if cleanupLock {
			_ = instanceLock.Close()
		}
	}()
	database, err := storage.Open(ctx, storage.OpenOptions{Path: options.DatabasePath})
	if err != nil {
		return nil, err
	}
	cleanupDatabase := true
	defer func() {
		if cleanupDatabase {
			_ = database.Close(context.Background())
		}
	}()
	sources, err := migrations.Sources()
	if err != nil {
		return nil, err
	}
	if _, err := database.Migrate(ctx, storage.MigrationOptions{
		ApplicationVersion: buildinfo.Current().Version,
		BackupDirectory:    options.BackupDirectory,
		Sources:            sources, Now: options.Now,
	}); err != nil {
		return nil, err
	}
	repositories, err := storage.NewRepositories(database, options.Now)
	if err != nil {
		return nil, err
	}
	taskPreflight, err := NewTaskPreflightService(repositories)
	if err != nil {
		return nil, err
	}
	credentialStore := credentials.NewPlatformStore()
	providerDependencies, err := newProviderDependencies(
		credentialStore, repositories,
	)
	if err != nil {
		return nil, err
	}
	workspaceDependencies, err := newWorkspaceDependencies(repositories)
	if err != nil {
		return nil, err
	}
	memoryScheduler, err := NewScheduler(options.WorkerLimit, options.ProviderLimits)
	if err != nil {
		return nil, err
	}
	scheduler, err := NewDurableScheduler(ctx, memoryScheduler, repositories)
	if err != nil {
		return nil, err
	}
	listener, err := listenLoopback(options.ListenAddress)
	if err != nil {
		return nil, err
	}
	cleanupListener := true
	defer func() {
		if cleanupListener {
			_ = listener.Close()
		}
	}()
	secretBytes := make([]byte, 32)
	count, err := options.Random(secretBytes)
	if err != nil || count != len(secretBytes) {
		return nil, errors.New("generate browser session secret")
	}
	application := &Application{
		lock: instanceLock, database: database, repos: repositories,
		credentials: credentialStore, providers: providerDependencies,
		workspace: workspaceDependencies, preflight: taskPreflight,
		scheduler: scheduler,
		listener:  listener, workers: options.Workers,
		secret:    base64.RawURLEncoding.EncodeToString(secretBytes),
		serveDone: make(chan error, 1),
		heartbeat: options.HeartbeatTimeout, now: options.Now,
		monitorDone: make(chan struct{}),
	}
	cleanupTaskListener := true
	cleanupCheckpointing := true
	defer func() {
		if cleanupTaskListener && application.taskListener != nil {
			_ = application.taskListener.Close()
		}
		if cleanupCheckpointing && application.checkpointClose != nil {
			_ = application.checkpointClose()
		}
	}()
	application.gateway, err = NewWorkerGateway(repositories)
	if err != nil {
		return nil, err
	}
	application.events, err = events.NewHub(repositories, 256)
	if err != nil {
		return nil, err
	}
	application.transport, err = transport.NewBoundary(transport.BoundaryOptions{
		SessionToken: application.secret,
		Now:          options.Now,
	})
	if err != nil {
		return nil, err
	}
	checkpointing, err := newApplicationCheckpointing(
		options.DatabasePath,
		repositories,
	)
	if err != nil {
		return nil, err
	}
	application.checkpointClose = checkpointing.close
	taskControls := options.TaskControls
	if application.workers == nil {
		application.supervisor, err = NewSupervisor(repositories, application.gateway)
		if err != nil {
			return nil, err
		}
		application.runtime, err = NewWorkerRuntime(
			application.scheduler, application.supervisor,
		)
		if err != nil {
			return nil, err
		}
		if err := application.supervisor.SetCompletionObserver(
			application.runtime.Complete,
		); err != nil {
			return nil, err
		}
		shutdownCheckpoints := options.ShutdownCheckpoints
		if shutdownCheckpoints == nil {
			shutdownCheckpoints = checkpointing.checkpoints
		}
		if err := application.supervisor.
			SetGracefulShutdownCheckpointer(
				shutdownCheckpoints,
			); err != nil {
			return nil, err
		}
		if taskControls == nil {
			taskControls, err = newApplicationTaskControls(
				repositories,
				application.supervisor,
				checkpointing,
			)
			if err != nil {
				return nil, err
			}
		}
		application.workers = application.supervisor
	}
	if taskControls == nil {
		return nil, errors.New(
			"task control service is required with a custom worker controller",
		)
	}
	taskAddress := options.TaskListenAddress
	if taskAddress == "" {
		taskAddress = "127.0.0.1:0"
	}
	application.taskListener, err = listenLoopback(taskAddress)
	if err != nil {
		return nil, err
	}
	taskService, err := transport.NewTaskService(taskControls)
	if err != nil {
		return nil, err
	}
	application.taskServer = grpc.NewServer(
		grpc.UnaryInterceptor(
			application.transport.UnaryInterceptor(),
		),
		grpc.StreamInterceptor(
			application.transport.StreamInterceptor(),
		),
	)
	codefluxv1.RegisterTaskServiceServer(
		application.taskServer,
		taskService,
	)
	application.taskServeDone = make(chan error, 1)
	go func() {
		application.taskServeDone <- application.taskServer.Serve(
			application.taskListener,
		)
	}()
	application.recovery, err = repositories.AbandonActiveWorkerLeasesAfterRestart(ctx)
	if err != nil {
		return nil, err
	}
	for _, candidate := range application.recovery {
		if candidate.Lease.ProcessID == nil {
			continue
		}
		alive, probeErr := worker.ProcessAlive(*candidate.Lease.ProcessID)
		if probeErr != nil || alive {
			application.orphans = append(
				application.orphans,
				OrphanedWorkerCandidate{
					Recovery: candidate, Alive: alive, Unknown: probeErr != nil,
					IdentityUnverified: true,
				},
			)
		}
	}
	application.worktrees, err = RecoverInvalidWorktrees(
		ctx, repositories, gitwork.ExecRunner{},
	)
	if err != nil {
		return nil, err
	}
	application.taskRuns, err = RecoverUnownedTaskRuns(ctx, repositories)
	if err != nil {
		return nil, err
	}
	compatibility, err := NewDurableRecoveryCompatibilitySource(repositories)
	if err != nil {
		return nil, err
	}
	actions, err := NewDurableRecoveryActionSource(repositories)
	if err != nil {
		return nil, err
	}
	observations, err := NewDurableRecoveryObservationSource(
		repositories,
		checkpointing.worktrees,
		compatibility,
		actions,
		workspace.ExecRunner{},
	)
	if err != nil {
		return nil, err
	}
	patches, err := NewDurableRecoveryPatchLocator(
		repositories,
		workspace.ExecRunner{},
	)
	if err != nil {
		return nil, err
	}
	assessments, err := NewRecoveryAssessmentService(
		repositories,
		observations,
		patches,
	)
	if err != nil {
		return nil, err
	}
	application.recoveryAssessments, err =
		assessments.AssessIncompleteTaskRuns(
			ctx,
			maximumRecoveryCandidates,
		)
	if err != nil {
		return nil, err
	}
	application.recoveryDecisions, err = NewRecoveryDecisionService(
		repositories,
		checkpointing.worktrees,
	)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", application.health)
	mux.Handle(
		"POST /internal/worker/heartbeat",
		application.gateway.Handler(),
	)
	application.server = &http.Server{
		Handler: mux, ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout: 30 * time.Second,
	}
	application.accepting.Store(true)
	go func() {
		err := application.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		application.serveDone <- err
	}()
	monitorContext, stopMonitor := context.WithCancel(context.Background())
	application.monitorStop = stopMonitor
	go application.monitorWorkerHeartbeats(monitorContext)
	cleanupLock = false
	cleanupDatabase = false
	cleanupListener = false
	cleanupTaskListener = false
	cleanupCheckpointing = false
	return application, nil
}

func (application *Application) WorkerGateway() *WorkerGateway {
	return application.gateway
}

func (application *Application) EventHub() *events.Hub {
	return application.events
}

func (application *Application) ProviderDependencies() ProviderDependencies {
	return application.providers
}

func (application *Application) WorkspaceDependencies() WorkspaceDependencies {
	return application.workspace
}

// TaskPreflightService exposes the fixed-policy preparation, start, and
// terminal outcome lifecycle to product handlers.
func (application *Application) TaskPreflightService() *TaskPreflightService {
	return application.preflight
}

func (application *Application) TransportBoundary() *transport.Boundary {
	return application.transport
}

func (application *Application) WorkerRuntime() *WorkerRuntime {
	return application.runtime
}

func (application *Application) SubmitWorker(
	ctx context.Context,
	queue storage.EnqueueTask,
	start StartWorker,
) (QueuePosition, error) {
	if !application.accepting.Load() {
		return QueuePosition{}, errors.New("coordinator is not accepting mutations")
	}
	if application.runtime == nil {
		return QueuePosition{}, errors.New("default worker runtime is unavailable")
	}
	return application.runtime.Submit(ctx, queue, start)
}

func (application *Application) StartNextWorker(
	ctx context.Context,
) (storage.WorkerLease, bool, error) {
	if !application.accepting.Load() {
		return storage.WorkerLease{}, false,
			errors.New("coordinator is not accepting mutations")
	}
	if application.runtime == nil {
		return storage.WorkerLease{}, false,
			errors.New("default worker runtime is unavailable")
	}
	return application.runtime.StartNext(ctx)
}

func (application *Application) QueueWorkerControl(
	runID domain.RunID,
	control worker.Control,
) error {
	if !application.accepting.Load() {
		return errors.New("coordinator is not accepting mutations")
	}
	if application.supervisor == nil {
		return errors.New("default worker supervisor is unavailable")
	}
	return application.supervisor.QueueControl(runID, control)
}

func (application *Application) WorkerQueuePosition(
	taskID domain.TaskID,
) (QueuePosition, error) {
	if taskID.IsZero() || application.runtime == nil {
		return QueuePosition{}, errors.New("worker queue position is unavailable")
	}
	position := application.runtime.Position(taskID)
	if position.Position == 0 {
		return QueuePosition{}, errors.New("task is not queued")
	}
	return position, nil
}

func (application *Application) health(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if !application.accepting.Load() {
		http.Error(writer, "shutting down", http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(
		`{"status":"ok","isolation":"` + worker.DefaultIsolationLabel + `"}`,
	))
}

func (application *Application) Address() string {
	return application.listener.Addr().String()
}

func (application *Application) BrowserSessionSecret() string {
	return application.secret
}

func (application *Application) TaskControlAddress() string {
	if application.taskListener == nil {
		return ""
	}
	return application.taskListener.Addr().String()
}

func (application *Application) RecoveryCandidates() []storage.RecoveryCandidate {
	application.recoveryMu.Lock()
	defer application.recoveryMu.Unlock()
	return append([]storage.RecoveryCandidate(nil), application.recovery...)
}

func (application *Application) WorktreeRecoveryCandidates() []storage.WorktreeRecoveryCandidate {
	return append(
		[]storage.WorktreeRecoveryCandidate(nil),
		application.worktrees...,
	)
}

func (application *Application) OrphanedWorkerCandidates() []OrphanedWorkerCandidate {
	return append([]OrphanedWorkerCandidate(nil), application.orphans...)
}

func (application *Application) TaskRunRecoveryCandidates() []storage.UnownedTaskRunRecoveryCandidate {
	return append(
		[]storage.UnownedTaskRunRecoveryCandidate(nil),
		application.taskRuns...,
	)
}

// RecoveryAssessments returns the immutable startup recovery presentation
// facts. No candidate has been resumed automatically.
func (application *Application) RecoveryAssessments() []storage.RecoveryAssessmentRecord {
	return append(
		[]storage.RecoveryAssessmentRecord(nil),
		application.recoveryAssessments...,
	)
}

// RecoveryDecisionService exposes explicit user-authorized recovery actions.
func (application *Application) RecoveryDecisionService() *RecoveryDecisionService {
	return application.recoveryDecisions
}

func (application *Application) AcceptingMutations() bool {
	return application.accepting.Load()
}

func (application *Application) Shutdown(ctx context.Context) error {
	application.closeOnce.Do(func() {
		application.accepting.Store(false)
		var schedulerErr error
		if application.runtime != nil {
			schedulerErr = application.runtime.Shutdown(ctx)
		} else {
			schedulerErr = application.scheduler.Shutdown(ctx)
		}
		application.monitorStop()
		<-application.monitorDone
		var workerErr error
		if application.workers != nil {
			workerErr = application.workers.CheckpointAndStopAll(ctx)
		}
		var checkpointResourcesErr error
		if application.checkpointClose != nil {
			checkpointResourcesErr = application.checkpointClose()
		}
		eventErr := application.events.Close()
		var taskServerErr error
		if application.taskServer != nil {
			taskServerErr = transport.StopGRPCServer(
				ctx,
				application.taskServer,
			)
			if application.taskListener != nil {
				closeErr := application.taskListener.Close()
				if !errors.Is(closeErr, net.ErrClosed) {
					taskServerErr = errors.Join(
						taskServerErr,
						closeErr,
					)
				}
			}
			serveErr := <-application.taskServeDone
			if !errors.Is(serveErr, grpc.ErrServerStopped) &&
				!errors.Is(serveErr, net.ErrClosed) {
				taskServerErr = errors.Join(
					taskServerErr,
					serveErr,
				)
			}
		}
		serverErr := application.server.Shutdown(ctx)
		if serverErr != nil {
			serverErr = errors.Join(serverErr, application.server.Close())
		}
		serveErr := <-application.serveDone
		checkpoint, checkpointErr := application.database.CheckpointWAL(ctx, true)
		if checkpointErr == nil && checkpoint.Busy {
			checkpointErr = errors.New("database WAL remained busy during shutdown")
		}
		databaseErr := application.database.Close(ctx)
		lockErr := application.lock.Close()
		application.closeErr = errors.Join(
			schedulerErr, workerErr, checkpointResourcesErr, eventErr,
			taskServerErr, serverErr,
			serveErr, checkpointErr,
			databaseErr, lockErr,
		)
	})
	return application.closeErr
}

func (application *Application) monitorWorkerHeartbeats(ctx context.Context) {
	defer close(application.monitorDone)
	interval := application.heartbeat / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			candidates, err := application.repos.ExpireWorkerHeartbeats(
				ctx, application.now().Add(-application.heartbeat),
			)
			if err != nil {
				continue
			}
			for _, candidate := range candidates {
				application.gateway.Unregister(candidate.Lease.RunID)
			}
			if len(candidates) != 0 {
				application.recoveryMu.Lock()
				application.recovery = append(application.recovery, candidates...)
				application.recoveryMu.Unlock()
			}
		}
	}
}

func listenLoopback(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse coordinator listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("coordinator must bind to a literal loopback address")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on coordinator loopback: %w", err)
	}
	return listener, nil
}
