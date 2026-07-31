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

type mountedRecoveryPatchState struct {
	TaskID    string
	Key       composer.IdempotencyKey
	Revision  uint64
	Busy      bool
	Notice    string
	PatchPath string
}

type recoveryPatchClient interface {
	PreserveRecoveryPatch(
		context.Context,
		*codefluxv1.PreserveRecoveryPatchRequest,
		...grpc.CallOption,
	) (*codefluxv1.PreserveRecoveryPatchResponse, error)
}

func prepareMountedRecoveryPatch(
	current mountedRecoveryPatchState,
	revision uint64,
	newKey taskMutationKeyFactory,
) (mountedRecoveryPatchState, bool) {
	if current.Busy || revision == 0 || current.PatchPath != "" {
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
		current.Key = key
		current.Revision = revision
	}
	current.Busy = true
	current.Notice = ""
	return current, true
}

func settleMountedRecoveryPatch(
	current mountedRecoveryPatchState,
	scope taskResourceScope,
	response *codefluxv1.PreserveRecoveryPatchResponse,
	err error,
) mountedRecoveryPatchState {
	if err == nil && response != nil {
		taskID, decodeErr := decodeTaskIdentity(response.GetTaskId())
		path := strings.TrimSpace(response.GetPatchPath())
		if decodeErr == nil && taskID == scope.taskID &&
			strings.TrimSpace(response.GetAssessmentId()) != "" && path != "" {
			return mountedRecoveryPatchState{
				TaskID:    scope.taskID.String(),
				PatchPath: path,
				Notice:    "The checkpoint patch was preserved at " + path + ".",
			}
		}
	}
	switch status.Code(err) {
	case codes.Aborted, codes.FailedPrecondition, codes.PermissionDenied, codes.NotFound:
		return mountedRecoveryPatchState{
			Notice: "Recovery state changed before patch preservation committed. Authoritative state was refreshed.",
		}
	}
	current.Busy = false
	current.Notice = "The coordinator did not confirm patch preservation. Its request identity is retained for a safe retry."
	return current
}

func bindMountedRecoveryPatchCallback(
	props *taskcontrols.Props,
	scope taskResourceScope,
	current mountedRecoveryPatchState,
	invoke func(),
) {
	if props == nil || !props.Recovery.PatchPreservable {
		return
	}
	if current.TaskID != "" && current.TaskID != scope.taskID.String() {
		current = mountedRecoveryPatchState{}
	}
	if current.Notice != "" {
		if props.CommandNotice != "" {
			props.CommandNotice += " "
		}
		props.CommandNotice += current.Notice
	}
	command := taskcontrols.CommandState{IdempotencyKey: string(current.Key)}
	switch {
	case current.PatchPath != "":
		command.DisabledReason = "This checkpoint patch is already preserved at " + current.PatchPath + "."
	case current.Busy:
		command.Busy = true
	case props.Delivery.State != taskcontrols.DeliveryLive || !props.Delivery.SequenceCertain:
		command.DisabledReason = "Session sequence certainty is required before preserving a recovery patch."
	case scope.taskID.IsZero() || props.TaskRevision == 0 || invoke == nil:
		command.DisabledReason = "Typed patch-preservation dispatch is unavailable."
	default:
		props.OnPreservePatch = invoke
	}
	props.Recovery.PreservePatch = command
}

func executeMountedRecoveryPatchWithClient(
	ctx context.Context,
	client recoveryPatchClient,
	scope taskResourceScope,
	revision uint64,
	key string,
) (*codefluxv1.PreserveRecoveryPatchResponse, error) {
	if client == nil || scope.taskID.IsZero() || revision == 0 || strings.TrimSpace(key) == "" {
		return nil, errors.New("invalid recovery patch request")
	}
	return client.PreserveRecoveryPatch(ctx, &codefluxv1.PreserveRecoveryPatchRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: key, ExpectedRevision: &revision},
		TaskId:  taskIdentity(scope.taskID),
		Reason:  "Preserve the current recovery checkpoint patch for user review.",
	})
}
