package main

import (
	"context"
	"errors"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeSessionProjectionSnapshotClient struct {
	response *codefluxv1.GetSessionSnapshotResponse
	err      error
	request  *codefluxv1.GetSessionSnapshotRequest
}

func (client *fakeSessionProjectionSnapshotClient) GetSessionSnapshot(
	_ context.Context,
	request *codefluxv1.GetSessionSnapshotRequest,
	_ ...grpc.CallOption,
) (*codefluxv1.GetSessionSnapshotResponse, error) {
	client.request = request
	return client.response, client.err
}

func TestDecodeSessionProjectionSnapshotMapsEveryAuthoritativeFact(t *testing.T) {
	snapshot := completeSessionProjectionSnapshot(t)
	got, err := decodeSessionProjectionSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	task, err := taskprojection.ApplySnapshot(*got.Task)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.ThroughSequence != 41 || got.Session.TaskRevision != 10 || got.GraphRevision != 6 ||
		task.LastSequence != 41 || task.State != domain.TaskStateRecoveryRequired ||
		!task.Plan.Present || task.Plan.Revision != 3 || task.Plan.Approval != domain.ApprovalRequestStateGranted ||
		!task.Tool.Present || task.Tool.Revision != 9 || task.Tool.CommandName != "go test" ||
		!task.Approval.Pending() || task.Approval.Revision != 11 ||
		!task.Budget.Present || task.Budget.Revision != 0 || task.Budget.HardLimit.MinorUnits != 400 ||
		!task.Validation.Present || task.Validation.Revision != 4 ||
		!task.Checkpoint.Present || task.Checkpoint.Revision != 5 ||
		!task.RecoveryDetail.Present || task.RecoveryDetail.Revision != 6 || !task.RecoveryDetail.ReconcileAvailable ||
		!task.Acceptance.Present || task.Acceptance.Revision != 7 ||
		!task.Review.Present || task.Review.Revision != 8 ||
		!task.Graph.Present || task.Graph.Revision != 6 ||
		len(task.Policy.Denied) != 1 || task.Policy.Denied[0] != taskprojection.ActionStop ||
		task.Policy.SafeReason != "Stop is denied while recovery is reconciled" ||
		task.PendingCommand.Status != taskprojection.CommandIdle || task.PendingCommand.OwnsKey() {
		t.Fatalf("decoded task projection = %+v", task)
	}
	if got.Session.ValidationRevision != 4 || got.Session.CheckpointRevision != 5 ||
		got.Session.ChangeAcceptanceRevision != 7 || got.Session.Checkpoint == nil ||
		got.Session.Validation == nil || got.Session.ChangeAcceptance == nil {
		t.Fatalf("decoded session snapshot = %+v", got.Session)
	}
}

func TestDecodeSessionProjectionSnapshotRejectsPartialOrUnattributedFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*codefluxv1.SessionProjectionSnapshot)
	}{
		{"missing task identity", func(value *codefluxv1.SessionProjectionSnapshot) { value.TaskId = nil }},
		{"plan approval missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.PlanApprovalState = "" }},
		{"approval revision missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.ApprovalRevision = 0 }},
		{"budget entity missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.Budget = nil; value.BudgetRevision = 1 }},
		{"validation revision missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.ValidationRevision = 0 }},
		{"checkpoint time missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.CheckpointCreatedAt = nil }},
		{"recovery revision missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.RecoveryRevision = 0 }},
		{"acceptance entity missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.ChangeAcceptance = nil }},
		{"tool revision missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.ToolRevision = 0 }},
		{"review revision missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.ReviewRevision = 0 }},
		{"unknown policy action", func(value *codefluxv1.SessionProjectionSnapshot) { value.DeniedTaskActions = []string{"invented"} }},
		{"policy reason missing", func(value *codefluxv1.SessionProjectionSnapshot) { value.TaskActionPolicyReason = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := completeSessionProjectionSnapshot(t)
			test.mutate(value)
			if got, err := decodeSessionProjectionSnapshot(value); err == nil {
				t.Fatalf("malformed snapshot decoded: %+v", got)
			}
		})
	}
}

func TestFetchSessionProjectionSnapshotBindsResponseToMountedIdentity(t *testing.T) {
	snapshot := completeSessionProjectionSnapshot(t)
	sessionID, _ := domain.ParseSessionID(snapshot.GetSessionId().GetValue())
	threadID, _ := domain.ParseThreadID(snapshot.GetThreadId().GetValue())
	taskID, _ := domain.ParseTaskID(snapshot.GetTaskId().GetValue())
	client := &fakeSessionProjectionSnapshotClient{
		response: &codefluxv1.GetSessionSnapshotResponse{Snapshot: snapshot},
	}

	got, err := fetchSessionProjectionSnapshot(context.Background(), client, sessionID, threadID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.ThroughSequence != 41 || client.request == nil ||
		client.request.GetSessionId().GetValue() != sessionID.String() {
		t.Fatalf("snapshot = %#v, request = %#v", got, client.request)
	}

	// A projection that has not caught up with a brand new task is behind, not
	// broken: the task exists, the events that carry it into the session
	// sequence have not been applied yet, and refusing the whole snapshot for
	// that reason disconnected the console the instant work started.
	behind := &codefluxv1.SessionProjectionSnapshot{
		SessionId:       snapshot.GetSessionId(),
		ThreadId:        snapshot.GetThreadId(),
		ThroughSequence: snapshot.GetThroughSequence(),
		ObservedAt:      snapshot.GetObservedAt(),
	}
	client.response = &codefluxv1.GetSessionSnapshotResponse{Snapshot: behind}
	if _, err = fetchSessionProjectionSnapshot(context.Background(), client, sessionID, threadID, taskID); err != nil {
		t.Fatalf("a snapshot that lags the new task was refused: %v", err)
	}

	// A snapshot naming a different task is a real mismatch.
	other := completeSessionProjectionSnapshot(t)
	other.TaskId = snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, "tsk_01890f3c-4a00-7abc-8def-2123456789ab")
	client.response = &codefluxv1.GetSessionSnapshotResponse{Snapshot: other}
	if _, err = fetchSessionProjectionSnapshot(context.Background(), client, sessionID, threadID, taskID); !errors.Is(err, errSessionProjectionSnapshotMalformed) {
		t.Fatalf("task mismatch error = %v", err)
	}

	mismatched := completeSessionProjectionSnapshot(t)
	mismatched.ThreadId = snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, "thr_01890f3c-4a00-7abc-8def-1123456789ab")
	client.response = &codefluxv1.GetSessionSnapshotResponse{Snapshot: mismatched}
	if _, err = fetchSessionProjectionSnapshot(context.Background(), client, sessionID, threadID, taskID); !errors.Is(err, errSessionProjectionSnapshotMalformed) {
		t.Fatalf("identity mismatch error = %v", err)
	}
}

func completeSessionProjectionSnapshot(t *testing.T) *codefluxv1.SessionProjectionSnapshot {
	t.Helper()
	bindings := &codefluxv1.SessionRevisionBindings{DiffRevision: 2, PlanRevision: 3, ValidationRevision: 4, EvidenceRevision: 5, GraphRevision: 6}
	return &codefluxv1.SessionProjectionSnapshot{
		SessionId:       snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, "ses_01890f3c-4a00-7abc-8def-0123456789ab"),
		ThreadId:        snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, "thr_01890f3c-4a00-7abc-8def-0123456789ab"),
		TaskId:          snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, "tsk_01890f3c-4a00-7abc-8def-0123456789ab"),
		ThroughSequence: 41, ObservedAt: timestamppb.New(time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC)),
		TaskState: string(domain.TaskStateRecoveryRequired), TaskRevision: 10,
		Plan:              &codefluxv1.PlanEvent{PlanRevision: 3, RedactedSummary: "Repair session certainty"},
		PlanApprovalState: string(domain.ApprovalRequestStateGranted),
		PendingApproval:   &codefluxv1.ApprovalEvent{ApprovalId: snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL, "apr_01890f3c-4a00-7abc-8def-0123456789ab"), State: string(domain.ApprovalRequestStatePending), Scope: "repository", RedactedReason: "write generated source"},
		ApprovalRevision:  11,
		Budget:            &codefluxv1.BudgetEvent{HardLimitMinor: 400, ReservedMinor: 100, ActualMinor: 75, Currency: "USD"}, BudgetRevision: 0,
		Validation: &codefluxv1.ValidationEvent{ValidationId: snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION, "val_01890f3c-4a00-7abc-8def-0123456789ab"), State: string(domain.ValidationStatePassed), RedactedSummary: "checks passed", Required: true, DiffRevision: 2}, ValidationRevision: 4,
		Checkpoint: &codefluxv1.CheckpointEvent{CheckpointId: snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT, "ckp_01890f3c-4a00-7abc-8def-0123456789ab"), TaskRevision: 9, PlanStep: "verify replay"}, CheckpointRevision: 5, CheckpointCreatedAt: timestamppb.New(time.Date(2026, 7, 31, 17, 59, 0, 0, time.UTC)),
		Recovery: &codefluxv1.RecoveryRequiredEvent{CheckpointId: snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT, "ckp_01890f3c-4a00-7abc-8def-0123456789ab"), RedactedReason: "projection mismatch", Classification: string(taskprojection.RecoveryNeedsReconcile), DivergenceSummary: "one safe divergence", ReconcileAvailable: true, PreservePatchAvailable: true, DiffRevision: 2, PlanRevision: 3, ValidationRevision: 4, EvidenceRevision: 5, GraphRevision: 6, RelatedEventIds: []*codefluxv1.StableIdentity{snapshotIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVENT, "evt_01890f3c-4a00-7abc-8def-0123456789ab")}, RelatedFiles: []string{"web/client/main.go"}}, RecoveryRevision: 6,
		ChangeAcceptance: &codefluxv1.ChangeAcceptanceEvent{State: string(domain.ChangeAcceptanceStatePending), DiffRevision: 2, PlanRevision: 3, ValidationRevision: 4, EvidenceRevision: 5, GraphRevision: 6}, ChangeAcceptanceRevision: 7,
		Tool: &codefluxv1.ToolEvent{ExecutionId: "exec-1", CommandName: "go test", State: string(domain.CommandExecutionStateRunning), RedactedSummary: "running checks"}, ToolRevision: 9,
		ReviewBindings: bindings, ReviewRevision: 8, GraphRevision: 6,
		DeniedTaskActions: []string{string(taskprojection.ActionStop)}, TaskActionPolicyReason: "Stop is denied while recovery is reconciled",
	}
}

func snapshotIdentity(kind codefluxv1.StableIdentityKind, value string) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: kind, Value: value}
}
