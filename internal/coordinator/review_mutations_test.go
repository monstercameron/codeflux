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
		t.Context(), taskID, storage.TaskReviewAccept, "commit-abc123", "reviewed", "accept-once",
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

// TestMEM001a_FinalizeReviewDecisionClosesTheOpenEpisodeWithTheRealOutcome
// proves the MEM-001a wiring end to end at the coordinator level: once
// RecordTaskReviewDecision durably records a human's terminal decision,
// finalizeReviewDecisionWithStorageErrors closes that task's still-open
// episode with the outcome the decision actually resolved to (not the
// uniform Unresolved MEM-001 left behind) and the revision identity the
// call site passed through. If closeReviewDecisionEpisode's call were
// deleted, or episodeOutcomeForReviewDecision's ok-guard were inverted,
// repositories.episodeCloseCalled would stay false for every case here and
// this test would fail.
func TestMEM001a_FinalizeReviewDecisionClosesTheOpenEpisodeWithTheRealOutcome(t *testing.T) {
	cases := []struct {
		name     string
		decision storage.TaskReviewDecisionKind
		outcome  domain.EpisodeOutcome
	}{
		{"accept", storage.TaskReviewAccept, domain.EpisodeOutcomeAccepted},
		{"request-repair", storage.TaskReviewRequestRepair, domain.EpisodeOutcomeRejected},
		{"abandon", storage.TaskReviewAbandon, domain.EpisodeOutcomeRejected},
		{"rollback", storage.TaskReviewRollback, domain.EpisodeOutcomeAbandoned},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			taskID, err := domain.NewTaskID()
			if err != nil {
				t.Fatal(err)
			}
			runID, err := domain.NewRunID()
			if err != nil {
				t.Fatal(err)
			}
			episodeID, err := domain.NewEpisodeID()
			if err != nil {
				t.Fatal(err)
			}
			repositories := &reviewMutationRepositoriesFixture{
				task:        storage.Task{ID: taskID, State: domain.TaskStateAwaitingReview, Revision: 1},
				candidate:   storage.CompletionCandidate{TaskID: taskID, RunID: runID, PlanRevision: 1, Revision: 1},
				runRevision: 1,
				episode:     storage.Episode{ID: episodeID, Status: domain.EpisodeStatusOpen, StartingRevision: "start-rev"},
			}
			service := &ReviewMutationService{repositories: repositories}
			wantRevision := "rev-" + testCase.name
			if _, err := service.finalizeReviewDecision(t.Context(), taskID, testCase.decision, wantRevision, "reason", testCase.name+"-once"); err != nil {
				t.Fatal(err)
			}
			if !repositories.episodeCloseCalled {
				t.Fatalf("%s: closeReviewDecisionEpisode did not close the open episode", testCase.name)
			}
			if repositories.closedEpisode.EpisodeID != episodeID {
				t.Fatalf("%s: closed episode ID = %v, want %v", testCase.name, repositories.closedEpisode.EpisodeID, episodeID)
			}
			if repositories.closedEpisode.Outcome != testCase.outcome {
				t.Fatalf("%s: closed episode outcome = %v, want %v", testCase.name, repositories.closedEpisode.Outcome, testCase.outcome)
			}
			if repositories.closedEpisode.EndingRevision != wantRevision {
				t.Fatalf("%s: closed episode ending revision = %q, want %q", testCase.name, repositories.closedEpisode.EndingRevision, wantRevision)
			}
		})
	}
}

// TestMEM001a_FinalizeReviewDecisionDoesNotReopenAnAlreadyClosedEpisode
// proves closeReviewDecisionEpisode's Open-status guard: RollbackTask is
// reachable from TaskStateCompleted, not only TaskStateAwaitingReview, so
// its target episode may already be closed (from an earlier Accept). If the
// guard in closeReviewDecisionEpisode (episode_lifecycle.go) were removed,
// this call would attempt a second CloseEpisode against an already-closed
// row -- exactly what episodes_immutable_after_close exists to refuse
// against a real store -- and repositories.episodeCloseCalled would flip to
// true here.
func TestMEM001a_FinalizeReviewDecisionDoesNotReopenAnAlreadyClosedEpisode(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	episodeID, err := domain.NewEpisodeID()
	if err != nil {
		t.Fatal(err)
	}
	repositories := &reviewMutationRepositoriesFixture{
		task:        storage.Task{ID: taskID, State: domain.TaskStateAwaitingReview, Revision: 1},
		candidate:   storage.CompletionCandidate{TaskID: taskID, RunID: runID, PlanRevision: 1, Revision: 1},
		runRevision: 1,
		episode:     storage.Episode{ID: episodeID, Status: domain.EpisodeStatusClosed, StartingRevision: "start-rev"},
	}
	service := &ReviewMutationService{repositories: repositories}
	if _, err := service.finalizeReviewDecision(t.Context(), taskID, storage.TaskReviewRollback, "rev", "reason", "rollback-once"); err != nil {
		t.Fatal(err)
	}
	if repositories.episodeCloseCalled {
		t.Fatal("closeReviewDecisionEpisode attempted to close an already-closed episode")
	}
}

type reviewMutationRepositoriesFixture struct {
	task        storage.Task
	candidate   storage.CompletionCandidate
	runRevision uint64
	recorded    storage.RecordTaskReviewDecision

	// episode, episodeErr, closedEpisode, and episodeCloseCalled let a test
	// observe exactly what closeReviewDecisionEpisode (episode_lifecycle.go,
	// MEM-001a) did with the episode finalizeReviewDecision resolved, without
	// a real SQLite-backed episode.
	episode            storage.Episode
	episodeErr         error
	closedEpisode      storage.CloseEpisode
	episodeCloseCalled bool
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

func (fixture *reviewMutationRepositoriesFixture) GetEpisodeByTask(context.Context, domain.TaskID) (storage.Episode, error) {
	if fixture.episodeErr != nil {
		return storage.Episode{}, fixture.episodeErr
	}
	return fixture.episode, nil
}

func (fixture *reviewMutationRepositoriesFixture) CloseEpisode(_ context.Context, input storage.CloseEpisode) (storage.Episode, error) {
	fixture.episodeCloseCalled = true
	fixture.closedEpisode = input
	fixture.episode.Status = domain.EpisodeStatusClosed
	return fixture.episode, nil
}
