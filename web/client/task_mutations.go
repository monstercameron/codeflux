package main

import (
	"context"
	"errors"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mountedTaskMutationKind string

const (
	mountedTaskPause  mountedTaskMutationKind = "pause"
	mountedTaskResume mountedTaskMutationKind = "resume"
	mountedTaskStop   mountedTaskMutationKind = "stop"
)

type mountedTaskMutationState struct {
	Kind     mountedTaskMutationKind
	Key      composer.IdempotencyKey
	Revision uint64
	Busy     bool
	Notice   string
}

type taskMutationClient interface {
	PauseTask(context.Context, *codefluxv1.PauseTaskRequest, ...grpc.CallOption) (*codefluxv1.PauseTaskResponse, error)
	ResumeTask(context.Context, *codefluxv1.ResumeTaskRequest, ...grpc.CallOption) (*codefluxv1.ResumeTaskResponse, error)
	CancelTask(context.Context, *codefluxv1.CancelTaskRequest, ...grpc.CallOption) (*codefluxv1.CancelTaskResponse, error)
}

type taskMutationKeyFactory func() (composer.IdempotencyKey, error)

func prepareMountedTaskMutation(
	current mountedTaskMutationState,
	kind mountedTaskMutationKind,
	revision uint64,
	newKey taskMutationKeyFactory,
) (mountedTaskMutationState, bool) {
	if current.Busy || current.Kind != "" && current.Kind != kind || revision == 0 {
		return current, false
	}
	if current.Key == "" {
		if newKey == nil {
			return current, false
		}
		key, err := newKey()
		if err != nil || key == "" {
			return current, false
		}
		current = mountedTaskMutationState{Kind: kind, Key: key, Revision: revision}
	}
	// A retry retains the complete canonical request identity: command kind,
	// idempotency key, and the original expected revision.
	current.Busy = true
	current.Notice = ""
	return current, true
}

func settleMountedTaskMutation(
	current mountedTaskMutationState,
	scope taskResourceScope,
	view *codefluxv1.TaskView,
	err error,
) mountedTaskMutationState {
	if err == nil && validCommittedTaskMutation(view, scope, current) {
		return mountedTaskMutationState{}
	}
	switch status.Code(err) {
	case codes.Aborted:
		return mountedTaskMutationState{
			Notice: "Task state changed before this command committed. Authoritative state was refreshed.",
		}
	case codes.FailedPrecondition, codes.PermissionDenied,
		codes.NotFound, codes.InvalidArgument:
		return mountedTaskMutationState{
			Notice: "The coordinator denied this task command in its current authoritative state. Authoritative state was refreshed.",
		}
	}
	current.Busy = false
	current.Notice = "The coordinator did not confirm this task command. Its request identity is retained for a safe retry."
	return current
}

func applyMountedTaskMutationSettlement(
	current mountedTaskMutationState,
	scope taskResourceScope,
	view *codefluxv1.TaskView,
	err error,
	setState func(mountedTaskMutationState),
	reload func(),
) mountedTaskMutationState {
	next := settleMountedTaskMutation(current, scope, view, err)
	if setState != nil {
		setState(next)
	}
	if reload != nil {
		reload()
	}
	return next
}

func validCommittedTaskMutation(
	view *codefluxv1.TaskView,
	scope taskResourceScope,
	command mountedTaskMutationState,
) bool {
	if view == nil || command.Revision == 0 || view.GetRevision() <= command.Revision {
		return false
	}
	taskID, err := decodeTaskIdentity(view.GetTaskId())
	if err != nil || taskID != scope.taskID {
		return false
	}
	state := domain.TaskState(strings.TrimSpace(view.GetState()))
	switch command.Kind {
	case mountedTaskPause:
		return state == domain.TaskStatePaused
	case mountedTaskResume:
		return state == domain.TaskStateRunning
	case mountedTaskStop:
		return state == domain.TaskStateCancelled
	default:
		return false
	}
}

func bindMountedTaskMutationCallbacks(
	props *taskcontrols.Props,
	scope taskResourceScope,
	current mountedTaskMutationState,
	invoke func(mountedTaskMutationKind),
) {
	if props == nil {
		return
	}
	props.CommandNotice = current.Notice
	if props.Delivery.State != taskcontrols.DeliveryLive || !props.Delivery.SequenceCertain ||
		scope.taskID.IsZero() || props.TaskRevision == 0 {
		return
	}
	if current.Busy {
		reason := "Another task command is awaiting authoritative settlement."
		props.Controls.Pause = busyTaskMutationCommand(current, mountedTaskPause, reason)
		props.Controls.Resume = busyTaskMutationCommand(current, mountedTaskResume, reason)
		props.Controls.Stop = busyTaskMutationCommand(current, mountedTaskStop, reason)
		return
	}

	retainedReason := ""
	if current.Kind != "" {
		retainedReason = "The earlier " + string(current.Kind) +
			" command has an uncertain outcome. Retry it with its retained request identity before issuing another command."
	}
	bind := func(kind mountedTaskMutationKind) (taskcontrols.CommandState, func()) {
		projected := taskcontrols.CommandState{}
		switch kind {
		case mountedTaskPause:
			projected = props.Controls.Pause
		case mountedTaskResume:
			projected = props.Controls.Resume
		case mountedTaskStop:
			projected = props.Controls.Stop
		}
		if strings.TrimSpace(projected.DisabledReason) != "" {
			return projected, nil
		}
		if current.Kind != "" && current.Kind != kind {
			return taskcontrols.CommandState{DisabledReason: retainedReason}, nil
		}
		command := taskcontrols.CommandState{IdempotencyKey: retainedMutationKey(current, kind)}
		if invoke == nil {
			command.DisabledReason = "Task command dispatch is unavailable."
			return command, nil
		}
		return command, func() { invoke(kind) }
	}
	props.Controls.Pause, props.OnPause = bind(mountedTaskPause)
	props.Controls.Resume, props.OnResume = bind(mountedTaskResume)
	props.Controls.Stop, props.OnStop = bind(mountedTaskStop)
}

func busyTaskMutationCommand(
	current mountedTaskMutationState,
	kind mountedTaskMutationKind,
	reason string,
) taskcontrols.CommandState {
	command := taskcontrols.CommandState{DisabledReason: reason}
	if current.Kind == kind {
		command.Busy = true
		command.IdempotencyKey = string(current.Key)
	}
	return command
}

func retainedMutationKey(state mountedTaskMutationState, kind mountedTaskMutationKind) string {
	if state.Kind == kind {
		return string(state.Key)
	}
	return ""
}

func executeMountedTaskMutationWithClient(
	ctx context.Context,
	client taskMutationClient,
	kind mountedTaskMutationKind,
	scope taskResourceScope,
	revision uint64,
	key string,
) (*codefluxv1.TaskView, error) {
	if client == nil || scope.taskID.IsZero() || revision == 0 || strings.TrimSpace(key) == "" {
		return nil, errors.New("invalid task mutation request")
	}
	control := &codefluxv1.MutationControl{IdempotencyKey: key, ExpectedRevision: &revision}
	switch kind {
	case mountedTaskPause:
		response, err := client.PauseTask(ctx, &codefluxv1.PauseTaskRequest{
			Control: control, TaskId: taskIdentity(scope.taskID), Reason: "User requested pause from task controls.",
		})
		if err != nil || response == nil {
			return nil, err
		}
		return response.GetTask(), nil
	case mountedTaskResume:
		response, err := client.ResumeTask(ctx, &codefluxv1.ResumeTaskRequest{
			Control: control, TaskId: taskIdentity(scope.taskID),
		})
		if err != nil || response == nil {
			return nil, err
		}
		return response.GetTask(), nil
	case mountedTaskStop:
		response, err := client.CancelTask(ctx, &codefluxv1.CancelTaskRequest{
			Control: control, TaskId: taskIdentity(scope.taskID), Reason: "User requested stop from task controls.",
		})
		if err != nil || response == nil {
			return nil, err
		}
		return response.GetTask(), nil
	default:
		return nil, errors.New("unsupported task mutation")
	}
}
