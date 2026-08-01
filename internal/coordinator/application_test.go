package coordinator

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"codeflux.dev/codeflux/internal/worker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const applicationCrashDatabase = "CODEFLUX_APPLICATION_CRASH_DATABASE"
const applicationCrashParentPID = "CODEFLUX_APPLICATION_CRASH_PARENT_PID"

func TestApplicationHostsAuthenticatedTaskControlService(t *testing.T) {
	root := t.TempDir()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	controls := &applicationTaskControlStub{
		view: transport.TaskControlView{
			TaskID:    taskID,
			State:     domain.TaskStatePaused,
			Revision:  6,
			UpdatedAt: time.Unix(1_700_000_000, 0).UTC(),
		},
	}
	application, err := StartApplication(
		t.Context(),
		ApplicationOptions{
			DatabasePath:      filepath.Join(root, "codeflux.sqlite3"),
			BackupDirectory:   filepath.Join(root, "backups"),
			ListenAddress:     "127.0.0.1:0",
			TaskListenAddress: "127.0.0.1:0",
			TaskControls:      controls,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := grpc.NewClient(
		application.TaskControlAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	revision := uint64(5)
	taskIdentity, err := transport.TaskIDToProto(taskID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.AppendToOutgoingContext(
		t.Context(),
		transport.SessionMetadataKey,
		application.BrowserSessionSecret(),
	)
	response, err := codefluxv1.NewTaskServiceClient(connection).PauseTask(
		ctx,
		&codefluxv1.PauseTaskRequest{
			Control: &codefluxv1.MutationControl{
				IdempotencyKey:   "application-pause-1",
				ExpectedRevision: &revision,
			},
			TaskId: taskIdentity,
			Reason: "integration pause",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.GetTask().GetState() !=
		string(domain.TaskStatePaused) ||
		controls.command.ExpectedRevision != 5 {
		t.Fatalf(
			"response=%#v command=%#v",
			response,
			controls.command,
		)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := application.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationHostsGeneratedFrontendOnCoordinatorOrigin(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "frontend")
	if err := os.MkdirAll(filepath.Join(assets, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for relative, content := range map[string]string{
		"index.html":    "<generated-codeflux-shell>",
		"wasm_exec.js":  "generated Go runtime",
		"bin/main.wasm": "generated application",
	} {
		if err := os.WriteFile(
			filepath.Join(assets, filepath.FromSlash(relative)),
			[]byte(content),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	application, err := StartApplication(
		t.Context(),
		ApplicationOptions{
			DatabasePath:            filepath.Join(root, "codeflux.sqlite3"),
			BackupDirectory:         filepath.Join(root, "backups"),
			ListenAddress:           "127.0.0.1:0",
			TaskListenAddress:       "127.0.0.1:0",
			TaskControls:            &applicationTaskControlStub{},
			FrontendAssetsDirectory: assets,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := application.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})

	client := &http.Client{}
	// /graphs rather than /tasks: /tasks is not a route the client can parse,
	// so the server correctly refuses it now.
	response, err := client.Get("http://" + application.Address() + "/graphs")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK ||
		string(body) != "<generated-codeflux-shell>" {
		t.Fatalf("frontend = %d %q, %v", response.StatusCode, body, readErr)
	}
	if len(response.Cookies()) != 1 || !response.Cookies()[0].HttpOnly {
		t.Fatalf("frontend cookies = %#v", response.Cookies())
	}

	bootstrap, err := client.Get(
		"http://" + application.Address() + "/bootstrap",
	)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapBody, readErr := io.ReadAll(bootstrap.Body)
	bootstrap.Body.Close()
	if readErr != nil || bootstrap.StatusCode != http.StatusOK ||
		strings.Contains(
			string(bootstrapBody),
			application.BrowserSessionSecret(),
		) ||
		!strings.Contains(string(bootstrapBody), `"bridge_path":"/grpc"`) ||
		strings.Contains(string(bootstrapBody), `"selected_session_id"`) {
		t.Fatalf(
			"bootstrap = %d %q, %v",
			bootstrap.StatusCode,
			bootstrapBody,
			readErr,
		)
	}
}

func TestApplicationStartsOnceOnLoopbackMigratesAndShutsDownInOrder(t *testing.T) {
	root := t.TempDir()
	workers := &recordingWorkerController{}
	options := ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.db"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0",
		WorkerLimit:     2, HeartbeatTimeout: 5 * time.Second,
		Random: func(buffer []byte) (int, error) {
			for index := range buffer {
				buffer[index] = byte(index + 1)
			}
			return len(buffer), nil
		},
		Workers:      workers,
		TaskControls: &applicationTaskControlStub{},
	}
	application, err := StartApplication(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !application.AcceptingMutations() ||
		len(application.BrowserSessionSecret()) < 32 {
		t.Fatal("application did not expose ready non-secret startup state")
	}
	if application.EventHub() == nil || application.TransportBoundary() == nil {
		t.Fatal("application did not initialize event and transport services")
	}
	if application.RecoveryDecisionService() == nil ||
		len(application.RecoveryAssessments()) != 0 {
		t.Fatal("application did not initialize bounded recovery services")
	}
	if application.ProviderDependencies().Credentials == nil ||
		application.ProviderDependencies().References == nil ||
		application.WorkspaceDependencies().Discovery == nil ||
		application.WorkspaceDependencies().Bindings == nil {
		t.Fatal("application did not initialize provider and workspace services")
	}
	host, _, err := net.SplitHostPort(application.Address())
	if err != nil || !net.ParseIP(host).IsLoopback() {
		t.Fatalf("application address = %q, %v", application.Address(), err)
	}
	response, err := http.Get("http://" + application.Address() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK ||
		string(body) != `{"status":"ok","database":"ready","migrations":"current","isolation":"mediated workspace confinement, not a perfect sandbox"}` {
		t.Fatalf("health = %d %q, %v", response.StatusCode, body, err)
	}
	if _, err := StartApplication(t.Context(), options); err == nil {
		t.Fatal("second coordinator acquired the same database")
	}
	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if application.AcceptingMutations() || workers.calls() != 1 {
		t.Fatalf("shutdown state accepting=%t worker-calls=%d",
			application.AcceptingMutations(), workers.calls())
	}
	sessionID, err := domain.NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.EventHub().Subscribe(
		context.Background(),
		events.SubscriptionQuery{SessionID: sessionID},
	); !errors.Is(err, events.ErrHubClosed) {
		t.Fatalf("event hub after shutdown = %v", err)
	}
	if err := application.Shutdown(context.Background()); err != nil ||
		workers.calls() != 1 {
		t.Fatalf("repeated shutdown = %v, calls=%d", err, workers.calls())
	}
}

func TestHealthReportsDatabaseFailureWithoutRawDetail(t *testing.T) {
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.db"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0",
		Workers:         &recordingWorkerController{},
		TaskControls:    &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	if err := application.database.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get("http://" + application.Address() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusServiceUnavailable ||
		string(body) != `{"status":"error","database":"unavailable","migrations":"unknown"}` {
		t.Fatalf("database failure health = %d %q, %v", response.StatusCode, body, readErr)
	}
	if strings.Contains(string(body), root) || strings.Contains(string(body), "SELECT") || strings.Contains(string(body), ".db") {
		t.Fatalf("database failure exposed sensitive detail: %q", body)
	}
}

func TestApplicationRejectsNonLoopbackAndShortRandomSecret(t *testing.T) {
	root := t.TempDir()
	base := ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.db"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "0.0.0.0:0",
	}
	if _, err := StartApplication(t.Context(), base); err == nil {
		t.Fatal("non-loopback coordinator was accepted")
	}
	base.ListenAddress = "127.0.0.1:0"
	base.Random = func([]byte) (int, error) { return 1, nil }
	if _, err := StartApplication(t.Context(), base); err == nil {
		t.Fatal("short session entropy was accepted")
	}
}

func TestApplicationBuildsDefaultWorkerRuntime(t *testing.T) {
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.db"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if application.WorkerRuntime() == nil ||
		application.TaskControlAddress() == "" {
		t.Fatal(
			"default worker runtime or task control service was not initialized",
		)
	}
	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationShutdownCheckpointsPausedWorkerAndCancelsQueuedTask(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "codeflux.db")
	shutdownCheckpointID, err := domain.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    databasePath,
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0",
		WorkerLimit:     1,
		ShutdownCheckpoints: &supervisorGracefulCheckpointer{
			id: shutdownCheckpointID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(supervisorHelperMode, "run")
	firstQueue, firstStart := applicationRuntimeFixture(
		t, application, databasePath, 1,
	)
	secondQueue, secondStart := applicationRuntimeFixture(
		t, application, databasePath, 2,
	)
	if _, err := application.SubmitWorker(
		t.Context(), firstQueue, firstStart,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SubmitWorker(
		t.Context(), secondQueue, secondStart,
	); err != nil {
		t.Fatal(err)
	}
	lease, started, err := application.StartNextWorker(t.Context())
	if err != nil || !started || lease.RunID != firstStart.RunID {
		t.Fatalf("start first worker = %#v, %t, %v", lease, started, err)
	}
	waitFor(t, func() bool {
		current, exists := application.gateway.SnapshotLease(firstStart.RunID)
		return exists && current.State == storage.WorkerLeaseRunning
	})
	if err := application.QueueWorkerControl(firstStart.RunID, worker.Control{
		Kind: worker.ControlPause, Reason: "combined shutdown test",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		current, exists := application.gateway.SnapshotLease(firstStart.RunID)
		return exists && current.State == storage.WorkerLeasePaused
	})
	if position, err := application.WorkerQueuePosition(
		secondStart.TaskID,
	); err != nil || position.Position != 1 {
		t.Fatalf("queued task position = %#v, %v", position, err)
	}
	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	sqlite, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()
	var queueState, leaseState string
	if err := sqlite.QueryRowContext(
		t.Context(),
		`SELECT state FROM task_queue_entries WHERE id = ?`,
		secondQueue.ID,
	).Scan(&queueState); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.QueryRowContext(
		t.Context(),
		`SELECT state FROM worker_leases WHERE id = ?`,
		firstStart.LeaseID,
	).Scan(&leaseState); err != nil {
		t.Fatal(err)
	}
	if queueState != string(storage.TaskQueueStateCancelled) ||
		leaseState != string(storage.WorkerLeaseExited) {
		t.Fatalf("shutdown durable states = queue:%s lease:%s", queueState, leaseState)
	}
	if _, err := application.SubmitWorker(
		t.Context(), secondQueue, secondStart,
	); err == nil {
		t.Fatal("shutdown application accepted a new worker mutation")
	}
}

func TestApplicationHeartbeatMonitorRequiresRecoveryAfterLiveExpiry(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "codeflux.db")
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:     databasePath,
		BackupDirectory:  filepath.Join(root, "backups"),
		ListenAddress:    "127.0.0.1:0",
		HeartbeatTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, start := applicationRuntimeFixture(t, application, databasePath, 1)
	if _, err := application.repos.AcquireWorkerLease(
		t.Context(),
		storage.AcquireWorkerLease{
			ID: start.LeaseID, TaskID: start.TaskID, RunID: start.RunID,
			ProtocolVersion: 1, ToolSchemaVersion: 1,
			WorktreePath:       start.WorktreePath,
			Endpoint:           start.CoordinatorEndpoint,
			SessionTokenSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		candidates := application.RecoveryCandidates()
		return len(candidates) == 1 &&
			candidates[0].Lease.RunID == start.RunID
	})
	task, err := application.repos.GetTask(t.Context(), start.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.State != domain.TaskStateRecoveryRequired {
		t.Fatalf("heartbeat-expired task state = %s", task.State)
	}
	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationWorkerCrashPreservesSQLiteAndOtherTaskWorktree(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "codeflux.db")
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    databasePath,
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0",
		WorkerLimit:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	crashQueue, crashStart := applicationRuntimeFixture(
		t, application, databasePath, 1,
	)
	healthyQueue, healthyStart := applicationRuntimeFixture(
		t, application, databasePath, 2,
	)
	sentinel := filepath.Join(healthyStart.WorktreePath, "healthy-task-only")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SubmitWorker(
		t.Context(), crashQueue, crashStart,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SubmitWorker(
		t.Context(), healthyQueue, healthyStart,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv(supervisorHelperMode, "crash")
	if _, started, err := application.StartNextWorker(t.Context()); err != nil || !started {
		t.Fatalf("start crashing worker = %t, %v", started, err)
	}
	t.Setenv(supervisorHelperMode, "run")
	if _, started, err := application.StartNextWorker(t.Context()); err != nil || !started {
		t.Fatalf("start healthy worker = %t, %v", started, err)
	}
	sqlite, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()
	waitFor(t, func() bool {
		var state string
		err := sqlite.QueryRowContext(
			t.Context(),
			`SELECT state FROM worker_leases WHERE id = ?`,
			crashStart.LeaseID,
		).Scan(&state)
		return err == nil && state == string(storage.WorkerLeaseCrashed)
	})
	waitFor(t, func() bool {
		current, exists := application.gateway.SnapshotLease(healthyStart.RunID)
		return exists && current.State == storage.WorkerLeaseRunning
	})
	content, err := os.ReadFile(sentinel)
	if err != nil || string(content) != "unchanged" {
		t.Fatalf("healthy worktree sentinel = %q, %v", content, err)
	}
	if err := application.database.IntegrityCheck(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := application.QueueWorkerControl(
		healthyStart.RunID,
		worker.Control{Kind: worker.ControlShutdown, Reason: "test complete"},
	); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return application.supervisor.ActiveCount() == 0 })
	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func applicationRuntimeFixture(
	t *testing.T,
	application *Application,
	databasePath string,
	sequence uint64,
) (storage.EnqueueTask, StartWorker) {
	t.Helper()
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	ctx := t.Context()
	if _, err := application.repos.CreateProject(ctx, storage.CreateProject{
		ID: projectID, Name: "Runtime fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateRepository(ctx, storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: filepath.Join(filepath.Dir(databasePath), repositoryID.String()),
		GitIdentity:   "runtime-" + repositoryID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID,
		Title: "Runtime fixture",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := application.repos.CreateTask(ctx, storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey:    "runtime-task-" + taskID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlite, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMicro()
	if _, err := sqlite.ExecContext(
		ctx,
		`INSERT INTO runs (
			id, task_id, state, attempt, task_revision, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, 'running', 1, ?, ?, ?, ?)`,
		runID, task.ID, task.Revision, "runtime-run-"+runID.String(), now, now,
	); err != nil {
		sqlite.Close()
		t.Fatal(err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	queue := storage.EnqueueTask{
		ID: "queue-application-" + taskID.String(), TaskID: taskID,
		ProviderKey: "scripted", Reason: "waiting for worker capacity",
		Priority: 1, EnqueueSequence: sequence,
	}
	start := StartWorker{
		LeaseID: "lease-application-" + runID.String(),
		TaskID:  taskID, RunID: runID, WorktreePath: t.TempDir(),
		ToolSchemaVersion:   1,
		CoordinatorEndpoint: "http://" + application.Address(),
		Executable:          os.Args[0],
		ExecutableArguments: []string{"-test.run=^TestSupervisorWorkerHelper$"},
		AdditionalAllowed:   []string{supervisorHelperMode},
	}
	return queue, start
}

func TestApplicationCrashHelper(t *testing.T) {
	databasePath := os.Getenv(applicationCrashDatabase)
	if databasePath == "" {
		return
	}
	application, err := StartApplication(context.Background(), ApplicationOptions{
		DatabasePath:    databasePath,
		BackupDirectory: filepath.Join(filepath.Dir(databasePath), "backups"),
		ListenAddress:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID, threadID, taskID, runID := crashFixtureIDs(t)
	ctx := context.Background()
	if _, err := application.repos.CreateProject(ctx, storage.CreateProject{
		ID: projectID, Name: "Crash fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateRepository(ctx, storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: filepath.Join(filepath.Dir(databasePath), "repository"),
		GitIdentity:   "crash-fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID,
		Title: "Crash recovery",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := application.repos.CreateTask(ctx, storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset:      domain.PolicyPresetBalanced,
		ReasoningEffort:   domain.ReasoningEffortStandard,
		RiskLevel:         domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey:    "application-crash-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlite, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMicro()
	if _, err := sqlite.ExecContext(
		ctx,
		`INSERT INTO runs (
			id, task_id, state, attempt, task_revision, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, 'running', 1, ?, 'application-crash-run', ?, ?)`,
		runID, task.ID, task.Revision, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	lease, err := application.repos.AcquireWorkerLease(ctx, storage.AcquireWorkerLease{
		ID: "lease-application-crash", TaskID: task.ID, RunID: runID,
		ProtocolVersion: 1, ToolSchemaVersion: 1,
		WorktreePath:       filepath.Join(filepath.Dir(databasePath), "task-worktree"),
		Endpoint:           "http://" + application.Address(),
		SessionTokenSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	if err != nil {
		t.Fatal(err)
	}
	parentPID, err := strconv.Atoi(os.Getenv(applicationCrashParentPID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.RecordWorkerProcessStarted(
		ctx,
		storage.RecordWorkerProcessStarted{
			ID: lease.ID, ExpectedRevision: lease.Revision,
			ProcessID: parentPID,
		},
	); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestCoordinatorCrashLeavesDurableRecoveryChoice(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "codeflux.db")
	command := exec.Command(os.Args[0], "-test.run=^TestApplicationCrashHelper$")
	command.Env = append(
		os.Environ(),
		applicationCrashDatabase+"="+databasePath,
		applicationCrashParentPID+"="+strconv.Itoa(os.Getpid()),
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("crash helper: %v\n%s", err, output)
	}
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    databasePath,
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, taskID, runID := crashFixtureIDs(t)
	candidates := application.RecoveryCandidates()
	if len(candidates) != 1 ||
		candidates[0].Lease.TaskID != taskID ||
		candidates[0].Lease.RunID != runID {
		t.Fatalf("recovery candidates = %#v", candidates)
	}
	orphans := application.OrphanedWorkerCandidates()
	if len(orphans) != 1 || !orphans[0].Alive || orphans[0].Unknown ||
		!orphans[0].IdentityUnverified {
		t.Fatalf("orphaned workers = %#v", orphans)
	}
	task, err := application.repos.GetTask(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.State != domain.TaskStateRecoveryRequired {
		t.Fatalf("task state after coordinator crash = %s", task.State)
	}
	assessments := application.RecoveryAssessments()
	if len(assessments) != 1 ||
		assessments[0].TaskID != taskID ||
		assessments[0].RunID != runID ||
		assessments[0].CheckpointID != nil ||
		assessments[0].Classification !=
			storage.RecoveryClassificationImpossible {
		t.Fatalf("startup recovery assessments = %#v", assessments)
	}
	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func crashFixtureIDs(
	t *testing.T,
) (
	domain.ProjectID,
	domain.RepositoryID,
	domain.ThreadID,
	domain.TaskID,
	domain.RunID,
) {
	t.Helper()
	projectID, err := domain.ParseProjectID("prj_00000000-0000-7000-8000-000000000201")
	if err != nil {
		t.Fatal(err)
	}
	repositoryID, err := domain.ParseRepositoryID("repo_00000000-0000-7000-8000-000000000202")
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.ParseThreadID("thr_00000000-0000-7000-8000-000000000203")
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.ParseTaskID("tsk_00000000-0000-7000-8000-000000000204")
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.ParseRunID("run_00000000-0000-7000-8000-000000000205")
	if err != nil {
		t.Fatal(err)
	}
	return projectID, repositoryID, threadID, taskID, runID
}

type recordingWorkerController struct {
	mu    sync.Mutex
	count int
}

type applicationTaskControlStub struct {
	command transport.TaskControlCommand
	view    transport.TaskControlView
	err     error
}

func (stub *applicationTaskControlStub) PauseTaskControl(
	_ context.Context,
	command transport.TaskControlCommand,
) (transport.TaskControlView, error) {
	stub.command = command
	return stub.view, stub.err
}

func (stub *applicationTaskControlStub) ResumeTaskControl(
	_ context.Context,
	command transport.TaskControlCommand,
) (transport.TaskControlView, error) {
	stub.command = command
	return stub.view, stub.err
}

func (stub *applicationTaskControlStub) CancelTaskControl(
	_ context.Context,
	command transport.TaskControlCommand,
) (transport.TaskControlView, error) {
	stub.command = command
	return stub.view, stub.err
}

func (controller *recordingWorkerController) CheckpointAndStopAll(
	context.Context,
) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.count++
	return nil
}

func (controller *recordingWorkerController) calls() int {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.count
}
