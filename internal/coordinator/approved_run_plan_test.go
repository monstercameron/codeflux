package coordinator

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestApprovedRunPlanInitializerBindsBeforeExactApprovalCheckpoint(
	t *testing.T,
) {
	taskID := newTaskControlTaskID(t)
	runID := newTaskControlRunID(t)
	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	order := &taskControlOrder{}
	store := &approvedRunPlanStoreStub{
		plan: storage.PlanRevision{
			TaskID:           taskID,
			Revision:         4,
			ApprovalRequired: true,
			ApprovalID:       &approvalID,
		},
		order: order,
	}
	checkpoints := &planApprovedCheckpointStub{order: order}
	service, err := NewApprovedRunPlanInitializer(store, checkpoints)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Initialize(
		t.Context(),
		InitializeApprovedRunPlan{
			TaskID:         taskID,
			RunID:          runID,
			PlanRevision:   4,
			IdempotencyKey: "approved-run-plan",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ApprovalID != approvalID ||
		result.CheckpointID.IsZero() ||
		checkpoints.approvalID != approvalID ||
		!equalTaskControlStrings(
			order.snapshot(),
			[]string{"bind", "checkpoint"},
		) {
		t.Fatalf(
			"result=%#v approval=%s order=%v",
			result,
			checkpoints.approvalID,
			order.snapshot(),
		)
	}
}

type approvedRunPlanStoreStub struct {
	plan  storage.PlanRevision
	order *taskControlOrder
}

func (stub *approvedRunPlanStoreStub) GetPlanRevision(
	context.Context,
	domain.TaskID,
	uint64,
) (storage.PlanRevision, error) {
	return stub.plan, nil
}

func (stub *approvedRunPlanStoreStub) BindRunPlan(
	_ context.Context,
	input storage.BindRunPlan,
) (storage.RunPlanBinding, error) {
	stub.order.add("bind")
	return storage.RunPlanBinding{
		TaskID:         input.TaskID,
		RunID:          input.RunID,
		PlanRevision:   input.PlanRevision,
		IdempotencyKey: input.IdempotencyKey,
	}, nil
}

type planApprovedCheckpointStub struct {
	order      *taskControlOrder
	approvalID domain.ApprovalID
}

func (stub *planApprovedCheckpointStub) CapturePlanApprovedCheckpoint(
	_ context.Context,
	_ domain.TaskID,
	_ domain.RunID,
	_ uint64,
	approvalID domain.ApprovalID,
	_ string,
) (domain.CheckpointID, error) {
	stub.order.add("checkpoint")
	stub.approvalID = approvalID
	return newTaskControlCheckpointID(), nil
}
