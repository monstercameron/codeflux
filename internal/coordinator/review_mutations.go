package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/acceptance"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

type ReviewMutationService struct {
	repositories reviewMutationRepositories
	worktrees    *gitwork.Service
	repairs      *AcceptanceRepairService
}

type reviewMutationRepositories interface {
	GetAcceptanceReviewHead(context.Context, domain.TaskID) (acceptance.Review, error)
	OpenAcceptanceReview(context.Context, storage.OpenAcceptanceReview) (acceptance.Review, error)
	GetAcceptanceReview(context.Context, domain.TaskID, acceptance.ReviewID) (acceptance.Review, error)
	RecordAcceptance(context.Context, storage.RecordAcceptance) (storage.AcceptanceDecision, error)
	GetTask(context.Context, domain.TaskID) (storage.Task, error)
	GetRepository(context.Context, domain.RepositoryID) (storage.Repository, error)
	GetPlanRevision(context.Context, domain.TaskID, uint64) (storage.PlanRevision, error)
	GetLatestCompletionCandidateForTask(context.Context, domain.TaskID) (storage.CompletionCandidate, error)
	GetRunRevision(context.Context, domain.TaskID, domain.RunID) (uint64, error)
	RecordTaskReviewDecision(context.Context, storage.RecordTaskReviewDecision) (storage.TaskReviewDecision, error)
}

func NewReviewMutationService(repositories *storage.Repositories, worktrees *gitwork.Service) (*ReviewMutationService, error) {
	if repositories == nil || worktrees == nil {
		return nil, errors.New("review mutation dependencies are required")
	}
	repairs, err := NewAcceptanceRepairService(repositories, worktrees)
	if err != nil {
		return nil, err
	}
	return &ReviewMutationService{repositories: repositories, worktrees: worktrees, repairs: repairs}, nil
}

func (service *ReviewMutationService) OpenReview(ctx context.Context, taskID domain.TaskID, reportID, diff string) (acceptance.Review, error) {
	head, err := service.repositories.GetAcceptanceReviewHead(ctx, taskID)
	expected := uint64(0)
	if err == nil {
		if head.ReportID == reportID && head.DiffIdentity == diff {
			return head, nil
		}
		expected = head.Revision
	} else if !errors.Is(err, storage.ErrNotFound) {
		return acceptance.Review{}, err
	}
	return service.repositories.OpenAcceptanceReview(ctx, storage.OpenAcceptanceReview{ID: acceptance.ReviewID(reviewDigest("review", taskID.String(), reportID, diff)), TaskID: taskID, ReportID: reportID, ExpectedReviewRevision: expected, OpenedBy: "authenticated-local-user", IdempotencyKey: "review:" + reportID + ":" + diff})
}

func (service *ReviewMutationService) AcceptChange(ctx context.Context, command transport.ReviewAcceptCommand) (transport.ReviewMutationResult, error) {
	id := acceptance.AcceptanceID(reviewDigest("accept", command.TaskID.String(), command.IdempotencyKey))
	decision, err := service.repositories.RecordAcceptance(ctx, storage.RecordAcceptance{ID: id, TaskID: command.TaskID, ReviewID: command.ReviewID, ExpectedReviewRevision: command.ReviewRevision, ExpectedReportID: command.ReportID, ExpectedDiffIdentity: command.DiffIdentity, AcknowledgedCheckIDs: command.Acknowledged, ActorReference: "authenticated-local-user", AuthorityReference: "ReviewService.AcceptChange", ReasonRedacted: command.Reason, IdempotencyKey: command.IdempotencyKey})
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	task, err := service.repositories.GetTask(ctx, command.TaskID)
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	repository, err := service.repositories.GetRepository(ctx, task.RepositoryID)
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	effect, err := service.worktrees.AcceptTaskChange(ctx, gitwork.AcceptTaskChangeInput{TaskID: command.TaskID, RepositoryPath: repository.CanonicalPath, ExpectedDiffIdentity: command.DiffIdentity, AcceptanceReference: id.String(), Mode: command.Mode, AuthorName: command.AuthorName, AuthorEmail: command.AuthorEmail, CommitMessage: command.CommitMessage})
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	task, err = service.finalizeReviewDecision(ctx, command.TaskID, storage.TaskReviewAccept, command.Reason, command.IdempotencyKey)
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	return mutationResult(task, id.String(), decision.ReportID, decision.DiffIdentity, effect.CommitRevision, "", effect.PatchApplied, 0, domain.CheckpointID{}), nil
}

func (service *ReviewMutationService) RequestRepair(ctx context.Context, command transport.ReviewRepairCommand) (transport.ReviewMutationResult, error) {
	task, err := service.repositories.GetTask(ctx, command.TaskID)
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	plan, err := service.repositories.GetPlanRevision(ctx, command.TaskID, mustReview(ctx, service.repositories, command).PlanRevision)
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	nextPlan := plan.Plan
	nextPlan.Assumptions = append(nextPlan.Assumptions, "Repair is bounded to explicitly selected review targets.")
	result, err := service.repairs.RequestRepair(ctx, RequestReviewRepairInput{ID: acceptance.RepairRequestID(reviewDigest("repair", command.TaskID.String(), command.IdempotencyKey)), TaskID: command.TaskID, ThreadID: task.ThreadID, ReviewID: command.ReviewID, ExpectedReviewRevision: command.ReviewRevision, ExpectedReportID: command.ReportID, ExpectedDiffIdentity: command.DiffIdentity, Feedback: command.Feedback, Targets: command.Targets, RequestedBy: "authenticated-local-user", FeedbackMessageID: command.MessageID, FeedbackMessageKey: command.IdempotencyKey + ":message", CheckpointID: command.CheckpointID, CheckpointEvent: task.Revision, CheckpointKey: command.IdempotencyKey + ":checkpoint", NextPlan: storage.RecordPlanRevision{TaskID: command.TaskID, RequirementRevision: plan.RequirementRevision, RepositoryRevision: plan.RepositoryRevision, ContextManifestID: plan.ContextManifestID, PolicyRevision: plan.PolicyRevision, ForecastRevision: plan.ForecastRevision, BudgetID: plan.BudgetID, BudgetLimitRevision: plan.BudgetLimitRevision, BudgetSnapshotRevision: plan.BudgetSnapshotRevision, Plan: nextPlan, IdempotencyKey: command.IdempotencyKey + ":plan"}, IdempotencyKey: command.IdempotencyKey})
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	task, err = service.finalizeReviewDecision(ctx, command.TaskID, storage.TaskReviewRequestRepair, command.Feedback, command.IdempotencyKey)
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	return mutationResult(task, result.Request.ID.String(), result.Request.ReportID, result.Request.DiffIdentity, "", "", false, result.Plan.Revision, result.Checkpoint.ID), nil
}

func (service *ReviewMutationService) RejectChange(ctx context.Context, command transport.ReviewRejectCommand) (transport.ReviewMutationResult, error) {
	result, err := service.repairs.RejectAndPreserve(ctx, RejectAndPreserveInput{Checkpoint: command.CheckpointID, Rejection: storage.RecordReviewRejection{ID: acceptance.RejectionID(reviewDigest("reject", command.TaskID.String(), command.IdempotencyKey)), TaskID: command.TaskID, ReviewID: command.ReviewID, ExpectedReviewRevision: command.ReviewRevision, ExpectedReportID: command.ReportID, ExpectedDiffIdentity: command.DiffIdentity, PreservePatch: true, ReasonRedacted: command.Reason, RejectedBy: "authenticated-local-user", IdempotencyKey: command.IdempotencyKey}})
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	task, err := service.finalizeReviewDecision(ctx, command.TaskID, storage.TaskReviewAbandon, command.Reason, command.IdempotencyKey)
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	return mutationResult(task, result.Rejection.ID.String(), result.Rejection.ReportID, result.Rejection.DiffIdentity, "", result.PatchPath, false, 0, command.CheckpointID), nil
}

func (service *ReviewMutationService) RollbackTask(ctx context.Context, command transport.ReviewRollbackCommand) (transport.ReviewMutationResult, error) {
	outcome, err := service.repairs.RollbackRepair(ctx, RollbackReviewRepairInput{Intent: storage.PrepareRepairRollback{ID: acceptance.RollbackIntentID(reviewDigest("rollback", command.TaskID.String(), command.IdempotencyKey)), RepairRequestID: command.RepairID, TaskID: command.TaskID, TargetCheckpointID: command.CheckpointID, ExpectedRepairDiffIdentity: command.ExpectedDiff, Reason: domain.RollbackReasonUserRequested, RequestedBy: "authenticated-local-user", IdempotencyKey: command.IdempotencyKey}})
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	task, err := service.finalizeReviewDecision(ctx, command.TaskID, storage.TaskReviewRollback, command.Reason, command.IdempotencyKey)
	if err != nil {
		return transport.ReviewMutationResult{}, err
	}
	return mutationResult(task, outcome.IntentID.String(), "", outcome.ObservedDiffIdentity, "", "", false, 0, command.CheckpointID), nil
}

func mustReview(ctx context.Context, repositories reviewMutationRepositories, command transport.ReviewRepairCommand) acceptance.Review {
	value, _ := repositories.GetAcceptanceReview(ctx, command.TaskID, command.ReviewID)
	return value
}
func mutationResult(task storage.Task, id, report, diff, commit, path string, applied bool, plan uint64, checkpoint domain.CheckpointID) transport.ReviewMutationResult {
	return transport.ReviewMutationResult{Task: transport.TaskControlView{TaskID: task.ID, State: task.State, Revision: task.Revision, UpdatedAt: task.UpdatedAt}, ID: id, ReportID: report, DiffIdentity: diff, CommitRevision: commit, PatchPath: path, PatchApplied: applied, PlanRevision: plan, CheckpointID: checkpoint}
}
func reviewDigest(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// finalizeReviewDecision translates storage-layer staleness into the
// acceptance port's own vocabulary before returning.
//
// internal/transport is an adapter and may not depend on internal/storage
// (a sibling adapter); it must classify failures through inward ports. This
// is where that translation belongs: the application knows a stale task
// revision means the review binding changed, and says so in terms the port
// already defines.
func (service *ReviewMutationService) finalizeReviewDecision(ctx context.Context, taskID domain.TaskID, decision storage.TaskReviewDecisionKind, reason, key string) (storage.Task, error) {
	task, err := service.finalizeReviewDecisionWithStorageErrors(ctx, taskID, decision, reason, key)
	if err != nil {
		if errors.Is(err, storage.ErrStaleRevision) {
			return storage.Task{}, fmt.Errorf("%w: %w", acceptance.ErrStaleReview, err)
		}
		return storage.Task{}, err
	}
	return task, nil
}

func (service *ReviewMutationService) finalizeReviewDecisionWithStorageErrors(ctx context.Context, taskID domain.TaskID, decision storage.TaskReviewDecisionKind, reason, key string) (storage.Task, error) {
	task, err := service.repositories.GetTask(ctx, taskID)
	if err != nil {
		return storage.Task{}, err
	}
	want := map[storage.TaskReviewDecisionKind]domain.TaskState{storage.TaskReviewAccept: domain.TaskStateCompleted, storage.TaskReviewRequestRepair: domain.TaskStateRunning, storage.TaskReviewRollback: domain.TaskStateRolledBack, storage.TaskReviewAbandon: domain.TaskStateCancelled}[decision]
	if task.State == want {
		return task, nil
	}
	if task.State != domain.TaskStateAwaitingReview {
		return storage.Task{}, storage.ErrStaleRevision
	}
	candidate, err := service.repositories.GetLatestCompletionCandidateForTask(ctx, taskID)
	if err != nil {
		return storage.Task{}, err
	}
	runRevision, err := service.repositories.GetRunRevision(ctx, taskID, candidate.RunID)
	if err != nil {
		return storage.Task{}, err
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		return storage.Task{}, err
	}
	_, err = service.repositories.RecordTaskReviewDecision(ctx, storage.RecordTaskReviewDecision{TaskID: taskID, RunID: candidate.RunID, PlanRevision: candidate.PlanRevision, CompletionRevision: candidate.Revision, ExpectedTaskRevision: task.Revision, ExpectedRunRevision: runRevision, Decision: decision, ActorReference: "authenticated-local-user", AuthorityReference: "ReviewService." + string(decision), ReasonRedacted: reason, EventID: eventID, EventIdempotencyKey: key + ":task-state-event", IdempotencyKey: key + ":task-review-decision"})
	if err != nil {
		return storage.Task{}, err
	}
	return service.repositories.GetTask(ctx, taskID)
}
