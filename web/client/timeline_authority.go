package main

import (
	"strings"

	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"codeflux.dev/codeflux/web/frontend/timelineview"
)

const (
	approvalResolutionDependency = "Approval resolution is unavailable until the coordinator atomically records the scoped permission decision and revalidates the exact pending action before continuing it."
	planApprovalDependency       = "Plan approval is unavailable until the coordinator atomically binds the exact plan, transitions the task to Ready, and publishes the durable session event."
	planChangeDependency         = "Plan changes are unavailable until the coordinator can persist review feedback, build a superseding plan revision, and reset approval authority."
	reviewAcceptDependency       = "Acceptance is unavailable until ReviewService verifies the task, diff, plan, validation, and evidence revisions and applies the exact reviewed Git change."
	reviewRepairDependency       = "Repair is unavailable until ReviewService can bind feedback to the reviewed revisions, create a new plan and checkpoint lineage, and resume bounded execution."
	reviewRejectDependency       = "Rejection is unavailable until ReviewService can atomically record the exact review-bound decision, preserve the patch, and publish the durable session event."
	reviewRollbackDependency     = "Rollback is unavailable until ReviewService can verify and restore the bound checkpoint and publish the resulting task and worktree revisions."
)

// reviewDecisionBinder turns projected review refusals into live commands.
//
// It is a parameter rather than a direct call because the identity a decision
// must carry -- the review, its revision, the report, the diff -- comes from
// the loaded review material, not from the task projection. Only the caller
// that has loaded that material can supply it, and a caller that has not must
// leave the refusals in place.
type reviewDecisionBinder func(shell.ReviewDecisionProps) shell.ReviewDecisionProps

func bindAuthoritativeTimelineActions(
	props shell.TimelineControlProps,
	task taskprojection.TaskProjection,
	connection taskprojection.ConnectionProjection,
	onOpenReview func(),
	onCloseReview func(),
	bindDecisions reviewDecisionBinder,
) shell.TimelineControlProps {
	props.Actions.OnApproval = nil
	props.Actions.OnApprovePlan = nil
	props.Actions.OnRequestPlanChange = nil
	props.Actions.ApprovalActionCommand = func(
		rawID string,
		action timelinecard.ApprovalAction,
	) timelineview.ApprovalCommandState {
		if !task.Approval.Present || rawID != task.Approval.ID.String() {
			return unavailableTimelineCommand("Approval identity does not match the current authoritative request.")
		}
		kind := taskprojection.ActionKind(action)
		switch kind {
		case taskprojection.ActionAllowOnce, taskprojection.ActionAllowForTask, taskprojection.ActionDeny:
			return projectedUnavailableTimelineCommand(
				task, connection, kind, approvalResolutionDependency,
			)
		default:
			return unavailableTimelineCommand("Approval decision is not recognized.")
		}
	}
	props.Actions.ApprovalCommand = func(rawID string) timelineview.ApprovalCommandState {
		if !task.Approval.Present || rawID != task.Approval.ID.String() {
			return unavailableTimelineCommand("Approval identity does not match the current authoritative request.")
		}
		return projectedUnavailableTimelineCommand(
			task, connection, taskprojection.ActionAllowOnce, approvalResolutionDependency,
		)
	}
	props.Actions.ApprovePlanCommand = func(revision uint64) timelineview.ActionCommandState {
		if !task.Plan.Present || revision != task.Plan.Revision {
			return unavailableTimelineCommand("Plan revision is no longer current; refresh before acting.")
		}
		return projectedUnavailableTimelineCommand(
			task, connection, taskprojection.ActionApprovePlan, planApprovalDependency,
		)
	}
	props.Actions.PlanChangeCommand = func(revision uint64) timelineview.ActionCommandState {
		if !task.Plan.Present || revision != task.Plan.Revision {
			return unavailableTimelineCommand("Plan revision is no longer current; refresh before acting.")
		}
		return projectedUnavailableTimelineCommand(
			task, connection, taskprojection.ActionRequestChange, planChangeDependency,
		)
	}

	props.OnOpenReview = nil
	props.OnCloseReview = nil
	props.Actions.OnOpenReview = nil
	if task.Review.Present && projectedActionEnabled(task, connection, taskprojection.ActionReview) {
		props.OnOpenReview = onOpenReview
		props.OnCloseReview = onCloseReview
		if onOpenReview != nil {
			props.Actions.OnOpenReview = func(reviewID string) {
				if strings.TrimSpace(reviewID) != "" {
					onOpenReview()
				}
			}
		}
	}
	props.ReviewBindings = shell.ReviewBindingView{
		Task: task.Revision, Diff: task.Review.Bindings.Diff,
		Plan: task.Review.Bindings.Plan, Validation: task.Review.Bindings.Validation,
		Evidence: task.Review.Bindings.Evidence,
	}
	props.ReviewDecisions = shell.ReviewDecisionProps{
		Accept: projectedUnavailableTimelineCommand(
			task, connection, taskprojection.ActionAccept, reviewAcceptDependency,
		),
		Repair: projectedUnavailableTimelineCommand(
			task, connection, taskprojection.ActionRepair, reviewRepairDependency,
		),
		Reject: projectedUnavailableTimelineCommand(
			task, connection, taskprojection.ActionReject, reviewRejectDependency,
		),
		Rollback: projectedUnavailableTimelineCommand(
			task, connection, taskprojection.ActionRollback, reviewRollbackDependency,
		),
	}
	if bindDecisions != nil {
		props.ReviewDecisions = bindDecisions(props.ReviewDecisions)
	}
	return props
}

func projectedUnavailableTimelineCommand(
	task taskprojection.TaskProjection,
	connection taskprojection.ConnectionProjection,
	action taskprojection.ActionKind,
	missingImplementation string,
) timelineview.ActionCommandState {
	for _, decision := range taskprojection.AvailableTaskActions(task, connection) {
		if decision.Kind != action {
			continue
		}
		if !decision.Enabled {
			return unavailableTimelineCommand(decision.Reason)
		}
		return unavailableTimelineCommand(missingImplementation)
	}
	return unavailableTimelineCommand("This action is not available in the current authoritative task state.")
}

func unavailableTimelineCommand(reason string) timelineview.ActionCommandState {
	return timelineview.ActionCommandState{
		TransportMode: "authoritative-disabled", DisabledReason: reason,
	}
}

func projectedActionEnabled(
	task taskprojection.TaskProjection,
	connection taskprojection.ConnectionProjection,
	action taskprojection.ActionKind,
) bool {
	for _, decision := range taskprojection.AvailableTaskActions(task, connection) {
		if decision.Kind == action {
			return decision.Enabled
		}
	}
	return false
}
