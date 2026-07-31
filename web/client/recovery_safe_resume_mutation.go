package main

import (
	"context"
	"errors"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mountedRecoverySafeResumeState struct {
	TaskID    string
	Key       composer.IdempotencyKey
	Revision  uint64
	Busy      bool
	Completed bool
	Notice    string
}

type recoverySafeResumeClient interface {
	SafeResumeRecovery(context.Context, *codefluxv1.SafeResumeRecoveryRequest, ...grpc.CallOption) (*codefluxv1.SafeResumeRecoveryResponse, error)
}

func prepareMountedRecoverySafeResume(
	current mountedRecoverySafeResumeState,
	revision uint64,
	newKey taskMutationKeyFactory,
) (mountedRecoverySafeResumeState, bool) {
	if current.Busy || current.Completed || revision == 0 {
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
		current.Key, current.Revision = key, revision
	}
	current.Busy = true
	current.Notice = ""
	return current, true
}

func settleMountedRecoverySafeResume(
	current mountedRecoverySafeResumeState,
	scope taskResourceScope,
	response *codefluxv1.SafeResumeRecoveryResponse,
	err error,
) mountedRecoverySafeResumeState {
	if err == nil && response != nil {
		taskID, decodeErr := decodeTaskIdentity(response.GetTaskId())
		if decodeErr == nil && taskID == scope.taskID &&
			strings.TrimSpace(response.GetAssessmentId()) != "" &&
			response.GetCheckpointId() != nil && response.GetState() == "running" &&
			response.GetRevision() > current.Revision {
			return mountedRecoverySafeResumeState{
				TaskID: scope.taskID.String(), Completed: true,
				Notice: "Recovery safety was re-verified and execution resumed from the authoritative checkpoint.",
			}
		}
	}
	switch status.Code(err) {
	case codes.Aborted, codes.FailedPrecondition, codes.PermissionDenied, codes.NotFound:
		return mountedRecoverySafeResumeState{
			TaskID: scope.taskID.String(),
			Notice: "Safe resume was refused because recovery state changed or verification no longer passed.",
		}
	}
	current.Busy = false
	current.Notice = "The coordinator did not confirm safe resume. Its request identity is retained for a safe retry."
	return current
}

func bindMountedRecoverySafeResumeCallback(
	props *taskcontrols.Props,
	scope taskResourceScope,
	current mountedRecoverySafeResumeState,
	invoke func(),
) {
	if props == nil || !props.Recovery.SafeResumeVerified {
		return
	}
	if current.TaskID != "" && current.TaskID != scope.taskID.String() {
		current = mountedRecoverySafeResumeState{}
	}
	if current.Notice != "" {
		if props.CommandNotice != "" {
			props.CommandNotice += " "
		}
		props.CommandNotice += current.Notice
	}
	command := taskcontrols.CommandState{IdempotencyKey: string(current.Key)}
	switch {
	case current.Completed:
		command.DisabledReason = "This recovery assessment has already been resumed."
	case current.Busy:
		command.Busy = true
	case props.Delivery.State != taskcontrols.DeliveryLive || !props.Delivery.SequenceCertain:
		command.DisabledReason = "Session sequence certainty is required before safe recovery resume."
	case scope.taskID.IsZero() || props.TaskRevision == 0 || invoke == nil:
		command.DisabledReason = "Typed safe recovery resume dispatch is unavailable."
	default:
		props.OnSafeResume = invoke
	}
	props.Recovery.SafeResume = command
}

func executeMountedRecoverySafeResumeWithClient(
	ctx context.Context,
	client recoverySafeResumeClient,
	scope taskResourceScope,
	revision uint64,
	key string,
) (*codefluxv1.SafeResumeRecoveryResponse, error) {
	if client == nil || scope.taskID.IsZero() || revision == 0 || strings.TrimSpace(key) == "" {
		return nil, errors.New("invalid safe recovery resume request")
	}
	return client.SafeResumeRecovery(ctx, &codefluxv1.SafeResumeRecoveryRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: key, ExpectedRevision: &revision},
		TaskId:  taskIdentity(scope.taskID),
		Reason:  "Resume only after re-verifying the authoritative recovery checkpoint.",
	})
}
