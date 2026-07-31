package composer

import "codeflux.dev/codeflux/internal/domain"

type KeyboardAction string

const (
	KeyboardNone    KeyboardAction = "none"
	KeyboardBlocked KeyboardAction = "blocked"
	KeyboardSubmit  KeyboardAction = "submit"
	KeyboardNewline KeyboardAction = "newline"
)

type KeyInput struct {
	Key       string
	Shift     bool
	Control   bool
	Alt       bool
	Meta      bool
	Composing bool
	Repeat    bool
}

// ResolveKeyboard implements the composer convention: Enter submits and
// Shift+Enter inserts a newline. Composition, repeats, other modifiers, and a
// disabled submit remain inert so they cannot duplicate a command.
func ResolveKeyboard(input KeyInput, canSubmit bool) KeyboardAction {
	if input.Key != "Enter" || input.Composing || input.Repeat ||
		input.Control || input.Alt || input.Meta {
		return KeyboardNone
	}
	if input.Shift {
		return KeyboardNewline
	}
	if canSubmit {
		return KeyboardSubmit
	}
	return KeyboardBlocked
}

type TaskAction string

const (
	ActionSend             TaskAction = "send"
	ActionChangePolicy     TaskAction = "change-policy"
	ActionChangeBudget     TaskAction = "change-budget"
	ActionStop             TaskAction = "stop"
	ActionApprovePlan      TaskAction = "approve-plan"
	ActionRequestChange    TaskAction = "request-plan-change"
	ActionStart            TaskAction = "start"
	ActionPause            TaskAction = "pause"
	ActionInspectGraph     TaskAction = "inspect-graph"
	ActionAllowOnce        TaskAction = "allow-once"
	ActionAllowForTask     TaskAction = "allow-for-task"
	ActionDeny             TaskAction = "deny"
	ActionResume           TaskAction = "resume"
	ActionReview           TaskAction = "review"
	ActionInspectChecks    TaskAction = "inspect-checks"
	ActionAccept           TaskAction = "accept"
	ActionRepair           TaskAction = "repair"
	ActionReject           TaskAction = "reject"
	ActionRollback         TaskAction = "rollback"
	ActionSafeResume       TaskAction = "safe-resume"
	ActionReconcile        TaskAction = "reconcile"
	ActionPreservePatch    TaskAction = "preserve-patch"
	ActionAbandon          TaskAction = "abandon"
	ActionInspectEvidence  TaskAction = "inspect-evidence"
	ActionStartRelatedTask TaskAction = "start-related-task"
	ActionInspect          TaskAction = "inspect"
	ActionNewAttempt       TaskAction = "new-attempt"
	ActionResumeNewPlan    TaskAction = "resume-from-new-plan"
	ActionFinish           TaskAction = "finish"
)

type TaskActionSet struct {
	PrimaryMessage  string
	Actions         []TaskAction
	DisabledReasons map[TaskAction]string
}

// DisabledReason returns the authoritative explanation for an action that is
// visible in the task-action matrix but cannot currently be invoked.
func (set TaskActionSet) DisabledReason(action TaskAction) string {
	return set.DisabledReasons[action]
}

// AvailableTaskActions is the exhaustive presentation projection of the
// plan's durable task-state action matrix. It never performs a transition.
func AvailableTaskActions(taskState domain.TaskState) TaskActionSet {
	switch taskState {
	case domain.TaskStateDraft:
		return taskActions("Describe or refine the requirement", ActionSend, ActionChangePolicy, ActionChangeBudget)
	case domain.TaskStateForecasting:
		return taskActions("Estimating scope and cost", ActionStop)
	case domain.TaskStateAwaitingPlanApproval:
		return taskActions("Review plan before work begins", ActionApprovePlan, ActionRequestChange, ActionStop)
	case domain.TaskStateReady:
		return taskActions("Plan approved and prerequisites valid", ActionStart, ActionChangeBudget, ActionStop)
	case domain.TaskStateRunning:
		return taskActions("Agent is working", ActionPause, ActionStop, ActionInspectGraph)
	case domain.TaskStateAwaitingAuthority:
		return taskActions("A scoped action needs approval", ActionAllowOnce, ActionAllowForTask, ActionDeny, ActionStop)
	case domain.TaskStatePaused:
		return taskActions("Work is checkpointed", ActionResume, ActionChangeBudget, ActionReview, ActionStop)
	case domain.TaskStateValidating:
		return taskActions("Checks are running", ActionPause, ActionStop, ActionInspectChecks)
	case domain.TaskStateAwaitingReview:
		return taskActions("Work is ready for a decision", ActionReview, ActionAccept, ActionRepair, ActionReject, ActionRollback)
	case domain.TaskStateRecoveryRequired:
		return taskActions("Stored state and external state diverged", ActionSafeResume, ActionReconcile, ActionPreservePatch, ActionAbandon)
	case domain.TaskStateCompleted:
		return taskActions("Change was accepted", ActionInspectEvidence, ActionStartRelatedTask)
	case domain.TaskStateFailed:
		return taskActions("Attempt ended with unresolved failure", ActionInspect, ActionRepair, ActionPreservePatch)
	case domain.TaskStateCancelled:
		return taskActions("Active attempt was stopped", ActionInspect, ActionPreservePatch, ActionNewAttempt)
	case domain.TaskStateRolledBack:
		return taskActions("Worktree restored to checkpoint", ActionResumeNewPlan, ActionFinish)
	default:
		return TaskActionSet{PrimaryMessage: "Task state is unavailable"}
	}
}

func taskActions(message string, actions ...TaskAction) TaskActionSet {
	return TaskActionSet{PrimaryMessage: message, Actions: append([]TaskAction(nil), actions...)}
}

func (set TaskActionSet) Has(action TaskAction) bool {
	for _, candidate := range set.Actions {
		if candidate == action {
			return true
		}
	}
	return false
}

// StopImmediatelyReachable reports whether Stop is present in the flat task
// control set. The component renders every action without a menu or disclosure,
// so membership means the control is immediately reachable.
func (set TaskActionSet) StopImmediatelyReachable() bool {
	for _, action := range set.Actions {
		if action == ActionStop {
			return true
		}
	}
	return false
}
