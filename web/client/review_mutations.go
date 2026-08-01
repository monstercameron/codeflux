package main

import (
	"context"
	"errors"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/timelineview"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mountedReviewMutationKind names one review decision.
//
// These four were previously presented as permanently unavailable, each with a
// message saying the capability did not exist yet. It does: AcceptChange,
// RequestRepair, RejectChange, and RollbackTask are all implemented and
// registered on the coordinator. The controls were dead against a working
// server, which is worse than an error — nothing tells you the product can do
// the thing you are being refused.
type mountedReviewMutationKind string

const (
	mountedReviewAccept   mountedReviewMutationKind = "accept"
	mountedReviewRepair   mountedReviewMutationKind = "repair"
	mountedReviewReject   mountedReviewMutationKind = "reject"
	mountedReviewRollback mountedReviewMutationKind = "rollback"
)

// mountedReviewMutationState is one in-flight decision.
//
// It retains the request identity across a retry for the same reason task
// mutations do: a decision whose outcome is unknown must be retried as the
// same request, or an accept that actually committed could be applied twice.
type mountedReviewMutationState struct {
	Kind     mountedReviewMutationKind
	Key      composer.IdempotencyKey
	Revision uint64
	Busy     bool
	Notice   string
}

// reviewMutationClient is the narrow surface these decisions need.
type reviewMutationClient interface {
	AcceptChange(
		context.Context, *codefluxv1.AcceptChangeRequest, ...grpc.CallOption,
	) (*codefluxv1.AcceptChangeResponse, error)
	RequestRepair(
		context.Context, *codefluxv1.ReviewServiceRequestRepairRequest, ...grpc.CallOption,
	) (*codefluxv1.ReviewServiceRequestRepairResponse, error)
	RejectChange(
		context.Context, *codefluxv1.RejectChangeRequest, ...grpc.CallOption,
	) (*codefluxv1.RejectChangeResponse, error)
	RollbackTask(
		context.Context, *codefluxv1.ReviewServiceRollbackTaskRequest, ...grpc.CallOption,
	) (*codefluxv1.ReviewServiceRollbackTaskResponse, error)
}

// reviewMutationScope is the exact material a decision is bound to.
//
// Every field is required. A decision that named a task but not the review
// revision, report, and diff it was made against would let the coordinator
// apply it to material the user never saw.
type reviewMutationScope struct {
	TaskID         domain.TaskID
	ReviewID       string
	ReviewRevision uint64
	ReportID       string
	DiffIdentity   string
}

// Complete reports whether a decision can be bound at all.
func (scope reviewMutationScope) Complete() bool {
	return !scope.TaskID.IsZero() &&
		strings.TrimSpace(scope.ReviewID) != "" &&
		scope.ReviewRevision != 0 &&
		strings.TrimSpace(scope.ReportID) != "" &&
		strings.TrimSpace(scope.DiffIdentity) != ""
}

// prepareMountedReviewMutation mints or retains a request identity.
func prepareMountedReviewMutation(
	current mountedReviewMutationState,
	kind mountedReviewMutationKind,
	revision uint64,
	newKey taskMutationKeyFactory,
) (mountedReviewMutationState, bool) {
	if current.Busy || current.Kind != "" && current.Kind != kind || revision == 0 {
		return current, false
	}
	if current.Key == "" {
		if newKey == nil {
			return current, false
		}
		key, err := newKey()
		if err != nil || key == "" {
			return current, false
		}
		current = mountedReviewMutationState{Kind: kind, Key: key, Revision: revision}
	}
	current.Busy = true
	current.Notice = ""
	return current, true
}

// settleMountedReviewMutation records what the coordinator said.
//
// A decision the coordinator refused clears the retained identity: the refusal
// is final for that request, and retaining it would offer a retry that can only
// be refused again. A decision whose outcome is unknown keeps it, because that
// is the case where retrying the same request is the safe thing to do.
func settleMountedReviewMutation(
	current mountedReviewMutationState,
	committed bool,
	err error,
) mountedReviewMutationState {
	if err == nil && committed {
		return mountedReviewMutationState{}
	}
	switch status.Code(err) {
	case codes.Aborted:
		return mountedReviewMutationState{
			Notice: "The review changed before this decision committed. " +
				"Authoritative state was refreshed.",
		}
	case codes.FailedPrecondition, codes.PermissionDenied,
		codes.NotFound, codes.InvalidArgument:
		return mountedReviewMutationState{
			Notice: "The coordinator denied this review decision against the exact " +
				"revisions it was bound to. Authoritative state was refreshed.",
		}
	}
	current.Busy = false
	current.Notice = "The coordinator did not confirm this review decision. " +
		"Its request identity is retained for a safe retry."
	return current
}

// bindMountedReviewDecisions replaces projected refusals with live commands.
//
// A control is bound only when the projection already permits the action and
// the scope is complete. Where either is missing the projected refusal stands,
// because it explains the actual state rather than the state of the code.
func bindMountedReviewDecisions(
	decisions shell.ReviewDecisionProps,
	scope reviewMutationScope,
	current mountedReviewMutationState,
	invoke func(mountedReviewMutationKind),
) shell.ReviewDecisionProps {
	if !scope.Complete() {
		return decisions
	}
	if current.Busy {
		reason := "Another review decision is awaiting authoritative settlement."
		decisions.Accept = busyReviewCommand(current, mountedReviewAccept, reason)
		decisions.Repair = busyReviewCommand(current, mountedReviewRepair, reason)
		decisions.Reject = busyReviewCommand(current, mountedReviewReject, reason)
		decisions.Rollback = busyReviewCommand(current, mountedReviewRollback, reason)
		decisions.OnAccept, decisions.OnRepair = nil, nil
		decisions.OnReject, decisions.OnRollback = nil, nil
		return decisions
	}

	retainedReason := ""
	if current.Kind != "" {
		retainedReason = "The earlier " + string(current.Kind) +
			" decision has an uncertain outcome. Retry it with its retained request " +
			"identity before making another."
	}
	bind := func(
		projected timelineview.ActionCommandState,
		kind mountedReviewMutationKind,
	) (timelineview.ActionCommandState, func()) {
		// A projection that refuses for a reason other than the missing
		// implementation still refuses: the task state, not the code, decides.
		if strings.TrimSpace(projected.DisabledReason) != "" &&
			!reviewDependencyPlaceholder(projected.DisabledReason) {
			return projected, nil
		}
		if current.Kind != "" && current.Kind != kind {
			return timelineview.ActionCommandState{DisabledReason: retainedReason}, nil
		}
		command := timelineview.ActionCommandState{
			TransportMode:  "authoritative",
			IdempotencyKey: retainedReviewKey(current, kind),
		}
		if invoke == nil {
			command.TransportMode = "authoritative-disabled"
			command.DisabledReason = "Review decision dispatch is unavailable."
			return command, nil
		}
		return command, func() { invoke(kind) }
	}
	decisions.Accept, decisions.OnAccept = bind(decisions.Accept, mountedReviewAccept)
	decisions.Repair, decisions.OnRepair = bind(decisions.Repair, mountedReviewRepair)
	decisions.Reject, decisions.OnReject = bind(decisions.Reject, mountedReviewReject)
	decisions.Rollback, decisions.OnRollback = bind(decisions.Rollback, mountedReviewRollback)
	return decisions
}

// reviewDependencyPlaceholder reports whether a refusal is one of the stale
// "not implemented yet" messages rather than a real state refusal.
//
// The distinction matters: a task that is not in review must still refuse, and
// only the placeholder is safe to replace with a live command.
func reviewDependencyPlaceholder(reason string) bool {
	for _, placeholder := range []string{
		reviewAcceptDependency, reviewRepairDependency,
		reviewRejectDependency, reviewRollbackDependency,
	} {
		if reason == placeholder {
			return true
		}
	}
	return false
}

func busyReviewCommand(
	current mountedReviewMutationState,
	kind mountedReviewMutationKind,
	reason string,
) timelineview.ActionCommandState {
	command := timelineview.ActionCommandState{
		TransportMode: "authoritative", DisabledReason: reason,
	}
	if current.Kind == kind {
		command.Busy = true
		command.IdempotencyKey = string(current.Key)
	}
	return command
}

func retainedReviewKey(
	state mountedReviewMutationState,
	kind mountedReviewMutationKind,
) string {
	if state.Kind == kind {
		return string(state.Key)
	}
	return ""
}

// executeMountedReviewMutationWithClient sends one bound decision.
//
// Every request carries the exact review, report, and diff identity the user
// was looking at. The coordinator rejects a decision whose bindings no longer
// match, which is what makes it safe to act on a screen that may be stale.
func executeMountedReviewMutationWithClient(
	ctx context.Context,
	client reviewMutationClient,
	kind mountedReviewMutationKind,
	scope reviewMutationScope,
	key string,
) (bool, error) {
	if client == nil || !scope.Complete() || strings.TrimSpace(key) == "" {
		return false, errors.New("invalid review decision request")
	}
	control := &codefluxv1.MutationControl{IdempotencyKey: key}
	switch kind {
	case mountedReviewAccept:
		response, err := client.AcceptChange(ctx, &codefluxv1.AcceptChangeRequest{
			Control: control, TaskId: taskIdentity(scope.TaskID),
			ReviewId: scope.ReviewID, ExpectedReviewRevision: scope.ReviewRevision,
			ExpectedReportId: scope.ReportID, ExpectedDiffIdentity: scope.DiffIdentity,
		})
		return response != nil && err == nil, err
	case mountedReviewRepair:
		response, err := client.RequestRepair(
			ctx, &codefluxv1.ReviewServiceRequestRepairRequest{
				Request: &codefluxv1.RequestRepairRequest{
					Control: control, TaskId: taskIdentity(scope.TaskID),
				},
			})
		return response != nil && err == nil, err
	case mountedReviewReject:
		response, err := client.RejectChange(ctx, &codefluxv1.RejectChangeRequest{
			Control: control, TaskId: taskIdentity(scope.TaskID),
			ReviewId: scope.ReviewID, ExpectedReviewRevision: scope.ReviewRevision,
		})
		return response != nil && err == nil, err
	case mountedReviewRollback:
		response, err := client.RollbackTask(
			ctx, &codefluxv1.ReviewServiceRollbackTaskRequest{
				Request: &codefluxv1.RollbackTaskRequest{
					Control: control, TaskId: taskIdentity(scope.TaskID),
				},
			})
		return response != nil && err == nil, err
	default:
		return false, errors.New("unsupported review decision")
	}
}
