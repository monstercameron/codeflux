package main

import (
	"context"
	"errors"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc"
)

// taskStartClient is the lifecycle surface starting a request needs.
type taskStartClient interface {
	CreateTask(context.Context, *codefluxv1.CreateTaskRequest, ...grpc.CallOption) (
		*codefluxv1.CreateTaskResponse, error)
	ApprovePlan(context.Context, *codefluxv1.ApprovePlanRequest, ...grpc.CallOption) (
		*codefluxv1.ApprovePlanResponse, error)
	StartTask(context.Context, *codefluxv1.StartTaskRequest, ...grpc.CallOption) (
		*codefluxv1.StartTaskResponse, error)
}

// errNoDeclaredTaskClass reports a request with no declared kind of change.
//
// It is a distinct error because it is not a failure: the message is recorded
// and the person simply has not said what kind of work this is. Nothing can
// guess it for them — a class lands inside the fingerprint that gates
// project-memory retrieval and routing, so an invented one would silently
// change which prior work the task is compared against.
var errNoDeclaredTaskClass = errors.New(
	"choose a kind of change beside the message field to start work")

// taskStartError marks a request that was recorded but did not start.
//
// The two failures are not the same and must not be reported as one: a send
// that failed means the words are gone and should be retried, while a start
// that failed means the request is durable and only the work did not begin.
// Reporting the second as the first told people their message had not been
// accepted when it had.
type taskStartError struct{ cause error }

func (failure taskStartError) Error() string {
	return "the request was recorded but work did not start: " + failure.cause.Error()
}

func (failure taskStartError) Unwrap() error { return failure.cause }

// startRequestedTask turns a recorded request into a running task.
//
// The three calls are separate because they are three different decisions:
// what the work is, that a person approved its forecast and budget, and that it
// should begin now. All three existed and none was reachable from a client, so
// a submitted request became a draft task that nothing could start, and the
// interface truthfully reported that no task was running.
func startRequestedTask(
	ctx context.Context,
	client taskStartClient,
	command composerSendCommand,
	messageID domain.MessageID,
) (domain.TaskID, error) {
	class := strings.TrimSpace(command.TaskClass)
	if class == "" {
		return domain.TaskID{}, errNoDeclaredTaskClass
	}
	if client == nil || command.ThreadID.IsZero() || messageID.IsZero() {
		return domain.TaskID{}, errors.New("task start command is invalid")
	}
	key := string(command.Key)

	created, err := client.CreateTask(ctx, &codefluxv1.CreateTaskRequest{
		Control:  &codefluxv1.MutationControl{IdempotencyKey: "create:" + key},
		ThreadId: threadIdentity(command.ThreadID),
		SourceMessageId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE,
			Value: messageID.String(),
		},
		Requirement: command.Draft.Text(),
		TaskClass:   class,
		// The base revision, toolchain, validation profile, and baseline model
		// are left unstated on purpose. They are facts about the coordinator's
		// machine, and a browser supplying them would be guessing.
	})
	if err != nil {
		return domain.TaskID{}, err
	}
	taskID, err := taskIdentityOf(created.GetTask())
	if err != nil {
		return domain.TaskID{}, err
	}

	approved, err := client.ApprovePlan(ctx, &codefluxv1.ApprovePlanRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey:   "approve:" + key,
			ExpectedRevision: revisionOf(created.GetTask()),
		},
		TaskId: created.GetTask().GetTaskId(),
	})
	if err != nil {
		// The task exists and is reviewable even though it did not start. It is
		// reported rather than cleaned up: deleting a task a person can still
		// look at would destroy the only record of what was asked for.
		return taskID, err
	}
	if approved.GetPreflightRevision() == 0 {
		return taskID, errors.New(
			"the coordinator approved the plan without binding what was reviewed")
	}

	if _, err := client.StartTask(ctx, &codefluxv1.StartTaskRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey:   "start:" + key,
			ExpectedRevision: revisionOf(approved.GetTask()),
		},
		TaskId:            approved.GetTask().GetTaskId(),
		PreflightRevision: approved.GetPreflightRevision(),
	}); err != nil {
		return taskID, err
	}
	return taskID, nil
}

// threadIdentity renders a thread identity for a request.
func threadIdentity(threadID domain.ThreadID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD,
		Value: threadID.String(),
	}
}

// taskIdentityOf reads a typed task identity out of a view.
func taskIdentityOf(view *codefluxv1.TaskView) (domain.TaskID, error) {
	identity := view.GetTaskId()
	if identity.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK {
		return domain.TaskID{}, errors.New("the response carries no typed task identity")
	}
	return domain.ParseTaskID(identity.GetValue())
}

// revisionOf reads the revision a following call must expect.
//
// Threading it forward is what makes each step apply to the task as the
// previous step left it; re-reading it would let something else move the task
// in between and have the next step apply to a different thing.
func revisionOf(view *codefluxv1.TaskView) *uint64 {
	revision := view.GetRevision()
	return &revision
}
