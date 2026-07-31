package acceptance

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestAcceptanceGateRequiresCurrentReviewAndExactAcknowledgements(t *testing.T) {
	review := validReview(t)
	gate, err := EvaluateAcceptance(review, review.ReportID, review.DiffIdentity, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gate.CanAccept || len(gate.RunningCheckIDs) != 1 || len(gate.AcknowledgementCheckIDs) != 2 || !errors.Is(AcceptanceGateError(gate), ErrValidationRunning) {
		t.Fatalf("initial gate = %#v", gate)
	}

	review.RequiredChecks[0].Status = CheckPassed
	gate, err = EvaluateAcceptance(review, review.ReportID, review.DiffIdentity, []string{"failed", "waived"})
	if err != nil {
		t.Fatal(err)
	}
	if !gate.CanAccept || AcceptanceGateError(gate) != nil {
		t.Fatalf("acknowledged gate = %#v", gate)
	}

	if _, err := EvaluateAcceptance(review, review.ReportID, review.DiffIdentity, []string{"passed"}); !errors.Is(err, ErrInvalidReview) {
		t.Fatalf("irrelevant acknowledgement error = %v", err)
	}
	gate, err = EvaluateAcceptance(review, strings.Repeat("9", 64), review.DiffIdentity, []string{"failed", "waived"})
	if err != nil || !gate.Stale || !errors.Is(AcceptanceGateError(gate), ErrStaleReview) {
		t.Fatalf("stale report gate = %#v, %v", gate, err)
	}
	gate, err = EvaluateAcceptance(review, review.ReportID, strings.Repeat("8", 64), []string{"failed", "waived"})
	if err != nil || !gate.Stale {
		t.Fatalf("stale diff gate = %#v, %v", gate, err)
	}
}

func TestAcceptanceGateUsesExactLiveRequiredCheckState(t *testing.T) {
	review := validReview(t)
	for index := range review.RequiredChecks {
		review.RequiredChecks[index].Status = CheckPassed
	}
	live := slices.Clone(review.RequiredChecks)
	live[0].Status = CheckRunning
	gate, err := EvaluateAcceptanceWithLiveChecks(
		review, review.ReportID, review.DiffIdentity, live, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if gate.CanAccept || !slices.Equal(gate.RunningCheckIDs, []string{"running"}) {
		t.Fatalf("live running gate = %#v", gate)
	}
	if _, err := EvaluateAcceptanceWithLiveChecks(
		review, review.ReportID, review.DiffIdentity, live[:len(live)-1], nil,
	); !errors.Is(err, ErrInvalidReview) {
		t.Fatalf("incomplete live check set error = %v", err)
	}
}

func TestAcceptanceGateHardBlocksNonAcknowledgementDispositions(t *testing.T) {
	for _, status := range []CheckStatus{CheckSkipped, CheckUnavailable, CheckCancelled, CheckInvalidated} {
		t.Run(string(status), func(t *testing.T) {
			review := validReview(t)
			review.RequiredChecks = []RequiredCheck{{ID: "blocked", Status: status}}
			gate, err := EvaluateAcceptance(review, review.ReportID, review.DiffIdentity, nil)
			if err != nil || gate.CanAccept || !errors.Is(AcceptanceGateError(gate), ErrRequiredCheckBlocked) {
				t.Fatalf("gate = %#v, %v", gate, err)
			}
		})
	}
}

func validReview(t *testing.T) Review {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	return Review{
		ID: ReviewID(strings.Repeat("1", 64)), TaskID: taskID, Revision: 1,
		ReportID: strings.Repeat("2", 64), DiffIdentity: strings.Repeat("3", 64), PlanRevision: 2,
		RequiredChecks: []RequiredCheck{{ID: "running", Status: CheckRunning}, {ID: "failed", Status: CheckFailed}, {ID: "waived", Status: CheckWaived}, {ID: "passed", Status: CheckPassed}},
		OpenedBy:       "user:fixture", IdempotencyKey: "review-fixture", OpenedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	}
}
