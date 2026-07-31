package coordinator

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/acceptance"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/evidence"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
)

func TestAcceptanceObservationResolverUsesDurableReportLiveGitAndValidation(t *testing.T) {
	ids := acceptanceServiceIDs(t)
	report := evidence.Report{ID: strings.Repeat("2", 64), TaskID: ids.task, DiffIdentity: strings.Repeat("3", 64)}
	reports := &acceptanceReportStub{report: report}
	worktree := &acceptanceWorktreeStub{diff: strings.Repeat("4", 64)}
	validations := &liveChecksStub{checks: []acceptance.RequiredCheck{{ID: "unit", Status: acceptance.CheckRunning}}}
	resolver, err := NewAcceptanceObservationResolver(reports, worktree, validations)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := resolver.ObserveAcceptance(t.Context(), ids.task)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ReportID != report.ID || observed.DiffIdentity != worktree.diff || !slices.Equal(observed.RequiredChecks, validations.checks) {
		t.Fatalf("authoritative observation = %#v", observed)
	}
}

func TestAcceptanceRepairServiceCreatesCheckpointAndSupersedingPlanLineage(t *testing.T) {
	ids := acceptanceServiceIDs(t)
	review := acceptance.Review{
		ID: acceptance.ReviewID(strings.Repeat("1", 64)), TaskID: ids.task, Revision: 4,
		ReportID: strings.Repeat("2", 64), DiffIdentity: strings.Repeat("3", 64), PlanRevision: 7,
	}
	repository := &acceptanceRepairRepositoryStub{review: review}
	worktree := &acceptanceWorktreeStub{diff: review.DiffIdentity}
	service, err := NewAcceptanceRepairService(repository, worktree)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RequestRepair(t.Context(), RequestReviewRepairInput{
		ID: acceptance.RepairRequestID(strings.Repeat("4", 64)), TaskID: ids.task,
		ThreadID: ids.thread, ReviewID: review.ID, ExpectedReviewRevision: review.Revision,
		ExpectedReportID: review.ReportID, ExpectedDiffIdentity: review.DiffIdentity,
		Feedback: "Repair the selected failed check and hunk.", RequestedBy: "user:test",
		Targets:           []acceptance.RepairTarget{{Kind: acceptance.RepairTargetValidation, ID: "unit"}, {Kind: acceptance.RepairTargetHunk, ID: "hunk-2"}},
		FeedbackMessageID: ids.message, FeedbackMessageKey: "repair-message",
		CheckpointID: ids.checkpoint, CheckpointEvent: 12, CheckpointKey: "repair-checkpoint",
		NextPlan: storage.RecordPlanRevision{IdempotencyKey: "repair-plan"}, IdempotencyKey: "repair-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(repository.calls, []string{"review", "message", "plan", "repair"}) ||
		!slices.Equal(worktree.calls, []string{"diff", "checkpoint"}) {
		t.Fatalf("orchestration order repository=%v worktree=%v", repository.calls, worktree.calls)
	}
	if repository.planInput.SupersedesRevision == nil || *repository.planInput.SupersedesRevision != review.PlanRevision ||
		repository.planInput.RedirectMessageID == nil || *repository.planInput.RedirectMessageID != ids.message ||
		result.Request.PreRepairCheckpointID != ids.checkpoint || result.Request.NewPlanRevision != review.PlanRevision+1 {
		t.Fatalf("repair lineage result=%#v plan=%#v", result, repository.planInput)
	}
}

func TestAcceptanceRepairServiceRollbackPerformsRestoreBeforeJournalingSuccess(t *testing.T) {
	ids := acceptanceServiceIDs(t)
	checkpointDiff := strings.Repeat("5", 64)
	repository := &acceptanceRepairRepositoryStub{}
	worktree := &acceptanceWorktreeStub{diff: checkpointDiff}
	service, err := NewAcceptanceRepairService(repository, worktree)
	if err != nil {
		t.Fatal(err)
	}
	intent := storage.PrepareRepairRollback{
		ID: acceptance.RollbackIntentID(strings.Repeat("6", 64)), RepairRequestID: acceptance.RepairRequestID(strings.Repeat("7", 64)),
		TaskID: ids.task, TargetCheckpointID: ids.checkpoint, ExpectedRepairDiffIdentity: strings.Repeat("8", 64),
		Reason: domain.RollbackReasonUserRequested, RequestedBy: "user:test", IdempotencyKey: "rollback",
	}
	outcome, err := service.RollbackRepair(t.Context(), RollbackReviewRepairInput{Intent: intent})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(worktree.calls, []string{"restore", "diff"}) || outcome.Outcome != storage.RepairRollbackRestored || outcome.ObservedDiffIdentity != checkpointDiff {
		t.Fatalf("rollback outcome=%#v calls=%v", outcome, worktree.calls)
	}
}

func TestAcceptanceRepairServiceDoesNotRejectWhenPatchPreservationFails(t *testing.T) {
	ids := acceptanceServiceIDs(t)
	diff := strings.Repeat("9", 64)
	repository := &acceptanceRepairRepositoryStub{checkpoint: storage.Checkpoint{ID: ids.checkpoint, TaskID: ids.task, WorktreeDiffHash: diff}}
	worktree := &acceptanceWorktreeStub{diff: diff, preserveAvailable: false}
	service, err := NewAcceptanceRepairService(repository, worktree)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RejectAndPreserve(t.Context(), RejectAndPreserveInput{
		Checkpoint: ids.checkpoint,
		Rejection: storage.RecordReviewRejection{
			ID: acceptance.RejectionID(strings.Repeat("a", 64)), TaskID: ids.task,
			ReviewID: acceptance.ReviewID(strings.Repeat("b", 64)), ExpectedReviewRevision: 1,
			ExpectedReportID: strings.Repeat("c", 64), ExpectedDiffIdentity: diff,
			ReasonRedacted: "Reject after preserving.", RejectedBy: "user:test", IdempotencyKey: "reject",
		},
	})
	if err == nil || slices.Contains(repository.calls, "reject") {
		t.Fatalf("non-preserved rejection error=%v calls=%v", err, repository.calls)
	}
}

type acceptanceIDs struct {
	task       domain.TaskID
	thread     domain.ThreadID
	message    domain.MessageID
	checkpoint domain.CheckpointID
}

func acceptanceServiceIDs(t *testing.T) acceptanceIDs {
	t.Helper()
	task, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	message, err := domain.NewMessageID()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := domain.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	return acceptanceIDs{task: task, thread: thread, message: message, checkpoint: checkpoint}
}

type acceptanceReportStub struct{ report evidence.Report }

func (stub *acceptanceReportStub) GetLatestFinalEvidenceReport(context.Context, domain.TaskID) (evidence.Report, error) {
	return stub.report, nil
}

type liveChecksStub struct{ checks []acceptance.RequiredCheck }

func (stub *liveChecksStub) ObserveRequiredChecks(context.Context, domain.TaskID, evidence.Report) ([]acceptance.RequiredCheck, error) {
	return slices.Clone(stub.checks), nil
}

type acceptanceWorktreeStub struct {
	diff              string
	calls             []string
	restoreErr        error
	preservePath      string
	preserveAvailable bool
}

func (stub *acceptanceWorktreeStub) ComputeDiffIdentity(context.Context, domain.TaskID) (string, error) {
	stub.calls = append(stub.calls, "diff")
	return stub.diff, nil
}
func (stub *acceptanceWorktreeStub) CreateCheckpoint(_ context.Context, input gitwork.CreateCheckpointInput) (storage.Checkpoint, error) {
	stub.calls = append(stub.calls, "checkpoint")
	return storage.Checkpoint{ID: input.ID, TaskID: input.TaskID, State: domain.CheckpointStateReady, WorktreeDiffHash: stub.diff}, nil
}
func (stub *acceptanceWorktreeStub) RestoreCheckpoint(context.Context, gitwork.RestoreCheckpointInput) (storage.WorktreeBinding, error) {
	stub.calls = append(stub.calls, "restore")
	return storage.WorktreeBinding{}, stub.restoreErr
}
func (stub *acceptanceWorktreeStub) PreserveCheckpointPatch(context.Context, domain.CheckpointID) (string, bool, error) {
	stub.calls = append(stub.calls, "preserve")
	return stub.preservePath, stub.preserveAvailable, nil
}

type acceptanceRepairRepositoryStub struct {
	review      acceptance.Review
	checkpoint  storage.Checkpoint
	calls       []string
	planInput   storage.RecordPlanRevision
	repairInput acceptance.RepairRequest
}

func (stub *acceptanceRepairRepositoryStub) GetAcceptanceReview(context.Context, domain.TaskID, acceptance.ReviewID) (acceptance.Review, error) {
	stub.calls = append(stub.calls, "review")
	return stub.review, nil
}
func (stub *acceptanceRepairRepositoryStub) AppendMessage(_ context.Context, input storage.AppendMessage) (storage.Message, error) {
	stub.calls = append(stub.calls, "message")
	return storage.Message{ID: input.ID, ThreadID: input.ThreadID, Role: input.Role, BodyRedacted: input.BodyRedacted}, nil
}
func (stub *acceptanceRepairRepositoryStub) RecordPlanRevision(_ context.Context, input storage.RecordPlanRevision) (storage.PlanRevision, error) {
	stub.calls = append(stub.calls, "plan")
	stub.planInput = input
	return storage.PlanRevision{TaskID: input.TaskID, Revision: *input.SupersedesRevision + 1}, nil
}
func (stub *acceptanceRepairRepositoryStub) RequestAcceptanceRepair(_ context.Context, input acceptance.RepairRequest) (acceptance.RepairRequest, error) {
	stub.calls = append(stub.calls, "repair")
	stub.repairInput = input
	input.CreatedAt = time.Unix(1, 0).UTC()
	return input, nil
}
func (stub *acceptanceRepairRepositoryStub) GetCheckpoint(context.Context, domain.CheckpointID) (storage.Checkpoint, error) {
	stub.calls = append(stub.calls, "get-checkpoint")
	return stub.checkpoint, nil
}
func (stub *acceptanceRepairRepositoryStub) PrepareAcceptanceRepairRollback(_ context.Context, input storage.PrepareRepairRollback) (storage.RepairRollbackIntent, error) {
	stub.calls = append(stub.calls, "prepare-rollback")
	return storage.RepairRollbackIntent{PrepareRepairRollback: input}, nil
}
func (stub *acceptanceRepairRepositoryStub) CompleteAcceptanceRepairRollback(_ context.Context, input storage.CompleteRepairRollback) (storage.RepairRollbackOutcome, error) {
	stub.calls = append(stub.calls, "complete-rollback")
	return storage.RepairRollbackOutcome{CompleteRepairRollback: input}, nil
}
func (stub *acceptanceRepairRepositoryStub) RejectAcceptanceReview(_ context.Context, input storage.RecordReviewRejection) (storage.ReviewRejection, error) {
	stub.calls = append(stub.calls, "reject")
	if !input.PreservePatch {
		return storage.ReviewRejection{}, errors.New("patch was not preserved")
	}
	return storage.ReviewRejection{RecordReviewRejection: input}, nil
}
