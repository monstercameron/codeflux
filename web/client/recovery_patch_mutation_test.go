package main

import (
	"context"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"google.golang.org/grpc"
)

func TestMountedRecoveryPatchUsesTypedRequestAndSettlesOnce(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	client := &recoveryPatchClientStub{response: &codefluxv1.PreserveRecoveryPatchResponse{
		TaskId: taskIdentity(scope.taskID), AssessmentId: "recovery-assessment-1",
		PatchPath: `C:\patches\task.patch`,
	}}
	current, started := prepareMountedRecoveryPatch(
		mountedRecoveryPatchState{},
		12,
		func() (composer.IdempotencyKey, error) { return "preserve-key-1", nil },
	)
	if !started || !current.Busy || current.Revision != 12 {
		t.Fatalf("prepared recovery patch = %#v, started=%t", current, started)
	}
	response, err := executeMountedRecoveryPatchWithClient(
		t.Context(), client, scope, current.Revision, string(current.Key),
	)
	if err != nil {
		t.Fatal(err)
	}
	if client.request.GetTaskId().GetValue() != scope.taskID.String() ||
		client.request.GetControl().GetExpectedRevision() != 12 ||
		client.request.GetControl().GetIdempotencyKey() != "preserve-key-1" ||
		strings.TrimSpace(client.request.GetReason()) == "" {
		t.Fatalf("typed recovery patch request = %#v", client.request)
	}
	settled := settleMountedRecoveryPatch(current, scope, response, nil)
	if settled.Busy || settled.PatchPath != client.response.GetPatchPath() ||
		!strings.Contains(settled.Notice, client.response.GetPatchPath()) {
		t.Fatalf("settled recovery patch = %#v", settled)
	}
	props := taskcontrols.Props{
		TaskRevision: 12,
		Delivery: taskcontrols.DeliveryView{
			State: taskcontrols.DeliveryLive, SequenceCertain: true,
		},
		Recovery: taskcontrols.RecoveryView{PatchPreservable: true},
	}
	bindMountedRecoveryPatchCallback(&props, scope, settled, func() {})
	if props.OnPreservePatch != nil || !strings.Contains(props.Recovery.PreservePatch.DisabledReason, "already preserved") {
		t.Fatalf("completed preservation remained actionable: %+v", props.Recovery.PreservePatch)
	}
}

func TestMountedRecoveryPatchRemainsDisabledWithoutSequenceCertainty(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	props := taskcontrols.Props{
		TaskRevision: 12,
		Delivery:     taskcontrols.DeliveryView{State: taskcontrols.DeliveryDisconnected},
		Recovery:     taskcontrols.RecoveryView{PatchPreservable: true},
	}
	bindMountedRecoveryPatchCallback(&props, scope, mountedRecoveryPatchState{}, func() {})
	if props.OnPreservePatch != nil || !strings.Contains(props.Recovery.PreservePatch.DisabledReason, "sequence certainty") {
		t.Fatalf("disconnected recovery patch = %+v", props.Recovery.PreservePatch)
	}
}

type recoveryPatchClientStub struct {
	request  *codefluxv1.PreserveRecoveryPatchRequest
	response *codefluxv1.PreserveRecoveryPatchResponse
	err      error
}

func (stub *recoveryPatchClientStub) PreserveRecoveryPatch(
	_ context.Context,
	request *codefluxv1.PreserveRecoveryPatchRequest,
	_ ...grpc.CallOption,
) (*codefluxv1.PreserveRecoveryPatchResponse, error) {
	stub.request = request
	return stub.response, stub.err
}
