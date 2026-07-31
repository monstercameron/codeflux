package main

import (
	"context"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"google.golang.org/grpc"
)

type recordingApprovalClient struct {
	request *codefluxv1.ApproveActionRequest
}

func (client *recordingApprovalClient) ApproveAction(
	_ context.Context,
	request *codefluxv1.ApproveActionRequest,
	_ ...grpc.CallOption,
) (*codefluxv1.ApproveActionResponse, error) {
	client.request = request
	return &codefluxv1.ApproveActionResponse{ApprovalRevision: 7}, nil
}

func TestGeneratedApprovalTransportPreservesTypedDecisionAndIdentity(t *testing.T) {
	taskID, err := domain.ParseTaskID("tsk_01890f3c-4a00-7abc-8def-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	approvalID, err := domain.ParseApprovalID("apr_01890f3c-4a00-7abc-8def-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	client := &recordingApprovalClient{}
	revision, err := (generatedApprovalTransport{client: client}).Resolve(t.Context(), approvalCommand{
		TaskID: taskID, ApprovalID: approvalID, Key: "approval-command-1",
		Action: timelinecard.ApprovalAllowForTask, Scope: "repository:current-task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision != 7 {
		t.Fatalf("revision = %d, want 7", revision)
	}
	request := client.request
	if request.GetControl().GetIdempotencyKey() != "approval-command-1" ||
		request.GetTaskId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK ||
		request.GetTaskId().GetValue() != taskID.String() ||
		request.GetApprovalId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL ||
		request.GetApprovalId().GetValue() != approvalID.String() ||
		request.GetDecision() != string(timelinecard.ApprovalAllowForTask) ||
		request.GetScope() != "repository:current-task" {
		t.Fatalf("approval request did not preserve the command: %#v", request)
	}
}
