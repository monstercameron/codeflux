package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"google.golang.org/grpc"
)

// recordingTaskClient records the chain a send drives.
type recordingTaskClient struct {
	calls             []string
	createErr         error
	approveErr        error
	startErr          error
	preflightRevision uint64
	taskID            domain.TaskID
	createdRevision   uint64
	approvedRevision  uint64
	startedExpected   uint64
	startedPreflight  uint64
}

func (client *recordingTaskClient) CreateTask(
	_ context.Context, request *codefluxv1.CreateTaskRequest, _ ...grpc.CallOption,
) (*codefluxv1.CreateTaskResponse, error) {
	client.calls = append(client.calls, "create:"+request.GetTaskClass())
	if client.createErr != nil {
		return nil, client.createErr
	}
	return &codefluxv1.CreateTaskResponse{Task: &codefluxv1.TaskView{
		TaskId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK,
			Value: client.taskID.String(),
		},
		Revision: client.createdRevision,
	}}, nil
}

func (client *recordingTaskClient) ApprovePlan(
	_ context.Context, request *codefluxv1.ApprovePlanRequest, _ ...grpc.CallOption,
) (*codefluxv1.ApprovePlanResponse, error) {
	client.calls = append(client.calls, "approve")
	if client.approveErr != nil {
		return nil, client.approveErr
	}
	if request.GetControl().GetExpectedRevision() != client.createdRevision {
		return nil, errors.New("approval did not carry the revision creation returned")
	}
	return &codefluxv1.ApprovePlanResponse{
		Task: &codefluxv1.TaskView{
			TaskId:   request.GetTaskId(),
			Revision: client.approvedRevision,
		},
		PreflightRevision: client.preflightRevision,
	}, nil
}

func (client *recordingTaskClient) StartTask(
	_ context.Context, request *codefluxv1.StartTaskRequest, _ ...grpc.CallOption,
) (*codefluxv1.StartTaskResponse, error) {
	client.calls = append(client.calls, "start")
	client.startedExpected = request.GetControl().GetExpectedRevision()
	client.startedPreflight = request.GetPreflightRevision()
	if client.startErr != nil {
		return nil, client.startErr
	}
	return &codefluxv1.StartTaskResponse{Task: &codefluxv1.TaskView{
		TaskId: request.GetTaskId(),
	}}, nil
}

func startChainFixture(t *testing.T) (*recordingTaskClient, composerSendCommand, domain.MessageID) {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	messageID, err := domain.NewMessageID()
	if err != nil {
		t.Fatal(err)
	}
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	// A draft is only writable in a thread bound to an authorized repository,
	// which is the same rule the product enforces.
	draft, err := composer.NewModel(composer.ThreadBinding{
		ThreadID: threadID, RepositoryID: repositoryID,
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := composer.Reduce(draft, composer.DraftTextChanged{
		ThreadID: threadID, Text: "Add a doc comment to ResolveRevision.",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingTaskClient{
		taskID: taskID, createdRevision: 1, approvedRevision: 4, preflightRevision: 2,
	}
	command := composerSendCommand{
		ThreadID: threadID, Key: composer.IdempotencyKey(strings.Repeat("k", 26)),
		Draft: next.Draft(threadID), TaskClass: "documentation",
	}
	return client, command, messageID
}

func TestASentRequestDrivesTheWholeStartChain(t *testing.T) {
	// Sending used to record a message and a bare draft task with no policy,
	// forecast, or budget, so nothing could ever start it. The interface then
	// truthfully reported that no task was running, and every test passed.
	client, command, messageID := startChainFixture(t)
	taskID, err := startRequestedTask(context.Background(), client, command, messageID)
	if err != nil {
		t.Fatalf("starting the request failed: %v", err)
	}
	if taskID != client.taskID {
		t.Errorf("returned task %s, want %s", taskID, client.taskID)
	}
	want := []string{"create:documentation", "approve", "start"}
	if strings.Join(client.calls, ",") != strings.Join(want, ",") {
		t.Errorf("drove %v, want %v", client.calls, want)
	}
	// Each step must apply to the task as the previous step left it, or
	// something else could move it in between and the next step would apply to
	// a different thing.
	if client.startedExpected != client.approvedRevision {
		t.Errorf("start expected revision %d, want the approved %d",
			client.startedExpected, client.approvedRevision)
	}
	if client.startedPreflight != client.preflightRevision {
		t.Errorf("start named preflight %d, want the approved %d",
			client.startedPreflight, client.preflightRevision)
	}
}

func TestNoKindMeansNoTaskAndNoFailure(t *testing.T) {
	// The person has not said what kind of work this is. Nothing can guess it
	// for them, and the message they wrote is already recorded, so this is a
	// state to explain rather than an error to report.
	client, command, messageID := startChainFixture(t)
	command.TaskClass = "   "
	_, err := startRequestedTask(context.Background(), client, command, messageID)
	if !errors.Is(err, errNoDeclaredTaskClass) {
		t.Fatalf("an undeclared kind reported %v", err)
	}
	if len(client.calls) != 0 {
		t.Errorf("an undeclared kind still called %v", client.calls)
	}
	if !strings.Contains(err.Error(), "Options") {
		t.Errorf("the refusal does not say where to fix it: %v", err)
	}
}

func TestAFailedApprovalStillNamesTheTaskThatExists(t *testing.T) {
	// The task exists and is reviewable even though it did not start. Losing
	// its identity would destroy the only handle on what was asked for.
	client, command, messageID := startChainFixture(t)
	client.approveErr = errors.New("the coordinator refused")
	taskID, err := startRequestedTask(context.Background(), client, command, messageID)
	if err == nil {
		t.Fatal("a refused approval reported success")
	}
	if taskID != client.taskID {
		t.Errorf("a refused approval lost the created task: %s", taskID)
	}
	if len(client.calls) != 2 {
		t.Errorf("a refused approval still drove %v", client.calls)
	}
}

func TestAnApprovalThatBindsNothingIsRefused(t *testing.T) {
	// Starting against a zero preflight would pass every check and mean
	// nothing: no policy, forecast, or budget would actually be bound.
	client, command, messageID := startChainFixture(t)
	client.preflightRevision = 0
	if _, err := startRequestedTask(
		context.Background(), client, command, messageID,
	); err == nil {
		t.Fatal("an approval binding nothing was accepted")
	}
	for _, call := range client.calls {
		if call == "start" {
			t.Error("a task was started against no binding")
		}
	}
}
