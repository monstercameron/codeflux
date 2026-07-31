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

type mountedRecoveryReconcileState struct {
	TaskID    string
	Key       composer.IdempotencyKey
	Revision  uint64
	Busy      bool
	Completed bool
	Notice    string
}

type recoveryReconcileClient interface {
	ReconcileRecovery(context.Context, *codefluxv1.ReconcileRecoveryRequest, ...grpc.CallOption) (*codefluxv1.ReconcileRecoveryResponse, error)
}

func prepareMountedRecoveryReconcile(
	current mountedRecoveryReconcileState,
	revision uint64,
	newKey taskMutationKeyFactory,
) (mountedRecoveryReconcileState, bool) {
	if current.Busy || revision == 0 {
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

func settleMountedRecoveryReconcile(
	current mountedRecoveryReconcileState,
	scope taskResourceScope,
	response *codefluxv1.ReconcileRecoveryResponse,
	err error,
) mountedRecoveryReconcileState {
	if err == nil && response != nil {
		taskID, decodeErr := decodeTaskIdentity(response.GetTaskId())
		if decodeErr == nil && taskID == scope.taskID &&
			strings.TrimSpace(response.GetAssessmentId()) != "" &&
			response.GetCheckpointId() != nil && response.GetState() == "paused" &&
			response.GetRevision() > current.Revision {
			return mountedRecoveryReconcileState{
				TaskID:    scope.taskID.String(),
				Completed: true,
				Notice:    "User worktree changes were reconciled into a verified checkpoint. Resume remains an explicit action.",
			}
		}
	}
	switch status.Code(err) {
	case codes.Aborted, codes.FailedPrecondition, codes.PermissionDenied, codes.NotFound:
		return mountedRecoveryReconcileState{
			TaskID: scope.taskID.String(),
			Notice: "Reconciliation was safely refused because authoritative recovery state changed or remained unsafe.",
		}
	}
	current.Busy = false
	current.Notice = "The coordinator did not confirm reconciliation. Its request identity is retained for a safe retry."
	return current
}

func bindMountedRecoveryReconcileCallback(
	props *taskcontrols.Props,
	scope taskResourceScope,
	current mountedRecoveryReconcileState,
	invoke func(),
) {
	if props == nil || !props.Recovery.ReconcileRequired {
		return
	}
	if current.TaskID != "" && current.TaskID != scope.taskID.String() {
		current = mountedRecoveryReconcileState{}
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
		command.DisabledReason = "This recovery assessment has already been reconciled."
	case current.Busy:
		command.Busy = true
	case props.Delivery.State != taskcontrols.DeliveryLive || !props.Delivery.SequenceCertain:
		command.DisabledReason = "Session sequence certainty is required before reconciling recovery state."
	case scope.taskID.IsZero() || props.TaskRevision == 0 || invoke == nil:
		command.DisabledReason = "Typed recovery reconciliation dispatch is unavailable."
	default:
		props.OnReconcile = invoke
	}
	props.Recovery.Reconcile = command
}

func executeMountedRecoveryReconcileWithClient(
	ctx context.Context,
	client recoveryReconcileClient,
	scope taskResourceScope,
	revision uint64,
	key string,
) (*codefluxv1.ReconcileRecoveryResponse, error) {
	if client == nil || scope.taskID.IsZero() || revision == 0 || strings.TrimSpace(key) == "" {
		return nil, errors.New("invalid recovery reconciliation request")
	}
	return client.ReconcileRecovery(ctx, &codefluxv1.ReconcileRecoveryRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: key, ExpectedRevision: &revision},
		TaskId:  taskIdentity(scope.taskID),
		Reason:  "Reconcile verified descendant user worktree changes into a new recovery checkpoint.",
	})
}
