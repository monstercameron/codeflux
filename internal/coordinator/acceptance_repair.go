package coordinator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/acceptance"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/evidence"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
)

type acceptanceReportSource interface {
	GetLatestFinalEvidenceReport(context.Context, domain.TaskID) (evidence.Report, error)
}

type acceptanceDiffSource interface {
	ComputeDiffIdentity(context.Context, domain.TaskID) (string, error)
}

// LiveRequiredCheckSource resolves controller state, including pending and
// running reruns, for the exact required checks in a sealed report.
type LiveRequiredCheckSource interface {
	ObserveRequiredChecks(context.Context, domain.TaskID, evidence.Report) ([]acceptance.RequiredCheck, error)
}

// AcceptanceObservationResolver composes durable report state, the live Git
// worktree, and the validation controller into storage's trusted observation.
type AcceptanceObservationResolver struct {
	reports     acceptanceReportSource
	diffs       acceptanceDiffSource
	validations LiveRequiredCheckSource
}

func NewAcceptanceObservationResolver(
	reports acceptanceReportSource,
	diffs acceptanceDiffSource,
	validations LiveRequiredCheckSource,
) (*AcceptanceObservationResolver, error) {
	if reports == nil || diffs == nil || validations == nil {
		return nil, errors.New("acceptance report, diff, and live-validation sources are required")
	}
	return &AcceptanceObservationResolver{reports: reports, diffs: diffs, validations: validations}, nil
}

func (resolver *AcceptanceObservationResolver) ObserveAcceptance(ctx context.Context, taskID domain.TaskID) (storage.AcceptanceObservation, error) {
	if resolver == nil {
		return storage.AcceptanceObservation{}, errors.New("acceptance observation resolver is unavailable")
	}
	report, err := resolver.reports.GetLatestFinalEvidenceReport(ctx, taskID)
	if err != nil {
		return storage.AcceptanceObservation{}, err
	}
	diff, err := resolver.diffs.ComputeDiffIdentity(ctx, taskID)
	if err != nil {
		return storage.AcceptanceObservation{}, err
	}
	checks, err := resolver.validations.ObserveRequiredChecks(ctx, taskID, report)
	if err != nil {
		return storage.AcceptanceObservation{}, err
	}
	return storage.AcceptanceObservation{ReportID: report.ID, DiffIdentity: diff, RequiredChecks: checks}, nil
}

type acceptanceRepairRepository interface {
	GetAcceptanceReview(context.Context, domain.TaskID, acceptance.ReviewID) (acceptance.Review, error)
	AppendMessage(context.Context, storage.AppendMessage) (storage.Message, error)
	RecordPlanRevision(context.Context, storage.RecordPlanRevision) (storage.PlanRevision, error)
	RequestAcceptanceRepair(context.Context, acceptance.RepairRequest) (acceptance.RepairRequest, error)
	GetCheckpoint(context.Context, domain.CheckpointID) (storage.Checkpoint, error)
	PrepareAcceptanceRepairRollback(context.Context, storage.PrepareRepairRollback) (storage.RepairRollbackIntent, error)
	CompleteAcceptanceRepairRollback(context.Context, storage.CompleteRepairRollback) (storage.RepairRollbackOutcome, error)
	RejectAcceptanceReview(context.Context, storage.RecordReviewRejection) (storage.ReviewRejection, error)
}

type acceptanceRepairWorktree interface {
	ComputeDiffIdentity(context.Context, domain.TaskID) (string, error)
	CreateCheckpoint(context.Context, gitwork.CreateCheckpointInput) (storage.Checkpoint, error)
	RestoreCheckpoint(context.Context, gitwork.RestoreCheckpointInput) (storage.WorktreeBinding, error)
	PreserveCheckpointPatch(context.Context, domain.CheckpointID) (string, bool, error)
}

type AcceptanceRepairService struct {
	repository acceptanceRepairRepository
	worktree   acceptanceRepairWorktree
}

func NewAcceptanceRepairService(repository acceptanceRepairRepository, worktree acceptanceRepairWorktree) (*AcceptanceRepairService, error) {
	if repository == nil || worktree == nil {
		return nil, errors.New("acceptance repair repository and worktree service are required")
	}
	return &AcceptanceRepairService{repository: repository, worktree: worktree}, nil
}

type RequestReviewRepairInput struct {
	ID                     acceptance.RepairRequestID
	TaskID                 domain.TaskID
	ReviewID               acceptance.ReviewID
	ExpectedReviewRevision uint64
	ExpectedReportID       string
	ExpectedDiffIdentity   string
	Feedback               string
	Targets                []acceptance.RepairTarget
	RequestedBy            string
	ThreadID               domain.ThreadID
	FeedbackMessageID      domain.MessageID
	FeedbackMessageKey     string
	CheckpointID           domain.CheckpointID
	CheckpointEvent        uint64
	CheckpointKey          string
	NextPlan               storage.RecordPlanRevision
	IdempotencyKey         string
}

type RequestReviewRepairResult struct {
	Checkpoint storage.Checkpoint
	Message    storage.Message
	Plan       storage.PlanRevision
	Request    acceptance.RepairRequest
}

// RequestRepair creates the checkpoint and superseding plan before sealing
// the repair request that binds both lineage nodes.
func (service *AcceptanceRepairService) RequestRepair(ctx context.Context, input RequestReviewRepairInput) (RequestReviewRepairResult, error) {
	if service == nil || input.TaskID.IsZero() || input.ThreadID.IsZero() || input.ReviewID.String() == "" || input.ExpectedReviewRevision == 0 || input.CheckpointID.IsZero() || input.FeedbackMessageID.IsZero() || strings.TrimSpace(input.Feedback) == "" {
		return RequestReviewRepairResult{}, errors.New("repair orchestration input is incomplete")
	}
	review, err := service.repository.GetAcceptanceReview(ctx, input.TaskID, input.ReviewID)
	if err != nil {
		return RequestReviewRepairResult{}, err
	}
	currentDiff, err := service.worktree.ComputeDiffIdentity(ctx, input.TaskID)
	if err != nil {
		return RequestReviewRepairResult{}, err
	}
	if review.Revision != input.ExpectedReviewRevision || review.ReportID != input.ExpectedReportID || review.DiffIdentity != input.ExpectedDiffIdentity || currentDiff != review.DiffIdentity {
		return RequestReviewRepairResult{}, acceptance.ErrStaleReview
	}
	checkpoint, err := service.worktree.CreateCheckpoint(ctx, gitwork.CreateCheckpointInput{
		ID: input.CheckpointID, TaskID: input.TaskID, EventSequence: input.CheckpointEvent, IdempotencyKey: input.CheckpointKey,
	})
	if err != nil {
		return RequestReviewRepairResult{}, err
	}
	message, err := service.repository.AppendMessage(ctx, storage.AppendMessage{
		ID: input.FeedbackMessageID, ThreadID: input.ThreadID,
		Role: storage.MessageRoleUser, BodyRedacted: input.Feedback, IdempotencyKey: input.FeedbackMessageKey,
	})
	if err != nil {
		return RequestReviewRepairResult{}, err
	}
	previous := review.PlanRevision
	next := input.NextPlan
	next.TaskID = input.TaskID
	next.SupersedesRevision = &previous
	next.RedirectMessageID = &message.ID
	plan, err := service.repository.RecordPlanRevision(ctx, next)
	if err != nil {
		return RequestReviewRepairResult{}, err
	}
	request, err := service.repository.RequestAcceptanceRepair(ctx, acceptance.RepairRequest{
		ID: input.ID, ReviewID: input.ReviewID, TaskID: input.TaskID,
		ReviewRevision: review.Revision, ReportID: review.ReportID, DiffIdentity: review.DiffIdentity,
		PreviousPlanRevision: review.PlanRevision, NewPlanRevision: plan.Revision,
		PreRepairCheckpointID: checkpoint.ID, Feedback: input.Feedback, Targets: input.Targets,
		RequestedBy: input.RequestedBy, IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		return RequestReviewRepairResult{}, err
	}
	return RequestReviewRepairResult{Checkpoint: checkpoint, Message: message, Plan: plan, Request: request}, nil
}

type RollbackReviewRepairInput struct {
	Intent storage.PrepareRepairRollback
}

// RollbackRepair records authority, performs the Git restore, verifies the
// resulting diff, and only then records a restored outcome.
func (service *AcceptanceRepairService) RollbackRepair(ctx context.Context, input RollbackReviewRepairInput) (storage.RepairRollbackOutcome, error) {
	intent, err := service.repository.PrepareAcceptanceRepairRollback(ctx, input.Intent)
	if err != nil {
		return storage.RepairRollbackOutcome{}, err
	}
	_, restoreErr := service.worktree.RestoreCheckpoint(ctx, gitwork.RestoreCheckpointInput{
		TaskID: intent.TaskID, CheckpointID: intent.TargetCheckpointID, DiscardCurrentApproved: true,
	})
	observed, observeErr := service.worktree.ComputeDiffIdentity(ctx, intent.TaskID)
	if observeErr != nil {
		return storage.RepairRollbackOutcome{}, errors.Join(restoreErr, observeErr)
	}
	completion := storage.CompleteRepairRollback{
		IntentID: intent.ID, ObservedDiffIdentity: observed,
		IdempotencyKey: input.Intent.IdempotencyKey + ":outcome",
	}
	if restoreErr != nil {
		completion.Outcome = storage.RepairRollbackFailed
		completion.DetailRedacted = "The pre-repair checkpoint restore failed; the observed diff was retained."
	} else {
		completion.Outcome = storage.RepairRollbackRestored
		completion.DetailRedacted = "Restored and verified the exact pre-repair checkpoint diff."
	}
	outcome, journalErr := service.repository.CompleteAcceptanceRepairRollback(ctx, completion)
	if restoreErr != nil || journalErr != nil {
		return outcome, errors.Join(restoreErr, journalErr)
	}
	return outcome, nil
}

type RejectAndPreserveInput struct {
	Rejection  storage.RecordReviewRejection
	Checkpoint domain.CheckpointID
}

type RejectAndPreserveResult struct {
	Rejection storage.ReviewRejection
	PatchPath string
}

// RejectAndPreserve materializes a recoverable patch before resolving the
// review as rejected. A failed or mismatched export leaves the review open.
func (service *AcceptanceRepairService) RejectAndPreserve(ctx context.Context, input RejectAndPreserveInput) (RejectAndPreserveResult, error) {
	checkpoint, err := service.repository.GetCheckpoint(ctx, input.Checkpoint)
	if err != nil {
		return RejectAndPreserveResult{}, err
	}
	if checkpoint.TaskID != input.Rejection.TaskID || checkpoint.WorktreeDiffHash != input.Rejection.ExpectedDiffIdentity {
		return RejectAndPreserveResult{}, fmt.Errorf("preservation checkpoint does not match the reviewed diff: %w", acceptance.ErrStaleReview)
	}
	path, available, err := service.worktree.PreserveCheckpointPatch(ctx, checkpoint.ID)
	if err != nil {
		return RejectAndPreserveResult{}, err
	}
	if !available || strings.TrimSpace(path) == "" {
		return RejectAndPreserveResult{}, errors.New("review patch could not be preserved")
	}
	input.Rejection.PreservePatch = true
	rejection, err := service.repository.RejectAcceptanceReview(ctx, input.Rejection)
	if err != nil {
		return RejectAndPreserveResult{}, err
	}
	return RejectAndPreserveResult{Rejection: rejection, PatchPath: path}, nil
}
