// Package taskactionview binds authoritative task projections to the mounted
// composer action surface without giving presentation code transport authority.
package taskactionview

import (
	"strings"

	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

type Callbacks struct {
	InspectGraph func()
	Review       func()
}

type binding struct {
	invoke  func()
	command taskcontrols.CommandState
}

// Bind derives every visible action from AvailableTaskActions, binds only
// mounted authoritative callbacks, and disables every other action before
// invocation with an action-specific accessible reason.
func Bind(
	props composer.Props,
	task taskprojection.TaskProjection,
	connection taskprojection.ConnectionProjection,
	controls *taskcontrols.Props,
	callbacks Callbacks,
) composer.Props {
	bindings := authoritativeBindings(controls, callbacks)
	decisions := taskprojection.AvailableTaskActions(task, connection)
	set := composer.TaskActionSet{
		PrimaryMessage:  composer.AvailableTaskActions(task.State).PrimaryMessage,
		Actions:         make([]composer.TaskAction, 0, len(decisions)),
		DisabledReasons: make(map[composer.TaskAction]string),
	}
	for _, decision := range decisions {
		action := composer.TaskAction(decision.Kind)
		set.Actions = append(set.Actions, action)
		binding, supported := bindings[decision.Kind]
		switch {
		case !decision.Enabled:
			set.DisabledReasons[action] = decision.Reason
		case !supported || binding.invoke == nil:
			set.DisabledReasons[action] = UnavailableReason(decision.Kind)
		case binding.command.Busy:
			set.DisabledReasons[action] = "This command is awaiting authoritative settlement."
		case strings.TrimSpace(binding.command.DisabledReason) != "":
			set.DisabledReasons[action] = binding.command.DisabledReason
		}
	}
	props.View.Task = set
	props.OnTaskAction = func(action composer.TaskAction) {
		if !set.Has(action) || set.DisabledReason(action) != "" {
			return
		}
		binding, ok := bindings[taskprojection.ActionKind(action)]
		if ok && binding.invoke != nil {
			binding.invoke()
		}
	}
	return props
}

func authoritativeBindings(
	controls *taskcontrols.Props,
	callbacks Callbacks,
) map[taskprojection.ActionKind]binding {
	bindings := map[taskprojection.ActionKind]binding{
		taskprojection.ActionInspectGraph: {invoke: callbacks.InspectGraph},
		taskprojection.ActionReview:       {invoke: callbacks.Review},
	}
	if controls == nil {
		return bindings
	}
	stop := controls.OnStop
	if controls.StopConfirm.Required {
		stop = controls.OnStopConfirm
	}
	bindings[taskprojection.ActionPause] = binding{invoke: controls.OnPause, command: controls.Controls.Pause}
	bindings[taskprojection.ActionResume] = binding{invoke: controls.OnResume, command: controls.Controls.Resume}
	bindings[taskprojection.ActionStop] = binding{invoke: stop, command: controls.Controls.Stop}
	bindings[taskprojection.ActionChangeBudget] = binding{invoke: controls.OnBudgetAdjust, command: controls.Controls.AdjustBudget}
	bindings[taskprojection.ActionPreservePatch] = binding{invoke: controls.OnPreservePatch, command: controls.Recovery.PreservePatch}
	bindings[taskprojection.ActionSafeResume] = binding{invoke: controls.OnSafeResume, command: controls.Recovery.SafeResume}
	bindings[taskprojection.ActionReconcile] = binding{invoke: controls.OnReconcile, command: controls.Recovery.Reconcile}
	return bindings
}

// UnavailableReason explains the missing current-milestone implementation for
// one otherwise state-eligible action without allowing a doomed click.
func UnavailableReason(action taskprojection.ActionKind) string {
	switch action {
	case taskprojection.ActionSend:
		return "Send is provided by the primary composer control and requires a retained non-empty draft plus a live authoritative session."
	case taskprojection.ActionChangePolicy:
		return "Policy changes are provided by the composer Options control and apply to the next submitted requirement."
	case taskprojection.ActionApprovePlan:
		return "Plan approval is unavailable until the coordinator atomically binds the exact plan, transitions the task to Ready, and publishes the durable session event."
	case taskprojection.ActionRequestChange:
		return "Plan changes are unavailable until the coordinator can persist review feedback, build a superseding plan revision, and reset approval authority."
	case taskprojection.ActionAllowOnce, taskprojection.ActionAllowForTask, taskprojection.ActionDeny:
		return "Approval resolution is unavailable until the coordinator atomically records the scoped permission decision and revalidates the exact pending action before continuing it."
	case taskprojection.ActionAccept:
		return "Acceptance is unavailable until ReviewService verifies the task, diff, plan, validation, and evidence revisions and applies the exact reviewed Git change."
	case taskprojection.ActionRepair:
		return "Repair is unavailable until ReviewService can bind feedback to the reviewed revisions, create a new plan and checkpoint lineage, and resume bounded execution."
	case taskprojection.ActionReject:
		return "Rejection is unavailable until ReviewService can atomically record the exact review-bound decision, preserve the patch, and publish the durable session event."
	case taskprojection.ActionRollback:
		return "Rollback is unavailable until ReviewService can verify and restore the bound checkpoint and publish the resulting task and worktree revisions."
	case taskprojection.ActionStart:
		return "Starting is unavailable until TaskService exposes an idempotent command bound to the approved plan and current task revision."
	case taskprojection.ActionInspectGraph:
		return "Graph inspection is unavailable until the selected authoritative task graph is mounted."
	case taskprojection.ActionReview:
		return "Review is unavailable until a current authoritative review projection is mounted."
	case taskprojection.ActionInspectChecks:
		return "Check inspection is unavailable until the authoritative validation detail view is mounted."
	case taskprojection.ActionSafeResume:
		return "Safe resume is unavailable until the coordinator verifies the checkpoint and exposes the scoped resume command."
	case taskprojection.ActionReconcile:
		return "Reconcile is unavailable until the coordinator exposes the typed worktree reconciliation command."
	case taskprojection.ActionPreservePatch:
		return "Patch preservation is unavailable until the coordinator exposes the typed preserve-patch command."
	case taskprojection.ActionAbandon:
		return "Abandon is unavailable until the coordinator can preserve recovery evidence and atomically classify the resulting task state."
	case taskprojection.ActionInspectEvidence:
		return "Evidence inspection is unavailable until the accepted evidence view is mounted."
	case taskprojection.ActionStartRelatedTask:
		return "Starting a related task is unavailable until task lineage creation is implemented."
	case taskprojection.ActionInspect:
		return "Attempt inspection is unavailable until the authoritative attempt detail view is mounted."
	case taskprojection.ActionNewAttempt:
		return "A new attempt is unavailable until TaskService can bind it to the cancelled task and preserved patch."
	case taskprojection.ActionResumeNewPlan:
		return "Resume from a new plan is unavailable until a superseding plan revision is approved."
	case taskprojection.ActionFinish:
		return "Finish is unavailable until the rolled-back task can be closed with durable evidence."
	default:
		return "This action has no authoritative implementation in the current milestone."
	}
}
