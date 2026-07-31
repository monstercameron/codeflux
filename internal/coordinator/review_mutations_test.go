package coordinator

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/acceptance"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestFinalizeReviewDecisionRecordsAuthoritativeTaskTransition(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	repositories := &reviewMutationRepositoriesFixture{
		task: storage.Task{ID: taskID, State: domain.TaskStateAwaitingReview, Revision: 7},
		candidate: storage.CompletionCandidate{
			TaskID: taskID, RunID: runID, PlanRevision: 3, Revision: 2,
		},
		runRevision: 11,
	}
	service := &ReviewMutationService{repositories: repositories}

	got, err := service.finalizeReviewDecision(
		t.Context(), taskID, storage.TaskReviewAccept, "reviewed", "accept-once",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.TaskStateCompleted || got.Revision != 8 {
		t.Fatalf("final task = %#v", got)
	}
	input := repositories.recorded
	if input.TaskID != taskID || input.RunID != runID ||
		input.PlanRevision != 3 || input.CompletionRevision != 2 ||
		input.ExpectedTaskRevision != 7 || input.ExpectedRunRevision != 11 ||
		input.Decision != storage.TaskReviewAccept || input.EventID.IsZero() ||
		input.EventIdempotencyKey != "accept-once:task-state-event" ||
		input.IdempotencyKey != "accept-once:task-review-decision" {
		t.Fatalf("recorded review decision = %#v", input)
	}
}

type reviewMutationRepositoriesFixture struct {
	task        storage.Task
	candidate   storage.CompletionCandidate
	runRevision uint64
	recorded    storage.RecordTaskReviewDecision
}

func (fixture *reviewMutationRepositoriesFixture) GetTask(context.Context, domain.TaskID) (storage.Task, error) {
	return fixture.task, nil
}

func (fixture *reviewMutationRepositoriesFixture) GetLatestCompletionCandidateForTask(context.Context, domain.TaskID) (storage.CompletionCandidate, error) {
	return fixture.candidate, nil
}

func (fixture *reviewMutationRepositoriesFixture) GetRunRevision(context.Context, domain.TaskID, domain.RunID) (uint64, error) {
	return fixture.runRevision, nil
}

func (fixture *reviewMutationRepositoriesFixture) RecordTaskReviewDecision(_ context.Context, input storage.RecordTaskReviewDecision) (storage.TaskReviewDecision, error) {
	fixture.recorded = input
	fixture.task.State = domain.TaskStateCompleted
	fixture.task.Revision++
	return storage.TaskReviewDecision{}, nil
}

func (*reviewMutationRepositoriesFixture) GetAcceptanceReviewHead(context.Context, domain.TaskID) (acceptance.Review, error) {
	panic("unexpected call")
}

func (*reviewMutationRepositoriesFixture) OpenAcceptanceReview(context.Context, storage.OpenAcceptanceReview) (acceptance.Review, error) {
	panic("unexpected call")
}

func (*reviewMutationRepositoriesFixture) GetAcceptanceReview(context.Context, domain.TaskID, acceptance.ReviewID) (acceptance.Review, error) {
	panic("unexpected call")
}

func (*reviewMutationRepositoriesFixture) RecordAcceptance(context.Context, storage.RecordAcceptance) (storage.AcceptanceDecision, error) {
	panic("unexpected call")
}

func (*reviewMutationRepositoriesFixture) GetRepository(context.Context, domain.RepositoryID) (storage.Repository, error) {
	panic("unexpected call")
}

func (*reviewMutationRepositoriesFixture) GetPlanRevision(context.Context, domain.TaskID, uint64) (storage.PlanRevision, error) {
	panic("unexpected call")
}
