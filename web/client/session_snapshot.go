package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/sessionprojection"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"google.golang.org/grpc"
)

var errSessionProjectionSnapshotMalformed = errors.New("authoritative session projection snapshot is malformed")

type sessionProjectionSnapshotClient interface {
	GetSessionSnapshot(context.Context, *codefluxv1.GetSessionSnapshotRequest, ...grpc.CallOption) (*codefluxv1.GetSessionSnapshotResponse, error)
}

// fetchSessionProjectionSnapshot obtains one sequence-bound projection and
// verifies it belongs to the mounted session before any frontend state changes.
func fetchSessionProjectionSnapshot(
	ctx context.Context,
	client sessionProjectionSnapshotClient,
	sessionID domain.SessionID,
	threadID domain.ThreadID,
	taskID domain.TaskID,
) (sessionprojection.SessionSnapshot, error) {
	if client == nil || sessionID.IsZero() || threadID.IsZero() {
		return sessionprojection.SessionSnapshot{}, errSessionProjectionSnapshotMalformed
	}
	response, err := client.GetSessionSnapshot(ctx, &codefluxv1.GetSessionSnapshotRequest{
		SessionId: &codefluxv1.StableIdentity{
			Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, Value: sessionID.String(),
		},
	})
	if err != nil {
		return sessionprojection.SessionSnapshot{}, err
	}
	decoded, err := decodeSessionProjectionSnapshot(response.GetSnapshot())
	if err != nil {
		return sessionprojection.SessionSnapshot{}, err
	}
	if decoded.Session.SessionID != sessionID || decoded.Session.ThreadID != threadID {
		return sessionprojection.SessionSnapshot{}, errSessionProjectionSnapshotMalformed
	}
	if !taskID.IsZero() && (decoded.Session.TaskID == nil || *decoded.Session.TaskID != taskID) {
		return sessionprojection.SessionSnapshot{}, errSessionProjectionSnapshotMalformed
	}
	return decoded, nil
}

// decodeSessionProjectionSnapshot maps every correctness-bearing wire fact to
// the sole authoritative frontend snapshot entry point. It never consults or
// merges TaskView, whose query projection is not bound to a session sequence.
func decodeSessionProjectionSnapshot(value *codefluxv1.SessionProjectionSnapshot) (sessionprojection.SessionSnapshot, error) {
	if value == nil || value.GetObservedAt() == nil || value.GetObservedAt().CheckValid() != nil {
		return sessionprojection.SessionSnapshot{}, errSessionProjectionSnapshotMalformed
	}
	sessionID, err := decodeSessionSnapshotIdentity(value.GetSessionId())
	if err != nil {
		return sessionprojection.SessionSnapshot{}, errSessionProjectionSnapshotMalformed
	}
	threadID, err := decodeThreadIdentity(value.GetThreadId())
	if err != nil {
		return sessionprojection.SessionSnapshot{}, errSessionProjectionSnapshotMalformed
	}
	observedAt := value.GetObservedAt().AsTime().UTC()
	result := sessionprojection.SessionSnapshot{Session: events.SessionSnapshot{
		SessionID: sessionID, ThreadID: threadID, ThroughSequence: value.GetThroughSequence(),
		SnapshotVersion: 1, CreatedAt: observedAt,
	}, GraphRevision: value.GetGraphRevision()}
	if value.GetTaskId() == nil {
		if snapshotCarriesTaskFacts(value) {
			return sessionprojection.SessionSnapshot{}, errSessionProjectionSnapshotMalformed
		}
		return result, nil
	}
	taskID, err := decodeTaskIdentity(value.GetTaskId())
	state := domain.TaskState(value.GetTaskState())
	if err != nil || !state.IsValid() || value.GetTaskRevision() == 0 && state != domain.TaskStateDraft {
		return sessionprojection.SessionSnapshot{}, errSessionProjectionSnapshotMalformed
	}
	result.Session.TaskID = &taskID
	result.Session.TaskState = state
	result.Session.TaskRevision = value.GetTaskRevision()
	projection := taskprojection.TaskProjection{
		TaskID: taskID, State: state, Revision: value.GetTaskRevision(), LastSequence: value.GetThroughSequence(),
		Recovery:       taskprojection.RecoveryNone,
		PendingCommand: taskprojection.CommandState{Status: taskprojection.CommandIdle},
	}
	if err := decodeSessionSnapshotTaskFacts(value, &projection, &result.Session); err != nil {
		return sessionprojection.SessionSnapshot{}, err
	}
	if _, err := taskprojection.ApplySnapshot(taskprojection.Snapshot{Projection: projection}); err != nil {
		return sessionprojection.SessionSnapshot{}, fmt.Errorf("%w: %v", errSessionProjectionSnapshotMalformed, err)
	}
	result.Task = &taskprojection.Snapshot{Projection: projection}
	return result, nil
}

func decodeSessionSnapshotTaskFacts(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection, session *events.SessionSnapshot) error {
	if err := decodeSnapshotPlan(value, projection); err != nil {
		return fmt.Errorf("plan snapshot: %w", err)
	}
	if err := decodeSnapshotTool(value, projection); err != nil {
		return fmt.Errorf("tool snapshot: %w", err)
	}
	if err := decodeSnapshotApproval(value, projection); err != nil {
		return fmt.Errorf("approval snapshot: %w", err)
	}
	if err := decodeSnapshotBudget(value, projection); err != nil {
		return fmt.Errorf("budget snapshot: %w", err)
	}
	if err := decodeSnapshotValidation(value, projection, session); err != nil {
		return fmt.Errorf("validation snapshot: %w", err)
	}
	if err := decodeSnapshotCheckpoint(value, projection, session); err != nil {
		return fmt.Errorf("checkpoint snapshot: %w", err)
	}
	if err := decodeSnapshotRecovery(value, projection); err != nil {
		return fmt.Errorf("recovery snapshot: %w", err)
	}
	if err := decodeSnapshotAcceptance(value, projection, session); err != nil {
		return fmt.Errorf("acceptance snapshot: %w", err)
	}
	if err := decodeSnapshotReviewGraphPolicy(value, projection); err != nil {
		return fmt.Errorf("review graph policy snapshot: %w", err)
	}
	return nil
}

func decodeSnapshotPlan(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection) error {
	plan := value.GetPlan()
	approval := domain.ApprovalRequestState(value.GetPlanApprovalState())
	if plan == nil {
		if value.GetPlanApprovalState() != "" {
			return errSessionProjectionSnapshotMalformed
		}
		return nil
	}
	if plan.GetPlanRevision() == 0 || strings.TrimSpace(plan.GetRedactedSummary()) == "" || !approval.IsValid() {
		return errSessionProjectionSnapshotMalformed
	}
	projection.Plan = taskprojection.PlanProjection{Present: true, Revision: plan.GetPlanRevision(), RedactedSummary: plan.GetRedactedSummary(), Approval: approval}
	return nil
}

func decodeSnapshotTool(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection) error {
	tool := value.GetTool()
	if tool == nil {
		if value.GetToolRevision() != 0 {
			return errSessionProjectionSnapshotMalformed
		}
		return nil
	}
	state := domain.CommandExecutionState(tool.GetState())
	if value.GetToolRevision() == 0 || strings.TrimSpace(tool.GetExecutionId()) == "" || strings.TrimSpace(tool.GetCommandName()) == "" || !state.IsValid() {
		return errSessionProjectionSnapshotMalformed
	}
	projection.Tool = taskprojection.ToolProjection{Present: true, ExecutionID: tool.GetExecutionId(), CommandName: tool.GetCommandName(), State: state, Revision: value.GetToolRevision(), SafeSummary: tool.GetRedactedSummary()}
	return nil
}

func decodeSnapshotApproval(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection) error {
	approval := value.GetPendingApproval()
	if approval == nil {
		if value.GetApprovalRevision() != 0 {
			return errSessionProjectionSnapshotMalformed
		}
		return nil
	}
	id, err := decodeApprovalSnapshotIdentity(approval.GetApprovalId())
	state := domain.ApprovalRequestState(approval.GetState())
	if err != nil || value.GetApprovalRevision() == 0 || state != domain.ApprovalRequestStatePending {
		return errSessionProjectionSnapshotMalformed
	}
	projection.Approval = taskprojection.ApprovalProjection{Present: true, ID: id, State: state, Scope: approval.GetScope(), SafeReason: approval.GetRedactedReason(), Revision: value.GetApprovalRevision()}
	return nil
}

func decodeSnapshotBudget(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection) error {
	budget := value.GetBudget()
	if budget == nil {
		if value.GetBudgetRevision() != 0 {
			return errSessionProjectionSnapshotMalformed
		}
		return nil
	}
	currency, err := domain.ParseCurrencyCode(budget.GetCurrency())
	if err != nil {
		return errSessionProjectionSnapshotMalformed
	}
	hard, err := domain.NewMoney(currency, budget.GetHardLimitMinor())
	if err != nil {
		return errSessionProjectionSnapshotMalformed
	}
	reserved, err := domain.NewMoney(currency, budget.GetReservedMinor())
	if err != nil {
		return errSessionProjectionSnapshotMalformed
	}
	actual, err := domain.NewMoney(currency, budget.GetActualMinor())
	if err != nil {
		return errSessionProjectionSnapshotMalformed
	}
	projection.Budget = taskprojection.BudgetProjection{Present: true, Revision: value.GetBudgetRevision(), HardLimit: hard, Reserved: reserved, Actual: actual}
	return nil
}

func decodeSnapshotValidation(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection, session *events.SessionSnapshot) error {
	validation := value.GetValidation()
	if validation == nil {
		if value.GetValidationRevision() != 0 {
			return errSessionProjectionSnapshotMalformed
		}
		return nil
	}
	id, err := decodeValidationSnapshotIdentity(validation.GetValidationId())
	state := domain.ValidationState(validation.GetState())
	if err != nil || value.GetValidationRevision() == 0 || !state.IsValid() || validation.GetDiffRevision() == 0 {
		return errSessionProjectionSnapshotMalformed
	}
	projection.Validation = taskprojection.ValidationProjection{Present: true, ID: id, State: state, Required: validation.GetRequired(), Acknowledged: validation.GetAcknowledged(), SafeSummary: validation.GetRedactedSummary(), Revision: value.GetValidationRevision(), DiffRevision: validation.GetDiffRevision()}
	session.Validation = &events.Validation{ValidationID: id, State: state, RedactedSummary: validation.GetRedactedSummary(), Required: validation.GetRequired(), Acknowledged: validation.GetAcknowledged(), DiffRevision: validation.GetDiffRevision()}
	session.ValidationRevision = value.GetValidationRevision()
	return nil
}

func decodeSnapshotCheckpoint(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection, session *events.SessionSnapshot) error {
	checkpoint := value.GetCheckpoint()
	if checkpoint == nil {
		if value.GetCheckpointRevision() != 0 || value.GetCheckpointCreatedAt() != nil {
			return errSessionProjectionSnapshotMalformed
		}
		return nil
	}
	id, err := decodeCheckpointIdentity(checkpoint.GetCheckpointId())
	created := value.GetCheckpointCreatedAt()
	if err != nil || value.GetCheckpointRevision() == 0 || created == nil || created.CheckValid() != nil {
		return errSessionProjectionSnapshotMalformed
	}
	createdAt := created.AsTime().UTC()
	projection.Checkpoint = taskprojection.CheckpointProjection{Present: true, ID: id, TaskRevision: checkpoint.GetTaskRevision(), PlanStep: checkpoint.GetPlanStep(), CreatedAt: createdAt, Revision: value.GetCheckpointRevision()}
	session.Checkpoint = &events.Checkpoint{CheckpointID: id, TaskRevision: checkpoint.GetTaskRevision(), PlanStep: checkpoint.GetPlanStep()}
	session.CheckpointRevision = value.GetCheckpointRevision()
	return nil
}

func decodeSnapshotRecovery(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection) error {
	recovery := value.GetRecovery()
	if recovery == nil {
		if value.GetRecoveryRevision() != 0 {
			return errSessionProjectionSnapshotMalformed
		}
		return nil
	}
	classification := taskprojection.RecoveryClassification(recovery.GetClassification())
	if value.GetRecoveryRevision() == 0 || !classification.IsValid() {
		return errSessionProjectionSnapshotMalformed
	}
	detail := taskprojection.RecoveryProjection{Present: true, Revision: value.GetRecoveryRevision(), Classification: classification, SafeReason: recovery.GetRedactedReason(), DivergenceSummary: recovery.GetDivergenceSummary(), ExternalOutcomeAmbiguous: recovery.GetExternalOutcomeAmbiguous(), SafeResumeVerified: recovery.GetSafeResumeVerified(), ReconcileAvailable: recovery.GetReconcileAvailable(), PreservePatchAvailable: recovery.GetPreservePatchAvailable(), Bindings: revisionBindingsFromRecovery(recovery)}
	if recovery.GetCheckpointId() != nil {
		id, err := decodeCheckpointIdentity(recovery.GetCheckpointId())
		if err != nil {
			return errSessionProjectionSnapshotMalformed
		}
		detail.CheckpointID = &id
	}
	for _, identity := range recovery.GetRelatedEventIds() {
		id, err := decodeEventSnapshotIdentity(identity)
		if err != nil {
			return errSessionProjectionSnapshotMalformed
		}
		detail.RelatedEventIDs = append(detail.RelatedEventIDs, id)
	}
	detail.RelatedFiles = append([]string(nil), recovery.GetRelatedFiles()...)
	projection.Recovery, projection.RecoveryDetail = classification, detail
	return nil
}

func decodeSnapshotAcceptance(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection, session *events.SessionSnapshot) error {
	acceptance := value.GetChangeAcceptance()
	if acceptance == nil {
		if value.GetChangeAcceptanceRevision() != 0 {
			return errSessionProjectionSnapshotMalformed
		}
		return nil
	}
	state := domain.ChangeAcceptanceState(acceptance.GetState())
	bindings := revisionBindingsFromAcceptance(acceptance)
	if value.GetChangeAcceptanceRevision() == 0 || !state.IsValid() {
		return errSessionProjectionSnapshotMalformed
	}
	projection.Acceptance = taskprojection.AcceptanceProjection{Present: true, State: state, Revision: value.GetChangeAcceptanceRevision(), Bindings: bindings}
	session.ChangeAcceptance = &events.ChangeAcceptance{State: state, Bindings: eventBindings(bindings)}
	session.ChangeAcceptanceRevision = value.GetChangeAcceptanceRevision()
	return nil
}

func decodeSnapshotReviewGraphPolicy(value *codefluxv1.SessionProjectionSnapshot, projection *taskprojection.TaskProjection) error {
	bindings := value.GetReviewBindings()
	if bindings == nil {
		if value.GetReviewRevision() != 0 {
			return errSessionProjectionSnapshotMalformed
		}
	} else {
		if value.GetReviewRevision() == 0 {
			return errSessionProjectionSnapshotMalformed
		}
		projection.Review = taskprojection.ReviewProjection{Present: true, Revision: value.GetReviewRevision(), Bindings: revisionBindingsFromProto(bindings)}
	}
	if value.GetGraphRevision() > 0 {
		projection.Graph = taskprojection.GraphProjection{Present: true, Revision: value.GetGraphRevision()}
	}
	for _, raw := range value.GetDeniedTaskActions() {
		action := taskprojection.ActionKind(raw)
		if !action.IsValid() {
			return errSessionProjectionSnapshotMalformed
		}
		projection.Policy.Denied = append(projection.Policy.Denied, action)
	}
	projection.Policy.SafeReason = value.GetTaskActionPolicyReason()
	if (len(projection.Policy.Denied) > 0 && strings.TrimSpace(projection.Policy.SafeReason) == "") ||
		(len(projection.Policy.Denied) == 0 && projection.Policy.SafeReason != "") {
		return errSessionProjectionSnapshotMalformed
	}
	return nil
}

func snapshotCarriesTaskFacts(value *codefluxv1.SessionProjectionSnapshot) bool {
	return value.GetTaskState() != "" || value.GetTaskRevision() != 0 || value.GetPlan() != nil || value.GetPlanApprovalState() != "" || value.GetPendingApproval() != nil || value.GetApprovalRevision() != 0 || value.GetBudget() != nil || value.GetBudgetRevision() != 0 || value.GetValidation() != nil || value.GetValidationRevision() != 0 || value.GetCheckpoint() != nil || value.GetCheckpointRevision() != 0 || value.GetCheckpointCreatedAt() != nil || value.GetRecovery() != nil || value.GetRecoveryRevision() != 0 || value.GetChangeAcceptance() != nil || value.GetChangeAcceptanceRevision() != 0 || value.GetTool() != nil || value.GetToolRevision() != 0 || value.GetReviewBindings() != nil || value.GetReviewRevision() != 0 || value.GetGraphRevision() != 0 || len(value.GetDeniedTaskActions()) != 0 || value.GetTaskActionPolicyReason() != ""
}

func revisionBindingsFromProto(value *codefluxv1.SessionRevisionBindings) taskprojection.RevisionBindings {
	return taskprojection.RevisionBindings{Diff: value.GetDiffRevision(), Plan: value.GetPlanRevision(), Validation: value.GetValidationRevision(), Evidence: value.GetEvidenceRevision(), Graph: value.GetGraphRevision()}
}

func revisionBindingsFromAcceptance(value *codefluxv1.ChangeAcceptanceEvent) taskprojection.RevisionBindings {
	return taskprojection.RevisionBindings{Diff: value.GetDiffRevision(), Plan: value.GetPlanRevision(), Validation: value.GetValidationRevision(), Evidence: value.GetEvidenceRevision(), Graph: value.GetGraphRevision()}
}

func revisionBindingsFromRecovery(value *codefluxv1.RecoveryRequiredEvent) taskprojection.RevisionBindings {
	return taskprojection.RevisionBindings{Diff: value.GetDiffRevision(), Plan: value.GetPlanRevision(), Validation: value.GetValidationRevision(), Evidence: value.GetEvidenceRevision(), Graph: value.GetGraphRevision()}
}

func eventBindings(value taskprojection.RevisionBindings) events.RevisionBindings {
	return events.RevisionBindings{Diff: value.Diff, Plan: value.Plan, Validation: value.Validation, Evidence: value.Evidence, Graph: value.Graph}
}

func decodeSessionSnapshotIdentity(value *codefluxv1.StableIdentity) (domain.SessionID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION {
		return domain.SessionID{}, domain.ErrInvalidID
	}
	return domain.ParseSessionID(value.GetValue())
}

func decodeApprovalSnapshotIdentity(value *codefluxv1.StableIdentity) (domain.ApprovalID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL {
		return domain.ApprovalID{}, domain.ErrInvalidID
	}
	return domain.ParseApprovalID(value.GetValue())
}

func decodeValidationSnapshotIdentity(value *codefluxv1.StableIdentity) (domain.ValidationID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION {
		return domain.ValidationID{}, domain.ErrInvalidID
	}
	return domain.ParseValidationID(value.GetValue())
}

func decodeEventSnapshotIdentity(value *codefluxv1.StableIdentity) (domain.EventID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVENT {
		return domain.EventID{}, domain.ErrInvalidID
	}
	return domain.ParseEventID(value.GetValue())
}
