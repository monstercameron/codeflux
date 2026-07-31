package main

import (
	"context"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/composer"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPrepareMountedTaskMutationBlocksDuplicateAndRetainsCanonicalRetry(t *testing.T) {
	created := 0
	newKey := func() (composer.IdempotencyKey, error) {
		created++
		return composer.IdempotencyKey("task-command-key-1"), nil
	}
	first, started := prepareMountedTaskMutation(
		mountedTaskMutationState{}, mountedTaskPause, 7, newKey,
	)
	if !started || !first.Busy || first.Kind != mountedTaskPause ||
		first.Key != "task-command-key-1" || first.Revision != 7 {
		t.Fatalf("first command = %+v, started=%t", first, started)
	}
	duplicate, started := prepareMountedTaskMutation(first, mountedTaskPause, 7, newKey)
	if started || duplicate != first || created != 1 {
		t.Fatalf("duplicate changed state: %+v, started=%t, created=%d", duplicate, started, created)
	}

	uncertain := settleMountedTaskMutation(first, taskResourceFixtureScope(t), nil, context.DeadlineExceeded)
	if uncertain.Busy || uncertain.Key != first.Key || uncertain.Kind != first.Kind ||
		uncertain.Revision != first.Revision || !strings.Contains(uncertain.Notice, "retained") {
		t.Fatalf("uncertain state = %+v", uncertain)
	}
	retry, started := prepareMountedTaskMutation(uncertain, mountedTaskPause, 99, newKey)
	if !started || retry.Key != first.Key || retry.Revision != 7 || created != 1 {
		t.Fatalf("retry did not preserve canonical request: %+v, started=%t, created=%d", retry, started, created)
	}
	if _, started := prepareMountedTaskMutation(uncertain, mountedTaskStop, 99, newKey); started {
		t.Fatal("different command started while an uncertain command identity was retained")
	}
}

func TestSettleMountedTaskMutationRefreshesStaleAndValidatesCommittedIdentity(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	current := mountedTaskMutationState{
		Kind: mountedTaskResume, Key: "task-command-key-2", Revision: 11, Busy: true,
	}
	var applied mountedTaskMutationState
	reloads := 0
	stale := applyMountedTaskMutationSettlement(
		current, scope, nil, status.Error(codes.Aborted, "stale revision"),
		func(next mountedTaskMutationState) { applied = next },
		func() { reloads++ },
	)
	if stale.Kind != "" || stale.Key != "" || stale.Revision != 0 || stale.Busy ||
		!strings.Contains(stale.Notice, "refreshed") {
		t.Fatalf("stale state = %+v", stale)
	}
	if applied != stale || reloads != 1 {
		t.Fatalf("stale settlement was not applied and refreshed once: applied=%+v reloads=%d", applied, reloads)
	}
	rejected := settleMountedTaskMutation(current, scope, nil, status.Error(codes.FailedPrecondition, "invalid transition"))
	if rejected.Kind != "" || rejected.Key != "" || !strings.Contains(rejected.Notice, "denied") {
		t.Fatalf("confirmed rejection retained a retry identity: %+v", rejected)
	}

	committed := taskResourceFixtureView(scope)
	committed.Revision = 12
	if got := settleMountedTaskMutation(current, scope, committed, nil); got != (mountedTaskMutationState{}) {
		t.Fatalf("committed state = %+v", got)
	}

	wrongTask := taskResourceFixtureView(scope)
	wrongTask.TaskId.Value = "tsk_01890f3c-4a00-7abc-8def-0123456789ac"
	wrongTask.Revision = 12
	got := settleMountedTaskMutation(current, scope, wrongTask, nil)
	if got.Key != current.Key || got.Revision != current.Revision || got.Busy {
		t.Fatalf("mismatched response incorrectly settled command: %+v", got)
	}
}

func TestBindMountedTaskMutationCallbacksWiresOnlyProjectedActions(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	props := taskMutationFixtureProps(t, scope)
	var invoked []mountedTaskMutationKind
	bindMountedTaskMutationCallbacks(&props, scope, mountedTaskMutationState{}, func(kind mountedTaskMutationKind) {
		invoked = append(invoked, kind)
	})
	for _, callback := range []struct {
		name string
		call func()
	}{
		{"pause", props.OnPause}, {"stop", props.OnStop},
	} {
		if callback.call == nil {
			t.Fatalf("%s callback was not wired", callback.name)
		}
		callback.call()
	}
	want := []mountedTaskMutationKind{mountedTaskPause, mountedTaskStop}
	for index := range want {
		if invoked[index] != want[index] {
			t.Fatalf("invoked = %v, want %v", invoked, want)
		}
	}
	if props.OnResume != nil || props.Controls.Resume.DisabledReason == "" {
		t.Fatalf("running projection exposed resume: command=%+v callback=%v", props.Controls.Resume, props.OnResume != nil)
	}
}

func TestBindMountedTaskMutationCallbacksExposesOnlySafeRetry(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	props := taskMutationFixtureProps(t, scope)
	current := mountedTaskMutationState{
		Kind: mountedTaskPause, Key: "task-command-key-3", Revision: props.TaskRevision,
		Notice: "uncertain",
	}
	bindMountedTaskMutationCallbacks(&props, scope, current, func(mountedTaskMutationKind) {})
	if props.Controls.Pause.IdempotencyKey != string(current.Key) || props.OnPause == nil {
		t.Fatalf("safe retry was not exposed: %+v", props.Controls.Pause)
	}
	if props.OnResume != nil || props.OnStop != nil ||
		props.Controls.Resume.DisabledReason == "" || props.Controls.Stop.DisabledReason == "" {
		t.Fatalf("conflicting commands remained actionable: resume=%+v stop=%+v", props.Controls.Resume, props.Controls.Stop)
	}
	if props.CommandNotice != current.Notice {
		t.Fatalf("notice = %q", props.CommandNotice)
	}

	props = taskMutationFixtureProps(t, scope)
	current.Busy = true
	bindMountedTaskMutationCallbacks(&props, scope, current, func(mountedTaskMutationKind) {})
	if !props.Controls.Pause.Busy || props.Controls.Pause.IdempotencyKey != string(current.Key) {
		t.Fatalf("active busy command = %+v", props.Controls.Pause)
	}
	if props.Controls.Resume.Busy || props.Controls.Resume.IdempotencyKey != "" ||
		props.Controls.Stop.Busy || props.Controls.Stop.IdempotencyKey != "" {
		t.Fatalf("request identity leaked to other commands: resume=%+v stop=%+v", props.Controls.Resume, props.Controls.Stop)
	}
	if err := props.Validate(); err != nil {
		t.Fatalf("busy props invalid: %v", err)
	}
}

func TestExecuteMountedTaskMutationWithClientMapsEveryCommand(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	for _, kind := range []mountedTaskMutationKind{mountedTaskPause, mountedTaskResume, mountedTaskStop} {
		t.Run(string(kind), func(t *testing.T) {
			client := &fakeTaskMutationClient{task: taskResourceFixtureView(scope)}
			view, err := executeMountedTaskMutationWithClient(
				t.Context(), client, kind, scope, 17, "task-command-key-4",
			)
			if err != nil {
				t.Fatal(err)
			}
			if view != client.task {
				t.Fatal("returned task view was not forwarded")
			}
			request := client.request(kind)
			if request == nil || request.control().GetIdempotencyKey() != "task-command-key-4" ||
				request.control().GetExpectedRevision() != 17 || request.taskID().GetValue() != scope.taskID.String() {
				t.Fatalf("request = %+v", request)
			}
		})
	}
}

type fakeTaskMutationClient struct {
	task   *codefluxv1.TaskView
	pause  *codefluxv1.PauseTaskRequest
	resume *codefluxv1.ResumeTaskRequest
	stop   *codefluxv1.CancelTaskRequest
	err    error
}

func (client *fakeTaskMutationClient) PauseTask(
	_ context.Context, request *codefluxv1.PauseTaskRequest, _ ...grpc.CallOption,
) (*codefluxv1.PauseTaskResponse, error) {
	client.pause = request
	return &codefluxv1.PauseTaskResponse{Task: client.task}, client.err
}

func (client *fakeTaskMutationClient) ResumeTask(
	_ context.Context, request *codefluxv1.ResumeTaskRequest, _ ...grpc.CallOption,
) (*codefluxv1.ResumeTaskResponse, error) {
	client.resume = request
	return &codefluxv1.ResumeTaskResponse{Task: client.task}, client.err
}

func (client *fakeTaskMutationClient) CancelTask(
	_ context.Context, request *codefluxv1.CancelTaskRequest, _ ...grpc.CallOption,
) (*codefluxv1.CancelTaskResponse, error) {
	client.stop = request
	return &codefluxv1.CancelTaskResponse{Task: client.task}, client.err
}

type capturedTaskMutationRequest struct {
	pause  *codefluxv1.PauseTaskRequest
	resume *codefluxv1.ResumeTaskRequest
	stop   *codefluxv1.CancelTaskRequest
}

func (client *fakeTaskMutationClient) request(kind mountedTaskMutationKind) *capturedTaskMutationRequest {
	request := &capturedTaskMutationRequest{}
	switch kind {
	case mountedTaskPause:
		request.pause = client.pause
	case mountedTaskResume:
		request.resume = client.resume
	case mountedTaskStop:
		request.stop = client.stop
	}
	return request
}

func (request *capturedTaskMutationRequest) control() *codefluxv1.MutationControl {
	switch {
	case request.pause != nil:
		return request.pause.GetControl()
	case request.resume != nil:
		return request.resume.GetControl()
	case request.stop != nil:
		return request.stop.GetControl()
	default:
		return nil
	}
}

func (request *capturedTaskMutationRequest) taskID() *codefluxv1.StableIdentity {
	switch {
	case request.pause != nil:
		return request.pause.GetTaskId()
	case request.resume != nil:
		return request.resume.GetTaskId()
	case request.stop != nil:
		return request.stop.GetTaskId()
	default:
		return nil
	}
}

func taskMutationFixtureProps(t *testing.T, scope taskResourceScope) taskcontrols.Props {
	t.Helper()
	props, err := decodeTaskControlProps(
		taskResourceFixtureView(scope), scope,
		frontendstate.SessionView{Connection: frontendstate.ConnectionLive},
		taskResourceFixtureProjection(scope),
	)
	if err != nil {
		t.Fatal(err)
	}
	return props
}

func TestExecuteMountedTaskMutationWithClientRejectsInvalidInput(t *testing.T) {
	_, err := executeMountedTaskMutationWithClient(
		t.Context(), nil, mountedTaskPause, taskResourceFixtureScope(t), 1, "key",
	)
	if err == nil {
		t.Fatalf("invalid input error = %v", err)
	}
}
