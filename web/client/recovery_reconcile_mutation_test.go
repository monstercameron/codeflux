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

func TestMountedRecoveryReconcileUsesTypedRequestAndRequiresSequenceCertainty(t *testing.T) {
	scope := taskResourceFixtureScope(t)
	checkpointID := "ckp_00000000-0000-4000-8000-000000002301"
	client := &recoveryReconcileClientStub{response: &codefluxv1.ReconcileRecoveryResponse{
		TaskId: taskIdentity(scope.taskID), AssessmentId: "recovery-assessment-1",
		CheckpointId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT,
			Value: checkpointID,
		},
		State: "paused", Revision: 13,
	}}
	current, started := prepareMountedRecoveryReconcile(
		mountedRecoveryReconcileState{}, 12,
		func() (composer.IdempotencyKey, error) { return "reconcile-key-1", nil },
	)
	if !started || !current.Busy || current.Revision != 12 {
		t.Fatalf("prepared reconciliation = %#v, started=%t", current, started)
	}
	response, err := executeMountedRecoveryReconcileWithClient(
		t.Context(), client, scope, current.Revision, string(current.Key),
	)
	if err != nil {
		t.Fatal(err)
	}
	if client.request.GetControl().GetExpectedRevision() != 12 ||
		client.request.GetControl().GetIdempotencyKey() != "reconcile-key-1" ||
		client.request.GetTaskId().GetValue() != scope.taskID.String() ||
		strings.TrimSpace(client.request.GetReason()) == "" {
		t.Fatalf("typed reconciliation request = %#v", client.request)
	}
	settled := settleMountedRecoveryReconcile(current, scope, response, nil)
	if settled.Busy || !strings.Contains(settled.Notice, "verified checkpoint") {
		t.Fatalf("settled reconciliation = %#v", settled)
	}
	completedProps := taskcontrols.Props{
		TaskRevision: 12,
		Delivery:     taskcontrols.DeliveryView{State: taskcontrols.DeliveryLive, SequenceCertain: true},
		Recovery:     taskcontrols.RecoveryView{ReconcileRequired: true},
	}
	bindMountedRecoveryReconcileCallback(&completedProps, scope, settled, func() {})
	if completedProps.OnReconcile != nil || !strings.Contains(completedProps.Recovery.Reconcile.DisabledReason, "already been reconciled") {
		t.Fatalf("completed reconciliation remained actionable: %+v", completedProps.Recovery.Reconcile)
	}

	props := taskcontrols.Props{
		TaskRevision: 12,
		Delivery:     taskcontrols.DeliveryView{State: taskcontrols.DeliveryDisconnected},
		Recovery:     taskcontrols.RecoveryView{ReconcileRequired: true},
	}
	bindMountedRecoveryReconcileCallback(&props, scope, mountedRecoveryReconcileState{}, func() {})
	if props.OnReconcile != nil || !strings.Contains(props.Recovery.Reconcile.DisabledReason, "sequence certainty") {
		t.Fatalf("disconnected reconciliation control = %+v", props.Recovery.Reconcile)
	}
	props.Delivery = taskcontrols.DeliveryView{State: taskcontrols.DeliveryLive, SequenceCertain: true}
	bindMountedRecoveryReconcileCallback(&props, scope, mountedRecoveryReconcileState{}, func() {})
	if props.OnReconcile == nil || props.Recovery.Reconcile.DisabledReason != "" {
		t.Fatalf("live reconciliation control = %+v", props.Recovery.Reconcile)
	}
}

type recoveryReconcileClientStub struct {
	request  *codefluxv1.ReconcileRecoveryRequest
	response *codefluxv1.ReconcileRecoveryResponse
	err      error
}

func (stub *recoveryReconcileClientStub) ReconcileRecovery(
	_ context.Context,
	request *codefluxv1.ReconcileRecoveryRequest,
	_ ...grpc.CallOption,
) (*codefluxv1.ReconcileRecoveryResponse, error) {
	stub.request = request
	return stub.response, stub.err
}
