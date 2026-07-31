package transport

import (
	"context"
	"net"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestTaskServicePreserveRecoveryPatchMapsTypedRevisionedCommand(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	application := &recoveryActionApplicationStub{view: RecoveryPatchView{
		TaskID: taskID, AssessmentID: "recovery-assessment-1", PatchPath: `C:\patches\task.patch`,
	}}
	service, err := NewTaskService(&taskControlApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	service.recovery = application
	identity, err := TaskIDToProto(taskID)
	if err != nil {
		t.Fatal(err)
	}
	revision := uint64(12)
	response, err := service.PreserveRecoveryPatch(t.Context(), &codefluxv1.PreserveRecoveryPatchRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "preserve-recovery-patch-1", ExpectedRevision: &revision,
		},
		TaskId: identity,
		Reason: "preserve the checkpoint patch for review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if application.command.TaskID != taskID || application.command.ExpectedRevision != revision ||
		application.command.IdempotencyKey != "preserve-recovery-patch-1" ||
		application.command.ReasonRedacted != "preserve the checkpoint patch for review" {
		t.Fatalf("recovery patch command = %#v", application.command)
	}
	if response.GetTaskId().GetValue() != taskID.String() ||
		response.GetAssessmentId() != application.view.AssessmentID ||
		response.GetPatchPath() != application.view.PatchPath {
		t.Fatalf("recovery patch response = %#v", response)
	}
}

func TestPreserveRecoveryPatchOverGRPC(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	application := &recoveryActionApplicationStub{view: RecoveryPatchView{
		TaskID: taskID, AssessmentID: "recovery-assessment-grpc", PatchPath: `C:\patches\grpc.patch`,
	}}
	service, err := NewTaskService(&taskControlApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	service.recovery = application
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	codefluxv1.RegisterTaskServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	revision := uint64(18)
	response, err := codefluxv1.NewTaskServiceClient(connection).PreserveRecoveryPatch(
		t.Context(),
		&codefluxv1.PreserveRecoveryPatchRequest{
			Control: &codefluxv1.MutationControl{
				IdempotencyKey: "preserve-recovery-grpc", ExpectedRevision: &revision,
			},
			TaskId: &codefluxv1.StableIdentity{
				Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: taskID.String(),
			},
			Reason: "preserve from mounted recovery",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.GetPatchPath() != application.view.PatchPath || application.command.ExpectedRevision != revision {
		t.Fatalf("gRPC recovery response=%#v command=%#v", response, application.command)
	}
}

func TestReconcileRecoveryOverGRPCUsesTypedRevisionAndReturnsCheckpoint(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	checkpointID, err := domain.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	application := &recoveryReconciliationApplicationStub{view: RecoveryReconcileView{
		TaskID: taskID, AssessmentID: "recovery-assessment-reconcile",
		CheckpointID: checkpointID, State: domain.TaskStatePaused, Revision: 20,
	}}
	service, err := NewTaskService(&taskControlApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	service.reconciler = application
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	codefluxv1.RegisterTaskServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	revision := uint64(19)
	response, err := codefluxv1.NewTaskServiceClient(connection).ReconcileRecovery(
		t.Context(),
		&codefluxv1.ReconcileRecoveryRequest{
			Control: &codefluxv1.MutationControl{IdempotencyKey: "reconcile-grpc", ExpectedRevision: &revision},
			TaskId:  &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: taskID.String()},
			Reason:  "adopt descendant user worktree changes",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if application.command.ExpectedRevision != revision || application.command.TaskID != taskID ||
		response.GetCheckpointId().GetValue() != checkpointID.String() ||
		response.GetState() != "paused" || response.GetRevision() != 20 {
		t.Fatalf("response=%#v command=%#v", response, application.command)
	}
}

func TestSafeResumeRecoveryOverGRPCUsesDistinctTypedCommand(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	checkpointID, _ := domain.NewCheckpointID()
	application := &recoverySafeResumeApplicationStub{view: RecoverySafeResumeView{
		TaskID: taskID, AssessmentID: "safe-resume-assessment",
		CheckpointID: checkpointID, State: domain.TaskStateRunning, Revision: 31,
	}}
	service, err := NewTaskService(&taskControlApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	service.safeResume = application
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	codefluxv1.RegisterTaskServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	revision := uint64(29)
	response, err := codefluxv1.NewTaskServiceClient(connection).SafeResumeRecovery(t.Context(), &codefluxv1.SafeResumeRecoveryRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "safe-resume-grpc", ExpectedRevision: &revision},
		TaskId:  &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: taskID.String()},
		Reason:  "resume only after authoritative verification",
	})
	if err != nil {
		t.Fatal(err)
	}
	if application.command.ExpectedRevision != revision || application.command.TaskID != taskID ||
		response.GetCheckpointId().GetValue() != checkpointID.String() || response.GetState() != "running" || response.GetRevision() != 31 {
		t.Fatalf("response=%#v command=%#v", response, application.command)
	}
}

type recoveryActionApplicationStub struct {
	command RecoveryPatchCommand
	view    RecoveryPatchView
	err     error
}

type recoveryReconciliationApplicationStub struct {
	command RecoveryReconcileCommand
	view    RecoveryReconcileView
	err     error
}

type recoverySafeResumeApplicationStub struct {
	command RecoverySafeResumeCommand
	view    RecoverySafeResumeView
	err     error
}

func (stub *recoverySafeResumeApplicationStub) SafeResumeTaskRecovery(
	_ context.Context,
	command RecoverySafeResumeCommand,
) (RecoverySafeResumeView, error) {
	stub.command = command
	return stub.view, stub.err
}

func (stub *recoveryReconciliationApplicationStub) ReconcileTaskRecovery(
	_ context.Context,
	command RecoveryReconcileCommand,
) (RecoveryReconcileView, error) {
	stub.command = command
	return stub.view, stub.err
}

func (stub *recoveryActionApplicationStub) PreserveTaskRecoveryPatch(
	_ context.Context,
	command RecoveryPatchCommand,
) (RecoveryPatchView, error) {
	stub.command = command
	return stub.view, stub.err
}
