package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
)

// lifecycleApplicationStub records what the RPC handler delegated, so the
// handler's own conversion and validation can be tested without standing up
// storage. Application behaviour is covered end to end over real SQLite in
// coordinator's TestLifecycleAdapter* tests.
type lifecycleApplicationStub struct {
	created      CreateTaskCommand
	started      StartTaskCommand
	createErr    error
	startErr     error
	createdView  CreatedTaskView
	startedView  TaskControlView
	createCalled bool
	startCalled  bool
}

func (stub *lifecycleApplicationStub) CreateTaskFromRequirement(
	_ context.Context,
	command CreateTaskCommand,
) (CreatedTaskView, error) {
	stub.createCalled = true
	stub.created = command
	return stub.createdView, stub.createErr
}

func (stub *lifecycleApplicationStub) StartPreparedTask(
	_ context.Context,
	command StartTaskCommand,
) (TaskControlView, error) {
	stub.startCalled = true
	stub.started = command
	return stub.startedView, stub.startErr
}

func mustLifecycleService(t *testing.T, stub *lifecycleApplicationStub) *TaskService {
	t.Helper()
	service, err := NewTaskService(&taskControlApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	if stub != nil {
		service.ConfigureTaskLifecycle(stub)
	}
	return service
}

func mustTaskIdentity(t *testing.T) (domain.TaskID, *codefluxv1.StableIdentity) {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := TaskIDToProto(taskID)
	if err != nil {
		t.Fatal(err)
	}
	return taskID, identity
}

// TestCreateTaskRPCDelegatesTheDeclaredTaskShape proves the RPC carries the
// caller-declared shape through to the application rather than dropping it.
// Those fields are what land inside the retrieval fingerprint, so silently
// losing them would produce a task that matches nothing.
func TestCreateTaskRPCDelegatesTheDeclaredTaskShape(t *testing.T) {
	taskID, taskIdentity := mustTaskIdentity(t)
	stub := &lifecycleApplicationStub{createdView: CreatedTaskView{
		TaskControlView: TaskControlView{
			TaskID: taskID, State: domain.TaskStateDraft, Revision: 1, UpdatedAt: time.Now().UTC(),
		},
		PolicyRevision: 1, ForecastRevision: 1,
	}}
	service := mustLifecycleService(t, stub)

	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	threadIdentity, err := ThreadIDToProto(threadID)
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.CreateTask(context.Background(), &codefluxv1.CreateTaskRequest{
		Control:                  &codefluxv1.MutationControl{IdempotencyKey: "rpc-create-1"},
		ThreadId:                 threadIdentity,
		Requirement:              "Add a readiness probe.",
		TaskClass:                "feature",
		RepositoryRevision:       "cafebabe",
		BaselineModelRevision:    "model-2026-08-01",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"internal/server"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !stub.createCalled {
		t.Fatal("CreateTask must delegate to the lifecycle application")
	}
	if stub.created.TaskClass != "feature" ||
		stub.created.RepositoryRevision != "cafebabe" ||
		stub.created.BaselineModelRevision != "model-2026-08-01" ||
		stub.created.ToolConfigurationVersion != "tools-v1" ||
		stub.created.ValidationProfileVersion != "profile-v1" {
		t.Fatalf("declared task shape was not carried through: %#v", stub.created)
	}
	if len(stub.created.AffectedPackages) != 1 || stub.created.AffectedPackages[0] != "internal/server" {
		t.Fatalf("scope hints were dropped: %#v", stub.created.AffectedPackages)
	}
	if stub.created.ThreadID != threadID {
		t.Fatalf("thread = %s, want %s", stub.created.ThreadID, threadID)
	}
	if response.GetTask().GetState() != string(domain.TaskStateDraft) {
		t.Fatalf("state = %q, want draft: a created task is not approved yet", response.GetTask().GetState())
	}
	_ = taskIdentity
}

// TestCreateTaskRPCReportsAnUnconfiguredLifecycleHonestly proves an
// uninstalled application surfaces as an error rather than a false success.
func TestCreateTaskRPCReportsAnUnconfiguredLifecycleHonestly(t *testing.T) {
	service := mustLifecycleService(t, nil)
	if _, err := service.CreateTask(context.Background(), &codefluxv1.CreateTaskRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "rpc-unconfigured"},
	}); err == nil {
		t.Fatal("an unconfigured lifecycle application must not report success")
	}
}

// TestStartTaskRPCRequiresTheApprovedPreflightRevision proves the RPC will
// not start work without naming the exact reviewed binding. Without this the
// "start exactly what was approved" guarantee would be advisory.
func TestStartTaskRPCRequiresTheApprovedPreflightRevision(t *testing.T) {
	taskID, taskIdentity := mustTaskIdentity(t)
	stub := &lifecycleApplicationStub{startedView: TaskControlView{
		TaskID: taskID, State: domain.TaskStateRunning, Revision: 4, UpdatedAt: time.Now().UTC(),
	}}
	service := mustLifecycleService(t, stub)
	control := &codefluxv1.MutationControl{
		IdempotencyKey: "rpc-start-1", ExpectedRevision: proto64(3),
	}

	if _, err := service.StartTask(context.Background(), &codefluxv1.StartTaskRequest{
		Control: control, TaskId: taskIdentity, ApprovedPlanRevision: 2,
	}); err == nil {
		t.Fatal("starting without a preflight revision must be rejected")
	}
	if stub.startCalled {
		t.Fatal("the application must not be reached when the request is invalid")
	}

	response, err := service.StartTask(context.Background(), &codefluxv1.StartTaskRequest{
		Control: control, TaskId: taskIdentity,
		ApprovedPlanRevision: 2, PreflightRevision: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.started.PreflightRevision != 7 {
		t.Fatalf("preflight revision = %d, want the approved 7", stub.started.PreflightRevision)
	}
	if stub.started.ApprovedPlanRevision != 2 {
		t.Fatalf("approved plan revision = %d, want 2 (distinct from the preflight binding)", stub.started.ApprovedPlanRevision)
	}
	if response.GetTask().GetState() != string(domain.TaskStateRunning) {
		t.Fatalf("state = %q, want running", response.GetTask().GetState())
	}
}

// TestStartTaskRPCPropagatesApplicationRefusal proves a refusal to start
// reaches the caller instead of being swallowed.
func TestStartTaskRPCPropagatesApplicationRefusal(t *testing.T) {
	_, taskIdentity := mustTaskIdentity(t)
	stub := &lifecycleApplicationStub{startErr: errors.New("execution preflight stale")}
	service := mustLifecycleService(t, stub)

	if _, err := service.StartTask(context.Background(), &codefluxv1.StartTaskRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "rpc-start-refused", ExpectedRevision: proto64(3),
		},
		TaskId: taskIdentity, PreflightRevision: 1,
	}); err == nil {
		t.Fatal("a refused start must surface to the caller")
	}
}

// proto64 returns a pointer to a uint64 for optional proto fields.
func proto64(value uint64) *uint64 { return &value }
