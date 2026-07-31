package main

import (
	"context"
	"errors"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"google.golang.org/grpc"
)

type approvalCommand struct {
	TaskID     domain.TaskID
	ApprovalID domain.ApprovalID
	Key        string
	Action     timelinecard.ApprovalAction
	Scope      string
}

type generatedApprovalClient interface {
	ApproveAction(context.Context, *codefluxv1.ApproveActionRequest, ...grpc.CallOption) (*codefluxv1.ApproveActionResponse, error)
}

type generatedApprovalTransport struct{ client generatedApprovalClient }

func (transport generatedApprovalTransport) Resolve(
	ctx context.Context,
	command approvalCommand,
) (uint64, error) {
	if transport.client == nil || command.TaskID.IsZero() || command.ApprovalID.IsZero() ||
		command.Key == "" || command.Scope == "" {
		return 0, errors.New("approval command is invalid")
	}
	switch command.Action {
	case timelinecard.ApprovalAllowOnce, timelinecard.ApprovalAllowForTask, timelinecard.ApprovalDeny:
	default:
		return 0, errors.New("approval decision is invalid")
	}
	response, err := transport.client.ApproveAction(ctx, &codefluxv1.ApproveActionRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: command.Key},
		TaskId: &codefluxv1.StableIdentity{
			Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: command.TaskID.String(),
		},
		ApprovalId: &codefluxv1.StableIdentity{
			Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL, Value: command.ApprovalID.String(),
		},
		Decision: string(command.Action), Scope: command.Scope,
	})
	if err != nil {
		return 0, err
	}
	if response == nil || response.GetApprovalRevision() == 0 {
		return 0, errors.New("approval response lacks a committed revision")
	}
	return response.GetApprovalRevision(), nil
}
