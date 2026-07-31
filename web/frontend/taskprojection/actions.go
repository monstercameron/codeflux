package taskprojection

import "codeflux.dev/codeflux/internal/domain"

type ConnectionProjection string

const (
	ConnectionConnecting   ConnectionProjection = "connecting"
	ConnectionLive         ConnectionProjection = "live"
	ConnectionReplaying    ConnectionProjection = "replaying"
	ConnectionDegraded     ConnectionProjection = "degraded"
	ConnectionDisconnected ConnectionProjection = "disconnected"
	ConnectionIncompatible ConnectionProjection = "incompatible"
	ConnectionUnauthorized ConnectionProjection = "unauthorized"
)

func AllConnectionProjections() []ConnectionProjection {
	return []ConnectionProjection{
		ConnectionConnecting, ConnectionLive, ConnectionReplaying,
		ConnectionDegraded, ConnectionDisconnected,
		ConnectionIncompatible, ConnectionUnauthorized,
	}
}

func (connection ConnectionProjection) IsValid() bool {
	for _, candidate := range AllConnectionProjections() {
		if connection == candidate {
			return true
		}
	}
	return false
}

func (connection ConnectionProjection) MutationCertain() bool {
	return connection == ConnectionLive
}

type ActionKind string

const (
	ActionSend             ActionKind = "send"
	ActionChangePolicy     ActionKind = "change-policy"
	ActionChangeBudget     ActionKind = "change-budget"
	ActionStop             ActionKind = "stop"
	ActionApprovePlan      ActionKind = "approve-plan"
	ActionRequestChange    ActionKind = "request-plan-change"
	ActionStart            ActionKind = "start"
	ActionPause            ActionKind = "pause"
	ActionInspectGraph     ActionKind = "inspect-graph"
	ActionAllowOnce        ActionKind = "allow-once"
	ActionAllowForTask     ActionKind = "allow-for-task"
	ActionDeny             ActionKind = "deny"
	ActionResume           ActionKind = "resume"
	ActionReview           ActionKind = "review"
	ActionInspectChecks    ActionKind = "inspect-checks"
	ActionAccept           ActionKind = "accept"
	ActionRepair           ActionKind = "repair"
	ActionReject           ActionKind = "reject"
	ActionRollback         ActionKind = "rollback"
	ActionSafeResume       ActionKind = "safe-resume"
	ActionReconcile        ActionKind = "reconcile"
	ActionPreservePatch    ActionKind = "preserve-patch"
	ActionAbandon          ActionKind = "abandon"
	ActionInspectEvidence  ActionKind = "inspect-evidence"
	ActionStartRelatedTask ActionKind = "start-related-task"
	ActionInspect          ActionKind = "inspect"
	ActionNewAttempt       ActionKind = "new-attempt"
	ActionResumeNewPlan    ActionKind = "resume-from-new-plan"
	ActionFinish           ActionKind = "finish"
)

func (kind ActionKind) IsValid() bool {
	switch kind {
	case ActionSend, ActionChangePolicy, ActionChangeBudget, ActionStop,
		ActionApprovePlan, ActionRequestChange, ActionStart, ActionPause,
		ActionInspectGraph, ActionAllowOnce, ActionAllowForTask, ActionDeny,
		ActionResume, ActionReview, ActionInspectChecks, ActionAccept,
		ActionRepair, ActionReject, ActionRollback, ActionSafeResume,
		ActionReconcile, ActionPreservePatch, ActionAbandon,
		ActionInspectEvidence, ActionStartRelatedTask, ActionInspect,
		ActionNewAttempt, ActionResumeNewPlan, ActionFinish:
		return true
	default:
		return false
	}
}

type TaskAction struct {
	Kind    ActionKind
	Enabled bool
	Reason  string
}

func (action TaskAction) Mutation() bool {
	switch action.Kind {
	case ActionInspectGraph, ActionReview, ActionInspectChecks,
		ActionInspectEvidence, ActionInspect:
		return false
	default:
		return true
	}
}

// AvailableTaskActions exhaustively projects the plan's Draft-through-
// Rolled-back matrix. Matrix actions remain present but disabled with a safe
// explanation when connection certainty, policy, pending command, approval,
// review freshness, or recovery classification prevents invocation.
func AvailableTaskActions(task TaskProjection, connection ConnectionProjection) []TaskAction {
	base := matrixActions(task.State)
	result := make([]TaskAction, 0, len(base))
	for _, kind := range base {
		decision := TaskAction{Kind: kind, Enabled: true}
		switch {
		case !connection.IsValid():
			decision.Enabled = false
			decision.Reason = "Connection state is unavailable"
		case decision.Mutation() && !connection.MutationCertain():
			decision.Enabled = false
			decision.Reason = connectionReason(connection)
		case decision.Mutation() && task.PendingCommand.OwnsKey():
			decision.Enabled = false
			decision.Reason = "Another command is awaiting authoritative settlement"
		case task.Policy.Denies(kind):
			decision.Enabled = false
			decision.Reason = task.Policy.SafeReason
		case requiresPendingApproval(kind) && !task.Approval.Pending():
			decision.Enabled = false
			decision.Reason = "No current scoped approval is pending"
		case kind == ActionApprovePlan &&
			(!task.Plan.Present || task.Plan.Approval != domain.ApprovalRequestStatePending):
			decision.Enabled = false
			decision.Reason = "The current plan is not awaiting approval"
		case kind == ActionStart &&
			(!task.Plan.Present || task.Plan.Approval != domain.ApprovalRequestStateGranted):
			decision.Enabled = false
			decision.Reason = "The current plan is not approved"
		case kind == ActionAccept && reviewStale(task):
			decision.Enabled = false
			decision.Reason = "Review bindings changed; refresh review before accepting"
		case kind == ActionAccept && !validationAllowsAcceptance(task.Validation):
			decision.Enabled = false
			decision.Reason = "Required validation has not passed or been explicitly acknowledged"
		case !recoveryActionAllowed(kind, task.Recovery):
			decision.Enabled = false
			decision.Reason = "The recovery assessment does not permit this action"
		}
		if !decision.Enabled && decision.Reason == "" {
			decision.Reason = "Action is unavailable"
		}
		result = append(result, decision)
	}
	return result
}

func matrixActions(state domain.TaskState) []ActionKind {
	switch state {
	case domain.TaskStateDraft:
		return []ActionKind{ActionSend, ActionChangePolicy, ActionChangeBudget}
	case domain.TaskStateForecasting:
		return []ActionKind{ActionStop}
	case domain.TaskStateAwaitingPlanApproval:
		return []ActionKind{ActionApprovePlan, ActionRequestChange, ActionStop}
	case domain.TaskStateReady:
		return []ActionKind{ActionStart, ActionChangeBudget, ActionStop}
	case domain.TaskStateRunning:
		return []ActionKind{ActionPause, ActionStop, ActionInspectGraph}
	case domain.TaskStateAwaitingAuthority:
		return []ActionKind{ActionAllowOnce, ActionAllowForTask, ActionDeny, ActionStop}
	case domain.TaskStatePaused:
		return []ActionKind{ActionResume, ActionChangeBudget, ActionReview, ActionStop}
	case domain.TaskStateValidating:
		return []ActionKind{ActionPause, ActionStop, ActionInspectChecks}
	case domain.TaskStateAwaitingReview:
		return []ActionKind{ActionReview, ActionAccept, ActionRepair, ActionReject, ActionRollback}
	case domain.TaskStateRecoveryRequired:
		return []ActionKind{ActionSafeResume, ActionReconcile, ActionPreservePatch, ActionAbandon}
	case domain.TaskStateCompleted:
		return []ActionKind{ActionInspectEvidence, ActionStartRelatedTask}
	case domain.TaskStateFailed:
		return []ActionKind{ActionInspect, ActionRepair, ActionPreservePatch}
	case domain.TaskStateCancelled:
		return []ActionKind{ActionInspect, ActionPreservePatch, ActionNewAttempt}
	case domain.TaskStateRolledBack:
		return []ActionKind{ActionResumeNewPlan, ActionFinish}
	default:
		return nil
	}
}

func connectionReason(connection ConnectionProjection) string {
	switch connection {
	case ConnectionConnecting:
		return "Connecting; authoritative delivery is not yet certain"
	case ConnectionReplaying:
		return "Replaying durable events before mutations can resume"
	case ConnectionDegraded:
		return "Connection is degraded and delivery certainty is unknown"
	case ConnectionDisconnected:
		return "Disconnected; the backend task may still be running"
	case ConnectionIncompatible:
		return "Client and coordinator versions are incompatible"
	case ConnectionUnauthorized:
		return "The current session is not authorized"
	default:
		return "Connection state is unavailable"
	}
}

func requiresPendingApproval(kind ActionKind) bool {
	return kind == ActionAllowOnce || kind == ActionAllowForTask || kind == ActionDeny
}

func recoveryActionAllowed(kind ActionKind, classification RecoveryClassification) bool {
	switch kind {
	case ActionSafeResume:
		return classification == RecoverySafeResume
	case ActionReconcile:
		return classification == RecoveryNeedsReconcile
	case ActionPreservePatch:
		return classification == RecoveryPreserveOnly ||
			classification == RecoveryNeedsReconcile ||
			classification == RecoveryAmbiguousOutcome
	case ActionAbandon:
		return classification != RecoveryNone
	default:
		return true
	}
}

func reviewStale(task TaskProjection) bool {
	if !task.Review.Present || !task.Acceptance.Present {
		return true
	}
	return task.Review.Bindings != task.Acceptance.Bindings
}

func validationAllowsAcceptance(validation ValidationProjection) bool {
	if !validation.Present {
		return false
	}
	if !validation.Required {
		return true
	}
	switch validation.State {
	case domain.ValidationStatePassed:
		return true
	case domain.ValidationStateFailed, domain.ValidationStateWaived:
		return validation.Acknowledged
	default:
		return false
	}
}
