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

func TestMountedRecoverySafeResumeRetainsIdentityAndRequiresSequenceCertainty(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	checkpointID := "ckp_00000000-0000-4000-8000-000000002401"
	client := &recoverySafeResumeClientStub{response: &codefluxv1.SafeResumeRecoveryResponse{
		TaskId: taskIdentity(scope.taskID), AssessmentId: "safe-resume-assessment",
		CheckpointId: &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT, Value: checkpointID},
		State:        "running", Revision: 15,
	}}
	current, started := prepareMountedRecoverySafeResume(mountedRecoverySafeResumeState{}, 13,
		func() (composer.IdempotencyKey, error) { return "safe-resume-key", nil })
	if !started || !current.Busy || current.Revision != 13 {
		t.Fatalf("prepared = %#v started=%t", current, started)
	}
	response, err := executeMountedRecoverySafeResumeWithClient(t.Context(), client, scope, current.Revision, string(current.Key))
	if err != nil {
		t.Fatal(err)
	}
	if client.request.GetControl().GetIdempotencyKey() != "safe-resume-key" ||
		client.request.GetControl().GetExpectedRevision() != 13 ||
		client.request.GetTaskId().GetValue() != scope.taskID.String() {
		t.Fatalf("typed request = %#v", client.request)
	}
	settled := settleMountedRecoverySafeResume(current, scope, response, nil)
	if !settled.Completed || settled.Busy || !strings.Contains(settled.Notice, "re-verified") {
		t.Fatalf("settled = %#v", settled)
	}
	props := taskcontrols.Props{
		TaskRevision: 13,
		Delivery:     taskcontrols.DeliveryView{State: taskcontrols.DeliveryDisconnected},
		Recovery:     taskcontrols.RecoveryView{SafeResumeVerified: true},
	}
	bindMountedRecoverySafeResumeCallback(&props, scope, mountedRecoverySafeResumeState{}, func() {})
	if props.OnSafeResume != nil || !strings.Contains(props.Recovery.SafeResume.DisabledReason, "sequence certainty") {
		t.Fatalf("disconnected safe resume = %+v", props.Recovery.SafeResume)
	}
	props.Delivery = taskcontrols.DeliveryView{State: taskcontrols.DeliveryLive, SequenceCertain: true}
	bindMountedRecoverySafeResumeCallback(&props, scope, mountedRecoverySafeResumeState{}, func() {})
	if props.OnSafeResume == nil {
		t.Fatal("live verified safe resume callback was not bound")
	}
}

type recoverySafeResumeClientStub struct {
	request  *codefluxv1.SafeResumeRecoveryRequest
	response *codefluxv1.SafeResumeRecoveryResponse
	err      error
}

func (stub *recoverySafeResumeClientStub) SafeResumeRecovery(
	_ context.Context,
	request *codefluxv1.SafeResumeRecoveryRequest,
	_ ...grpc.CallOption,
) (*codefluxv1.SafeResumeRecoveryResponse, error) {
	stub.request = request
	return stub.response, stub.err
}
