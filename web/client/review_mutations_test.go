package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/timelineview"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func fixtureReviewScope(t *testing.T) reviewMutationScope {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	return reviewMutationScope{
		TaskID:         taskID,
		ReviewID:       "rvw_fixture",
		ReviewRevision: 4,
		ReportID:       "rpt_fixture",
		DiffIdentity:   "sha256:fixture",
	}
}

func TestAReviewDecisionMustNameEverythingItIsBoundTo(t *testing.T) {
	// A decision that named the task but not the exact review, report, and
	// diff would let the coordinator apply it to material the user never saw.
	complete := fixtureReviewScope(t)
	if !complete.Complete() {
		t.Fatal("a fully specified scope was reported incomplete")
	}
	for name, damage := range map[string]func(*reviewMutationScope){
		"no task":     func(scope *reviewMutationScope) { scope.TaskID = domain.TaskID{} },
		"no review":   func(scope *reviewMutationScope) { scope.ReviewID = " " },
		"no revision": func(scope *reviewMutationScope) { scope.ReviewRevision = 0 },
		"no report":   func(scope *reviewMutationScope) { scope.ReportID = "" },
		"no diff":     func(scope *reviewMutationScope) { scope.DiffIdentity = "" },
	} {
		t.Run(name, func(t *testing.T) {
			scope := fixtureReviewScope(t)
			damage(&scope)
			if scope.Complete() {
				t.Fatalf("a scope with %s was reported complete", name)
			}
		})
	}
}

func TestStaleUnavailabilityIsReplacedButRealRefusalsStand(t *testing.T) {
	// The four "unavailable until ReviewService can..." messages describe a
	// server that now exists, so they are replaced with live commands. Any
	// other refusal describes the task's actual state and must survive.
	scope := fixtureReviewScope(t)
	invoked := []mountedReviewMutationKind{}
	invoke := func(kind mountedReviewMutationKind) { invoked = append(invoked, kind) }

	decisions := shell.ReviewDecisionProps{
		Accept: timelineview.ActionCommandState{
			TransportMode: "authoritative-disabled", DisabledReason: reviewAcceptDependency,
		},
		Repair: timelineview.ActionCommandState{
			TransportMode: "authoritative-disabled", DisabledReason: reviewRepairDependency,
		},
		Reject: timelineview.ActionCommandState{
			TransportMode: "authoritative-disabled", DisabledReason: reviewRejectDependency,
		},
		Rollback: timelineview.ActionCommandState{
			TransportMode:  "authoritative-disabled",
			DisabledReason: "This task is not awaiting review.",
		},
	}
	bound := bindMountedReviewDecisions(decisions, scope, mountedReviewMutationState{}, invoke)

	for name, command := range map[string]timelineview.ActionCommandState{
		"accept": bound.Accept, "repair": bound.Repair, "reject": bound.Reject,
	} {
		if command.DisabledReason != "" {
			t.Errorf("%s is still refused: %s", name, command.DisabledReason)
		}
		if command.TransportMode != "authoritative" {
			t.Errorf("%s transport mode = %q, want authoritative", name, command.TransportMode)
		}
	}
	if bound.OnAccept == nil || bound.OnRepair == nil || bound.OnReject == nil {
		t.Fatal("a replaced decision has no handler, so the control is still dead")
	}
	// The real state refusal must survive untouched.
	if bound.Rollback.DisabledReason != "This task is not awaiting review." {
		t.Errorf("a genuine state refusal was overwritten: %+v", bound.Rollback)
	}
	if bound.OnRollback != nil {
		t.Error("a genuinely refused decision was given a handler")
	}

	bound.OnAccept()
	if len(invoked) != 1 || invoked[0] != mountedReviewAccept {
		t.Errorf("accept dispatched %v", invoked)
	}
}

func TestAnIncompleteScopeLeavesEveryRefusalInPlace(t *testing.T) {
	// Without the exact bindings there is nothing safe to send, so the
	// projected refusal is the honest thing to keep showing.
	decisions := shell.ReviewDecisionProps{
		Accept: timelineview.ActionCommandState{DisabledReason: reviewAcceptDependency},
	}
	bound := bindMountedReviewDecisions(
		decisions, reviewMutationScope{}, mountedReviewMutationState{},
		func(mountedReviewMutationKind) {},
	)
	if bound.Accept.DisabledReason != reviewAcceptDependency || bound.OnAccept != nil {
		t.Fatalf("an unbound decision was made actionable: %+v", bound)
	}
}

func TestOnlyOneReviewDecisionIsInFlightAtATime(t *testing.T) {
	scope := fixtureReviewScope(t)
	busy := mountedReviewMutationState{
		Kind: mountedReviewAccept, Key: "key-accept", Revision: 4, Busy: true,
	}
	decisions := shell.ReviewDecisionProps{
		Accept:   timelineview.ActionCommandState{DisabledReason: reviewAcceptDependency},
		Repair:   timelineview.ActionCommandState{DisabledReason: reviewRepairDependency},
		Reject:   timelineview.ActionCommandState{DisabledReason: reviewRejectDependency},
		Rollback: timelineview.ActionCommandState{DisabledReason: reviewRollbackDependency},
	}
	bound := bindMountedReviewDecisions(decisions, scope, busy, func(mountedReviewMutationKind) {})

	if bound.OnAccept != nil || bound.OnRepair != nil ||
		bound.OnReject != nil || bound.OnRollback != nil {
		t.Fatal("a decision remained dispatchable while another was in flight")
	}
	// The in-flight one shows its own progress and retains its identity; the
	// others say why they are waiting.
	if !bound.Accept.Busy || bound.Accept.IdempotencyKey != "key-accept" {
		t.Errorf("the in-flight decision lost its identity: %+v", bound.Accept)
	}
	if bound.Repair.Busy {
		t.Error("a decision that was not sent is showing as busy")
	}
	if !strings.Contains(bound.Repair.DisabledReason, "awaiting authoritative settlement") {
		t.Errorf("a waiting decision does not say why: %q", bound.Repair.DisabledReason)
	}
}

func TestARetriedDecisionKeepsItsRequestIdentity(t *testing.T) {
	// An accept whose outcome is unknown must be retried as the same request,
	// or an accept that actually committed could be applied twice.
	minted := 0
	newKey := func() (composer.IdempotencyKey, error) {
		minted++
		return composer.IdempotencyKey("key-1"), nil
	}
	first, ok := prepareMountedReviewMutation(
		mountedReviewMutationState{}, mountedReviewAccept, 4, newKey)
	if !ok || first.Key != "key-1" || !first.Busy {
		t.Fatalf("the first attempt was not prepared: %+v", first)
	}

	uncertain := settleMountedReviewMutation(first, false, errors.New("connection lost"))
	if uncertain.Key != "key-1" || uncertain.Kind != mountedReviewAccept {
		t.Fatalf("an uncertain outcome discarded the request identity: %+v", uncertain)
	}
	if uncertain.Busy {
		t.Error("an uncertain decision is still reported as in flight")
	}

	retry, ok := prepareMountedReviewMutation(uncertain, mountedReviewAccept, 4, newKey)
	if !ok || retry.Key != "key-1" {
		t.Fatalf("the retry did not reuse the identity: %+v", retry)
	}
	if minted != 1 {
		t.Errorf("the retry minted a second identity (%d total)", minted)
	}

	// A different decision cannot start while one is retained.
	if _, ok := prepareMountedReviewMutation(uncertain, mountedReviewReject, 4, newKey); ok {
		t.Error("a different decision started while another was retained")
	}
}

func TestARefusedDecisionDoesNotOfferTheSameRetry(t *testing.T) {
	// A refusal is final for that request. Retaining its identity would offer
	// a retry that can only be refused again.
	current := mountedReviewMutationState{
		Kind: mountedReviewAccept, Key: "key-1", Revision: 4, Busy: true,
	}
	for name, err := range map[string]error{
		"stale":  status.Error(codes.Aborted, "revision moved"),
		"denied": status.Error(codes.FailedPrecondition, "not awaiting review"),
	} {
		t.Run(name, func(t *testing.T) {
			settled := settleMountedReviewMutation(current, false, err)
			if settled.Key != "" || settled.Kind != "" {
				t.Errorf("a refused decision retained its identity: %+v", settled)
			}
			if settled.Notice == "" {
				t.Error("a refused decision said nothing about why")
			}
		})
	}
	if committed := settleMountedReviewMutation(current, true, nil); committed != (mountedReviewMutationState{}) {
		t.Errorf("a committed decision left state behind: %+v", committed)
	}
}

// recordingReviewClient captures the requests a decision sends.
type recordingReviewClient struct {
	accept   *codefluxv1.AcceptChangeRequest
	repair   *codefluxv1.ReviewServiceRequestRepairRequest
	reject   *codefluxv1.RejectChangeRequest
	rollback *codefluxv1.ReviewServiceRollbackTaskRequest
	err      error
}

func (client *recordingReviewClient) AcceptChange(
	_ context.Context, request *codefluxv1.AcceptChangeRequest, _ ...grpc.CallOption,
) (*codefluxv1.AcceptChangeResponse, error) {
	client.accept = request
	if client.err != nil {
		return nil, client.err
	}
	return &codefluxv1.AcceptChangeResponse{}, nil
}

func (client *recordingReviewClient) RequestRepair(
	_ context.Context, request *codefluxv1.ReviewServiceRequestRepairRequest, _ ...grpc.CallOption,
) (*codefluxv1.ReviewServiceRequestRepairResponse, error) {
	client.repair = request
	if client.err != nil {
		return nil, client.err
	}
	return &codefluxv1.ReviewServiceRequestRepairResponse{}, nil
}

func (client *recordingReviewClient) RejectChange(
	_ context.Context, request *codefluxv1.RejectChangeRequest, _ ...grpc.CallOption,
) (*codefluxv1.RejectChangeResponse, error) {
	client.reject = request
	if client.err != nil {
		return nil, client.err
	}
	return &codefluxv1.RejectChangeResponse{}, nil
}

func (client *recordingReviewClient) RollbackTask(
	_ context.Context, request *codefluxv1.ReviewServiceRollbackTaskRequest, _ ...grpc.CallOption,
) (*codefluxv1.ReviewServiceRollbackTaskResponse, error) {
	client.rollback = request
	if client.err != nil {
		return nil, client.err
	}
	return &codefluxv1.ReviewServiceRollbackTaskResponse{}, nil
}

func TestEveryDecisionCarriesTheRevisionsItWasMadeAgainst(t *testing.T) {
	// This is what makes it safe to act on a screen that may be stale: the
	// coordinator rejects a decision whose bindings no longer match.
	scope := fixtureReviewScope(t)
	client := &recordingReviewClient{}

	committed, err := executeMountedReviewMutationWithClient(
		t.Context(), client, mountedReviewAccept, scope, "key-accept")
	if err != nil || !committed {
		t.Fatalf("accept did not commit: %v", err)
	}
	if client.accept.GetReviewId() != scope.ReviewID ||
		client.accept.GetExpectedReviewRevision() != scope.ReviewRevision ||
		client.accept.GetExpectedReportId() != scope.ReportID ||
		client.accept.GetExpectedDiffIdentity() != scope.DiffIdentity {
		t.Errorf("accept dropped a binding: %+v", client.accept)
	}
	if client.accept.GetControl().GetIdempotencyKey() != "key-accept" {
		t.Errorf("accept did not carry its request identity: %+v", client.accept.GetControl())
	}

	if _, err := executeMountedReviewMutationWithClient(
		t.Context(), client, mountedReviewReject, scope, "key-reject"); err != nil {
		t.Fatalf("reject failed: %v", err)
	}
	if client.reject.GetExpectedReviewRevision() != scope.ReviewRevision {
		t.Errorf("reject dropped its review revision: %+v", client.reject)
	}
	if _, err := executeMountedReviewMutationWithClient(
		t.Context(), client, mountedReviewRepair, scope, "key-repair"); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if client.repair.GetRequest().GetControl().GetIdempotencyKey() != "key-repair" {
		t.Errorf("repair dropped its request identity: %+v", client.repair)
	}
	if _, err := executeMountedReviewMutationWithClient(
		t.Context(), client, mountedReviewRollback, scope, "key-rollback"); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if client.rollback.GetRequest().GetTaskId() == nil {
		t.Errorf("rollback named no task: %+v", client.rollback)
	}
}

func TestADecisionIsNotSentWithoutAClientScopeOrIdentity(t *testing.T) {
	scope := fixtureReviewScope(t)
	for name, send := range map[string]func() (bool, error){
		"no client": func() (bool, error) {
			return executeMountedReviewMutationWithClient(
				t.Context(), nil, mountedReviewAccept, scope, "key")
		},
		"no scope": func() (bool, error) {
			return executeMountedReviewMutationWithClient(
				t.Context(), &recordingReviewClient{}, mountedReviewAccept,
				reviewMutationScope{}, "key")
		},
		"no identity": func() (bool, error) {
			return executeMountedReviewMutationWithClient(
				t.Context(), &recordingReviewClient{}, mountedReviewAccept, scope, "  ")
		},
		"unknown decision": func() (bool, error) {
			return executeMountedReviewMutationWithClient(
				t.Context(), &recordingReviewClient{}, "invented", scope, "key")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := send(); err == nil {
				t.Fatalf("a decision with %s was sent", name)
			}
		})
	}
}
